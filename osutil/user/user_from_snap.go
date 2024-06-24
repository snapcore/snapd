//go:build snap && osusergo

package user

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	origUser "os/user"
)

type User = origUser.User
type Group = origUser.Group
type UnknownUserError = origUser.UnknownUserError
type UnknownGroupError = origUser.UnknownGroupError

func getEnt(database string) ([]byte, error) {
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer

	cmd := exec.Command("getent", database)
	cmd.Stdin = nil
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("getent %s returned an error: %q", database, errBuf.Bytes())
	}

	return outBuf.Bytes(), nil
}

func lookupGroup(index int, expectedValue string) (*Group, error) {
	buf, err := getEnt("group")
	if err != nil {
		return nil, err
	}
	rd := bufio.NewReader(bytes.NewReader(buf))
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

func lookupUser(index int, expectedValue string) (*User, error) {
	buf, err := getEnt("passwd")
	if err != nil {
		return nil, err
	}
	rd := bufio.NewReader(bytes.NewReader(buf))
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

func Current() (*User, error) {
	return lookupUser(2, strconv.Itoa(os.Getuid()))
}

func Lookup(username string) (*User, error) {
	return lookupUser(0, username)
}

func LookupId(uid string) (*User, error) {
	return lookupUser(2, uid)
}

func LookupGroup(groupname string) (*Group, error) {
	return lookupGroup(0, groupname)
}
