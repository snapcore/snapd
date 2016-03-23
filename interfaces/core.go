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

package interfaces

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
)

// Plug represents the potential of a given snap to connect to a slot.
type Plug struct {
	*snap.PlugInfo
	Connections []SlotRef `json:"connections,omitempty"`
}

// PlugRef is a reference to a plug.
type PlugRef struct {
	Snap string `json:"snap"`
	Name string `json:"plug"`
}

// Slot represents a capacity offered by a snap.
type Slot struct {
	*snap.SlotInfo
	Connections []PlugRef `json:"connections,omitempty"`
}

// SlotRef is a reference to a slot.
type SlotRef struct {
	Snap string `json:"snap"`
	Name string `json:"slot"`
}

// Interfaces holds information about a list of plugs and slots, and their connections.
type Interfaces struct {
	Plugs []*Plug `json:"plugs"`
	Slots []*Slot `json:"slots"`
}

// Interface describes a group of interchangeable capabilities with common features.
// Interfaces act as a contract between system builders, application developers
// and end users.
type Interface interface {
	// Unique and public name of this interface.
	Name() string

	// SanitizePlug checks if a plug is correct, altering if necessary.
	SanitizePlug(plug *Plug) error

	// SanitizeSlot checks if a slot is correct, altering if necessary.
	SanitizeSlot(slot *Slot) error

	// PermanentPlugSnippet returns the snippet of text for the given security
	// system that is used during the whole lifetime of affected applications,
	// whether the plug is connected or not.
	//
	// Permanent security snippet can be used to grant permissions to a snap that
	// has a plug of a given interface even before the plug is connected to a
	// slot.
	//
	// An empty snippet is returned when there are no additional permissions
	// that are required to implement this interface. ErrUnknownSecurity error
	// is returned when the plug cannot deal with the requested security
	// system.
	PermanentPlugSnippet(plug *Plug, securitySystem SecuritySystem) ([]byte, error)

	// ConnectedPlugSnippet returns the snippet of text for the given security
	// system that is used by affected application, while a specific connection
	// between a plug and a slot exists.
	//
	// Connection-specific security snippet can be used to grant permission to
	// a snap that has a plug of a given interface connected to a slot in
	// another snap.
	//
	// The snippet should be specific to both the plug and the slot. If the
	// slot is not necessary then consider using PermanentPlugSnippet()
	// instead.
	//
	// An empty snippet is returned when there are no additional permissions
	// that are required to implement this interface. ErrUnknownSecurity error
	// is returned when the plug cannot deal with the requested security
	// system.
	ConnectedPlugSnippet(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error)

	// PermanentSlotSnippet returns the snippet of text for the given security
	// system that is used during the whole lifetime of affected applications,
	// whether the slot is connected or not.
	//
	// Permanent security snippet can be used to grant permissions to a snap that
	// has a slot of a given interface even before the first connection to that
	// slot is made.
	//
	// An empty snippet is returned when there are no additional permissions
	// that are required to implement this interface. ErrUnknownSecurity error
	// is returned when the plug cannot deal with the requested security
	// system.
	PermanentSlotSnippet(slot *Slot, securitySystem SecuritySystem) ([]byte, error)

	// ConnectedSlotSnippet returns the snippet of text for the given security
	// system that is used by affected application, while a specific connection
	// between a plug and a slot exists.
	//
	// Connection-specific security snippet can be used to grant permission to
	// a snap that has a slot of a given interface connected to a plug in
	// another snap.
	//
	// The snippet should be specific to both the plug and the slot, if the
	// plug is not necessary then consider using PermanentSlotSnippet()
	// instead.
	//
	// An empty snippet is returned when there are no additional permissions
	// that are required to implement this interface. ErrUnknownSecurity error
	// is returned when the plug cannot deal with the requested security
	// system.
	ConnectedSlotSnippet(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error)
}

// SecuritySystem is a name of a security system.
type SecuritySystem string

const (
	// SecurityAppArmor identifies the apparmor security system.
	SecurityAppArmor SecuritySystem = "apparmor"
	// SecuritySecComp identifies the seccomp security system.
	SecuritySecComp SecuritySystem = "seccomp"
	// SecurityDBus identifies the DBus security system.
	SecurityDBus SecuritySystem = "dbus"
	// SecurityUDev identifies the UDev security system.
	SecurityUDev SecuritySystem = "udev"
)

// SecurityConfigurator abstracts knowledge to configure a security system.
type SecurityConfigurator interface {
	// ConfigureSnapSecurity creates and loads security artefacts specific to a
	// given snap. The snap can be in developer mode to make security
	// violations non-fatal to the offending application process.
	//
	// This method should be called after changing plug, slots, connections
	// between them or application present in the snap.
	ConfigureSnapSecurity(repo *Repository, snapInfo *snap.Info, developerMode bool) error

	// DeconfigureSnapSecurity removes security artefacts of a given snap.
	//
	// This method should be called after removing a snap.
	DeconfigureSnapSecurity(snapInfo *snap.Info) error

	// Finalize performs optional post-processing steps after
	// configuring/deconfiguring any number of snaps.
	//
	// This method should be called unconditionally to finalize changes made
	// earlier. Some changes are buffered so that the cost is only incurred
	// once.
	Finalize() error
}

// SecurityBackend is the common interface for low-level SecurityConfigurator API
type SecurityBackend interface {
	SecurityConfigurator
	SecuritySystem() SecuritySystem
	DirStateForInstalledSnap(snapInfo *snap.Info, developerMode bool, snippets map[string][][]byte) (dir, glob string, content map[string]*osutil.FileState, err error)
	DirStateForRemovedSnap(snapInfo *snap.Info) (dir, glob string)
}

var (
	// ErrUnknownSecurity is reported when a interface is unable to deal with a given security system.
	ErrUnknownSecurity = errors.New("unknown security system")
)

// Regular expression describing correct identifiers.
var validName = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidateName checks if a string can be used as a plug or slot name.
func ValidateName(name string) error {
	valid := validName.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid interface name: %q", name)
	}
	return nil
}
