// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2023 Canonical Ltd
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

// Package install implements installation logic details for UC20+ systems.  It
// is meant for use by overlord/devicestate and the single-reboot installation
// code in snap-bootstrap.
package install

import (
	"bytes"
	"crypto"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/timings"
)

// EncryptionSupportInfo describes what encryption is available and needed
// for the current device.
type EncryptionSupportInfo struct {
	// Disabled is set to true if encryption was forcefully
	// disabled (e.g. via the seed partition), if set the rest
	// of the struct content is not relevant.
	Disabled bool

	// StorageSafety describes the level safety properties
	// requested by the model
	StorageSafety asserts.StorageSafety
	// Available is set to true if encryption is available on this device
	// with the used gadget.
	Available bool

	// Type is set to the EncryptionType that can be used if
	// Available is true.
	Type secboot.EncryptionType

	// UnvailableErr is set if the encryption support availability of
	// the this device and used gadget do not match the
	// storage safety requirements.
	UnavailableErr error
	// UnavailbleWarning describes why encryption support is not
	// available in case it is optional.
	UnavailableWarning string
}

var (
	timeNow = time.Now

	secbootCheckTPMKeySealingSupported = secboot.CheckTPMKeySealingSupported
	sysconfigConfigureTargetSystem     = sysconfig.ConfigureTargetSystem
)

// MockSecbootCheckTPMKeySealingSupported mocks secboot.CheckTPMKeySealingSupported usage by the package for testing.
func MockSecbootCheckTPMKeySealingSupported(f func(tpmMode secboot.TPMProvisionMode) error) (restore func()) {
	old := secbootCheckTPMKeySealingSupported
	secbootCheckTPMKeySealingSupported = f
	return func() {
		secbootCheckTPMKeySealingSupported = old
	}
}

// GetEncryptionSupportInfo returns the encryption support information
// for the given model, TPM provision mode, kernel and gadget information and
// system hardware. It uses runSetupHook to invoke the kernel fde-setup hook if
// any is available, leaving the caller to decide how, based on the environment.
func GetEncryptionSupportInfo(model *asserts.Model, tpmMode secboot.TPMProvisionMode, kernelInfo *snap.Info, gadgetInfo *gadget.Info, runSetupHook fde.RunSetupHookFunc) (EncryptionSupportInfo, error) {
	secured := model.Grade() == asserts.ModelSecured
	dangerous := model.Grade() == asserts.ModelDangerous
	encrypted := model.StorageSafety() == asserts.StorageSafetyEncrypted

	res := EncryptionSupportInfo{
		StorageSafety: model.StorageSafety(),
	}

	// check if we should disable encryption non-secured devices
	// TODO:UC20: this is not the final mechanism to bypass encryption
	if dangerous && osutil.FileExists(filepath.Join(boot.InitramfsUbuntuSeedDir, ".force-unencrypted")) {
		res.Disabled = true
		return res, nil
	}

	// check encryption: this can either be provided by the fde-setup
	// hook mechanism or by the built-in secboot based encryption
	checkFDESetupHookEncryption := hasFDESetupHookInKernel(kernelInfo)
	// Note that having a fde-setup hook will disable the internal
	// secboot based encryption
	checkSecbootEncryption := !checkFDESetupHookEncryption
	var checkEncryptionErr error
	switch {
	case checkFDESetupHookEncryption:
		res.Type, checkEncryptionErr = checkFDEFeatures(runSetupHook)
	case checkSecbootEncryption:
		checkEncryptionErr = secbootCheckTPMKeySealingSupported(tpmMode)
		if checkEncryptionErr == nil {
			res.Type = secboot.EncryptionTypeLUKS
		}
	default:
		return res, fmt.Errorf("internal error: no encryption checked in encryptionSupportInfo")
	}
	res.Available = (checkEncryptionErr == nil)

	if checkEncryptionErr != nil {
		switch {
		case secured:
			res.UnavailableErr = fmt.Errorf("cannot encrypt device storage as mandated by model grade secured: %v", checkEncryptionErr)
		case encrypted:
			res.UnavailableErr = fmt.Errorf("cannot encrypt device storage as mandated by encrypted storage-safety model option: %v", checkEncryptionErr)
		case checkFDESetupHookEncryption:
			res.UnavailableWarning = fmt.Sprintf("not encrypting device storage as querying kernel fde-setup hook did not succeed: %v", checkEncryptionErr)
		case checkSecbootEncryption:
			res.UnavailableWarning = fmt.Sprintf("not encrypting device storage as checking TPM gave: %v", checkEncryptionErr)
		default:
			return res, fmt.Errorf("internal error: checkEncryptionErr is set but not handled by the code")
		}
	}

	// If encryption is available check if the gadget is
	// compatible with encryption.
	if res.Available {
		opts := &gadget.ValidationConstraints{
			EncryptedData: true,
		}
		mylog.Check(gadget.Validate(gadgetInfo, model, opts))

	}

	return res, nil
}

