// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package wrappers

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// From the freedesktop Desktop Entry Specification¹,
//
//	Keys with type localestring may be postfixed by [LOCALE], where
//	LOCALE is the locale type of the entry. LOCALE must be of the form
//	lang_COUNTRY.ENCODING@MODIFIER, where _COUNTRY, .ENCODING, and
//	@MODIFIER may be omitted. If a postfixed key occurs, the same key
//	must be also present without the postfix.
//
//	When reading in the desktop entry file, the value of the key is
//	selected by matching the current POSIX locale for the LC_MESSAGES
//	category against the LOCALE postfixes of all occurrences of the
//	key, with the .ENCODING part stripped.
//
// sadly POSIX doesn't mention what values are valid for LC_MESSAGES,
// beyond mentioning² that it's implementation-defined (and can be of
// the form [language[_territory][.codeset][@modifier]])
//
// 1. https://specifications.freedesktop.org/desktop-entry-spec/latest/ar01s04.html
// 2. http://pubs.opengroup.org/onlinepubs/9699919799/basedefs/V1_chap08.html#tag_08_02
//
// So! The following is simplistic, and based on the contents of
// PROVIDED_LOCALES in locales.config, and does not cover all of
// "locales -m" (and ignores XSetLocaleModifiers(3), which may or may
// not be related). Patches welcome, as long as it's readable.
//
// REVIEWERS: this could also be left as `(?:\[[@_.A-Za-z-]\])?=` if even
// the following is hard to read:
const localizedSuffix = `(?:\[[a-z]+(?:_[A-Z]+)?(?:\.[0-9A-Z-]+)?(?:@[a-z]+)?\])?=`

var isValidDesktopFileLine = regexp.MustCompile(strings.Join([]string{
	// NOTE (mostly to self): as much as possible keep the
	// individual regexp simple, optimize for legibility
	//
	// empty lines and comments
	`^\s*$`,
	`^\s*#`,
	// headers
	`^\[Desktop Entry\]$`,
	`^\[Desktop Action [0-9A-Za-z-]+\]$`,
	`^\[[A-Za-z0-9-]+ Shortcut Group\]$`,
	// https://specifications.freedesktop.org/desktop-entry-spec/latest/ar01s05.html
	"^Type=",
	"^Version=",
	"^Name" + localizedSuffix,
	"^GenericName" + localizedSuffix,
	"^NoDisplay=",
	"^Comment" + localizedSuffix,
	"^Icon=",
	"^Hidden=",
	"^OnlyShowIn=",
	"^NotShowIn=",
	"^Exec=",
	// Note that we do not support TryExec, it does not make sense
	// in the snap context
	"^Terminal=",
	"^Actions=",
	"^MimeType=",
	"^Categories=",
	"^Keywords" + localizedSuffix,
	"^StartupNotify=",
	"^StartupWMClass=",
	"^PrefersNonDefaultGPU=",
	"^SingleMainWindow=",
	// unity extension
	"^X-Ayatana-Desktop-Shortcuts=",
	"^TargetEnvironment=",
}, "|")).Match

