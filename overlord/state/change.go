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

package state

import (
	"encoding/json"
)

// Change represents a tracked modification to the system state.
//
// The Change provides both the justification for individual tasks
// to be performed and the grouping of them.
//
// As an example, if an administrator requests an interface connection,
// multiple hooks might be individually run to accomplish the task. The
// Change summary would reflect the request for an interface connection,
// while the individual Task values would track the running of
// the hooks themselves.
type Change struct {
	state   *State
	id      string
	kind    string
	summary string
	data    customData
	taskIDs map[string]bool
}

func newChange(state *State, id, kind, summary string) *Change {
	return &Change{
		state:   state,
		id:      id,
		kind:    kind,
		summary: summary,
		data:    make(customData),
		taskIDs: make(map[string]bool),
	}
}

type marshalledChange struct {
	ID      string                      `json:"id"`
	Kind    string                      `json:"kind"`
	Summary string                      `json:"summary"`
	Data    map[string]*json.RawMessage `json:"data"`
	TaskIDs map[string]bool             `json:"task-ids"`
}

// MarshalJSON makes Change a json.Marshaller
func (c *Change) MarshalJSON() ([]byte, error) {
	c.state.ensureLocked()
	return json.Marshal(marshalledChange{
		ID:      c.id,
		Kind:    c.kind,
		Summary: c.summary,
		Data:    c.data,
		TaskIDs: c.taskIDs,
	})
}

// UnmarshalJSON makes Change a json.Unmarshaller
func (c *Change) UnmarshalJSON(data []byte) error {
	if c.state != nil {
		c.state.ensureLocked()
	}
	var unmarshalled marshalledChange
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	c.id = unmarshalled.ID
	c.kind = unmarshalled.Kind
	c.summary = unmarshalled.Summary
	c.data = unmarshalled.Data
	c.taskIDs = unmarshalled.TaskIDs
	return nil
}

// ID returns the individual random key for the change.
func (c *Change) ID() string {
	return c.id
}

// Kind returns the nature of the change for managers to know how to handle it.
func (c *Change) Kind() string {
	return c.kind
}

// Summary returns a summary describing what the change is about.
func (c *Change) Summary() string {
	return c.summary
}

// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (c *Change) Set(key string, value interface{}) {
	c.state.ensureLocked()
	c.data.set(key, value)
}

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
func (c *Change) Get(key string, value interface{}) error {
	c.state.ensureLocked()
	return c.data.get(key, value)
}

// NewTask creates a new task and registers it as a required task for the
// state change to be accomplished.
func (c *Change) NewTask(kind, summary string) *Task {
	c.state.ensureLocked()
	id := c.state.genID()
	t := newTask(c.state, id, kind, summary)
	c.state.tasks[id] = t
	c.taskIDs[id] = true
	return t
}

// Tasks returns all the tasks this state change depends on.
func (c *Change) Tasks() []*Task {
	c.state.ensureLocked()
	res := make([]*Task, 0, len(c.taskIDs))
	for tid := range c.taskIDs {
		res = append(res, c.state.tasks[tid])
	}
	return res
}
