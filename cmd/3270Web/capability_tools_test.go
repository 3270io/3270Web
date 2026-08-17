// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/copilot"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/mcptools"
	"github.com/jnnngs/3270Web/internal/session"
)

// The assistant's tool surface grew out of chaos exploration, and the terminal
// grew past it: snapshots, the display toggles, the connection's own account of
// itself, the task catalogue and the printer session were on the HTTP API and
// nowhere a model could reach. These tests pin the fix — not the wiring of any
// one tool, which the registry join and the browser/catalogue match already
// cover, but that each capability is reachable at all.

// capabilityTools are the tools that make a capability visible to a model.
// Losing one does not break a build; it makes the assistant answer that this
// build cannot do something it can, which is worse, because nobody reports it
// as a bug.
var capabilityTools = map[string][]string{
	"snapshots":          {"snapshot_take", "snapshot_list", "snapshot_diff", "snapshot_delete"},
	"display toggles":    {"get_display_toggles", "set_display_toggle"},
	"connection details": {"get_connection_details"},
	"printer session":    {"printer_status", "printer_start", "printer_stop", "printer_read_job"},
	"task catalogue":     {"list_tasks", "run_task"},
}

func TestCapabilityToolsAreOfferedToTheModel(t *testing.T) {
	declared := map[string]bool{}
	for _, tool := range copilot.DefaultTools() {
		declared[tool.Function.Name] = true
	}
	for capability, names := range capabilityTools {
		for _, name := range names {
			if !declared[name] {
				t.Errorf("%s is on the API but %q is not in the tool catalogue, "+
					"so an assistant asked about it cannot find out that this build can", capability, name)
			}
		}
	}
}

// TestEveryToolNameIsOfferedOnce — the MCP server draws from two lists, and
// list_tasks and run_task moved from one to the other. A name in both would be
// registered twice against one server.
func TestEveryToolNameIsOfferedOnce(t *testing.T) {
	seen := map[string]int{}
	for _, tool := range mcptools.Enabled(mcptools.TierChaos) {
		seen[tool.Name]++
	}
	for _, only := range mcpOnlyTools() {
		seen[only.Tool.Name]++
	}
	var doubled []string
	for name, count := range seen {
		if count > 1 {
			doubled = append(doubled, name)
		}
	}
	sort.Strings(doubled)
	if len(doubled) > 0 {
		t.Errorf("offered more than once: %v — a tool bound in mcpOnlyTools must be listed in "+
			"mcptools.Omitted so the registry does not bind it as well", doubled)
	}
}

// TestSharedToolDescriptorsMatchTheCatalogue — a tool offered on both surfaces
// is described once. Restating it here is how the two came to disagree about
// what the same tool does.
func TestSharedToolDescriptorsMatchTheCatalogue(t *testing.T) {
	declared := map[string]copilot.ToolFunction{}
	for _, tool := range copilot.DefaultTools() {
		declared[tool.Function.Name] = tool.Function
	}
	omitted := mcptools.Omitted()

	for _, only := range mcpOnlyTools() {
		name := only.Tool.Name
		want, shared := declared[name]
		if !shared {
			// list_sessions and use_session are MCP's alone, and are declared
			// in full in mcp_only_tools.go. Nothing to compare.
			continue
		}
		if _, skipped := omitted[name]; !skipped {
			t.Errorf("%q is in the shared catalogue and bound in mcpOnlyTools, but the registry "+
				"binds it too; add it to mcptools.omitted with the reason", name)
		}
		if only.Tool.Description != want.Description {
			t.Errorf("%q: the MCP description has drifted from the catalogue's", name)
		}
		schema, _ := only.Tool.InputSchema.(map[string]any)
		if !reflect.DeepEqual(schema, want.Parameters) {
			t.Errorf("%q: the MCP schema has drifted from the catalogue's:\n got %v\nwant %v",
				name, schema, want.Parameters)
		}
	}
}

