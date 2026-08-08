package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// registerAPIv1 wires the public /api/v1/* surface used by RPA bots, CI
// jobs, and other non-browser clients. All routes require the
// API_TOKEN environment variable to be set and an Authorization: Bearer
// header that matches it.
func (app *App) registerAPIv1(r *gin.Engine) {
	g := r.Group("/api/v1", app.RequireAPIToken())
	g.GET("/sessions", app.APIListSessions)
	g.POST("/sessions", app.APICreateSession)
	g.DELETE("/sessions/:id", app.APIDeleteSession)
	g.GET("/sessions/:id/screen", app.APIGetScreen)
	g.POST("/sessions/:id/key", app.APISendKey)
	g.POST("/sessions/:id/field", app.APIWriteField)
	g.POST("/sessions/:id/submit", app.APISubmit)
	g.POST("/sessions/:id/profile", app.APIProfileHandler)
	g.GET("/sessions/:id/profile", app.APIProfileGetHandler)
}

// RequireAPIToken enforces Bearer-token auth against the API_TOKEN env var.
// When API_TOKEN is unset or empty, the entire /api/v1 surface returns
// 503 so it can't be accidentally exposed without explicit opt-in.
func (app *App) RequireAPIToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(os.Getenv("API_TOKEN"))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "API disabled: API_TOKEN not configured",
			})
			return
		}
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing Bearer token",
			})
			return
		}
		got := strings.TrimSpace(header[len(prefix):])
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid token",
			})
			return
		}
		c.Next()
	}
}

// apiSessionFromPath resolves the session referenced by the :id path
// parameter and writes a 404 if it doesn't exist. Returns ok=false when
// the request has already been answered with an error.
func (app *App) apiSessionFromPath(c *gin.Context) (*session.Session, bool) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
		return nil, false
	}
	s, ok := app.SessionManager.GetSession(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return nil, false
	}
	return s, true
}

// APIListSessions returns a snapshot of all active sessions.
func (app *App) APIListSessions(c *gin.Context) {
	sessions := app.SessionManager.ListSessions()
	out := make([]gin.H, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		s.Lock()
		out = append(out, gin.H{
			"id":          s.ID,
			"host":        s.TargetHost,
			"port":        s.TargetPort,
			"last_access": s.LastAccess,
		})
		s.Unlock()
	}
	c.JSON(http.StatusOK, gin.H{"sessions": out})
}

// APICreateSession starts a new host session and returns its id. The API
// rejects sample-app pseudo-hostnames; UI users hit those via /connect.
func (app *App) APICreateSession(c *gin.Context) {
	var body struct {
		Host string `json:"host"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hostname := strings.TrimSpace(body.Host)
	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host is required"})
		return
	}
	if _, _, ok := parseSampleAppHost(hostname); ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sample-app hostnames are not allowed via the API"})
		return
	}
	if hostname == "mock" || hostname == "demo" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sample-app hostnames are not allowed via the API"})
		return
	}
	if !isValidHostname(hostname) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid hostname format: %q", hostname)})
		return
	}
	s, err := app.startHostSession(hostname)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":   s.ID,
		"host": s.TargetHost,
		"port": s.TargetPort,
	})
}

// APIDeleteSession terminates a session and removes it from the manager.
// Deleting a session that's already gone (never existed, or a repeat of
// this same call) is treated as success rather than 404 — DELETE should be
// idempotent, and a client retrying a dropped response shouldn't see an
// error for a delete that already took effect.
func (app *App) APIDeleteSession(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
		return
	}
	app.SessionManager.RemoveSession(id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// APIGetScreen refreshes and returns the current screen as JSON.
func (app *App) APIGetScreen(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	h := app.sessionHost(s)
	if err := h.UpdateScreen(); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "update screen: " + err.Error()})
		return
	}
	screen := hostScreenSnapshot(h)
	if screen == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "screen unavailable"})
		return
	}
	if rows, cols, ok := app.modelDimensions(); ok {
		screen = limitScreenForDisplay(screen, rows, cols)
	}
	c.JSON(http.StatusOK, screenToPublicJSON(screen))
}

// APISendKey sends one AID or navigation key without first submitting any
// pending field writes.
func (app *App) APISendKey(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Key) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key is required"})
		return
	}
	key, ok := normalizeKey(body.Key)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unrecognized key %q", body.Key)})
		return
	}
	if err := app.sessionHost(s).SendKey(key); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "key": key})
}

// APIWriteField writes text into the input field that contains (row, col).
// Matches the CR/LF/TAB rejection the Copilot ScreenWriteHandler enforces.
func (app *App) APIWriteField(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	var body struct {
		Row  int    `json:"row"`
		Col  int    `json:"col"`
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Row < 0 || body.Col < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "row/col must be >= 0"})
		return
	}
	if host.ContainsForbiddenFieldText(body.Text) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text must not contain CR/LF/TAB"})
		return
	}
	if err := app.sessionHost(s).WriteStringAt(body.Row, body.Col, body.Text); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// APISubmit submits any modified fields with an Enter (or the AID key
// supplied in the body) and returns the resulting screen.
func (app *App) APISubmit(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	var body struct {
		AID string `json:"aid"`
	}
	_ = c.ShouldBindJSON(&body) // body is optional
	h := app.sessionHost(s)
	if err := h.SubmitScreen(); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	aid := strings.TrimSpace(body.AID)
	if aid == "" {
		aid = "Enter"
	}
	normalizedAID, ok := normalizeKey(aid)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unrecognized aid %q", aid)})
		return
	}
	if err := h.SendKey(normalizedAID); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if err := h.UpdateScreen(); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "update screen: " + err.Error()})
		return
	}
	screen := hostScreenSnapshot(h)
	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"aid":    aid,
		"screen": screenToPublicJSON(screen),
	})
}

// screenToPublicJSON renders s with stable snake_case keys for external
// consumers. It deliberately does not edit the AI-facing screenToJSON
// shape, whose camelCase keys the chat panel depends on.
//
// Hidden fields are redacted here exactly as they are there — see
// screen_redaction.go. This API is reachable by any bearer-token holder and
// is what an MCP client reads, so a password left in `value` or in `text`
// would travel further than the terminal it was typed into.
func screenToPublicJSON(s *host.Screen) gin.H {
	if s == nil {
		return gin.H{}
	}
	fields := make([]gin.H, 0, len(s.Fields))
	for _, f := range s.Fields {
		if f == nil {
			continue
		}
		fields = append(fields, gin.H{
			"start_row": f.StartY,
			"start_col": f.StartX,
			"end_row":   f.EndY,
			"end_col":   f.EndX,
			"value":     visibleFieldValue(f),
			"protected": f.IsProtected(),
			"numeric":   f.IsNumeric(),
			"hidden":    f.IsHidden(),
			"length":    fieldLength(f),
		})
	}
	out := gin.H{
		"width":     s.Width,
		"height":    s.Height,
		"text":      redactHiddenFieldText(s),
		"formatted": s.IsFormatted,
		"fields":    fields,
		"status":    strings.TrimSpace(s.Status),
	}
	if state, ok := s.StatusKeyboardState(); ok {
		out["kbd_lock"] = state
	}
	if row, col, ok := s.StatusCursor(); ok {
		out["cursor"] = gin.H{"row": row, "col": col}
	}
	return out
}
