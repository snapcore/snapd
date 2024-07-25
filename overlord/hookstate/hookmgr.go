// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

package hookstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

type hijackFunc func(ctx *Context) error
type hijackKey struct{ hook, snap string }

// HookManager is responsible for the maintenance of hooks in the system state.
// It runs hooks when they're requested, assuming they're present in the given
// snap. Otherwise they're skipped with no error.
type HookManager struct {
	state      *state.State
	repository *repository

	contextsMutex sync.RWMutex
	contexts      map[string]*Context

	hijackMap map[hijackKey]hijackFunc

	runningHooks int32
	runner       *state.TaskRunner
}

// Handler is the interface a client must satify to handle hooks.
type Handler interface {
	// Before is called right before the hook is to be run.
	Before() error

	// Done is called right after the hook has finished successfully.
	Done() error

	// Error is called if the hook encounters an error while running.
	// The returned bool flag indicates if the original hook error should be
	// ignored by hook manager.
	Error(hookErr error) (ignoreHookErr bool, err error)
}

// HandlerGenerator is the function signature required to register for hooks.
type HandlerGenerator func(*Context) Handler

type Preconditioner interface {
	// Precondition is called before the Before method and should return true if
	// the hook should run or false, if it should stop with erroring.
	Precondition() (bool, error)
}

type Multiplexer interface {
	// returns a list of snaps that runHook should run the hook for
	Multiplex() []string
}

// HookSetup is a reference to a hook within a specific snap.
type HookSetup struct {
	Snap     string        `json:"snap"`
	Revision snap.Revision `json:"revision"`
	Hook     string        `json:"hook"`
	Timeout  time.Duration `json:"timeout,omitempty"`

	// Optional is true if we should not error if the script is missing.
	Optional bool `json:"optional,omitempty"`

	// Always is true if we should run the handler even if the script is
	// missing.
	Always bool `json:"always,omitempty"`

	// IgnoreError is true if we should not run the handler's Error() on error.
	IgnoreError bool `json:"ignore-error,omitempty"`

	// Component is the component name that the hook is associated with. If the
	// hook is not associated with a component, the string will be empty.
	Component string `json:"component,omitempty"`

	// ComponentRevision is the revision of the component that the hook is
	// associated with. Only valid if Component is not empty.
	ComponentRevision snap.Revision `json:"component-revision"`
}

// Manager returns a new HookManager.
func Manager(s *state.State, runner *state.TaskRunner) (*HookManager, error) {
	// Make sure we only run 1 hook task for given snap at a time
	runner.AddBlocked(func(thisTask *state.Task, running []*state.Task) bool {
		// check if we're a hook task
		if thisTask.Kind() != "run-hook" {
			return false
		}
		var hooksup HookSetup
		if thisTask.Get("hook-setup", &hooksup) != nil {
			return false
		}
		thisSnapName := hooksup.Snap
		// examine all hook tasks, block thisTask if we find any other hook task affecting same snap
		for _, t := range running {
			if t.Kind() != "run-hook" || t.Get("hook-setup", &hooksup) != nil {
				continue // ignore errors and continue checking remaining tasks
			}
			if hooksup.Snap == thisSnapName {
				// found hook task affecting same snap, block thisTask.
				return true
			}
		}
		return false
	})

	manager := &HookManager{
		state:      s,
		repository: newRepository(),
		contexts:   make(map[string]*Context),
		hijackMap:  make(map[hijackKey]hijackFunc),
		runner:     runner,
	}

	runner.AddHandler("run-hook", manager.doRunHook, manager.undoRunHook)
	// Compatibility with snapd between 2.29 and 2.30 in edge only.
	// We generated a configure-snapd task on core refreshes and
	// for compatibility we need to handle those.
	runner.AddHandler("configure-snapd", func(*state.Task, *tomb.Tomb) error {
		return nil
	}, nil)

	setupHooks(manager)

	snapstate.RegisterAffectedSnapsByAttr("hook-setup", manager.hookAffectedSnaps)

	return manager, nil
}

// Register registers a function to create Handler values whenever hooks
// matching the provided pattern are run.
func (m *HookManager) Register(pattern *regexp.Regexp, generator HandlerGenerator) {
	m.repository.addHandlerGenerator(pattern, generator)
}

// Ensure implements StateManager.Ensure.
func (m *HookManager) Ensure() error {
	return nil
}

