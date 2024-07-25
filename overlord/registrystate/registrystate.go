// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023-2024 Canonical Ltd
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
	"sort"
	"strings"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var assertstateRegistry = assertstate.Registry

// SetViaView finds the view identified by the account, registry and view names
// and sets the request fields to their respective values.
func SetViaView(st *state.State, account, registryName, viewName string, requests map[string]interface{}) error {
	registryAssert, err := assertstateRegistry(st, account, registryName)
	if err != nil {
		return err
	}
	reg := registryAssert.Registry()

	view := reg.View(viewName)
	if view == nil {
		var keys []string
		if len(requests) > 0 {
			keys = make([]string, 0, len(requests))
			for k := range requests {
				keys = append(keys, k)
			}
		}

		return &registry.NotFoundError{
			Account:      account,
			RegistryName: registryName,
			View:         viewName,
			Operation:    "set",
			Requests:     keys,
			Cause:        "not found",
		}
	}

	tx, err := NewTransaction(st, reg.Account, reg.Name)
	if err != nil {
		return err
	}

	if err = SetViaViewInTx(tx, view, requests); err != nil {
		return err
	}

	return tx.Commit(st, reg.Schema)
}

// SetViaViewInTx uses the view to set the requests in the transaction's databag.
func SetViaViewInTx(tx *Transaction, view *registry.View, requests map[string]interface{}) error {
	for field, value := range requests {
		var err error
		if value == nil {
			err = view.Unset(tx, field)
		} else {
			err = view.Set(tx, field, value)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// GetViaView finds the view identified by the account, registry and view names
// and uses it to get the values for the specified fields. The results are
// returned in a map of fields to their values, unless there are no fields in
// which case all views are returned.
func GetViaView(st *state.State, account, registryName, viewName string, fields []string) (interface{}, error) {
	registryAssert, err := assertstateRegistry(st, account, registryName)
	if err != nil {
		return nil, err
	}
	reg := registryAssert.Registry()

	view := reg.View(viewName)
	if view == nil {
		return nil, &registry.NotFoundError{
			Account:      account,
			RegistryName: registryName,
			View:         viewName,
			Operation:    "get",
			Requests:     fields,
			Cause:        "not found",
		}
	}

	tx, err := NewTransaction(st, reg.Account, reg.Name)
	if err != nil {
		return nil, err
	}

	return GetViaViewInTx(tx, view, fields)
}

// GetViaViewInTx uses the view to get values for the fields from the databag
// in the transaction.
func GetViaViewInTx(tx *Transaction, view *registry.View, fields []string) (interface{}, error) {
	if len(fields) == 0 {
		val, err := view.Get(tx, "")
		if err != nil {
			return nil, err
		}

		return val, nil
	}

	results := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		value, err := view.Get(tx, field)
		if err != nil {
			if errors.Is(err, &registry.NotFoundError{}) && len(fields) > 1 {
				// keep looking; return partial result if only some fields are found
				continue
			}

			return nil, err
		}

		results[field] = value
	}

	if len(results) == 0 {
		account, registryName := tx.RegistryInfo()
		return nil, &registry.NotFoundError{
			Account:      account,
			RegistryName: registryName,
			View:         view.Name,
			Operation:    "get",
			Requests:     fields,
			Cause:        "matching rules don't map to any values",
		}
	}

	return results, nil
}

var readDatabag = func(st *state.State, account, registryName string) (registry.JSONDataBag, error) {
	var databags map[string]map[string]registry.JSONDataBag
	if err := st.Get("registry-databags", &databags); err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return registry.NewJSONDataBag(), nil
		}
		return nil, err
	}

	if databags[account] == nil || databags[account][registryName] == nil {
		return registry.NewJSONDataBag(), nil
	}

	return databags[account][registryName], nil
}

