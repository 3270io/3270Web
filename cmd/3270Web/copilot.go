package main

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/aiprovider"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/copilot"
	"github.com/jnnngs/3270Web/internal/host"
)

// initCopilot wires the Copilot package's HTTP routes and adds the
// /screen.json route used by the get_screen tool.
//
// Copilot login/token state is scoped per-browser (see the copilotAuthStore
// field and the identity cookie in internal/copilot/handlers.go), separately
// from the 3270Web_session cookie that tool execution resolves the active
// host connection through.
func (app *App) initCopilot(r *gin.Engine) {
	authPath, err := copilot.DefaultAuthPath()
	if err != nil {
		log.Printf("[copilot] disabled: cannot resolve auth path: %v", err)
		return
	}
	app.copilotAuthStore = copilot.NewAuthStore(filepath.Dir(authPath))
	copilot.NewHandlers(app.copilotAuthStore).Register(r)

	// Provider selection (Claude, OpenAI, Ollama, Google AI, any
	// OpenAI-compatible endpoint, ...) lives one layer above Copilot: it
	// stores per-browser credentials and proxies chat to whichever backend is
	// selected, delegating back to internal/copilot when that backend is
	// Copilot itself. The /api/copilot/* routes above stay for the OAuth
	// device flow and for backwards compatibility.
	app.aiConfigStore = aiprovider.NewConfigStore(filepath.Dir(authPath))
	aiprovider.NewHandlers(app.aiConfigStore, app.copilotAuthStore).Register(r)

	// Tool-supporting endpoints for the get_screen / send_key / write_field
	// / submit_screen tools. These wrap host.* calls in JSON so the
	// frontend can drive them from the Copilot panel.
	r.GET("/screen.json", app.ScreenJSONHandler)
	r.POST("/screen/key", app.ScreenKeyHandler)
	r.POST("/screen/write", app.ScreenWriteHandler)
	r.POST("/screen/submit", app.ScreenSubmitHandler)
	r.POST("/screen/connect", app.ScreenConnectHandler)
	r.POST("/screen/cursor", app.ScreenCursorHandler)
	r.POST("/screen/wait", app.ScreenWaitHandler)

	// Per-turn orientation block injected by the chat panel into the system
	// prompt (current screen + learned application knowledge).
	r.GET("/copilot/context", app.CopilotContextHandler)
}

// ScreenKeyHandler sends an AID key to the host without first submitting
// any pending field writes. Used by the Copilot send_key tool.
func (app *App) ScreenKeyHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	key, ok := normalizeKey(body.Key)
	if !ok {
		// A model asking for a key that doesn't exist (e.g. a typo like
		// "PF25") must fail loudly, not silently press Enter and submit
		// whatever is currently on screen.
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unrecognized key %q", body.Key)})
		return
	}
	if err := app.sessionHost(s).SendKey(key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": key})
}

// ScreenWriteHandler writes text into the field at (row, col). Used by
// the Copilot write_field tool. Accepts either a 1-indexed field_key (the
// R<row>C<col>L<len> key from chaos_list_screens/business function steps)
// or 0-indexed row/col (from get_screen) — never both conventions applied
// to the same call, which is what let the model silently land one row and
// one column off when it passed a business function's field_key straight
// through as if it were 0-indexed.
func (app *App) ScreenWriteHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		FieldKey string `json:"field_key"`
		Row      int    `json:"row"`
		Col      int    `json:"col"`
		Text     string `json:"text"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	row, col := body.Row, body.Col
	if fieldKey := strings.TrimSpace(body.FieldKey); fieldKey != "" {
		fkRow, fkCol, _, ok := chaos.ParseMindMapFieldKey(fieldKey)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid field_key %q (expected R<row>C<col>L<len>)", fieldKey)})
			return
		}
		// field_key is 1-indexed; WriteStringAt is 0-indexed.
		row, col = fkRow-1, fkCol-1
	}
	if row < 0 || col < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "row/col must be >= 0"})
		return
	}
	// Reject control characters that could be interpreted as s3270 commands.
	if host.ContainsForbiddenFieldText(body.Text) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text must not contain CR/LF/TAB"})
		return
	}
	if err := app.sessionHost(s).WriteStringAt(row, col, body.Text); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ScreenSubmitHandler submits any modified fields with an Enter AID. Used
// by the Copilot submit_screen tool.
func (app *App) ScreenSubmitHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if err := app.sessionHost(s).SubmitScreen(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ScreenConnectHandler establishes (or replaces) the current session's host
// connection. Used by the Copilot connect_session tool so a chat request
// like "log in to the payments app" doesn't dead-end when nothing is
// connected yet, or when the user wants to switch targets mid-conversation.
// Reuses resetSessionHost (already used by the workflow-driven reconnect
// path), which validates the hostname and stops any existing host first.
func (app *App) ScreenConnectHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		Hostname string `json:"hostname"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hostname := strings.TrimSpace(body.Hostname)
	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "hostname is required"})
		return
	}
	if err := app.resetSessionHost(s, hostname); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	snap := app.snapshotSession(s)
	c.JSON(http.StatusOK, gin.H{
		"ok":         true,
		"targetHost": snap.TargetHost,
		"targetPort": snap.TargetPort,
	})
}

