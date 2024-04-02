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

package user

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"unicode"
)

func getEnt(params ...string) ([]byte, error) {
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer

	cmd := exec.Command("getent", params...)
	cmd.Stdin = nil
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("getent returned an error: %q", errBuf.Bytes())
	}

	return outBuf.Bytes(), nil
}

// lookupFromGetent calls getent, parses and filters its output
// The component at `index` will need to match `expectedValue`.
// If `isKey`, then `expectedValue` will also be passed as parameter
// to getent along `database`. `numComponents` should be 4 for groups
// and 7 for users.
func lookupFromGetent(database string, index int, expectedValue string, isKey bool, numComponents int) ([]string, error) {
	params := []string{database}
	if isKey {
		params = append(params, expectedValue)
	}
	buf, err := getEnt(params...)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(buf))
	for scanner.Scan() {
		components := strings.SplitN(scanner.Text(), ":", numComponents)
		if len(components) != numComponents {
			continue
		}

		if components[index] != expectedValue {
			continue
		}

		return components, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func isNumeric(value string) bool {
	for _, c := range value {
		return unicode.IsDigit(c)
	}
	return false
}

func isKey(index int, expectedValue string) bool {
	numeric := isNumeric(expectedValue)
	return (index == 0 && !numeric) || (index == 2 && numeric)
}

func lookupGroupFromGetent(index int, expectedValue string) (*Group, error) {
	components, err := lookupFromGetent("group", index, expectedValue, isKey(index, expectedValue), 4)

	if err != nil {
		return nil, err
	}

	if components == nil {
		return nil, nil
	}

	return &Group{
		Name: components[0],
		Gid:  components[2],
	}, nil
}

func lookupUserFromGetent(index int, expectedValue string) (*User, error) {
	components, err := lookupFromGetent("passwd", index, expectedValue, isKey(index, expectedValue), 7)

	if err != nil {
		return nil, err
	}

	if components == nil {
		return nil, nil
	}

	return &User{
		Username: components[0],
		Uid:      components[2],
		Gid:      components[3],
		Name:     components[4],
		HomeDir:  components[5],
	}, nil
}
