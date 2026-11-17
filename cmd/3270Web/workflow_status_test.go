// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func TestWorkflowStatusHandler_IncludesChaosAttemptDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mockHost.Connected = true

	app := &App{
		SessionManager: session.NewManager(),
		chaosEngines:   newChaosEngineStore(),
	}
	sess := app.SessionManager.CreateSession(mockHost)
	withSessionLock(sess, func() {
		sess.Chaos = &session.ChaosState{
			Active:        true,
			StartedAt:     time.Now(),
			StepsRun:      1,
			Transitions:   1,
			UniqueScreens: 2,
			UniqueInputs:  1,
			LastAttempt: &session.ChaosAttempt{
				Attempt:        1,
				Time:           time.Now(),
				FromHash:       "from123",
				ToHash:         "to456",
				AIDKey:         "Enter",
				FieldsTargeted: 2,
				FieldsWritten:  1,
				Transitioned:   true,
				FieldWrites: []session.ChaosFieldWrite{
					{
						Row:     3,
						Column:  11,
						Length:  5,
						Value:   "HELLO",
						Success: true,
					},
				},
			},
			RecentAttempts: []session.ChaosAttempt{
				{
					Attempt:        1,
					Time:           time.Now(),
					FromHash:       "from123",
					ToHash:         "to456",
					AIDKey:         "Enter",
					FieldsTargeted: 2,
					FieldsWritten:  1,
					Transitioned:   true,
				},
			},
			MindMap: json.RawMessage(`{"areas":{"from123":{"hash":"from123","label":"Sample App","knownWorkingValues":{"R3C11L5":["HELLO"]},"keyPresses":{"Enter":{"presses":1,"progressions":1,"destinations":{"to456":1}}}}}}`),
		}
	})

	r := gin.New()
	r.GET("/workflow/status", app.WorkflowStatusHandler)

	req := httptest.NewRequest(http.MethodGet, "/workflow/status", nil)
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /workflow/status: want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got, ok := payload["chaosActive"].(bool); !ok || !got {
		t.Fatalf("chaosActive = %v (ok=%v), want true", payload["chaosActive"], ok)
	}
	if got, ok := payload["chaosStepsRun"].(float64); !ok || int(got) != 1 {
		t.Fatalf("chaosStepsRun = %v (ok=%v), want 1", payload["chaosStepsRun"], ok)
	}
	if startedAt, _ := payload["chaosStartedAt"].(string); strings.TrimSpace(startedAt) == "" {
		t.Fatalf("chaosStartedAt = %q, want non-empty RFC3339 timestamp", startedAt)
	}
	chaosLast, ok := payload["chaosLastAttempt"].(map[string]interface{})
	if !ok {
		t.Fatalf("chaosLastAttempt missing or invalid: %T", payload["chaosLastAttempt"])
	}
	if aid, _ := chaosLast["aidKey"].(string); aid != "Enter" {
		t.Fatalf("chaosLastAttempt.aidKey = %q, want %q", aid, "Enter")
	}
	chaosEvents, ok := payload["chaosEvents"].([]interface{})
	if !ok || len(chaosEvents) == 0 {
		t.Fatalf("chaosEvents missing or empty: %v", payload["chaosEvents"])
	}
	firstEvent, _ := chaosEvents[0].(map[string]interface{})
	msg, _ := firstEvent["message"].(string)
	if !strings.Contains(msg, "Attempt 1 AID Enter") {
		t.Fatalf("chaosEvents[0].message = %q, want attempt details", msg)
	}
	// This endpoint is polled every 700 ms for the life of the page. It used
	// to carry the whole mind map on every poll — decoded and re-encoded per
	// request, read by nothing — and every field value of every recent
	// attempt. The map is served by GET /chaos/status?verbose=true; what is
	// left here is what the status strip shows.
	if _, present := payload["chaosMindMap"]; present {
		t.Fatalf("the workflow-status poll still carries the mind map: %v", payload["chaosMindMap"])
	}
	if _, present := chaosLast["fieldWrites"]; present {
		t.Fatalf("chaosLastAttempt still carries per-field writes: %v", chaosLast["fieldWrites"])
	}
	attempts, ok := payload["chaosAttempts"].([]interface{})
	if !ok || len(attempts) != 1 {
		t.Fatalf("chaosAttempts = %v, want the one recent attempt", payload["chaosAttempts"])
	}
	first, _ := attempts[0].(map[string]interface{})
	for _, key := range []string{"attempt", "aidKey", "fromHash", "toHash", "fieldsTargeted", "fieldsWritten", "transitioned"} {
		if _, present := first[key]; !present {
			t.Errorf("chaosAttempts[0] is missing %q", key)
		}
	}
	if _, present := first["fieldWrites"]; present {
		t.Errorf("chaosAttempts[0] still carries per-field writes")
	}
	if strings.Contains(w.Body.String(), "HELLO") {
		t.Errorf("a value typed into a field reached the workflow-status poll: %s", w.Body.String())
	}
}

