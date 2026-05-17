package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/profiler"
	"github.com/jnnngs/3270Web/internal/session"
)

func newTestAppWithSession(t *testing.T, mh *host.MockHost) (*App, *session.Session) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	app := &App{
		SessionManager: session.NewManager(),
		chaosEngines:   newChaosEngineStore(),
		profiles:       newProfileCache(),
	}
	sess := app.SessionManager.CreateSession(mh)
	sess.TargetHost = "host.example"
	sess.TargetPort = 23
	return app, sess
}

func TestProfileHandler_ReturnsProfileForActiveSession(t *testing.T) {
	mh, _ := host.NewMockHost("")
	mh.Connected = true
	mh.QueryResponses = map[string]string{
		"Host":  "host mvs.example.com 23",
		"Model": "IBM-3279-2-E",
	}
	if mh.Screen != nil {
		mh.Screen.Status = "U F P C(localhost) I 4 24 80 0 0 0x0 0.000"
	}
	app, sess := newTestAppWithSession(t, mh)

	r := gin.New()
	r.POST("/profile", app.ProfileHandler)
	req := httptest.NewRequest(http.MethodPost, "/profile", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var p profiler.CompatibilityProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode profile: %v\nbody: %s", err, rec.Body.String())
	}
	if p.SchemaVersion != profiler.SchemaVersion {
		t.Errorf("schema_version: got %q want %q", p.SchemaVersion, profiler.SchemaVersion)
	}
	if p.Device.TerminalType != "IBM-3279-2-E" {
		t.Errorf("terminal_type: got %q", p.Device.TerminalType)
	}
	if p.Host.Host != "mvs.example.com" {
		t.Errorf("host: got %q", p.Host.Host)
	}
	// Cached profile should be readable via GET /profile.
	getReq := httptest.NewRequest(http.MethodGet, "/profile", nil)
	getReq.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	getRec := httptest.NewRecorder()
	r2 := gin.New()
	r2.GET("/profile", app.ProfileGetHandler)
	r2.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Errorf("GET /profile after probe: status %d body=%s", getRec.Code, getRec.Body.String())
	}
}

func TestProfileGetHandler_404IfNoProbe(t *testing.T) {
	mh, _ := host.NewMockHost("")
	mh.Connected = true
	app, sess := newTestAppWithSession(t, mh)

	r := gin.New()
	r.GET("/profile", app.ProfileGetHandler)
	req := httptest.NewRequest(http.MethodGet, "/profile", nil)
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 before probe, got %d", rec.Code)
	}
}

func TestProfileHandler_409IfHostNotConnected(t *testing.T) {
	mh, _ := host.NewMockHost("")
	mh.Connected = false
	app, sess := newTestAppWithSession(t, mh)

	r := gin.New()
	r.POST("/profile", app.ProfileHandler)
	req := httptest.NewRequest(http.MethodPost, "/profile", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 when host not connected, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChaosMindMapCompareHandler_JSONResponse(t *testing.T) {
	mh, _ := host.NewMockHost("")
	mh.Connected = true
	app, sess := newTestAppWithSession(t, mh)

	baseline := &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{
		"h-a": {Hash: "h-a", DedupSignature: "sig-a", FieldMetadata: map[string]chaos.MindMapFieldMetadata{
			"R1C1L5": {Row: 1, Column: 1, Length: 5},
		}},
	}}
	candidate := &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{
		"h-a": {Hash: "h-a", DedupSignature: "sig-a", FieldMetadata: map[string]chaos.MindMapFieldMetadata{
			"R1C1L5": {Row: 1, Column: 1, Length: 5},
			"R2C1L5": {Row: 2, Column: 1, Length: 5}, // added in candidate
		}},
	}}
	body, _ := json.Marshal(map[string]any{"baseline": baseline, "candidate": candidate})

	r := gin.New()
	r.POST("/chaos/mindmap/compare", app.ChaosMindMapCompareHandler)
	req := httptest.NewRequest(http.MethodPost, "/chaos/mindmap/compare", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var diff chaos.MindMapDiff
	if err := json.Unmarshal(rec.Body.Bytes(), &diff); err != nil {
		t.Fatalf("decode diff: %v\nbody: %s", err, rec.Body.String())
	}
	if diff.SchemaVersion != chaos.CompareSchemaVersion {
		t.Errorf("schema_version: %q", diff.SchemaVersion)
	}
	if diff.Summary.FieldChanges != 1 {
		t.Errorf("expected 1 field change, got %d", diff.Summary.FieldChanges)
	}
}

func TestChaosMindMapCompareHandler_HTMLResponse(t *testing.T) {
	mh, _ := host.NewMockHost("")
	mh.Connected = true
	app, sess := newTestAppWithSession(t, mh)

	body, _ := json.Marshal(map[string]any{
		"baseline":  &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{}},
		"candidate": &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{}},
	})
	r := gin.New()
	r.POST("/chaos/mindmap/compare", app.ChaosMindMapCompareHandler)
	req := httptest.NewRequest(http.MethodPost, "/chaos/mindmap/compare", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" || ct[:9] != "text/html" {
		t.Errorf("expected text/html, got %q", ct)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("Chaos Mind-Map Diff")) {
		t.Errorf("HTML body missing title; got: %s", rec.Body.String())
	}
}
