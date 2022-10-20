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
	"fmt"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
)

// The seed.manifest generated by ubuntu-image contains entries in the following
// format:
// <snap-name> <snap-revision>.snap
// Why they end with a .snap is anyone's guess. The goal in a future iteration of
// this will be to move the generation of the seed.manifest to this package, out
// of ubuntu-image.
// TODO: Move generation of seed.manifest from ubuntu-image to here
var revisionEntryRegex = regexp.MustCompile(`([^\s]+) ([0-9]+).snap`)

// ReadSeedManifest reads a seed.manifest generated by ubuntu-image, and returns
// an map containing the snap names and their revisions.
func ReadSeedManifest(manifestFile string) (map[string]int, error) {
	contents, err := ioutil.ReadFile(manifestFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read seed manifest: %v", err)
	}

	matches := revisionEntryRegex.FindAllStringSubmatch(string(contents), -1)
	revisions := make(map[string]int, len(matches))
	for _, c := range matches {
		value, err := strconv.Atoi(c[2])
		if err != nil {
			return nil, fmt.Errorf("cannot read seed manifest file: %v", err)
		}
		if value <= 0 {
			return nil, fmt.Errorf("cannot use revision %d for snap %q: revision must be higher than 0", value, c[1])
		}
		revisions[c[1]] = value
	}
	return revisions, nil
}

// WriteSeedManifest generates the seed.manifest contents from the provided map of
// snaps and their revisions, and stores in the the file path provided
func WriteSeedManifest(filePath string, revisions map[string]int) error {
	if len(revisions) == 0 {
		return nil
	}

	var sb strings.Builder
	for key, value := range revisions {
		if value <= 0 {
			return fmt.Errorf("invalid revision %d given for snap %q, revision must be a positive value", value, key)
		}
		sb.WriteString(fmt.Sprintf("%s %d.snap\n", key, value))
	}
	return ioutil.WriteFile(filePath, []byte(sb.String()), 0755)
}
