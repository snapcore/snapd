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

import (
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
)

const physicalMemoryObserveSummary = `allows read access to all physical memory`

const physicalMemoryObserveBaseDeclarationSlots = `
  physical-memory-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const physicalMemoryObserveConnectedPlugAppArmor = `
# Description: With kernels with STRICT_DEVMEM=n, read-only access to all physical
# memory. With STRICT_DEVMEM=y, allow reading /dev/mem for read-only
# access to architecture-specific subset of the physical address (eg, PCI,
# space, BIOS code and data regions on x86, etc).
/dev/mem r,
`

// The type for physical-memory-observe interface
type physicalMemoryObserveInterface struct{}

// Getter for the name of the physical-memory-observe interface
func (iface *physicalMemoryObserveInterface) Name() string {
	return "physical-memory-observe"
}

func (iface *physicalMemoryObserveInterface) String() string {
	return iface.Name()
}

func (iface *physicalMemoryObserveInterface) MetaData() interfaces.MetaData {
	return interfaces.MetaData{
		Summary:              physicalMemoryObserveSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: physicalMemoryObserveBaseDeclarationSlots,
	}
}

// Check validity of the defined slot
func (iface *physicalMemoryObserveInterface) SanitizeSlot(slot *interfaces.Slot) error {
	ensureSlotIfaceMatch(iface, slot)
	return sanitizeSlotReservedForOS(iface, slot)
}

// Checks and possibly modifies a plug
func (iface *physicalMemoryObserveInterface) SanitizePlug(plug *interfaces.Plug) error {
	ensurePlugIfaceMatch(iface, plug)
	return nil
}

func (iface *physicalMemoryObserveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(physicalMemoryObserveConnectedPlugAppArmor)
	return nil
}

func (iface *physicalMemoryObserveInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	const udevRule = `KERNEL=="mem", TAG+="%s"`
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(fmt.Sprintf(udevRule, tag))
	}
	return nil
}

func (iface *physicalMemoryObserveInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&physicalMemoryObserveInterface{})
}
