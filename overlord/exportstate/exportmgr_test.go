// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package exportstate_test

import (
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/exportstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type mgrSuite struct {
	testutil.BaseTest
	o  *overlord.Overlord
	st *state.State
}

var _ = Suite(&mgrSuite{})

func (s *mgrSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.o = overlord.Mock()
	s.st = s.o.State()
}

func (s *mgrSuite) manager(c *C) *exportstate.ExportManager {
	mgr, err := exportstate.Manager(s.st, s.o.TaskRunner())
	c.Assert(err, IsNil)
	return mgr
}

func (s *mgrSuite) TestEnsure(c *C) {
	mgr := s.manager(c)
	err := mgr.Ensure()
	c.Assert(err, IsNil)
}

func (s *mgrSuite) TestStartUpOnClassicWithoutSnaps(c *C) {
	s.AddCleanup(release.MockOnClassic(true))

	// The start-up of the export manager elects the new provider of snapd tools.
	// In absence of other snaps, on classic systems, the host tools are exported.
	mgr := s.manager(c)
	err := mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(exportstate.CurrentSubKeySymlinkPath("snapd"), testutil.SymlinkTargetEquals, "host")
}

func (s *mgrSuite) TestStartUpOnCoreWithoutSnaps(c *C) {
	s.AddCleanup(release.MockOnClassic(false))

	// The start-up of the export manager elects the new provider of snapd tools.
	// In absence of other snaps, on core system nothing is done. Subsequent seeding
	// of snapd will do the right thing.
	mgr := s.manager(c)
	err := mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, "snapd", "current"), testutil.FileAbsent)
}

func (s *mgrSuite) TestStartUpOnClassicWithOnlyCore(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		switch snapName {
		case "core":
			return snaptest.MockInfo(c, "name: core\nversion: 1\ntype: os\n",
				&snap.SideInfo{Revision: snap.Revision{N: 1}}), nil
		case "snapd":
			return nil, &snap.NotInstalledError{}
		default:
			panic("unexpected")
		}
	}))

	// The start-up of the export manager elects the new provider of snapd tools.
	// When core snap is installed, it is used in preference to host tools.
	mgr := s.manager(c)
	err := mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, "snapd", "current"),
		testutil.SymlinkTargetEquals, "core_1")
}

func (s *mgrSuite) TestStartUpOnClassicWithOnlySnapd(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		switch snapName {
		case "core":
			return nil, &snap.NotInstalledError{}
		case "snapd":
			return snaptest.MockInfo(c, "name: snapd\nversion: 1\ntype: snapd\n",
				&snap.SideInfo{Revision: snap.Revision{N: 2}}), nil
		default:
			panic("unexpected")
		}
	}))

	// The start-up of the export manager elects the new provider of snapd tools.
	// When snapd snap is installed, it is used in preference to host tools.
	mgr := s.manager(c)
	err := mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, "snapd", "current"),
		testutil.SymlinkTargetEquals, "2")
}

func (s *mgrSuite) TestStartUpOnClassicWithBothSnapdAndCore(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		switch snapName {
		case "core":
			return snaptest.MockInfo(c, "name: core\nversion: 1\ntype: os\n",
				&snap.SideInfo{Revision: snap.Revision{N: 1}}), nil
		case "snapd":
			return snaptest.MockInfo(c, "name: snapd\nversion: 1\ntype: snapd\n",
				&snap.SideInfo{Revision: snap.Revision{N: 2}}), nil
		default:
			panic("unexpected")
		}
	}))

	// The start-up of the export manager elects the new provider of snapd tools.
	// When both snapd and core snaps are present, snapd is preferred.
	mgr := s.manager(c)
	err := mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, "snapd", "current"),
		testutil.SymlinkTargetEquals, "2")
}

func (s *mgrSuite) TestSnapLinkageChangedToLinked(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		c.Assert(snapName, Equals, "snap-name")
		return snaptest.MockInfo(c, "name: snap-name\nversion: 1\n",
			&snap.SideInfo{Revision: snap.Revision{N: 1}}), nil
	}))
	err := os.MkdirAll(filepath.Join(exportstate.ExportDir, "snap-name"), 0755)
	c.Assert(err, IsNil)

	p := &exportstate.LinkSnapParticipant{}
	err = p.SnapLinkageChanged(s.st, "snap-name")
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, "snap-name", "current"), testutil.SymlinkTargetEquals, "1")
}

func (s *mgrSuite) TestSnapLinkageChangedToUnlinked(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		c.Assert(snapName, Equals, "snap-name")
		return nil, &snap.NotInstalledError{Snap: snapName}
	}))
	err := os.MkdirAll(filepath.Join(exportstate.ExportDir, "snap-name"), 0755)
	c.Assert(err, IsNil)
	err = os.Symlink("1", filepath.Join(exportstate.ExportDir, "snap-name", "current"))
	c.Assert(err, IsNil)

	p := &exportstate.LinkSnapParticipant{}
	err = p.SnapLinkageChanged(s.st, "snap-name")
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, "snap-name", "current"), testutil.FileAbsent)
}
