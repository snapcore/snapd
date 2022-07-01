// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

// ResponseType is the response type
type ResponseType string

// "there are three standard return types: Standard return value,
// Background operation, Error", each returning a JSON object with the
// following "type" field:
const (
	ResponseTypeSync  ResponseType = "sync"
	ResponseTypeAsync ResponseType = "async"
	ResponseTypeError ResponseType = "error"
)

// Response knows how to serve itself.
type Response interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// A StructuredResponse serializes itself to our standard JSON response format.
type StructuredResponse interface {
	Response

	JSON() *respJSON
}

// respJSON represents our standard JSON response format.
type respJSON struct {
	Type ResponseType `json:"type"`
	// Status is the HTTP status code.
	Status int `json:"status-code"`
	// StatusText is filled by the serving pipeline.
	StatusText string `json:"status"`
	// Result is a free-form optional result object.
	Result interface{} `json:"result"`
	// Change is the change ID for an async response.
	Change string `json:"change,omitempty"`
	// Sources is used in find responses.
	Sources []string `json:"sources,omitempty"`
	// XXX SuggestedCurrency is part of unsupported paid snap code.
	SuggestedCurrency string `json:"suggested-currency,omitempty"`
	// Maintenance...  are filled as needed by the serving pipeline.
	WarningTimestamp *time.Time   `json:"warning-timestamp,omitempty"`
	WarningCount     int          `json:"warning-count,omitempty"`
	Maintenance      *errorResult `json:"maintenance,omitempty"`
}

func (r *respJSON) JSON() *respJSON {
	return r
}

func maintenanceForRestartType(rst restart.RestartType) *errorResult {
	e := &errorResult{}
	switch rst {
	case restart.RestartSystem, restart.RestartSystemNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemRestartMsg
		e.Value = map[string]interface{}{
			"op": "reboot",
		}
	case restart.RestartSystemHaltNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemHaltMsg
		e.Value = map[string]interface{}{
			"op": "halt",
		}
	case restart.RestartSystemPoweroffNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemPoweroffMsg
		e.Value = map[string]interface{}{
			"op": "poweroff",
		}
	case restart.RestartDaemon:
		e.Kind = client.ErrorKindDaemonRestart
		e.Message = daemonRestartMsg
	case restart.RestartSocket:
		e.Kind = client.ErrorKindDaemonRestart
		e.Message = socketRestartMsg
	case restart.RestartUnset:
		// shouldn't happen, maintenance for unset type should just be nil
		panic("internal error: cannot marshal maintenance for RestartUnset")
	}
	return e
}

func (r *respJSON) addMaintenanceFromRestartType(rst restart.RestartType) {
	if rst == restart.RestartUnset {
		// nothing to do
		return
	}
	r.Maintenance = maintenanceForRestartType(rst)
}

func (r *respJSON) addWarningCount(count int, stamp time.Time) {
	if count == 0 {
		return
	}
	r.WarningCount = count
	r.WarningTimestamp = &stamp
}

func (r *respJSON) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	status := r.Status
	r.StatusText = http.StatusText(r.Status)
	bs, err := json.Marshal(r)
	if err != nil {
		logger.Noticef("cannot marshal %#v to JSON: %v", *r, err)
		bs = nil
		status = 500
	}

	hdr := w.Header()
	if r.Status == 202 || r.Status == 201 {
		if m, ok := r.Result.(map[string]interface{}); ok {
			if location, ok := m["resource"]; ok {
				if location, ok := location.(string); ok && location != "" {
					hdr.Set("Location", location)
				}
			}
		}
	}

	hdr.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(bs)
}

// SyncResponse builds a "sync" response from the given result.
func SyncResponse(result interface{}) Response {
	if rsp, ok := result.(Response); ok {
		return rsp
	}

	if err, ok := result.(error); ok {
		return InternalError("internal error: %v", err)
	}

	return &respJSON{
		Type:   ResponseTypeSync,
		Status: 200,
		Result: result,
	}
}

// AsyncResponse builds an "async" response for a created change
func AsyncResponse(result map[string]interface{}, change string) Response {
	return &respJSON{
		Type:   ResponseTypeAsync,
		Status: 202,
		Result: result,
		Change: change,
	}
}

