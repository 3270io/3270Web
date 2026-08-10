package main

import (
	"context"
	"errors"
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

// registerAPIv1 wires the public /api/v1/* surface used by RPA bots, CI jobs,
// and other non-browser clients. Every route requires an Authorization:
// Bearer header carrying a token the instance recognises — see
// authenticateAPIToken for which token that is in which deployment shape.
func (app *App) registerAPIv1(r *gin.Engine) {
	// CORS runs ahead of the token check so that a preflight — which a browser
	// sends without the Authorization header, by specification — is answered
	// rather than rejected as unauthenticated. Nothing is granted by it: the
	// real request that follows still has to carry the token. See
	// embedding.go.
	g := r.Group("/api/v1", EmbedCORSMiddleware(), app.RequireAPIToken())
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

	// The task catalogue and the host presets as one document, so a
	// deployment's configuration moves in one call each way instead of one
	// call per entry. GET returns exactly what POST accepts. See library.go.
	g.GET("/library", app.LibraryExportHandler)
	g.POST("/library", app.LibraryImportHandler)

	// Skills and instructions are not session-scoped either — the catalogue
	// is the same whichever host you are connected to, and a client will
	// usually want to read it before opening a session at all.
	g.GET("/skills", app.SkillsListHandler)
	g.GET("/skills/load", app.SkillLoadHandler)
	g.GET("/instructions", app.InstructionsListHandler)
	g.GET("/instructions/load", app.InstructionLoadHandler)
	g.GET("/extensions", app.ExtensionsListHandler)

	app.registerAPIv1SessionScoped(r)
	registerAPIv1Preflight(r)
}

// registerAPIv1Preflight gives every API path an OPTIONS route.
//
// A browser preflights a cross-origin call with OPTIONS to the same path, and
// a router matches methods before paths — so without this the preflight 404s
// before any middleware runs, and the console reports a CORS failure on a
// surface that is configured correctly. The routes are derived from what was
// just registered rather than listed again here, because a hand-maintained
// second list is a list that goes stale the first time somebody adds an
// endpoint.
//
// A single wildcard would have been shorter and is wrong: it claims the whole
// subtree, and the MCP endpoint mounted under /api/v1 later serves its own
// OPTIONS. Path by path, nothing is claimed that was not already ours.
func registerAPIv1Preflight(r *gin.Engine) {
	existing := r.Routes()
	seen := make(map[string]bool, len(existing))
	for _, route := range existing {
		if route.Method == http.MethodOptions {
			seen[route.Path] = true
		}
	}
	for _, route := range existing {
		if !strings.HasPrefix(route.Path, "/api/v1/") || seen[route.Path] {
			continue
		}
		seen[route.Path] = true
		r.OPTIONS(route.Path, EmbedCORSMiddleware(), func(c *gin.Context) {
			// Unreachable in practice: the CORS middleware answers a preflight
			// and aborts either way. This is what gives the router something to
			// match so that it runs at all.
			c.Status(http.StatusNoContent)
		})
	}
}

// APISessionScope resolves the :id path parameter and points the handlers
// downstream at that session.
//
// app.getSession reads one session per request, so naming the session in the
// path is enough to serve the entire interactive surface to
// token-authenticated, browser-free clients without forking a second copy of
// every handler. SessionManager.GetSession also refreshes LastAccess, so a
// conversation that stays busy keeps its session out of the idle reaper's way
// for free.
//
// The scoped ID is set on the request context rather than appended as a
// cookie. Appending meant a client that sent both a cookie and a path
// parameter had the path validated here and the cookie used downstream —
// the check and the effect could disagree about which session was in play.
func (app *App) APISessionScope() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
			return
		}
		s, ok := app.SessionManager.GetSession(id)
		if !ok {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		// Same answer for "no such session" and "not yours", so a caller
		// cannot use the difference to discover which IDs are real.
		if !app.mayUseSession(c, s) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		scopeToSession(c, id)
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
	g := r.Group("/api/v1/sessions/:id", EmbedCORSMiddleware(), app.RequireAPIToken(), app.APISessionScope())

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

	// Point-in-time screen copies, and the comparison between two of them.
	// This is the regression-testing surface: capture the screen a flow is
	// meant to land on, run it again later, ask what moved. See snapshots.go.
	g.POST("/snapshots", app.APITakeSnapshot)
	g.GET("/snapshots", app.APIListSnapshots)
	g.DELETE("/snapshots", app.APIDeleteSnapshot)
	g.POST("/snapshots/diff", app.APIDiffSnapshots)

	// The terminal's own display toggles — monocase, crosshair, cursor blink.
	// A narrow allowlist; see internal/host/toggles.go for why it is narrow.
	g.GET("/toggles", app.APIToggles)
	g.POST("/toggles", app.APISetToggle)

	// The HLLAPI-shaped door onto the same terminal: numbered functions,
	// one-based linear positions, return codes. For porting an existing
	// screen-scraper by changing how it calls rather than what it does. See
	// hllapi.go.
	g.POST("/hllapi", app.APIHLLAPI)

	// Screen tracing. Unlike everything else here this writes a file on the
	// server, so it is behind ALLOW_SCREEN_TRACE as well as the token. See
	// screen_trace.go.
	g.POST("/screen-trace", app.APIStartScreenTrace)
	g.DELETE("/screen-trace", app.APIStopScreenTrace)
	g.GET("/screen-trace", app.APIScreenTraceStatus)

	// The 3287 printer session. On the token surface as well as the browser's
	// because the case for it is unattended: a job runs overnight, the output
	// goes to a printer LU, and something has to collect it in the morning
	// without a person watching a panel. Its jobs are files on the server, but
	// files this session's own printer wrote — unlike file transfer, which
	// reaches whatever the host names. See printer.go.
	g.POST("/printer", app.APIPrinterStart)
	g.GET("/printer", app.APIPrinterStatus)
	g.DELETE("/printer", app.APIPrinterStop)
	g.GET("/printer/jobs", app.APIPrinterJobs)
	g.DELETE("/printer/jobs", app.APIPrinterJobDelete)

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
	// The flat /sessions/:id/* routes resolve here rather than through
	// APISessionScope, so the ownership check has to be repeated. Same answer
	// for "no such session" and "not yours".
	if !app.mayUseSession(c, s) {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return nil, false
	}
	return s, true
}

