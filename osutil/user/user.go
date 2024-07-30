// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !snap

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
	origUser "os/user"
)

type User = origUser.User
type Group = origUser.Group
type UnknownUserError = origUser.UnknownUserError
type UnknownGroupError = origUser.UnknownGroupError

func Current() (*User, error) {
	return origUser.Current()
}

func Lookup(username string) (*User, error) {
	return origUser.Lookup(username)
}

func LookupId(uid string) (*User, error) {
	return origUser.LookupId(uid)
}

func LookupGroup(groupname string) (*Group, error) {
	return origUser.LookupGroup(groupname)
}