// A snapStream ServeHTTP method streams a snap
type snapStream struct {
	SnapName string
	Filename string
	Info     *snap.DownloadInfo
	Token    string
	stream   io.ReadCloser
	resume   int64
}

// ServeHTTP from the Response interface
func (s *snapStream) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/octet-stream")
	snapname := fmt.Sprintf("attachment; filename=%s", s.Filename)
	hdr.Set("Content-Disposition", snapname)

	hdr.Set("Snap-Sha3-384", s.Info.Sha3_384)
	// can't set Content-Length when stream is nil as it breaks http clients
	// setting it also when there is a stream, for consistency
	hdr.Set("Snap-Length", strconv.FormatInt(s.Info.Size, 10))
	if s.Token != "" {
		hdr.Set("Snap-Download-Token", s.Token)
	}

	if s.stream == nil {
		// nothing to actually stream
		return
	}
	hdr.Set("Content-Length", strconv.FormatInt(s.Info.Size-s.resume, 10))

	if s.resume > 0 {
		hdr.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", s.resume, s.Info.Size-1, s.Info.Size))
		w.WriteHeader(206)
	}

	defer s.stream.Close()
	bytesCopied, err := io.Copy(w, s.stream)
	if err != nil {
		logger.Noticef("cannot copy snap %s (%#v) to the stream: %v", s.SnapName, s.Info, err)
		http.Error(w, err.Error(), 500)
	}
	if bytesCopied != s.Info.Size-s.resume {
		logger.Noticef("cannot copy snap %s (%#v) to the stream: bytes copied=%d, expected=%d", s.SnapName, s.Info, bytesCopied, s.Info.Size)
		http.Error(w, io.EOF.Error(), 502)
	}
}

// A snapshotExportResponse 's ServeHTTP method serves a specific snapshot ID
type snapshotExportResponse struct {
	*snapshotstate.SnapshotExport
	setID uint64
	st    *state.State
}

// ServeHTTP from the Response interface
func (s snapshotExportResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Length", strconv.FormatInt(s.Size(), 10))
	w.Header().Add("Content-Type", client.SnapshotExportMediaType)
	if err := s.StreamTo(w); err != nil {
		logger.Debugf("cannot export snapshot: %v", err)
	}
	s.Close()
	s.st.Lock()
	defer s.st.Unlock()
	snapshotstate.UnsetSnapshotOpInProgress(s.st, s.setID)
}

// A fileResponse 's ServeHTTP method serves the file
type fileResponse string

// ServeHTTP from the Response interface
func (f fileResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filename := fmt.Sprintf("attachment; filename=%s", filepath.Base(string(f)))
	w.Header().Add("Content-Disposition", filename)
	http.ServeFile(w, r, string(f))
}

// A journalLineReaderSeqResponse's ServeHTTP method reads lines (presumed to
// be, each one on its own, a JSON dump of a systemd.Log, as output by
// journalctl -o json) from an io.ReadCloser, loads that into a client.Log, and
// outputs the json dump of that, padded with RS and LF to make it a valid
// json-seq response.
//
// The reader is always closed when done (this is important for
// osutil.WatingStdoutPipe).
//
// Tip: “jq” knows how to read this; “jq --seq” both reads and writes this.
type journalLineReaderSeqResponse struct {
	readers []io.ReadCloser
	follow  bool
}

var errCannotWriteToClient = errors.New("cannot write data, client may have hung up unexpectedly")

func (rr *journalLineReaderSeqResponse) safeSendError(c chan error, value error) (closed bool) {
	defer func() {
		if recover() != nil {
			closed = true
		}
	}()
	c <- value
	return false
}

func (rr *journalLineReaderSeqResponse) safeSendLog(c chan systemd.Log, value systemd.Log) (closed bool) {
	defer func() {
		if recover() != nil {
			closed = true
		}
	}()
	c <- value
	return false
}

func (rr *journalLineReaderSeqResponse) logReader(r io.ReadCloser, c chan systemd.Log, e chan error) {
	defer r.Close()
	decoder := json.NewDecoder(r)
	for {
		var log systemd.Log

		// This will always cause an error before or later because of an
		// io.EOF. This means we can rely on this being our termination
		// condition for the read loop, and then do the error handling in
		// the main go routine.
		if err := decoder.Decode(&log); err != nil {
			// Ignore the return value here as we are breaking out
			// anyway and not sending more messages.
			rr.safeSendError(e, err)
			break
		}

		if closed := rr.safeSendLog(c, log); closed {
			break
		}
	}
}

