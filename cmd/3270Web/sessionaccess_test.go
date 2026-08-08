package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func mustMockHost(t *testing.T) host.Host {
	t.Helper()
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	return mh
}

func TestMayUseSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newCtx := func(p authz.Principal) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Set(principalContextKey, p)
		return c
	}
	owned := func(owner string) *session.Session { return &session.Session{ID: "s", OwnerID: owner} }

	separated := &App{authMode: authz.ModeLocal}
	single := &App{authMode: authz.ModeNone}

	alice := authz.Principal{UserID: "alice", Role: authz.RoleUser, Kind: authz.KindWeb}
	bob := authz.Principal{UserID: "bob", Role: authz.RoleUser, Kind: authz.KindWeb}
	admin := authz.Principal{UserID: "root", Role: authz.RoleAdmin, Kind: authz.KindWeb}

	if !separated.mayUseSession(newCtx(alice), owned("alice")) {
		t.Error("alice cannot use her own session")
	}
	if separated.mayUseSession(newCtx(bob), owned("alice")) {
		t.Error("bob can use alice's session")
	}
	// Administering the instance is not the same power as driving somebody
	// else's authenticated terminal.
	if separated.mayUseSession(newCtx(admin), owned("alice")) {
		t.Error("an admin can drive another user's session")
	}
	if separated.mayUseSession(newCtx(authz.Anonymous()), owned("alice")) {
		t.Error("an anonymous caller can use a session")
	}
	// Unowned must not be readable as "anyone's" where users are separated.
	if separated.mayUseSession(newCtx(alice), owned("")) {
		t.Error("an unowned session was usable while users are separated")
	}
	if separated.mayUseSession(newCtx(alice), nil) {
		t.Error("a nil session was usable")
	}

	// With one operator there is nothing to separate.
	if !single.mayUseSession(newCtx(authz.Local()), owned("")) {
		t.Error("the single operator cannot use an unowned session")
	}
	if !single.mayUseSession(newCtx(authz.Local()), owned(authz.LocalUserID)) {
		t.Error("the single operator cannot use their own session")
	}

	// A token is issued to an account and reaches what that account reaches —
	// no special case for arriving on a bearer header rather than a cookie.
	aliceToken := authz.Token("alice", authz.RoleUser, []string{"read"})
	if !separated.mayUseSession(newCtx(aliceToken), owned("alice")) {
		t.Error("alice's token cannot reach alice's session")
	}
	if separated.mayUseSession(newCtx(aliceToken), owned("bob")) {
		t.Error("alice's token reached bob's session")
	}
	anonToken := authz.Principal{Kind: authz.KindAPIToken}
	if separated.mayUseSession(newCtx(anonToken), owned("alice")) {
		t.Error("a token Kind with no identity was accepted")
	}

	// With one operator the shared token is that operator, so the API can
	// drive a session opened in the browser and the other way round.
	if !single.mayUseSession(newCtx(authz.Service()), owned(authz.LocalUserID)) {
		t.Error("the shared token cannot reach the operator's own session")
	}
}

// A new authenticated mode must isolate by default. The alternative — a list
// of modes that isolate — fails silently: everything works and everybody can
// see everybody else's sessions.
func TestSeparatesUsersFailsClosedForNewModes(t *testing.T) {
	for mode, want := range map[authz.Mode]bool{
		"":              false,
		authz.ModeNone:  false,
		authz.ModeLocal: true,
		"oidc":          true,
		"proxy":         true,
	} {
		app := &App{authMode: mode}
		if got := app.separatesUsers(); got != want {
			t.Errorf("mode %q separatesUsers = %v, want %v", mode, got, want)
		}
	}
}

