package main

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
)

// adminOnlyRoutes is what the whole instance shares: settings every session
// reads, a log holding everyone's activity, a restart that ends everyone's
// sessions, the account list, and the audit trail.
//
// Kept as an explicit list because the test below checks it in *both*
// directions. A route here that a regular account can reach is a hole; a route
// not here that refuses a regular account is one somebody gated without
// recording why. Either way the test says which, so the list cannot quietly
// drift from the router.
var adminOnlyRoutes = map[string]bool{
	"GET /api/settings":               true,
	"POST /api/settings":              true,
	"POST /app/restart":               true,
	"GET /logs":                       true,
	"GET /logs/access":                true,
	"POST /logs/access":               true,
	"POST /logs/toggle":               true,
	"POST /logs/clear":                true,
	"GET /logs/download":              true,
	"GET /admin":                      true,
	"GET /api/admin/overview":         true,
	"GET /api/admin/sessions":         true,
	"DELETE /api/admin/sessions/:id":  true,
	"GET /admin/users":                true,
	"GET /api/admin/users":            true,
	"POST /api/admin/users":           true,
	"PATCH /api/admin/users/:id":      true,
	"DELETE /api/admin/users/:id":     true,
	"GET /admin/audit":                true,
	"GET /api/admin/audit":            true,
	"GET /api/admin/audit/download":   true,
	"GET /api/admin/menu-branding":    true,
	"POST /api/admin/menu-branding":   true,
	"GET /api/admin/menu-preview":     true,
	"GET /admin/session-screen":       true,
	"GET /api/admin/profiles":         true,
	"POST /api/admin/profiles":        true,
	"POST /api/admin/profiles/delete": true,
	"POST /api/admin/group-roles":     true,
	"GET /admin/groups":               true,
	"GET /api/admin/groups":           true,
	"POST /api/admin/groups":          true,
	"PATCH /api/admin/groups":         true,
	"DELETE /api/admin/groups":        true,
	"PATCH /api/admin/groups/:name":   true,
	"DELETE /api/admin/groups/:name":  true,
}

// A signed-in account is not the same as an entitled one. The existing
// classification test drives every route with no credential at all; this one
// drives them as an ordinary user, which is the caller a hole would actually
// be found by — somebody who already has a login.
//
// Both directions are checked, so the list above cannot drift from the router:
// a new administrative route that nobody adds to it shows up as "refuses a
// regular account but is not listed", and a listed route that stops being
// gated shows up as reachable.
func TestAdminRoutesRefuseARegularAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	addUser(t, app, "bob", authz.RoleUser, false)
	cookie := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.1"))
	if cookie == "" {
		t.Fatal("could not sign in as a regular account")
	}

	// A terminal session of bob's own, so a route that needs one is refused
	// for being admin-only rather than for having nothing to act on.
	own := app.SessionManager.CreateSessionFor(userID(t, app, "bob"), mustMockHost(t))

	var reachable, ungated []string
	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if route.Method == http.MethodOptions || strings.HasPrefix(route.Path, "/api/v1") {
			continue // token-authenticated, or a CORS preflight
		}
		if strings.HasPrefix(route.Path, "/static") || strings.HasPrefix(route.Path, "/assets") {
			continue
		}
		// A server-sent-events route holds the connection open until the
		// client goes away, so driving it with a recorder never returns. It is
		// covered by the anonymous classification test, which sees the login
		// gate refuse it before the handler runs.
		if route.Path == "/screen/stream" {
			continue
		}
		// Skipped because driving them takes the credential away, and every
		// route visited afterwards would then be answered as anonymous — which
		// looks like "not admin-only" for the rest of the table. The first
		// version of this test did exactly that and reported eight admin
		// routes as reachable.
		if route.Path == logoutPath || route.Path == changePasswordPath {
			continue
		}

		req := httptest.NewRequest(route.Method, requestPathFor(route.Path), strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Referer", "http://example.test/")
		req.Host = "example.test"
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: own.ID})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		refused := w.Code == http.StatusForbidden
		switch {
		case adminOnlyRoutes[key] && !refused:
			reachable = append(reachable, key+" -> "+http.StatusText(w.Code))
		case !adminOnlyRoutes[key] && refused:
			ungated = append(ungated, key)
		}
	}

	sort.Strings(reachable)
	sort.Strings(ungated)
	if len(reachable) > 0 {
		t.Errorf("a regular account reached instance-wide administration:\n  %s",
			strings.Join(reachable, "\n  "))
	}
	if len(ungated) > 0 {
		t.Errorf("these refuse a regular account but are not listed as admin-only; "+
			"add them to adminOnlyRoutes so the gate is recorded:\n  %s",
			strings.Join(ungated, "\n  "))
	}
}

// The same routes must of course still work for an administrator, or the
// test above would pass just as well with everything broken.
func TestAdminRoutesWorkForAnAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	cookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))
	own := app.SessionManager.CreateSessionFor(userID(t, app, "root"), mustMockHost(t))

	// Reading routes only: the writes restart the process, clear the log or
	// delete accounts, none of which a test wants as a side effect.
	for _, path := range []string{
		"/api/admin/users", "/api/admin/audit", "/api/settings", "/logs/access",
		"/api/admin/overview", "/api/admin/sessions",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: own.ID})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusForbidden {
			t.Errorf("%s refused an administrator: %s", path, w.Body.String())
		}
	}
}

// userID looks up an account's ID by name.
func userID(t *testing.T, app *App, username string) string {
	t.Helper()
	list, err := app.userStore().List()
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range list {
		if u.Username == username {
			return u.ID
		}
	}
	t.Fatalf("no account named %q", username)
	return ""
}
