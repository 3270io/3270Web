// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
)

// settingsSaver drives the settings endpoint the way the form does, as an
// administrator with a terminal session.
type settingsSaver struct {
	t      *testing.T
	app    *App
	r      *gin.Engine
	cookie string
	term   string
}

func newSettingsSaver(t *testing.T) *settingsSaver {
	t.Helper()
	app, r := newAuthTestApp(t, "local")
	root := addUser(t, app, "root", authz.RoleAdmin, false)
	cookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))
	terminal := app.SessionManager.CreateSessionFor(root.ID, mustMockHost(t))
	return &settingsSaver{t: t, app: app, r: r, cookie: cookie, term: terminal.ID}
}

func (s *settingsSaver) do(method string, body string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, "/api/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/screen")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: s.cookie})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: s.term})
	w := httptest.NewRecorder()
	s.r.ServeHTTP(w, req)
	return w
}

// served is what the settings form is given: every setting and its value.
func (s *settingsSaver) served() map[string]string {
	s.t.Helper()
	w := s.do(http.MethodGet, "")
	if w.Code != http.StatusOK {
		s.t.Fatalf("GET /api/settings = %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Settings map[string]string `json:"settings"`
		ReadOnly []string          `json:"readOnly"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		s.t.Fatal(err)
	}
	// The form leaves the read-only ones out of what it posts.
	for _, key := range body.ReadOnly {
		delete(body.Settings, key)
	}
	return body.Settings
}

// save posts the whole form, as the page does, with edits applied.
func (s *settingsSaver) save(edits map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	form := s.served()
	for key, value := range edits {
		form[key] = value
	}
	payload, err := json.Marshal(map[string]any{"settings": form})
	if err != nil {
		s.t.Fatal(err)
	}
	w := s.do(http.MethodPost, string(payload))
	if w.Code != http.StatusOK {
		s.t.Fatalf("POST /api/settings = %d %s", w.Code, w.Body.String())
	}
	return w
}

func savedEntries(t *testing.T, app *App) []audit.Entry {
	t.Helper()
	var out []audit.Entry
	for _, e := range trailOf(t, app) {
		if e.Event == audit.EventSettingsSaved {
			out = append(out, e)
		}
	}
	return out
}

// The form posts every setting it shows, so a save that changed one checkbox
// used to record the whole list as changed — some sixty keys, which an
// administrator reading the trail cannot tell from a real sweep.
func TestSettingsAuditNamesOnlyWhatChanged(t *testing.T) {
	s := newSettingsSaver(t)
	if n := len(s.served()); n < 10 {
		t.Fatalf("the form carries only %d settings; this test proves nothing", n)
	}

	s.save(map[string]string{"ALLOW_LOG_ACCESS": "true"})

	entries := savedEntries(t, s.app)
	if len(entries) != 1 {
		t.Fatalf("recorded %d settings entries, want 1", len(entries))
	}
	if got := entries[0].Detail["keys"]; got != "ALLOW_LOG_ACCESS" {
		t.Errorf("keys = %q, want only ALLOW_LOG_ACCESS", got)
	}
}

// Saving a form nobody touched changes nothing, so there is nothing to record.
func TestSettingsAuditSkipsASaveThatChangedNothing(t *testing.T) {
	s := newSettingsSaver(t)

	s.save(nil)

	if entries := savedEntries(t, s.app); len(entries) != 0 {
		t.Errorf("recorded %d settings entries for a save that changed nothing: %+v", len(entries), entries)
	}
}

// The second save of the same value is not a change either.
func TestSettingsAuditDoesNotRepeatAnEarlierChange(t *testing.T) {
	s := newSettingsSaver(t)
	s.save(map[string]string{"ALLOW_LOG_ACCESS": "true"})
	s.save(map[string]string{"CHAOS_MAX_STEPS": "250"})

	entries := savedEntries(t, s.app)
	if len(entries) != 2 {
		t.Fatalf("recorded %d settings entries, want 2", len(entries))
	}
	// Newest first.
	if got := entries[0].Detail["keys"]; got != "CHAOS_MAX_STEPS" {
		t.Errorf("second save recorded %q, want only CHAOS_MAX_STEPS", got)
	}
}

// A secret is served masked and posted back as the mask when it was not
// touched, which is not a change; a new value is, and is named without being
// written to the trail.
func TestSettingsAuditTreatsAnEchoedSecretAsUnchanged(t *testing.T) {
	s := newSettingsSaver(t)
	const secret = "hunter2-the-keyfile-password"

	s.save(map[string]string{"S3270_KEY_PASSWORD": secret})
	first := savedEntries(t, s.app)
	if len(first) != 1 || first[0].Detail["keys"] != "S3270_KEY_PASSWORD" {
		t.Fatalf("setting the secret recorded %+v", first)
	}

	// The form now serves the mask; saving it back changes nothing.
	s.save(nil)
	if again := savedEntries(t, s.app); len(again) != 1 {
		t.Errorf("echoing the mask recorded another entry: %+v", again)
	}
	raw, err := readWholeAudit(s.app)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, secret) {
		t.Error("a setting's value was written to the audit log")
	}
}
