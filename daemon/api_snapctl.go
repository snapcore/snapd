// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package daemon

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	snapctlCmd = &Command{
		Path:        "/v2/snapctl",
		POST:        runSnapctl,
		WriteAccess: snapAccess{},
	}
)

var ctlcmdRun = ctlcmd.Run

func runSnapctl(c *Command, r *http.Request, user *auth.UserState) Response {
	var snapctlPostData client.SnapCtlPostData

	if err := jsonutil.DecodeWithNumber(r.Body, &snapctlPostData); err != nil {
		return BadRequest("cannot decode snapctl request: %s", err)
	}

	if len(snapctlPostData.Args) == 0 {
		return BadRequest("snapctl cannot run without args")
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %s", err)
	}

	// Ignore missing context error to allow 'snapctl -h' without a context;
	// Actual context is validated later by get/set.
	context, _ := c.d.overlord.HookManager().Context(snapctlPostData.ContextID)

	// make the data read from stdin available for the hook
	// TODO: use a forwarded stdin here
	if snapctlPostData.Stdin != nil {
		context.Lock()
		context.Set("stdin", snapctlPostData.Stdin)
		context.Unlock()
	}

	stdout, stderr, err := ctlcmdRun(context, snapctlPostData.Args, ucred.Uid)
	if err != nil {
		if e, ok := err.(*ctlcmd.UnsuccessfulError); ok {
			result := map[string]interface{}{
				"stdout":    string(stdout),
				"stderr":    string(stderr),
				"exit-code": e.ExitCode,
			}
			return &apiError{
				Status:  200,
				Message: e.Error(),
				Kind:    client.ErrorKindUnsuccessful,
				Value:   result,
			}
		}
		if e, ok := err.(*ctlcmd.ForbiddenCommandError); ok {
			return Forbidden(e.Error())
		}
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			stdout = []byte(e.Error())
		} else {
			return BadRequest("error running snapctl: %s", err)
		}
	}

	if context != nil && context.IsEphemeral() {
		context.Lock()
		err := context.Done()
		context.Unlock()
		if err != nil {
			return BadRequest(i18n.G("set failed: %v"), err)
		}

		chg, err := getRegistryCommitChange(c.d.state, context)
		if err != nil {
			return InternalError(err.Error())
		}

		if chg != nil {
			// wait for registry commit
			select {
			case <-chg.Ready():
				c.d.state.Lock()
				if chg.Err() != nil {
					// TODO: rethink this
					stderr = []byte(chg.Err().Error())
				}
				c.d.state.Unlock()
			case <-time.After(10 * time.Minute):
				// TODO; reasonable timeout? hooks have large timeouts (default is 10m)
				return BadRequest(i18n.G("registry commit timed out"))
			}
		}
	}

	result := map[string]string{
		"stdout": string(stdout),
		"stderr": string(stderr),
	}

	return SyncResponse(result)
}

func getRegistryCommitChange(st *state.State, ctx *hookstate.Context) (*state.Change, error) {
	ctx.Lock()
	defer ctx.Unlock()

	chgIDVal := ctx.Cached("change-id")
	if chgIDVal == nil {
		return nil, nil
	}

	// wait for registry commit
	chgID, ok := chgIDVal.(string)
	if !ok {
		return nil, fmt.Errorf(i18n.G("cannot read registry commit change ID: unexpected type %T"), chgIDVal)
	}

	return st.Change(chgID), nil
}
