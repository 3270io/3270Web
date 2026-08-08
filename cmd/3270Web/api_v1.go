package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
	"github.com/jnnngs/3270Web/internal/task"
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
	g.GET("/sessions/:id/query", app.APIQuery)

	// Guided Business Tasks. The catalogue is deployment-wide, so it is not
	// under /sessions; running one needs a connected session, so that is.
	g.GET("/tasks", app.APIListTasks)
	g.POST("/tasks", app.APISaveTask)
	g.POST("/sessions/:id/tasks/run", app.APIRunTask)

	// Skills and instructions are not session-scoped either — the catalogue
	// is the same whichever host you are connected to, and a client will
	// usually want to read it before opening a session at all.
	g.GET("/skills", app.SkillsListHandler)
	g.GET("/skills/load", app.SkillLoadHandler)
	g.GET("/instructions", app.InstructionsListHandler)
	g.GET("/instructions/load", app.InstructionLoadHandler)
	g.GET("/extensions", app.ExtensionsListHandler)

	app.registerAPIv1SessionScoped(r)
}

// APISessionScope resolves the :id path parameter and presents it to the
// handler downstream as the session cookie those handlers already read.
//
// app.getSession reads exactly one thing — the 3270Web_session cookie — so
// naming the session in the path is enough to serve the entire interactive
// surface to token-authenticated, browser-free clients without forking a
// second copy of every handler. SessionManager.GetSession also refreshes
// LastAccess, so a conversation that stays busy keeps its session out of the
// idle reaper's way for free.
func (app *App) APISessionScope() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
			return
		}
		if _, ok := app.SessionManager.GetSession(id); !ok {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		c.Request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: id})
		c.Next()
	}
}

// registerAPIv1SessionScoped exposes the interactive surface — screen
// control, chaos exploration and business understanding — under a session
// named in the path instead of in a cookie.
//
// What is absent is deliberate. Log access, settings, theme writes, app
// restart, file transfer, workflow playback and chaos-run deletion stay on
// the browser surface only: they are administrative or filesystem-facing,
// and nothing an automated client needs to drive an application requires
// them. TestAPIv1DenyList pins that list.
func (app *App) registerAPIv1SessionScoped(r *gin.Engine) {
	g := r.Group("/api/v1/sessions/:id", app.RequireAPIToken(), app.APISessionScope())

	// Screen control. Only what the flat surface above does not already
	// cover: key and submit stay on their documented /sessions/:id/key and
	// /sessions/:id/submit routes rather than being shadowed here, and
	// /write exists alongside /field because it additionally accepts the
	// 1-indexed R<row>C<col>L<len> field key that chaos and business
	// functions quote, which /field does not.
	g.GET("/screen.json", app.ScreenJSONHandler)
	g.POST("/write", app.ScreenWriteHandler)
	g.POST("/connect", app.ScreenConnectHandler)
	g.POST("/cursor", app.ScreenCursorHandler)
	g.POST("/wait", app.ScreenWaitHandler)
	g.GET("/context", app.CopilotContextHandler)

	// Chaos exploration.
	g.POST("/chaos/start", app.ChaosStartHandler)
	g.POST("/chaos/stop", app.ChaosStopHandler)
	g.POST("/chaos/resume", app.ChaosResumeHandler)
	g.POST("/chaos/remove", app.ChaosRemoveHandler)
	g.POST("/chaos/export", app.ChaosExportHandler)
	g.POST("/chaos/report", app.ChaosReportHandler)
	g.POST("/chaos/load", app.ChaosLoadHandler)
	g.GET("/chaos/status", app.ChaosStatusHandler)
	g.GET("/chaos/runs", app.ChaosListRunsHandler)
	g.GET("/chaos/hints", app.ChaosHintsGetHandler)
	g.POST("/chaos/hints", app.ChaosHintsSaveHandler)
	g.GET("/chaos/screen-hints", app.ChaosScreenHintsGetHandler)
	g.POST("/chaos/screen-hints", app.ChaosScreenHintsSaveHandler)
	g.GET("/chaos/screens", app.ChaosScreensListHandler)
	g.POST("/chaos/screens/annotate", app.ChaosScreenAnnotateHandler)
	g.GET("/chaos/insights", app.ChaosInsightsHandler)

	// Business understanding.
	g.GET("/chaos/business/functions", app.ChaosBusinessFunctionsListHandler)
	g.POST("/chaos/business/functions", app.ChaosBusinessFunctionSaveHandler)
	g.POST("/chaos/business/generate-workflow", app.ChaosBusinessGenerateWorkflowHandler)
	g.GET("/chaos/business/overview", app.ChaosBusinessOverviewHandler)
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

// isSampleAppHostname reports whether hostname names a bundled sample app
// or the in-process mock rather than a real TN3270 host.
func isSampleAppHostname(hostname string) bool {
	if _, _, ok := parseSampleAppHost(hostname); ok {
		return true
	}
	return hostname == "mock" || hostname == "demo"
}

// sampleAppsAllowed reports whether the headless API may open a session
// against a bundled sample app.
//
// The sample apps are how someone evaluates 3270Web — or drives it from an
// AI client — without a mainframe to point at, so a flat refusal here means
// the only way to try the API is against production. It stays opt-in, in
// the same shape as ALLOW_LOG_ACCESS, because a sample app is still a
// listener this process starts on the user's behalf.
func sampleAppsAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ALLOW_SAMPLE_APPS"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// APICreateSession starts a new host session and returns its id. Sample-app
// pseudo-hostnames require ALLOW_SAMPLE_APPS; UI users hit those via
// /connect.
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
	if isSampleAppHostname(hostname) {
		if !sampleAppsAllowed() {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "sample-app hostnames require ALLOW_SAMPLE_APPS=1",
			})
			return
		}
		s, err := app.startHostSession(hostname)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"id": s.ID, "host": s.TargetHost, "port": s.TargetPort})
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

