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

package integrity_test

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/integrity"
	"github.com/snapcore/snapd/snap/integrity/dm_verity"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type IntegrityTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&IntegrityTestSuite{})

func (s *IntegrityTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *IntegrityTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *IntegrityTestSuite) TestAlign(c *C) {
	align := integrity.Align
	blockSize := uint64(integrity.BlockSize)

	for _, tc := range []struct {
		input          uint64
		expectedOutput uint64
	}{
		{0, 0},
		{1, blockSize},
		{blockSize, blockSize},
		{blockSize + 1, 2 * blockSize},
	} {
		ret := align(tc.input)
		c.Check(ret, Equals, tc.expectedOutput, Commentf("%v", tc))
	}
}

func (s *IntegrityTestSuite) TestIntegrityHeaderMarshalJSON(c *C) {
	dmVerityBlock := &dm_verity.DmVerityBlock{}
	integrityDataHeader, err := integrity.NewIntegrityDataHeader(dmVerityBlock, 4096)
	c.Assert(err, IsNil)

	jsonHeader, err := json.Marshal(integrityDataHeader)
	c.Assert(err, IsNil)

	c.Check(json.Valid(jsonHeader), Equals, true)
}

func (s *IntegrityTestSuite) TestIntegrityHeaderUnmarshalJSON(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	integrityHeaderJSON := `{
		"type": "integrity",
		"size": "4096",
		"dm-verity": {
			"root-hash": "00000000000000000000000000000000"
		}
	}`

	err := json.Unmarshal([]byte(integrityHeaderJSON), &integrityDataHeader)
	c.Assert(err, IsNil)

	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, uint64(4096))
	c.Check(integrityDataHeader.DmVerityBlock.RootHash, Equals, "00000000000000000000000000000000")
}

func (s *IntegrityTestSuite) TestIntegrityHeaderSerialize(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	magic := integrity.Magic

	header, err := integrityDataHeader.Serialize()
	c.Assert(err, IsNil)

	magicRead := header[0:len(magic)]
	c.Check(magicRead, DeepEquals, magic)

	nullByte := header[len(header)-1:]
	c.Check(nullByte, DeepEquals, []byte{0x0})

	c.Check(uint64(len(header)), Equals, integrity.Align(uint64(len(header))))
}

func (s *IntegrityTestSuite) TestIntegrityHeaderUnserialize(c *C) {
	var integrityDataHeader integrity.IntegrityDataHeader
	magic := integrity.Magic

	integrityHeaderJSON := `{
		"type": "integrity",
		"size": "4096",
		"dm-verity": {
			"root-hash": "00000000000000000000000000000000"
		}
	}`
	header := append(magic, integrityHeaderJSON...)
	header = append(header, 0)

	headerBlock := make([]byte, 4096)
	copy(headerBlock, header)

	err := integrityDataHeader.Unserialize(headerBlock)
	c.Assert(err, IsNil)

	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, uint64(4096))
	c.Check(integrityDataHeader.DmVerityBlock.RootHash, Equals, "00000000000000000000000000000000")
}

func (s *IntegrityTestSuite) TestGenerateAndAppendSuccess(c *C) {
	blockSize := uint64(integrity.BlockSize)

	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	snapFileInfo, err := os.Stat(snapPath)
	c.Assert(err, IsNil)
	orig_size := snapFileInfo.Size()

	err = integrity.GenerateAndAppend(snapPath)
	c.Assert(err, IsNil)

	snapFile, err := os.Open(snapPath)
	c.Assert(err, IsNil)
	defer snapFile.Close()

	// check integrity header
	_, err = snapFile.Seek(orig_size, io.SeekStart)
	c.Assert(err, IsNil)

	header := make([]byte, blockSize-1)
	n, err := snapFile.Read(header)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int(blockSize)-1)

	var integrityDataHeader integrity.IntegrityDataHeader
	err = integrityDataHeader.Unserialize(header)
	c.Check(err, IsNil)
	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, uint64(2*4096))
	c.Check(integrityDataHeader.DmVerityBlock.RootHash, HasLen, 64)
}

func (s *IntegrityTestSuite) TestFindIntegrityData(c *C) {
	blockSize := uint64(integrity.BlockSize)

	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	snapFileInfo, err := os.Stat(snapPath)
	c.Assert(err, IsNil)
	orig_size := snapFileInfo.Size()

	err = integrity.GenerateAndAppend(snapPath)
	c.Assert(err, IsNil)

	snapFileInfo, err = os.Stat(snapPath)
	c.Assert(err, IsNil)
	size := snapFileInfo.Size()

	integrityData, err := integrity.FindIntegrityData(snapPath)
	c.Assert(err, IsNil)
	c.Check(integrityData.SourceFilePath, Equals, snapPath)

	snapFile, err := os.Open(snapPath)
	c.Assert(err, IsNil)
	defer snapFile.Close()

	// Read header from file
	header := make([]byte, blockSize-1)
	_, err = snapFile.Seek(orig_size, io.SeekStart)
	c.Assert(err, IsNil)

	n, err := snapFile.Read(header)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int(blockSize)-1)

	var integrityDataHeader integrity.IntegrityDataHeader
	integrityDataHeader.Unserialize(header)
	c.Check(*integrityData.Header, DeepEquals, integrityDataHeader)
	c.Check(integrityData.Offset, Equals, blockSize)

	// Read all hash data from file
	data := make([]byte, size-orig_size)
	_, err = snapFile.Seek(orig_size, io.SeekStart)
	c.Assert(err, IsNil)

	n, err = snapFile.Read(data)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int(size-orig_size))

	c.Check(*integrityData.Bytes, DeepEquals, data)
}
