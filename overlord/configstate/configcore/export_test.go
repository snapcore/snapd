// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2022 Canonical Ltd
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

package configcore

import (
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

var (
	UpdatePiConfig       = updatePiConfig
	SwitchHandlePowerKey = switchHandlePowerKey
	SwitchDisableService = switchDisableService
	UpdateKeyValueStream = updateKeyValueStream
	AddFSOnlyHandler     = addFSOnlyHandler
	FilesystemOnlyApply  = filesystemOnlyApply
)

type PlainCoreConfig = plainCoreConfig

type MockConf interface {
	Get(snapName, key string, result interface{}) error
	GetMaybe(snapName, key string, result interface{}) error
	GetPristine(snapName, key string, result interface{}) error
	Set(snapName, key string, value interface{}) error
	Changes() []string
}

// MockRun is used for tests that run also when nomanagers flag is
// set, that is, for config groups that do not need access to the
// state.
func MockRun(dev sysconfig.Device, cfg MockConf) error {
	for _, h := range handlers {
		if err := h.validate(cfg); err != nil {
			return err
		}
	}

	for _, h := range handlers {
		if h.flags().coreOnlyConfig && dev.Classic() {
			continue
		}
		if err := h.handle(dev, cfg, nil); err != nil {
			return err
		}
	}
	return nil
}

func MockFindGid(f func(string) (uint64, error)) func() {
	old := osutilFindGid
	osutilFindGid = f
	return func() {
		osutilFindGid = old
	}
}

func MockChownPath(f func(string, sys.UserID, sys.GroupID) error) func() {
	old := sysChownPath
	sysChownPath = f
	return func() {
		sysChownPath = old
	}
}

func MockEnsureFileState(f func(string, osutil.FileState) error) func() {
	r := testutil.Backup(&osutilEnsureFileState)
	osutilEnsureFileState = f
	return r
}

func MockDirExists(f func(string) (bool, bool, error)) func() {
	r := testutil.Backup(&osutilDirExists)
	osutilDirExists = f
	return r
}

func MockApparmorUpdateHomedirsTunable(f func([]string) error) func() {
	r := testutil.Backup(&apparmorUpdateHomedirsTunable)
	apparmorUpdateHomedirsTunable = f
	return r
}

func MockApparmorReloadAllSnapProfiles(f func() error) func() {
	r := testutil.Backup(&apparmorReloadAllSnapProfiles)
	apparmorReloadAllSnapProfiles = f
	return r
}