var writeDatabag = func(st *state.State, databag registry.JSONDataBag, account, registryName string) error {
	var databags map[string]map[string]registry.JSONDataBag
	err := st.Get("registry-databags", &databags)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	} else if errors.Is(err, &state.NoStateError{}) || databags[account] == nil || databags[account][registryName] == nil {
		databags = map[string]map[string]registry.JSONDataBag{
			account: {registryName: registry.NewJSONDataBag()},
		}
	}

	databags[account][registryName] = databag
	st.Set("registry-databags", databags)
	return nil
}

type cachedRegistryTx struct {
	account  string
	registry string
}

// RegistryTransaction returns the registry.Transaction cached in the context
// or creates one and caches it, if none existed. The context must be locked by
// the caller.
func RegistryTransaction(ctx *hookstate.Context, reg *registry.Registry) (*Transaction, error) {
	key := cachedRegistryTx{
		account:  reg.Account,
		registry: reg.Name,
	}
	tx, ok := ctx.Cached(key).(*Transaction)
	if ok {
		return tx, nil
	}

	var chg *state.Change
	if !ctx.IsEphemeral() {
		// running in the context of a hook (although not necessarily a registry hook)
		task, _ := ctx.Task()

		tx, commitTask, err := GetTransaction(task)
		if err != nil && !errors.Is(err, &state.NoStateError{}) {
			return nil, err
		}

		if commitTask != nil {
			// running in a registry hook, just make sure to save changes once its done
			ctx.OnDone(func() error {
				commitTask.Set("registry-transaction", tx)
				return nil
			})

			ctx.Cache(key, tx)
			return tx, nil
		}

		// running in a non-registry hook so we'll create the registry hooks/commit
		// and add them to the existing change
		chg = task.Change()

		// TODO: this isn't imposing an order on the registry tasks because I'm not
		// sure whether we should chain them on just this one hook or all of them.
		// Should all registry changes in the install/configure hooks belong to one
		// single transaction? Or is there a reason to have one transaction per hook?
	}

	// non-hook modification to registry, create tx
	st := ctx.State()
	tx, err := NewTransaction(st, reg.Account, reg.Name)
	if err != nil {
		return nil, err
	}

	ctx.OnDone(func() error {
		paths := tx.AlteredPaths()
		if len(paths) == 0 {
			// nothing changed, nothing to commit
			return nil
		}

		if chg == nil {
			chg = st.NewChange("commit-registry", fmt.Sprintf("Commit changes to registry \"%s/%s\"", reg.Account, tx.RegistryName))
		}

		err := populateCommitChange(ctx, chg, tx, reg)
		if err != nil {
			return err
		}

		// attach the change ID to the context, so the API knows when the changes
		// have been committed
		ctx.Cache("change-id", chg.ID())
		st.EnsureBefore(0)

		return nil
	})

	ctx.Cache(key, tx)
	return tx, nil
}

func populateCommitChange(ctx *hookstate.Context, chg *state.Change, tx *Transaction, reg *registry.Registry) error {
	st := ctx.State()
	commitTask := st.NewTask("commit-transaction", fmt.Sprintf("Commit changes to registry \"%s/%s\"", reg.Account, reg.Name))
	commitTask.Set("registry-transaction", tx)
	chg.AddTask(commitTask)

	linkTask := func(t *state.Task) {
		t.Set("commit-task", commitTask.ID())
		chg.AddTask(t)
	}

	// add registry to the commit task, then put the task ID in the hooks tasks.
	paths := tx.AlteredPaths()

	affectedPlugs, err := getAffectedPlugs(st, reg, paths)
	if err != nil {
		return err
	}

	// TODO: possible TOCTOU issue here? check again in handlers
	managers, err := getRegistryManagers(affectedPlugs)
	if err != nil {
		return err
	}

	if len(managers) == 0 {
		return fmt.Errorf("cannot commit changes to registry %s/%s: no manager snap installed", reg.Account, reg.Name)
	}

	// sort so the change/save hooks are run in a deterministic order (for testing)
	sort.Strings(managers)
	managerSnaps := strings.Join(managers, ",")

	ignoreError := false
	chgRegistryTask := setupRegistryHook(st, managerSnaps, "change-registry", ignoreError)
	linkTask(chgRegistryTask)

	saveRegistryTask := setupRegistryHook(st, managerSnaps, "save-registry", ignoreError)
	saveRegistryTask.WaitFor(chgRegistryTask)
	linkTask(saveRegistryTask)
	// commit after managers save ephemeral data
	commitTask.WaitFor(saveRegistryTask)

	for snapName, plugs := range affectedPlugs {
		if snapName == ctx.InstanceName() {
			// the snap making the changes doesn't need to be notified
			continue
		}

		for _, plug := range plugs {
			ignoreError = true
			task := setupRegistryHook(st, snapName, plug.Name+"-view-changed", ignoreError)
			task.WaitFor(commitTask)
			linkTask(task)
		}
	}

	return nil
}

