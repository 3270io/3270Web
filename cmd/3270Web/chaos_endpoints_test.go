// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// These tests cover the HTTP layer of chaos mode at the points where it used
// to lose something: a malformed request it acted on anyway, a finished run
// it could no longer export or explain, a load it allowed mid-run.

// setupChaosEndpointsApp is setupFullChaosTestApp plus the routes these tests
// need that it does not register.
func setupChaosEndpointsApp(t *testing.T) (*App, *gin.Engine, string) {
	t.Helper()
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mockHost.Screen = buildSampleApp1Screen()
	mockHost.Connected = true

	app, r, sessID := setupFullChaosTestApp(t, mockHost)
	r.GET("/chaos/mindmap/export", app.ChaosMindMapExportHandler)
	r.POST("/chaos/mindmap/import", app.ChaosMindMapImportHandler)
	r.POST("/chaos/mindmap/compare", app.ChaosMindMapCompareHandler)
	r.POST("/chaos/runs/label", app.ChaosLabelRunHandler)
	r.GET("/chaos/insights", app.ChaosInsightsHandler)
	r.GET("/chaos/screens", app.ChaosScreensListHandler)
	r.POST("/chaos/report", app.ChaosReportHandler)
	return app, r, sessID
}

// runChaosToCompletion starts a short run and waits until it has finished
// and its engine has been discarded — the state every "after the run"
// endpoint has to work in.
func runChaosToCompletion(t *testing.T, app *App, r *gin.Engine, sessID string, maxSteps int) {
	t.Helper()
	startChaosRun(t, r, sessID, maxSteps)
	waitForChaosEngineGone(t, app, sessID)
}

func waitForChaosEngineGone(t *testing.T, app *App, sessID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := app.chaosEngines.get(sessID); !ok {
			if !app.waitForChaosSync(5 * time.Second) {
				t.Fatal("the status goroutine did not finish after the engine went")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the chaos engine was never discarded")
}

// startSlowChaosRun starts a run that is still going when the call returns,
// and for several seconds after.
func startSlowChaosRun(t *testing.T, r *gin.Engine, sessID string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"maxSteps":     5000,
		"stepDelaySec": 0.4,
		"seed":         7,
	})
	if w := chaosRequest(r, http.MethodPost, "/chaos/start", payload, sessID); w.Code != http.StatusOK {
		t.Fatalf("start: want 200, got %d — body %s", w.Code, w.Body.String())
	}
}

func decodeJSONBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("response is not JSON: %v — %s", err, body)
	}
	return out
}

// --- 1. A body that does not parse starts nothing -------------------------

func TestChaosStartRejectsABodyItCannotParse(t *testing.T) {
	cases := map[string]string{
		// The case from the review: a number sent as a string. It used to
		// start a default run with nothing blocked and answer 200.
		"snake_case number as string": `{"max_steps":"50","key_blacklist":["PF3"]}`,
		"camelCase number as string":  `{"maxSteps":"50","keyBlacklist":["PF3"]}`,
		"list as string":              `{"key_blacklist":"PF3"}`,
		"not JSON":                    `max_steps=50`,
		"truncated":                   `{"max_steps": 5`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			app, r, sessID := setupChaosEndpointsApp(t)
			w := chaosRequest(r, http.MethodPost, "/chaos/start", []byte(body), sessID)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d — body %s", w.Code, w.Body.String())
			}
			resp := decodeJSONBody(t, w.Body.Bytes())
			msg, _ := resp["error"].(string)
			if !strings.Contains(msg, "nothing was started") {
				t.Errorf("the error does not say that nothing was started: %q", msg)
			}
			if _, ok := app.chaosEngines.get(sessID); ok {
				t.Fatal("a run was started from a body that did not parse")
			}
		})
	}
}

func TestChaosStartNamesTheOptionThatWasWrong(t *testing.T) {
	_, r, sessID := setupChaosEndpointsApp(t)
	w := chaosRequest(r, http.MethodPost, "/chaos/start", []byte(`{"max_steps":"50"}`), sessID)
	msg, _ := decodeJSONBody(t, w.Body.Bytes())["error"].(string)
	if !strings.Contains(msg, "max_steps") || !strings.Contains(msg, "number") {
		t.Errorf("want an error naming max_steps and the type it should be, got %q", msg)
	}
}

func TestChaosStartStillTreatsAnEmptyBodyAsDefaults(t *testing.T) {
	for name, body := range map[string][]byte{"no body": nil, "whitespace": []byte("  \n")} {
		t.Run(name, func(t *testing.T) {
			app, r, sessID := setupChaosEndpointsApp(t)
			w := chaosRequest(r, http.MethodPost, "/chaos/start", body, sessID)
			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d — body %s", w.Code, w.Body.String())
			}
			chaosRequest(r, http.MethodPost, "/chaos/stop", nil, sessID)
			waitForChaosEngineGone(t, app, sessID)
		})
	}
}

func TestChaosResumeRejectsABodyItCannotParse(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	runChaosToCompletion(t, app, r, sessID, 2)

	w := chaosRequest(r, http.MethodPost, "/chaos/resume", []byte(`{"max_steps":"50"}`), sessID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d — body %s", w.Code, w.Body.String())
	}
	if _, ok := app.chaosEngines.get(sessID); ok {
		t.Fatal("a run was resumed from a body that did not parse")
	}
}

// --- 2. Map export and import work when no run is active ------------------

func TestChaosMindMapExportWorksAfterTheRunHasEnded(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	runChaosToCompletion(t, app, r, sessID, 3)

	w := chaosRequest(r, http.MethodGet, "/chaos/mindmap/export", nil, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("export after the run: want 200, got %d — body %s", w.Code, w.Body.String())
	}
	var mm chaos.MindMap
	if err := json.Unmarshal(w.Body.Bytes(), &mm); err != nil {
		t.Fatalf("export is not a mind map: %v", err)
	}
	if len(mm.Areas) == 0 {
		t.Fatal("the exported map has no screens")
	}
}

func TestChaosMindMapExportWithoutARunSaysWhatToDo(t *testing.T) {
	_, r, sessID := setupChaosEndpointsApp(t)
	w := chaosRequest(r, http.MethodGet, "/chaos/mindmap/export", nil, sessID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
	msg, _ := decodeJSONBody(t, w.Body.Bytes())["error"].(string)
	if strings.Contains(msg, "engine") {
		t.Errorf("the error talks about an engine, which means nothing to a caller: %q", msg)
	}
	if !strings.Contains(msg, "load a saved run") {
		t.Errorf("the error does not say what to do: %q", msg)
	}
}

func importedMapFixture() *chaos.MindMap {
	return &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{
		"imported-screen": {
			Hash:   "imported-screen",
			Label:  "IMPORTED MENU",
			Visits: 4,
			FieldMetadata: map[string]chaos.MindMapFieldMetadata{
				"R3C20L4": {Row: 3, Column: 20, Length: 4},
			},
			KnownWorkingValues: map[string][]string{"R3C20L4": {"INQ"}},
			KeyPresses: map[string]*chaos.MindMapKeyPress{
				"Enter": {Presses: 2, Progressions: 2, Destinations: map[string]int{"elsewhere": 2}},
			},
		},
	}}
}

