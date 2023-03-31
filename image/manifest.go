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

package image

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/snap"
)

type SeedManifestEntry interface {
	// String should return a formatted string of the manifest
	// entry as it should be written.
	String() string
	// Unique should return an identifier that can be used to track
	// allowed/used entries.
	Unique() string
	// Check will be invoked to verify an used entry against a previously
	// allowed one.
	Check(against SeedManifestEntry) error
}

// SeedManifestSnapRevision represents a snap revision as noted
// in the seed manifest.
type SeedManifestSnapRevision struct {
	SnapName string
	Revision snap.Revision
}

func (s *SeedManifestSnapRevision) Check(against SeedManifestEntry) error {
	sr, ok := against.(*SeedManifestSnapRevision)
	if !ok {
		return fmt.Errorf("internal error: expected SeedManifestEntry to be SeedManifestSnapRevision")
	}

	if s.Revision != sr.Revision {
		return fmt.Errorf("revision %s does not match the allowed revision %s", sr.Revision, s.Revision)
	}
	return nil
}

func (s *SeedManifestSnapRevision) String() string {
	return fmt.Sprintf("%s %s", s.SnapName, s.Revision)
}

func (s *SeedManifestSnapRevision) Unique() string {
	return s.SnapName
}

// SeedManifestValidationSet represents a validation set as noted
// in the seed manifest. A validation set can optionally be pinned,
// but the sequence will always be set to the sequence that was used
// during the image build.
type SeedManifestValidationSet struct {
	AccountID string
	Name      string
	Sequence  int
	Pinned    bool
}

func (s *SeedManifestValidationSet) Check(against SeedManifestEntry) error {
	sr, ok := against.(*SeedManifestValidationSet)
	if !ok {
		return fmt.Errorf("internal error: expected SeedManifestEntry to be SeedManifestValidationSet")
	}

	if s.Sequence != sr.Sequence {
		return fmt.Errorf("sequence of %q (%d) does not match the allowed sequence (%d)", sr.Unique(), sr.Sequence, s.Sequence)
	} else if s.Pinned != sr.Pinned {
		return fmt.Errorf("pinning of %q (%t) does not match the allowed pinning (%t)", sr.Unique(), sr.Pinned, s.Pinned)
	}
	return nil
}

func (s *SeedManifestValidationSet) String() string {
	if s.Pinned {
		return fmt.Sprintf("%s/%s=%d", s.AccountID, s.Name, s.Sequence)
	} else {
		return fmt.Sprintf("%s/%s %d", s.AccountID, s.Name, s.Sequence)
	}
}

func (s *SeedManifestValidationSet) Unique() string {
	return fmt.Sprintf("%s/%s", s.AccountID, s.Name)
}

// Represents the validation-sets and snaps that are used to build
// an image seed. The manifest will only allow adding entries once to support
// a pre-provided manifest
// The seed.manifest generated by ubuntu-image contains entries in the following
// format:
// <account-id>/<name>=<sequence>
// <account-id>/<name> <sequence>
// <snap-name> <snap-revision>
type SeedManifest struct {
	allowed map[string]SeedManifestEntry
	used    map[string]SeedManifestEntry
}

func NewSeedManifest() *SeedManifest {
	return &SeedManifest{
		allowed: make(map[string]SeedManifestEntry),
		used:    make(map[string]SeedManifestEntry),
	}
}

// SeedManifestFromSnapRevisions is only here for usage in tests to simplify
// testing contents of ImageManifest as rules/used are not exported.
func SeedManifestFromSnapRevisions(rules map[string]snap.Revision) *SeedManifest {
	im := NewSeedManifest()
	for sn, rev := range rules {
		im.SetAllowedSnapRevision(sn, rev.N)
	}
	return im
}

func (sm *SeedManifest) addAllowedOnce(allowed SeedManifestEntry) {
	key := allowed.Unique()
	if _, ok := sm.allowed[key]; !ok {
		sm.allowed[key] = allowed
	}
}

func (sm *SeedManifest) addUsedOnce(used SeedManifestEntry) error {
	key := used.Unique()
	if allowed, ok := sm.allowed[key]; ok {
		// Found a rule for this key
		if err := allowed.Check(used); err != nil {
			return err
		}
	}
	if _, ok := sm.used[key]; !ok {
		sm.used[key] = used
	}
	return nil
}

// SetAllowedSnapRevision adds a revision rule for the given snap name, meaning
// that any snap marked used through MarkSnapRevisionUsed will be validated against
// this rule. The manifest will only allow one revision per snap, meaning that any
// subsequent calls to this will be ignored.
func (sm *SeedManifest) SetAllowedSnapRevision(snapName string, revision int) error {
	if revision == 0 {
		return fmt.Errorf("cannot add a rule for a zero-value revision")
	}
	sm.addAllowedOnce(&SeedManifestSnapRevision{
		SnapName: snapName,
		Revision: snap.R(revision),
	})
	return nil
}

// SetAllowedValidationSet adds a sequence rule for the given validation set, meaning
// that any validation set marked for use through MarkValidationSetUsed must match the
// given parameters. The manifest will only allow one sequence per validation set,
// meaning that any subsequent calls to this will be ignored.
func (sm *SeedManifest) SetAllowedValidationSet(accountID, name string, sequence int, pinned bool) error {
	if sequence <= 0 {
		return fmt.Errorf("cannot add allowed validation set for a unknown sequence")
	}
	sm.addAllowedOnce(&SeedManifestValidationSet{
		AccountID: accountID,
		Name:      name,
		Sequence:  sequence,
		Pinned:    pinned,
	})
	return nil
}