func hasFDESetupHookInKernel(kernelInfo *snap.Info) bool {
	_, ok := kernelInfo.Hooks["fde-setup"]
	return ok
}

func checkFDEFeatures(runSetupHook fde.RunSetupHookFunc) (et secboot.EncryptionType, err error) {
	// Run fde-setup hook with "op":"features". If the hook
	// returns any {"features":[...]} reply we consider the
	// hardware supported. If the hook errors or if it returns
	// {"error":"hardware-unsupported"} we don't.
	features := mylog.Check2(fde.CheckFeatures(runSetupHook))

	switch {
	case strutil.ListContains(features, "inline-crypto-engine"):
		et = secboot.EncryptionTypeLUKSWithICE
	default:
		et = secboot.EncryptionTypeLUKS
	}

	return et, nil
}

// CheckEncryptionSupport checks the type of encryption support for disks
// available if any and returns the corresponding secboot.EncryptionType,
// internally it uses GetEncryptionSupportInfo with the provided parameters.
func CheckEncryptionSupport(model *asserts.Model, tpmMode secboot.TPMProvisionMode, kernelInfo *snap.Info, gadgetInfo *gadget.Info, runSetupHook fde.RunSetupHookFunc) (secboot.EncryptionType, error) {
	res := mylog.Check2(GetEncryptionSupportInfo(model, tpmMode, kernelInfo, gadgetInfo, runSetupHook))

	if res.UnavailableWarning != "" {
		logger.Noticef("%s", res.UnavailableWarning)
	}
	// encryption disabled or preferred unencrypted: follow the model preferences here even if encryption would be available
	if res.Disabled || res.StorageSafety == asserts.StorageSafetyPreferUnencrypted {
		res.Type = secboot.EncryptionTypeNone
	}

	return res.Type, res.UnavailableErr
}

// BuildInstallObserver creates an observer for gadget assets if
// applicable, otherwise the returned gadget.ContentObserver is nil.
// The observer if any is also returned as non-nil trustedObserver if
// encryption is in use.
func BuildInstallObserver(model *asserts.Model, gadgetDir string, useEncryption bool) (
	observer gadget.ContentObserver, trustedObserver *boot.TrustedAssetsInstallObserver, err error,
) {
	// observer will be a nil interface by default
	trustedObserver = mylog.Check2(boot.TrustedAssetsInstallObserverForModel(model, gadgetDir, useEncryption))
	if err != nil && err != boot.ErrObserverNotApplicable {
		return nil, nil, fmt.Errorf("cannot setup asset install observer: %v", err)
	}
	if err == nil {
		observer = trustedObserver
		if !useEncryption {
			// there will be no key sealing, so past the
			// installation pass no other methods need to be called
			trustedObserver = nil
		}
	}

	return observer, trustedObserver, nil
}

