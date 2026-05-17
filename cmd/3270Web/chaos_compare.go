package main

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/chaos"
)

// chaosCompareBody is the request body for /chaos/mindmap/compare. Both
// baseline and candidate must be JSON-encoded MindMap documents in the same
// shape returned by /chaos/mindmap/export.
type chaosCompareBody struct {
	Baseline  *chaos.MindMap `json:"baseline"`
	Candidate *chaos.MindMap `json:"candidate"`
}

//go:embed templates/mindmap_diff.html
var mindMapDiffTemplate string

var mindMapDiffTmpl = template.Must(
	template.New("mindmap_diff").Funcs(template.FuncMap{
		"truncate": func(n int, s string) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
	}).Parse(mindMapDiffTemplate),
)

// ChaosMindMapCompareHandler handles POST /chaos/mindmap/compare — diffs two
// previously-exported mind maps and returns the deltas.
//
// Response is JSON by default; pass `Accept: text/html` (or include
// `?format=html` in the query string) to render the HTML report instead.
func (app *App) ChaosMindMapCompareHandler(c *gin.Context) {
	// The caller does not need an active chaos engine — comparison is a
	// pure function on two supplied JSON blobs. We still require a session
	// cookie so the endpoint is not internet-reachable on its own.
	if s := app.getSession(c); s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	var body chaosCompareBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
		return
	}

	diff := chaos.CompareMindMaps(body.Baseline, body.Candidate)

	if wantsHTML(c) {
		var buf bytes.Buffer
		if err := mindMapDiffTmpl.Execute(&buf, diff); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "render diff: " + err.Error()})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
		return
	}
	c.JSON(http.StatusOK, diff)
}

func wantsHTML(c *gin.Context) bool {
	if strings.EqualFold(strings.TrimSpace(c.Query("format")), "html") {
		return true
	}
	accept := strings.ToLower(c.GetHeader("Accept"))
	if accept == "" {
		return false
	}
	// Prefer JSON when both are listed (the default).
	if strings.Contains(accept, "application/json") {
		return false
	}
	return strings.Contains(accept, "text/html")
}
