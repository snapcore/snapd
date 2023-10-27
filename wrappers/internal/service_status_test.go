// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package internal_test

import (
	"fmt"
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	_ "github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/agent"
	"github.com/snapcore/snapd/wrappers"
	"github.com/snapcore/snapd/wrappers/internal"
)

type serviceStatusSuite struct {
	testutil.DBusTest
	tempdir                           string
	sysdLog                           [][]string
	systemctlRestorer, delaysRestorer func()
	agent                             *agent.SessionAgent
}

var _ = Suite(&serviceStatusSuite{})

func (s *serviceStatusSuite) SetUpTest(c *C) {
	s.DBusTest.SetUpTest(c)
	s.tempdir = c.MkDir()
	s.sysdLog = nil
	dirs.SetRootDir(s.tempdir)

	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	s.delaysRestorer = systemd.MockStopDelays(2*time.Millisecond, 4*time.Millisecond)

	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	err := os.MkdirAll(xdgRuntimeDir, 0700)
	c.Assert(err, IsNil)
	s.agent, err = agent.New()
	c.Assert(err, IsNil)
	s.agent.Start()
}

func (s *serviceStatusSuite) TearDownTest(c *C) {
	if s.agent != nil {
		err := s.agent.Stop()
		c.Check(err, IsNil)
	}
	s.systemctlRestorer()
	s.delaysRestorer()
	dirs.SetRootDir("")
	s.DBusTest.TearDownTest(c)
}

// addSnapServices adds service units for the applications from the snap which
// are services. The services do not get enabled or started.
func (s *serviceStatusSuite) addSnapServices(snapInfo *snap.Info, preseeding bool) error {
	m := map[*snap.Info]*wrappers.SnapServiceOptions{
		snapInfo: nil,
	}
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding: preseeding,
	}
	return wrappers.EnsureSnapServices(m, ensureOpts, nil, progress.Null)
}

func (s *serviceStatusSuite) TestQueryServiceStatusMany(c *C) {
	const surviveYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
  bar:
    command: bin/bar
    daemon: simple
`
	info := snaptest.MockSnap(c, surviveYaml, &snap.SideInfo{Revision: snap.R(1)})
	fooSrvFile := "snap.test-snap.foo.service"
	barSrvFile := "snap.test-snap.bar.service"

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}
		if cmd[0] == "--user" && cmd[1] == "show" {
			return []byte(`Type=simple
Id=snap.test-snap.foo.service
Names=snap.test-snap.foo.service
ActiveState=inactive
UnitFileState=enabled
NeedDaemonReload=no
`), nil
		}
		return []byte(`ActiveState=inactive`), nil
	})
	defer r()

	err := s.addSnapServices(info, false)
	c.Assert(err, IsNil)

	sysd := systemd.New(systemd.SystemMode, progress.Null)
	svcs, usrSvcs, err := internal.QueryServiceStatusMany(info.Services(), sysd)
	c.Assert(err, IsNil)
	c.Assert(svcs, HasLen, 1)
	c.Check(svcs[0].Name(), Equals, "bar")
	c.Check(svcs[0].User(), Equals, false)
	c.Check(svcs[0].ServiceUnitStatus(), DeepEquals, &systemd.UnitStatus{
		Daemon:           "simple",
		Id:               "snap.test-snap.bar.service",
		Name:             "snap.test-snap.bar.service",
		Names:            []string{"snap.test-snap.bar.service"},
		Enabled:          true,
		Active:           true,
		Installed:        true,
		NeedDaemonReload: false,
	})
	c.Assert(usrSvcs, HasLen, 1)
	c.Assert(usrSvcs[1000], HasLen, 1)
	c.Check(usrSvcs[1000][0].Name(), Equals, "foo")
	c.Check(usrSvcs[1000][0].User(), Equals, true)
	c.Check(usrSvcs[1000][0].ServiceUnitStatus(), DeepEquals, &systemd.UnitStatus{
		Daemon:           "simple",
		Id:               "snap.test-snap.foo.service",
		Name:             "snap.test-snap.foo.service",
		Names:            []string{"snap.test-snap.foo.service"},
		Enabled:          true,
		Active:           false, // ActiveState=inactive
		Installed:        true,
		NeedDaemonReload: false,
	})

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--user", "daemon-reload"},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", barSrvFile},
		{"--user", "show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", fooSrvFile},
	})
}

func (s *serviceStatusSuite) TestSnapServiceUnits(c *C) {
	const surviveYaml = `name: test-snap
version: 1.0
apps:
  foo:
    command: bin/foo
    daemon: simple
    daemon-scope: user
    timer: 10:00-12:00,20:00-22:00
    sockets:
      sock1:
       listen-stream: $SNAP_DATA/sock1.socket
      sock2:
       listen-stream: $SNAP_DATA/sock2.socket
`
	info := snaptest.MockSnap(c, surviveYaml, &snap.SideInfo{Revision: snap.R(1)})

	svc, activators := internal.SnapServiceUnits(info.Apps["foo"])
	c.Check(svc, Equals, "snap.test-snap.foo.service")

	// The activators must appear the in following order:
	// Sockets, sorted
	// Timer unit
	c.Check(activators, DeepEquals, []string{
		"snap.test-snap.foo.sock1.socket",
		"snap.test-snap.foo.sock2.socket",
		"snap.test-snap.foo.timer",
	})
}