// getSession is the chokepoint every browser handler resolves through, so
// this is where isolation either holds or does not.
func TestGetSessionRefusesAnotherUsersSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager(), authMode: authz.ModeLocal}

	mine := app.SessionManager.CreateSessionFor("alice", mustMockHost(t))
	theirs := app.SessionManager.CreateSessionFor("bob", mustMockHost(t))

	get := func(p authz.Principal, id string) *session.Session {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: id})
		c.Set(principalContextKey, p)
		return app.getSession(c)
	}

	alice := authz.Principal{UserID: "alice", Role: authz.RoleUser, Kind: authz.KindWeb}
	if got := get(alice, mine.ID); got == nil || got.ID != mine.ID {
		t.Fatal("alice cannot reach her own session")
	}
	// Holding the ID is no longer enough; before ownership it was the whole
	// credential.
	if got := get(alice, theirs.ID); got != nil {
		t.Error("alice reached bob's session by presenting its id")
	}
	if got := get(authz.Anonymous(), mine.ID); got != nil {
		t.Error("an anonymous caller reached a session")
	}
}

// The scoped ID must come from one place. Appending a cookie meant the check
// and the effect could name different sessions.
func TestSessionIDForPrefersTheScopedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "from-cookie"})

	if got := sessionIDFor(c); got != "from-cookie" {
		t.Errorf("unscoped = %q, want the cookie", got)
	}
	scopeToSession(c, "from-path")
	if got := sessionIDFor(c); got != "from-path" {
		t.Errorf("scoped = %q, want the scoped value to win", got)
	}
	if got := sessionIDFor(nil); got != "" {
		t.Errorf("nil context = %q, want empty", got)
	}
}

