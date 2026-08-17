// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/render"
	"github.com/jnnngs/3270Web/internal/session"
)

type oiaPayload struct {
	OIAOnline      string `json:"oiaOnline"`
	OIAConnected   bool   `json:"oiaConnected"`
	OIAPeer        string `json:"oiaPeer"`
	OIAIndicator   string `json:"oiaIndicator"`
	OIAExplanation string `json:"oiaExplanation"`
}

func fetchOIA(t *testing.T, status string) oiaPayload {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.Screen = buildSampleApp1Screen()
	mh.Screen.Status = status

	app := &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
	}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.GET("/screen/content", app.ScreenContentHandler)

	req := httptest.NewRequest(http.MethodGet, "/screen/content", nil)
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var payload oiaPayload
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return payload
}

// The screen refresh has to carry the OIA, not just the screen HTML: the
// client cannot otherwise tell "the host is thinking" from "the app broke",
// which is the whole reason the indicator exists.
func TestScreenContentCarriesOIA(t *testing.T) {
	tests := []struct {
		name          string
		status        string
		wantOnline    string
		wantConnected bool
		wantIndicator string
	}{
		{
			name:          "ready and owned by an application",
			status:        "U F P C(localhost) I 4 24 80 5 12 0x0 0.000",
			wantOnline:    "4-A",
			wantConnected: true,
			wantIndicator: "",
		},
		{
			name:          "host holding the keyboard",
			status:        "L F P C(localhost) I 4 24 80 5 12 0x0 0.000",
			wantOnline:    "4-A",
			wantConnected: true,
			wantIndicator: "X SYSTEM",
		},
		{
			name:          "operator error",
			status:        "E F P C(localhost) I 4 24 80 5 12 0x0 0.000",
			wantOnline:    "4-A",
			wantConnected: true,
			wantIndicator: "X -f",
		},
		{
			name:          "host hung up",
			status:        "U F P N I 4 24 80 5 12 0x0 0.000",
			wantOnline:    "–",
			wantConnected: false,
			wantIndicator: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fetchOIA(t, tt.status)
			if got.OIAOnline != tt.wantOnline {
				t.Errorf("oiaOnline = %q, want %q", got.OIAOnline, tt.wantOnline)
			}
			if got.OIAConnected != tt.wantConnected {
				t.Errorf("oiaConnected = %v, want %v", got.OIAConnected, tt.wantConnected)
			}
			if got.OIAIndicator != tt.wantIndicator {
				t.Errorf("oiaIndicator = %q, want %q", got.OIAIndicator, tt.wantIndicator)
			}
		})
	}
}

// oiaConnected=false is what drives the client's automatic reconnect, so a
// session whose status line is not yet readable must not report it — that
// would kick off a reconnect against a session that is merely still starting.
func TestOIADoesNotClaimDisconnectedWithoutStatus(t *testing.T) {
	got := fetchOIA(t, "")
	if got.OIAConnected {
		t.Error("oiaConnected = true with no status line")
	}
	if got.OIAOnline != "?" {
		t.Errorf("oiaOnline = %q, want %q for an unreadable status line", got.OIAOnline, "?")
	}
}

// Reconnect needs somewhere to reconnect to. With neither a session nor the
// last-target cookie it must fail cleanly rather than 500 or hang.
func TestReconnectWithoutTargetFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
	}
	r := gin.New()
	r.POST("/reconnect", app.ReconnectHandler)

	req := httptest.NewRequest(http.MethodPost, "/reconnect", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// The connect page renders a theme picker but has no session, so it cannot
// use the session-gated /api/themes. Before this the fetch 401'd and custom
// themes silently never appeared there; the list is inlined instead.
func TestConnectPageInlinesCustomThemes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	base := t.TempDir()
	dir := filepath.Join(base, "themes")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	themeFile := filepath.Join(dir, "midnight.json")
	body := `{"name":"Midnight","customTheme":{"bg":"#000000","fg":"#00ff00"}}`
	if err := os.WriteFile(themeFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	app := &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
		baseDir:        base,
	}

	items, err := app.listFileThemes(nil)
	if err != nil {
		t.Fatalf("listFileThemes: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Midnight" {
		t.Fatalf("listFileThemes() = %+v, want one theme named Midnight", items)
	}

	// /api/themes must stay gated — the inlining is what solves the connect
	// page, not opening the endpoint up.
	r := gin.New()
	r.GET("/api/themes", app.ThemeListHandler)
	req := httptest.NewRequest(http.MethodGet, "/api/themes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/themes without a session: status = %d, want 401", w.Code)
	}
}
