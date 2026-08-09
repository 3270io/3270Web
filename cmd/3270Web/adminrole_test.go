package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
)

// patchUser drives the admin update endpoint as the caller holding cookie.
func patchUser(t *testing.T, r *gin.Engine, cookie, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/admin/users")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Demoting an administrator has to take hold of the session they are already
// in, for the same reason disabling an account ends its logins: a change that
// only applies at the next sign-in is not a revocation.
//
// It is the worse half of the pair, too. A demoted administrator whose live
// session kept the role could simply promote themselves back through the page
// they are still standing on, so the demotion would never take effect at all.
func TestDemotingAnAdministratorTakesEffectOnTheirLiveSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	// A second administrator, because the store refuses to demote the only one.
	second := addUser(t, app, "second", authz.RoleAdmin, false)

	rootCookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))
	secondCookie := authCookieFrom(doLogin(t, r, "second", loginTestPassword, "10.0.0.2"))
	if rootCookie == "" || secondCookie == "" {
		t.Fatal("could not sign both administrators in")
	}
	if w := getWithAuth(r, "/api/admin/users", secondCookie, "10.0.0.2"); w.Code != http.StatusOK {
		t.Fatalf("the second administrator could not administer before demotion: %d", w.Code)
	}

	if w := patchUser(t, r, rootCookie, second.ID, `{"role":"user"}`); w.Code != http.StatusOK {
		t.Fatalf("demote: status = %d (body %s)", w.Code, w.Body.String())
	}

	if w := getWithAuth(r, "/api/admin/users", secondCookie, "10.0.0.2"); w.Code != http.StatusForbidden {
		t.Errorf("a demoted administrator still reached account administration: status = %d, want 403", w.Code)
	}
	// The promotion path back, specifically: this is what would make the
	// demotion undoable by the person it was applied to.
	if w := patchUser(t, r, secondCookie, second.ID, `{"role":"admin"}`); w.Code != http.StatusForbidden {
		t.Errorf("a demoted administrator promoted themselves back: status = %d, want 403", w.Code)
	}
}

// The other direction, so the fix cannot be "end every session on any role
// change" without anyone noticing that it signs people out for being promoted.
func TestPromotingAnAccountTakesEffectWithoutSigningThemOut(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	bob := addUser(t, app, "bob", authz.RoleUser, false)

	rootCookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))
	bobCookie := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.2"))
	if rootCookie == "" || bobCookie == "" {
		t.Fatal("could not sign both accounts in")
	}

	if w := patchUser(t, r, rootCookie, bob.ID, `{"role":"admin"}`); w.Code != http.StatusOK {
		t.Fatalf("promote: status = %d (body %s)", w.Code, w.Body.String())
	}

	w := getWithAuth(r, "/api/admin/users", bobCookie, "10.0.0.2")
	if w.Code == http.StatusUnauthorized {
		t.Fatal("promotion signed the account out of the session it was already in")
	}
	if w.Code != http.StatusOK {
		t.Errorf("a promoted account could not administer: status = %d (body %s)", w.Code, w.Body.String())
	}
}

// An account can be disabled without the web interface: the console command
// edits the same file the running server reads, which is the documented way to
// do it without a browser. The server has no way to notice, so its periodic
// sweep is what makes that disable reach a browser already signed in.
func TestTheSweepEndsLoginsForAnAccountDisabledOutsideTheWebInterface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	addUser(t, app, "bob", authz.RoleUser, false)

	cookie := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.2"))
	if cookie == "" {
		t.Fatal("could not sign in")
	}

	// Straight to the store, as the console command does.
	if err := app.userStore().SetDisabled("bob", true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if w := getWithAuth(r, "/api/whoami", cookie, "10.0.0.2"); w.Code == http.StatusUnauthorized {
		t.Fatal("the login ended before the sweep ran; this test would prove nothing")
	}

	app.sweepLogins()

	// 401 rather than a whoami saying "not authenticated": the route is behind
	// the login gate, so an ended login is refused before the handler runs.
	if w := getWithAuth(r, "/api/whoami", cookie, "10.0.0.2"); w.Code != http.StatusUnauthorized {
		t.Errorf("a disabled account's login survived the sweep: status = %d (body %s)", w.Code, w.Body.String())
	}
}

// The counterpart: a sweep that ended everyone's login would pass the test
// above and be useless.
func TestTheSweepLeavesAValidLoginAlone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	addUser(t, app, "bob", authz.RoleUser, false)

	cookie := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.2"))
	app.sweepLogins()

	if w := getWithAuth(r, "/api/whoami", cookie, "10.0.0.2"); !strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Errorf("the sweep signed out an account in good standing: %s", w.Body.String())
	}
}
