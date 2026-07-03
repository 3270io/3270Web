package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/profiler"
)

// profileCache holds the most recent CompatibilityProfile per session ID so a
// session can run a probe once and read it back without re-querying the host.
// Cleared when a session ends (see deleteProfileForSession wired in main.go).
type profileCache struct {
	mu   sync.RWMutex
	data map[string]*profiler.CompatibilityProfile
}

func newProfileCache() *profileCache {
	return &profileCache{data: make(map[string]*profiler.CompatibilityProfile)}
}

func (c *profileCache) get(sessionID string) *profiler.CompatibilityProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data[sessionID]
}

func (c *profileCache) set(sessionID string, p *profiler.CompatibilityProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[sessionID] = p
}

func (c *profileCache) delete(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, sessionID)
}

// profileRequestBody is the optional body shared by all probe endpoints.
type profileRequestBody struct {
	INDFileProbe     bool `json:"ind_file_probe,omitempty"`
	CollectRaw       bool `json:"collect_raw,omitempty"`
	PerActionTimeout int  `json:"per_action_timeout_ms,omitempty"`
}

// probeSessionTimeout caps the overall HTTP request time spent in Probe.
const probeSessionTimeout = 30 * time.Second

// runProbeForSessionID is the shared probe-and-cache path used by both the
// cookie-auth (/profile) handler and the Bearer-auth API handler.
// Returns the populated profile, an HTTP status code, and an error message
// suitable for the caller to surface.
func (app *App) runProbeForSessionID(ctx context.Context, sessionID string, body profileRequestBody) (*profiler.CompatibilityProfile, int, error) {
	s, ok := app.SessionManager.GetSession(sessionID)
	if !ok {
		return nil, http.StatusNotFound, errSessionNotFound
	}
	h := app.sessionHost(s)
	if h == nil {
		return nil, http.StatusConflict, errSessionHostMissing
	}
	if !h.IsConnected() {
		return nil, http.StatusConflict, errSessionNotConnected
	}

	opts := profiler.ProbeOptions{
		Tool:         "3270Web",
		Version:      app.versionString(),
		CollectRaw:   body.CollectRaw,
		INDFileProbe: body.INDFileProbe,
		Host:         s.TargetHost,
		Port:         s.TargetPort,
	}
	if body.PerActionTimeout > 0 {
		opts.PerActionTimeout = time.Duration(body.PerActionTimeout) * time.Millisecond
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeSessionTimeout)
	defer cancel()
	p, err := profiler.Probe(probeCtx, h, opts)
	if err != nil {
		if probeCtx.Err() == context.DeadlineExceeded {
			return nil, http.StatusGatewayTimeout, err
		}
		return nil, http.StatusBadGateway, err
	}
	if app.profiles == nil {
		app.profiles = newProfileCache()
	}
	app.profiles.set(sessionID, p)
	return p, http.StatusOK, nil
}

// versionString returns a coarse version label embedded in the profile. The
// codebase does not currently expose a single build-version constant, so the
// schema-version string doubles as the profiler version for now.
func (app *App) versionString() string {
	return "1.0.0"
}

// ProfileHandler handles POST /profile — runs a probe against the caller's
// active session (cookie-auth) and returns the resulting CompatibilityProfile
// JSON.
func (app *App) ProfileHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body profileRequestBody
	_ = c.ShouldBindJSON(&body) // body is optional

	p, status, err := app.runProbeForSessionID(c.Request.Context(), s.ID, body)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

// ProfileGetHandler handles GET /profile — returns the cached profile for the
// caller's active session, or 404 if no probe has been run yet.
func (app *App) ProfileGetHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if app.profiles == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no cached profile for session"})
		return
	}
	p := app.profiles.get(s.ID)
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no cached profile for session"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// APIProfileHandler handles POST /api/v1/sessions/:id/profile — Bearer-auth
// variant used by external tools (CI, fleet probes, 3270Connect dashboards).
func (app *App) APIProfileHandler(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
		return
	}
	var body profileRequestBody
	_ = c.ShouldBindJSON(&body)

	p, status, err := app.runProbeForSessionID(c.Request.Context(), id, body)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

// APIProfileGetHandler handles GET /api/v1/sessions/:id/profile — Bearer-auth
// variant of the cached-profile getter.
func (app *App) APIProfileGetHandler(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
		return
	}
	if _, ok := app.SessionManager.GetSession(id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if app.profiles == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no cached profile for session"})
		return
	}
	p := app.profiles.get(id)
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no cached profile for session"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// Sentinel errors so handlers can map to consistent HTTP status codes.
var (
	errSessionNotFound     = sentinelErr("session not found")
	errSessionHostMissing  = sentinelErr("session has no host")
	errSessionNotConnected = sentinelErr("session host is not connected")
)

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }
