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

package keymgr

import (
	"fmt"
	"regexp"
	"time"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keyring"
	"github.com/snapcore/snapd/secboot/luks2"
)

const (
	// key slot used by the encryption key
	encryptionKeySlot = 0
	// key slot used by the recovery key
	recoveryKeySlot = 1
	// temporary key slot used when changing the encryption key
	tempKeySlot = recoveryKeySlot + 1
)

var (
	sbGetDiskUnlockKeyFromKernel = sb.GetDiskUnlockKeyFromKernel
	keyringAddKeyToUserKeyring   = keyring.AddKeyToUserKeyring
)

func getEncryptionKeyFromUserKeyring(dev string) ([]byte, error) {
	const remove = false
	const defaultPrefix = "ubuntu-fde"
	// note this is the unlock key, which can be either the main key which
	// was unsealed, or the recovery key, in which case some operations may
	// not make sense
	currKey, err := sbGetDiskUnlockKeyFromKernel(defaultPrefix, dev, remove)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain current unlock key for %v: %v", dev, err)
	}
	return currKey, err
}

func isKeyslotNotActive(err error) bool {
	match, _ := regexp.MatchString(`.*: Keyslot [0-9]+ is not active`, err.Error())
	return match
}

func sbKDFToLuksKDF(o *sb.KDFOptions) luks2.KDFOptions {
	return luks2.KDFOptions{
		TargetDuration:  o.TargetDuration,
		MemoryKiB:       o.MemoryKiB,
		ForceIterations: o.ForceIterations,
		Parallel:        o.Parallel,
	}
}

// AddRecoveryKeyToLUKSDevice adds a recovery key to a LUKS2 device. It the
// devuce unlock key from the user keyring to authorize the change. The
// recoveyry key is added to keyslot 1.
func AddRecoveryKeyToLUKSDevice(dev string, recoveryKey secboot.RecoveryKey) error {
	opts, err := secboot.RecoveryKDF()
	if err != nil {
		return err
	}

	currKey, err := getEncryptionKeyFromUserKeyring(dev)
	if err != nil {
		return err
	}

	if err := luks2.KillSlot(dev, recoveryKeySlot, currKey[:]); err != nil {
		if !isKeyslotNotActive(err) {
			return fmt.Errorf("cannot kill existing slot: %v", err)
		}
	}
	// TODO: fixup options?
	options := luks2.AddKeyOptions{
		KDFOptions: sbKDFToLuksKDF(opts),
		Slot:       recoveryKeySlot,
	}
	if err := luks2.AddKey(dev, currKey, recoveryKey[:], &options); err != nil {
		return fmt.Errorf("cannot add key: %w", err)
	}

	if err := luks2.SetSlotPriority(dev, encryptionKeySlot, luks2.SlotPriorityHigh); err != nil {
		return fmt.Errorf("cannot change keyslot priority: %w", err)
	}

	return nil
}

// RemoveRecoveryKeyFromLUKSDevice removes an existing recovery key a LUKS2
// device.
func RemoveRecoveryKeyFromLUKSDevice(dev string) error {
	// TODO: just remove the key we think is a recovery key (luks keyslot 1)
	currKey, err := getEncryptionKeyFromUserKeyring(dev)
	if err != nil {
		return err
	}
	if err := luks2.KillSlot(dev, recoveryKeySlot, currKey); err != nil {
		if !isKeyslotNotActive(err) {
			return fmt.Errorf("cannot kill recovery key slot: %v", err)
		}
	}
	return nil
}

// ChangeLUKSDeviceEncryptionKey changes the main encryption key of the device.
// Uses an existing unlock key of that device, which is present in the kernel
// user keyring. Once complete the user keyring contains the new encryption key.
func ChangeLUKSDeviceEncryptionKey(dev string, newKey secboot.EncryptionKey) error {
	if len(newKey) != secboot.EncryptionKeySize {
		return fmt.Errorf("cannot use a key of size different than %v", secboot.EncryptionKeySize)
	}

	// TODO: just remove the key we think is a recovery key (luks keyslot 1)
	currKey, err := getEncryptionKeyFromUserKeyring(dev)
	if err != nil {
		return err
	}

	// we only have the current key, we cannot add a key to an occupied
	// keyslot, and cannot start with killing its keyslot as that would make
	// the device unusable, so instead add the new key to an auxiliary
	// keyslot, then use the new key to authorize removal of keyslot 0
	// (which refers to the old key), add the new key again, but this time
	// to keyslot 0, lastly kill the aux keyslot

	if err := luks2.KillSlot(dev, tempKeySlot, currKey); err != nil {
		if !isKeyslotNotActive(err) {
			return fmt.Errorf("cannot kill the temporary keyslot: %v", err)
		}
	}

	options := luks2.AddKeyOptions{
		KDFOptions: luks2.KDFOptions{TargetDuration: 100 * time.Millisecond},
		Slot:       tempKeySlot,
	}
	if err := luks2.AddKey(dev, currKey[:], newKey, &options); err != nil {
		return fmt.Errorf("cannot add key: %w", err)
	}

	// now it should be possible to kill the original keyslot by using the
	// new key for authorization
	if err := luks2.KillSlot(dev, encryptionKeySlot, newKey); err != nil {
		if !isKeyslotNotActive(err) {
			return fmt.Errorf("cannot kill existing slot: %w", err)
		}
	}
	options.Slot = encryptionKeySlot
	// add the new key to keyslot 0
	if err := luks2.AddKey(dev, newKey, newKey, &options); err != nil {
		return fmt.Errorf("cannot add key: %w", err)
	}
	// and kill the aux slot
	if err := luks2.KillSlot(dev, tempKeySlot, newKey); err != nil {
		return fmt.Errorf("cannot kill aux slot: %w", err)
	}
	// TODO needed?
	if err := luks2.SetSlotPriority(dev, encryptionKeySlot, luks2.SlotPriorityHigh); err != nil {
		return fmt.Errorf("cannot change keyslot priority: %w", err)
	}

	// XXX what about aux key?
	const keyringPurposeDiskUnlock = "unlock"
	const keyringPrefix = "ubuntu-fde"
	// TODO: make the key permanent in the keyring
	if err := keyringAddKeyToUserKeyring(newKey, dev, keyringPurposeDiskUnlock, keyringPrefix); err != nil {
		return fmt.Errorf("cannot add key to user keyring: %v", err)
	}

	// TODO: update the keyring?
	return nil
}