func TestChaosMindMapImportMergesIntoTheFinishedRun(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	runChaosToCompletion(t, app, r, sessID, 3)
	before, _ := app.chaosEngines.getLoadedRun(sessID)
	if before == nil || before.MindMap == nil || len(before.MindMap.Areas) == 0 {
		t.Fatal("the finished run has no map to merge into")
	}

	body, _ := json.Marshal(importedMapFixture())
	w := chaosRequest(r, http.MethodPost, "/chaos/mindmap/import", body, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("import with no run active: want 200, got %d — body %s", w.Code, w.Body.String())
	}
	resp := decodeJSONBody(t, w.Body.Bytes())
	if resp["target"] != "loadedRun" {
		t.Errorf("target = %v, want loadedRun", resp["target"])
	}

	after, _ := app.chaosEngines.getLoadedRun(sessID)
	if after.ID != before.ID {
		t.Errorf("the import replaced the finished run (%s) with another (%s)", before.ID, after.ID)
	}
	if _, ok := after.MindMap.Areas["imported-screen"]; !ok {
		t.Error("the imported screen is not in the run's map")
	}
	for hash := range before.MindMap.Areas {
		if _, ok := after.MindMap.Areas[hash]; !ok {
			t.Errorf("the import lost screen %s the run had found", hash)
		}
	}

	// And a following resume starts from the merged map: the engine it
	// builds is seeded from the loaded run.
	resume, _ := json.Marshal(map[string]any{"maxSteps": 1, "stepDelaySec": 0.4, "extendLimits": true})
	if w := chaosRequest(r, http.MethodPost, "/chaos/resume", resume, sessID); w.Code != http.StatusOK {
		t.Fatalf("resume after import: want 200, got %d — body %s", w.Code, w.Body.String())
	}
	eng, ok := app.chaosEngines.get(sessID)
	if !ok {
		t.Fatal("no engine after resume")
	}
	if _, ok := eng.MindMapSnapshot().Areas["imported-screen"]; !ok {
		t.Error("the resumed run did not start from the imported map")
	}
	chaosRequest(r, http.MethodPost, "/chaos/stop", nil, sessID)
	waitForChaosEngineGone(t, app, sessID)
}

func TestChaosMindMapImportIntoAnEmptySessionMakesARunToContinue(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	body, _ := json.Marshal(importedMapFixture())
	w := chaosRequest(r, http.MethodPost, "/chaos/mindmap/import", body, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — body %s", w.Code, w.Body.String())
	}
	resp := decodeJSONBody(t, w.Body.Bytes())
	if resp["target"] != "newRun" {
		t.Errorf("target = %v, want newRun", resp["target"])
	}
	run, ok := app.chaosEngines.getLoadedRun(sessID)
	if !ok || run.MindMap == nil || run.MindMap.Areas["imported-screen"] == nil {
		t.Fatal("the imported map is not held as the session's run")
	}
	// It must be exportable again, which is the round trip the docs describe.
	if w := chaosRequest(r, http.MethodGet, "/chaos/mindmap/export", nil, sessID); w.Code != http.StatusOK {
		t.Fatalf("export after import: want 200, got %d", w.Code)
	}
}

func TestChaosMindMapImportIsRefusedWhileARunIsActive(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	startSlowChaosRun(t, r, sessID)
	body, _ := json.Marshal(importedMapFixture())
	w := chaosRequest(r, http.MethodPost, "/chaos/mindmap/import", body, sessID)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d — body %s", w.Code, w.Body.String())
	}
	chaosRequest(r, http.MethodPost, "/chaos/stop", nil, sessID)
	waitForChaosEngineGone(t, app, sessID)
}

// --- 3. Why the run ended survives the engine ------------------------------

func TestChaosStatusKeepsTheEndReasonAfterTheEngineHasGone(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	runChaosToCompletion(t, app, r, sessID, 2)

	status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, "/chaos/status", nil, sessID).Body.Bytes())
	if got := status["terminationReason"]; got != chaos.TerminationReasonMaxSteps {
		t.Errorf("terminationReason = %v, want %q", got, chaos.TerminationReasonMaxSteps)
	}
	if got, _ := status["terminationSummary"].(string); !strings.Contains(got, "step limit") {
		t.Errorf("terminationSummary = %q, want it to mention the step limit", got)
	}
	if got := status["targetHost"]; got != "127.0.0.1:3270" {
		t.Errorf("targetHost = %v, want 127.0.0.1:3270", got)
	}
	cfg, _ := status["config"].(map[string]any)
	if cfg == nil || cfg["maxSteps"] != float64(2) {
		t.Errorf("config = %v, want the limits the run was started with", status["config"])
	}

	// The session's own record carries it, not just the response.
	sess, _ := app.SessionManager.GetSession(sessID)
	state := chaosStateSnapshot(sess)
	if state == nil || state.TerminationReason != chaos.TerminationReasonMaxSteps {
		t.Errorf("session state TerminationReason = %+v", state)
	}
}

