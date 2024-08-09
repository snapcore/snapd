// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2024 Canonical Ltd
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

package registrystate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
)

func init() {
	hookstate.ViewChangedHandlerGenerator = func(context *hookstate.Context) hookstate.Handler {
		return &viewChangedHandler{ctx: context}
	}
	hookstate.SaveRegistryHandlerGenerator = func(context *hookstate.Context) hookstate.Handler {
		return &saveRegistryHandler{
			ctx: context,
		}
	}
	hookstate.ChangeRegistryHandlerGenerator = func(context *hookstate.Context) hookstate.Handler {
		return &changeRegistryHandler{
			ctx: context,
		}
	}
}

func setupRegistryHook(st *state.State, snapName, hookName string, ignoreError bool) *state.Task {
	hookSup := &hookstate.HookSetup{
		Snap:        snapName,
		Hook:        hookName,
		Optional:    true,
		IgnoreError: ignoreError,
	}
	summary := fmt.Sprintf(i18n.G("Run hook %s of snap %q"), hookName, snapName)
	task := hookstate.HookTask(st, summary, hookSup, nil)
	return task
}

type viewChangedHandler struct {
	hookstate.SnapHookHandler
	ctx *hookstate.Context
}

func (h *viewChangedHandler) Precondition() (bool, error) {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	// TODO: find all the plugs again? new plugs might've been connected since the
	// previous check when the change is created (possible TOCTOU)

	plugName, _, ok := strings.Cut(h.ctx.HookName(), "-view-changed")
	if !ok || plugName == "" {
		// TODO: add support for manager hooks (e.g., change-registry, save-registry)
		return false, fmt.Errorf("cannot run registry hook handler for unknown hook: %s", h.ctx.HookName())
	}

	repo := ifacerepo.Get(h.ctx.State())
	conns, err := repo.Connected(h.ctx.InstanceName(), plugName)
	if err != nil {
		return false, fmt.Errorf("cannot determine precondition for hook %s: %w", h.ctx.HookName(), err)
	}

	return len(conns) > 0, nil
}

type changeRegistryHandler struct {
	ctx *hookstate.Context
}

// TODO: precondition
func (h *changeRegistryHandler) Before() error { return nil }
func (h *changeRegistryHandler) Error(hookErr error) (ignoreHookErr bool, err error) {
	return false, nil
}

func (h *changeRegistryHandler) Done() error {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()
	tx, _, err := GetTransaction(t)
	if err != nil {
		return fmt.Errorf("cannot get transaction in change-registry handler: %v", err)
	}

	if tx.aborted() {
		return fmt.Errorf("cannot change registry: snap %s rejected changes: %s", tx.AbortingSnap, tx.AbortReason)
	}

	return nil
}

type saveRegistryHandler struct {
	ctx *hookstate.Context
}

// TODO: precondition
func (h *saveRegistryHandler) Before() error { return nil }

// TODO: all of this is now invalid since hooks are no longer multiplexed.
// On the bright side, the new logic will be much simpler
func (h *saveRegistryHandler) Error(origErr error) (ignoreErr bool, err error) {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()
	st := h.ctx.State()

	// we're not failing yet to run the rollback, so we need to manually set
	// the waiting tasks to hold to avoid running them
	for _, t := range t.HaltTasks() {
		// TODO: at the moment there are no other tasks but maybe this should
		// set ALL status in Do to Hold
		t.SetStatus(state.HoldStatus)
	}

	var saveRegErr string
	err = t.Change().Get("save-registry-error", &saveRegErr)
	if err == nil {
		// this is a failed rollback attempt, log the failure but return the original error
		logger.Noticef("rollback attempt failed: %v", origErr)
		return false, errors.New(saveRegErr)
	} else if !errors.Is(err, &state.NoStateError{}) {
		return false, err
	}

	// log the original error with as much information as we could get
	var account, registryName string
	defer func() {
		var extraInfo string
		if account != "" && registryName != "" {
			extraInfo = " of %s/%s"
		}

		if err == nil {
			logger.Noticef("attempting rollback of failed save-registry%s", extraInfo)
		} else {
			logger.Noticef("cannot rollback failed save-registry%s", extraInfo)
		}
	}()

	tx, commitTask, err := GetTransaction(t)
	if err != nil {
		return false, fmt.Errorf("cannot rollback failed save-registry: cannot get transaction: %v", err)
	}

	err = tx.Clear(st)
	if err != nil {
		return false, fmt.Errorf("cannot rollback failed save-registry: cannot clear transaction changes: %v", err)
	}

	commitTask.Set("registry-transaction", tx)
	t.Change().Set("save-registry-error", origErr.Error())

	ignoreError := true
	rollbackTask := setupRegistryHook(st, h.ctx.InstanceName(), "save-registry", ignoreError)
	rollbackTask.WaitFor(t)
	t.Change().AddTask(rollbackTask)

	// register which task is rolling back so we can fail w/ the original error
	// if the rollback succeeds
	t.Change().Set("rollback-task", rollbackTask.ID())

	// ignore error for now so we run again to try to undo any committed data
	return true, nil
}

func (h *saveRegistryHandler) Done() error {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()
	var rollbackTask string
	err := t.Change().Get("rollback-task", &rollbackTask)
	if err != nil && !errors.Is(err, &state.NoStateError{}) {
		return err
	}

	if rollbackTask != t.ID() {
		// this is not the rollback task, do nothing
		return nil
	}

	var saveRegErr string
	err = t.Change().Get("save-registry-error", &saveRegErr)
	if err != nil {
		return err
	}

	// fail with the original error
	logger.Noticef("successfully rolled back failed save-registry")
	return errors.New(saveRegErr)
}

func IsRegistryHook(ctx *hookstate.Context) bool {
	return strings.HasPrefix(ctx.HookName(), "change-registry-") ||
		strings.HasPrefix(ctx.HookName(), "save-registry-") ||
		strings.HasSuffix(ctx.HookName(), "-view-changed")
}