// MarkSnapRevisionUsed validates the revision of the given snap name against any
// previously setup revision rules by SetAllowedSnapRevision. Attempting to mark
// the same snap of multiple revisions used will be ignored..
func (sm *SeedManifest) MarkSnapRevisionUsed(snapName string, revision int) error {
	return sm.addUsedOnce(&SeedManifestSnapRevision{
		SnapName: snapName,
		Revision: snap.R(revision),
	})
}

// MarkValidationSetUsed tracks the used validation set in the manifest.T he manifest
// will only allow one revision per snap, meaning that any subsequent calls to this will
// be ignored.
func (sm *SeedManifest) MarkValidationSetUsed(accountID, name string, sequence int, pinned bool) error {
	if sequence <= 0 {
		return fmt.Errorf("cannot mark validation-set \"%s/%s\" used, sequence must be set", accountID, name)
	}
	return sm.addUsedOnce(&SeedManifestValidationSet{
		AccountID: accountID,
		Name:      name,
		Sequence:  sequence,
		Pinned:    pinned,
	})
}

// AllowedRevision retrieves any specified revision rule for the snap
// name.
func (sm *SeedManifest) AllowedSnapRevision(snapName string) snap.Revision {
	if allowed, ok := sm.allowed[snapName]; ok {
		if sr, ok := allowed.(*SeedManifestSnapRevision); ok {
			return sr.Revision
		}
	}
	return snap.Revision{}
}

// ValidationSetsAllowed returns the validation sets specified as allowed.
func (sm *SeedManifest) ValidationSetsAllowed() []*SeedManifestValidationSet {
	var vss []*SeedManifestValidationSet
	for _, s := range sm.allowed {
		if vs, ok := s.(*SeedManifestValidationSet); ok {
			vss = append(vss, vs)
		}
	}
	return vss
}

func parsePinnedValidationSet(sm *SeedManifest, vs string) error {
	acc, name, seq, err := snapasserts.ParseValidationSet(vs)
	if err != nil {
		return err
	}
	return sm.SetAllowedValidationSet(acc, name, seq, true)
}

func parseUnpinnedValidationSet(sm *SeedManifest, vs, seqStr string) error {
	acc, name, _, err := snapasserts.ParseValidationSet(vs)
	if err != nil {
		return err
	}
	seq, err := strconv.Atoi(seqStr)
	if err != nil {
		return fmt.Errorf("invalid formatted validation-set sequence: %q", seqStr)
	}
	return sm.SetAllowedValidationSet(acc, name, seq, false)
}

func parseSnapRevision(sm *SeedManifest, sn, revStr string) error {
	if err := snap.ValidateName(sn); err != nil {
		return err
	}

	rev, err := snap.ParseRevision(revStr)
	if err != nil {
		return err
	}

	// Values that are higher than 0 indicate the revision comes from the store, and values
	// lower than 0 indicate the snap was sourced locally. We allow both in the seed.manifest as
	// long as the user can provide us with the correct snaps. The only number we won't accept is
	// 0.
	if rev.Unset() {
		return fmt.Errorf("cannot use revision %d for snap %q: revision must not be 0", rev, sn)
	}
	return sm.SetAllowedSnapRevision(sn, rev.N)
}

// ReadSeedManifest reads a seed.manifest generated by ubuntu-image, and returns
// a map containing the snap names and their revisions.
func ReadSeedManifest(manifestFile string) (*SeedManifest, error) {
	f, err := os.Open(manifestFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sm := NewSeedManifest()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, " ") {
			return nil, fmt.Errorf("line cannot start with any spaces: %q", line)
		}

		tokens := strings.Fields(line)

		if len(tokens) == 1 && strings.Contains(tokens[0], "/") {
			// Pinned validation-set: <account-id>/<name>=<sequence>
			if err := parsePinnedValidationSet(sm, tokens[0]); err != nil {
				return nil, err
			}
		} else if len(tokens) == 2 {
			if strings.Contains(tokens[0], "/") {
				// Unpinned validation-set: <account-id>/<name> <sequence>
				if err := parseUnpinnedValidationSet(sm, tokens[0], tokens[1]); err != nil {
					return nil, err
				}
			} else {
				// Snap revision: <snap> <revision>
				if err := parseSnapRevision(sm, tokens[0], tokens[1]); err != nil {
					return nil, err
				}
			}
		} else {
			return nil, fmt.Errorf("line is illegally formatted: %q", line)
		}
	}
	return sm, nil
}

// Write generates the seed.manifest contents from the provided map of
// snaps and their revisions, and stores them in the given file path.
func (sm *SeedManifest) Write(filePath string) error {
	if len(sm.used) == 0 {
		return nil
	}

	vsKeys := make([]string, 0, len(sm.used))
	for k, s := range sm.used {
		if _, ok := s.(*SeedManifestValidationSet); ok {
			vsKeys = append(vsKeys, k)
		}
	}
	sort.Strings(vsKeys)

	revisionKeys := make([]string, 0, len(sm.used))
	for k, s := range sm.used {
		if _, ok := s.(*SeedManifestSnapRevision); ok {
			revisionKeys = append(revisionKeys, k)
		}
	}
	sort.Strings(revisionKeys)

	buf := bytes.NewBuffer(nil)
	for _, key := range vsKeys {
		fmt.Fprintf(buf, "%s\n", sm.used[key])
	}
	for _, key := range revisionKeys {
		fmt.Fprintf(buf, "%s\n", sm.used[key])
	}
	return ioutil.WriteFile(filePath, buf.Bytes(), 0755)
}
