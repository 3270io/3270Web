// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/session"
)

// The values chaos mode learns for a hidden field must not leave the server
// by any route. These tests name the secrets and then look for them in the
// bytes each route sends, rather than checking a particular field of a
// particular response: the bug was never one serializer, it was that every
// serializer decided for itself.

const (
	secretPassword = "s3cret-Pa55"
	secretWrongTry = "wrong-gue55"
	secretAccount  = "4111-ACCT-77"
	plainUserName  = "ALICE"
)

var chaosSecrets = []string{secretPassword, secretWrongTry, secretAccount}

// secretsMindMap is a sign-on screen: a user name (ordinary), a password (a
// hidden field) and an account number (an ordinary field somebody annotated
// as sensitive).
func secretsMindMap() *chaos.MindMap {
	return &chaos.MindMap{
		Areas: map[string]*chaos.MindMapArea{
			"signon": {
				Hash:             "signon",
				Label:            "SIGN ON",
				Visits:           6,
				InputFieldCount:  3,
				HiddenFieldCount: 1,
				FieldMetadata: map[string]chaos.MindMapFieldMetadata{
					"R5C20L8":  {Row: 5, Column: 20, Length: 8},
					"R6C20L8":  {Row: 6, Column: 20, Length: 8, Hidden: true},
					"R7C20L12": {Row: 7, Column: 20, Length: 12},
				},
				FieldSemantics: map[string]chaos.BusinessFieldSemantic{
					"R5C20L8":  {Name: "User ID", Example: plainUserName},
					"R6C20L8":  {Name: "Password", Example: secretPassword},
					"R7C20L12": {Name: "Account", Example: secretAccount, Sensitive: true},
				},
				FieldDiscovery: map[string]chaos.MindMapFieldDiscovery{
					"R5C20L8":  {Writes: 4, WriteSuccesses: 4, Progressions: 2, LastValue: plainUserName, LastWorkedValue: plainUserName},
					"R6C20L8":  {Writes: 4, WriteSuccesses: 4, Progressions: 2, LastValue: secretWrongTry, LastWorkedValue: secretPassword},
					"R7C20L12": {Writes: 2, WriteSuccesses: 2, Progressions: 1, LastValue: secretAccount, LastWorkedValue: secretAccount},
				},
				KnownWorkingValues: map[string][]string{
					"R5C20L8":  {plainUserName},
					"R6C20L8":  {secretPassword},
					"R7C20L12": {secretAccount},
				},
				KnownTriedValues: map[string][]string{
					"R5C20L8":  {plainUserName},
					"R6C20L8":  {secretWrongTry, secretPassword},
					"R7C20L12": {secretAccount},
				},
				KeyPresses: map[string]*chaos.MindMapKeyPress{
					"Enter": {Presses: 4, Progressions: 2, Destinations: map[string]int{"menu": 2}},
				},
			},
			"menu": {Hash: "menu", Label: "MAIN MENU", Visits: 2},
		},
		BusinessFunctions: map[string]*chaos.BusinessFunction{
			"sign on": {
				Name: "Sign on",
				Steps: []chaos.BusinessFunctionStep{{
					ScreenHash: "signon",
					AIDKey:     "Enter",
					Inputs: []chaos.BusinessFunctionInput{
						{FieldKey: "R5C20L8", Value: plainUserName},
						{FieldKey: "R6C20L8", Value: secretPassword},
					},
				}},
				Parameters: []chaos.BusinessParameter{
					{Name: "account", ScreenHash: "signon", FieldKey: "R7C20L12", Example: secretAccount},
				},
			},
		},
	}
}

func assertNoChaosSecrets(t *testing.T, where string, body []byte) {
	t.Helper()
	for _, secret := range chaosSecrets {
		if strings.Contains(string(body), secret) {
			t.Errorf("%s: a value typed into a hidden or sensitive field is in the output: %q", where, secret)
		}
	}
}

func TestRedactedMindMapMasksHiddenAndSensitiveValues(t *testing.T) {
	original := secretsMindMap()
	redacted := redactedMindMap(original)

	out, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertNoChaosSecrets(t, "redacted map", out)
	if !strings.Contains(string(out), plainUserName) {
		t.Error("an ordinary field's value was removed along with the secret ones")
	}
	area := redacted.Areas["signon"]
	if got := area.KnownWorkingValues["R6C20L8"]; len(got) != 1 || got[0] != chaosHiddenValueMask {
		t.Errorf("hidden field's working values = %v, want the mask, so a reader can tell a value is known", got)
	}
	if got := area.KnownTriedValues["R6C20L8"]; len(got) != 1 {
		t.Errorf("hidden field's tried values = %v, want one mask — the count of guesses is not shown either", got)
	}

	// The map the engine and the saved run hold is not the one that was
	// scrubbed: a run has to be able to go on using what it learned.
	if got := original.Areas["signon"].KnownWorkingValues["R6C20L8"]; len(got) != 1 || got[0] != secretPassword {
		t.Errorf("redaction altered the original map: %v", got)
	}
}

