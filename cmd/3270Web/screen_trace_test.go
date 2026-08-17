// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Tracing writes a file holding everything that crossed the display,
// including whatever was typed into a field the host did not mark hidden.
// Off unless someone turned it on, and the refusal names the flag.
func TestScreenTraceIsOffUnlessEnabled(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	t.Setenv("ALLOW_SCREEN_TRACE", "")

	w := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/screen-trace", token, `{}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "ALLOW_SCREEN_TRACE") {
		t.Errorf("refusal should name the flag that enables this, got %s", w.Body.String())
	}
}

func TestScreenTraceStartStatusStop(t *testing.T) {
	app, r, sess, mock, token := snapshotTestApp(t)
	t.Setenv("ALLOW_SCREEN_TRACE", "1")
	base := "/api/v1/sessions/" + sess.ID

	w := doToken(t, r, http.MethodPost, base+"/screen-trace", token, `{}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("start: status = %d (body %s)", w.Code, w.Body.String())
	}
	if !mock.TraceRunning {
		t.Fatal("the terminal was never told to start tracing")
	}

	// The destination is the server's alone. A caller supplies no part of it,
	// which is what stops this being a way to write a file anywhere the server
	// can reach.
	wantDir := filepath.Join(app.baseDir, screenTraceDirName)
	if filepath.Dir(mock.TracePath) != wantDir {
		t.Errorf("trace written to %q, want a file under %q", mock.TracePath, wantDir)
	}
	if !strings.Contains(filepath.Base(mock.TracePath), sess.ID) {
		t.Errorf("trace file %q should carry the session it belongs to", mock.TracePath)
	}

	if w := doToken(t, r, http.MethodPost, base+"/screen-trace", token, `{}`); w.Code != http.StatusConflict {
		t.Errorf("second start: status = %d, want 409 — the terminal has one trace destination", w.Code)
	}

	w = doToken(t, r, http.MethodGet, base+"/screen-trace", token, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"running":true`) {
		t.Fatalf("status: %d (body %s)", w.Code, w.Body.String())
	}

	w = doToken(t, r, http.MethodGet, base+"/screen-trace?download=1", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("download: status = %d (body %s)", w.Code, w.Body.String())
	}
	// An HTML trace is a page built out of whatever the host painted; served
	// inline it would run as a document on this origin.
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}

	w = doToken(t, r, http.MethodDelete, base+"/screen-trace", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("stop: status = %d (body %s)", w.Code, w.Body.String())
	}
	if mock.TraceRunning {
		t.Error("the terminal was never told to stop")
	}
	if !strings.Contains(w.Body.String(), `"running":false`) {
		t.Errorf("stop should answer with the finished trace, got %s", w.Body.String())
	}

	// Stopping again succeeds: a cleanup path should not have to know whether
	// it is cleaning up.
	if w := doToken(t, r, http.MethodDelete, base+"/screen-trace", token, ""); w.Code != http.StatusOK {
		t.Errorf("repeat stop: status = %d, want 200", w.Code)
	}
}

func TestScreenTraceHTMLFormat(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	t.Setenv("ALLOW_SCREEN_TRACE", "1")

	w := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/screen-trace", token, `{"format":"html"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("start: status = %d (body %s)", w.Code, w.Body.String())
	}
	if filepath.Ext(mock.TracePath) != ".html" {
		t.Errorf("html trace written to %q, want a .html file", mock.TracePath)
	}
}

func TestScreenTraceRejectsUnknownFormat(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	t.Setenv("ALLOW_SCREEN_TRACE", "1")

	w := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/screen-trace", token, `{"format":"pdf"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
}

func TestScreenTraceStatusIs404WhenNoneWasStarted(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)

	if w := doToken(t, r, http.MethodGet, "/api/v1/sessions/"+sess.ID+"/screen-trace", token, ""); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// Ending a session drops this server's record of where the trace went. The
// file stays on disk for whoever asked for it — the subprocess exiting is
// what closes it.
func TestScreenTraceRecordIsDroppedWithTheSession(t *testing.T) {
	app, r, sess, _, token := snapshotTestApp(t)
	t.Setenv("ALLOW_SCREEN_TRACE", "1")
	doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/screen-trace", token, `{}`)

	app.cleanupSession(sess)

	if _, found := app.screenTraces().get(sess.ID); found {
		t.Error("session cleanup left a screen-trace record behind")
	}
}
