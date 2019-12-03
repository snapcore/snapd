// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package devicestate

import (
	"context"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/timings"
)

func MockKeyLength(n int) (restore func()) {
	if n < 1024 {
		panic("key length must be >= 1024")
	}

	oldKeyLength := keyLength
	keyLength = n
	return func() {
		keyLength = oldKeyLength
	}
}

func MockBaseStoreURL(url string) (restore func()) {
	oldURL := baseStoreURL
	baseStoreURL = mustParse(url).ResolveReference(authRef)
	return func() {
		baseStoreURL = oldURL
	}
}

func MockRetryInterval(interval time.Duration) (restore func()) {
	old := retryInterval
	retryInterval = interval
	return func() {
		retryInterval = old
	}
}

func MockMaxTentatives(max int) (restore func()) {
	old := maxTentatives
	maxTentatives = max
	return func() {
		maxTentatives = old
	}
}

func MockSysClassBlock(dir string) (restore func()) {
	old := sysClassBlock
	sysClassBlock = dir
	return func() {
		sysClassBlock = old
	}
}

func MockDevBlock(dir string) (restore func()) {
	old := devBlock
	devBlock = dir
	return func() {
		devBlock = old
	}
}

func MockGadgetFindDeviceForStructure(f func(*gadget.LaidOutStructure) (string, error)) (restore func()) {
	old := gadgetFindDeviceForStructure
	gadgetFindDeviceForStructure = f
	return func() {
		gadgetFindDeviceForStructure = old
	}
}

func KeypairManager(m *DeviceManager) asserts.KeypairManager {
	return m.keypairMgr
}

func EnsureOperationalShouldBackoff(m *DeviceManager, now time.Time) bool {
	return m.ensureOperationalShouldBackoff(now)
}

func BecomeOperationalBackoff(m *DeviceManager) time.Duration {
	return m.becomeOperationalBackoff
}

func SetLastBecomeOperationalAttempt(m *DeviceManager, t time.Time) {
	m.lastBecomeOperationalAttempt = t
}

func SetOperatingMode(m *DeviceManager, mode string) {
	m.modeEnv.Mode = mode
}

// XXX: will become properly exported but we probably want to make
//      mode a type and not a string before we do that
func OperatingMode(m *DeviceManager) string {
	return m.operatingMode()
}

func MockRepeatRequestSerial(label string) (restore func()) {
	old := repeatRequestSerial
	repeatRequestSerial = label
	return func() {
		repeatRequestSerial = old
	}
}

func MockSnapstateInstallWithDeviceContext(f func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error)) (restore func()) {
	old := snapstateInstallWithDeviceContext
	snapstateInstallWithDeviceContext = f
	return func() {
		snapstateInstallWithDeviceContext = old
	}
}

func MockSnapstateUpdateWithDeviceContext(f func(st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error)) (restore func()) {
	old := snapstateUpdateWithDeviceContext
	snapstateUpdateWithDeviceContext = f
	return func() {
		snapstateUpdateWithDeviceContext = old
	}
}

var (
	RecoveryBootloaderPath = recoveryBootloaderPath
)

func MockBootstrapCreatePartitions(f func(gadgetDir, device string) error) func() {
	old := bootstrapCreatePartitions
	bootstrapCreatePartitions = f
	return func() {
		bootstrapCreatePartitions = old
	}
}

func MockDiskFromRole(f func(gadgetDir, role string) (string, error)) func() {
	old := diskFromRole
	diskFromRole = f
	return func() {
		diskFromRole = old
	}
}

func MockRecoveryBootloaderPath(p string) func() {
	old := recoveryBootloaderPath
	recoveryBootloaderPath = p
	return func() {
		recoveryBootloaderPath = old
	}
}

func EnsureSeeded(m *DeviceManager) error {
	return m.ensureSeeded()
}

var PopulateStateFromSeedImpl = populateStateFromSeedImpl

type PopulateStateFromSeedOptions = populateStateFromSeedOptions

func MockPopulateStateFromSeed(f func(*state.State, *PopulateStateFromSeedOptions, timings.Measurer) ([]*state.TaskSet, error)) (restore func()) {
	old := populateStateFromSeed
	populateStateFromSeed = f
	return func() {
		populateStateFromSeed = old
	}
}

func EnsureBootOk(m *DeviceManager) error {
	return m.ensureBootOk()
}

func SetBootOkRan(m *DeviceManager, b bool) {
	m.bootOkRan = b
}

type (
	RegistrationContext = registrationContext
	RemodelContext      = remodelContext
)

func RegistrationCtx(m *DeviceManager, t *state.Task) (registrationContext, error) {
	return m.registrationCtx(t)
}

func RemodelDeviceBackend(remodCtx remodelContext) storecontext.DeviceBackend {
	return remodCtx.(interface {
		deviceBackend() storecontext.DeviceBackend
	}).deviceBackend()
}

var (
	ImportAssertionsFromSeed     = importAssertionsFromSeed
	CheckGadgetOrKernel          = checkGadgetOrKernel
	CheckGadgetRemodelCompatible = checkGadgetRemodelCompatible
	CanAutoRefresh               = canAutoRefresh
	NewEnoughProxy               = newEnoughProxy

	IncEnsureOperationalAttempts = incEnsureOperationalAttempts
	EnsureOperationalAttempts    = ensureOperationalAttempts

	RemodelTasks = remodelTasks

	RemodelCtx        = remodelCtx
	CleanupRemodelCtx = cleanupRemodelCtx
	CachedRemodelCtx  = cachedRemodelCtx

	GadgetUpdateBlocked = gadgetUpdateBlocked
	CurrentGadgetInfo   = currentGadgetInfo
	PendingGadgetInfo   = pendingGadgetInfo

	PartitionFromRole = partitionFromRole
	DiskFromPartition = diskFromPartition
)

func MockGadgetUpdate(mock func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc) error) (restore func()) {
	old := gadgetUpdate
	gadgetUpdate = mock
	return func() {
		gadgetUpdate = old
	}
}

func MockGadgetIsCompatible(mock func(current, update *gadget.Info) error) (restore func()) {
	old := gadgetIsCompatible
	gadgetIsCompatible = mock
	return func() {
		gadgetIsCompatible = old
	}
}