// rewriteExecLine rewrites a "Exec=" line to use the wrapper path for snap application.
func rewriteExecLine(s *snap.Info, desktopFile, line string) (string, error) {
	env := fmt.Sprintf("env BAMF_DESKTOP_FILE_HINT=%s ", desktopFile)

	cmd := strings.SplitN(line, "=", 2)[1]
	for _, app := range s.Apps {
		wrapper := app.WrapperPath()
		validCmd := filepath.Base(wrapper)
		if s.InstanceKey != "" {
			// wrapper uses s.InstanceName(), with the instance key
			// set the command will be 'snap_foo.app' instead of
			// 'snap.app', need to account for that
			validCmd = snap.JoinSnapApp(s.SnapName(), app.Name)
		}
		// check the prefix to allow %flag style args
		// this is ok because desktop files are not run through sh
		// so we don't have to worry about the arguments too much
		if cmd == validCmd {
			return "Exec=" + env + wrapper, nil
		} else if strings.HasPrefix(cmd, validCmd+" ") {
			return fmt.Sprintf("Exec=%s%s%s", env, wrapper, line[len("Exec=")+len(validCmd):]), nil
		}
	}

	logger.Noticef("cannot use line %q for desktop file %q (snap %s)", line, desktopFile, s.InstanceName())
	// The Exec= line in the desktop file is invalid. Instead of failing
	// hard we rewrite the Exec= line. The convention is that the desktop
	// file has the same name as the application we can use this fact here.
	df := filepath.Base(desktopFile)
	desktopFileApp := strings.TrimSuffix(df, filepath.Ext(df))
	app, ok := s.Apps[desktopFileApp]
	if ok {
		newExec := fmt.Sprintf("Exec=%s%s", env, app.WrapperPath())
		logger.Noticef("rewriting desktop file %q to %q", desktopFile, newExec)
		return newExec, nil
	}

	return "", fmt.Errorf("invalid exec command: %q", cmd)
}

func rewriteIconLine(s *snap.Info, line string) (string, error) {
	icon := strings.SplitN(line, "=", 2)[1]

	// If there is a path separator, assume the icon is a path name
	if strings.ContainsRune(icon, filepath.Separator) {
		if !strings.HasPrefix(icon, "${SNAP}/") {
			return "", fmt.Errorf("icon path %q is not part of the snap", icon)
		}
		if filepath.Clean(icon) != icon {
			return "", fmt.Errorf("icon path %q is not canonicalized, did you mean %q?", icon, filepath.Clean(icon))
		}
		return line, nil
	}

	// If the icon is prefixed with "snap.${SNAP_NAME}.", rewrite
	// to the instance name.
	snapIconPrefix := fmt.Sprintf("snap.%s.", s.SnapName())
	if strings.HasPrefix(icon, snapIconPrefix) {
		return fmt.Sprintf("Icon=snap.%s.%s", s.InstanceName(), icon[len(snapIconPrefix):]), nil
	}

	// If the icon has any other "snap." prefix, treat this as an error.
	if strings.HasPrefix(icon, "snap.") {
		return "", fmt.Errorf("invalid icon name: %q, must start with %q", icon, snapIconPrefix)
	}

	// Allow other icons names through unchanged.
	return line, nil
}

func sanitizeDesktopFile(s *snap.Info, desktopFile string, rawcontent []byte) []byte {
	var newContent bytes.Buffer
	mountDir := []byte(s.MountDir())
	scanner := bufio.NewScanner(bytes.NewReader(rawcontent))
	for i := 0; scanner.Scan(); i++ {
		bline := scanner.Bytes()

		if !isValidDesktopFileLine(bline) {
			logger.Debugf("ignoring line %d (%q) in source of desktop file %q", i, bline, filepath.Base(desktopFile))
			continue
		}

		// rewrite exec lines to an absolute path for the binary
		if bytes.HasPrefix(bline, []byte("Exec=")) {
			var err error
			line, err := rewriteExecLine(s, desktopFile, string(bline))
			if err != nil {
				// something went wrong, ignore the line
				continue
			}
			bline = []byte(line)
		}

		// rewrite icon line if it references an icon theme icon
		if bytes.HasPrefix(bline, []byte("Icon=")) {
			line, err := rewriteIconLine(s, string(bline))
			if err != nil {
				logger.Debugf("ignoring icon in source desktop file %q: %s", filepath.Base(desktopFile), err)
				continue
			}
			bline = []byte(line)
		}

		// do variable substitution
		bline = bytes.Replace(bline, []byte("${SNAP}"), mountDir, -1)

		newContent.Grow(len(bline) + 1)
		newContent.Write(bline)
		newContent.WriteByte('\n')

		// insert snap name
		if bytes.Equal(bline, []byte("[Desktop Entry]")) {
			newContent.Write([]byte("X-SnapInstanceName=" + s.InstanceName() + "\n"))
		}
	}

	return newContent.Bytes()
}

