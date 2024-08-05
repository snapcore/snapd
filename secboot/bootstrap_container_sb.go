// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

	sb "github.com/snapcore/secboot"
)

type bootstrappedContainer struct {
	oldContainerKeySlot string
	devicePath          string
	key                 DiskUnlockKey
	finished            bool
}

func (bc *bootstrappedContainer) AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error) {
	if slotName == "" {
		slotName = "default"
	}
	if err := sb.AddLUKS2ContainerUnlockKey(bc.devicePath, slotName, sb.DiskUnlockKey(bc.key), sb.DiskUnlockKey(newKey)); err != nil {
		return nil, err
	}
	if !token {
		return nil, nil
	}
	return nil, fmt.Errorf("not implemented")
}

func (bc *bootstrappedContainer) RemoveBootstrapKey() error {
	if bc.finished {
		return nil
	}
	bc.finished = true
	return sb.DeleteLUKS2ContainerKey(bc.devicePath, bc.oldContainerKeySlot)
}

func createBootstrappedContainerImpl(key DiskUnlockKey, devicePath string) BootstrappedContainer {
	return &bootstrappedContainer{
		oldContainerKeySlot: "bootstrap-key",
		devicePath:          devicePath,
		key:                 key,
		finished:            false,
	}
}

func init() {
	CreateBootstrappedContainer = createBootstrappedContainerImpl
}
