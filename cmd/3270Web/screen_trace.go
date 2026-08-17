// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// A screen trace records every screen the terminal draws, as it draws it,
// including the ones that were replaced before anyone asked to see them. That
// is what separates it from polling /screen: a host that paints an error and
// immediately paints over it leaves nothing behind for a poller to find, and
// that screen is usually the one the operator needs afterwards.
//
// It is also a file on the server holding everything that crossed the
// display. So it is off unless someone turned it on — ALLOW_SCREEN_TRACE,
// in the same shape as the existing opt-ins for log access and the sample
// apps — the destination is chosen by the server and never by the caller, and
// the reader will only serve a file it started.

// screenTraceDirName is where traces are written, under the app's base
// directory alongside chaos-runs.
const screenTraceDirName = "screen-traces"

// maxScreenTraceBytes bounds what the download endpoint will serve in one
// response. A long-running trace can grow without limit; a reader that
// streamed it unbounded would turn a session someone forgot to stop into a
// memory problem for the server rather than a disk one.
const maxScreenTraceBytes = 16 * 1024 * 1024

// screenTraceIDPattern is what may appear in a trace filename. The session ID
// becomes part of a path, so it is checked rather than trusted, even though
// this server is the only thing that ever makes one.
var screenTraceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// screenTraceAllowed reports whether traces may be started at all.
func screenTraceAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ALLOW_SCREEN_TRACE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// screenTracer is the part of a host that can capture screens to a file.
type screenTracer interface {
	StartScreenTrace(path string, format host.ScreenTraceFormat) error
	StopScreenTrace() error
}

// screenTrace is one running or finished capture.
type screenTrace struct {
	Session   string    `json:"session"`
	Path      string    `json:"-"`
	File      string    `json:"file"`
	Format    string    `json:"format"`
	StartedAt time.Time `json:"started_at"`
	// A pointer so a running trace reports no stop time at all. A zero
	// time.Time is not "empty" to the JSON encoder, so the value type would
	// have put year 1 in the field of every trace still running.
	StoppedAt *time.Time `json:"stopped_at,omitempty"`
	Running   bool       `json:"running"`
}

// screenTraceStore holds at most one trace per session. One is the right
// number: the terminal itself has a single trace destination, so a second
// concurrent trace on the same session would silently redirect the first.
type screenTraceStore struct {
	mu     sync.Mutex
	traces map[string]*screenTrace
}

func newScreenTraceStore() *screenTraceStore {
	return &screenTraceStore{traces: make(map[string]*screenTrace)}
}

func (s *screenTraceStore) get(sessionID string) (screenTrace, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.traces[sessionID]
	if !ok {
		return screenTrace{}, false
	}
	return *t, true
}

// begin records a starting trace, refusing if one is already running for the
// session.
func (s *screenTraceStore) begin(t *screenTrace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.traces[t.Session]; ok && existing.Running {
		return fmt.Errorf("a screen trace is already running in this session")
	}
	s.traces[t.Session] = t
	return nil
}

// end marks the session's trace stopped and returns it.
func (s *screenTraceStore) end(sessionID string) (screenTrace, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.traces[sessionID]
	if !ok {
		return screenTrace{}, false
	}
	t.Running = false
	now := time.Now()
	t.StoppedAt = &now
	return *t, true
}

func (s *screenTraceStore) forget(sessionID string) (screenTrace, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.traces[sessionID]
	delete(s.traces, sessionID)
	if !ok {
		return screenTrace{}, false
	}
	return *t, true
}

// screenTraces lazily creates the store.
func (app *App) screenTraces() *screenTraceStore {
	app.screenTraceOnce.Do(func() {
		if app.screenTraceStore == nil {
			app.screenTraceStore = newScreenTraceStore()
		}
	})
	return app.screenTraceStore
}

// screenTraceDir returns the directory traces are written to.
func (app *App) screenTraceDir() string {
	base := strings.TrimSpace(app.baseDir)
	if base == "" {
		base = resolveStateDir()
	}
	return filepath.Join(base, screenTraceDirName)
}