// elidedQueryValue is what s3270 puts in the bare Query() response in place of
// its longest fields (Copyright, Proxies, Tasks), meaning "ask for this one by
// name".
const elidedQueryValue = "..."

// parseQueryFields splits s3270's bare Query() response into its fields. The
// response is one "Name: value" per line; a line without a colon is kept under
// its own name with an empty value rather than dropped, so an s3270 that reports
// something in a shape we did not expect still shows up.
//
// It returns the map and the names in the order s3270 gave them, because that
// order is s3270's own grouping and is more useful to read than alphabetical.
func parseQueryFields(raw string) (map[string]string, []string) {
	fields := make(map[string]string)
	order := make([]string, 0, 24)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !found {
			value = ""
		}
		if _, seen := fields[name]; !seen {
			order = append(order, name)
		}
		fields[name] = strings.TrimSpace(value)
	}
	return fields, order
}

// APIQuery surfaces s3270's own Query action: the connection's account of
// itself.
//
// None of it is derivable from the screen, which is the point. A session that
// renders perfectly may still have failed to negotiate TN3270E, or bound a
// different LU from the one that was asked for, and the only place that shows is
// here. It is the difference between "the app looks fine" and "the connection is
// what we specified".
//
// It always issues the *bare* Query, which returns every field the running
// s3270 knows in one command, and answers ?name= out of that response rather
// than by sending the name on. That is not a shortcut, it is the safe design: a
// query keyword this s3270 does not handle can block instead of erroring —
// Tn3270eFunctions does exactly that against a plain TN3270 server — and a
// blocked command on the s3270 pipe is unrecoverable, so the wrapper's only
// answer is to kill the subprocess. One unknown name would take the session with
// it. Deriving the valid names from s3270 itself means no name we did not get
// from s3270 is ever sent to it, and an unknown one can be refused with the list
// of names that would have worked.
func (app *App) APIQuery(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	h := app.sessionHost(s)
	if h == nil || !h.IsConnected() {
		c.JSON(http.StatusConflict, gin.H{"error": "session is not connected"})
		return
	}

	raw, err := h.Query("")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	fields, order := parseQueryFields(raw)

	if name := strings.TrimSpace(c.Query("name")); name != "" {
		for _, known := range order {
			if !strings.EqualFold(known, name) {
				continue
			}
			value := fields[known]
			if value == elidedQueryValue {
				// s3270 abbreviates its longest fields to "..." in the bare
				// response and expects them to be asked for by name. Forwarding
				// the name is safe here in a way that forwarding an arbitrary one
				// is not: this name came out of s3270's own list, so it is a
				// keyword by construction.
				full, err := h.Query(known)
				if err != nil {
					c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
					return
				}
				value = full
			}
			c.JSON(http.StatusOK, gin.H{
				"session": s.ID,
				"name":    known,
				"value":   value,
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "unknown query " + name,
			"available": order,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session":   s.ID,
		"queries":   fields,
		"available": order,
	})
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

/* ---------------------------------------------------------------- */
/* Guided Business Tasks                                             */
/* ---------------------------------------------------------------- */

// APIListTasks returns the task catalogue. This doubles as export: the
// response is exactly the document /tasks/save accepts, so a catalogue can be
// version-controlled and moved between deployments with two curl calls.
func (app *App) APIListTasks(c *gin.Context) {
	tasks, err := app.taskStore().List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not read the task catalogue: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// APISaveTask adds or replaces a task. Validated by the same gate the browser
// and the runner go through, so an imported task cannot be malformed.
func (app *App) APISaveTask(c *gin.Context) {
	var t task.Task
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	if _, err := app.taskStore().Upsert(t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "task": t.Name})
}

// APIRunTask runs a task in the named session and returns the result.
//
// Synchronous, unlike the browser's /tasks/run. The two callers want opposite
// things: a browser needs to show progress and offer Cancel while a
// transaction takes its seconds, so it polls; a bot or a CI job wants the
// answer in the response and would otherwise have to implement a poll loop to
// get it. The run is bounded by the same five-minute ceiling either way.
//
// The task name travels in the body rather than the path. Task names are
// prose — "Account balance enquiry" — and a name containing a slash would
// silently become two path segments.
func (app *App) APIRunTask(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	h := app.sessionHost(s)
	if h == nil || !h.IsConnected() {
		c.JSON(http.StatusConflict, gin.H{"error": "the session is not connected to a host"})
		return
	}

	var payload struct {
		Name       string            `json:"name"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	t, found := app.taskStore().Find(payload.Name)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("there is no task called %q", payload.Name)})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), maxTaskRunDuration)
	defer cancel()

	// Registered in the same per-session store the browser uses. A task drives
	// the one terminal that session owns, so an API run and a browser run must
	// not overlap on it — and this is what makes the two paths mutually
	// exclusive rather than merely unlikely to collide.
	run := &taskRun{Task: t.Name, StartedAt: time.Now(), Total: len(t.Steps), cancel: cancel}
	if err := app.taskRuns().begin(s.ID, run); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	defer app.taskRuns().update(s.ID, func(r *taskRun) { r.done = true })

	runner := &task.Runner{Terminal: h}
	result, err := runner.Run(ctx, t, payload.Parameters)
	if err != nil {
		// A parameter the task rejects is the caller's error, not a failure of
		// the run — nothing was sent to the host.
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	app.taskRuns().update(s.ID, func(r *taskRun) { r.result = result })

	// A task that stopped early is a 200 carrying completed:false, not an HTTP
	// error: the request succeeded, and the body says what the host did. An
	// HTTP status cannot express "step 3 saw the wrong screen", and collapsing
	// it into 500 would throw away the only useful part of the answer.
	c.JSON(http.StatusOK, result)
}
