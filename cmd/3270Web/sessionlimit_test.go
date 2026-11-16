// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
)

// postConnect submits the connect form the way a browser does, so the
// request passes the same-origin check.
func postConnect(r *gin.Engine, hostname string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	form := url.Values{"hostname": {hostname}}
	req := httptest.NewRequest(http.MethodPost, "/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Host = "example.test"
	req.RemoteAddr = "10.0.0.1:40000"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// At the session cap the connect page used to say "Connection error" over a
// sentence telling the person to close a session, and offered no way to find
// one. The panel is now titled for what happened and lists the sessions the
// account holds, each with the two things worth doing to it.
func TestConnectPage_SessionLimitListsOpenSessions(t *testing.T) {
	t.Setenv("MAX_SESSIONS_PER_USER", "2")
	app, r := newAuthTestApp(t, "none")
	first := newOwnedMockSession(t, app, authz.LocalUserID, "alpha.example:23")
	second := newOwnedMockSession(t, app, authz.LocalUserID, "bravo.example:23")

	w := postConnect(r, "sampleapp:app1:3272")

	body := w.Body.String()
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(body, "Session limit reached") {
		t.Errorf("the panel is not titled for the session limit:\n%s", body)
	}
	if strings.Contains(body, "Connection error") {
		t.Errorf("a session-limit refusal is still titled %q", "Connection error")
	}
	for _, s := range []string{first.ID, second.ID} {
		if !strings.Contains(body, `data-session-id="`+s+`"`) {
			t.Errorf("session %s is not listed", s)
		}
	}
	for _, want := range []string{"data-session-resume", "data-session-close", "alpha.example", "bravo.example"} {
		if !strings.Contains(body, want) {
			t.Errorf("the session list is missing %q", want)
		}
	}
}

// A host that did not answer is not a session limit, and gets no session list.
func TestConnectPage_HostFailureKeepsTheConnectionErrorTitle(t *testing.T) {
	t.Setenv("MAX_SESSIONS_PER_USER", "2")
	app, r := newAuthTestApp(t, "none")
	newOwnedMockSession(t, app, authz.LocalUserID, "alpha.example:23")

	// An unreachable name, refused before anything starts.
	w := postConnect(r, "localhost:3270")

	body := w.Body.String()
	if !strings.Contains(body, "Connection error") {
		t.Errorf("a refused host lost its title:\n%s", body)
	}
	if strings.Contains(body, "Session limit reached") || strings.Contains(body, "data-session-id") {
		t.Errorf("a host failure was shown as a session limit")
	}
}

// Nothing to list means no list, rather than an empty box with two headings.
func TestConnectPage_SessionLimitWithNoSessionsOfYoursHasNoList(t *testing.T) {
	t.Setenv("MAX_TOTAL_SESSIONS", "1")
	app, r := newAuthTestApp(t, "none")
	// Somebody else holds the only slot.
	newOwnedMockSession(t, app, "someone-else", "alpha.example:23")

	w := postConnect(r, "sampleapp:app1:3272")

	body := w.Body.String()
	if !strings.Contains(body, "Session limit reached") {
		t.Errorf("the instance-wide limit was not titled as a session limit:\n%s", body)
	}
	if strings.Contains(body, "data-session-id") {
		t.Errorf("another account's session was listed")
	}
}

// The sessions listed are the account's, which is not the same as this
// browser's roster: a cleared or expired cookie, or a session opened from
// another browser or by an API client, still holds a slot.
func TestAccountSessionTabsIncludeSessionsOutsideTheRoster(t *testing.T) {
	app := newSessionsApp(t)
	inRoster := newOwnedMockSession(t, app, authz.LocalUserID, "alpha.example:23")
	elsewhere := newOwnedMockSession(t, app, authz.LocalUserID, "bravo.example:23")
	other := newOwnedMockSession(t, app, "someone-else", "charlie.example:23")

	var ids []string
	withCookies(app, map[string]string{sessionRosterCookieName: inRoster.ID}, func(c *gin.Context) {
		c.Set(principalContextKey, authz.Local())
		for _, tab := range app.accountSessionTabs(c) {
			ids = append(ids, tab.ID)
		}
	})

	got := strings.Join(ids, ",")
	if !strings.Contains(got, inRoster.ID) || !strings.Contains(got, elsewhere.ID) {
		t.Errorf("tabs = %v, want both of the account's sessions", ids)
	}
	if strings.Contains(got, other.ID) {
		t.Errorf("tabs = %v include another account's session", ids)
	}
}

func sessionsPost(r *gin.Engine, path, id string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	body := url.Values{"id": {id}}.Encode()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Closing is how somebody gets out of the limit, so it has to work on a
// session the account owns even when this browser never held it. Ownership is
// the boundary; the roster was before ownership existed.
func TestCloseAcceptsAnOwnedSessionOutsideTheRoster(t *testing.T) {
	app := newSessionsApp(t)
	// cleanupSession stops any chaos run the session had.
	app.chaosEngines = newChaosEngineStore()
	elsewhere := newOwnedMockSession(t, app, authz.LocalUserID, "bravo.example:23")

	r := gin.New()
	r.Use(app.Authenticate())
	r.POST("/sessions/close", app.SessionsCloseHandler)

	w := sessionsPost(r, "/sessions/close", elsewhere.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if _, alive := app.SessionManager.PeekSession(elsewhere.ID); alive {
		t.Error("the owned session is still running")
	}
}

func TestSwitchAcceptsAnOwnedSessionOutsideTheRoster(t *testing.T) {
	app := newSessionsApp(t)
	elsewhere := newOwnedMockSession(t, app, authz.LocalUserID, "bravo.example:23")

	r := gin.New()
	r.Use(app.Authenticate())
	r.POST("/sessions/switch", app.SessionsSwitchHandler)

	w := sessionsPost(r, "/sessions/switch", elsewhere.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	var active, roster string
	for _, sc := range w.Result().Cookies() {
		switch sc.Name {
		case sessionCookieName:
			active = sc.Value
		case sessionRosterCookieName:
			roster = sc.Value
		}
	}
	if active != elsewhere.ID {
		t.Errorf("active session = %q, want %q", active, elsewhere.ID)
	}
	if !strings.Contains(roster, elsewhere.ID) {
		t.Errorf("roster = %q, want the resumed session added to it", roster)
	}
}

// What the roster check protected is still protected: another account's
// session, and one nobody owns, stay out of reach.
func TestSwitchAndCloseStillRefuseSessionsTheAccountDoesNotOwn(t *testing.T) {
	app := newSessionsApp(t)
	theirs := newOwnedMockSession(t, app, "someone-else", "charlie.example:23")

	r := gin.New()
	r.Use(app.Authenticate())
	r.POST("/sessions/switch", app.SessionsSwitchHandler)
	r.POST("/sessions/close", app.SessionsCloseHandler)

	for _, path := range []string{"/sessions/switch", "/sessions/close"} {
		if w := sessionsPost(r, path, theirs.ID); w.Code != http.StatusNotFound {
			t.Errorf("%s on another account's session = %d, want 404", path, w.Code)
		}
	}
	if _, alive := app.SessionManager.PeekSession(theirs.ID); !alive {
		t.Error("another account's session was closed")
	}
}
