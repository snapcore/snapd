// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap_test

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/snap"
)

type infoSuite struct{}

var _ = Suite(&infoSuite{})

func (s *infoSuite) TestSideInfoOverrides(c *C) {
	info := &snap.Info{
		SuggestedName:       "name",
		OriginalSummary:     "summary",
		OriginalDescription: "desc",
	}

	info.SideInfo = snap.SideInfo{
		OfficialName:      "newname",
		EditedSummary:     "fixed summary",
		EditedDescription: "fixed desc",
		Revision:          1,
	}

	c.Check(info.ZName(), Equals, "newname")
	c.Check(info.ZSummary(), Equals, "fixed summary")
	c.Check(info.ZDescription(), Equals, "fixed desc")
	c.Check(info.Revision, Equals, 1)
}
