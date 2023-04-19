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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/logger"
)

// copied from seed/helpers.go
var ErrNoAssertions = errors.New("no seed assertions")

func NewMemDB(batch *asserts.Batch) (*asserts.Database, error) {
	var trusted = sysdb.Trusted()

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   trusted,
	})
	if err != nil {
		return nil, err
	}

	// set up the database to check for key expiry only assuming
	// earliestTime (if not zero)
	earliestTime := timeNow()
	db.SetEarliestTime(earliestTime)

	onlySnapRevisionsAndDeclarations := func(a asserts.Assertion) {
		// we consider only snap-revision and snap-declaration
		// assertions here as they must be store-signed, see
		// checkConsistency for each type
		// other assertions might be signed not by the store
		// nor the brand so they might be provided by an
		// attacker, signed using a registered key but
		// containing unreliable time
		var tstamp time.Time
		switch a.Type() {
		default:
			// not one of the store-signed assertion types
			return
		case asserts.SnapRevisionType:
			sr := a.(*asserts.SnapRevision)
			tstamp = sr.Timestamp()
		case asserts.SnapDeclarationType:
			sd := a.(*asserts.SnapDeclaration)
			tstamp = sd.Timestamp()
		}
		if tstamp.After(earliestTime) {
			earliestTime = tstamp
		}
	}

	commitTo := func(b *asserts.Batch) error {
		return b.CommitToAndObserve(db, onlySnapRevisionsAndDeclarations, nil)
	}

	if err := commitTo(batch); err != nil {
		return nil, err
	}

	return db, nil
}

func LoadAssertions(assertsDir string) (*asserts.Batch, error) {
	// collect assertions that are not the model
	var declRefs []*asserts.Ref
	var revRefs []*asserts.Ref

	checkAssertion := func(ref *asserts.Ref) error {
		switch ref.Type {
		case asserts.ModelType:
			return fmt.Errorf("system cannot have any model assertion but the one in the system model assertion file")
		case asserts.SnapDeclarationType:
			declRefs = append(declRefs, ref)
		case asserts.SnapRevisionType:
			revRefs = append(revRefs, ref)
		}
		return nil
	}

	batch, err := loadAssertions(assertsDir, checkAssertion)
	if err != nil {
		return nil, err
	}

	return batch, nil
}

func loadAssertions(assertsDir string, loadedFunc func(*asserts.Ref) error) (*asserts.Batch, error) {
	logger.Debugf("loading assertions from %s", assertsDir)
	dc, err := ioutil.ReadDir(assertsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoAssertions
		}
		return nil, fmt.Errorf("cannot read assertions dir: %s", err)
	}

	batch := asserts.NewBatch(nil)
	for _, fi := range dc {
		fn := filepath.Join(assertsDir, fi.Name())
		refs, err := readAsserts(batch, fn)
		if err != nil {
			return nil, fmt.Errorf("cannot read assertions: %s", err)
		}
		if loadedFunc != nil {
			for _, ref := range refs {
				if err := loadedFunc(ref); err != nil {
					return nil, err
				}
			}
		}
	}

	return batch, nil
}

func readAsserts(batch *asserts.Batch, fn string) ([]*asserts.Ref, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return batch.AddStream(f)
}
