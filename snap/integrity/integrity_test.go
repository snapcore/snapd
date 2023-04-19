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
	headerSize := uint64(integrity.HeaderSize)

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

	header := make([]byte, headerSize)
	n, err := snapFile.Read(header)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, int(headerSize))

	var integrityDataHeader integrity.IntegrityDataHeader
	err = integrityDataHeader.Unserialize(header)
	c.Check(err, IsNil)
	c.Check(integrityDataHeader.Type, Equals, "integrity")
	c.Check(integrityDataHeader.Size, Equals, uint64(2*4096))
	c.Check(integrityDataHeader.DmVerityBlock.RootHash, HasLen, 64)
}

type testFindIntegrityDataData struct {
	snapPath  string
	orig_size int64
}

func (s *IntegrityTestSuite) testFindIntegrityData(c *C, data *testFindIntegrityDataData) error {
	blockSize := uint64(integrity.BlockSize)
	headerSize := uint64(integrity.HeaderSize)

	integrityData, err := integrity.FindIntegrityData(data.snapPath)
	if err != nil {
		return err
	}

	c.Check(integrityData.SourceFilePath, Equals, data.snapPath)

	snapFile, err := os.Open(data.snapPath)
	if err != nil {
		return err
	}
	defer snapFile.Close()

	// Read header from file
	header := make([]byte, headerSize)
	_, err = snapFile.Seek(data.orig_size, io.SeekStart)
	c.Assert(err, IsNil)

	n, err := snapFile.Read(header)
	if err != nil {
		return err
	}
	c.Assert(n, Equals, int(headerSize))

	var integrityDataHeader integrity.IntegrityDataHeader
	integrityDataHeader.Unserialize(header)
	c.Check(*integrityData.Header, DeepEquals, integrityDataHeader)
	c.Check(integrityData.Offset, Equals, blockSize)

	return nil
}

func (s *IntegrityTestSuite) TestIntegrityDataAttached(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	snapFileInfo, err := os.Stat(snapPath)
	c.Assert(err, IsNil)
	orig_size := snapFileInfo.Size()

	err = integrity.GenerateAndAppend(snapPath)
	c.Assert(err, IsNil)

	err = s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath:  snapPath,
		orig_size: orig_size,
	})

	c.Check(err, IsNil)
}

func (s *IntegrityTestSuite) TestSnapFileNotExist(c *C) {
	c.Check(s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath: "foo.snap",
	}), ErrorMatches, "open foo.snap: no such file or directory")
}

func (s *IntegrityTestSuite) TestIntegrityDataNotAttached(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	snapFileInfo, err := os.Stat(snapPath)
	c.Assert(err, IsNil)
	orig_size := snapFileInfo.Size()

	c.Check(s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath:  snapPath,
		orig_size: orig_size,
	}), ErrorMatches, "Integrity data not found for snap "+snapPath)
}

func (s *IntegrityTestSuite) TestIntegrityDataAttachedWrongHeader(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	snapFileInfo, err := os.Stat(snapPath)
	c.Assert(err, IsNil)
	orig_size := snapFileInfo.Size()

	snapFile, err := os.OpenFile(snapPath, os.O_APPEND|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)

	extraData := make([]byte, uint64(integrity.BlockSize))

	_, err = snapFile.Write(extraData)
	c.Assert(err, IsNil)

	snapFile.Close()

	c.Check(s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath:  snapPath,
		orig_size: orig_size,
	}), ErrorMatches, "invalid integrity data header")
}

func (s *IntegrityTestSuite) TestIntegrityDataAttachedWrongHeaderSmall(c *C) {
	snapPath, _ := snaptest.MakeTestSnapInfoWithFiles(c, "name: foo\nversion: 1.0", nil, nil)

	snapFile, err := os.OpenFile(snapPath, os.O_APPEND|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)

	// expect a different error for an unaligned header
	extraData := make([]byte, uint64(integrity.BlockSize)-1)

	_, err = snapFile.Write(extraData)
	c.Assert(err, IsNil)

	snapFile.Close()

	c.Check(s.testFindIntegrityData(c, &testFindIntegrityDataData{
		snapPath: snapPath,
	}), ErrorMatches, "failed to read integrity data: integrity data header corrupted\\?")
}
