// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package builtin

import (
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
)

const bluezSummary = `allows operating as the bluez service`

const bluezBaseDeclarationSlots = `
  bluez:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const bluezPermanentSlotAppArmor = `
# Description: Allow operating as the bluez service. This gives privileged
# access to the system.

  network bluetooth,

  capability net_admin,
  capability net_bind_service,

  # libudev
  network netlink raw,

  # File accesses
  /sys/bus/usb/drivers/btusb/     r,
  /sys/bus/usb/drivers/btusb/**   r,
  /sys/class/bluetooth/           r,
  /sys/devices/**/bluetooth/      rw,
  /sys/devices/**/bluetooth/**    rw,
  /sys/devices/**/id/chassis_type r,

  # TODO: use snappy hardware assignment for this once LP: #1498917 is fixed
  /dev/rfkill rw,

  # DBus accesses
  #include <abstractions/dbus-strict>
  dbus (send)
     bus=system
     path=/org/freedesktop/DBus
     interface=org.freedesktop.DBus
     member={Request,Release}Name
     peer=(name=org.freedesktop.DBus, label=unconfined),

  dbus (send)
    bus=system
    path=/org/freedesktop/*
    interface=org.freedesktop.DBus.Properties
    peer=(label=unconfined),

  # Allow binding the service to the requested connection name
  dbus (bind)
      bus=system
      name="org.bluez",

  # Allow binding the service to the requested connection name
  dbus (bind)
      bus=system
      name="org.bluez.obex",

  # Allow traffic to/from our path and interface with any method for unconfined
  # cliens to talk to our bluez services.
  dbus (receive, send)
      bus=system
      path=/org/bluez{,/**}
      interface=org.bluez.*
      peer=(label=unconfined),
  dbus (receive, send)
      bus=system
      path=/org/bluez{,/**}
      interface=org.freedesktop.DBus.*
      peer=(label=unconfined),

  # Allow traffic to/from org.freedesktop.DBus for bluez service. This rule is
  # not snap-specific and grants privileged access to the org.freedesktop.DBus
  # on the system bus.
  dbus (receive, send)
      bus=system
      path=/
      interface=org.freedesktop.DBus.*
      peer=(label=unconfined),

  # Allow access to hostname system service
  dbus (receive, send)
      bus=system
      path=/org/freedesktop/hostname1
      interface=org.freedesktop.DBus.Properties
      peer=(label=unconfined),
`

const bluezConnectedSlotAppArmor = `
# Allow connected clients to interact with the service

# Allow all access to bluez service
dbus (receive, send)
    bus=system
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const bluezConnectedPlugAppArmor = `
# Description: Allow using bluez service. This gives privileged access to the
# bluez service.

#include <abstractions/dbus-strict>

# Allow all access to bluez service
dbus (receive, send)
    bus=system
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    peer=(name=org.bluez, label=unconfined),

dbus (send)
    bus=system
    peer=(name=org.bluez.obex, label=unconfined),

dbus (receive)
    bus=system
    path=/
    interface=org.freedesktop.DBus.ObjectManager
    peer=(label=unconfined),

dbus (receive)
    bus=system
    path=/org/bluez{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),
`

const bluezPermanentSlotSecComp = `
# Description: Allow operating as the bluez service. This gives privileged
# access to the system.
accept
accept4
bind
listen
shutdown
# libudev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const bluezPermanentSlotDBus = `
<policy user="root">
    <allow own="org.bluez"/>
    <allow own="org.bluez.obex"/>
    <allow send_destination="org.bluez"/>
    <allow send_destination="org.bluez.obex"/>
    <allow send_interface="org.bluez.Agent1"/>
    <allow send_interface="org.bluez.ThermometerWatcher1"/>
    <allow send_interface="org.bluez.AlertAgent1"/>
    <allow send_interface="org.bluez.Profile1"/>
    <allow send_interface="org.bluez.HeartRateWatcher1"/>
    <allow send_interface="org.bluez.CyclingSpeedWatcher1"/>
    <allow send_interface="org.bluez.GattCharacteristic1"/>
    <allow send_interface="org.bluez.GattDescriptor1"/>
    <allow send_interface="org.freedesktop.DBus.ObjectManager"/>
    <allow send_interface="org.freedesktop.DBus.Properties"/>
</policy>
<policy context="default">
    <deny send_destination="org.bluez"/>
</policy>
`

const bluezConnectedPlugUDev = `KERNEL=="rfkill", TAG+="###CONNECTED_SECURITY_TAGS###"`

type bluezInterface struct{}

func (iface *bluezInterface) Name() string {
	return "bluez"
}

func (iface *bluezInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              bluezSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: bluezBaseDeclarationSlots,
	}
}

func (iface *bluezInterface) DBusPermanentSlot(spec *dbus.Specification, slot *interfaces.Slot) error {
	if !release.OnClassic {
		spec.AddSnippet(bluezPermanentSlotDBus)
	}
	return nil
}

func (iface *bluezInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if release.OnClassic {
		new = "unconfined"
	} else {
		new = slotAppLabelExpr(slot)
	}
	snippet := strings.Replace(bluezConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *bluezInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if !release.OnClassic {
		old := "###PLUG_SECURITY_TAGS###"
		new := plugAppLabelExpr(plug)
		snippet := strings.Replace(bluezConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *bluezInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###CONNECTED_SECURITY_TAGS###"
	for appName := range plug.Apps() {
		tag := udevSnapSecurityName(plug.Snap().Name(), appName)
		snippet := strings.Replace(bluezConnectedPlugUDev, old, tag, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *bluezInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	if !release.OnClassic {
		spec.AddSnippet(bluezPermanentSlotAppArmor)
	}
	return nil
}

func (iface *bluezInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *interfaces.Slot) error {
	if !release.OnClassic {
		spec.AddSnippet(bluezPermanentSlotSecComp)
	}
	return nil
}

func (iface *bluezInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&bluezInterface{})
}
