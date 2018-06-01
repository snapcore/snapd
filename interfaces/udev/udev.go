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

package udev

import (
	"fmt"
	"os/exec"
)

// ReloadRules runs three commands that reload udev rule database.
//
// The commands are: udevadm control --reload-rules
//                   udevadm trigger --subsystem-nomatch=input
//                   udevadm trigger --property-match=ID_INPUT_JOYSTICK=1
// and optionally a fourth:
//                   udevadm trigger --subsystem-match=input
func ReloadRules(subsystemTrigger string) error {
	output, err := exec.Command("udevadm", "control", "--reload-rules").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot reload udev rules: %s\nudev output:\n%s", err, string(output))
	}

	// By default, trigger for all events except the input subsystem since
	// it can cause noticeable blocked input on, for example, classic desktop.
	output, err = exec.Command("udevadm", "trigger", "--subsystem-nomatch=input").CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot run udev triggers: %s\nudev output:\n%s", err, string(output))
	}

	// FIXME: also trigger the joystick property when subsystemTrigger is
	// empty since we are not able to detect interfaces that are removed
	// and set subsystemTrigger correctly. When we can, remove the
	// '|| subsystemTrigger == ""' check. This allows joysticks to be
	// removed from the device cgroup on interface disconnect.
	if subsystemTrigger == "input/joystick" || subsystemTrigger == "" {
		// If one of the interfaces said it uses the input subsystem
		// for joysticks, then trigger the joystick events in a way
		// that is specific to joysticks to not block other inputs.
		output, err = exec.Command("udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1").CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot run udev triggers for joysticks: %s\nudev output:\n%s", err, string(output))
		}
	} else if subsystemTrigger != "" {
		// If one of the interfaces said it uses a subsystem, then do
		// it too.
		output, err = exec.Command("udevadm", "trigger", "--subsystem-match="+subsystemTrigger).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot run udev triggers for %s subsystem: %s\nudev output:\n%s", subsystemTrigger, err, string(output))
		}
	}
	return nil
}
