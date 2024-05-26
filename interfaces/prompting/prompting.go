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

package prompting

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ddkwork/golibrary/mylog"
)

// OutcomeType describes the outcome associated with a reply or rule.
type OutcomeType string

const (
	// OutcomeUnset indicates that no outcome was specified, and should only
	// be used while unmarshalling outcome fields marked as omitempty.
	OutcomeUnset OutcomeType = ""
	// OutcomeAllow indicates that a corresponding request should be allowed.
	OutcomeAllow OutcomeType = "allow"
	// OutcomeDeny indicates that a corresponding request should be denied.
	OutcomeDeny OutcomeType = "deny"
)

func (outcome *OutcomeType) UnmarshalJSON(data []byte) error {
	var s string
	mylog.Check(json.Unmarshal(data, &s))

	value := OutcomeType(s)
	switch value {
	case OutcomeAllow, OutcomeDeny:
		*outcome = value
	default:
		return fmt.Errorf(`cannot have outcome other than %q or %q: %q`, OutcomeAllow, OutcomeDeny, value)
	}
	return nil
}

// IsAllow returns true if the outcome is OutcomeAllow, false if the outcome is
// OutcomeDeny, or an error if it cannot be parsed.
func (outcome OutcomeType) IsAllow() (bool, error) {
	switch outcome {
	case OutcomeAllow:
		return true, nil
	case OutcomeDeny:
		return false, nil
	default:
		return false, fmt.Errorf(`internal error: invalid outcome: %q`, outcome)
	}
}

// LifespanType describes the temporal scope for which a reply or rule applies.
type LifespanType string

const (
	// LifespanUnset indicates that no lifespan was specified, and should only
	// be used while unmarshalling lifespan fields marked as omitempty.
	LifespanUnset LifespanType = ""
	// LifespanForever indicates that the reply/rule should never expire.
	LifespanForever LifespanType = "forever"
	// LifespanSingle indicates that a reply should only apply once, and should
	// not be used to create a rule.
	LifespanSingle LifespanType = "single"
	// LifespanTimespan indicates that a reply/rule should apply for a given
	// duration or until a given expiration timestamp.
	LifespanTimespan LifespanType = "timespan"
	// TODO: add LifespanSession which expires after the user logs out
	// LifespanSession  LifespanType = "session"
)

func (lifespan *LifespanType) UnmarshalJSON(data []byte) error {
	var lifespanStr string
	mylog.Check(json.Unmarshal(data, &lifespanStr))

	value := LifespanType(lifespanStr)
	switch value {
	case LifespanForever, LifespanSingle, LifespanTimespan:
		*lifespan = value
	default:
		return fmt.Errorf(`cannot have lifespan other than %q, %q, or %q: %q`, LifespanForever, LifespanSingle, LifespanTimespan, value)
	}
	return nil
}

// ValidateExpiration checks that the given expiration is valid for the
// receiver lifespan.
//
// If the lifespan is LifespanTimespan, then expiration must be non-zero and be
// after the given currTime. Otherwise, it must be zero. Returns an error if
// any of the above are invalid.
func (lifespan LifespanType) ValidateExpiration(expiration time.Time, currTime time.Time) error {
	switch lifespan {
	case LifespanForever, LifespanSingle:
		if !expiration.IsZero() {
			return fmt.Errorf(`cannot have specified expiration when lifespan is %q: %q`, lifespan, expiration)
		}
	case LifespanTimespan:
		if expiration.IsZero() {
			return fmt.Errorf(`cannot have unspecified expiration when lifespan is %q`, lifespan)
		}
		if currTime.After(expiration) {
			return fmt.Errorf("cannot have expiration time in the past: %q", expiration)
		}
	default:
		// Should not occur, since lifespan is validated when unmarshalled
		return fmt.Errorf(`internal error: invalid lifespan: %q`, lifespan)
	}
	return nil
}

// ParseDuration checks that the given duration is valid for the receiver
// lifespan and parses it into an expiration timestamp.
//
// If the lifespan is LifespanTimespan, then duration must be a string parsable
// by time.ParseDuration(), representing the duration of time for which the rule
// should be valid. Otherwise, it must be empty. Returns an error if any of the
// above are invalid, otherwise computes the expiration time of the rule based
// on the given currTime and the given duration and returns it.
func (lifespan LifespanType) ParseDuration(duration string, currTime time.Time) (time.Time, error) {
	var expiration time.Time
	switch lifespan {
	case LifespanForever, LifespanSingle:
		if duration != "" {
			return expiration, fmt.Errorf(`cannot have specified duration when lifespan is %q: %q`, lifespan, duration)
		}
	case LifespanTimespan:
		if duration == "" {
			return expiration, fmt.Errorf(`cannot have unspecified duration when lifespan is %q`, lifespan)
		}
		parsedDuration := mylog.Check2(time.ParseDuration(duration))

		if parsedDuration <= 0 {
			return expiration, fmt.Errorf(`cannot have zero or negative duration: %q`, duration)
		}
		expiration = currTime.Add(parsedDuration)
	default:
		// Should not occur, since lifespan is validated when unmarshalled
		return expiration, fmt.Errorf(`internal error: invalid lifespan: %q`, lifespan)
	}
	return expiration, nil
}
