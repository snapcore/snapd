// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"fmt"
	"time"

	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/timeutil"
)

func validateRefreshSchedule(tr Conf) error {
	scheduleSet := false
	defer func() {
		if scheduleSet {
			// give an immediate chance to recompute the
			// next refresh time
			tr.State().EnsureBefore(0)
		}
	}()

	refreshTimerStr, err := coreCfg(tr, "refresh.timer")
	if err != nil {
		return err
	}
	if refreshTimerStr != "" {
		// try legacy refresh.schedule setting if new-style
		// refresh.timer is not set
		if _, err = timeutil.ParseSchedule(refreshTimerStr); err != nil {
			return err
		}
		scheduleSet = true
	}

	refreshHoldStr, err := coreCfg(tr, "refresh.hold")
	if err != nil {
		return err
	}
	if refreshHoldStr != "" {
		if _, err := time.Parse(time.RFC3339, refreshHoldStr); err != nil {
			return fmt.Errorf("refresh.hold cannot be parsed: %v", err)
		}
	}

	refreshScheduleStr, err := coreCfg(tr, "refresh.schedule")
	if err != nil {
		return err
	}
	if refreshScheduleStr == "" {
		return nil
	}

	if refreshScheduleStr == "managed" {
		st := tr.State()
		st.Lock()
		defer st.Unlock()

		if !devicestate.CanManageRefreshes(st) {
			return fmt.Errorf("cannot set schedule to managed")
		}
		scheduleSet = true
		return nil
	}

	_, err = timeutil.ParseLegacySchedule(refreshScheduleStr)
	if err != nil {
		return err
	}

	scheduleSet = true
	return nil
}