// StopHooks kills all currently running hooks and returns after
// that's done.
func (m *HookManager) StopHooks() {
	m.runner.StopKinds("run-hook")
}

func (m *HookManager) hijacked(hookName, instanceName string) hijackFunc {
	return m.hijackMap[hijackKey{hookName, instanceName}]
}

func (m *HookManager) RegisterHijack(hookName, instanceName string, f hijackFunc) {
	if _, ok := m.hijackMap[hijackKey{hookName, instanceName}]; ok {
		panic(fmt.Sprintf("hook %s for snap %s already hijacked", hookName, instanceName))
	}
	m.hijackMap[hijackKey{hookName, instanceName}] = f
}

func (m *HookManager) hookAffectedSnaps(t *state.Task) ([]string, error) {
	var hooksup HookSetup
	if err := t.Get("hook-setup", &hooksup); err != nil {
		return nil, fmt.Errorf("internal error: cannot obtain hook data from task: %s", t.Summary())

	}

	if m.hijacked(hooksup.Hook, hooksup.Snap) != nil {
		// assume being these internal they should not
		// generate conflicts
		return nil, nil
	}

	return []string{hooksup.Snap}, nil
}

func (m *HookManager) ephemeralContext(cookieID string) (context *Context, err error) {
	var contexts map[string]string
	m.state.Lock()
	defer m.state.Unlock()
	err = m.state.Get("snap-cookies", &contexts)
	if err != nil {
		return nil, fmt.Errorf("cannot get snap cookies: %v", err)
	}
	if instanceName, ok := contexts[cookieID]; ok {
		// create new ephemeral cookie
		context, err = NewContext(nil, m.state, &HookSetup{Snap: instanceName}, nil, cookieID)
		return context, err
	}
	return nil, fmt.Errorf("invalid snap cookie requested")
}

// Context obtains the context for the given cookie ID.
func (m *HookManager) Context(cookieID string) (*Context, error) {
	m.contextsMutex.RLock()
	defer m.contextsMutex.RUnlock()

	var err error
	context, ok := m.contexts[cookieID]
	if !ok {
		context, err = m.ephemeralContext(cookieID)
		if err != nil {
			return nil, err
		}
	}

	return context, nil
}

func hookSetup(task *state.Task, key string) (*HookSetup, error) {
	var hooksup HookSetup
	err := task.Get(key, &hooksup)
	if err != nil {
		return nil, err
	}

	return &hooksup, nil
}

// NumRunningHooks returns the number of hooks running at the moment.
func (m *HookManager) NumRunningHooks() int {
	return int(atomic.LoadInt32(&m.runningHooks))
}

// GracefullyWaitRunningHooks waits for currently running hooks to finish up to the default hook timeout. Returns true if there are no more running hooks on exit.
func (m *HookManager) GracefullyWaitRunningHooks() bool {
	toutC := time.After(defaultHookTimeout)
	doWait := true
	for m.NumRunningHooks() > 0 && doWait {
		select {
		case <-time.After(1 * time.Second):
		case <-toutC:
			doWait = false
		}
	}
	return m.NumRunningHooks() == 0
}

// doRunHook actually runs the hook that was requested.
//
// Note that this method is synchronous, as the task is already running in a
// goroutine.
func (m *HookManager) doRunHook(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	hooksup, err := hookSetup(task, "hook-setup")
	task.State().Unlock()
	if err != nil {
		return fmt.Errorf("cannot extract hook setup from task: %s", err)
	}

	return m.runHookForTask(task, tomb, hooksup)
}

// undoRunHook runs the undo-hook that was requested.
//
// Note that this method is synchronous, as the task is already running in a
// goroutine.
func (m *HookManager) undoRunHook(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	hooksup, err := hookSetup(task, "undo-hook-setup")
	task.State().Unlock()
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			// no undo hook setup
			return nil
		}
		return fmt.Errorf("cannot extract undo hook setup from task: %s", err)
	}

	return m.runHookForTask(task, tomb, hooksup)
}

func (m *HookManager) EphemeralRunHook(ctx context.Context, hooksup *HookSetup, contextData map[string]interface{}) (*Context, error) {
	context, err := newEphemeralHookContextWithData(m.state, hooksup, contextData)
	if err != nil {
		return nil, err
	}

	tomb, _ := tomb.WithContext(ctx)
	if err := m.runHook(context, hooksup, tomb); err != nil {
		return nil, err
	}
	return context, nil
}