// ScreenCursorHandler moves the host cursor to (row, col) without sending a
// key or writing a field. Used by the Copilot move_cursor tool — MoveCursor
// already existed on host.Host but was never exposed as a tool, so the
// model had no way to position the cursor ahead of a key that acts relative
// to it (e.g. a bare Tab/BackTab, or a host action keyed off cursor
// position rather than a specific field).
func (app *App) ScreenCursorHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		Row int `json:"row"`
		Col int `json:"col"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Row < 0 || body.Col < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "row/col must be >= 0"})
		return
	}
	if err := app.sessionHost(s).MoveCursor(body.Row, body.Col); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

const (
	screenWaitDefaultTimeout = 5 * time.Second
	screenWaitMaxTimeout     = 15 * time.Second
	screenWaitPollInterval   = 150 * time.Millisecond
)

// ScreenWaitHandler polls the host status line until the keyboard unlocks
// (the transaction has finished processing) or timeout_ms elapses, then
// returns the screen at that point. Used by the Copilot wait_for_unlock
// tool — without it, a multi-second transaction's get_screen call right
// after send_key/submit_screen returned the stale pre-action screen, since
// nothing previously polled for the host to actually finish.
func (app *App) ScreenWaitHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var body struct {
		TimeoutMs int `json:"timeout_ms"`
	}
	_ = c.ShouldBindJSON(&body) // body is optional; zero value falls back to the default below

	timeout := time.Duration(body.TimeoutMs) * time.Millisecond
	if timeout <= 0 || timeout > screenWaitMaxTimeout {
		timeout = screenWaitDefaultTimeout
	}

	h := app.sessionHost(s)
	deadline := time.Now().Add(timeout)
	for {
		if err := h.UpdateScreen(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "update screen: " + err.Error()})
			return
		}
		screen := hostScreenSnapshot(h)
		if screen == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "screen unavailable"})
			return
		}
		state, ok := screen.StatusKeyboardState()
		unlocked := !ok || state != "L"
		timedOut := !unlocked && time.Now().After(deadline)
		if unlocked || timedOut {
			if rows, cols, dimOK := app.modelDimensions(); dimOK {
				screen = limitScreenForDisplay(screen, rows, cols)
			}
			c.JSON(http.StatusOK, gin.H{
				"unlocked": unlocked,
				"timedOut": timedOut,
				"screen":   screenToJSON(screen),
			})
			return
		}
		time.Sleep(screenWaitPollInterval)
	}
}

// ScreenJSONHandler returns the current screen as a JSON object suitable
// for handing to Copilot via the get_screen tool. It mirrors the shape of
// (*host.Screen) but flattens the Buffer into a plain text field plus a
// per-field list so an LLM can reason about it without parsing internal
// rune arrays.
func (app *App) ScreenJSONHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	h := app.sessionHost(s)
	if err := h.UpdateScreen(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update screen: " + err.Error()})
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

	c.JSON(http.StatusOK, screenToJSON(screen))
}

func screenToJSON(s *host.Screen) gin.H {
	if s == nil {
		return gin.H{}
	}
	fields := make([]gin.H, 0, len(s.Fields))
	for _, f := range s.Fields {
		if f == nil {
			continue
		}
		// Hidden (e.g. password) fields never leave the server: their typed
		// value must not reach the Copilot API or be echoed back to the
		// browser, where it would land in the tool-card transcript and
		// persist in localStorage.
		val := ""
		if !f.IsHidden() {
			val = f.GetValue()
		}
		fields = append(fields, gin.H{
			"row":       f.StartY,
			"col":       f.StartX,
			"endRow":    f.EndY,
			"endCol":    f.EndX,
			"value":     val,
			"protected": f.IsProtected(),
			"numeric":   f.IsNumeric(),
			"hidden":    f.IsHidden(),
			"length":    fieldLength(f),
		})
	}
	cursorRow, cursorCol, hasCursor := s.StatusCursor()
	out := gin.H{
		"width":     s.Width,
		"height":    s.Height,
		"text":      redactHiddenFieldText(s),
		"fields":    fields,
		"formatted": s.IsFormatted,
		"status":    strings.TrimSpace(s.Status),
		"hasCursor": hasCursor,
	}
	if hasCursor {
		out["cursorRow"] = cursorRow
		out["cursorCol"] = cursorCol
	}
	return out
}

// redactHiddenFieldText returns the screen's text with any hidden (e.g.
// password) field's characters replaced by '*'. screen.Text() renders the
// raw buffer, which still holds the actual typed characters for hidden
// fields — 3270 "hidden" only suppresses local echo on a real terminal, it
// doesn't stop the value from being present in the buffer sent here.
func redactHiddenFieldText(s *host.Screen) string {
	text := s.Text()
	if s.Width <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	for _, f := range s.Fields {
		if f == nil || !f.IsHidden() {
			continue
		}
		curX, curY := f.StartX, f.StartY
		endX, endY := f.EndX, f.EndY
		for {
			if curY >= 0 && curY < len(lines) {
				line := []rune(lines[curY])
				if curX >= 0 && curX < len(line) {
					line[curX] = '*'
					lines[curY] = string(line)
				}
			}
			if curX == endX && curY == endY {
				break
			}
			curX++
			if curX >= s.Width {
				curX = 0
				curY++
				if curY >= s.Height {
					break
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

func fieldLength(f *host.Field) int {
	if f == nil {
		return 0
	}
	if f.EndY == f.StartY {
		return f.EndX - f.StartX + 1
	}
	// Multi-line field: approximate length as the rune count of the value.
	return len([]rune(f.GetValue()))
}
