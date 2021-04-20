// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package servicestate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

// ServiceManager is responsible for starting and stopping snap services.
type ServiceManager struct {
	state *state.State

	ensuredSnapSvcs bool
}

// Manager returns a new service manager.
func Manager(st *state.State, runner *state.TaskRunner) *ServiceManager {
	delayedCrossMgrInit()
	m := &ServiceManager{
		state: st,
	}
	// TODO: undo handler
	runner.AddHandler("service-control", m.doServiceControl, nil)
	return m
}

func MockEnsuredSnapServices(mgr *ServiceManager, ensured bool) (restore func()) {
	osutil.MustBeTestBinary("ensured snap services can only be mocked from tests")
	old := mgr.ensuredSnapSvcs
	mgr.ensuredSnapSvcs = ensured
	return func() {
		mgr.ensuredSnapSvcs = old
	}
}

func restartServicesKilledInSnapdSnapRefresh(modified map[*snap.Info][]*snap.AppInfo) error {
	// we decide on which services to restart by identifying (out of the set of
	// services we just modified) services that were stopped after
	// usr-lib-snapd.mount was written, but before usr-lib-snapd.mount was last
	// stopped - this is the time window in which snapd (accidentally) killed
	// all snap services using Requires=, see LP #1924805 for full details, so
	// we need to undo that by restarting those snaps

	// TODO: use the var from core18.go here instead
	st, err := os.Stat(filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit))
	if err != nil {
		return err
	}

	// TODO: we should check if usr-lib-snapd.mount was modified before the
	// current boot time, if it was then we can just skip this since we know
	// any service stops that happened were unrelated
	lowerTimeBound := st.ModTime()

	// Get the InactiveEnterTimestamp property for the usr-lib-snapd.mount unit,
	// this is the time that usr-lib-snapd.mount was transitioned from
	// deactivating to inactive and was done being started. This is the correct
	// upper bound for our window in which systemd killed snap services because
	// systemd orders the transactions when we stop usr-lib-snapd.mount thusly:
	//
	// 1. Find all units which have Requires=usr-lib-snapd.mount (all snap
	//    services which would have been refreshed during snapd 2.49.2)
	// 2. Stop all such services found in 1.
	// 3. Stop usr-lib-snapd.mount itself.
	//
	// Thus the time after all the services were killed is given by the time
	// that systemd transitioned usr-lib-snapd.mount to inactive, which is given
	// by InactiveEnterTimestamp.

	// TODO: pass a real interactor here?
	sysd := systemd.New(systemd.SystemMode, progress.Null)

	upperTimeBound, err := sysd.InactiveEnterTimestamp(wrappers.SnapdToolingMountUnit)
	if err != nil {
		return err
	}

	if upperTimeBound.IsZero() {
		// this means that the usr-lib-snapd.mount unit never exited during this
		// boot, which means we are done in this ensure because the bug we care
		// about (LP #1924805) here was never triggered
		return nil
	}

	candidateAppsToRestartBySnap := make(map[*snap.Info][]*snap.AppInfo)

	for sn, apps := range modified {
		for _, app := range apps {
			// get the InactiveEnterTimestamp for the service
			t, err := sysd.InactiveEnterTimestamp(app.ServiceName())
			if err != nil {
				return err
			}

			// check if this unit entered the inactive state between the time
			// range, but be careful about time precision here, we want an
			// inclusive range i.e. [lower,upper] not (lower,upper) in case the
			// time that systemd saves these events as is imprecise or slow and
			// things get saved as having happened at the exact same time
			if !t.Before(lowerTimeBound) && !t.After(upperTimeBound) {
				candidateAppsToRestartBySnap[sn] = append(candidateAppsToRestartBySnap[sn], app)
			}
		}
	}

	// Second loop actually restarts the services per-snap by sorting them and
	// removing disabled services. Note that we could have disabled services
	// here because a service could have been running, but disabled when snapd
	// was refreshed, hence it got killed, but we don't want to restart it,
	// since it is disabled, and so that disabled running service is just SOL.
	for sn, apps := range candidateAppsToRestartBySnap {
		// TODO: should we try to start as many services as possible here before
		// giving up given the severity of the bug?
		disabledSvcs, err := wrappers.QueryDisabledServices(sn, progress.Null)
		if err != nil {
			return err
		}

		startupOrdered, err := snap.SortServices(apps)
		if err != nil {
			return err
		}

		// TODO: what to do about timings here?
		nullPerfTimings := &timings.Timings{}
		if err := wrappers.StartServices(startupOrdered, disabledSvcs, nil, progress.Null, nullPerfTimings); err != nil {
			return err
		}
	}

	return nil
}

