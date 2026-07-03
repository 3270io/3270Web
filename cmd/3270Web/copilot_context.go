package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// contextMaxFields caps how many input fields the per-turn orientation block
// lists so the injected context stays small.
const contextMaxFields = 12

// CopilotContextHandler handles GET /copilot/context – returns a compact,
// pre-formatted orientation block ("text") that the chat panel prepends to the
// system prompt at the start of each user turn. It tells Copilot what is on the
// screen right now and what it has already learned about the application, so it
// does not have to spend tool rounds re-discovering its bearings.
func (app *App) CopilotContextHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		// No session: still return an empty-but-valid payload so the frontend
		// can inject nothing without special-casing the error.
		c.JSON(http.StatusOK, gin.H{"text": ""})
		return
	}

	var sb strings.Builder
	sb.WriteString("# Session context (snapshot at the start of this turn)\n")

	// --- Current screen -------------------------------------------------
	h := app.sessionHost(s)
	if h != nil && h.IsConnected() {
		if err := h.UpdateScreen(); err == nil {
			if screen := hostScreenSnapshot(h); screen != nil {
				if rows, cols, ok := app.modelDimensions(); ok {
					screen = limitScreenForDisplay(screen, rows, cols)
				}
				writeScreenContext(&sb, screen)
			}
		}
	} else {
		sb.WriteString("\n## Current screen\nNot connected to a host.\n")
	}

	// --- Application knowledge learned so far ---------------------------
	writeKnowledgeContext(&sb, app, s)

	c.JSON(http.StatusOK, gin.H{"text": sb.String()})
}

// writeScreenContext appends a concise description of the live screen.
func writeScreenContext(sb *strings.Builder, screen *host.Screen) {
	sb.WriteString("\n## Current screen\n")
	if title := firstNonEmptyScreenLine(screen.Text()); title != "" {
		sb.WriteString("- Title: " + title + "\n")
	}
	if status := strings.TrimSpace(screen.Status); status != "" {
		sb.WriteString("- Status: " + status + "\n")
	}
	if r, col, ok := screen.StatusCursor(); ok {
		sb.WriteString(fmt.Sprintf("- Cursor: row %d, col %d\n", r, col))
	}

	inputs := make([]*host.Field, 0)
	for _, f := range screen.Fields {
		if f == nil || f.IsProtected() {
			continue
		}
		inputs = append(inputs, f)
	}
	sb.WriteString(fmt.Sprintf("- Input fields: %d\n", len(inputs)))
	if len(inputs) == 0 {
		return
	}
	sb.WriteString("- Key input fields (row,col):\n")
	shown := inputs
	if len(shown) > contextMaxFields {
		shown = shown[:contextMaxFields]
	}
	for _, f := range shown {
		flags := make([]string, 0, 2)
		if f.IsNumeric() {
			flags = append(flags, "numeric")
		}
		if f.IsHidden() {
			flags = append(flags, "hidden")
		}
		flagStr := ""
		if len(flags) > 0 {
			flagStr = " [" + strings.Join(flags, ",") + "]"
		}
		val := ""
		if !f.IsHidden() {
			if v := strings.TrimSpace(f.GetValue()); v != "" {
				val = " = " + truncateForContext(v, 24)
			}
		}
		sb.WriteString(fmt.Sprintf("  - (%d,%d)%s%s\n", f.StartY, f.StartX, flagStr, val))
	}
	if len(inputs) > len(shown) {
		sb.WriteString(fmt.Sprintf("  - …and %d more (use get_screen for the full field map)\n", len(inputs)-len(shown)))
	}
}

// writeKnowledgeContext appends a summary of what chaos exploration has learned.
func writeKnowledgeContext(sb *strings.Builder, app *App, s *session.Session) {
	sb.WriteString("\n## Application knowledge\n")

	active := false
	if eng, ok := app.chaosEngines.get(s.ID); ok && eng != nil {
		active = eng.Status().Active
	}
	if active {
		sb.WriteString("- Chaos exploration: RUNNING\n")
	}

	mm := app.sessionChaosMindMap(s)
	if mm == nil || len(mm.Areas) == 0 {
		sb.WriteString("- No screens discovered yet. Suggest running chaos monkey, or call business_app_overview after a run.\n")
		return
	}
	annotated := 0
	for _, area := range mm.Areas {
		if area != nil && area.BusinessPurpose != "" {
			annotated++
		}
	}
	fns := chaos.BusinessFunctionsOf(mm)
	sb.WriteString(fmt.Sprintf("- Screens discovered: %d (%d with a business purpose recorded)\n", len(mm.Areas), annotated))
	sb.WriteString(fmt.Sprintf("- Business functions cataloged: %d\n", len(fns)))
	if len(fns) > 0 {
		names := make([]string, 0, len(fns))
		for _, fn := range fns {
			names = append(names, fn.Name)
		}
		sort.Strings(names)
		if len(names) > 8 {
			names = append(names[:8], "…")
		}
		sb.WriteString("  - " + strings.Join(names, ", ") + "\n")
	}
	sb.WriteString("- For the full picture and the gaps still to investigate, call business_app_overview.\n")
}

func firstNonEmptyScreenLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if collapsed := strings.Join(strings.Fields(line), " "); collapsed != "" {
			return truncateForContext(collapsed, 72)
		}
	}
	return ""
}

func truncateForContext(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-1]) + "…"
}