func (m *HookManager) runHookForTask(task *state.Task, tomb *tomb.Tomb, hooksup *HookSetup) error {
	context, err := NewContext(task, m.state, hooksup, nil, "")
	if err != nil {
		return err
	}
	return m.runHook(context, hooksup, tomb)
}

// runHookGuardForRestarting helps avoiding running a hook if we are
// restarting by returning state.Retry in such case.
func (m *HookManager) runHookGuardForRestarting(context *Context) error {
	context.Lock()
	defer context.Unlock()
	if ok, _ := restart.Pending(m.state); ok {
		return &state.Retry{}
	}

	// keep count of running hooks
	atomic.AddInt32(&m.runningHooks, 1)
	return nil
}

// TODO: docs
func maybeMultiplexHook(context *Context) ([]string, []*snapstate.SnapState, error) {
	context.Lock()
	defer context.Unlock()

	var snaps []string
	if multiplexer, ok := context.Handler().(Multiplexer); ok {
		snaps = multiplexer.Multiplex()
	} else {
		snaps = []string{context.setup.Snap}
	}

	var states []*snapstate.SnapState
	for _, snap := range snaps {
		var snapst snapstate.SnapState
		err := snapstate.Get(context.state, snap, &snapst)
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, nil, fmt.Errorf("cannot handle %q snap: %v", snap, err)
		}

		states = append(states, &snapst)
	}

	return snaps, states, nil
}

// TODO: rm snapstate
func (m *HookManager) runHook(context *Context, hooksup *HookSetup, tomb *tomb.Tomb) error {
	// Obtain a handler for this hook. The repository returns a list since it's
	// possible for regular expressions to overlap, but multiple handlers is an
	// error (as is no handler).
	handlers := m.repository.generateHandlers(context)
	handlersCount := len(handlers)
	if handlersCount == 0 {
		// Do not report error if hook handler doesn't exist as long as the hook is optional.
		// This is to avoid issues when downgrading to an old core snap that doesn't know about
		// particular hook type and a task for it exists (e.g. "post-refresh" hook).
		if hooksup.Optional {
			return nil
		}
		return fmt.Errorf("internal error: no registered handlers for hook %q", hooksup.Hook)
	}
	if handlersCount > 1 {
		return fmt.Errorf("internal error: %d handlers registered for hook %q, expected 1", handlersCount, hooksup.Hook)
	}
	context.handler = handlers[0]

	snaps, snapstates, err := maybeMultiplexHook(context)
	if err != nil {
		return err
	}

	var hookOrHijackExists bool
	hooksExist := make([]bool, len(snaps))
	for i, snapst := range snapstates {
		// for now, we will only support hijacking snap hooks, not component hooks.
		// if we ever add components to the snapd snap, we might need to handle
		// hijacking component hooks as well.
		mustHijack := context.IsSnapHook() && m.hijacked(hooksup.Hook, snaps[i]) != nil

		if !mustHijack {
			// not hijacked, snap must be installed
			if !snapst.IsInstalled() {
				return fmt.Errorf("cannot find %q snap", snaps[i])
			}

			info, err := snapst.CurrentInfo()
			if err != nil {
				return fmt.Errorf("cannot read %q snap details: %v", snaps[i], err)
			}

			if context.IsSnapHook() {
				hooksExist[i] = info.Hooks[hooksup.Hook] != nil
				if !hooksExist[i] && !hooksup.Optional {
					return fmt.Errorf("snap %q has no %q hook", snaps[i], hooksup.Hook)
				}
			} else {
				comp, err := snapst.CurrentComponentInfo(naming.ComponentRef{
					SnapName:      info.SnapName(),
					ComponentName: hooksup.Component,
				})
				if err != nil {
					return fmt.Errorf(`cannot read "%s+%s" component details: %v`, info.SnapName(), hooksup.Component, err)
				}

				hooksExist[i] = comp.Hooks[hooksup.Hook] != nil
				if !hooksExist[i] && !hooksup.Optional {
					return fmt.Errorf(`component "%s+%s" has no %q hook`, info.SnapName(), hooksup.Component, hooksup.Hook)
				}
			}
		}

		// keep track of whether we'll run or hijack at least one hook, so we can
		// count running hooks or bail early if possible
		if mustHijack || hooksExist[i] {
			hookOrHijackExists = true
		}
	}

	if hookOrHijackExists {
		// we will run something, not a noop
		if err := m.runHookGuardForRestarting(context); err != nil {
			return err
		}
		defer atomic.AddInt32(&m.runningHooks, -1)
	} else if !hooksup.Always {
		// a noop with no 'always' flag: bail
		return nil
	}

	contextID := context.ID()
	m.contextsMutex.Lock()
	m.contexts[contextID] = context
	m.contextsMutex.Unlock()

	defer func() {
		m.contextsMutex.Lock()
		delete(m.contexts, contextID)
		m.contextsMutex.Unlock()
	}()

	if ph, ok := context.Handler().(Preconditioner); ok {
		precond, err := ph.Precondition()
		if err != nil {
			return err
		}

		if !precond {
			return nil
		}
	}

	if err := context.Handler().Before(); err != nil {
		return err
	}

	var output []byte
	for i, snap := range snaps {
		// some hooks get hijacked, e.g. the core configuration
		if f := m.hijacked(hooksup.Hook, snap); f != nil {
			err = f(context)
		} else if hooksExist[i] {
			originalSnap := context.setup.Snap
			context.setup.Snap = snap
			output, err = runHook(context, tomb)
			context.setup.Snap = originalSnap
		}

		if err != nil {
			// TODO: telemetry about errors here
			err = osutil.OutputErr(output, err)
			if hooksup.IgnoreError {
				context.Lock()
				context.Errorf("ignoring failure in hook %q: %v", hooksup.Hook, err)
				context.Unlock()
			} else {
				ignoreOriginalErr, handlerErr := context.Handler().Error(err)
				if handlerErr != nil {
					return handlerErr
				}
				if ignoreOriginalErr {
					return nil
				}

				return fmt.Errorf("run hook %q: %v", hooksup.Hook, err)
			}
		}
	}

	if err = context.Handler().Done(); err != nil {
		return err
	}

	context.Lock()
	defer context.Unlock()
	if err = context.Done(); err != nil {
		return err
	}

	return nil
}

