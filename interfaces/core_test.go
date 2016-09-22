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

package interfaces_test

import (
	"testing"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/interfaces"
)

func Test(t *testing.T) {
	TestingT(t)
}

type CoreSuite struct{}

var _ = Suite(&CoreSuite{})

func (s *CoreSuite) TestValidateName(c *C) {
	validNames := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
	}
	for _, name := range validNames {
		err := ValidateName(name)
		c.Assert(err, IsNil)
	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// dashes alone are not a name
		"-", "--",
		// double dashes in a name are not allowed
		"a--a",
		// name should not end with a dash
		"a-",
		// name cannot have any spaces in it
		"a ", " a", "a a",
		// a number alone is not a name
		"0", "123",
		// identifier must be plain ASCII
		"日本語", "한글", "ру́сский язы́к",
	}
	for _, name := range invalidNames {
		err := ValidateName(name)
		c.Assert(err, ErrorMatches, `invalid interface name: ".*"`)
	}
}

func (s *CoreSuite) TestValidateDBusBusName(c *C) {
	// https://dbus.freedesktop.org/doc/dbus-specification.html#message-protocol-names
	validNames := []string{
		"a.b", "a.b.c", "a.b1", "a.b1.c2d",
		"a_a.b", "a_a.b_b.c_c", "a_a.b_b1", "a_a.b_b1.c_c2d_d",
		"a-a.b", "a-a.b-b.c-c", "a-a.b-b1", "a-a.b-b1.c-c2d-d",
	}
	for _, name := range validNames {
		err := ValidateDBusBusName(name)
		c.Assert(err, IsNil)
	}

	invalidNames := []string{
		// must not start with ':'
		":a.b",
		// only from [A-Z][a-z][0-9]_-
		"@.a",
		// elements may not start with number
		"0.a",
		"a.0a",
		// must have more than one element
		"a",
		"a_a",
		"a-a",
		// element must not begin with '.'
		".a",
		// each element must be at least 1 character
		"a.",
		"a..b",
		".a.b",
		"a.b.",
	}
	for _, name := range invalidNames {
		err := ValidateDBusBusName(name)
		c.Assert(err, ErrorMatches, `invalid bus name: ".*"`)
	}

	// must not be empty
	err := ValidateDBusBusName("")
	c.Assert(err, ErrorMatches, `bus name must be set`)

	// must not exceed maximum length
	longName := make([]byte, 256)
	for i := range longName {
		longName[i] = 'b'
	}
	// make it look otherwise valid (a.bbbb...)
	longName[0] = 'a'
	longName[1] = '.'
	err = ValidateDBusBusName(string(longName))
	c.Assert(err, ErrorMatches, `bus name is too long \(must be <= 255\)`)
}