func TestChaosSavedRunCarriesReasonHostAndLabel(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	runChaosToCompletion(t, app, r, sessID, 2)

	var metas []chaos.SavedRunMeta
	w := chaosRequest(r, http.MethodGet, "/chaos/runs", nil, sessID)
	if err := json.Unmarshal(w.Body.Bytes(), &metas); err != nil || len(metas) != 1 {
		t.Fatalf("runs list: %v — %s", err, w.Body.String())
	}
	meta := metas[0]
	if meta.TerminationReason != chaos.TerminationReasonMaxSteps {
		t.Errorf("saved terminationReason = %q", meta.TerminationReason)
	}
	if meta.TargetHost != "127.0.0.1:3270" {
		t.Errorf("saved targetHost = %q", meta.TargetHost)
	}
	if meta.FirstScreenHash == "" {
		t.Error("saved run has no firstScreenHash")
	}

	// Name it, and the name is in the list, the file and the status.
	body, _ := json.Marshal(map[string]string{"id": meta.ID, "label": "  Nightly\tbaseline\n"})
	w = chaosRequest(r, http.MethodPost, "/chaos/runs/label", body, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("label: want 200, got %d — body %s", w.Code, w.Body.String())
	}
	if got := decodeJSONBody(t, w.Body.Bytes())["label"]; got != "Nightly baseline" {
		t.Errorf("label = %q, want it reduced to one clean line", got)
	}
	w = chaosRequest(r, http.MethodGet, "/chaos/runs", nil, sessID)
	metas = nil
	_ = json.Unmarshal(w.Body.Bytes(), &metas)
	if len(metas) != 1 || metas[0].Label != "Nightly baseline" {
		t.Errorf("runs list after labelling: %+v", metas)
	}
	status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, "/chaos/status", nil, sessID).Body.Bytes())
	if status["label"] != "Nightly baseline" {
		t.Errorf("status label = %v", status["label"])
	}
}

func TestChaosLabelRunOnDiskOnly(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	runChaosToCompletion(t, app, r, sessID, 2)
	loaded, _ := app.chaosEngines.getLoadedRun(sessID)
	// Forget the run in memory: the label must reach the file all the same.
	if w := chaosRequest(r, http.MethodPost, "/chaos/remove", nil, sessID); w.Code != http.StatusOK {
		t.Fatalf("remove: %d", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"id": loaded.ID, "label": "kept"})
	if w := chaosRequest(r, http.MethodPost, "/chaos/runs/label", body, sessID); w.Code != http.StatusOK {
		t.Fatalf("label: want 200, got %d — body %s", w.Code, w.Body.String())
	}
	sess, _ := app.SessionManager.GetSession(sessID)
	run, err := chaos.LoadRun(app.chaosRunsDirForSession(sess.OwnerID), loaded.ID)
	if err != nil || run.Label != "kept" {
		t.Fatalf("label on disk = %v, %v", run, err)
	}

	for name, body := range map[string]string{
		"unknown run": `{"id":"no-such-run","label":"x"}`,
		"no id":       `{"label":"x"}`,
		"not JSON":    `label=x`,
	} {
		w := chaosRequest(r, http.MethodPost, "/chaos/runs/label", []byte(body), sessID)
		if w.Code != http.StatusNotFound && w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400 or 404, got %d", name, w.Code)
		}
	}
}