func (m *ServiceManager) ensureSnapServicesUpdated() (err error) {
	m.state.Lock()
	defer m.state.Unlock()
	if m.ensuredSnapSvcs {
		return nil
	}

	// only run after we are seeded
	var seeded bool
	err = m.state.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if !seeded {
		return nil
	}

	// we are seeded, now we need to find all snap services and re-generate
	// services as necessary

	// ensure all snap services are updated
	allStates, err := snapstate.All(m.state)
	if err != nil && err != state.ErrNoState {
		return err
	}

	// if we have no snaps we can exit early
	if len(allStates) == 0 {
		m.ensuredSnapSvcs = true
		return nil
	}

	snapsMap := map[*snap.Info]*wrappers.SnapServiceOptions{}

	for _, snapSt := range allStates {
		info, err := snapSt.CurrentInfo()
		if err != nil {
			return err
		}

		// don't use EnsureSnapServices with the snapd snap
		if info.Type() == snap.TypeSnapd {
			continue
		}

		snapSvcOpts, err := SnapServiceOptions(m.state, info.InstanceName())
		if err != nil {
			return err
		}
		snapsMap[info] = snapSvcOpts
	}

	// setup ensure options
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: snapdenv.Preseeding(),
	}

	// set RequireMountedSnapdSnap if we are on UC18+ only
	deviceCtx, err := snapstate.DeviceCtx(m.state, nil, nil)
	if err != nil {
		return err
	}

	if !deviceCtx.Classic() && deviceCtx.Model().Base() != "" {
		ensureOpts.RequireMountedSnapdSnap = true
	}

	// TODO: should we use an actual interacter here ?

	// TODO: this should take a callback so that we can gate the logic about
	// restarting services below on actually observing a Requires= to Wants=
	// transition which is what we need to do to fix LP #1924805
	modified, err := wrappers.EnsureSnapServices(snapsMap, ensureOpts, progress.Null)
	if err != nil {
		return err
	}

	// if nothing was modified or we are not on UC18+, we are done
	if len(modified) == 0 || deviceCtx.Classic() || deviceCtx.Model().Base() == "" {
		m.ensuredSnapSvcs = true
		return nil
	}

	// otherwise, we know now that we have rewritten some snap services, we need
	// to handle the case of LP #1924805, and restart any services that were
	// accidentally killed when we refreshed snapd
	if err := restartServicesKilledInSnapdSnapRefresh(modified); err != nil {
		// we failed to restart services that were killed by a snapd refresh, so
		// we need to immediately reboot in the hopes that this restores
		// services to a functioning state

		// TODO: implement reboot here
		m.state.RequestRestart(state.RestartSystemNow)
		return fmt.Errorf("error trying to restart killed services, immediately rebooting: %v", err)
	}

	m.ensuredSnapSvcs = true

	return nil
}

// Ensure implements StateManager.Ensure.
func (m *ServiceManager) Ensure() error {
	if err := m.ensureSnapServicesUpdated(); err != nil {
		return err
	}
	return nil
}

func delayedCrossMgrInit() {
	// hook into conflict checks mechanisms
	snapstate.AddAffectedSnapsByAttr("service-action", serviceControlAffectedSnaps)
	snapstate.SnapServiceOptions = SnapServiceOptions
}

func serviceControlAffectedSnaps(t *state.Task) ([]string, error) {
	var serviceAction ServiceAction
	if err := t.Get("service-action", &serviceAction); err != nil {
		return nil, fmt.Errorf("internal error: cannot obtain service action from task: %s", t.Summary())
	}
	return []string{serviceAction.SnapName}, nil
}