func updateDesktopDatabase(desktopFiles []string) error {
	if len(desktopFiles) == 0 {
		return nil
	}

	if _, err := exec.LookPath("update-desktop-database"); err == nil {
		if output, err := exec.Command("update-desktop-database", dirs.SnapDesktopFilesDir).CombinedOutput(); err != nil {
			return fmt.Errorf("cannot update-desktop-database %q: %s", output, err)
		}
		logger.Debugf("update-desktop-database successful")
	}
	return nil
}

func findDesktopFiles(rootDir string) ([]string, error) {
	if !osutil.IsDirectory(rootDir) {
		return nil, nil
	}
	desktopFiles, err := filepath.Glob(filepath.Join(rootDir, "*.desktop"))
	if err != nil {
		return nil, fmt.Errorf("cannot get desktop files from %v: %s", rootDir, err)
	}
	return desktopFiles, nil
}

func findSnapDesktopFileIDs(s *snap.Info) (map[string]bool, error) {
	var desktopPlug *snap.PlugInfo
	for _, plug := range s.Plugs {
		if plug.Interface == "desktop" {
			desktopPlug = plug
			break
		}
	}
	if desktopPlug == nil {
		return nil, nil
	}

	attrVal, exists := desktopPlug.Lookup("desktop-file-ids")
	if !exists {
		// desktop-file-ids attribute is optional
		return nil, nil
	}

	// desktop-file-ids must be a list of strings
	desktopFileIDs, ok := attrVal.([]interface{})
	if !ok {
		return nil, errors.New(`internal error: "desktop-file-ids" must be a list of strings`)
	}

	desktopFileIDsMap := make(map[string]bool, len(desktopFileIDs))
	for _, val := range desktopFileIDs {
		desktopFileID, ok := val.(string)
		if !ok {
			return nil, errors.New(`internal error: "desktop-file-ids" must be a list of strings`)
		}
		desktopFileIDsMap[desktopFileID] = true
	}
	return desktopFileIDsMap, nil
}

func deriveDesktopFilesContent(s *snap.Info, desktopFileIDs map[string]bool) (map[string]osutil.FileState, error) {
	rootDir := filepath.Join(s.MountDir(), "meta", "gui")
	desktopFiles, err := findDesktopFiles(rootDir)
	if err != nil {
		return nil, err
	}

	content := make(map[string]osutil.FileState)
	for _, df := range desktopFiles {
		base := filepath.Base(df)
		fileContent, err := os.ReadFile(df)
		if err != nil {
			return nil, err
		}
		// Don't mangle desktop files if listed under desktop-file-ids attribute
		// XXX: Do we want to fail if a desktop-file-ids entry doesn't
		// have a corresponding file?
		if !desktopFileIDs[strings.TrimSuffix(base, ".desktop")] {
			// FIXME: don't blindly use the snap desktop filename, mangle it
			// but we can't just use the app name because a desktop file
			// may call the same app with multiple parameters, e.g.
			// --create-new, --open-existing etc
			base = fmt.Sprintf("%s_%s", s.DesktopPrefix(), base)
		}
		installedDesktopFileName := filepath.Join(dirs.SnapDesktopFilesDir, base)
		fileContent = sanitizeDesktopFile(s, installedDesktopFileName, fileContent)
		content[base] = &osutil.MemoryFileState{
			Content: fileContent,
			Mode:    0644,
		}
	}
	return content, nil
}

