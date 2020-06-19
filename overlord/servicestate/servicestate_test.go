// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

package servicestate_test

import (
	"fmt"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

type statusDecoratorSuite struct{}

var _ = Suite(&statusDecoratorSuite{})

func (s *statusDecoratorSuite) TestDecorateWithStatus(c *C) {
	snp := &snap.Info{
		SuggestedName: "foo",
	}
	r := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		c.Assert(args[0], Equals, "show")
		unit := args[2]
		if strings.HasSuffix(unit, ".timer") || strings.HasSuffix(unit, ".socket") {
			return []byte(fmt.Sprintf(`Id=%s
ActiveState=active
UnitFileState=enabled
`, args[2])), nil
		} else {
			return []byte(fmt.Sprintf(`Id=%s
Type=simple
ActiveState=active
UnitFileState=enabled
`, args[2])), nil
		}
	})
	defer r()

	sd := servicestate.NewStatusDecorator(nil)

	// not a service
	app := &client.AppInfo{
		Snap: "foo",
		Name: "app",
	}
	snapApp := &snap.AppInfo{Snap: snp, Name: "app"}

	err := sd.DecorateWithStatus(app, snapApp)
	c.Assert(err, IsNil)

	// service only
	app = &client.AppInfo{
		Snap:   "foo",
		Name:   "svc",
		Daemon: "simple",
	}
	snapApp = &snap.AppInfo{
		Snap:   snp,
		Name:   "svc",
		Daemon: "simple",
	}

	err = sd.DecorateWithStatus(app, snapApp)
	c.Assert(err, IsNil)
	c.Check(app.Active, Equals, true)
	c.Check(app.Enabled, Equals, true)

	// service  + timer
	app = &client.AppInfo{
		Snap:   "foo",
		Name:   "svc",
		Daemon: "simple",
	}
	snapApp = &snap.AppInfo{
		Snap:        snp,
		Name:        "svc",
		Daemon:      "simple",
		DaemonScope: snap.SystemDaemon,
	}
	snapApp.Timer = &snap.TimerInfo{
		App:   snapApp,
		Timer: "10:00",
	}

	err = sd.DecorateWithStatus(app, snapApp)
	c.Assert(err, IsNil)
	c.Check(app.Active, Equals, true)
	c.Check(app.Enabled, Equals, true)
	c.Check(app.Activators, DeepEquals, []client.AppActivator{
		{Name: "svc", Type: "timer", Active: true, Enabled: true},
	})

	// service with socket
	app = &client.AppInfo{
		Snap:   "foo",
		Name:   "svc",
		Daemon: "simple",
	}
	snapApp = &snap.AppInfo{
		Snap:        snp,
		Name:        "svc",
		Daemon:      "simple",
		DaemonScope: snap.SystemDaemon,
	}
	snapApp.Sockets = map[string]*snap.SocketInfo{
		"socket1": {
			App:          snapApp,
			Name:         "socket1",
			ListenStream: "a.socket",
		},
	}

	err = sd.DecorateWithStatus(app, snapApp)
	c.Assert(err, IsNil)
	c.Check(app.Active, Equals, true)
	c.Check(app.Enabled, Equals, true)
	c.Check(app.Activators, DeepEquals, []client.AppActivator{
		{Name: "socket1", Type: "socket", Active: true, Enabled: true},
	})

}
