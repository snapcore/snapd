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

package integrity

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap/integrity/dm_verity"
	"github.com/snapcore/snapd/snap/squashfs"
	"golang.org/x/crypto/sha3"
)

const (
	blockSize = 4096
	// https://github.com/plougher/squashfs-tools/blob/master/squashfs-tools/squashfs_fs.h#L289
	squashfsSuperblockBytesUsedOffset = 40
)

var (
	// magic is the magic prefix of snap metadata blocks.
	magic = []byte{'s', 'n', 'a', 'p', 'e', 'x', 't'}
)

// Align input `size` to closest `blockSize` value
func align(size uint64) uint64 {
	return (size + blockSize - 1) / blockSize * blockSize
}

type IntegrityData struct {
	Header         *IntegrityDataHeader
	Offset         uint64
	SourceFilePath string

	Bytes *[]byte
}

func FindIntegrityData(snapName string) (*IntegrityData, error) {
	integrityData := IntegrityData{}

	if !squashfs.FileHasSquashfsHeader(snapName) {
		return nil, fmt.Errorf("input file does not contain a SquashFS filesystem")
	}

	snapFile, err := os.OpenFile(snapName, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	defer snapFile.Close()

	// Seek to bytes_used field of SquashFS superblock
	_, err = snapFile.Seek(squashfsSuperblockBytesUsedOffset, io.SeekStart)

	var squashFSSize uint64
	if err := binary.Read(snapFile, binary.LittleEndian, &squashFSSize); err != nil {
		return nil, err
	}

	logger.Debugf("SquashFS bytes used: %d", squashFSSize)

	snapFileInfo, err := snapFile.Stat()
	if err != nil {
		return nil, err
	}

	// Align squashFSSize to blockSize
	offset := align(squashFSSize)

	// If no extra data attached at the end of the squashfs return empty integrityData
	if offset == uint64(snapFileInfo.Size()) {
		return nil, nil
	}

	_, err = snapFile.Seek(int64(offset), io.SeekStart)

	integrityDataBytes := make([]byte, uint64(snapFileInfo.Size())-offset)
	n, err := snapFile.Read(integrityDataBytes)
	if n < blockSize-1 {
		return &integrityData, fmt.Errorf("failed to read integrity data: integrity data header corrupted?")
	}

	// TODO dm-verity: try to read from a separate hash file
	integrityData.SourceFilePath = snapName

	integrityDataHeader, err := ExtractIntegrityDataHeader(integrityDataBytes)
	if err != nil {
		return nil, err
	}
	integrityData.Header = integrityDataHeader
	integrityData.Offset = offset
	integrityData.Bytes = &integrityDataBytes

	return &integrityData, nil
}

func (integrityData IntegrityData) Validate(snapRev asserts.SnapRevision) error {
	integrityDataHash := integrityData.Sha3_384()

	assertionIntegrity, _ := snapRev.Integrity().(map[string]string)
	assertionIntegrityHash := assertionIntegrity["sha3-384"]

	if integrityDataHash != assertionIntegrityHash {
		return fmt.Errorf("integrity data hash mismatch")
	}
	return nil
}

func ExtractIntegrityDataHeader(integrityDataBytes []byte) (*IntegrityDataHeader, error) {
	integrityDataHeader := IntegrityDataHeader{}

	err := integrityDataHeader.Unserialize(integrityDataBytes[:blockSize])
	if err != nil {
		return nil, err
	}

	return &integrityDataHeader, nil
}

func (integrityData IntegrityData) Sha3_384() string {
	hash := sha3.Sum384(*integrityData.Bytes)
	return hex.EncodeToString(hash[:])
}

// IntegrityDataHeader gets appended first at the end of a squashfs packed snap
// before the dm-verity data
// Size field includes the header size
type IntegrityDataHeader struct {
	Type          string                  `json:"type"`
	Size          uint64                  `json:"size,string"`
	DmVerityBlock dm_verity.DmVerityBlock `json:"dm-verity"`
}

func NewIntegrityDataHeader(dmVerityBlock *dm_verity.DmVerityBlock, integrityDataSize uint64) (*IntegrityDataHeader, error) {
	integrityDataHeader := IntegrityDataHeader{}
	integrityDataHeader.Type = "integrity"
	integrityDataHeader.DmVerityBlock = *dmVerityBlock

	// calculate IntegrityDataHeader serialized size
	jsonHeader, err := json.Marshal(integrityDataHeader)
	if err != nil {
		return nil, err
	}

	// For now that the header only includes a fixed string and a fixed-size hash,
	// the size calculation is irrelevant and will effectively always return blockSize
	headerSize := align(uint64(len(magic) + len(jsonHeader) + 1))
	logger.Debugf("Magic size: %d", len(magic))
	logger.Debugf("IntegrityDataHeader JSON size: %d (+1 byte for the null byte)", len(jsonHeader))
	logger.Debugf("Aligned header size: %d", headerSize)

	integrityDataHeader.Size = headerSize + integrityDataSize

	return &integrityDataHeader, nil
}

func (integrityDataHeader IntegrityDataHeader) Serialize() ([]byte, error) {
	jsonHeader, err := json.Marshal(integrityDataHeader)
	if err != nil {
		return nil, err
	}
	logger.Debugf("integrity data header:\n%s", string(jsonHeader))

	// \0 terminate
	jsonHeader = append(jsonHeader, 0)

	headerSize := align(uint64(len(magic) + len(jsonHeader)))
	header := make([]byte, headerSize)

	copy(header, append(magic, jsonHeader...))

	return header, nil
}

func (integrityDataHeader *IntegrityDataHeader) Unserialize(input []byte) error {
	if !bytes.HasPrefix(input, magic) {
		return fmt.Errorf("invalid integrity data header")
	}

	input = bytes.Trim(input, "\x00")
	err := json.Unmarshal(input[len(magic):], &integrityDataHeader)
	if err != nil {
		return err
	}

	return nil
}

func GenerateAndAppend(snapName string) error {
	// Generate verity metadata
	hashFileName := snapName + ".verity"
	dmVerityBlock, err := dm_verity.Format(snapName, hashFileName)
	if err != nil {
		return err
	}

	hashFile, err := os.OpenFile(hashFileName, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		hashFile.Close()
		if e := os.Remove(hashFileName); e != nil {
			err = e
		}
	}()

	fi, err := hashFile.Stat()
	if err != nil {
		return err
	}

	integrityDataHeader, err := NewIntegrityDataHeader(dmVerityBlock, uint64(fi.Size()))
	if err != nil {
		return err
	}

	// Append header to snap
	header, err := integrityDataHeader.Serialize()
	if err != nil {
		return err
	}

	snapFile, err := os.OpenFile(snapName, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer snapFile.Close()

	if _, err = snapFile.Write(header); err != nil {
		return err
	}

	// Append verity metadata to snap
	if _, err := io.Copy(snapFile, hashFile); err != nil {
		return err
	}

	return err
}
