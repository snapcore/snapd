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

package kmod_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/testutil"
)

type kmodSuite struct {
	ifacetest.BackendSuite
}

var _ = Suite(&kmodSuite{})

func (s *kmodSuite) SetUpTest(c *C) {
	s.Backend = &kmod.Backend{}
	s.BackendSuite.SetUpTest(c)
}

func (s *kmodSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)
}

func (s *kmodSuite) TestModprobeCall(c *C) {
	cmd := testutil.MockCommand(c, "modprobe", "")
	defer cmd.Restore()

	b, ok := s.Backend.(*kmod.Backend)
	c.Assert(ok, Equals, true)
	b.LoadModules([]string{
		"module1",
		"module2",
	})
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"modprobe", "--syslog", "module1"},
		{"modprobe", "--syslog", "module2"},
	})
}

func (s *kmodSuite) TestNoModprobeCallWhenPreseeding(c *C) {
	cmd := testutil.MockCommand(c, "modprobe", "")
	defer cmd.Restore()

	b := kmod.Backend{}
	opts := &interfaces.SecurityBackendOptions{
		Preseed: true,
	}
	c.Assert(b.Initialize(opts), IsNil)

	b.LoadModules([]string{"module1"})
	c.Assert(cmd.Calls(), HasLen, 0)
}
