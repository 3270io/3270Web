// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/printer"
)

// noPrinterBinary points PR3287_PATH at something that is not there, so the
// tests do not depend on whether the machine running them happens to have the
// x3270 suite installed.
func noPrinterBinary(t *testing.T) {
	t.Helper()
	t.Setenv("PR3287_PATH", filepath.Join(t.TempDir(), "pr3287-not-installed"))
}

// aPrinterBinary points PR3287_PATH at a real file so the handler gets past
// "not installed" and on to the checks worth testing. Nothing is ever run:
// every case below is refused before a process would start.
func aPrinterBinary(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pr3287")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write the stub binary: %v", err)
	}
	t.Setenv("PR3287_PATH", path)
}

// An installation without pr3287 says so, and names what to install. The
// alternative — a 500 out of a failed exec — tells the operator nothing they
// can act on.
func TestPrinterStartWithoutTheBinary(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	noPrinterBinary(t)

	w := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/printer", token, `{"lu":"PRT001"}`)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "pr3287") {
		t.Errorf("refusal should name the binary to install, got %s", w.Body.String())
	}
}

// The status endpoint answers for a session that has never had a printer,
// because that is the state the panel opens in.
func TestPrinterStatusWithNoPrinter(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	noPrinterBinary(t)

	w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/printer", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	var body struct {
		Available bool          `json:"available"`
		Running   bool          `json:"running"`
		Jobs      []printer.Job `json:"jobs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body %s)", err, w.Body.String())
	}
	if body.Available {
		t.Error("available = true with no pr3287 installed")
	}
	if body.Running {
		t.Error("running = true with no printer started")
	}
	if body.Jobs == nil {
		t.Error("jobs = null, want an empty list — the panel renders a list either way")
	}
}

// An LU name reaches an argument vector, so it is checked rather than trusted.
// The refusal happens before anything is launched.
func TestPrinterStartRefusesAnLUThatWouldReadAsAnOption(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	aPrinterBinary(t)

	w := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/printer", token,
		`{"lu":"-command echo pwned"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
}

// Binding the associated printer needs the host to have bound this session to
// a named LU. When it has not, the message says what to do instead.
func TestPrinterStartAssociateWithoutAnLU(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	aPrinterBinary(t)
	mock.QueryResponses = map[string]string{"LuName": ""}

	w := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/printer", token, `{"associate":true}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Name the printer LU") {
		t.Errorf("refusal should say what to do instead, got %s", w.Body.String())
	}
}

// A bundled sample app is a listener this process opened to demonstrate the
// terminal. It has no printer LU, and its target is a pseudo-host rather than
// an address, so the refusal says that rather than letting pr3287 complain
// about a hostname the operator never typed.
func TestPrinterStartAgainstASampleApp(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	aPrinterBinary(t)
	withSessionLock(sess, func() {
		sess.TargetHost = "sampleapp:petstore"
		sess.TargetPort = 3270
	})

	w := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/printer", token, `{"lu":"PRT001"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "sample application") {
		t.Errorf("refusal should name what is wrong, got %s", w.Body.String())
	}
}

// spoolAJob writes a job into the session's spool the way the helper process
// would, so the reading half can be tested without a host.
func spoolAJob(t *testing.T, app *App, sessionID, content string) printer.Job {
	t.Helper()
	dir, err := app.printerSpoolDir(sessionID)
	if err != nil {
		t.Fatalf("printerSpoolDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	job, err := (printer.Spool{Dir: dir}).Receive(strings.NewReader(content), time.Now())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	return job
}

func TestPrinterJobsListDownloadAndDelete(t *testing.T) {
	app, r, sess, _, token := snapshotTestApp(t)
	noPrinterBinary(t)
	base := "/api/v1/sessions/" + sess.ID
	job := spoolAJob(t, app, sess.ID, "DAILY BALANCE REPORT\n")

	w := doToken(t, r, http.MethodGet, base+"/printer/jobs", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), job.Name) {
		t.Fatalf("list = %s, want the spooled job", w.Body.String())
	}

	w = doToken(t, r, http.MethodGet, base+"/printer/jobs?name="+job.Name, token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("download: status = %d (body %s)", w.Code, w.Body.String())
	}
	if w.Body.String() != "DAILY BALANCE REPORT\n" {
		t.Errorf("download returned %q, want the job's bytes", w.Body.String())
	}
	// Served as a file to save, never rendered: a print job is whatever the
	// host sent, and this origin is not the place to run it as a document.
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", got)
	}

	w = doToken(t, r, http.MethodDelete, base+"/printer/jobs?name="+job.Name, token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("delete: status = %d (body %s)", w.Code, w.Body.String())
	}
	w = doToken(t, r, http.MethodGet, base+"/printer/jobs", token, "")
	if strings.Contains(w.Body.String(), job.Name) {
		t.Errorf("the job is still listed after a delete: %s", w.Body.String())
	}
}

