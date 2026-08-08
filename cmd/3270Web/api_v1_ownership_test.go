package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/session"
)

// Every route that names a session in its path must refuse one belonging to
// somebody else — and the check has to be in each handler, because the flat
// /api/v1/sessions/:id/* group does not run APISessionScope.
//
// Enumerated from the router rather than listed here. A hand-written list is
// a list that goes stale the first time somebody adds an endpoint, which is
// exactly how DELETE /sessions/:id and the two profile routes came to be the
// three ways into another account's terminal after everything else was
// closed.
func TestEverySessionRouteRefusesAnotherAccountsSession(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	bob := addUser(t, app, "bob", authz.RoleUser, false)

	theirs := app.SessionManager.CreateSessionFor(bob.ID, mustMockHost(t))
	token := issueToken(t, app, alice.ID, "prober", nil)

	// A session of alice's own, so a route that answers "you have no session"
	// for an unrelated reason is not mistaken for one that refused properly.
	app.SessionManager.CreateSessionFor(alice.ID, mustMockHost(t))

	const marker = ":id"
	var checked int
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/sessions/"+marker) {
			continue
		}
		if route.Method == http.MethodOptions {
			continue // answered by CORS before anything looks at a session
		}

		path := strings.Replace(route.Path, marker, theirs.ID, 1)
		req := httptest.NewRequest(route.Method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		checked++

		// DELETE is the one exception, and its answer is checked separately
		// below: it must stay idempotent, so it reports success for a session
		// that is absent and for one that is not the caller's alike. What
		// matters there is that the session survives, which the check after
		// this loop covers.
		if route.Method == http.MethodDelete && route.Path == "/api/v1/sessions/"+marker {
			if w.Code != http.StatusOK {
				t.Errorf("DELETE stopped being idempotent: %d %s", w.Code, w.Body.String())
			}
			continue
		}

		// 404 "session not found" is the required answer: the same one an
		// invented ID gets, so the difference cannot be used to discover
		// which IDs are real.
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "session not found") {
			t.Errorf("%s %s reached another account's session: %d %s",
				route.Method, route.Path, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}

	// If the prefix stops matching, this test would pass by checking nothing.
	if checked < 20 {
		t.Fatalf("only %d session routes were checked; the enumeration is not finding them", checked)
	}

	// And bob's session is still there — a route that "refused" by destroying
	// it would otherwise look like a pass.
	if _, ok := app.SessionManager.PeekSession(theirs.ID); !ok {
		t.Error("bob's session was removed by a route that should have refused")
	}
}

// The other half of the DELETE rule: refusing quietly must not become
// refusing always.
func TestDeleteStillClosesYourOwnSession(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	mine := app.SessionManager.CreateSessionFor(alice.ID, mustMockHost(t))
	token := issueToken(t, app, alice.ID, "ci", nil)

	if w := apiCall(r, http.MethodDelete, "/api/v1/sessions/"+mine.ID, token, ""); w.Code != http.StatusOK {
		t.Fatalf("delete = %d (%s)", w.Code, w.Body.String())
	}
	if _, ok := app.SessionManager.PeekSession(mine.ID); ok {
		t.Error("the session survived its owner closing it")
	}
}

// The same enumeration for the browser surface, where the session comes from
// a cookie rather than a path.
func TestBrowserRoutesRefuseAPlantedSessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager(), authMode: authz.ModeLocal}
	theirs := app.SessionManager.CreateSessionFor("bob", mustMockHost(t))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/screen", nil)
	c.Request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: theirs.ID})
	c.Set(principalContextKey, authz.Principal{UserID: "alice", Role: authz.RoleUser, Kind: authz.KindWeb})

	if got := app.getSession(c); got != nil {
		t.Errorf("a planted cookie resolved to %s", got.ID)
	}
}
