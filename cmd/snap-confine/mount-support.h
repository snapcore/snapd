/*
 * Copyright (C) 2015 Canonical Ltd
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

#ifndef SNAP_MOUNT_SUPPORT_H
#define SNAP_MOUNT_SUPPORT_H

#include "../libsnap-confine-private/apparmor-support.h"
#include "snap-confine-invocation.h"
#include <sys/types.h>

/* Base location where extra libraries might be made available to the snap.
 * This is currently used for graphics drivers, but could pontentially be used
 * for other goals as well.
 *
 * NOTE: do not bind-mount anything directly onto this directory! This is only
 * a *base* directory: for exposing drivers and libraries, create a
 * sub-directory in SC_EXTRA_LIB_DIR and use that one as the bind mount target.
 */
#define SC_EXTRA_LIB_DIR "/var/lib/snapd/lib"

/* Property file typically shipped on a classic Halium system
 *
 * Allows detecing Halium-based GNU/Linux adaptations.
 */
#define SC_HYBRIS_PROPERTY_FILE "/system/build.prop"

/**
 * Assuming a new mountspace, populate it accordingly.
 *
 * This function performs many internal tasks:
 * - prepares and chroots into the core snap (on classic systems)
 * - creates private /tmp
 * - creates private /dev/pts
 * - processes mount profiles
 **/
void sc_populate_mount_ns(struct sc_apparmor *apparmor, int snap_update_ns_fd,
			  const sc_invocation * inv, const gid_t real_gid,
			  const gid_t saved_gid);

/**
 * Ensure that / or /snap is mounted with the SHARED option.
 *
 * If the system is found to be not having a shared mount for "/"
 * snap-confine will create a shared bind mount for "/snap" to
 * ensure that "/snap" is mounted shared. See LP:#1668659
 */
void sc_ensure_shared_snap_mount(void);

/**
 * Set up user mounts, private to this process.
 *
 * If any user mounts have been configured for this process, this does
 * the following:
 * - create a new mount namespace
 * - reconfigure all existing mounts to slave mode
 * - perform all user mounts
 */
void sc_setup_user_mounts(struct sc_apparmor *apparmor, int snap_update_ns_fd,
			  const char *snap_name);

/**
 * Ensure that SNAP_MOUNT_DIR and /var/snap are mount points.
 *
 * Create bind mounts and set up shared propagation for SNAP_MOUNT_DIR and
 * /var/snap as needed. This allows for further propagation changes after the
 * initial mount namespace is unshared.
 */
void sc_ensure_snap_dir_shared_mounts(void);

/**
 * Set up mount namespace for parallel installed classic snap
 *
 * Create bind mounts from instance specific locations to non-instance ones.
 */
void sc_setup_parallel_instance_classic_mounts(const char *snap_name,
					       const char *snap_instance_name);

/**
 * Populate libgl_dir with a symlink farm to files matching glob_list.
 *
 * The symbolic links are made in one of two ways. If the library found is a
 * file a regular symlink "$libname" -> "/path/to/hostfs/$libname" is created.
 * If the library is a symbolic link then relative links are kept as-is but
 * absolute links are translated to have "/path/to/hostfs" up front so that
 * they work after the pivot_root elsewhere.
 *
 * The glob list passed to us is produced with paths relative to source dir,
 * to simplify the various tie-in points with this function.
 */
void sc_populate_libgl_with_hostfs_symlinks(const char *libgl_dir,
					    const char *source_dir,
					    const char *glob_list[],
					    size_t glob_list_len);

void sc_mkdir_and_mount_and_glob_files(const char *rootfs_dir,
				       const char *source_dir[],
				       size_t source_dir_len,
				       const char *tgt_dir,
				       const char *glob_list[],
				       size_t glob_list_len);
#endif
