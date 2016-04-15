// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// InstallSnap adds the snap with the given name from the given channel (or
// the system default channel if not), returning the UUID of the background
// operation upon success.
func (client *Client) InstallSnap(name, channel string, devMode bool) (changeID string, err error) {
	path := fmt.Sprintf("/v2/snaps/%s", name)
	req := struct {
		Action  string `json:"action"`
		Channel string `json:"channel"`
		DevMode bool   `json:"devmode"`
	}{Action: "install", Channel: channel, DevMode: devMode}
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	body := bytes.NewReader(data)
	return client.doAsync("POST", path, nil, body)
}

// InstallSnapPath sideloads the snap with the given path, returning the UUID
// of the background operation upon success.
//
// XXX: add support for "X-Allow-Unsigned"
func (client *Client) InstallSnapPath(path string, devMode bool) (changeID string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open: %q", path)
	}

	client.xxxHackDevMode = devMode
	defer func() { client.xxxHackDevMode = false }()
	return client.doAsync("POST", "/v2/snaps", nil, f)
}

// RemoveSnap removes the snap with the given name, returning the UUID of the
// background operation upon success.
func (client *Client) RemoveSnap(name string) (changeID string, err error) {
	path := fmt.Sprintf("/v2/snaps/%s", name)
	body := strings.NewReader(`{"action":"remove"}`)

	return client.doAsync("POST", path, nil, body)
}

// RefreshSnap refreshes the snap with the given name (switching it to track
// the given channel if given), returning the UUID of the background operation
// upon success.
func (client *Client) RefreshSnap(name, channel string, devMode bool) (changeID string, err error) {
	path := fmt.Sprintf("/v2/snaps/%s", name)
	req := struct {
		Action  string `json:"action"`
		Channel string `json:"channel"`
		DevMode bool   `json:"devmode"`
	}{Action: "refresh", Channel: channel, DevMode: devMode}
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	body := bytes.NewReader(data)
	return client.doAsync("POST", path, nil, body)
}

// PurgeSnap purges the snap with the given name, returning the UUID of the
// background operation upon success.
//
// TODO: nuke purge, when we have snapshots/backups done
func (client *Client) PurgeSnap(name string) (changeID string, err error) {
	path := fmt.Sprintf("/v2/snaps/%s", name)
	body := strings.NewReader(`{"action":"purge"}`)

	return client.doAsync("POST", path, nil, body)
}

// RollbackSnap rolls back the snap with the given name, returning the UUID of
// the background operation upon success.
func (client *Client) RollbackSnap(name string) (changeID string, err error) {
	path := fmt.Sprintf("/v2/snaps/%s", name)
	body := strings.NewReader(`{"action":"rollback"}`)

	return client.doAsync("POST", path, nil, body)
}

// ActivateSnap activates the snap with the given name, returning the UUID of
// the background operation upon success.
func (client *Client) ActivateSnap(name string) (changeID string, err error) {
	path := fmt.Sprintf("/v2/snaps/%s", name)
	body := strings.NewReader(`{"action":"activate"}`)

	return client.doAsync("POST", path, nil, body)
}

// DeactivateSnap deactivates the snap with the given name, returning the UUID
// of the background operation upon success.
func (client *Client) DeactivateSnap(name string) (changeID string, err error) {
	path := fmt.Sprintf("/v2/snaps/%s", name)
	body := strings.NewReader(`{"action":"deactivate"}`)

	return client.doAsync("POST", path, nil, body)
}
