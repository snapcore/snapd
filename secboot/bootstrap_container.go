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

package secboot

import (
	"fmt"

	"github.com/snapcore/snapd/osutil"
)

type KeyDataWriter interface {
	// TODO: this will typically have a function that takes a key
	// data as input
}

// BootstrappedContainer is an abstraction for an encrypted container
// along with a key that is able to enroll other keys.  This key is
// meant to be an initial key that is removed after all required keys
// are enrolled, by calling RemoveBootstrapKey.
type BootstrappedContainer interface {
	//AddKey adds a key "newKey" to "slotName"
	//If "token", the a KeyDataWriter is returned to write key data to the token of the new key slot
	AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error)
	//RemoveBootstrapKey removes the bootstrap key
	RemoveBootstrapKey() error
}

type mockBootstrappedContainer struct {
	finished bool
}

func (m *mockBootstrappedContainer) AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error) {
	if m.finished {
		return nil, fmt.Errorf("internal error: key resetter was a already finished")
	}

	if token {
		return nil, fmt.Errorf("not implemented")
	} else {
		return nil, nil
	}
}

func (l *mockBootstrappedContainer) RemoveBootstrapKey() error {
	l.finished = true
	return nil
}

func CreateMockBootstrappedContainer() BootstrappedContainer {
	osutil.MustBeTestBinary("CreateMockBootstrappedContainer can be only called from tests")
	return &mockBootstrappedContainer{
		finished: false,
	}
}

func createBootstrappedContainerMockImpl(key DiskUnlockKey, devicePath string) BootstrappedContainer {
	return &mockBootstrappedContainer{
		finished: false,
	}
}

var CreateBootstrappedContainer = createBootstrappedContainerMockImpl

func MockCreateBootstrappedContainer(f func(key DiskUnlockKey, devicePath string) BootstrappedContainer) func() {
	osutil.MustBeTestBinary("MockCreateBootstrappedContainer can be only called from tests")
	old := CreateBootstrappedContainer
	CreateBootstrappedContainer = f
	return func() {
		CreateBootstrappedContainer = old
	}
}
