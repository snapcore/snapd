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

package dm_verity

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type DmVerityBlock struct {
	RootHash string `json:"root-hash"`
}

func NewDmVerityBlock(rootHash string) *DmVerityBlock {
	dmVerityBlock := DmVerityBlock{}
	dmVerityBlock.RootHash = rootHash
	return &dmVerityBlock
}

func getRootHashFromOutput(output []byte) (string, error) {
	rootHash := ""

	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "Root hash") {
			val := strings.SplitN(line, ":", 2)[1]
			rootHash = strings.TrimSpace(val)
		}
	}

	if len(rootHash) == 0 {
		return "", fmt.Errorf("empty root hash")
	}

	return rootHash, nil

}

// Runs veritysetup format and returns a DmVerityBlock which include the rootHash
func Format(dataDevice string, hashDevice string) (*DmVerityBlock, error) {
	cmd := exec.Command("veritysetup", "format", dataDevice, hashDevice)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, osutil.OutputErr(output, err)
	}

	logger.Debugf("%s", string(output))

	rootHash, err := getRootHashFromOutput(output)
	if err != nil {
		return nil, err
	}

	return NewDmVerityBlock(rootHash), nil
}