// The job name comes from the request, so the endpoint reaches exactly the
// files this session's own printer wrote and nothing else.
func TestPrinterJobDownloadRefusesAnyOtherPath(t *testing.T) {
	app, r, sess, _, token := snapshotTestApp(t)
	noPrinterBinary(t)
	spoolAJob(t, app, sess.ID, "REPORT")

	secret := filepath.Join(app.baseDir, "users.json")
	if err := os.WriteFile(secret, []byte(`{"accounts":"secret"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	for _, name := range []string{
		"../../users.json",
		"..%2f..%2fusers.json",
		"/etc/passwd",
		"users.json",
	} {
		w := doToken(t, r, http.MethodGet,
			"/api/v1/sessions/"+sess.ID+"/printer/jobs?name="+name, token, "")
		if w.Code == http.StatusOK {
			t.Errorf("download of %q returned 200 with body %q, want a refusal", name, w.Body.String())
		}
	}
}

// A job stays downloadable after the printer that produced it is stopped:
// stopping is something an operator does on purpose, often to rebind, and
// losing the output they just collected would be a surprise.
func TestPrinterStopKeepsTheJobs(t *testing.T) {
	app, r, sess, _, token := snapshotTestApp(t)
	noPrinterBinary(t)
	job := spoolAJob(t, app, sess.ID, "REPORT")

	w := doToken(t, r, http.MethodDelete, "/api/v1/sessions/"+sess.ID+"/printer", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("stop: status = %d (body %s)", w.Code, w.Body.String())
	}
	w = doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/printer/jobs", token, "")
	if !strings.Contains(w.Body.String(), job.Name) {
		t.Errorf("the job went with the stop: %s", w.Body.String())
	}
	// And the panel's own call has to show it too: an operator who stopped the
	// printer is usually stopping it *because* the job they wanted arrived.
	w = doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/printer", token, "")
	if !strings.Contains(w.Body.String(), job.Name) {
		t.Errorf("status after a stop lists no jobs: %s", w.Body.String())
	}
}

// The session ending takes the spool with it. The jobs belong to the session,
// and a directory nothing can reach any more is a disk leak with a name on it.
func TestSessionCleanupRemovesTheSpool(t *testing.T) {
	app, _, sess, _, _ := snapshotTestApp(t)
	spoolAJob(t, app, sess.ID, "REPORT")

	dir, err := app.printerSpoolDir(sess.ID)
	if err != nil {
		t.Fatalf("printerSpoolDir: %v", err)
	}
	// Registered the way a start would, so cleanup has something to find.
	if err := app.printers().begin(&printerSession{Session: sess.ID, dir: dir}); err != nil {
		t.Fatalf("begin: %v", err)
	}

	app.cleanupSession(sess)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the spool directory outlived the session (%v)", err)
	}
	if _, ok := app.printers().get(sess.ID); ok {
		t.Error("the printer is still registered after the session ended")
	}
}

// The printer follows the terminal onto TLS or off it. A host expecting TLS on
// 992 will not talk to a printer session in the clear, and the operator who
// chose a TLS profile did not choose it for half the connection.
func TestTerminalTLSReadsWhatTheTerminalNegotiated(t *testing.T) {
	_, _, _, mock, _ := snapshotTestApp(t)

	cases := map[string]bool{
		"true host.example:992": true,
		"TRUE":                  true,
		"false":                 false,
		"":                      false,
	}
	for answer, want := range cases {
		mock.QueryResponses = map[string]string{"Tls": answer}
		if got := terminalTLS(mock); got != want {
			t.Errorf("terminalTLS(%q) = %v, want %v", answer, got, want)
		}
	}
}

// A spool directory is named from the session ID, so the ID is checked before
// it becomes a path even though this server is the only thing that mints one.
func TestPrinterSpoolDirRefusesAnUnusableSessionID(t *testing.T) {
	app := &App{baseDir: t.TempDir()}
	for _, id := range []string{"", "../escape", "a/b", strings.Repeat("x", 65)} {
		if _, err := app.printerSpoolDir(id); err == nil {
			t.Errorf("printerSpoolDir(%q) was accepted, want a refusal", id)
		}
	}
}
