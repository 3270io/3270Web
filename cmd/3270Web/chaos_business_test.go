package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/host"
)

// setupChaosBusinessTestApp extends the standard chaos test app with the
// business-understanding routes and a temp chaos-runs directory.
func setupChaosBusinessTestApp(t *testing.T) (*App, *gin.Engine, string) {
	t.Helper()
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("failed to create mock host: %v", err)
	}
	mockHost.Connected = true
	app, r, sessID := setupChaosTestApp(t, mockHost)
	app.chaosRunsDir = t.TempDir()
	r.GET("/chaos/screens", app.ChaosScreensListHandler)
	r.POST("/chaos/screens/annotate", app.ChaosScreenAnnotateHandler)
	r.GET("/chaos/business/functions", app.ChaosBusinessFunctionsListHandler)
	r.POST("/chaos/business/functions", app.ChaosBusinessFunctionSaveHandler)
	r.POST("/chaos/business/generate-workflow", app.ChaosBusinessGenerateWorkflowHandler)
	return app, r, sessID
}

// seedBusinessSavedRun stores a saved run with two connected screens as the
// session's loaded run and persists it to the app's chaos-runs directory.
func seedBusinessSavedRun(t *testing.T, app *App, sessID string) *chaos.SavedRun {
	t.Helper()
	mindMapJSON := `{
		"areas": {
			"menu": {
				"hash": "menu",
				"label": "MAIN MENU",
				"visits": 3,
				"fieldCount": 1,
				"inputFieldCount": 1,
				"previewText": "MAIN MENU\n1. ACCOUNTS",
				"fieldMetadata": {"R20C1L2": {"row": 20, "column": 1, "length": 2}},
				"keyPresses": {"Enter": {"presses": 3, "progressions": 3, "destinations": {"form": 3}}}
			},
			"form": {
				"hash": "form",
				"label": "ACCOUNT INQUIRY",
				"visits": 2,
				"fieldCount": 1,
				"inputFieldCount": 1,
				"previewText": "ACCOUNT INQUIRY\nACCOUNT NO: ________",
				"fieldMetadata": {"R2C13L8": {"row": 2, "column": 13, "length": 8}}
			}
		}
	}`
	var mm chaos.MindMap
	if err := json.Unmarshal([]byte(mindMapJSON), &mm); err != nil {
		t.Fatalf("seed mind map: %v", err)
	}
	run := &chaos.SavedRun{
		SavedRunMeta: chaos.SavedRunMeta{ID: chaos.NewRunID(), StepsRun: 5, Transitions: 3, UniqueScreens: 2},
		MindMap:      &mm,
	}
	if err := chaos.SaveRun(app.chaosRunsDir, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	app.chaosEngines.setLoadedRun(sessID, run)
	return run
}

func TestChaosBusinessAnnotateLoadedRunPersistsToDisk(t *testing.T) {
	app, r, sessID := setupChaosBusinessTestApp(t)
	run := seedBusinessSavedRun(t, app, sessID)

	body := []byte(`{
		"screen_hash": "form",
		"business_purpose": "Account inquiry entry",
		"notes": "Needs a valid account number",
		"field_semantics": {"R2C13L8": {"name": "account_number", "example": "1234"}}
	}`)
	w := chaosRequest(r, http.MethodPost, "/chaos/screens/annotate", body, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("annotate status = %d, body = %s", w.Code, w.Body.String())
	}

	// The annotation must survive a fresh read from disk.
	reloaded, err := chaos.LoadRun(app.chaosRunsDir, run.ID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	area := reloaded.MindMap.Areas["form"]
	if area == nil || area.BusinessPurpose != "Account inquiry entry" {
		t.Fatalf("annotation not persisted: %+v", area)
	}
	if area.FieldSemantics["R2C13L8"].Name != "account_number" {
		t.Fatalf("field semantics not persisted: %+v", area.FieldSemantics)
	}
}

func TestChaosScreensListIncludesAnnotations(t *testing.T) {
	app, r, sessID := setupChaosBusinessTestApp(t)
	seedBusinessSavedRun(t, app, sessID)

	annotate := []byte(`{"screen_hash": "menu", "business_purpose": "Application main menu"}`)
	if w := chaosRequest(r, http.MethodPost, "/chaos/screens/annotate", annotate, sessID); w.Code != http.StatusOK {
		t.Fatalf("annotate status = %d", w.Code)
	}

	w := chaosRequest(r, http.MethodGet, "/chaos/screens", nil, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("screens status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		ScreenCount int              `json:"screenCount"`
		Screens     []map[string]any `json:"screens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if resp.ScreenCount != 2 {
		t.Fatalf("screenCount = %d, want 2", resp.ScreenCount)
	}
	// Sorted by visits desc: menu (3) first.
	if resp.Screens[0]["hash"] != "menu" || resp.Screens[0]["businessPurpose"] != "Application main menu" {
		t.Fatalf("first screen = %+v", resp.Screens[0])
	}
	if _, ok := resp.Screens[0]["previewText"]; !ok {
		t.Fatal("previewText missing with default include_previews")
	}

	// include_previews=false drops previews.
	w = chaosRequest(r, http.MethodGet, "/chaos/screens?include_previews=false", nil, sessID)
	if strings.Contains(w.Body.String(), "previewText") {
		t.Fatal("previewText present despite include_previews=false")
	}
}

func TestChaosBusinessFunctionSaveListAndGenerate(t *testing.T) {
	app, r, sessID := setupChaosBusinessTestApp(t)
	run := seedBusinessSavedRun(t, app, sessID)

	fnBody := []byte(`{
		"name": "Account inquiry",
		"description": "Look up an account",
		"steps": [
			{"screen_hash": "menu", "inputs": [{"field_key": "R20C1L2", "value": "1"}], "aid_key": "Enter", "expect_hash": "form"},
			{"screen_hash": "form", "inputs": [{"field_key": "R2C13L8", "parameter": "account_number"}], "aid_key": "Enter"}
		],
		"parameters": [
			{"name": "account_number", "screen_hash": "form", "field_key": "R2C13L8", "required": true, "example": "1234"}
		]
	}`)
	if w := chaosRequest(r, http.MethodPost, "/chaos/business/functions", fnBody, sessID); w.Code != http.StatusOK {
		t.Fatalf("save function status = %d, body = %s", w.Code, w.Body.String())
	}

	// Catalog must be persisted to disk alongside the run.
	reloaded, err := chaos.LoadRun(app.chaosRunsDir, run.ID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if len(reloaded.MindMap.BusinessFunctions) != 1 {
		t.Fatalf("catalog not persisted: %+v", reloaded.MindMap.BusinessFunctions)
	}

	w := chaosRequest(r, http.MethodGet, "/chaos/business/functions", nil, sessID)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Account inquiry") {
		t.Fatalf("list functions = %d: %s", w.Code, w.Body.String())
	}

	// Missing required parameter is a 400.
	gen := []byte(`{"name": "account inquiry"}`)
	if w := chaosRequest(r, http.MethodPost, "/chaos/business/generate-workflow", gen, sessID); w.Code != http.StatusBadRequest {
		t.Fatalf("generate without params status = %d, want 400", w.Code)
	}

	gen = []byte(`{"name": "Account Inquiry", "parameters": {"account_number": "9876"}}`)
	w = chaosRequest(r, http.MethodPost, "/chaos/business/generate-workflow", gen, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("generate status = %d, body = %s", w.Code, w.Body.String())
	}

	// The generated document must load through the standard workflow parser
	// and carry the business metadata.
	wf, err := parseWorkflowPayload(w.Body.Bytes())
	if err != nil {
		t.Fatalf("generated workflow failed parseWorkflowPayload: %v", err)
	}
	if wf.Name != "Account inquiry" || wf.BusinessFunction != "Account inquiry" {
		t.Fatalf("workflow metadata = %q / %q", wf.Name, wf.BusinessFunction)
	}
	if len(wf.Parameters) != 1 || wf.Parameters[0].Value != "9876" {
		t.Fatalf("workflow parameters = %+v", wf.Parameters)
	}
	if wf.Host != "127.0.0.1" || wf.Port != 3270 {
		t.Fatalf("workflow host/port = %s:%d, want session target", wf.Host, wf.Port)
	}
	foundFill := false
	for _, step := range wf.Steps {
		if step.Type == "FillString" && step.Text == "9876" {
			if step.Coordinates == nil || step.Coordinates.Row != 2 || step.Coordinates.Column != 13 {
				t.Fatalf("fill coordinates = %+v", step.Coordinates)
			}
			foundFill = true
		}
	}
	if !foundFill {
		t.Fatalf("no FillString with the parameter value in steps: %+v", wf.Steps)
	}
}

func TestChaosBusinessEndpointsWithoutRunData(t *testing.T) {
	_, r, sessID := setupChaosBusinessTestApp(t)

	if w := chaosRequest(r, http.MethodGet, "/chaos/screens", nil, sessID); w.Code != http.StatusNotFound {
		t.Fatalf("screens status = %d, want 404", w.Code)
	}
	body := []byte(`{"screen_hash": "x", "business_purpose": "y"}`)
	if w := chaosRequest(r, http.MethodPost, "/chaos/screens/annotate", body, sessID); w.Code != http.StatusNotFound {
		t.Fatalf("annotate status = %d, want 404", w.Code)
	}
	gen := []byte(`{"name": "x"}`)
	if w := chaosRequest(r, http.MethodPost, "/chaos/business/generate-workflow", gen, sessID); w.Code != http.StatusNotFound {
		t.Fatalf("generate status = %d, want 404", w.Code)
	}
	// Listing with no data returns an empty catalog rather than an error.
	w := chaosRequest(r, http.MethodGet, "/chaos/business/functions", nil, sessID)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"functions":[]`) {
		t.Fatalf("list = %d: %s", w.Code, w.Body.String())
	}
}

func TestChaosBusinessAnnotateLiveEngine(t *testing.T) {
	app, r, sessID := setupChaosBusinessTestApp(t)

	eng := chaos.New(nil, chaos.DefaultConfig())
	if err := eng.AnnotateArea("h1", "seeded", "", nil); err != nil {
		t.Fatalf("seed annotate: %v", err)
	}
	app.chaosEngines.set(sessID, eng)

	body := []byte(`{"screen_hash": "h1", "business_purpose": "Login screen"}`)
	if w := chaosRequest(r, http.MethodPost, "/chaos/screens/annotate", body, sessID); w.Code != http.StatusOK {
		t.Fatalf("annotate status = %d", w.Code)
	}
	w := chaosRequest(r, http.MethodGet, "/chaos/screens", nil, sessID)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Login screen") {
		t.Fatalf("screens = %d: %s", w.Code, w.Body.String())
	}
}

func TestCompleteEngineAtomicallyInstallsAnnotatedSnapshot(t *testing.T) {
	app, _, sessID := setupChaosBusinessTestApp(t)

	eng := chaos.New(nil, chaos.DefaultConfig())
	app.chaosEngines.set(sessID, eng)

	// A business write routed through withEngine before completion must be
	// captured by the completion snapshot.
	if err := app.applyChaosBusinessWrite(sessID, func(e *chaos.Engine) error {
		return e.AnnotateArea("h1", "Login screen", "", nil)
	}, func(run *chaos.SavedRun) error {
		t.Fatal("write went to loaded run while engine registered")
		return nil
	}); err != nil {
		t.Fatalf("annotate via engine: %v", err)
	}

	runID := chaos.NewRunID()
	snapshot := app.chaosEngines.completeEngine(sessID, runID)
	if snapshot == nil {
		t.Fatal("completeEngine returned nil with a registered engine")
	}
	if snapshot.MindMap == nil || snapshot.MindMap.Areas["h1"] == nil ||
		snapshot.MindMap.Areas["h1"].BusinessPurpose != "Login screen" {
		t.Fatalf("completion snapshot missing pre-completion annotation: %+v", snapshot.MindMap)
	}
	// The engine must be unregistered and the snapshot installed as the
	// loaded run in the same operation.
	if _, ok := app.chaosEngines.get(sessID); ok {
		t.Fatal("engine still registered after completeEngine")
	}
	loaded, ok := app.chaosEngines.getLoadedRun(sessID)
	if !ok || loaded != snapshot {
		t.Fatal("snapshot not installed as the loaded run")
	}
	// Second completion is a no-op.
	if again := app.chaosEngines.completeEngine(sessID, chaos.NewRunID()); again != nil {
		t.Fatal("completeEngine on a completed session should return nil")
	}

	// Post-completion writes land on the loaded run and persist to disk.
	if err := app.applyChaosBusinessWrite(sessID, func(e *chaos.Engine) error {
		t.Fatal("write went to an engine after completion")
		return nil
	}, func(run *chaos.SavedRun) error {
		return chaos.AnnotateSavedRun(run, "h1", "", "verified after completion", nil)
	}); err != nil {
		t.Fatalf("annotate via loaded run: %v", err)
	}
	reloaded, err := chaos.LoadRun(app.chaosRunsDir, runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if reloaded.MindMap.Areas["h1"].BusinessNotes != "verified after completion" {
		t.Fatal("post-completion annotation not persisted to disk")
	}
}

func TestWorkflowConfigBackwardCompatibleWithBusinessFields(t *testing.T) {
	// Old workflows (no business metadata) keep parsing.
	legacy := []byte(`{"Host": "h", "Port": 23, "Steps": [{"Type": "PressEnter"}]}`)
	wf, err := parseWorkflowPayload(legacy)
	if err != nil {
		t.Fatalf("legacy workflow failed to parse: %v", err)
	}
	if wf.Name != "" || wf.BusinessFunction != "" || wf.Parameters != nil {
		t.Fatalf("legacy workflow grew unexpected metadata: %+v", wf)
	}

	// New workflows round-trip the metadata.
	business := []byte(`{
		"Host": "h", "Port": 23,
		"Name": "Account inquiry",
		"Description": "Look up an account",
		"BusinessFunction": "Account inquiry",
		"Parameters": [{"Name": "account_number", "Value": "1234", "Row": 2, "Column": 13, "Length": 8}],
		"Steps": [{"Type": "PressEnter"}]
	}`)
	wf, err = parseWorkflowPayload(business)
	if err != nil {
		t.Fatalf("business workflow failed to parse: %v", err)
	}
	if wf.Name != "Account inquiry" || len(wf.Parameters) != 1 || wf.Parameters[0].Name != "account_number" {
		t.Fatalf("business metadata lost: %+v", wf)
	}
}

func TestResolveChaosScreenHintsPrecedence(t *testing.T) {
	app, _, sessID := setupChaosBusinessTestApp(t)

	reqHint := chaos.ScreenHint{KnownData: []string{"from-request"}}
	sessionHint := chaos.ScreenHint{KnownData: []string{"from-session"}}
	savedHint := chaos.ScreenHint{KnownData: []string{"from-file"}}

	// No sources: no first-screen hint.
	hints := app.resolveChaosScreenHints(sessID, chaosStartRequest{}, chaosHintsPayload{})
	if _, ok := hints[chaos.FirstScreenHintKey]; ok {
		t.Fatal("unexpected first-screen hint with no sources")
	}

	// Saved file only.
	hints = app.resolveChaosScreenHints(sessID, chaosStartRequest{}, chaosHintsPayload{FirstScreenHint: &savedHint})
	if got := hints[chaos.FirstScreenHintKey].KnownData; len(got) != 1 || got[0] != "from-file" {
		t.Fatalf("saved-file hint = %v", got)
	}

	// Session-scoped hint beats the saved file.
	app.chaosEngines.upsertScreenHint(sessID, chaos.FirstScreenHintKey, sessionHint)
	hints = app.resolveChaosScreenHints(sessID, chaosStartRequest{}, chaosHintsPayload{FirstScreenHint: &savedHint})
	if got := hints[chaos.FirstScreenHintKey].KnownData; len(got) != 1 || got[0] != "from-session" {
		t.Fatalf("session hint should win over saved file, got %v", got)
	}

	// Request hint beats everything.
	hints = app.resolveChaosScreenHints(sessID, chaosStartRequest{FirstScreenHint: &reqHint}, chaosHintsPayload{FirstScreenHint: &savedHint})
	if got := hints[chaos.FirstScreenHintKey].KnownData; len(got) != 1 || got[0] != "from-request" {
		t.Fatalf("request hint should win, got %v", got)
	}
}
