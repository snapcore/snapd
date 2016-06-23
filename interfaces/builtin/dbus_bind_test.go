// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
)

type DbusBindInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&DbusBindInterfaceSuite{
	iface: &builtin.DbusBindInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "dbus-bind"},
			Name:      "dbus-bind-player",
			Interface: "dbus-bind",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "dbus-bind"},
			Name:      "dbus-bind-client",
			Interface: "dbus-bind",
		},
	},
})

func (s *DbusBindInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dbus-bind")
}

func (s *DbusBindInterfaceSuite) TestUnusedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecuritySecComp,
		interfaces.SecurityDBus, interfaces.SecurityUDev,
		interfaces.SecurityMount}
	for _, system := range systems {
		snippet, err := s.iface.PermanentPlugSnippet(s.plug, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)

		snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)

		snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
	}
}

func (s *DbusBindInterfaceSuite) TestUsedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp}
	for _, system := range systems {
		snippet, err := s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
	}
}

func (s *DbusBindInterfaceSuite) TestUnexpectedSecuritySystems(c *C) {
	snippet, err := s.iface.PermanentPlugSnippet(s.plug, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.PermanentSlotSnippet(s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotSession(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: session
  name: org.dbus-bind-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotSystem(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: system
  name: org.dbus-bind-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotFull(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: system
  name: org.dbus-bind-snap.foo.bar.baz.n0rf_qux
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}
func (s *DbusBindInterfaceSuite) TestSanitizeSlotMissingBus(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  name: org.dbus-bind-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "bus must be set")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotEmptyBus(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: ""
  name: org.dbus-bind-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "bus must be set")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotNonexistentBus(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: nonexistent
  name: org.dbus-bind-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "bus must be one of 'session' or 'system'")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotMissingName(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: session
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "bus name must be set")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotEmptyName(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: session
  name: ""
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "bus name must be set")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotNameTooLong(c *C) {
	long_name := make([]byte, 256)
	for i := range long_name {
		long_name[i] = 'b'
	}
	// make it look otherwise valid (a.bbbb...)
	long_name[0] = 'a'
	long_name[1] = '.'

	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: session
  name: `)
	mockSnapYaml = append(mockSnapYaml, long_name...)
	mockSnapYaml = append(mockSnapYaml, "\n"...)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "bus name is too long \\(must be <= 255\\)")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotNameStartsWithColon(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: session
  name: :dbus-bind-snap.bar
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "invalid bus name: \":dbus-bind-snap.bar\"")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotNameStartsWithNum(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: session
  name: 0dbus-bind-snap.bar
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "invalid bus name: \"0dbus-bind-snap.bar\"")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotNameMissingDot(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: session
  name: dbus-bind-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "invalid bus name: \"dbus-bind-snap\"")
}

func (s *DbusBindInterfaceSuite) TestSanitizeSlotNameMissingElement(c *C) {
	var mockSnapYaml = []byte(`name: dbus-bind-snap
version: 1.0
slots:
 dbus-bind-slot:
  interface: dbus-bind
  bus: session
  name: dbus-bind-snap.
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-bind-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "invalid bus name: \"dbus-bind-snap\\.\"")
}
