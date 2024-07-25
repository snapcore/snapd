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

package registrystate

import "github.com/snapcore/snapd/overlord/state"

type RegistryManager struct {
	state *state.State
}

func Manager(st *state.State, runner *state.TaskRunner) *RegistryManager {
	m := &RegistryManager{
		state: st,
	}

	// no undo since if we commit there's no rolling back
	runner.AddHandler("commit-transaction", m.doCommitTransaction, nil)

	return m
}

func (m *RegistryManager) Ensure() error { return nil }
