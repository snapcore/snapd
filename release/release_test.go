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

package release_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type ReleaseTestSuite struct {
}

var _ = Suite(&ReleaseTestSuite{})

func (s *ReleaseTestSuite) TestSetup(c *C) {
	c.Check(release.Series, Equals, "16")
}

func makeMockLSBRelease(c *C) string {
	// FIXME: use AddCleanup here once available so that we
	//        can do release.SetLSBReleasePath() here directly
	mockLSBRelease := filepath.Join(c.MkDir(), "mock-lsb-release")
	s := `
NAME="Ubuntu"
VERSION="18.09 (Awesome Artichoke)"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="I'm not real!"
VERSION_ID="18.09"
HOME_URL="http://www.ubuntu.com/"
SUPPORT_URL="http://help.ubuntu.com/"
BUG_REPORT_URL="http://bugs.launchpad.net/ubuntu/"
UBUNTU_CODENAME=awesome
`
	err := ioutil.WriteFile(mockLSBRelease, []byte(s), 0644)
	c.Assert(err, IsNil)

	return mockLSBRelease
}

func (s *ReleaseTestSuite) TestReadLSB(c *C) {
	reset := release.MockLSBReleasePath(makeMockLSBRelease(c))
	defer reset()

	lsb, err := release.ReadLSB()
	c.Assert(err, IsNil)
	c.Assert(lsb.ID, Equals, "ubuntu")
	c.Assert(lsb.Name, Equals, "Ubuntu")
	c.Assert(lsb.Release, Equals, "18.09")
	c.Assert(lsb.Codename, Equals, "awesome")
}

func (s *ReleaseTestSuite) TestReadLSBNotFound(c *C) {
	reset := release.MockLSBReleasePath("not-there")
	defer reset()

	_, err := release.ReadLSB()
	c.Assert(err, ErrorMatches, "cannot read os-release:.*")
}

func (s *ReleaseTestSuite) TestOnClassic(c *C) {
	reset := release.MockOnClassic(true)
	defer reset()
	c.Assert(release.OnClassic, Equals, true)

	reset = release.MockOnClassic(false)
	defer reset()
	c.Assert(release.OnClassic, Equals, false)
}

func (s *ReleaseTestSuite) TestLSBInfo(c *C) {
	reset := release.MockLSB(&release.LSB{
		ID: "distro-id",
	})
	defer reset()
	c.Assert(release.LSBInfo.ID, Equals, "distro-id")
}
