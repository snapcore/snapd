// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

// TODO this should be it's own package, but depends on splitting out
// snap.yaml's

package snappy

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/legacygadget"
)

type GadgetSuite struct {
}

var _ = Suite(&GadgetSuite{})

func (s *GadgetSuite) SetUpTest(c *C) {
	getGadget = func() (*snap.Info, error) {
		legacy := &snap.LegacyYaml{
			Gadget: legacygadget.Gadget{
				Store: legacygadget.Store{
					ID: "ninjablocks"},
			},
		}
		return &snap.Info{
			Legacy: legacy,
		}, nil
	}
}

func (s *GadgetSuite) TearDownTest(c *C) {
	getGadget = getGadgetImpl
}

func (s *GadgetSuite) TestStoreID(c *C) {
	c.Assert(StoreID(), Equals, "ninjablocks")
}
