// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// TestScreenWriteHandlerFieldKeyIsOneIndexed guards against the coordinate-
// basis mismatch between the chaos catalog's field keys (1-indexed
// R<row>C<col>L<len>, e.g. from chaos_list_screens or a business function
// step) and write_field's row/col (0-indexed, from get_screen). The system
// prompt tells the model to pass a business function step's field_key
// straight through to write_field; before this fix there was no field_key
// parameter at all, so a model following that instruction naively would
// pass the 1-indexed numbers as if they were 0-indexed row/col — landing
// one row and one column off. This test drives the field_key path exactly
// as the model would and asserts the text lands at the correct, converted
// 0-indexed cell.
func TestScreenWriteHandlerFieldKeyIsOneIndexed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.Screen = &host.Screen{Width: 80, Height: 24, IsFormatted: true}
	mh.Screen.Buffer = make([][]rune, 24)
	for i := range mh.Screen.Buffer {
		mh.Screen.Buffer[i] = make([]rune, 80)
		for j := range mh.Screen.Buffer[i] {
			mh.Screen.Buffer[i][j] = ' '
		}
	}

	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.POST("/screen/write", app.ScreenWriteHandler)

	// R6C11L8 -> 1-indexed row 6, col 11 -> 0-indexed row 5, col 10.
	body, _ := json.Marshal(map[string]string{
		"field_key": "R6C11L8",
		"text":      "hello",
	})
	req := httptest.NewRequest(http.MethodPost, "/screen/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	got := string(mh.Screen.Buffer[5][10:15])
	if got != "hello" {
		t.Fatalf("text landed at wrong cell: row 5 col 10..15 = %q, want \"hello\"", got)
	}
	// Confirm it did NOT land at the naive (unconverted, off-by-one) cell.
	wrongCell := string(mh.Screen.Buffer[6][11:16])
	if wrongCell == "hello" {
		t.Fatal("text landed at the 1-indexed (unconverted) cell instead of the correct 0-indexed cell")
	}
}

// TestScreenWriteHandlerRejectsInvalidFieldKey confirms a malformed
// field_key is rejected with 400 rather than silently falling back to
// row=0, col=0.
func TestScreenWriteHandlerRejectsInvalidFieldKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true

	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.POST("/screen/write", app.ScreenWriteHandler)

	body, _ := json.Marshal(map[string]string{
		"field_key": "not-a-field-key",
		"text":      "x",
	})
	req := httptest.NewRequest(http.MethodPost, "/screen/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// TestScreenWriteHandlerRowColStillWorks confirms the pre-existing 0-indexed
// row/col path (used by get_screen-driven manual writes) is unaffected by
// the field_key addition.
func TestScreenWriteHandlerRowColStillWorks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.Screen = &host.Screen{Width: 80, Height: 24, IsFormatted: true}
	mh.Screen.Buffer = make([][]rune, 24)
	for i := range mh.Screen.Buffer {
		mh.Screen.Buffer[i] = make([]rune, 80)
		for j := range mh.Screen.Buffer[i] {
			mh.Screen.Buffer[i][j] = ' '
		}
	}

	app := &App{SessionManager: session.NewManager()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.POST("/screen/write", app.ScreenWriteHandler)

	body, _ := json.Marshal(map[string]any{
		"row":  5,
		"col":  10,
		"text": "world",
	})
	req := httptest.NewRequest(http.MethodPost, "/screen/write", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	got := string(mh.Screen.Buffer[5][10:15])
	if got != "world" {
		t.Fatalf("text landed at wrong cell: row 5 col 10..15 = %q, want \"world\"", got)
	}
}
