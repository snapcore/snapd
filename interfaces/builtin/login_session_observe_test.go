// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type LoginSessionObserveSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&LoginSessionObserveSuite{
	iface: builtin.MustInterface("login-session-observe"),
})

const loginObserveMockPlugSnapInfo = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [login-session-observe]
`

func (s *LoginSessionObserveSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "login-session-observe",
		Interface: "login-session-observe",
		Apps: map[string]*snap.AppInfo{
			"app1": {
				Snap: &snap.Info{
					SuggestedName: "core",
				},
				Name: "app1",
			},
		},
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)

	plugSnap := snaptest.MockInfo(c, loginObserveMockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["login-session-observe"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *LoginSessionObserveSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "login-session-observe")
}

func (s *LoginSessionObserveSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *LoginSessionObserveSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *LoginSessionObserveSuite) TestAppArmor(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "/{,usr/}bin/who")
}

func (s *LoginSessionObserveSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