// PrepareEncryptedSystemData executes preparations related to encrypted system data:
// * provides trustedInstallObserver with the chosen keys
// * uses trustedInstallObserver to track any trusted assets in ubuntu-seed
// * save keys and markers for ubuntu-data being able to safely open ubuntu-save
func PrepareEncryptedSystemData(model *asserts.Model, keyForRole map[string]keys.EncryptionKey, trustedInstallObserver *boot.TrustedAssetsInstallObserver) error {
	// validity check
	if len(keyForRole) == 0 || keyForRole[gadget.SystemData] == nil || keyForRole[gadget.SystemSave] == nil {
		return fmt.Errorf("internal error: system encryption keys are unset")
	}
	dataEncryptionKey := keyForRole[gadget.SystemData]
	saveEncryptionKey := keyForRole[gadget.SystemSave]

	// make note of the encryption keys
	trustedInstallObserver.ChosenEncryptionKeys(dataEncryptionKey, saveEncryptionKey)
	mylog.Check(

		// XXX is the asset cache problematic from initramfs?
		// keep track of recovery assets
		trustedInstallObserver.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir))
	mylog.Check(saveKeys(model, keyForRole))
	mylog.Check(

		// write markers containing a secret to pair data and save
		writeMarkers(model))

	return nil
}

// writeMarkers writes markers containing the same secret to pair data and save.
func writeMarkers(model *asserts.Model) error {
	mylog.Check(
		// ensure directory for markers exists
		os.MkdirAll(boot.InstallHostFDEDataDir(model), 0755))
	mylog.Check(os.MkdirAll(boot.InstallHostFDESaveDir, 0755))

	// generate a secret random marker
	markerSecret := mylog.Check2(randutil.CryptoTokenBytes(32))

	return device.WriteEncryptionMarkers(boot.InstallHostFDEDataDir(model), boot.InstallHostFDESaveDir, markerSecret)
}

func saveKeys(model *asserts.Model, keyForRole map[string]keys.EncryptionKey) error {
	saveEncryptionKey := keyForRole[gadget.SystemSave]
	if saveEncryptionKey == nil {
		// no system-save support
		return nil
	}
	mylog.Check(
		// ensure directory for keys exists
		os.MkdirAll(boot.InstallHostFDEDataDir(model), 0755))
	mylog.Check(saveEncryptionKey.Save(device.SaveKeyUnder(boot.InstallHostFDEDataDir(model))))

	return nil
}

// PrepareRunSystemData prepares the run system:
// * it writes the model to ubuntu-boot
// * sets up/copies any allowed and relevant cloud init configuration
// * plus other details
func PrepareRunSystemData(model *asserts.Model, gadgetDir string, perfTimings timings.Measurer) error {
	mylog.
		// keep track of the model we installed
		Check(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755))
	mylog.Check(writeModel(model, filepath.Join(boot.InitramfsUbuntuBootDir, "device/model")))
	mylog.Check(

		// XXX does this make sense from initramfs?
		// preserve systemd-timesyncd clock timestamp, so that RTC-less devices
		// can start with a more recent time on the next boot
		writeTimesyncdClock(dirs.GlobalRootDir, boot.InstallHostWritableDir(model)))

	// configure the run system
	opts := &sysconfig.Options{TargetRootDir: boot.InstallHostWritableDir(model), GadgetDir: gadgetDir}
	// configure cloud init
	setSysconfigCloudOptions(opts, gadgetDir, model)
	timings.Run(perfTimings, "sysconfig-configure-target-system", "Configure target system", func(timings.Measurer) {
		mylog.Check(sysconfigConfigureTargetSystem(model, opts))
	})

	// TODO: FIXME: this should go away after we have time to design a proper
	//              solution

	if !model.Classic() {
		mylog.Check(
			// on some specific devices, we need to create these directories in
			// _writable_defaults in order to allow the install-device hook to install
			// some files there, this eventually will go away when we introduce a proper
			// mechanism not using system-files to install files onto the root
			// filesystem from the install-device hook
			fixupWritableDefaultDirs(boot.InstallHostWritableDir(model)))
	}

	return nil
}

func writeModel(model *asserts.Model, where string) error {
	f := mylog.Check2(os.OpenFile(where, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644))

	defer f.Close()
	return asserts.NewEncoder(f).Encode(model)
}

