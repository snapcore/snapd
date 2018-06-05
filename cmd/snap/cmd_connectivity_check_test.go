// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package main_test

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestConnectivityHappy(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/debug")
			c.Check(r.URL.RawQuery, check.Equals, "")
			data, err := ioutil.ReadAll(r.Body)
			c.Check(err, check.IsNil)
			c.Check(data, check.DeepEquals, []byte(`{"action":"connectivity"}`))
			fmt.Fprintln(w, `{"type": "sync", "result": {"api.snapcraft.io":true}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := snap.Parser().ParseArgs([]string{"debug", "connectivity"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `Connectivity status:
 * PASS
`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestConnectivityUnhappy(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "POST")
			c.Check(r.URL.Path, check.Equals, "/v2/debug")
			c.Check(r.URL.RawQuery, check.Equals, "")
			data, err := ioutil.ReadAll(r.Body)
			c.Check(err, check.IsNil)
			c.Check(data, check.DeepEquals, []byte(`{"action":"connectivity"}`))
			fmt.Fprintln(w, `{"type": "sync", "result": {"api.snapcraft.io":true, "foo.bar.com":false}}`)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	_, err := snap.Parser().ParseArgs([]string{"debug", "connectivity"})
	c.Assert(err, check.ErrorMatches, "cannot connect to 1 of 2 servers")
        // note that only the unreachable hosts are displayed
	c.Check(s.Stdout(), check.Equals, `Connectivity status:
 * foo.bar.com: unreachable
`)
	c.Check(s.Stderr(), check.Equals, "")
}