func getRegistryManagers(snapToPlugs map[string][]*snap.PlugInfo) ([]string, error) {
	var managerSnaps []string
	for snapName, plugs := range snapToPlugs {
		for _, plug := range plugs {
			var role string
			if err := plug.Attr("role", &role); err != nil && !errors.Is(err, snap.AttributeNotFoundError{}) {
				return nil, err
			}

			// snap is a manager for one of plugs so run change-registry for it
			if role == "manager" {
				managerSnaps = append(managerSnaps, snapName)
				break
			}
		}
	}

	return managerSnaps, nil
}

func getAffectedPlugs(st *state.State, registry *registry.Registry, storagePaths []string) (map[string][]*snap.PlugInfo, error) {
	var viewNames []string
	for _, path := range storagePaths {
		views := registry.GetAffectedViews(path)
		for _, view := range views {
			viewNames = append(viewNames, view.Name)
		}
	}

	repo := ifacerepo.Get(st)
	plugs := repo.AllPlugs("registry")

	affectedPlugs := make(map[string][]*snap.PlugInfo)
	for _, plug := range plugs {
		conns, err := repo.Connected(plug.Snap.InstanceName(), plug.Name)
		if err != nil {
			return nil, err
		}

		if len(conns) == 0 {
			continue
		}

		var accAttr string
		if err = plug.Attr("account", &accAttr); err != nil {
			return nil, err
		}

		var viewAttr string
		if err = plug.Attr("view", &viewAttr); err != nil {
			return nil, err
		}

		registryName, viewName, ok := strings.Cut(viewAttr, "/")
		if !ok {
			// shouldn't be possible
			return nil, fmt.Errorf("malformed \"view\" attribute in plug %s", plug.Name)
		}

		if accAttr != registry.Account || registryName != registry.Name || !strutil.ListContains(viewNames, viewName) {
			continue
		}

		snapPlugs := affectedPlugs[plug.Snap.InstanceName()]
		affectedPlugs[plug.Snap.InstanceName()] = append(snapPlugs, plug)
	}

	return affectedPlugs, nil
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

func (m *RegistryManager) doCommitTransaction(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	tx, _, err := GetTransaction(t)
	if err != nil {
		return err
	}

	registryAssert, err := assertstateRegistry(t.State(), tx.RegistryAccount, tx.RegistryName)
	if err != nil {
		return err
	}
	schema := registryAssert.Registry().Schema

	return tx.Commit(t.State(), schema)
}

// GetTransction returns the registry transaction associate with the task (even
// if indirectly) and the task in which it was stored.
func GetTransaction(t *state.Task) (*Transaction, *state.Task, error) {
	var tx *Transaction
	err := t.Get("registry-transaction", &tx)
	if err == nil {
		return tx, t, nil
	} else if !errors.Is(err, &state.NoStateError{}) {
		return nil, nil, err
	}

	var id string
	err = t.Get("commit-task", &id)
	if err != nil {
		return nil, nil, err
	}

	ct := t.State().Task(id)
	if ct == nil {
		return nil, nil, fmt.Errorf("cannot find task %s", id)
	}

	if err := ct.Get("registry-transaction", &tx); err != nil {
		return nil, nil, err
	}

	return tx, ct, nil
}