// snapshotBrowserApp returns an app whose cookie door onto snapshots is wired,
// with one connected mock session.
func snapshotBrowserApp(t *testing.T) (*gin.Engine, *session.Session, *host.MockHost) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	app := &App{SessionManager: session.NewManager(), baseDir: t.TempDir()}
	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	mock.Connected = true
	sess := app.SessionManager.CreateSession(mock)

	r := gin.New()
	r.POST("/screen/snapshots", app.SnapshotTakeHandler)
	r.GET("/screen/snapshots", app.SnapshotListHandler)
	r.DELETE("/screen/snapshots", app.SnapshotDeleteHandler)
	r.POST("/screen/snapshots/diff", app.SnapshotDiffHandler)
	return r, sess, mock
}

// TestSnapshotCookieDoorRoundTrip — snapshots were the one capability with no
// browser door at all, and the chat panel runs its tool loop in the page.
func TestSnapshotCookieDoorRoundTrip(t *testing.T) {
	r, sess, mock := snapshotBrowserApp(t)

	if err := mock.WriteStringAt(0, 0, "BALANCE 100"); err != nil {
		t.Fatalf("seed screen: %v", err)
	}
	if w := doJSON(r, http.MethodPost, "/screen/snapshots", sess.ID, map[string]any{"name": "before"}); w.Code != http.StatusCreated {
		t.Fatalf("take: status = %d (body %s)", w.Code, w.Body.String())
	}

	w := doJSON(r, http.MethodGet, "/screen/snapshots", sess.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d (body %s)", w.Code, w.Body.String())
	}
	var listed struct {
		Snapshots []struct {
			Name string `json:"name"`
		} `json:"snapshots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(listed.Snapshots) != 1 || listed.Snapshots[0].Name != "before" {
		t.Fatalf("list = %+v, want the one snapshot named \"before\"", listed.Snapshots)
	}

	// The screen moves on, as it would after a transaction, and the diff is
	// against the screen as it stands rather than a second snapshot.
	if err := mock.WriteStringAt(0, 0, "BALANCE 250"); err != nil {
		t.Fatalf("move the screen on: %v", err)
	}
	w = doJSON(r, http.MethodPost, "/screen/snapshots/diff", sess.ID, map[string]any{"left": "before"})
	if w.Code != http.StatusOK {
		t.Fatalf("diff: status = %d (body %s)", w.Code, w.Body.String())
	}
	var diff struct {
		Identical bool `json:"identical"`
		Lines     []struct {
			Row   int    `json:"row"`
			Left  string `json:"left"`
			Right string `json:"right"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &diff); err != nil {
		t.Fatalf("diff body: %v", err)
	}
	if diff.Identical || len(diff.Lines) == 0 {
		t.Fatalf("diff = %+v, want the changed row reported", diff)
	}
	if diff.Lines[0].Left == diff.Lines[0].Right {
		t.Errorf("diff line %+v reports no change", diff.Lines[0])
	}

	if w := doJSON(r, http.MethodDelete, "/screen/snapshots?name=before", sess.ID, nil); w.Code != http.StatusOK {
		t.Fatalf("delete: status = %d (body %s)", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodGet, "/screen/snapshots", sess.ID, nil)
	listed.Snapshots = nil
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(listed.Snapshots) != 0 {
		t.Errorf("after delete the session still holds %+v", listed.Snapshots)
	}
}

// TestSnapshotCookieDoorNeedsASession — no cookie must mean no snapshot, not
// somebody else's.
func TestSnapshotCookieDoorNeedsASession(t *testing.T) {
	r, _, _ := snapshotBrowserApp(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodPost, "/screen/snapshots"},
		{http.MethodGet, "/screen/snapshots"},
		{http.MethodDelete, "/screen/snapshots?name=before"},
		{http.MethodPost, "/screen/snapshots/diff"},
	} {
		w := doJSON(r, call.method, call.path, "", map[string]any{"name": "before", "left": "before"})
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s with no session: status = %d, want 404", call.method, call.path, w.Code)
		}
	}
}
