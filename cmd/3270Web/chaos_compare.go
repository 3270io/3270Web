// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/session"
)

// chaosCompareBody is the request body for /chaos/mindmap/compare. Each side
// of the comparison is given one of two ways:
//
//   - as a document: baseline / candidate, a MindMap in the shape returned by
//     /chaos/mindmap/export;
//   - by reference: baseRunId / candidateRunId, the ID of a saved run (as
//     listed by GET /chaos/runs), or the word "current" for the run this
//     session holds.
//
// The two forms can be mixed — a saved run against an exported file, say. A
// document wins when both are given for the same side.
type chaosCompareBody struct {
	Baseline       *chaos.MindMap `json:"baseline"`
	Candidate      *chaos.MindMap `json:"candidate"`
	BaseRunID      string         `json:"baseRunId"`
	CandidateRunID string         `json:"candidateRunId"`
}

// UnmarshalJSON also accepts the spellings a caller is likely to reach for
// (baselineRunId, base_run_id, …) rather than treating them as absent and
// comparing against nothing.
func (b *chaosCompareBody) UnmarshalJSON(data []byte) error {
	type plain chaosCompareBody
	var body plain
	if err := json.Unmarshal(data, &body); err != nil {
		return err
	}
	var alt struct {
		BaseRunIDUpper      string `json:"baseRunID"`
		BaselineRunID       string `json:"baselineRunId"`
		BaseRunIDSnake      string `json:"base_run_id"`
		BaselineRunIDSnake  string `json:"baseline_run_id"`
		CandidateRunIDUpper string `json:"candidateRunID"`
		CandidateRunIDSnake string `json:"candidate_run_id"`
	}
	if err := json.Unmarshal(data, &alt); err != nil {
		return err
	}
	*b = chaosCompareBody(body)
	for _, v := range []string{alt.BaseRunIDUpper, alt.BaselineRunID, alt.BaseRunIDSnake, alt.BaselineRunIDSnake} {
		if strings.TrimSpace(b.BaseRunID) == "" {
			b.BaseRunID = v
		}
	}
	for _, v := range []string{alt.CandidateRunIDUpper, alt.CandidateRunIDSnake} {
		if strings.TrimSpace(b.CandidateRunID) == "" {
			b.CandidateRunID = v
		}
	}
	return nil
}

// chaosCompareCurrentRun is the run reference that means "the run this
// session holds" instead of a saved run's ID.
const chaosCompareCurrentRun = "current"

// resolveChaosCompareSide returns the mind map for one side of a comparison:
// the document when one was sent, otherwise the map of the referenced run.
// side is "baseline" or "candidate", for the error message.
func (app *App) resolveChaosCompareSide(s *session.Session, side string, doc *chaos.MindMap, runID string) (*chaos.MindMap, int, error) {
	if doc != nil {
		return doc, 0, nil
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		// A side left out altogether compares as an empty map, as it always
		// has: everything on the other side is then reported as only there.
		return nil, 0, nil
	}
	if strings.EqualFold(runID, chaosCompareCurrentRun) {
		var mm *chaos.MindMap
		if !app.chaosEngines.isRemoved(s.ID) {
			mm = app.sessionChaosMindMap(s)
		}
		if mm == nil {
			return nil, http.StatusNotFound, fmt.Errorf("the %s is \"current\", but this session has no chaos run", side)
		}
		return mm, 0, nil
	}
	run, err := chaos.LoadRun(app.chaosRunsDirForSession(s.OwnerID), runID)
	if err != nil {
		return nil, http.StatusNotFound, fmt.Errorf("%s run: %w", side, err)
	}
	if run.MindMap == nil {
		return nil, http.StatusUnprocessableEntity, fmt.Errorf("%s run %q was saved without a map, so there is nothing to compare", side, runID)
	}
	return run.MindMap, 0, nil
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
// mind maps and returns the deltas. Each side is either an exported document
// or the ID of a saved run; see chaosCompareBody.
//
// Response is JSON by default; pass `Accept: text/html` (or include
// `?format=html` in the query string) to render the HTML report instead.
func (app *App) ChaosMindMapCompareHandler(c *gin.Context) {
	// The caller does not need an active chaos engine — comparison is a
	// pure function on two supplied JSON blobs. We still require a session
	// cookie so the endpoint is not internet-reachable on its own.
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	var body chaosCompareBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
		return
	}

	baseline, status, err := app.resolveChaosCompareSide(s, "baseline", body.Baseline, body.BaseRunID)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	candidate, status, err := app.resolveChaosCompareSide(s, "candidate", body.Candidate, body.CandidateRunID)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	diff := chaos.CompareMindMaps(baseline, candidate)

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