func TestChaosStatusForALoadedRunReportsItsReasonAndHost(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	sess, _ := app.SessionManager.GetSession(sessID)
	run := &chaos.SavedRun{
		SavedRunMeta: chaos.SavedRunMeta{
			ID:                "earlier-run",
			StepsRun:          40,
			Transitions:       6,
			UniqueScreens:     3,
			TerminationReason: chaos.TerminationReasonSaturated,
			TargetHost:        "mainframe.example:992",
			Label:             "last week",
		},
		MindMap: importedMapFixture(),
	}
	if err := chaos.SaveRun(app.chaosRunsDirForSession(sess.OwnerID), run); err != nil {
		t.Fatalf("save: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"runID": "earlier-run"})
	if w := chaosRequest(r, http.MethodPost, "/chaos/load", body, sessID); w.Code != http.StatusOK {
		t.Fatalf("load: %d — %s", w.Code, w.Body.String())
	}
	status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, "/chaos/status", nil, sessID).Body.Bytes())
	if status["terminationReason"] != chaos.TerminationReasonSaturated {
		t.Errorf("terminationReason = %v", status["terminationReason"])
	}
	if status["targetHost"] != "mainframe.example:992" {
		t.Errorf("targetHost = %v, want the host the run explored", status["targetHost"])
	}
	if status["sessionTargetHost"] != "127.0.0.1:3270" {
		t.Errorf("sessionTargetHost = %v, want the host this session is on", status["sessionTargetHost"])
	}
	if status["label"] != "last week" {
		t.Errorf("label = %v", status["label"])
	}

	// Insights: diagnostics for a loaded run, with no engine anywhere.
	w := chaosRequest(r, http.MethodGet, "/chaos/insights", nil, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("insights: %d — %s", w.Code, w.Body.String())
	}
	diag, _ := decodeJSONBody(t, w.Body.Bytes())["run"].(map[string]any)
	if diag == nil {
		t.Fatal("insights for a loaded run has no run diagnostics")
	}
	if diag["terminationReason"] != chaos.TerminationReasonSaturated || diag["active"] != false {
		t.Errorf("insights run = %v", diag)
	}
}

func TestChaosInsightsKeepTheEndReasonAfterCompletion(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	runChaosToCompletion(t, app, r, sessID, 2)
	w := chaosRequest(r, http.MethodGet, "/chaos/insights", nil, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("insights after completion: %d — %s", w.Code, w.Body.String())
	}
	diag, _ := decodeJSONBody(t, w.Body.Bytes())["run"].(map[string]any)
	if diag == nil || diag["terminationReason"] != chaos.TerminationReasonMaxSteps {
		t.Fatalf("insights run diagnostics after completion = %v", diag)
	}
	if diag["stepsRun"] != float64(2) {
		t.Errorf("stepsRun = %v, want 2", diag["stepsRun"])
	}
}

func TestChaosStatusReportsTheTargetHostInEveryState(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	check := func(state string) {
		t.Helper()
		status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, "/chaos/status", nil, sessID).Body.Bytes())
		if status["targetHost"] != "127.0.0.1:3270" {
			t.Errorf("%s: targetHost = %v", state, status["targetHost"])
		}
	}
	check("before any run")
	startSlowChaosRun(t, r, sessID)
	check("while active")
	// Coverage appears once the run has taken a step.
	var coverage map[string]any
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline) && coverage == nil; time.Sleep(20 * time.Millisecond) {
		status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, "/chaos/status", nil, sessID).Body.Bytes())
		coverage, _ = status["coverageStats"].(map[string]any)
	}
	if coverage == nil {
		t.Error("no coverageStats while active")
	} else if _, ok := coverage["frontierAreas"]; !ok {
		t.Errorf("coverageStats has no frontier counts: %v", coverage)
	}
	chaosRequest(r, http.MethodPost, "/chaos/stop", nil, sessID)
	waitForChaosEngineGone(t, app, sessID)
	check("after the run")
	status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, "/chaos/status", nil, sessID).Body.Bytes())
	if status["terminationReason"] != chaos.TerminationReasonStopped {
		t.Errorf("terminationReason after a manual stop = %v", status["terminationReason"])
	}
	chaosRequest(r, http.MethodPost, "/chaos/remove", nil, sessID)
	check("after remove")
}

// --- 4. Resume applies the configured defaults -----------------------------

