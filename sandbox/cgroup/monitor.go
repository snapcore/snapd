// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package cgroup

import (
	"errors"
	"path"
	"sync"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/inotify"
)

// inotifyWatcher manages the inotify watcher, allowing to have a single watch descriptor open
type inotifyWatcher struct {
	wd *inotify.Watcher

	// Contains the list of groups to monitor, to detect when they have been deleted
	groupList []*groupToWatch

	// Contains the list of paths being monitored by the inotify watcher.
	// The paths monitored aren't the CGroup paths, but the parent ones, because
	// in /sys/fs is not possible to detect the InDeleteSelf message, only the
	// InDelete one, so we must monitor the parent folder and detect when a
	// specific folder, corresponding to a CGroup, has been deleted.
	// Each path has associated an integer, which is an usage counter. Every
	// time a new CGroup paths have to be monitored, the parent paths are
	// checked in the map, and if they already exist, the usage counter is
	// incremented by one (of course, if a parent path isn't in the map,
	// it is initialized to one). Every time the path of a CGroup is deleted,
	// the corresponding usage counter here corresponding to the parent path
	// is decremented, and when it reaches zero, it is removed and inotify is
	// notified to stop monitoring it.
	pathList map[string]int32

	// serialize operations
	mu sync.Mutex
}

// Contains the data corresponding to a CGroup that must be watched to detect
// when it is destroyed
type groupToWatch struct {
	// This is the CGroup identifier
	name string
	// This contains a list of folders to monitor. When all of them have been
	// deleted, the CGroup has been destroyed and there are no processes running
	folders []string

	// This channel is used to notify to the requester that this CGroup has been
	// destroyed. The watcher writes the CGroup identifier on it; this way, the
	// same channel can be shared to monitor several CGroups.
	channel chan<- string
}

func (gtw *groupToWatch) sendClosedNotification() {
	defer func() {
		if err := recover(); err != nil {
			logger.Noticef("Failed to send Closed notification for %s", gtw.name)
		}
	}()
	gtw.channel <- gtw.name
}

// newInotifyWatcher will create a new inotifyWatcher and starts
// a monitor go-routine. Use "Stop()" when finished.
func newInotifyWatcher() (*inotifyWatcher, error) {
	wd, err := inotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if wd == nil {
		return nil, errors.New("cannot initialise inotify")
	}
	iw := &inotifyWatcher{
		wd:       wd,
		pathList: make(map[string]int32),
	}
	go iw.watcherMainLoop()
	return iw, nil
}

// MonitorDelete monitors the specified paths for deletion.
// Once all of them have been deleted, it pushes the specified name through the channel.
func (iw *inotifyWatcher) MonitorDelete(folders []string, name string, channel chan<- string) error {
	iw.mu.Lock()
	defer iw.mu.Unlock()

	newWatch := &groupToWatch{
		name:    name,
		folders: folders,
		channel: channel,
	}

	var folderList []string
	for _, fullPath := range newWatch.folders {
		// It's not possible to use inotify.InDeleteSelf in /sys/fs because it
		// isn't triggered, so we must monitor the parent folder and use InDelete
		basePath := path.Dir(fullPath)
		if _, exists := iw.pathList[basePath]; !exists {
			iw.pathList[basePath] = 0
			if err := iw.wd.AddWatch(basePath, inotify.InDelete); err != nil {
				return err
			}
		}
		iw.pathList[fullPath]++
		if osutil.FileExists(fullPath) {
			folderList = append(folderList, fullPath)
		} else {
			iw.removePath(fullPath)
		}
	}
	if len(folderList) == 0 {
		// XXX: this has to be a go-routine to support un-buffered channels
		go newWatch.sendClosedNotification()
	} else {
		newWatch.folders = folderList
		iw.groupList = append(iw.groupList, newWatch)
	}

	return nil
}

func (iw *inotifyWatcher) removePath(fullPath string) {
	iw.pathList[fullPath]--
	if iw.pathList[fullPath] == 0 {
		iw.wd.RemoveWatch(fullPath)
		delete(iw.pathList, fullPath)
	}
}

// processDeletedPath checks if the received path corresponds to the passed
// CGroup, removing it from the list of folders being watched in that CGroup if
// needed. It returns true if there remain folders to be monitored in that CGroup,
// or false if all the folders of that CGroup have been deleted.
func (iw *inotifyWatcher) processDeletedPath(watch *groupToWatch, deletedPath string) bool {
	for i, fullPath := range watch.folders {
		if fullPath == deletedPath {
			// if the folder name is in the list of folders to monitor, decrement the
			// parent's usage counter, and remove it from the list of folders to watch
			// in this CGroup (by not adding it to tmpFolders)
			iw.removePath(fullPath)
			watch.folders = append(watch.folders[:i], watch.folders[i+1:]...)
			break
		}
	}
	if len(watch.folders) == 0 {
		// if all the files/folders of this CGroup have been deleted, notify the
		// client that it is done.
		watch.sendClosedNotification()
		return false
	}
	return true
}

func (iw *inotifyWatcher) watcherMainLoop() {
	for {
		select {
		case err, ok := <-iw.wd.Error:
			if err != nil {
				logger.Debugf("failed to read inotify event: %s", err)
			}
			if !ok {
				// the Error channel is closed after Event so there's nothing else to read
				return
			}
		case event, ok := <-iw.wd.Event:
			if !ok {
				// the Error channel is closed next so check it for errors before returning
				continue
			}
			if event.Mask&inotify.InDelete == 0 {
				continue
			}
			var newGroupList []*groupToWatch
			for _, watch := range iw.groupList {
				if iw.processDeletedPath(watch, event.Name) {
					newGroupList = append(newGroupList, watch)
				}
			}
			iw.groupList = newGroupList
		}
	}
}

func (iw *inotifyWatcher) Stop() error {
	return iw.wd.Close()
}

// SnapWatcher can monitor the snap related cgroups
type SnapWatcher struct {
	iw *inotifyWatcher
}

// NewSnapWatcher returns a watcher that can monitor when all processes
// of a snap are finished.
//
// Use "Stop()" when done with the watching.
func NewSnapWatcher() (*SnapWatcher, error) {
	iw, err := newInotifyWatcher()
	if err != nil {
		return nil, err
	}
	return &SnapWatcher{iw: iw}, nil
}

// AllProcessesEnded monitors the running instances of a snap. Once all
// instances of the snap have stopped, its name is pushed through the supplied
// channel. This allows the caller to use the same channel to monitor several snaps.
func (sw *SnapWatcher) AllProcessesEnded(snapName string, channel chan string) error {
	options := InstancePathsOptions{
		ReturnCGroupPath: true,
	}
	paths, err := InstancePathsOfSnap(snapName, options)
	if err != nil {
		return err
	}
	return sw.iw.MonitorDelete(paths, snapName, channel)
}

func (sw *SnapWatcher) Stop() {
	sw.iw.Stop()
}
