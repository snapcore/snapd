// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package snapstate_test

import (
	"errors"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

func expectedComponentRemoveTasks(opts int) []string {
	var removeTasks []string
	removeTasks = append(removeTasks, "unlink-current-component")
	if opts&compTypeIsKernMods != 0 {
		removeTasks = append(removeTasks, "clear-kernel-modules-components")
	}
	removeTasks = append(removeTasks, "discard-component")
	return removeTasks
}

func verifyComponentRemoveTasks(c *C, opts int, ts *state.TaskSet) {
	kinds := taskKinds(ts.Tasks())

	expected := expectedComponentRemoveTasks(opts)
	c.Assert(kinds, DeepEquals, expected)

	checkSetupTasks(c, ts)
}

func (s *snapmgrTestSuite) TestRemoveComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneComponent(s.state, snapName, snapRev, compName, compRev)

	tss, err := snapstate.RemoveComponents(s.state, snapName, []string{compName})
	c.Assert(err, IsNil)

	c.Assert(len(tss), Equals, 1)
	totalTasks := 0
	for _, ts := range tss {
		verifyComponentRemoveTasks(c, 0, ts)
		totalTasks += len(ts.Tasks())
	}

	c.Assert(s.state.TaskCount(), Equals, totalTasks)
}

func (s *snapmgrTestSuite) TestRemoveComponents(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "other-comp"
	snapRev := snap.R(1)

	s.state.Lock()
	defer s.state.Unlock()

	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(1))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName2), snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2})

	tss, err := snapstate.RemoveComponents(s.state, snapName, []string{compName, compName2})
	c.Assert(err, IsNil)

	c.Assert(len(tss), Equals, 2)
	totalTasks := 0
	for _, ts := range tss {
		verifyComponentRemoveTasks(c, compTypeIsKernMods, ts)
		totalTasks += len(ts.Tasks())
	}

	c.Assert(s.state.TaskCount(), Equals, totalTasks)
}

func (s *snapmgrTestSuite) TestRemoveComponentNoSnap(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"

	s.state.Lock()
	defer s.state.Unlock()

	tss, err := snapstate.RemoveComponents(s.state, snapName, []string{compName})
	c.Assert(tss, IsNil)
	var notInstalledError *snap.NotInstalledError
	c.Assert(errors.As(err, &notInstalledError), Equals, true)
	c.Assert(notInstalledError, DeepEquals, &snap.NotInstalledError{
		Snap: snapName,
		Rev:  snap.R(0),
	})
}

func (s *snapmgrTestSuite) TestRemoveNonPresentComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	tss, err := snapstate.RemoveComponents(s.state, snapName, []string{compName})
	c.Assert(tss, IsNil)
	var notInstalledError *snap.ComponentNotInstalledError
	c.Assert(errors.As(err, &notInstalledError), Equals, true)
	c.Assert(notInstalledError, DeepEquals, &snap.ComponentNotInstalledError{
		NotInstalledError: snap.NotInstalledError{
			Snap: snapName,
			Rev:  snap.R(1),
		},
		Component: compName,
		CompRev:   snap.R(0),
	})
}

func (s *snapmgrTestSuite) TestRemoveComponentPathRun(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "other-comp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, _ := createTestComponent(c, snapName, compName, info)
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(1))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName2), snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2})

	tss, err := snapstate.RemoveComponents(s.state, snapName, []string{compName})
	c.Assert(err, IsNil)

	c.Assert(len(tss), Equals, 1)

	chg := s.state.NewChange("remove component", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	for _, ts := range tss {
		verifyComponentRemoveTasks(c, compTypeIsKernMods, ts)
	}
}
