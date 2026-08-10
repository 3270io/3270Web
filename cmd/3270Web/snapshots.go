package main

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// A snapshot is the screen frozen and kept under a name, so it can be
// compared with a later one. That is what makes a regression test possible
// against a green screen: capture the screen a flow is supposed to land on,
// run the flow again tomorrow, and ask what changed. The answer is a list of
// rows, not a yes/no, which is the difference between a test that tells you
// it failed and one that tells you why.
//
// They live in memory for the life of the session. A snapshot is a working
// note taken during a run, not a record — a caller that wants to keep one
// past the session reads it back and stores it itself, and nothing that
// crossed a terminal gets written to the server's disk without someone asking
// for that in as many words. See screen_trace.go, where someone does.

// maxSnapshotsPerSession bounds what one session can accumulate. A snapshot
// is a screen's worth of text, so the ceiling is about memory discipline
// rather than a real limit anyone will meet by hand.
const maxSnapshotsPerSession = 32

// maxSnapshotNameLen bounds a snapshot name.
const maxSnapshotNameLen = 64

// snapshotNamePattern is what a snapshot may be called. Names are quoted back
// in JSON and used as map keys, never as paths, so this is about keeping them
// legible rather than about traversal.
var snapshotNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]*$`)

// validateSnapshotName reports why a name is unusable, or nil.
func validateSnapshotName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > maxSnapshotNameLen {
		return fmt.Errorf("name must be %d characters or fewer", maxSnapshotNameLen)
	}
	if !snapshotNamePattern.MatchString(name) {
		return fmt.Errorf("name may contain only letters, digits, spaces, dots, dashes and underscores, and must start with a letter or digit")
	}
	return nil
}

// namedSnapshot is a snapshot with the name and time it was taken under.
type namedSnapshot struct {
	Name     string         `json:"name"`
	TakenAt  time.Time      `json:"taken_at"`
	Snapshot *host.Snapshot `json:"snapshot"`
}

// snapshotStore holds the snapshots taken in each session.
type snapshotStore struct {
	mu   sync.Mutex
	byID map[string]map[string]*namedSnapshot
}

func newSnapshotStore() *snapshotStore {
	return &snapshotStore{byID: make(map[string]map[string]*namedSnapshot)}
}

// put stores snap under name, replacing any snapshot already held under it.
// Replacing rather than refusing is deliberate: re-capturing "before" at the
// top of a loop is the ordinary way to use this, and a caller should not have
// to delete first.
func (s *snapshotStore) put(sessionID, name string, snap *host.Snapshot) (*namedSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.byID[sessionID]
	if held == nil {
		held = make(map[string]*namedSnapshot)
		s.byID[sessionID] = held
	}
	if _, replacing := held[name]; !replacing && len(held) >= maxSnapshotsPerSession {
		return nil, fmt.Errorf("this session already holds %d snapshots; delete one before taking another", maxSnapshotsPerSession)
	}
	entry := &namedSnapshot{Name: name, TakenAt: time.Now(), Snapshot: snap}
	held[name] = entry
	return entry, nil
}

// get returns the snapshot held under name.
func (s *snapshotStore) get(sessionID, name string) (*namedSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.byID[sessionID][name]
	return entry, ok
}

// list returns every snapshot the session holds, oldest first.
func (s *snapshotStore) list(sessionID string) []*namedSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.byID[sessionID]
	out := make([]*namedSnapshot, 0, len(held))
	for _, entry := range held {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TakenAt.Before(out[j].TakenAt) })
	return out
}

// remove drops one snapshot and reports whether it was there.
func (s *snapshotStore) remove(sessionID, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.byID[sessionID]
	if _, ok := held[name]; !ok {
		return false
	}
	delete(held, name)
	if len(held) == 0 {
		delete(s.byID, sessionID)
	}
	return true
}

// forget drops everything a session held. Called from cleanupSession; without
// it the store keeps a screen's worth of text per snapshot for the life of
// the process, for sessions that ended hours ago.
func (s *snapshotStore) forget(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, sessionID)
}

// snapshots lazily creates the store.
func (app *App) snapshots() *snapshotStore {
	app.snapshotStoreOnce.Do(func() {
		if app.snapshotStore == nil {
			app.snapshotStore = newSnapshotStore()
		}
	})
	return app.snapshotStore
}

// snapshotter is the part of a host that can freeze its display. Declared
// here rather than added to host.Host so the fakes in other packages, which
// only ever drive a terminal, do not have to grow a method to satisfy it.
type snapshotter interface {
	Snap() (*host.Snapshot, error)
}

// captureSnapshot freezes the session's display, or explains why it could
// not. The returned error is already phrased for the caller.
func (app *App) captureSnapshot(s *session.Session) (*host.Snapshot, int, error) {
	h := app.sessionHost(s)
	if h == nil {
		return nil, http.StatusConflict, errSessionHostMissing
	}
	if !h.IsConnected() {
		return nil, http.StatusConflict, errSessionNotConnected
	}
	snapper, ok := h.(snapshotter)
	if !ok {
		return nil, http.StatusNotImplemented, fmt.Errorf("this terminal does not support snapshots")
	}
	snap, err := snapper.Snap()
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return snap, http.StatusOK, nil
}

// writeTakeSnapshot freezes the screen and keeps it under a name.
func (app *App) writeTakeSnapshot(c *gin.Context, s *session.Session) {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if err := validateSnapshotName(name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	snap, code, err := app.captureSnapshot(s)
	if err != nil {
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	entry, err := app.snapshots().put(s.ID, name, snap)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, entry)
}

// writeSnapshotList answers a listing request. With ?name= it returns that one
// snapshot in full; without, the list, each with its text so a caller can diff
// them itself if it would rather not ask the server to.
func (app *App) writeSnapshotList(c *gin.Context, s *session.Session) {
	if name := strings.TrimSpace(c.Query("name")); name != "" {
		entry, found := app.snapshots().get(s.ID, name)
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("this session holds no snapshot called %q", name)})
			return
		}
		c.JSON(http.StatusOK, entry)
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": app.snapshots().list(s.ID)})
}

// writeSnapshotDelete drops one snapshot. Deleting one that is not there
// succeeds, so a retried request does not report a failure for a delete that
// already took effect.
func (app *App) writeSnapshotDelete(c *gin.Context, s *session.Session) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	app.snapshots().remove(s.ID, name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// writeSnapshotDiff compares two screens.
//
// "right" may be omitted, and then the comparison is against the screen as it
// stands now. That is the shape the feature is actually used in — capture a
// screen, do something, ask what moved — and making the caller take a second
// snapshot first would only add a name it has no use for.
func (app *App) writeSnapshotDiff(c *gin.Context, s *session.Session) {
	var body struct {
		Left  string `json:"left"`
		Right string `json:"right"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	leftName := strings.TrimSpace(body.Left)
	if leftName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "left is required"})
		return
	}
	left, found := app.snapshots().get(s.ID, leftName)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("this session holds no snapshot called %q", leftName)})
		return
	}

	rightName := strings.TrimSpace(body.Right)
	var right *host.Snapshot
	if rightName == "" {
		snap, code, err := app.captureSnapshot(s)
		if err != nil {
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		right = snap
		rightName = "(live screen)"
	} else {
		entry, found := app.snapshots().get(s.ID, rightName)
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("this session holds no snapshot called %q", rightName)})
			return
		}
		right = entry.Snapshot
	}

	diff := host.DiffSnapshots(left.Snapshot, right)
	c.JSON(http.StatusOK, gin.H{
		"left":      leftName,
		"right":     rightName,
		"identical": diff.Identical,
		"lines":     diff.Lines,
	})
}

