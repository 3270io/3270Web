// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/copilot"
	"github.com/jnnngs/3270Web/internal/session"
)

// browserHandlerRE matches the method-shorthand keys of the HANDLERS table in
// web/static/copilot-tools.js, e.g. "        async get_screen(_args) {".
var browserHandlerRE = regexp.MustCompile(`(?m)^\s+async ([a-z_][a-z0-9_]*)\(`)

// TestBrowserHandlersMatchToolCatalogue keeps the two halves of the tool
// surface honest about each other.
//
// The schemas live in Go and the dispatch table lives in JavaScript, so a
// tool can be described to the model with nothing behind it, or implemented
// and never offered. Neither failure is visible until a conversation hits it,
// and the model's report is "the tool returned an error", which points at the
// host rather than at the missing wiring.
func TestBrowserHandlersMatchToolCatalogue(t *testing.T) {
	src, err := os.ReadFile("../../web/static/copilot-tools.js")
	if err != nil {
		t.Fatalf("read the browser dispatch table: %v", err)
	}

	implemented := map[string]bool{}
	for _, m := range browserHandlerRE.FindAllStringSubmatch(string(src), -1) {
		implemented[m[1]] = true
	}
	// Helpers defined the same way that are not tools.
	for _, notATool := range []string{"getJSON", "postJSON", "runTool"} {
		delete(implemented, notATool)
	}

	declared := map[string]bool{}
	for _, tool := range copilot.DefaultTools() {
		declared[tool.Function.Name] = true
	}

	// ask_user is answered by the panel's own UI, not by an HTTP route, so it
	// is declared without appearing in the dispatch table.
	uiOnly := map[string]bool{"ask_user": true}

	var missing, orphaned []string
	for name := range declared {
		if !implemented[name] && !uiOnly[name] {
			missing = append(missing, name)
		}
	}
	for name := range implemented {
		if !declared[name] {
			orphaned = append(orphaned, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(orphaned)

	if len(missing) > 0 {
		t.Errorf("declared to the model but not implemented in copilot-tools.js: %v", missing)
	}
	if len(orphaned) > 0 {
		t.Errorf("implemented in copilot-tools.js but never offered to the model: %v", orphaned)
	}
}

func skillsTestApp(t *testing.T) (*App, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	app := &App{SessionManager: session.NewManager(), baseDir: t.TempDir()}
	r := gin.New()
	app.registerSkillRoutes(r)
	return app, r
}

func getJSONBody(t *testing.T, r *gin.Engine, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", path, w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("GET %s: body is not JSON: %v", path, err)
	}
	return out
}

func TestSkillsListAndLoad(t *testing.T) {
	_, r := skillsTestApp(t)

	list := getJSONBody(t, r, "/skills")
	skills, _ := list["skills"].([]any)
	if len(skills) == 0 {
		t.Fatal("no skills listed")
	}
	// A listing is metadata only — the body is what load_skill is for, and
	// sending it here would defeat the whole two-level arrangement.
	for _, entry := range skills {
		m, _ := entry.(map[string]any)
		if _, hasBody := m["body"]; hasBody {
			t.Errorf("list_skills must not carry skill bodies, got %v", m)
		}
		if m["source"] == nil {
			t.Errorf("every listed skill must say where it came from, got %v", m)
		}
	}

	loaded := getJSONBody(t, r, "/skills/load?name=chaos-monkey")
	if body, _ := loaded["body"].(string); !strings.Contains(body, "chaos") {
		t.Errorf("loaded skill body looks wrong: %q", body)
	}
	refs, _ := loaded["instruction_refs"].([]any)
	if len(refs) == 0 {
		t.Error("chaos-monkey should cite instruction fragments")
	}
	// The fragments are named, not inlined.
	if body, _ := loaded["body"].(string); strings.Contains(body, "# Choosing which key to press") {
		t.Error("load_skill inlined an instruction fragment; it should only name them")
	}
}

func TestSkillLoadUnknownNameExplainsItself(t *testing.T) {
	_, r := skillsTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/skills/load?name=no-such-skill", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "list_skills") {
		t.Errorf("the error should point at how to find a valid name, got %s", w.Body.String())
	}
}

// TestRepeatLoadReturnsReminderNotBody covers what actually enforces the
// context budget. Prose in the prompt asking the model not to re-request a
// skill is a request; returning a one-line reminder instead of the body is
// the part that holds.
func TestRepeatLoadReturnsReminderNotBody(t *testing.T) {
	app, r := skillsTestApp(t)

	// Give the request a session so the tracker is the per-conversation one
	// rather than a throwaway.
	sess := app.SessionManager.CreateSession(nil)
	get := func(path string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return out
	}

	first := get("/skills/load?name=chaos-monkey")
	firstBody, _ := first["body"].(string)
	if len(firstBody) < 200 {
		t.Fatalf("first load should return the full body, got %d chars", len(firstBody))
	}

	second := get("/skills/load?name=chaos-monkey")
	if already, _ := second["already_load"].(bool); !already {
		t.Error("a repeat load should be marked as already loaded")
	}
	secondBody, _ := second["body"].(string)
	if len(secondBody) >= len(firstBody) {
		t.Errorf("a repeat load should return a reminder, not the body again (%d chars)", len(secondBody))
	}
	if !strings.Contains(secondBody, "Already loaded") {
		t.Errorf("the reminder should say so plainly, got %q", secondBody)
	}

	// A different conversation has seen nothing.
	other := app.SessionManager.CreateSession(nil)
	req := httptest.NewRequest(http.MethodGet, "/skills/load?name=chaos-monkey", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: other.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var fresh map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &fresh); err != nil {
		t.Fatal(err)
	}
	if already, _ := fresh["already_load"].(bool); already {
		t.Error("dedup must be per conversation; a second session was told it already had the skill")
	}
}

// TestConversationHeaderDrivesDedup covers the headless path.
//
// The tracker was originally keyed only on the session cookie, so a client
// with no cookie — which is every non-browser client — got a fresh tracker on
// every request and the dedup silently did nothing. That is the surface where
// conversations run longest and re-sending a playbook costs most, so it is
// worth a test of its own rather than being folded into the cookie case.
func TestConversationHeaderDrivesDedup(t *testing.T) {
	_, r := skillsTestApp(t)

	load := func(conversation string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/skills/load?name=chaos-monkey", nil)
		if conversation != "" {
			req.Header.Set(ConversationHeader, conversation)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	first := load("conv-A")
	if already, _ := first["already_load"].(bool); already {
		t.Fatal("the first load in a conversation should return the body")
	}
	second := load("conv-A")
	if already, _ := second["already_load"].(bool); !already {
		t.Error("a repeat load in the same conversation should be deduplicated")
	}

	// A different conversation has seen nothing, even with no cookie in play.
	other := load("conv-B")
	if already, _ := other["already_load"].(bool); already {
		t.Error("a second conversation was wrongly told it already had the skill")
	}

	// With no identifier at all we cannot tell callers apart, so nothing is
	// deduplicated rather than the wrong caller being refused.
	if already, _ := load("")["already_load"].(bool); already {
		t.Error("an unidentified caller must not inherit another conversation's history")
	}
}

func TestInstructionsListAndLoad(t *testing.T) {
	_, r := skillsTestApp(t)

	list := getJSONBody(t, r, "/instructions")
	if items, _ := list["instructions"].([]any); len(items) == 0 {
		t.Fatal("no instruction fragments listed")
	}

	// The suffix is optional, because a skill's instruction_refs carry it and
	// a human writing frontmatter usually will not.
	for _, name := range []string{"aid-key-safety", "aid-key-safety.instructions.md"} {
		loaded := getJSONBody(t, r, "/instructions/load?name="+name)
		if body, _ := loaded["body"].(string); !strings.Contains(body, "AID key") {
			t.Errorf("load_instruction(%q) body looks wrong: %q", name, body)
		}
	}
}

// TestSkillIndexSectionIsGenerated pins that the prompt's skill list comes
// from the catalogue. Hand-maintaining it would mean a skill dropped in
// beside the binary stayed invisible to the model.
func TestSkillIndexSectionIsGenerated(t *testing.T) {
	app, _ := skillsTestApp(t)

	section := app.skillIndexSection()
	if !strings.Contains(section, "## Available skills") {
		t.Fatalf("index section missing its heading:\n%s", section)
	}
	for _, s := range app.skillCatalogue().Skills() {
		if !strings.Contains(section, s.Name) {
			t.Errorf("skill %q is not in the generated index", s.Name)
		}
	}
}
