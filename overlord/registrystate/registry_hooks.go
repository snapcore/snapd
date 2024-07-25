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
			MultiplexHandler: hookstate.MultiplexHandler{
				Ctx: context,
			},
		}
	}
	hookstate.ChangeRegistryHandlerGenerator = func(context *hookstate.Context) hookstate.Handler {
		return &changeRegistryHandler{
			MultiplexHandler: hookstate.MultiplexHandler{
				Ctx: context,
			},
		}
	}
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
	hookstate.MultiplexHandler
}

func (h *changeRegistryHandler) Done() error {
	h.Ctx.Lock()
	defer h.Ctx.Unlock()

	t, _ := h.Ctx.Task()
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
	hookstate.MultiplexHandler
}

func (h *saveRegistryHandler) Error(origErr error) (ignoreErr bool, err error) {
	h.Ctx.Lock()
	defer h.Ctx.Unlock()

	t, _ := h.Ctx.Task()
	st := h.Ctx.State()

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
	rollbackTask := setupRegistryHook(st, h.Ctx.InstanceName(), "save-registry", ignoreError)
	rollbackTask.WaitFor(t)
	t.Change().AddTask(rollbackTask)

	// register which task is rolling back so we can fail w/ the original error
	// if the rollback succeeds
	t.Change().Set("rollback-task", rollbackTask.ID())

	// ignore error for now so we run again to try to undo any committed data
	return true, nil
}

func (h *saveRegistryHandler) Done() error {
	h.Ctx.Lock()
	defer h.Ctx.Unlock()

	t, _ := h.Ctx.Task()
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
