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

package main

import (
	"fmt"
	"syscall"
)

// SecureBindMount performs a bind mount between two absolute paths
// containing no symlinks.
func SecureBindMount(sourceDir, targetDir string, flags uint) error {
	// This function only attempts to handle bind
	// mounts. Expanding to other mounts will require examining
	// do_mount() from fs/namespace.c of the kernel that called
	// functions (eventually) verify `DCACHE_CANT_MOUNT` is not
	// set (eg, by calling lock_mount()).
	if flags&syscall.MS_BIND == 0 {
		return fmt.Errorf("only bind mounts are supported")
	}
	// The kernel doesn't support recursively switching a tree of
	// bind mounts to read only, and we haven't written a work
	// around.
	if flags&syscall.MS_RDONLY != 0 && flags&syscall.MS_REC != 0 {
		return fmt.Errorf("cannot use MS_RDONLY and MS_REC together")
	}

	// Step 1: acquire file descriptors representing the source
	// and destination directories, ensuring no symlinks are
	// followed.
	sourceFd, err := secureOpenPath(sourceDir)
	if err != nil {
		return err
	}
	defer sysClose(sourceFd)
	targetFd, err := secureOpenPath(targetDir)
	if err != nil {
		return err
	}
	defer sysClose(targetFd)

	// Step 2: perform a bind mount between the paths identified
	// by the two file descriptors.
	sourceFdPath := fmt.Sprintf("/proc/self/fd/%d", sourceFd)
	targetFdPath := fmt.Sprintf("/proc/self/fd/%d", targetFd)
	bindFlags := syscall.MS_BIND | (flags & syscall.MS_REC)
	if err := sysMount(sourceFdPath, targetFdPath, "", uintptr(bindFlags), ""); err != nil {
		return err
	}

	// Step 3: optionally change to readonly
	if flags&syscall.MS_RDONLY != 0 {
		// We need to look up the target directory a second
		// time, because targetFd refers to the path shadowed
		// by the mount point.
		mountFd, err := secureOpenPath(targetDir)
		if err != nil {
			// FIXME: the mount occurred, but the user
			// moved the target somewhere
			return err
		}
		defer sysClose(mountFd)
		mountFdPath := fmt.Sprintf("/proc/self/fd/%d", mountFd)
		remountFlags := syscall.MS_REMOUNT | syscall.MS_BIND | syscall.MS_RDONLY
		if err := sysMount("none", mountFdPath, "", uintptr(remountFlags), ""); err != nil {
			sysUnmount(mountFdPath, syscall.MNT_DETACH|umountNoFollow)
			return err
		}
	}
	return nil
}