func runHookImpl(c *Context, tomb *tomb.Tomb) ([]byte, error) {
	return runHookAndWait(c.HookSource(), c.SnapRevision(), c.HookName(), c.ID(), c.Timeout(), tomb)
}

var runHook = runHookImpl

// MockRunHook mocks the actual invocation of hooks for tests.
func MockRunHook(hookInvoke func(c *Context, tomb *tomb.Tomb) ([]byte, error)) (restore func()) {
	oldRunHook := runHook
	runHook = hookInvoke
	return func() {
		runHook = oldRunHook
	}
}

var osReadlink = os.Readlink

// snapCmd returns the "snap" command to run. If snapd is re-execed
// it will be the snap command from the core snap, otherwise it will
// be the system "snap" command (c.f. LP: #1668738).
func snapCmd() string {
	// sensible default, assume PATH is correct
	snapCmd := "snap"

	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		logger.Noticef("cannot read /proc/self/exe: %v, using default snap command", err)
		return snapCmd
	}
	if !strings.HasPrefix(exe, dirs.SnapMountDir) {
		return snapCmd
	}

	// snap is running from the core snap, we know the relative
	// location of "snap" from "snapd"
	return filepath.Join(filepath.Dir(exe), "../../bin/snap")
}

var defaultHookTimeout = 10 * time.Minute

func runHookAndWait(hookSource string, revision snap.Revision, hookName, hookContext string, timeout time.Duration, tomb *tomb.Tomb) ([]byte, error) {
	argv := []string{snapCmd(), "run", "--hook", hookName, "-r", revision.String(), hookSource}
	if timeout == 0 {
		timeout = defaultHookTimeout
	}

	env := []string{
		// Make sure the hook has its context defined so it can
		// communicate via the REST API.
		fmt.Sprintf("SNAP_COOKIE=%s", hookContext),
		// Set SNAP_CONTEXT too for compatibility with old snapctl
		// binary when transitioning to a new core - otherwise configure
		// hook would fail during transition.
		fmt.Sprintf("SNAP_CONTEXT=%s", hookContext),
	}

	return osutil.RunAndWait(argv, env, timeout, tomb)
}
