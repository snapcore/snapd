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

package bootloader

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

type uboot struct {
	rootdir string
	basedir string

	ubootEnvFmt      ubootenv.Format
	ubootEnvFileName string
}

func (u *uboot) setDefaults() {
	u.basedir = "/boot/uboot/"
	// default native format, change if opts is set
	u.ubootEnvFmt = ubootenv.NativeFormat
	u.ubootEnvFileName = "uboot.env"
}

func (u *uboot) processBlOpts(blOpts *Options) {
	if blOpts != nil {
		switch {
		case blOpts.NoSlashBoot, blOpts.Recovery:
			// Recovery or NoSlashBoot imply we use the "boot.sel" simple text
			// format file in /uboot/ubuntu as it exists on the partition
			// directly
			u.basedir = "/uboot/ubuntu/"
			fallthrough
		case blOpts.ExtractedRunKernelImage:
			// if just ExtractedRunKernelImage is defined, we expect to find
			// /boot/uboot/boot.sel
			u.ubootEnvFmt = ubootenv.TextFormat
			u.ubootEnvFileName = "boot.sel"
		}
	}
}

// newUboot create a new Uboot bootloader object
func newUboot(rootdir string, blOpts *Options) ExtractedRecoveryKernelImageBootloader {
	u := &uboot{
		rootdir: rootdir,
	}
	u.setDefaults()
	u.processBlOpts(blOpts)

	if !osutil.FileExists(u.envFile()) {
		return nil
	}

	return u
}

func (u *uboot) Name() string {
	return "uboot"
}

func (u *uboot) setRootDir(rootdir string) {
	u.rootdir = rootdir
}

func (u *uboot) dir() string {
	if u.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(u.rootdir, u.basedir)
}

func (u *uboot) InstallBootConfig(gadgetDir string, blOpts *Options) (bool, error) {
	gadgetFile := filepath.Join(gadgetDir, u.Name()+".conf")
	// if the gadget file is empty, then we don't install anything
	// this is because there are some gadgets, namely the 20 pi gadget right
	// now, that don't use a uboot.env to boot and instead use a boot.scr, and
	// installing a uboot.env file of any form in the root directory will break
	// the boot.scr, so for these setups we just don't install anything
	// TODO:UC20: how can we do this better? maybe parse the file to get the
	//            actual format?
	st, err := os.Stat(gadgetFile)
	if err != nil {
		return false, err
	}
	if st.Size() == 0 {
		// we have a uboot.conf, and hence a uboot bootloader in the gadget, so
		// we don't copy anything from the gadget and instead just install our
		// own boot.sel file
		u.processBlOpts(blOpts)

		err := os.MkdirAll(filepath.Dir(u.envFile()), 0755)
		if err != nil {
			return false, err
		}
		_, err = ubootenv.Create(u.envFile(), u.ubootEnvFmt, 0)
		if err != nil {
			return false, err
		}

		return true, nil
	}

	// InstallBootConfig gets called on a uboot that does not come from newUboot
	// so we need to apply the defaults here
	u.setDefaults()

	if blOpts != nil && blOpts.Recovery {
		// not supported yet, this is traditional uboot.env from gadget
		// TODO:UC20: support this use-case
		return false, fmt.Errorf("internal error: non-empty uboot.env not supported on uc20 yet")
	}

	systemFile := u.ConfigFile()
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (u *uboot) ConfigFile() string {
	return u.envFile()
}

func (u *uboot) envFile() string {
	return filepath.Join(u.dir(), u.ubootEnvFileName)
}

func (u *uboot) SetBootVars(values map[string]string) error {
	env, err := ubootenv.OpenWithFlags(u.envFile(), u.ubootEnvFmt, ubootenv.OpenBestEffort)
	if err != nil {
		return err
	}

	dirty := false
	for k, v := range values {
		// already set to the right value, nothing to do
		if env.Get(k) == v {
			continue
		}
		env.Set(k, v)
		dirty = true
	}

	if dirty {
		return env.Save()
	}

	return nil
}

func (u *uboot) GetBootVars(names ...string) (map[string]string, error) {
	out := map[string]string{}

	env, err := ubootenv.OpenWithFlags(u.envFile(), u.ubootEnvFmt, ubootenv.OpenBestEffort)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (u *uboot) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	dstDir := filepath.Join(u.dir(), s.Filename())
	assets := []string{"kernel.img", "initrd.img", "dtbs/*"}
	return extractKernelAssetsToBootDir(dstDir, snapf, assets)
}

func (u *uboot) ExtractRecoveryKernelAssets(recoverySystemDir string, s snap.PlaceInfo, snapf snap.Container) error {
	if recoverySystemDir == "" {
		return fmt.Errorf("internal error: recoverySystemDir unset")
	}

	recoverySystemUbootKernelAssetsDir := filepath.Join(u.rootdir, recoverySystemDir, "kernel")
	assets := []string{"kernel.img", "initrd.img", "dtbs/*"}
	return extractKernelAssetsToBootDir(recoverySystemUbootKernelAssetsDir, snapf, assets)
}

func (u *uboot) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(u.dir(), s)
}
