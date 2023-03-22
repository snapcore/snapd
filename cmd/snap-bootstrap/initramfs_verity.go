// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package main

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap/integrity"
)

func getAndValidateVerityMountOptions(snapName string, snapRev asserts.SnapRevision) (*systemdMountOptions, error) {
	mountOptions := &systemdMountOptions{
		ReadOnly: true,
		Private:  true,
	}

	integrityData, err := integrity.FindIntegrityData(snapName)
	if err != nil {
		return nil, err
	}

	if integrityData == nil {
		return nil, nil
	}

	if err := integrityData.Validate(snapRev); err != nil {
		return nil, err
	}

	mountOptions.VerityHashDevice = integrityData.SourceFilePath
	mountOptions.VerityRootHash = integrityData.Header.DmVerityBlock.RootHash
	mountOptions.VerityHashOffset = integrityData.Offset

	return mountOptions, nil
}