// APIListSessions returns a snapshot of the sessions the caller may use.
//
// Today that is every session, because the API token is one instance-wide
// credential and the service principal it resolves to is unconfined. Filtering
// through the same predicate the rest of the surface uses means this narrows
// on its own once tokens belong to individual users, rather than staying an
// enumeration of everyone's sessions that somebody has to remember to fix.
func (app *App) APIListSessions(c *gin.Context) {
	sessions := app.SessionManager.ListSessions()
	out := make([]gin.H, 0, len(sessions))
	for _, s := range sessions {
		if s == nil || !app.mayUseSession(c, s) {
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
		s, err := app.startHostSession(c, hostname)
		if err != nil {
			c.JSON(connectFailureStatus(err), gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"id": s.ID, "host": s.TargetHost, "port": s.TargetPort})
		return
	}
	if !isValidHostname(hostname) {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid hostname format: %q", hostname)})
		return
	}
	s, err := app.startHostSession(c, hostname)
	if err != nil {
		c.JSON(connectFailureStatus(err), gin.H{"error": err.Error()})
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

	// Closing a session is as much a use of it as typing into one, so it is
	// resolved through the same ownership check. This route did not have one,
	// because it has nothing to do with the session afterwards — which is how
	// it became a way to end somebody else's work by guessing an ID.
	//
	// Unlike every other route the refusal is a silent success rather than a
	// 404. DELETE has to be idempotent: a client retrying after a dropped
	// response must not be told its own completed delete failed, and "already
	// gone" is indistinguishable from "never existed". Answering 200 in both
	// cases keeps that true and still tells a caller nothing — what matters is
	// the line below, which only runs for a session that is theirs.
	s, found := app.SessionManager.GetSession(id)
	if found && app.mayUseSession(c, s) {
		app.SessionManager.RemoveSession(id)
	}
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

/* ---------------------------------------------------------------- */
/* Guided Business Tasks                                             */
/* ---------------------------------------------------------------- */

// APIListTasks returns the task catalogue. This doubles as export: the
// response is exactly the document /tasks/save accepts, so a catalogue can be
// version-controlled and moved between deployments with two curl calls.
func (app *App) APIListTasks(c *gin.Context) {
	tasks, err := app.allTasks(c)
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
	if _, err := app.taskStoreForRequest(c).Upsert(t); err != nil {
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
	t, found := app.findTask(c, payload.Name)
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

// connectFailureStatus separates "this connection did not work" from "this
// connection is not allowed".
//
// Everything used to answer 502, which tells a client the far end failed and
// the request is worth repeating. For a host the allowlist will never permit,
// or a caller who has spent their allowance, that is both wrong and an
// invitation to retry in a loop.
func connectFailureStatus(err error) int {
	switch {
	case errors.Is(err, errHostNotAllowed):
		return http.StatusForbidden
	case errors.Is(err, errRateLimited), errors.Is(err, errSessionLimit):
		return http.StatusTooManyRequests
	default:
		return http.StatusBadGateway
	}
}