func setSysconfigCloudOptions(opts *sysconfig.Options, gadgetDir string, model *asserts.Model) {
	ubuntuSeedCloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")

	grade := model.Grade()

	// we always set the cloud-init src directory if it exists, it is
	// automatically ignored by sysconfig in the case it shouldn't be used
	if osutil.IsDirectory(ubuntuSeedCloudCfg) {
		opts.CloudInitSrcDir = ubuntuSeedCloudCfg
	}

	switch {
	// if the gadget has a cloud.conf file, always use that regardless of grade
	case sysconfig.HasGadgetCloudConf(gadgetDir):
		opts.AllowCloudInit = true

	// next thing is if are in secured grade and didn't have gadget config, we
	// disable cloud-init always, clouds should have their own config via
	// gadgets for grade secured
	case grade == asserts.ModelSecured:
		opts.AllowCloudInit = false

	// all other cases we allow cloud-init to run, either through config that is
	// available at runtime via a CI-DATA USB drive, or via config on
	// ubuntu-seed if that is allowed by the model grade, etc.
	default:
		opts.AllowCloudInit = true
	}
}

func fixupWritableDefaultDirs(systemDataDir string) error {
	// the _writable_default directory is used to put files in place on
	// ubuntu-data from install mode, so we abuse it here for a specific device
	// to let that device install files with system-files and the install-device
	// hook

	// eventually this will be a proper, supported, designed mechanism instead
	// of just this hack, but this hack is just creating the directories, since
	// the system-files interface only allows creating the file, not creating
	// the directories leading up to that file, and since the file is deeply
	// nested we would effectively have to give all permission to the device
	// to create any file on ubuntu-data which we don't want to do, so we keep
	// this restriction to let the device create one specific file, and then
	// we behind the scenes just create the directories for the device

	for _, subDirToCreate := range []string{"/etc/udev/rules.d", "/etc/modprobe.d", "/etc/modules-load.d/", "/etc/systemd/network"} {
		dirToCreate := sysconfig.WritableDefaultsDir(systemDataDir, subDirToCreate)
		mylog.Check(os.MkdirAll(dirToCreate, 0755))

	}

	return nil
}

func writeTimesyncdClock(srcRootDir, dstRootDir string) error {
	// keep track of the time
	const timesyncClockInRoot = "/var/lib/systemd/timesync/clock"
	clockSrc := filepath.Join(srcRootDir, timesyncClockInRoot)
	clockDst := filepath.Join(dstRootDir, timesyncClockInRoot)
	mylog.Check(os.MkdirAll(filepath.Dir(clockDst), 0755))

	if !osutil.FileExists(clockSrc) {
		logger.Noticef("timesyncd clock timestamp %v does not exist", clockSrc)
		return nil
	}
	mylog.Check(
		// clock file is owned by a specific user/group, thus preserve
		// attributes of the source
		osutil.CopyFile(clockSrc, clockDst, osutil.CopyFlagPreserveAll))
	mylog.Check(

		// the file is empty however, its modification timestamp is used to set
		// up the current time
		os.Chtimes(clockDst, timeNow(), timeNow()))

	return nil
}

