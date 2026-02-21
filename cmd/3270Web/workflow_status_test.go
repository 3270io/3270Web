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
