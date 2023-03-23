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
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/integrity"
)

func generateVerityMountOptions(mountOptions *systemdMountOptions, snapInfo snap.PlaceInfo, snapName string, assertionDB *asserts.Database) error {
	snapRevNum := snapInfo.SnapRevision().String()

	// Find snap-id from snap declaration
	as, err := assertionDB.FindMany(asserts.SnapDeclarationType, map[string]string{
		"snap-name": snapInfo.SnapName(),
	})
	if err != nil {
		return err
	}

	if len(as) > 1 {
		/// XXX: shouldn't be reachable
		return fmt.Errorf("GetMountOptionsForSnap: multiple snap-declaration assertions found for snap-name: %s", snapInfo.SnapName())
	}

	snapDecl, ok := as[0].(*asserts.SnapDeclaration)
	if !ok {
		return fmt.Errorf("GetMountOptionsForSnap: type assertion failed for snap declaration")
	}

	snapID := snapDecl.SnapID()

	// Searching the database with a snap-id and snap-revision.
	as, err = assertionDB.FindMany(asserts.SnapRevisionType, map[string]string{
		"snap-id":       snapID,
		"snap-revision": snapRevNum,
	})
	if err != nil {
		return err
	}

	if len(as) > 1 {
		/// XXX: shouldn't be reachable
		return fmt.Errorf("GetMountOptionsForSnap: multiple snap-revisions for for snap-id: %s and snap-revision: %s", snapID, snapRevNum)
	}

	snapRev, ok := as[0].(*asserts.SnapRevision)
	if !ok {
		return fmt.Errorf("GetMountOptionsForSnap: type assertion failed for snap revision")
	}

	// Check if revision contains integrity data
	assertionIntegrity, _ := snapRev.Integrity().(map[string]string)
	_, ok = assertionIntegrity["sha3-384"]
	if !ok {
		return nil
	}

	integrityData, err := integrity.FindIntegrityData(snapName)
	if err != nil {
		return err
	}

	if err := integrityData.Validate(*snapRev); err != nil {
		return err
	}

	mountOptions.VerityHashDevice = integrityData.SourceFilePath
	mountOptions.VerityRootHash = integrityData.Header.DmVerityBlock.RootHash
	mountOptions.VerityHashOffset = integrityData.Offset

	return nil
}
