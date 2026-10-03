// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"encoding/json"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUIPreferencesFollowOnlyTheirAuthenticatedOwner(t *testing.T) {
	app, router := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	bob := addUser(t, app, "bob", authz.RoleUser, false)
	cookie := authCookieFrom(doLogin(t, router, alice.Username, loginTestPassword, "10.0.0.1"))
	req := httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader(`{"workspaceMode":"engineering","quickStartDismissed":true,"terminalView":"readable","userID":"`+bob.ID+`"}`))
	req.Host = "example.test"
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	other, _, _ := app.userStore().ByID(bob.ID)
	if other.UIPreferences.WorkspaceMode != "" {
		t.Fatal("preferences leaked to another account")
	}
	// A new sign-in represents another browser; it sees the persisted choices.
	nextCookie := authCookieFrom(doLogin(t, router, alice.Username, loginTestPassword, "10.0.0.2"))
	got := getWithAuth(router, "/api/preferences", nextCookie, "10.0.0.2")
	var prefs users.UIPreferences
	if json.Unmarshal(got.Body.Bytes(), &prefs) != nil || prefs.WorkspaceMode != "engineering" || !prefs.QuickStartDismissed || prefs.TerminalView != "readable" {
		t.Fatalf("preferences did not survive new login: %s", got.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader(`{"workspaceMode":"admin"}`))
	req.Host = "example.test"
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: nextCookie})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid choice accepted: %d", w.Code)
	}
}

func TestAccessHelpRejectsExecutableURLs(t *testing.T) {
	for _, value := range []string{"javascript:alert(1)", "data:text/html,bad", "http://helpdesk.example"} {
		t.Setenv("AUTH_HELP_URL", value)
		if accessHelpURL() != "" {
			t.Fatal("unsafe help URL accepted")
		}
	}
	t.Setenv("AUTH_HELP_URL", "https://helpdesk.example/3270")
	if accessHelpURL() == "" {
		t.Fatal("HTTPS help URL rejected")
	}
}
