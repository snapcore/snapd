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

package configcore_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/state"
)

type refreshSuite struct {
	configcoreSuite
	b *fakeStateBackend
}

type fakeStateBackend struct {
	ensureBeforeCalls []time.Duration
}

func (fsb *fakeStateBackend) Checkpoint(_ []byte) error {
	return nil
}

func (fsb *fakeStateBackend) RequestRestart(_ state.RestartType) {}

func (fsb *fakeStateBackend) EnsureBefore(d time.Duration) {
	fsb.ensureBeforeCalls = append(fsb.ensureBeforeCalls, d)
}

func (s *refreshSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	s.b = &fakeStateBackend{}
	s.state = state.New(s.b)
}

var _ = Suite(&refreshSuite{})

func (s *refreshSuite) TestConfigureRefreshTimerHappy(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.timer": "8:00~12:00/2",
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.b.ensureBeforeCalls, DeepEquals, []time.Duration{0})
}

func (s *refreshSuite) TestConfigureRefreshTimerRejected(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.timer": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": "invalid" is not a valid weekday`)
	c.Check(s.b.ensureBeforeCalls, HasLen, 0)
}

func (s *refreshSuite) TestConfigureLegacyRefreshScheduleHappy(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "8:00-12:00",
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.b.ensureBeforeCalls, DeepEquals, []time.Duration{0})
}

func (s *refreshSuite) TestConfigureLegacyRefreshScheduleRejected(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "invalid": not a valid interval`)

	// check that refresh.schedule is verified against legacy parser
	err = configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.schedule": "8:00~12:00/2",
		},
	})
	c.Assert(err, ErrorMatches, `cannot parse "8:00~12:00": not a valid interval`)

	c.Check(s.b.ensureBeforeCalls, HasLen, 0)
}

func (s *refreshSuite) TestConfigureRefreshHoldHappy(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.hold": "2018-08-18T15:00:00Z",
		},
	})
	c.Assert(err, IsNil)
}

func (s *refreshSuite) TestConfigureRefreshHoldInvalid(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"refresh.hold": "invalid",
		},
	})
	c.Assert(err, ErrorMatches, `refresh\.hold cannot be parsed:.*`)
}
