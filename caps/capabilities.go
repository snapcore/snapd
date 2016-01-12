// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"fmt"
)

// Capability holds information about a capability that a snap may request
// from a snappy system to do its job while running on it.
type Capability struct {
	// Name is a key that identifies the capability. It must be unique within
	// its context, which may be either a snap or a snappy runtime.
	Name string `json:"name"`
	// Label provides an optional title for the capability to help a human tell
	// which physical device this capability is referring to. It might say
	// "Front USB", or "Green Serial Port", for example.
	Label string `json:"label"`
	// Type defines the type of this capability. The capability type defines
	// the behavior allowed and expected from providers and consumers of that
	// capability, and also which information should be exchanged by these
	// parties.
	Type *Type `json:"type"`
	// Attrs are key-value pairs that provide type-specific capability details.
	Attrs map[string]string `json:"attrs,omitempty"`
}

// String representation of a capability.
func (c Capability) String() string {
	return c.Name
}

// SetAttr sets capability attribute to a given value.
// TODO: remove temporary function implementation once attrtypes are merged.
func (c *Capability) SetAttr(name string, value string) error {
	if c.Attrs == nil {
		c.Attrs = make(map[string]string)
	}
	c.Attrs[name] = value
	return nil
}

// GetAttr gets capability attribute with a given name.
// TODO: remove temporary function implementation once attrtypes are merged.
func (c *Capability) GetAttr(name string) (interface{}, error) {
	if value, ok := c.Attrs[name]; ok {
		return value, nil
	}
	return nil, fmt.Errorf("%s is not set", name)
}