func (rr *journalLineReaderSeqResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json-seq")

	flusher, hasFlusher := w.(http.Flusher)
	writer := bufio.NewWriter(w)
	enc := json.NewEncoder(writer)

	// Buffer 128 (arbitrary, seems appropriate) messages, and
	// the number of readers in errors as we know exactly how
	// many errors to expect on the channel.
	c := make(chan systemd.Log, 128)
	e := make(chan error, len(rr.readers))
	for _, r := range rr.readers {
		go rr.logReader(r, c, e)
	}

	writeError := func(err error) {
		fmt.Fprintf(writer, `\x1E{"error": %q}\n`, err)
		logger.Noticef("cannot stream response; problem reading: %v", err)
	}

	writeLogs := func(logs []systemd.Log) error {
		// sort by timestamp ascending
		sort.Slice(logs, func(i, j int) bool {
			ti, _ := logs[i].Time()
			tj, _ := logs[j].Time()
			return ti.Before(tj)
		})

		for _, l := range logs {
			writer.WriteByte(0x1E) // RS -- see ascii(7), and RFC7464

			// ignore the error...
			t, _ := l.Time()
			if err := enc.Encode(client.Log{
				Timestamp: t,
				Message:   l.Message(),
				SID:       l.SID(),
				PID:       l.PID(),
			}); err != nil {
				return err
			}
		}

		if rr.follow {
			if err := writer.Flush(); err != nil {
				return errCannotWriteToClient
			}
			if hasFlusher {
				flusher.Flush()
			}
		}
		return nil
	}

	var logReadersDone int
	for {
		var logs []systemd.Log

		// always block read on the first one to ensure we don't waste any
		// time spinning on a channel that will never have any logs
		select {
		case log := <-c:
			logs = append(logs, log)
		case err := <-e:
			log.Println("error waiting for logs:", err)
			if err != io.EOF {
				writeError(err)
			}
			logReadersDone++
		}

		// Now we spend a small amount of time batch reading all available logs
		// on the log/error channel, the reason we do this is to make sure we read
		// everything available for the initial/final batch of logs (when following) or
		// we read everything available when not following. It's done like this to
		// ensure that output is consistent and sorted correctly by timestamp when
		// multiple log-sources are in play.
		timeout := time.After(time.Millisecond * 25)
		for {
			var terminate bool
			select {
			case log := <-c:
				logs = append(logs, log)
			case err := <-e:
				log.Println("error reading logs:", err)
				if err != io.EOF {
					logger.Noticef("cannot decode systemd log: %v", err)
				}

				logReadersDone++
			case <-timeout:
				terminate = true
			}

			if terminate {
				break
			}
		}

		if err := writeLogs(logs); err != nil {
			if err != errCannotWriteToClient {
				writeError(err)
			}
			break
		}

		if logReadersDone == len(rr.readers) {
			break
		}
	}

	// Close the channels to clean up if we have terminated early
	// due to errors. This can cause the go routines to panic, but
	// the safeSend should catch this and then terminate cleanly
	close(c)
	close(e)

	if err := writer.Flush(); err != nil {
		logger.Noticef("cannot stream response; problem writing: %v", err)
	}
}

type assertResponse struct {
	assertions []asserts.Assertion
	bundle     bool
}

// AssertResponse builds a response whose ServerHTTP method serves one or a bundle of assertions.
func AssertResponse(asserts []asserts.Assertion, bundle bool) Response {
	if len(asserts) > 1 {
		bundle = true
	}
	return &assertResponse{assertions: asserts, bundle: bundle}
}

func (ar assertResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := asserts.MediaType
	if ar.bundle {
		t = mime.FormatMediaType(t, map[string]string{"bundle": "y"})
	}
	w.Header().Set("Content-Type", t)
	w.Header().Set("X-Ubuntu-Assertions-Count", strconv.Itoa(len(ar.assertions)))
	w.WriteHeader(200)
	enc := asserts.NewEncoder(w)
	for _, a := range ar.assertions {
		err := enc.Encode(a)
		if err != nil {
			logger.Noticef("cannot write encoded assertion into response: %v", err)
			break

		}
	}
}
