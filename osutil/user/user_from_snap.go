// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build snap

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

package user

import (
	"os"
	"strconv"

	origUser "os/user"
)

type User = origUser.User
type Group = origUser.Group
type UnknownUserError = origUser.UnknownUserError
type UnknownUserIdError = origUser.UnknownUserIdError
type UnknownGroupError = origUser.UnknownGroupError

func Current() (*User, error) {
	u, err := lookupUserFromGetent(2, strconv.Itoa(os.Getuid()))
	if u == nil && err == nil {
		return nil, UnknownUserIdError(os.Getuid())
	}
	return u, err
}

func Lookup(username string) (*User, error) {
	u, err := lookupUserFromGetent(0, username)
	if u == nil && err == nil {
		return nil, UnknownUserError(username)
	}
	return u, err
}

func LookupId(uid string) (*User, error) {
	u, err := lookupUserFromGetent(2, uid)
	if u == nil && err == nil {
		uidn, errAtoi := strconv.Atoi(uid)
		if errAtoi != nil {
			return nil, UnknownUserError(uid)
		} else {
			return nil, UnknownUserIdError(uidn)
		}
	}
	return u, err
}

func LookupGroup(groupname string) (*Group, error) {
	g, err := lookupGroupFromGetent(0, groupname)
	if g == nil && err == nil {
		return nil, UnknownGroupError(groupname)
	}
	return g, err
}