// The API resolves sessions by path, and must answer the same way for "not
// yours" as for "does not exist" — otherwise the difference confirms which
// IDs are real.
func TestAPISessionScopeHidesOtherUsersSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager(), authMode: authz.ModeLocal}
	theirs := app.SessionManager.CreateSessionFor("bob", mustMockHost(t))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authz.Principal{UserID: "alice", Role: authz.RoleUser, Kind: authz.KindWeb})
	})
	r.GET("/api/v1/sessions/:id/probe", app.APISessionScope(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for _, tc := range []struct{ name, id string }{
		{"another user's session", theirs.ID},
		{"a session that does not exist", "0123456789abcdef0123456789abcdef"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+tc.id+"/probe", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", tc.name, w.Code)
		}
		if !strings.Contains(w.Body.String(), "session not found") {
			t.Errorf("%s: body = %q", tc.name, w.Body.String())
		}
	}
}

// Naming a session in the path is the API's way of choosing one, so it is
// also the way an API client would reach somebody else's. A token carries its
// owner's identity, and this is where that has to bite.
func TestAPITokenIsConfinedToItsOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager(), authMode: authz.ModeLocal}
	mine := app.SessionManager.CreateSessionFor("alice", mustMockHost(t))
	theirs := app.SessionManager.CreateSessionFor("bob", mustMockHost(t))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authz.Token("alice", authz.RoleUser, []string{"read", "write"}))
	})
	r.GET("/api/v1/sessions/:id/probe", app.APISessionScope(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	probe := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+id+"/probe", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := probe(mine.ID); w.Code != http.StatusOK {
		t.Errorf("own session = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	w := probe(theirs.ID)
	if w.Code != http.StatusNotFound {
		t.Errorf("another account's session = %d, want 404", w.Code)
	}
	// Same answer as an ID that does not exist, so the difference cannot be
	// used to discover which IDs are real.
	if !strings.Contains(w.Body.String(), "session not found") {
		t.Errorf("body = %q", w.Body.String())
	}

	// An admin's token is no different: administering an instance is not the
	// same power as typing into somebody's authenticated mainframe session.
	adminRouter := gin.New()
	adminRouter.Use(func(c *gin.Context) {
		c.Set(principalContextKey, authz.Token("carol", authz.RoleAdmin, nil))
	})
	adminRouter.GET("/api/v1/sessions/:id/probe", app.APISessionScope(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+theirs.ID+"/probe", nil)
	aw := httptest.NewRecorder()
	adminRouter.ServeHTTP(aw, req)
	if aw.Code != http.StatusNotFound {
		t.Errorf("an admin's token reached another account's session: %d", aw.Code)
	}
}

// The tab bar is built from a client-supplied cookie, so it must be filtered
// rather than trusted: a planted ID would otherwise show as a tab for
// somebody else's session.
func TestSessionRosterFiltersForeignSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager(), authMode: authz.ModeLocal}

	mine := app.SessionManager.CreateSessionFor("alice", mustMockHost(t))
	theirs := app.SessionManager.CreateSessionFor("bob", mustMockHost(t))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.AddCookie(&http.Cookie{
		Name:  sessionRosterCookieName,
		Value: strings.Join([]string{mine.ID, theirs.ID}, ","),
	})
	c.Set(principalContextKey, authz.Principal{UserID: "alice", Role: authz.RoleUser, Kind: authz.KindWeb})

	roster := app.sessionRoster(c)
	if len(roster) != 1 || roster[0] != mine.ID {
		t.Errorf("roster = %v, want only %s", roster, mine.ID)
	}
}

// TestEveryRouteIsClassified walks the real router and requires every route to
// refuse a caller with no credential.
//
// RequireLogin is global, so a browser route added later is behind it whether
// or not its author thought about authentication — which is the point of
// putting the gate there rather than in each handler. What this test actually
// guards is the exemptions: adding a path to publicPaths or publicPrefixes, or
// registering a group ahead of the middleware, is how a route stops being
// protected, and both are easy to do while believing otherwise. Verified to
// fail by temporarily adding a route to publicPaths.
func TestEveryRouteIsClassified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "local")

	app := newApp(t.TempDir())
	r, err := buildRouter(app)
	if err != nil {
		t.Fatalf("buildRouter: %v", err)
	}
	// A configured instance, not one waiting in first-run setup. Setup
	// redirects everything to /setup, which would make this pass without
	// RequireLogin being involved at all — the gate under test would never
	// run and an ungated route would look protected.
	if _, err := app.userStore().Add("probe-admin", loginTestPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	app.setup.complete()

	// Reachable without any credential. Everything here is either needed to
	// obtain one, or carries nothing worth protecting.
	public := map[string]bool{
		loginPath: true, logoutPath: true, setupPath: true,
		"/healthz": true, "/favicon.ico": true,
	}
	// Authenticated by a bearer token instead of a login session.
	tokenAuth := func(path string) bool { return strings.HasPrefix(path, "/api/v1") }
	// Static assets must load before the login page renders.
	static := func(path string) bool {
		return strings.HasPrefix(path, "/static") || strings.HasPrefix(path, "/assets") ||
			strings.HasPrefix(path, "/favicon")
	}

	var unclassified []string
	for _, route := range r.Routes() {
		path := route.Path
		switch {
		case public[path], static(path), tokenAuth(path):
			continue
		case strings.HasPrefix(path, "/admin/"), strings.HasPrefix(path, "/api/admin/"):
			continue // gated by RequireAdmin
		case path == changePasswordPath, path == "/api/whoami":
			continue // needs a login, which RequireLogin enforces globally
		default:
			// Everything else is a browser route behind RequireLogin. That is
			// a real classification, not a catch-all: the assertion below
			// proves each one actually refuses an anonymous caller.
		}

		req := httptest.NewRequest(route.Method, requestPathFor(path), nil)
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Referer", "http://example.test/")
		req.Host = "example.test"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// With no credential the answer must be a refusal or a redirect to
		// one of the pages that can produce one — never the resource.
		switch w.Code {
		case http.StatusUnauthorized, http.StatusForbidden,
			http.StatusServiceUnavailable, http.StatusNotFound:
		case http.StatusFound, http.StatusMovedPermanently, http.StatusSeeOther:
			to := w.Header().Get("Location")
			if to != loginPath && to != setupPath && to != changePasswordPath && to != "/" {
				unclassified = append(unclassified, route.Method+" "+path+" -> redirect "+to)
			}
		default:
			unclassified = append(unclassified,
				route.Method+" "+path+" -> "+http.StatusText(w.Code))
		}
	}

	if len(unclassified) > 0 {
		t.Errorf("these routes answered an unauthenticated caller instead of refusing:\n  %s",
			strings.Join(unclassified, "\n  "))
	}
}

// requestPathFor turns a route pattern into something requestable by filling
// in its parameters.
func requestPathFor(pattern string) string {
	parts := strings.Split(pattern, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "0123456789abcdef0123456789abcdef"
		}
		if strings.HasPrefix(part, "*") {
			parts[i] = "probe"
		}
	}
	return strings.Join(parts, "/")
}
