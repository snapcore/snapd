// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

var errNoSnap = errors.New("snap not installed")

// snapIcon tries to find the icon inside the snap
func snapIcon(info *snap.Info) string {
	// XXX: copy of snap.Snap.Icon which will go away
	found, _ := filepath.Glob(filepath.Join(info.MountDir(), "meta", "gui", "icon.*"))
	if len(found) == 0 {
		return info.IconURL
	}

	return found[0]
}

// snapDate returns the time of the snap mount directory.
func snapDate(info *snap.Info) time.Time {
	st, err := os.Stat(info.MountDir())
	if err != nil {
		return time.Time{}
	}

	return st.ModTime()
}

func publisherName(st *state.State, info *snap.Info) (string, error) {
	if info.SnapID == "" {
		return "", nil
	}

	pubAcct, err := assertstate.Publisher(st, info.SnapID)
	if err != nil {
		return "", fmt.Errorf("cannot find publisher details: %v", err)
	}
	return pubAcct.Username(), nil
}

type aboutSnap struct {
	info      *snap.Info
	snapst    *snapstate.SnapState
	publisher string
}

// localSnapInfo returns the information about the current snap for the given name plus the SnapState with the active flag and other snap revisions.
func localSnapInfo(st *state.State, name string) (aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return aboutSnap{}, fmt.Errorf("cannot consult state: %v", err)
	}

	info, err := snapst.CurrentInfo()
	if err == snapstate.ErrNoCurrent {
		return aboutSnap{}, errNoSnap
	}
	if err != nil {
		return aboutSnap{}, fmt.Errorf("cannot read snap details: %v", err)
	}

	publisher, err := publisherName(st, info)
	if err != nil {
		return aboutSnap{}, err
	}

	return aboutSnap{
		info:      info,
		snapst:    &snapst,
		publisher: publisher,
	}, nil
}

// allLocalSnapInfos returns the information about the all current snaps and their SnapStates.
func allLocalSnapInfos(st *state.State, all bool, wanted map[string]bool) ([]aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	if err != nil {
		return nil, err
	}
	about := make([]aboutSnap, 0, len(snapStates))

	var firstErr error
	for name, snapst := range snapStates {
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		var aboutThis []aboutSnap
		var info *snap.Info
		var publisher string
		var err error
		if all {
			for _, seq := range snapst.Sequence {
				info, err = snap.ReadInfo(seq.RealName, seq)
				if err != nil {
					break
				}
				publisher, err = publisherName(st, info)
				aboutThis = append(aboutThis, aboutSnap{info, snapst, publisher})
			}
		} else {
			info, err = snapst.CurrentInfo()
			if err == nil {
				var publisher string
				publisher, err = publisherName(st, info)
				aboutThis = append(aboutThis, aboutSnap{info, snapst, publisher})
			}
		}

		if err != nil {
			// XXX: aggregate instead?
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		about = append(about, aboutThis...)
	}

	return about, firstErr
}

// screenshotJSON contains the json for snap.ScreenshotInfo
type screenshotJSON struct {
	URL    string `json:"url"`
	Width  int64  `json:"width,omitempty"`
	Height int64  `json:"height,omitempty"`
}

func mapLocal(about aboutSnap) map[string]interface{} {
	localSnap, snapst := about.info, about.snapst
	status := "installed"
	if snapst.Active && localSnap.Revision == snapst.Current {
		status = "active"
	}

	appNames := make([]string, 0, len(localSnap.Apps))
	for appName := range localSnap.Apps {
		appNames = append(appNames, appName)
	}
	sort.Strings(appNames)
	apps := make([]*client.AppInfo, len(localSnap.Apps))
	// TODO: pass in an actual notifier here instead of nil
	//       (Status doesn't _need_ it, but benefits from it)
	sysd := systemd.New(dirs.GlobalRootDir, nil)
	for i, appName := range appNames {
		app := localSnap.Apps[appName]
		apps[i] = &client.AppInfo{Name: app.Name}
		if fn := app.DesktopFile(); osutil.FileExists(fn) {
			apps[i].DesktopFile = fn
		}

		if app.IsService() {
			// TODO: look into making a single call to Status for all services
			if sts, err := sysd.Status(app.ServiceName()); err != nil {
				logger.Noticef("cannot get status of service %q: %v", app.Name, err)
			} else if len(sts) != 1 {
				logger.Noticef("cannot get status of service %q: expected 1 result, got %d", app.Name, len(sts))
			} else {
				apps[i].ServiceInfo = &client.ServiceInfo{
					Daemon:          sts[0].Daemon,
					ServiceFileName: sts[0].ServiceFileName,
					Enabled:         sts[0].Enabled,
					Active:          sts[0].Active,
				}
			}
		}
	}

	// TODO: expose aliases information and state?

	result := map[string]interface{}{
		"description":      localSnap.Description(),
		"developer":        about.publisher,
		"icon":             snapIcon(localSnap),
		"id":               localSnap.SnapID,
		"install-date":     snapDate(localSnap),
		"installed-size":   localSnap.Size,
		"name":             localSnap.Name(),
		"revision":         localSnap.Revision,
		"status":           status,
		"summary":          localSnap.Summary(),
		"type":             string(localSnap.Type),
		"version":          localSnap.Version,
		"channel":          localSnap.Channel,
		"tracking-channel": snapst.Channel,
		"confinement":      localSnap.Confinement,
		"devmode":          snapst.DevMode,
		"trymode":          snapst.TryMode,
		"jailmode":         snapst.JailMode,
		"private":          localSnap.Private,
		"apps":             apps,
		"broken":           localSnap.Broken,
		"contact":          localSnap.Contact,
	}

	if localSnap.Title() != "" {
		result["title"] = localSnap.Title()
	}

	return result
}

func mapRemote(remoteSnap *snap.Info) map[string]interface{} {
	status := "available"
	if remoteSnap.MustBuy {
		status = "priced"
	}

	confinement := remoteSnap.Confinement
	if confinement == "" {
		confinement = snap.StrictConfinement
	}

	screenshots := make([]screenshotJSON, len(remoteSnap.Screenshots))
	for i, screenshot := range remoteSnap.Screenshots {
		screenshots[i] = screenshotJSON{
			URL:    screenshot.URL,
			Width:  screenshot.Width,
			Height: screenshot.Height,
		}
	}

	result := map[string]interface{}{
		"description":   remoteSnap.Description(),
		"developer":     remoteSnap.Publisher,
		"download-size": remoteSnap.Size,
		"icon":          snapIcon(remoteSnap),
		"id":            remoteSnap.SnapID,
		"name":          remoteSnap.Name(),
		"revision":      remoteSnap.Revision,
		"status":        status,
		"summary":       remoteSnap.Summary(),
		"type":          string(remoteSnap.Type),
		"version":       remoteSnap.Version,
		"channel":       remoteSnap.Channel,
		"private":       remoteSnap.Private,
		"confinement":   confinement,
		"contact":       remoteSnap.Contact,
	}

	if remoteSnap.Title() != "" {
		result["title"] = remoteSnap.Title()
	}

	if len(screenshots) > 0 {
		result["screenshots"] = screenshots
	}

	if len(remoteSnap.Prices) > 0 {
		result["prices"] = remoteSnap.Prices
	}

	if len(remoteSnap.Channels) > 0 {
		result["channels"] = remoteSnap.Channels
	}

	if len(remoteSnap.Tracks) > 0 {
		result["tracks"] = remoteSnap.Tracks
	}

	return result
}
