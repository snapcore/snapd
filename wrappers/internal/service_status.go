// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package internal

import (
	"context"
	"path/filepath"
	"sort"
	"time"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/usersession/client"
)

// ServiceStatus represents the status of a service, and any of its activation
// service units. It also provides a method isEnabled which can determine the true
// enable status for services that are activated.
type ServiceStatus struct {
	name        string
	user        bool
	service     *systemd.UnitStatus
	activators  []*systemd.UnitStatus
	slotEnabled bool
}

func (s *ServiceStatus) Name() string {
	return s.name
}

func (s *ServiceStatus) ServiceUnitStatus() *systemd.UnitStatus {
	return s.service
}

func (s *ServiceStatus) User() bool {
	return s.user
}

func (s *ServiceStatus) IsEnabled() bool {
	// If the service is slot activated, it cannot be disabled and thus always
	// is enabled.
	if s.slotEnabled {
		return true
	}

	// If there are no activator units, then return status of the
	// primary service.
	if len(s.activators) == 0 {
		return s.service.Enabled
	}

	// Just a single of those activators need to be enabled for us
	// to report the service as enabled.
	for _, a := range s.activators {
		if a.Enabled {
			return true
		}
	}
	return false
}

func appServiceUnitsMany(apps []*snap.AppInfo) (sys, usr []string) {
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		svc, activators := SnapServiceUnits(app)
		if app.DaemonScope == snap.SystemDaemon {
			sys = append(sys, svc)
			sys = append(sys, activators...)
		} else if app.DaemonScope == snap.UserDaemon {
			usr = append(usr, svc)
			usr = append(usr, activators...)
		}
	}
	return sys, usr
}

func serviceIsSlotActivated(app *snap.AppInfo) bool {
	return len(app.ActivatesOn) > 0
}

func userSessionQueryServiceStatusMany(units []string) (map[int][]client.UserServiceUnitStatus, error) {
	// Avoid any expensive call if there are no user daemons
	if len(units) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout.DefaultTimeout))
	defer cancel()
	cli := client.New()
	return cli.ServiceStatus(ctx, units)
}

func queryUserServiceStatusMany(apps []*snap.AppInfo, units []string) (map[int][]*ServiceStatus, error) {
	usrUnitStss, err := userSessionQueryServiceStatusMany(units)
	if err != nil {
		return nil, err
	}

	var usrIndex int
	getStatus := func(app *snap.AppInfo, uid int, activators []string) *ServiceStatus {
		svcSt := &ServiceStatus{
			name:        app.Name,
			user:        true,
			service:     usrUnitStss[uid][usrIndex].SystemdUnitStatus(),
			slotEnabled: serviceIsSlotActivated(app),
		}
		if len(activators) > 0 {
			for _, u := range usrUnitStss[uid][usrIndex+1 : usrIndex+1+len(activators)] {
				svcSt.activators = append(svcSt.activators, u.SystemdUnitStatus())
			}
		}
		usrIndex += 1 + len(activators)
		return svcSt
	}

	// For each user we have results from, go through services and build a list of service results
	svcsStatusMap := make(map[int][]*ServiceStatus)
	for uid := range usrUnitStss {
		var svcs []*ServiceStatus
		usrIndex = 0
		for _, app := range apps {
			if !app.IsService() {
				continue
			}
			if app.DaemonScope != snap.UserDaemon {
				continue
			}

			// This builds on the principle that sysd.Status returns service unit statuses
			// in the exact same order we requested them in.
			_, activators := SnapServiceUnits(app)
			svcs = append(svcs, getStatus(app, uid, activators))
		}
		svcsStatusMap[uid] = svcs
	}
	return svcsStatusMap, nil
}

func querySystemServiceStatusMany(sysd systemd.Systemd, apps []*snap.AppInfo, units []string) ([]*ServiceStatus, error) {
	sysUnitStss, err := sysd.Status(units)
	if err != nil {
		return nil, err
	}

	var sysIndex int
	getStatus := func(app *snap.AppInfo, activators []string) *ServiceStatus {
		svcSt := &ServiceStatus{
			name:        app.Name,
			service:     sysUnitStss[sysIndex],
			slotEnabled: serviceIsSlotActivated(app),
		}
		if len(activators) > 0 {
			svcSt.activators = sysUnitStss[sysIndex+1 : sysIndex+1+len(activators)]
		}
		sysIndex += 1 + len(activators)
		return svcSt
	}

	// For each of the system services, go through and build a service status result
	var svcsStatuses []*ServiceStatus
	for _, app := range apps {
		if !app.IsService() {
			continue
		}
		if app.DaemonScope != snap.SystemDaemon {
			continue
		}

		// This builds on the principle that sysd.Status returns service unit statuses
		// in the exact same order we requested them in.
		_, activators := SnapServiceUnits(app)
		svcsStatuses = append(svcsStatuses, getStatus(app, activators))
	}
	return svcsStatuses, nil
}

// QueryServiceStatusMany queries service statuses for all the provided apps. A list of system-service statuses
// is returned, and a map detailing the statuses of services per logged in user.
func QueryServiceStatusMany(apps []*snap.AppInfo, sysd systemd.Systemd) (sysSvcs []*ServiceStatus, userSvcs map[int][]*ServiceStatus, err error) {
	sysUnits, usrUnits := appServiceUnitsMany(apps)
	sysSvcs, err = querySystemServiceStatusMany(sysd, apps, sysUnits)
	if err != nil {
		return nil, nil, err
	}
	userSvcs, err = queryUserServiceStatusMany(apps, usrUnits)
	if err != nil {
		return nil, nil, err
	}
	return sysSvcs, userSvcs, nil
}

// SnapServiceUnits returns the service unit of the primary service, and a list
// of service units for the activation services.
func SnapServiceUnits(app *snap.AppInfo) (service string, activators []string) {
	// Add application sockets
	for _, socket := range app.Sockets {
		activators = append(activators, filepath.Base(socket.File()))
	}
	// Sort the results from sockets for consistency
	sort.Strings(activators)

	// Add application timer
	if app.Timer != nil {
		activators = append(activators, filepath.Base(app.Timer.File()))
	}
	return app.ServiceName(), activators
}
