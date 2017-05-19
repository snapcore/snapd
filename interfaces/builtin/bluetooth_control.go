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

package builtin

const bluetoothControlConnectedPlugAppArmor = `
# Description: Allow managing the kernel side Bluetooth stack. Reserved
# because this gives privileged access to the system.

  network bluetooth,
  # For crypto functionality the kernel offers
  network alg,

  capability net_admin,

  # File accesses
  /sys/bus/usb/drivers/btusb/     r,
  /sys/bus/usb/drivers/btusb/**   r,
  /sys/class/bluetooth/           r,
  /sys/devices/**/bluetooth/      rw,
  /sys/devices/**/bluetooth/**    rw,

  # Requires CONFIG_BT_VHCI to be loaded
  /dev/vhci                       rw,
`

const bluetoothControlConnectedPlugSecComp = `
# Description: Allow managing the kernel side Bluetooth stack. Reserved
# because this gives privileged access to the system.
bind
`

func init() {
	registerIface(&commonInterface{
		name:                  "bluetooth-control",
		implicitOnCore:        true,
		implicitOnClassic:     true,
		connectedPlugAppArmor: bluetoothControlConnectedPlugAppArmor,
		connectedPlugSecComp:  bluetoothControlConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