func TestChaosResumeAppliesTheConfiguredSettings(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	app.envPath = filepath.Join(app.baseDir, ".env")
	// A step limit no built-in default has, so it can only have come from
	// the settings.
	if err := os.WriteFile(app.envPath, []byte("CHAOS_MAX_STEPS=7\nCHAOS_STEP_DELAY_SEC=0\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	runChaosToCompletion(t, app, r, sessID, 2)

	if w := chaosRequest(r, http.MethodPost, "/chaos/resume", nil, sessID); w.Code != http.StatusOK {
		t.Fatalf("resume: %d — %s", w.Code, w.Body.String())
	}
	waitForChaosEngineGone(t, app, sessID)

	sess, _ := app.SessionManager.GetSession(sessID)
	state := chaosStateSnapshot(sess)
	if state == nil || state.Config == nil {
		t.Fatal("no run configuration kept for the resumed run")
	}
	if state.Config.MaxSteps != 7 {
		t.Errorf("resumed run's step limit = %d, want 7 from CHAOS_MAX_STEPS (the built-in default is %d)",
			state.Config.MaxSteps, chaos.DefaultConfig().MaxSteps)
	}
	if state.Config.StepDelay != 0 {
		t.Errorf("resumed run's step delay = %s, want 0 from CHAOS_STEP_DELAY_SEC", state.Config.StepDelay)
	}
	if !state.Config.Continued {
		t.Error("the resumed run is not marked as continuing an earlier one")
	}
}

// --- 5. Loading is refused while a run is active ---------------------------

func TestChaosLoadIsRefusedWhileARunIsActive(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	sess, _ := app.SessionManager.GetSession(sessID)
	saved := &chaos.SavedRun{SavedRunMeta: chaos.SavedRunMeta{ID: "other-run", StepsRun: 9}, MindMap: importedMapFixture()}
	if err := chaos.SaveRun(app.chaosRunsDirForSession(sess.OwnerID), saved); err != nil {
		t.Fatalf("save: %v", err)
	}

	startSlowChaosRun(t, r, sessID)
	body, _ := json.Marshal(map[string]string{"runID": "other-run"})
	w := chaosRequest(r, http.MethodPost, "/chaos/load", body, sessID)
	if w.Code != http.StatusConflict {
		t.Fatalf("load during a run: want 409, got %d — body %s", w.Code, w.Body.String())
	}
	if loaded, ok := app.chaosEngines.getLoadedRun(sessID); ok && loaded.ID == "other-run" {
		t.Fatal("the run was loaded over the active one despite the refusal")
	}
	// The load used to wipe the running run's status on its way through.
	status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, "/chaos/status", nil, sessID).Body.Bytes())
	if status["active"] != true {
		t.Errorf("the active run's status was disturbed: %v", status)
	}

	chaosRequest(r, http.MethodPost, "/chaos/stop", nil, sessID)
	waitForChaosEngineGone(t, app, sessID)
	if w := chaosRequest(r, http.MethodPost, "/chaos/load", body, sessID); w.Code != http.StatusOK {
		t.Fatalf("load once the run has ended: want 200, got %d — body %s", w.Code, w.Body.String())
	}
}

// --- 9. The status goroutine does not marshal the map while the run goes ---

func TestChaosSyncKeepsNoMapCopyUntilTheRunEnds(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	sess, _ := app.SessionManager.GetSession(sessID)
	startSlowChaosRun(t, r, sessID)

	// Wait for the goroutine to have synced at least one step.
	deadline := time.Now().Add(8 * time.Second)
	var state *session.ChaosState
	for time.Now().Before(deadline) {
		state = chaosStateSnapshot(sess)
		if state != nil && state.Active && state.StepsRun > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state == nil || !state.Active || state.StepsRun == 0 {
		t.Fatalf("the run never reported a step: %+v", state)
	}
	if len(state.MindMap) != 0 {
		t.Errorf("the session holds a %d-byte marshalled map while the run is still going", len(state.MindMap))
	}
	// Status still serves the map during the run, from the engine.
	status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, "/chaos/status?verbose=true", nil, sessID).Body.Bytes())
	if mm, _ := status["mindMap"].(map[string]any); mm == nil || mm["areas"] == nil {
		t.Errorf("verbose status has no map during the run: %v", status["mindMap"])
	}

	chaosRequest(r, http.MethodPost, "/chaos/stop", nil, sessID)
	waitForChaosEngineGone(t, app, sessID)

	state = chaosStateSnapshot(sess)
	if state == nil || len(state.MindMap) == 0 {
		t.Fatal("the session has no map copy after the run ended")
	}
	var kept chaos.MindMap
	if err := json.Unmarshal(state.MindMap, &kept); err != nil || len(kept.Areas) == 0 {
		t.Fatalf("the kept map is not a usable mind map: %v", err)
	}
	if state.LoadedRunID == "" {
		t.Error("the finished run's ID was not recorded")
	}
}

