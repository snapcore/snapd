// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"text/template"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

func generateDBusActivationFile(app *snap.AppInfo, busName string) ([]byte, error) {
	// The D-Bus service activation file format is defined as part
	// of the protocol specification:
	//
	// https://dbus.freedesktop.org/doc/dbus-specification.html#message-bus-starting-services
	serviceTemplate := `[D-BUS Service]
Name={{.BusName}}
Comment=Bus name for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
SystemdService={{.App.ServiceName}}
Exec={{.App.LauncherCommand}}
AssumedAppArmorLabel={{.App.SecurityTag}}
{{- if eq .App.DaemonScope "system"}}
User=root
{{- end}}
X-Snap={{.App.Snap.InstanceName}}
`
	t := template.Must(template.New("dbus-service").Parse(serviceTemplate))

	serviceData := struct {
		App     *snap.AppInfo
		BusName string
	}{
		App:     app,
		BusName: busName,
	}
	var templateOut bytes.Buffer
	mylog.Check(t.Execute(&templateOut, serviceData))

	return templateOut.Bytes(), nil
}

var snapNameLine = regexp.MustCompile(`(?m)^X-Snap=(.*)$`)

// snapNameFromServiceFile returns the snap name for the D-Bus service activation file.
func snapNameFromServiceFile(filename string) (owner string, err error) {
	content := mylog.Check2(os.ReadFile(filename))

	m := snapNameLine.FindSubmatch(content)
	if m != nil {
		owner = string(m[1])
	}
	return owner, nil
}

// snapServiceActivationFiles returns the list of service activation files for a snap.
func snapServiceActivationFiles(dir, snapName string) (services []string, err error) {
	glob := filepath.Join(dir, "*.service")
	matches := mylog.Check2(filepath.Glob(glob))

	for _, match := range matches {
		serviceSnap := mylog.Check2(snapNameFromServiceFile(match))

		if serviceSnap == snapName {
			services = append(services, filepath.Base(match))
		}
	}
	return services, nil
}

func AddSnapDBusActivationFiles(s *snap.Info) error {
	mylog.Check(os.MkdirAll(dirs.SnapDBusSessionServicesDir, 0755))
	mylog.Check(os.MkdirAll(dirs.SnapDBusSystemServicesDir, 0755))

	// Make sure we include any service files that claim to have
	// been written by the snap.
	sessionServices := mylog.Check2(snapServiceActivationFiles(dirs.SnapDBusSessionServicesDir, s.InstanceName()))

	systemServices := mylog.Check2(snapServiceActivationFiles(dirs.SnapDBusSystemServicesDir, s.InstanceName()))

	sessionContent := make(map[string]osutil.FileState)
	systemContent := make(map[string]osutil.FileState)

	for _, app := range s.Apps {
		if !app.IsService() {
			continue
		}

		for _, slot := range app.ActivatesOn {
			var busName string
			mylog.Check(slot.Attr("name", &busName))

			content := mylog.Check2(generateDBusActivationFile(app, busName))

			filename := busName + ".service"
			fileState := &osutil.MemoryFileState{
				Content: content,
				Mode:    0644,
			}
			switch app.DaemonScope {
			case snap.SystemDaemon:
				systemContent[filename] = fileState
				systemServices = append(systemServices, filename)
			case snap.UserDaemon:
				sessionContent[filename] = fileState
				sessionServices = append(sessionServices, filename)
			}
		}
	}

	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapDBusSessionServicesDir, sessionServices, sessionContent))

	_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(dirs.SnapDBusSystemServicesDir, systemServices, systemContent))
	// On error, remove files installed by first invocation

	return nil
}

func RemoveSnapDBusActivationFiles(s *snap.Info) error {
	// Select files to delete via X-Snap line to ensure everything
	// is cleaned up if "snap try" is used and snap.yaml is
	// modified.
	for _, servicesDir := range []string{
		dirs.SnapDBusSessionServicesDir,
		dirs.SnapDBusSystemServicesDir,
	} {
		toRemove := mylog.Check2(snapServiceActivationFiles(servicesDir, s.InstanceName()))

		_, _ := mylog.Check3(osutil.EnsureDirStateGlobs(servicesDir, toRemove, nil))

	}
	return nil
}
