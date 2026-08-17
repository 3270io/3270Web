// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// uiSettingsFieldKeys returns every setting key the settings form in ui.js
// renders as an editable field.
func uiSettingsFieldKeys(t *testing.T) []string {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "ui.js"))
	if err != nil {
		t.Fatalf("read ui.js: %v", err)
	}
	pattern := regexp.MustCompile(`\{\s*key:\s*'([A-Z0-9_]+)'`)
	matches := pattern.FindAllStringSubmatch(string(source), -1)
	if len(matches) == 0 {
		t.Fatal("no settings fields found in ui.js — has the form been restructured?")
	}
	keys := make([]string, 0, len(matches))
	for _, match := range matches {
		keys = append(keys, match[1])
	}
	return keys
}

// The settings form posts every field it renders, so a field whose key the
// server refuses fails the whole save — including the fields the operator
// actually changed, on a tab they may never have opened. S3270_SCRIPT_PORT is
// how that happened: it is on settingsDeniedKeys (it opens a scripting port)
// yet the form offered it as an ordinary editable field, so every save
// answered "invalid settings".
//
// A field key must therefore be one of two things: writable, or a key the
// snapshot reports in readOnly so the form can render it locked and leave it
// out of the payload. A key that is neither — a typo, or a setting the server
// has since dropped — is the case that breaks saving again.
func TestSettingsForm_EveryRenderedFieldIsWritableOrReportedReadOnly(t *testing.T) {
	allowed := settingsWritableKeys()
	known := settingsDefaults()

	for _, key := range uiSettingsFieldKeys(t) {
		if settingsKeyWritable(key, allowed) {
			continue
		}
		if _, ok := known[key]; ok && settingsDeniedKeys[key] {
			// Reported as read-only by settingsSnapshot; the form locks it.
			continue
		}
		t.Errorf("ui.js renders %s as a field, but the settings API neither writes it nor reports it "+
			"as read-only — every save that includes it will fail", key)
	}
}

// A round trip of the whole form — GET the snapshot, post it back untouched —
// is what the Save button does, so it has to succeed. Nothing changed, so
// nothing should be rejected.
func TestUpdateSettings_WholeSnapshotRoundTrips(t *testing.T) {
	gin.SetMode(gin.TestMode)

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("S3270_KEY_PASSWORD=super-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{SessionManager: session.NewManager(), envPath: envPath}

	r := gin.New()
	r.GET("/api/settings", app.SettingsHandler)
	r.POST("/api/settings", app.SettingsHandler)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	sess := app.SessionManager.CreateSession(mh)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET settings: status = %d, body=%s", w.Code, w.Body.String())
	}

	var snapshot settingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	// The read-only keys are the ones the form is told not to send back. The
	// form renders a field for this one, so the snapshot has to name it —
	// silence here is what left the form offering an edit the save refused.
	if !slices.Contains(snapshot.ReadOnly, "S3270_SCRIPT_PORT") {
		t.Errorf("readOnly = %v, want it to name S3270_SCRIPT_PORT", snapshot.ReadOnly)
	}
	for _, key := range snapshot.ReadOnly {
		delete(snapshot.Settings, key)
	}

	body, err := json.Marshal(settingsPayload{Settings: snapshot.Settings})
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("posting the snapshot back: status = %d, body=%s", w.Code, w.Body.String())
	}

	stored, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "S3270_KEY_PASSWORD=super-secret") {
		t.Errorf("the round trip overwrote the stored secret: %s", stored)
	}
}

// An older cached copy of ui.js still posts the read-only keys. Echoing a value
// back unchanged is not an attempt to write anything, so it must not fail the
// save — while a value that differs is still refused.
func TestUpdateSettings_UnchangedReadOnlyKeyIsANoOp(t *testing.T) {
	gin.SetMode(gin.TestMode)

	envPath := filepath.Join(t.TempDir(), ".env")
	app := &App{SessionManager: session.NewManager(), envPath: envPath}

	r := gin.New()
	r.POST("/api/settings", app.SettingsHandler)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	sess := app.SessionManager.CreateSession(mh)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := post(`{"settings":{"S3270_SCRIPT_PORT":"","APP_USE_KEYPAD":"true"}}`); w.Code != http.StatusOK {
		t.Fatalf("unchanged read-only key: status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	stored, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "S3270_SCRIPT_PORT=") {
		t.Errorf("a read-only key was persisted: %s", stored)
	}
	if !strings.Contains(string(stored), "APP_USE_KEYPAD=true") {
		t.Errorf("the writable key in the same save was not persisted: %s", stored)
	}

	if w := post(`{"settings":{"S3270_SCRIPT_PORT":"4001"}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("changing a read-only key: status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}