func TestChaosStatusFallsBackToTheKeptMapWhenTheRunIsNoLongerHeld(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	runChaosToCompletion(t, app, r, sessID, 2)
	app.chaosEngines.deleteLoadedRun(sessID)
	sess, _ := app.SessionManager.GetSession(sessID)
	withSessionLock(sess, func() { sess.Chaos.LoadedRunID = "" })

	for _, path := range []string{"/chaos/status", "/chaos/status?verbose=true"} {
		status := decodeJSONBody(t, chaosRequest(r, http.MethodGet, path, nil, sessID).Body.Bytes())
		if _, ok := status["mindMap"].(map[string]any); !ok {
			t.Errorf("%s: no map from the copy kept at the end of the run: %v", path, status["mindMap"])
		}
	}
}

// --- Compare two saved runs by ID ------------------------------------------

func TestChaosCompareAcceptsSavedRunIDs(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	sess, _ := app.SessionManager.GetSession(sessID)
	dir := app.chaosRunsDirForSession(sess.OwnerID)

	base := &chaos.SavedRun{SavedRunMeta: chaos.SavedRunMeta{ID: "run-base"}, MindMap: &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{
		"h-a": {Hash: "h-a", DedupSignature: "sig-a", FieldMetadata: map[string]chaos.MindMapFieldMetadata{"R1C1L5": {Row: 1, Column: 1, Length: 5}}},
	}}}
	cand := &chaos.SavedRun{SavedRunMeta: chaos.SavedRunMeta{ID: "run-cand"}, MindMap: &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{
		"h-a": {Hash: "h-a", DedupSignature: "sig-a", FieldMetadata: map[string]chaos.MindMapFieldMetadata{
			"R1C1L5": {Row: 1, Column: 1, Length: 5},
			"R2C1L5": {Row: 2, Column: 1, Length: 5},
		}},
		"h-b": {Hash: "h-b", DedupSignature: "sig-b"},
	}}}
	for _, run := range []*chaos.SavedRun{base, cand} {
		if err := chaos.SaveRun(dir, run); err != nil {
			t.Fatalf("save %s: %v", run.ID, err)
		}
	}

	body, _ := json.Marshal(map[string]string{"baseRunId": "run-base", "candidateRunId": "run-cand"})
	w := chaosRequest(r, http.MethodPost, "/chaos/mindmap/compare", body, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("compare by run ID: want 200, got %d — body %s", w.Code, w.Body.String())
	}
	var diff chaos.MindMapDiff
	if err := json.Unmarshal(w.Body.Bytes(), &diff); err != nil {
		t.Fatalf("diff: %v", err)
	}
	if diff.Summary.FieldChanges != 1 || diff.Summary.AreasOnlyInCand != 1 {
		t.Errorf("summary = %+v, want 1 field change and 1 screen only in the candidate", diff.Summary)
	}

	// A saved run against an uploaded document: the forms mix.
	mixed, _ := json.Marshal(map[string]any{"baseRunId": "run-base", "candidate": cand.MindMap})
	if w := chaosRequest(r, http.MethodPost, "/chaos/mindmap/compare", mixed, sessID); w.Code != http.StatusOK {
		t.Fatalf("mixed compare: %d — %s", w.Code, w.Body.String())
	}

	missing, _ := json.Marshal(map[string]string{"baseRunId": "run-base", "candidateRunId": "no-such-run"})
	w = chaosRequest(r, http.MethodPost, "/chaos/mindmap/compare", missing, sessID)
	if w.Code != http.StatusNotFound {
		t.Fatalf("compare with an unknown run: want 404, got %d", w.Code)
	}
	if msg, _ := decodeJSONBody(t, w.Body.Bytes())["error"].(string); !strings.Contains(msg, "candidate") {
		t.Errorf("the error does not say which side was missing: %q", msg)
	}
}
