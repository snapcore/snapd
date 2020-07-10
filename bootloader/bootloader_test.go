// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package bootloader_test

import (
	"errors"
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

type baseBootenvTestSuite struct {
	testutil.BaseTest

	rootdir string
}

func (s *baseBootenvTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.rootdir = c.MkDir()
}

type bootenvTestSuite struct {
	baseBootenvTestSuite

	b *bootloadertest.MockBootloader
}

var _ = Suite(&bootenvTestSuite{})

func (s *bootenvTestSuite) SetUpTest(c *C) {
	s.baseBootenvTestSuite.SetUpTest(c)

	s.b = bootloadertest.Mock("mocky", c.MkDir())
}

func (s *bootenvTestSuite) TestForceBootloader(c *C) {
	bootloader.Force(s.b)
	defer bootloader.Force(nil)

	got, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	c.Check(got, Equals, s.b)
}

func (s *bootenvTestSuite) TestForceBootloaderError(c *C) {
	myErr := errors.New("zap")
	bootloader.ForceError(myErr)
	defer bootloader.ForceError(nil)

	got, err := bootloader.Find("", nil)
	c.Assert(err, Equals, myErr)
	c.Check(got, IsNil)
}

func (s *bootenvTestSuite) TestInstallBootloaderConfigNoConfig(c *C) {
	err := bootloader.InstallBootConfig(c.MkDir(), s.rootdir, nil)
	c.Assert(err, ErrorMatches, `cannot find boot config in.*`)
}

func (s *bootenvTestSuite) TestInstallBootloaderConfigFromGadget(c *C) {
	for _, t := range []struct {
		name                string
		gadgetFile, sysFile string
		gadgetFileContent   []byte
		opts                *bootloader.Options
	}{
		{name: "grub", gadgetFile: "grub.conf", sysFile: "/boot/grub/grub.cfg"},
		// traditional uboot.env - the uboot.env file needs to be non-empty
		{name: "uboot.env", gadgetFile: "uboot.conf", sysFile: "/boot/uboot/uboot.env", gadgetFileContent: []byte{1}},
		// boot.scr in place of uboot.env means we create the boot.sel file
		{
			name:       "uboot boot.scr",
			gadgetFile: "uboot.conf",
			sysFile:    "/uboot/ubuntu/boot.sel",
			opts:       &bootloader.Options{NoSlashBoot: true},
		},
		{name: "androidboot", gadgetFile: "androidboot.conf", sysFile: "/boot/androidboot/androidboot.env"},
		{name: "lk", gadgetFile: "lk.conf", sysFile: "/boot/lk/snapbootsel.bin"},
	} {
		mockGadgetDir := c.MkDir()
		rootDir := c.MkDir()
		err := ioutil.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), t.gadgetFileContent, 0644)
		c.Assert(err, IsNil)
		err = bootloader.InstallBootConfig(mockGadgetDir, rootDir, t.opts)
		c.Assert(err, IsNil, Commentf("installing boot config for %s", t.name))
		fn := filepath.Join(rootDir, t.sysFile)
		c.Assert(fn, testutil.FilePresent, Commentf("boot config missing for %s at %s", t.name, t.sysFile))
	}
}