// TODO: Merge desktop file helpers into desktop/desktopentry package
func readSnapInstanceName(desktopFile string) (string, error) {
	file, err := os.Open(desktopFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for i := 0; scanner.Scan(); i++ {
		bline := scanner.Text()
		if !strings.HasPrefix(bline, "X-SnapInstanceName=") {
			continue
		}
		return strings.TrimPrefix(bline, "X-SnapInstanceName="), nil
	}

	return "", fmt.Errorf("cannot find X-SnapInstanceName entry in %q", desktopFile)
}

// EnsureSnapDesktopFiles puts in place the desktop files for the applications from the snap.
//
// It also removes desktop files from the applications of the old snap revision to ensure
// that only new snap desktop files exist.
func EnsureSnapDesktopFiles(snaps []*snap.Info) error {
	if err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755); err != nil {
		return err
	}

	var updated []string
	for _, info := range snaps {
		if info == nil {
			return fmt.Errorf("internal error: snap info cannot be nil")
		}

		desktopFileIDs, err := findSnapDesktopFileIDs(info)
		if err != nil {
			return err
		}
		desktopFilesGlobs := []string{fmt.Sprintf("%s_*.desktop", info.DesktopPrefix())}
		for desktopFileID := range desktopFileIDs {
			desktopFilesGlobs = append(desktopFilesGlobs, desktopFileID+".desktop")
		}
		content, err := deriveDesktopFilesContent(info, desktopFileIDs)
		if err != nil {
			return err
		}

		installedDesktopFiles, err := findDesktopFiles(dirs.SnapDesktopFilesDir)
		if err != nil {
			return err
		}
		for _, desktopFile := range installedDesktopFiles {
			instanceName, err := readSnapInstanceName(desktopFile)
			if err != nil {
				// cannot read instance name from desktop file, ignore
				logger.Noticef("cannot read instance name: %s", err)
				continue
			}

			base := filepath.Base(desktopFile)
			_, hasTarget := content[base]
			if hasTarget && instanceName != info.InstanceName() {
				// Check if a target desktop file belongs to another snap
				return fmt.Errorf("cannot install %q: %q already exists for another snap", base, desktopFile)
			}
			hasDesktopPrefix, err := filepath.Match(fmt.Sprintf("%s_*.desktop", info.DesktopPrefix()), base)
			if err != nil {
				return err
			}
			if instanceName == info.InstanceName() && !hasTarget && !hasDesktopPrefix {
				// An unmangled desktop file existed and is no longer used by the snap
				// Let's include it in glob patterns space to ensure it is removed
				desktopFilesGlobs = append(desktopFilesGlobs, base)
			}
		}

		changed, removed, err := osutil.EnsureDirStateGlobs(dirs.SnapDesktopFilesDir, desktopFilesGlobs, content)
		if err != nil {
			return err
		}
		updated = append(updated, changed...)
		updated = append(updated, removed...)
	}

	// updates mime info etc
	if err := updateDesktopDatabase(updated); err != nil {
		return err
	}

	return nil
}

// RemoveSnapDesktopFiles removes the added desktop files for the applications in the snap.
func RemoveSnapDesktopFiles(s *snap.Info) error {
	if !osutil.IsDirectory(dirs.SnapDesktopFilesDir) {
		return nil
	}

	installedDesktopFiles, err := findDesktopFiles(dirs.SnapDesktopFilesDir)
	if err != nil {
		return err
	}

	desktopFilesGlobs := []string{fmt.Sprintf("%s_*.desktop", s.DesktopPrefix())}
	for _, desktopFile := range installedDesktopFiles {
		instanceName, err := readSnapInstanceName(desktopFile)
		if err != nil {
			// cannot read instance name from desktop file, ignore
			logger.Noticef("cannot read instance name: %s", err)
			continue
		}
		base := filepath.Base(desktopFile)
		hasDesktopPrefix, err := filepath.Match(fmt.Sprintf("%s_*.desktop", s.DesktopPrefix()), base)
		if err != nil {
			return err
		}
		if instanceName == s.InstanceName() && !hasDesktopPrefix {
			// An unmangled desktop file exists for the snap, add to glob
			// patterns for removal
			desktopFilesGlobs = append(desktopFilesGlobs, base)
		}
	}
	_, removed, err := osutil.EnsureDirStateGlobs(dirs.SnapDesktopFilesDir, desktopFilesGlobs, nil)
	if err != nil {
		return err
	}

	// updates mime info etc
	if err := updateDesktopDatabase(removed); err != nil {
		return err
	}

	return nil
}
