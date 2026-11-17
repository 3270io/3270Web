// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/config"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// View > Virtual keypad is one person's choice about their own screen. It used
// to be written into the process environment and the shared .env file, so the
// next person to connect (or the next restart) got whatever the last person
// toggled, while Settings described the very same value as "Show the virtual
// keypad by default". The choice now lives in the browser; the server keeps
// only the per-session copy, so a reload within the session agrees with the
// page, and the instance default stays what Settings says it is.
func TestKeypadPrefHandler_NeverTouchesTheInstanceDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("APP_USE_KEYPAD", "false")
	envPath := filepath.Join(t.TempDir(), ".env")
	const initialEnv = "APP_USE_KEYPAD=false\nOTHER=1\n"
	if err := os.WriteFile(envPath, []byte(initialEnv), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{SessionManager: session.NewManager(), Config: &config.Config{}, envPath: envPath}

	r := gin.New()
	r.POST("/prefs/keypad", app.KeypadPrefHandler)

	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	sess := app.SessionManager.CreateSession(mh)

	post := func(value string) *httptest.ResponseRecorder {
		form := url.Values{"keypad": {value}}
		req := httptest.NewRequest(http.MethodPost, "/prefs/keypad", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	for _, step := range []struct {
		value string
		want  bool
	}{{"on", true}, {"off", false}, {"on", true}} {
		if w := post(step.value); w.Code != http.StatusOK {
			t.Fatalf("keypad=%s: status = %d, want 200, body=%s", step.value, w.Code, w.Body.String())
		}

		// The session's own copy follows the toggle: this is what a reload
		// inside the session renders when the browser has nothing stored.
		if got := sess.Prefs.UseKeypad; got != step.want {
			t.Errorf("keypad=%s: session UseKeypad = %v, want %v", step.value, got, step.want)
		}
		// The instance default does not: neither the process environment ...
		if got := os.Getenv("APP_USE_KEYPAD"); got != "false" {
			t.Errorf("keypad=%s: APP_USE_KEYPAD in the environment = %q, want it left at %q", step.value, got, "false")
		}
		// ... nor the shared .env that survives a restart.
		stored, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(stored) != initialEnv {
			t.Errorf("keypad=%s: .env was rewritten:\n%s", step.value, stored)
		}
	}

	// Someone else connecting afterwards starts from the instance default, not
	// from what the first person chose.
	other, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	next := app.SessionManager.CreateSession(other)
	app.applyDefaultPrefs(next)
	if next.Prefs.UseKeypad {
		t.Errorf("a new session inherited the first person's keypad choice; it should start from APP_USE_KEYPAD=false")
	}
}

// With no session there is nobody whose preference this could be, and the
// answer stays the same as for every other session-scoped preference.
func TestKeypadPrefHandler_NoSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Setenv("APP_USE_KEYPAD", "false")
	envPath := filepath.Join(t.TempDir(), ".env")
	app := &App{SessionManager: session.NewManager(), Config: &config.Config{}, envPath: envPath}

	r := gin.New()
	r.POST("/prefs/keypad", app.KeypadPrefHandler)

	form := url.Values{"keypad": {"on"}}
	req := httptest.NewRequest(http.MethodPost, "/prefs/keypad", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if got := os.Getenv("APP_USE_KEYPAD"); got != "false" {
		t.Errorf("APP_USE_KEYPAD = %q, want it left at %q", got, "false")
	}
	if _, err := os.Stat(envPath); err == nil {
		t.Errorf(".env was created by a request with no session")
	}
}