func TestExportableMindMapDropsRatherThanMasks(t *testing.T) {
	out, err := json.Marshal(exportableMindMap(secretsMindMap()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertNoChaosSecrets(t, "export", out)
	// A mask in an export would be imported as a value that works, and the
	// engine would type eight asterisks into a password field on a live host.
	if strings.Contains(string(out), chaosHiddenValueMask) {
		t.Error("the export contains the display mask; an import would learn it as a working value")
	}
	if !strings.Contains(string(out), plainUserName) {
		t.Error("the export lost an ordinary field's value")
	}
}

func TestImportDiscardsTheDisplayMask(t *testing.T) {
	mm := redactedMindMap(secretsMindMap()) // what a status response contains
	dropMaskedMindMapValues(mm)
	out, _ := json.Marshal(mm)
	if strings.Contains(string(out), chaosHiddenValueMask) {
		t.Errorf("the mask survived import: %s", out)
	}
}

func TestChaosAttemptWriteValueMasksHiddenFields(t *testing.T) {
	mm := secretsMindMap()
	cases := []struct {
		name     string
		fromHash string
		row, col int
		length   int
		value    string
		want     string
	}{
		{"hidden field", "signon", 6, 20, 8, secretPassword, chaosHiddenValueMask},
		{"sensitive field", "signon", 7, 20, 12, secretAccount, chaosHiddenValueMask},
		{"ordinary field", "signon", 5, 20, 8, plainUserName, plainUserName},
		// The attempt names a screen the map no longer has: the position is
		// still a hidden field somewhere, so the value is still withheld.
		{"hidden field, screen not in the map", "gone", 6, 20, 8, secretPassword, chaosHiddenValueMask},
		{"nothing written", "signon", 6, 20, 8, "", ""},
	}
	for _, tc := range cases {
		if got := chaosAttemptWriteValue(mm, tc.fromHash, tc.row, tc.col, tc.length, tc.value); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func secretsStatus() chaos.Status {
	attempt := chaos.Attempt{
		Attempt:        4,
		Time:           time.Now(),
		FromHash:       "signon",
		ToHash:         "menu",
		AIDKey:         "Enter",
		FieldsTargeted: 3,
		FieldsWritten:  3,
		Transitioned:   true,
		FieldWrites: []chaos.AttemptFieldWrite{
			{Row: 5, Column: 20, Length: 8, Value: plainUserName, Success: true},
			{Row: 6, Column: 20, Length: 8, Value: secretPassword, Success: true},
			{Row: 7, Column: 20, Length: 12, Value: secretAccount, Success: true},
		},
	}
	return chaos.Status{
		Active:         true,
		StepsRun:       4,
		LastAttempt:    &attempt,
		RecentAttempts: []chaos.Attempt{attempt},
		MindMap:        secretsMindMap(),
	}
}

func TestChaosStatusJSONMasksAttemptValuesAndTheMap(t *testing.T) {
	out, err := json.Marshal(chaosStatusToJSON(secretsStatus()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertNoChaosSecrets(t, "live status", out)
	if !strings.Contains(string(out), plainUserName) {
		t.Error("an ordinary field write lost its value")
	}
}

func TestSessionChaosStateNeverHoldsHiddenValues(t *testing.T) {
	state := chaosStateFromStatus(secretsStatus(), nil, "host:23")
	for _, attempt := range append([]session.ChaosAttempt{*state.LastAttempt}, state.RecentAttempts...) {
		for _, fw := range attempt.FieldWrites {
			for _, secret := range chaosSecrets {
				if fw.Value == secret {
					t.Errorf("the session state holds a hidden field's value: %q", secret)
				}
			}
		}
	}
	if len(state.MindMap) != 0 {
		t.Error("the session state carries a map copy during the run")
	}
	out, _ := json.Marshal(chaosStateToJSON(state))
	assertNoChaosSecrets(t, "status from session state", out)
}

// Every route that serves the session's run, with the run loaded and no
// engine — the state a user is in when they read about what was found.
func TestChaosEndpointsNeverEmitHiddenValues(t *testing.T) {
	app, r, sessID := setupChaosEndpointsApp(t)
	r.GET("/chaos/business/overview", app.ChaosBusinessOverviewHandler)
	run := &chaos.SavedRun{
		SavedRunMeta: chaos.SavedRunMeta{ID: "secrets-run", StepsRun: 6, Transitions: 1, UniqueScreens: 2},
		MindMap:      secretsMindMap(),
		Attempts:     secretsStatus().RecentAttempts,
		AIDKeyCounts: map[string]int{"Enter": 4},
	}
	sess, _ := app.SessionManager.GetSession(sessID)
	if err := chaos.SaveRun(app.chaosRunsDirForSession(sess.OwnerID), run); err != nil {
		t.Fatalf("save: %v", err)
	}

	load, _ := json.Marshal(map[string]string{"runID": "secrets-run"})
	w := chaosRequest(r, http.MethodPost, "/chaos/load", load, sessID)
	if w.Code != http.StatusOK {
		t.Fatalf("load: %d — %s", w.Code, w.Body.String())
	}
	assertNoChaosSecrets(t, "POST /chaos/load", w.Body.Bytes())

	routes := []struct{ method, path string }{
		{http.MethodGet, "/chaos/status"},
		{http.MethodGet, "/chaos/status?verbose=true"},
		{http.MethodGet, "/chaos/screens"},
		{http.MethodGet, "/chaos/business/overview"},
		{http.MethodGet, "/chaos/insights"},
		{http.MethodGet, "/chaos/mindmap/export"},
		{http.MethodPost, "/chaos/report"},
	}
	for _, route := range routes {
		w := chaosRequest(r, route.method, route.path, nil, sessID)
		if w.Code != http.StatusOK {
			t.Errorf("%s %s: want 200, got %d — %s", route.method, route.path, w.Code, w.Body.String())
			continue
		}
		assertNoChaosSecrets(t, route.method+" "+route.path, w.Body.Bytes())
	}

	// The verbose status and the screens list still say that a value is
	// known, and still show the ordinary one.
	for _, path := range []string{"/chaos/status?verbose=true", "/chaos/screens"} {
		body := chaosRequest(r, http.MethodGet, path, nil, sessID).Body.String()
		if !strings.Contains(body, chaosHiddenValueMask) || !strings.Contains(body, plainUserName) {
			t.Errorf("%s: want the mask for the hidden field and the plain value for the ordinary one", path)
		}
	}

	// The report names the field and says the value is withheld.
	report := chaosRequest(r, http.MethodPost, "/chaos/report", nil, sessID).Body.String()
	if !strings.Contains(report, "withheld") {
		t.Error("the report does not say the hidden field's values were withheld")
	}
	if strings.Contains(report, chaosHiddenValueMask) {
		t.Error("the report prints the mask as if it were data")
	}

	// Serving it scrubbed did not scrub the run itself.
	held, _ := app.chaosEngines.getLoadedRun(sessID)
	if got := held.MindMap.Areas["signon"].KnownWorkingValues["R6C20L8"]; len(got) != 1 || got[0] != secretPassword {
		t.Errorf("the loaded run lost what it knew: %v", got)
	}
}

// A recording does not say which fields were hidden, so a password typed
// while recording would be seeded as an ordinary known value. The metadata a
// recording does carry marks the field hidden.
func TestChaosSeedFromRecordingMarksSensitiveFillsHidden(t *testing.T) {
	workflow := &WorkflowConfig{
		Steps: []session.WorkflowStep{
			{Type: "Connect"},
			{Type: "FillString", Coordinates: &session.WorkflowCoordinates{Row: 5, Column: 20, Length: 8}, Text: plainUserName},
			{Type: "FillString", Coordinates: &session.WorkflowCoordinates{Row: 6, Column: 20, Length: 8}, Text: secretPassword},
			{Type: "PressEnter"},
			{Type: "SetVariable", Variable: "pin", Text: "ignored", Sensitive: true},
			{Type: "FillString", Coordinates: &session.WorkflowCoordinates{Row: 9, Column: 4, Length: 6}, Text: "${pin}"},
			{Type: "PressEnter"},
		},
		Parameters: []session.WorkflowParameter{
			{Name: "password", Row: 6, Column: 20, Length: 8, Sensitive: true},
		},
	}
	run := chaosSeedRunFromWorkflow(workflow)

	first := run.MindMap.Areas["recording:area-1"]
	if first == nil {
		t.Fatalf("no first area in the seed map: %v", run.MindMap.Areas)
	}
	if !first.FieldMetadata["R6C20L8"].Hidden {
		t.Error("the field a Sensitive parameter fills was not marked hidden")
	}
	if first.FieldMetadata["R5C20L8"].Hidden {
		t.Error("an ordinary field was marked hidden")
	}
	if first.HiddenFieldCount != 1 {
		t.Errorf("HiddenFieldCount = %d, want 1", first.HiddenFieldCount)
	}
	fromVariable := false
	for _, area := range run.MindMap.Areas {
		if meta, ok := area.FieldMetadata["R9C4L6"]; ok {
			fromVariable = meta.Hidden
		}
	}
	if !fromVariable {
		t.Error("a fill from a Sensitive variable was not marked hidden")
	}

	out, _ := json.Marshal(redactedMindMap(run.MindMap))
	assertNoChaosSecrets(t, "seed map as served", out)
	// The seed itself keeps the value: the run that continues from it needs it.
	if got := first.KnownWorkingValues["R6C20L8"]; len(got) != 1 || got[0] != secretPassword {
		t.Errorf("the seed lost the recorded value: %v", got)
	}
}
