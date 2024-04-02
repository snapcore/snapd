//go:build !snap

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
