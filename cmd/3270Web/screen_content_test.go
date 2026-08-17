// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/render"
	"github.com/jnnngs/3270Web/internal/session"
)

// TestScreenContentHandlerIncludesStatusLineFields guards the OIA status
// line (keyboard lock, model, dimensions, cursor): it renders once at page
// load from ScreenHandler's template data and is otherwise never touched,
// since the async refresh only ever replaced .screen-container's innerHTML.
// Fixing that client-side requires /screen/content to actually carry these
// values in the first place.
func TestScreenContentHandlerIncludesStatusLineFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.Screen = buildSampleApp1Screen()
	mh.Screen.Status = "U F P C(localhost) I 4 24 80 5 12 0x0 0.000"

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

	var payload struct {
		StatusKeyboard   string `json:"statusKeyboard"`
		StatusModel      string `json:"statusModel"`
		StatusDimensions string `json:"statusDimensions"`
		StatusCursor     string `json:"statusCursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload.StatusKeyboard != "UNLOCKED" {
		t.Errorf("statusKeyboard = %q, want %q", payload.StatusKeyboard, "UNLOCKED")
	}
	if payload.StatusModel != "4" {
		t.Errorf("statusModel = %q, want %q", payload.StatusModel, "4")
	}
	if payload.StatusDimensions != "24x80" {
		t.Errorf("statusDimensions = %q, want %q", payload.StatusDimensions, "24x80")
	}
	if payload.StatusCursor != "6,13" {
		t.Errorf("statusCursor = %q, want %q", payload.StatusCursor, "6,13")
	}
}