// ApplyPreseededData applies the preseed payload from the given seed, including
// installing snaps, to the given target system filesystem.
func ApplyPreseededData(preseedSeed seed.PreseedCapable, writableDir string) error {
	preseedAs := mylog.Check2(preseedSeed.LoadPreseedAssertion())

	preseedArtifact := preseedSeed.ArtifactPath("preseed.tgz")

	// TODO: consider a writer that feeds the file to stdin of tar and calculates the digest at the same time.
	sha3_384, _ := mylog.Check3(osutil.FileDigest(preseedArtifact, crypto.SHA3_384))

	digest := mylog.Check2(base64.RawURLEncoding.DecodeString(preseedAs.ArtifactSHA3_384()))

	if !bytes.Equal(sha3_384, digest) {
		return fmt.Errorf("invalid preseed artifact digest")
	}

	logger.Noticef("apply preseed data: %q, %q", writableDir, preseedArtifact)
	cmd := exec.Command("tar", "--extract", "--preserve-permissions", "--preserve-order", "--gunzip", "--directory", writableDir, "-f", preseedArtifact)
	mylog.Check(cmd.Run())

	logger.Noticef("copying snaps")
	mylog.Check(os.MkdirAll(filepath.Join(writableDir, "var/lib/snapd/snaps"), 0755))

	tm := timings.New(nil)
	snapHandler := &preseedSnapHandler{writableDir: writableDir}
	mylog.Check(preseedSeed.LoadMeta("run", snapHandler, tm))

	preseedSnaps := make(map[string]*asserts.PreseedSnap)
	for _, ps := range preseedAs.Snaps() {
		preseedSnaps[ps.Name] = ps
	}

	checkSnap := func(ssnap *seed.Snap) error {
		ps, ok := preseedSnaps[ssnap.SnapName()]
		if !ok {
			return fmt.Errorf("snap %q not present in the preseed assertion", ssnap.SnapName())
		}
		if ps.Revision != ssnap.SideInfo.Revision.N {
			rev := snap.Revision{N: ps.Revision}
			return fmt.Errorf("snap %q has wrong revision %s (expected: %s)", ssnap.SnapName(), ssnap.SideInfo.Revision, rev)
		}
		if ps.SnapID != ssnap.SideInfo.SnapID {
			return fmt.Errorf("snap %q has wrong snap id %q (expected: %q)", ssnap.SnapName(), ssnap.SideInfo.SnapID, ps.SnapID)
		}
		return nil
	}

	esnaps := preseedSeed.EssentialSnaps()
	msnaps := mylog.Check2(preseedSeed.ModeSnaps("run"))

	if len(msnaps)+len(esnaps) != len(preseedSnaps) {
		return fmt.Errorf("seed has %d snaps but %d snaps are required by preseed assertion", len(msnaps)+len(esnaps), len(preseedSnaps))
	}

	for _, esnap := range esnaps {
		mylog.Check(checkSnap(esnap))
	}

	for _, ssnap := range msnaps {
		mylog.Check(checkSnap(ssnap))
	}

	return nil
}

// TODO: consider reusing this kind of handler for UC20 seeding
type preseedSnapHandler struct {
	writableDir string
}

func (p *preseedSnapHandler) HandleUnassertedSnap(name, path string, _ timings.Measurer) (string, error) {
	pinfo := snap.MinimalPlaceInfo(name, snap.Revision{N: -1})
	targetPath := filepath.Join(p.writableDir, pinfo.MountFile())
	mountDir := filepath.Join(p.writableDir, pinfo.MountDir())

	sq := squashfs.New(path)
	opts := &snap.InstallOptions{MustNotCrossDevices: true}
	mylog.Check2(sq.Install(targetPath, mountDir, opts))

	return targetPath, nil
}

func (p *preseedSnapHandler) HandleAndDigestAssertedSnap(name, path string, essType snap.Type, snapRev *asserts.SnapRevision, _ func(string, uint64) (snap.Revision, error), _ timings.Measurer) (string, string, uint64, error) {
	pinfo := snap.MinimalPlaceInfo(name, snap.Revision{N: snapRev.SnapRevision()})
	targetPath := filepath.Join(p.writableDir, pinfo.MountFile())
	mountDir := filepath.Join(p.writableDir, pinfo.MountDir())

	logger.Debugf("copying: %q to %q; mount dir=%q", path, targetPath, mountDir)

	srcFile := mylog.Check2(os.Open(path))

	defer srcFile.Close()

	destFile := mylog.Check2(osutil.NewAtomicFile(targetPath, 0644, 0, osutil.NoChown, osutil.NoChown))

	defer destFile.Cancel()

	finfo := mylog.Check2(srcFile.Stat())

	destFile.SetModTime(finfo.ModTime())

	h := crypto.SHA3_384.New()
	w := io.MultiWriter(h, destFile)

	size := mylog.Check2(io.CopyBuffer(w, srcFile, make([]byte, 2*1024*1024)))
	mylog.Check(destFile.Commit())

	sq := squashfs.New(targetPath)
	opts := &snap.InstallOptions{MustNotCrossDevices: true}
	mylog.Check2(
		// since Install target path is the same as source path passed to squashfs.New,
		// Install isn't going to copy the blob, but we call it to set up mount directory etc.
		sq.Install(targetPath, mountDir, opts))

	sha3_384 := mylog.Check2(asserts.EncodeDigest(crypto.SHA3_384, h.Sum(nil)))

	return targetPath, sha3_384, uint64(size), nil
}
