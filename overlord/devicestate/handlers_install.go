// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"os/exec"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/timings"
)

const (
	recoveryBootloaderPath = "/run/mnt/ubuntu-seed/EFI/ubuntu"
	recoveryModeBootVar    = "snapd_recovery_mode"
)

func (m *DeviceManager) doCreatePartitions(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := timings.NewForTask(t)
	defer perfTimings.Save(st)

	// get gadget mountpoint
	model, err := findModel(st)
	if err != nil {
		return fmt.Errorf("cannot find model: %v", err)
	}
	info, err := snapstate.CurrentInfo(st, model.GadgetSnap().SnapName())
	if err != nil {
		return fmt.Errorf("cannot get gadget info: %v", err)
	}
	gadgetDir := info.MountDir()
	st.Unlock()

	// determine the block device to install
	// XXX: we're assuming that the gadget has only one volume
	part, err := partitionFromLabel("ubuntu-seed")
	if err != nil {
		return fmt.Errorf("cannot find ubuntu-seed partition: %v", err)
	}
	device, err := diskFromPartition(part)
	if err != nil {
		return fmt.Errorf("cannot determine device to install: %v", err)
	}
	logger.Noticef("Create partitions on %s", device)

	// XXX: we can create partitions internally instead of executing the utility
	output, err := exec.Command("/usr/lib/snapd/snap-bootstrap", "create-partitions", gadgetDir, device).CombinedOutput()
	if err != nil {
		return osutil.OutputErr(output, err)
	}

	// update recovery mode in grubenv
	bl, err := bootloader.Find(recoveryBootloaderPath, nil)
	if err != nil {
		return err
	}
	if err := bl.SetBootVars(map[string]string{recoveryModeBootVar: "run"}); err != nil {
		return fmt.Errorf("cannot update recovery mode: %v", err)
	}

	// reboot the system
	st.Lock()
	st.RequestRestart(state.RestartSystem)

	t.SetStatus(state.DoneStatus)

	return nil
}