func TestWorkflowStatusHandler_ChaosCompletionFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mockHost.Connected = true

	app := &App{
		SessionManager: session.NewManager(),
		chaosEngines:   newChaosEngineStore(),
	}
	sess := app.SessionManager.CreateSession(mockHost)
	withSessionLock(sess, func() {
		sess.Chaos = &session.ChaosState{
			Active:      false,
			StepsRun:    3,
			StoppedAt:   time.Now(),
			Transitions: 2,
		}
	})

	r := gin.New()
	r.GET("/workflow/status", app.WorkflowStatusHandler)

	req := httptest.NewRequest(http.MethodGet, "/workflow/status", nil)
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /workflow/status: want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got, ok := payload["chaosCompleted"].(bool); !ok || !got {
		t.Fatalf("chaosCompleted = %v (ok=%v), want true", payload["chaosCompleted"], ok)
	}
	if stepLabel, _ := payload["chaosStepLabel"].(string); !strings.Contains(stepLabel, "completed") {
		t.Fatalf("chaosStepLabel = %q, want completion text", stepLabel)
	}
	if stoppedAt, _ := payload["chaosStoppedAt"].(string); strings.TrimSpace(stoppedAt) == "" {
		t.Fatalf("chaosStoppedAt = %q, want non-empty RFC3339 timestamp", stoppedAt)
	}
}

func TestWorkflowStatusHandler_IncludesRecordingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mockHost.Connected = true

	app := &App{
		SessionManager: session.NewManager(),
		chaosEngines:   newChaosEngineStore(),
	}
	sess := app.SessionManager.CreateSession(mockHost)
	started := time.Now().Add(-45 * time.Second)
	withSessionLock(sess, func() {
		sess.Recording = &session.WorkflowRecording{
			Active:    true,
			StartedAt: started,
			FilePath:  "/tmp/recording-workflow.json",
			Steps: []session.WorkflowStep{
				{Type: "Connect"},
				{Type: "Type"},
				{Type: "Enter"},
			},
		}
		sess.Playback = &session.WorkflowPlayback{
			Active:    true,
			StartedAt: time.Now().Add(-20 * time.Second),
		}
	})

	r := gin.New()
	r.GET("/workflow/status", app.WorkflowStatusHandler)

	req := httptest.NewRequest(http.MethodGet, "/workflow/status", nil)
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /workflow/status: want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got, ok := payload["recordingActive"].(bool); !ok || !got {
		t.Fatalf("recordingActive = %v (ok=%v), want true", payload["recordingActive"], ok)
	}
	if got, ok := payload["recordingSteps"].(float64); !ok || int(got) != 3 {
		t.Fatalf("recordingSteps = %v (ok=%v), want 3", payload["recordingSteps"], ok)
	}
	if got, _ := payload["recordingFile"].(string); got != "recording-workflow.json" {
		t.Fatalf("recordingFile = %q, want %q", got, "recording-workflow.json")
	}
	if startedAt, _ := payload["recordingStartedAt"].(string); strings.TrimSpace(startedAt) == "" {
		t.Fatalf("recordingStartedAt = %q, want non-empty RFC3339 timestamp", startedAt)
	}
	if playbackStartedAt, _ := payload["playbackStartedAt"].(string); strings.TrimSpace(playbackStartedAt) == "" {
		t.Fatalf("playbackStartedAt = %q, want non-empty RFC3339 timestamp", playbackStartedAt)
	}
}