// One implementation, two doors, as with connection details and the display
// toggles: the browser arrives with its session cookie, the API with a bearer
// token. The cookie door exists because the chat panel runs its tool loop in
// the page, and a tool the assistant cannot reach is a capability it will tell
// the user this build does not have.

// SnapshotTakeHandler handles POST /screen/snapshots — the cookie door.
func (app *App) SnapshotTakeHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writeTakeSnapshot(c, s)
	}
}

// SnapshotListHandler handles GET /screen/snapshots — the cookie door.
func (app *App) SnapshotListHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writeSnapshotList(c, s)
	}
}

// SnapshotDeleteHandler handles DELETE /screen/snapshots?name= — the cookie
// door.
func (app *App) SnapshotDeleteHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writeSnapshotDelete(c, s)
	}
}

// SnapshotDiffHandler handles POST /screen/snapshots/diff — the cookie door.
func (app *App) SnapshotDiffHandler(c *gin.Context) {
	if s := app.browserSessionOr404(c); s != nil {
		app.writeSnapshotDiff(c, s)
	}
}

// The API door.

// APITakeSnapshot handles POST /api/v1/sessions/:id/snapshots.
func (app *App) APITakeSnapshot(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		app.writeTakeSnapshot(c, s)
	}
}

// APIListSnapshots handles GET /api/v1/sessions/:id/snapshots.
func (app *App) APIListSnapshots(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		app.writeSnapshotList(c, s)
	}
}

// APIDeleteSnapshot handles DELETE /api/v1/sessions/:id/snapshots?name=.
func (app *App) APIDeleteSnapshot(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		app.writeSnapshotDelete(c, s)
	}
}

// APIDiffSnapshots handles POST /api/v1/sessions/:id/snapshots/diff.
func (app *App) APIDiffSnapshots(c *gin.Context) {
	if s, ok := app.apiSessionFromPath(c); ok {
		app.writeSnapshotDiff(c, s)
	}
}
