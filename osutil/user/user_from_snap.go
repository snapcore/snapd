//go:build snap && osusergo

package user

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"

	origUser "os/user"

	"github.com/snapcore/snapd/release"
)

type User = origUser.User
type Group = origUser.Group
type UnknownUserError = origUser.UnknownUserError
type UnknownGroupError = origUser.UnknownGroupError

func classicCurrent() (*User, error) {
	return origUser.Current()
}

func classicLookup(username string) (*User, error) {
	return origUser.Lookup(username)
}

func classicLookupId(uid string) (*User, error) {
	return origUser.LookupId(uid)
}

func classicLookupGroup(groupname string) (*Group, error) {
	return origUser.LookupGroup(groupname)
}

func lookupExtraGroup(index int, expectedValue string) (*Group, error) {
	f, err := os.Open("/var/lib/extrausers/group")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}
	defer f.Close()
	rd := bufio.NewReader(f)
	for {
		var line []byte
		for {
			chunk, isPrefix, err := rd.ReadLine()
			if err != nil {
				if err == io.EOF {
					return nil, nil
				}
				return nil, err
			}
			line = append(line, chunk...)
			if !isPrefix {
				break
			}
		}

		if len(line) == 0 || line[0] == '#' {
			continue
		}
		components := strings.SplitN(string(line), ":", 4)
		if len(components) != 4 {
			continue
		}

		if components[index] != expectedValue {
			continue
		}

		return &Group{
			Name: components[0],
			Gid:  components[2],
		}, nil
	}
	return nil, nil
}

func lookupExtraUser(index int, expectedValue string) (*User, error) {
	f, err := os.Open("/var/lib/extrausers/passwd")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}
	defer f.Close()
	rd := bufio.NewReader(f)
	for {
		var line []byte
		for {
			chunk, isPrefix, err := rd.ReadLine()
			if err != nil {
				if err == io.EOF {
					return nil, nil
				}
				return nil, err
			}
			line = append(line, chunk...)
			if !isPrefix {
				break
			}
		}

		if len(line) == 0 || line[0] == '#' {
			continue
		}
		components := strings.SplitN(string(line), ":", 7)
		if len(components) != 7 {
			continue
		}

		if components[index] != expectedValue {
			continue
		}

		return &User{
			Username: components[0],
			Uid:      components[2],
			Gid:      components[3],
			Name:     components[4],
			HomeDir:  components[5],
		}, nil
	}
	return nil, nil
}

func coreCurrent() (*User, error) {
	foundExtraUser, err := lookupExtraUser(2, strconv.Itoa(os.Getuid()))
	if err != nil {
		return nil, err
	}
	if foundExtraUser != nil {
		return foundExtraUser, nil
	}
	return origUser.Current()
}

func coreLookup(username string) (*User, error) {
	foundExtraUser, err := lookupExtraUser(0, username)
	if err != nil {
		return nil, err
	}
	if foundExtraUser != nil {
		return foundExtraUser, nil
	}
	return origUser.Lookup(username)
}

func coreLookupId(uid string) (*User, error) {
	foundExtraUser, err := lookupExtraUser(2, uid)
	if err != nil {
		return nil, err
	}
	if foundExtraUser != nil {
		return foundExtraUser, nil
	}
	return origUser.LookupId(uid)
}

func coreLookupGroup(groupname string) (*Group, error) {
	foundExtraGroup, err := lookupExtraGroup(0, groupname)
	if err != nil {
		return nil, err
	}
	if foundExtraGroup != nil {
		return foundExtraGroup, nil
	}
	return origUser.LookupGroup(groupname)
}

func Current() (*User, error) {
	if release.OnClassic {
		return classicCurrent()
	} else {
		return coreCurrent()
	}
}

func Lookup(username string) (*User, error) {
	if release.OnClassic {
		return classicLookup(username)
	} else {
		return coreLookup(username)
	}
}

func LookupId(uid string) (*User, error) {
	if release.OnClassic {
		return classicLookupId(uid)
	} else {
		return coreLookupId(uid)
	}
}

func LookupGroup(groupname string) (*Group, error) {
	if release.OnClassic {
		return classicLookupGroup(groupname)
	} else {
		return coreLookupGroup(groupname)
	}
}
