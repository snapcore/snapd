// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

import (
	"encoding/json"
	"fmt"
)

// Type describes a group of interchangeable capabilities with common features.
// Types are managed centrally and act as a contract between system builders,
// application developers and end users.
type Type struct {
	// Name is a key that identifies the capability type. It must be unique
	// within the whole OS. The name forms a part of the stable system API.
	Name string
	// RequiredAttrs contains names of attributes that are required by
	// capability of this type.
	RequiredAttrs []string
	// SecuritySystems contains the associated security systems that enable actual
	// access to system resources needed by this capability.
	SecuritySystems []securitySystem
}

var (
	// BoolFileType is a built-in capability type for files that follow a
	// simple boolean protocol. The file can be read, which yields ASCII '0'
	// (zero) or ASCII '1' (one). The same can be done for writing.
	//
	// This capability type can be used to describe many boolean flags exposed
	// in sysfs, including certain hardware like exported GPIO pins.
	BoolFileType = &Type{
		Name:          "bool-file",
		RequiredAttrs: []string{"path"},
	}
)

var builtInTypes = [...]*Type{
	BoolFileType,
}

// String returns a string representation for the capability type.
func (t *Type) String() string {
	return t.Name
}

// Validate whether a capability is correct according to the given type.
func (t *Type) Validate(c *Capability) error {
	if t != c.Type {
		return fmt.Errorf("capability is not of type %q", t)
	}
	// Check that all required attributes are present
	for _, attr := range t.RequiredAttrs {
		if _, ok := c.Attrs[attr]; !ok {
			return fmt.Errorf("capabilities of type %q must provide a %q attribute", t, attr)
		}
	}
	return nil
}

// MarshalJSON encodes a Type object as the name of the type.
func (t *Type) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Name)
}

// UnmarshalJSON decodes the name of a Type object.
// NOTE: In the future, when more properties are added, those properties will
// not be decoded and will be left over as empty values.
func (t *Type) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &t.Name)
}

// GrantPermissions makes it possible for the package `snapName` to use the
// resource described by the capability `cap`.
func (t *Type) GrantPermissions(snapName string, cap *Capability) error {
	// Ensure that the capability is valid
	if err := t.Validate(cap); err != nil {
		return err
	}
	// Grant all permissions required.
	for i := range cap.Type.SecuritySystems {
		sec := cap.Type.SecuritySystems[i]
		if err := sec.GrantPermissions(snapName, cap); err != nil {
			// If we already granted something, try to revoke that permission instead
			// NOTE: "i - 1" because grant that fails should fail atomically
			for j := i - 1; j >= 0; j-- {
				sec := cap.Type.SecuritySystems[j]
				if err := sec.RevokePermissions(snapName, cap); err != nil {
					// XXX: Should we do something other than panic here?
					panic(fmt.Sprintf("unable to revoke partially granted permissions: %q", err))
				}
			}
			return err
		}
	}
	return nil
}

// RevokePermissions undoes the effects of GrantPermissions.
func (t *Type) RevokePermissions(snapName string, cap *Capability) error {
	// Ensure that the capability is valid
	if err := t.Validate(cap); err != nil {
		return err
	}
	// Revoke all permissions required
	for i, sec := range cap.Type.SecuritySystems {
		if err := sec.RevokePermissions(snapName, cap); err != nil {
			// If we already revoked something, try to grant that same permission again
			for j := i - 1; j >= 0; j-- {
				sec := cap.Type.SecuritySystems[j]
				if err := sec.GrantPermissions(snapName, cap); err != nil {
					// XXX: Should we do something other than panic here?
					panic(fmt.Sprintf("unable to grant partially revoked permissions: %q", err))
				}
			}
			return err
		}
	}
	return nil
}