// screenTracePath builds the destination for one session's trace. The name is
// the server's alone — a caller never supplies any part of it — which is what
// keeps this endpoint from being a way to write a file of the caller's
// choosing anywhere the server can reach.
func (app *App) screenTracePath(sessionID string, format host.ScreenTraceFormat) (string, error) {
	if !screenTraceIDPattern.MatchString(sessionID) {
		return "", fmt.Errorf("session id is not in a form that can name a file")
	}
	ext := ".txt"
	if format == host.ScreenTraceHTML {
		ext = ".html"
	}
	name := fmt.Sprintf("%s-%s%s", sessionID, time.Now().UTC().Format("20060102T150405Z"), ext)
	return filepath.Join(app.screenTraceDir(), name), nil
}

// sessionScreenTracer resolves the session's host to a screenTracer, or
// writes the reason it could not and returns nil.
func (app *App) sessionScreenTracer(c *gin.Context, s *session.Session) screenTracer {
	h := app.sessionHost(s)
	if h == nil {
		c.JSON(http.StatusConflict, gin.H{"error": errSessionHostMissing.Error()})
		return nil
	}
	if !h.IsConnected() {
		c.JSON(http.StatusConflict, gin.H{"error": errSessionNotConnected.Error()})
		return nil
	}
	t, ok := h.(screenTracer)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "this terminal does not support screen tracing"})
		return nil
	}
	return t
}

// APIStartScreenTrace handles POST /api/v1/sessions/:id/screen-trace.
func (app *App) APIStartScreenTrace(c *gin.Context) {
	if !screenTraceAllowed() {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "screen tracing writes every screen to a file on the server and requires ALLOW_SCREEN_TRACE=1",
		})
		return
	}
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	var body struct {
		Format string `json:"format"`
	}
	_ = c.ShouldBindJSON(&body) // body is optional; text is the default

	format := host.ScreenTraceText
	switch strings.ToLower(strings.TrimSpace(body.Format)) {
	case "", "text":
	case "html":
		format = host.ScreenTraceHTML
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unsupported format %q (allowed: text, html)", body.Format)})
		return
	}

	tracer := app.sessionScreenTracer(c, s)
	if tracer == nil {
		return
	}
	path, err := app.screenTracePath(s.ID, format)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create the screen-trace directory: " + err.Error()})
		return
	}

	trace := &screenTrace{
		Session:   s.ID,
		Path:      path,
		File:      filepath.Base(path),
		Format:    string(format),
		StartedAt: time.Now(),
		Running:   true,
	}
	// Registered before the terminal is told, so two concurrent starts cannot
	// both get past the check and leave the second one's file orphaned with
	// the store pointing at the first.
	if err := app.screenTraces().begin(trace); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err := tracer.StartScreenTrace(path, format); err != nil {
		app.screenTraces().forget(s.ID)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, trace)
}

// APIStopScreenTrace handles DELETE /api/v1/sessions/:id/screen-trace.
func (app *App) APIStopScreenTrace(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	tracer := app.sessionScreenTracer(c, s)
	if tracer == nil {
		return
	}
	if err := tracer.StopScreenTrace(); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	trace, existed := app.screenTraces().end(s.ID)
	if !existed {
		c.JSON(http.StatusOK, gin.H{"ok": true, "running": false})
		return
	}
	c.JSON(http.StatusOK, trace)
}

// APIScreenTraceStatus handles GET /api/v1/sessions/:id/screen-trace, and
// with ?download=1 serves the captured file.
//
// Only a file this server started is served, and it is named from the store
// rather than from the request, so the endpoint cannot be talked into reading
// anything else under the trace directory or outside it.
func (app *App) APIScreenTraceStatus(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	trace, found := app.screenTraces().get(s.ID)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "this session has no screen trace"})
		return
	}
	if !isTruthyParam(c.Query("download")) {
		c.JSON(http.StatusOK, trace)
		return
	}

	info, err := os.Stat(trace.Path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "the trace file is not readable: " + err.Error()})
		return
	}
	if info.Size() > maxScreenTraceBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": fmt.Sprintf("the trace file is %d bytes, larger than the %d-byte download limit; read it from %s on the server",
				info.Size(), maxScreenTraceBytes, screenTraceDirName),
		})
		return
	}
	// Served as an attachment whatever the format. An HTML trace is a page
	// built out of whatever the host put on the screen, and rendering it
	// inline would be running that as a document on this origin.
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", trace.File))
	c.Header("Content-Type", "application/octet-stream")
	c.File(trace.Path)
}

// isTruthyParam reads the spellings of "yes" a query string turns up with.
func isTruthyParam(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
