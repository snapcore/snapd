// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
)

type snapshotSuite struct{}

var _ = Suite(&snapshotSuite{})

func (s *snapshotSuite) TestReadSnapshotYamlOpenFails(c *C) {
	var returnedError error
	defer snap.MockOsOpen(func(string) (*os.File, error) {
		return nil, returnedError
	})()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}}

	// Try a generic error, this is reported as such
	returnedError = errors.New("Some error")
	_, err := snap.ReadSnapshotYaml(info)
	c.Check(err, ErrorMatches, "Some error")

	// But if the file is not found, that's just a nil error
	returnedError = os.ErrNotExist
	_, err = snap.ReadSnapshotYaml(info)
	c.Check(err, IsNil)
}

func (s *snapshotSuite) TestReadSnapshotYamlFailures(c *C) {
	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}}

	for _, testData := range []struct {
		contents      string
		expectedError string
	}{
		{
			"", "cannot read snapshot manifest: EOF",
		},
		{
			"invalid", "cannot read snapshot manifest: yaml: unmarshal errors:\n.*",
		},
		{
			"exclude:\n  - /home/ubuntu", "snapshot exclude path must start with one of.*",
		},
		{
			"exclude:\n  - $SNAP_COMMON_STUFF", "snapshot exclude path must start with one of.*",
		},
		{
			"exclude:\n  - $SNAP_DATA/../../meh", "snapshot exclude path not clean.*",
		},
	} {
		manifestFile := filepath.Join(c.MkDir(), "snapshots.yaml")
		err := ioutil.WriteFile(manifestFile, []byte(testData.contents), 0644)
		c.Assert(err, IsNil)
		defer snap.MockOsOpen(func(string) (*os.File, error) {
			return os.Open(manifestFile)
		})()

		_, err = snap.ReadSnapshotYaml(info)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("%s", testData.contents))
	}
}

var snapshotYamlHappy = []byte(`exclude:
  - $SNAP_DATA/one
  - $SNAP_COMMON/two?
  - $SNAP_USER_DATA/three*
  - $SNAP_USER_COMMON/four[0-3]`)

func (s *snapshotSuite) TestReadSnapshotYamlHappy(c *C) {
	manifestFile := filepath.Join(c.MkDir(), "snapshots.yaml")
	err := ioutil.WriteFile(manifestFile, []byte(snapshotYamlHappy), 0644)
	c.Assert(err, IsNil)

	defer snap.MockOsOpen(func(path string) (*os.File, error) {
		return os.Open(manifestFile)
	})()

	info := &snap.Info{SideInfo: snap.SideInfo{RealName: "hello-snap", Revision: snap.R(42), SnapID: "hello-id"}}

	opts, err := snap.ReadSnapshotYaml(info)
	c.Check(err, IsNil)
	c.Check(opts.ExcludePaths, DeepEquals, []string{
		"$SNAP_DATA/one",
		"$SNAP_COMMON/two?",
		"$SNAP_USER_DATA/three*",
		"$SNAP_USER_COMMON/four[0-3]",
	})
}
