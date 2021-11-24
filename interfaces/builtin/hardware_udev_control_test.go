// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type HardwareUdevControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&HardwareUdevControlInterfaceSuite{
	iface: builtin.MustInterface("hardware-udev-control"),
})

const hardwareUdevControlConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [hardware-udev-control]
`

const hardwareUdevControlCoreYaml = `name: core
version: 0
type: os
slots:
  hardware-udev-control:
`

func (s *HardwareUdevControlInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, hardwareUdevControlConsumerYaml, nil, "hardware-udev-control")
	s.slot, s.slotInfo = MockConnectedSlot(c, hardwareUdevControlCoreYaml, nil, "hardware-udev-control")
}

func (s *HardwareUdevControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "hardware-udev-control")
}

func (s *HardwareUdevControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *HardwareUdevControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *HardwareUdevControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/run/udev/control rw,")
}

func (s *HardwareUdevControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows control over the hardware udev daemon`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "hardware-udev-control")
}

func (s *HardwareUdevControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *HardwareUdevControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