func (s *bootenvTestSuite) TestInstallBootloaderConfigFromAssets(c *C) {
	recoveryOpts := &bootloader.Options{
		Recovery: true,
	}
	systemBootOpts := &bootloader.Options{
		ExtractedRunKernelImage: true,
	}
	defaultRecoveryGrubAsset := assets.Internal("grub-recovery.cfg")
	c.Assert(defaultRecoveryGrubAsset, NotNil)
	defaultGrubAsset := assets.Internal("grub.cfg")
	c.Assert(defaultGrubAsset, NotNil)

	for _, t := range []struct {
		name                string
		gadgetFile, sysFile string
		gadgetFileContent   []byte
		sysFileContent      []byte
		assetContent        []byte
		assetName           string
		err                 string
		opts                *bootloader.Options
	}{
		{
			name:       "recovery grub",
			opts:       recoveryOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub-recovery.cfg",
			assetContent:      []byte("hello assets"),
			// boot config from assets
			sysFileContent: []byte("hello assets"),
		}, {
			name:              "recovery grub with non empty gadget file",
			opts:              recoveryOpts,
			gadgetFile:        "grub.conf",
			gadgetFileContent: []byte("not so empty"),
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub-recovery.cfg",
			assetContent:      []byte("hello assets"),
			// boot config from assets
			sysFileContent: []byte("hello assets"),
		}, {
			name:       "recovery grub with default asset",
			opts:       recoveryOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			sysFileContent:    defaultRecoveryGrubAsset,
		}, {
			name:       "recovery grub missing asset",
			opts:       recoveryOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub-recovery.cfg",
			// no asset content
			err: `internal error: no boot asset for "grub-recovery.cfg"`,
		}, {
			name:       "system-boot grub",
			opts:       systemBootOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			assetName:         "grub.cfg",
			assetContent:      []byte("hello assets"),
			sysFileContent:    []byte("hello assets"),
		}, {
			name:       "system-boot grub with default asset",
			opts:       systemBootOpts,
			gadgetFile: "grub.conf",
			// empty file in the gadget
			gadgetFileContent: nil,
			sysFile:           "/EFI/ubuntu/grub.cfg",
			sysFileContent:    defaultGrubAsset,
		},
	} {
		mockGadgetDir := c.MkDir()
		rootDir := c.MkDir()
		fn := filepath.Join(rootDir, t.sysFile)
		err := ioutil.WriteFile(filepath.Join(mockGadgetDir, t.gadgetFile), t.gadgetFileContent, 0644)
		c.Assert(err, IsNil)
		var restoreAsset func()
		if t.assetName != "" {
			restoreAsset = assets.MockInternal(t.assetName, t.assetContent)
		}
		err = bootloader.InstallBootConfig(mockGadgetDir, rootDir, t.opts)
		if t.err == "" {
			c.Assert(err, IsNil, Commentf("installing boot config for %s", t.name))
			// mocked asset content
			c.Assert(fn, testutil.FileEquals, string(t.sysFileContent))
		} else {
			c.Assert(err, ErrorMatches, t.err)
			c.Assert(fn, testutil.FileAbsent)
		}
		if restoreAsset != nil {
			restoreAsset()
		}
	}
}

func (s *bootenvTestSuite) TestSplitKernelCommandLine(c *C) {
	for idx, tc := range []struct {
		cmd    string
		exp    []string
		errStr string
	}{
		{cmd: ``, exp: nil},
		{cmd: `foo bar baz`, exp: []string{"foo", "bar", "baz"}},
		{cmd: `foo=" many   spaces  " bar`, exp: []string{`foo=" many   spaces  "`, "bar"}},
		{cmd: `foo="1$2"`, exp: []string{`foo="1$2"`}},
		{cmd: `foo=1$2`, exp: []string{`foo=1$2`}},
		{cmd: `foo= bar`, exp: []string{"foo=", "bar"}},
		{cmd: `foo= bar`, exp: []string{"foo=", "bar"}},
		{cmd: `foo=""`, exp: []string{`foo=""`}},
		{cmd: `   cpu=1,2,3   mem=0x2000;0x4000:$2  `, exp: []string{"cpu=1,2,3", "mem=0x2000;0x4000:$2"}},
		{cmd: "isolcpus=1,2,10-20,100-2000:2/25", exp: []string{"isolcpus=1,2,10-20,100-2000:2/25"}},
		// bad quoting, or otherwise malformed command line
		{cmd: `foo="1$2`, errStr: "unbalanced quoting"},
		{cmd: `"foo"`, errStr: "unexpected quoting"},
		{cmd: `="foo"`, errStr: "unexpected quoting"},
		{cmd: `foo"foo"`, errStr: "unexpected quoting"},
		{cmd: `foo=foo"`, errStr: "unexpected quoting"},
		{cmd: `foo="a""b"`, errStr: "unexpected quoting"},
		{cmd: `foo="a foo="b`, errStr: "unexpected argument"},
		{cmd: `foo="a"="b"`, errStr: "unexpected assignment"},
	} {
		c.Logf("%v: cmd: %q", idx, tc.cmd)
		out, err := bootloader.KernelCommandLineSplit(tc.cmd)
		if tc.errStr != "" {
			c.Assert(err, ErrorMatches, tc.errStr)
			c.Check(out, IsNil)
		} else {
			c.Assert(err, IsNil)
			c.Check(out, DeepEquals, tc.exp)
		}
	}
}
