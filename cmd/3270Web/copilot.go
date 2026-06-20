package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/copilot"
	"github.com/jnnngs/3270Web/internal/host"
)

// initCopilot wires the Copilot package's HTTP routes and adds the
// /screen.json route used by the get_screen tool.
//
// Auth state is shared across requests; tool execution always resolves the
// active session via the existing 3270Web_session cookie.
func (app *App) initCopilot(r *gin.Engine) {
	authPath, err := copilot.DefaultAuthPath()
	if err != nil {
		log.Printf("[copilot] disabled: cannot resolve auth path: %v", err)
		return
	}
	mgr := copilot.NewAuthManager(authPath)
	copilot.NewHandlers(mgr).Register(r)

	// Tool-supporting endpoints for the get_screen / send_key / write_field
	// / submit_screen tools. These wrap host.* calls in JSON so the
	// frontend can drive them from the Copilot panel.
	r.GET("/screen.json", app.ScreenJSONHandler)
	r.POST("/screen/key", app.ScreenKeyHandler)
	r.POST("/screen/write", app.ScreenWriteHandler)
	r.POST("/screen/submit", app.ScreenSubmitHandler)

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
	key := normalizeKey(body.Key)
	if err := s.Host.SendKey(key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": key})
}

// ScreenWriteHandler writes text into the field at (row, col). Used by
// the Copilot write_field tool.
func (app *App) ScreenWriteHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
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
	// Reject control characters that could be interpreted as s3270 commands.
	if host.ContainsForbiddenFieldText(body.Text) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text must not contain CR/LF/TAB"})
		return
	}
	if err := s.Host.WriteStringAt(body.Row, body.Col, body.Text); err != nil {
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
	if err := s.Host.SubmitScreen(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
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
	if err := s.Host.UpdateScreen(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update screen: " + err.Error()})
		return
	}
	screen := hostScreenSnapshot(s.Host)
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
		val := f.GetValue()
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
		"text":      s.Text(),
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
