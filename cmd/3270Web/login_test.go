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

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

const loginTestPassword = "correct-horse-battery-staple"

// newAuthTestApp builds a real router in the given mode, so these tests
// exercise the middleware chain rather than a handler in isolation.
func newAuthTestApp(t *testing.T, mode string) (*App, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, mode)
	// Pin binding off by default; the binding tests turn it on explicitly.
	t.Setenv("AUTH_BIND_SESSION_IP", "false")

	app := newApp(t.TempDir())
	r, err := buildRouter(app)
	if err != nil {
		t.Fatalf("buildRouter: %v", err)
	}
	// These tests describe a configured instance. First-run setup arms itself
	// on an empty store and would otherwise intercept every request; the
	// setup flow has its own tests in setup_test.go.
	app.setup.complete()
	return app, r
}

func addUser(t *testing.T, app *App, name string, role authz.Role, mustChange bool) users.User {
	t.Helper()
	u, err := app.userStore().Add(name, loginTestPassword, role, mustChange)
	if err != nil {
		t.Fatalf("add user %q: %v", name, err)
	}
	return u
}

func doLogin(t *testing.T, r *gin.Engine, username, password, clientIP string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/login")
	req.Host = "example.test"
	if clientIP != "" {
		req.RemoteAddr = clientIP + ":40000"
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func authCookieFrom(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == authCookieName && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

func getWithAuth(r *gin.Engine, path, cookie, clientIP string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	}
	if clientIP != "" {
		req.RemoteAddr = clientIP + ":40000"
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// With no authentication configured nothing about the request path changes.
// This is the regression guard for the default deployment.
func TestAuthModeNone_NoLoginRequired(t *testing.T) {
	_, r := newAuthTestApp(t, "none")

	if w := getWithAuth(r, "/", "", "10.0.0.1"); w.Code == http.StatusFound &&
		strings.Contains(w.Header().Get("Location"), loginPath) {
		t.Fatal("AUTH_MODE=none redirected to the login page")
	}
	if w := getWithAuth(r, "/api/whoami", "", "10.0.0.1"); w.Code != http.StatusOK {
		t.Errorf("whoami under AUTH_MODE=none: status = %d, want 200", w.Code)
	}
	// The login page has nothing to offer when there are no accounts.
	w := getWithAuth(r, loginPath, "", "10.0.0.1")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Errorf("login page under AUTH_MODE=none: %d %q, want a redirect to /", w.Code, w.Header().Get("Location"))
	}
}

func TestAuthModeLocal_UnauthenticatedIsRefused(t *testing.T) {
	_, r := newAuthTestApp(t, "local")

	// A browser gets sent to the login page.
	w := getWithAuth(r, "/", "", "10.0.0.1")
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if loc := w.Header().Get("Location"); loc != loginPath {
		t.Errorf("Location = %q, want %q", loc, loginPath)
	}

	// A script gets JSON, because a redirect to HTML is useless to fetch().
	w = getWithAuth(r, "/api/whoami", "", "10.0.0.1")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("api status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "authentication required") {
		t.Errorf("api body = %q", w.Body.String())
	}
}

// The login page and health check must work before anyone can log in, or the
// instance is unusable and unmonitorable.
func TestAuthModeLocal_PublicPathsReachable(t *testing.T) {
	_, r := newAuthTestApp(t, "local")

	for _, path := range []string{loginPath, "/healthz"} {
		w := getWithAuth(r, path, "", "10.0.0.1")
		if w.Code == http.StatusFound {
			t.Errorf("%s redirected to login", path)
		}
		if w.Code >= 500 {
			t.Errorf("%s returned %d", path, w.Code)
		}
	}
}

func TestLoginSucceedsAndGrantsAccess(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)

	w := doLogin(t, r, "alice", loginTestPassword, "10.0.0.1")
	if w.Code != http.StatusFound {
		t.Fatalf("login status = %d, want %d (body=%s)", w.Code, http.StatusFound, w.Body.String())
	}
	cookie := authCookieFrom(w)
	if cookie == "" {
		t.Fatal("login did not set an auth cookie")
	}

	got := getWithAuth(r, "/api/whoami", cookie, "10.0.0.1")
	if got.Code != http.StatusOK {
		t.Fatalf("authenticated whoami = %d, want 200", got.Code)
	}
	body := got.Body.String()
	if !strings.Contains(body, `"authenticated":true`) || !strings.Contains(body, `"username":"alice"`) {
		t.Errorf("whoami body = %q", body)
	}
	// A plain user must not be reported as an admin.
	if !strings.Contains(body, `"isAdmin":false`) {
		t.Errorf("a RoleUser login reported isAdmin true: %q", body)
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)

	for _, tc := range []struct{ user, pass string }{
		{"alice", "wrong-password-entirely"},
		{"nobody", loginTestPassword},
		{"", ""},
	} {
		w := doLogin(t, r, tc.user, tc.pass, "10.0.0.1")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("login(%q): status = %d, want %d", tc.user, w.Code, http.StatusUnauthorized)
		}
		if authCookieFrom(w) != "" {
			t.Errorf("login(%q) set a cookie on failure", tc.user)
		}
		// The same message for every failure, so the form is not an oracle
		// for which usernames exist.
		if !strings.Contains(w.Body.String(), "Incorrect username or password") {
			t.Errorf("login(%q) body = %q", tc.user, w.Body.String())
		}
	}
}

// A pre-planted cookie value must not survive login, or an attacker who can
// set a cookie can fixate a session and inherit it once the victim signs in.
func TestLoginRotatesTheSessionID(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	user := addUser(t, app, "alice", authz.RoleUser, false)

	planted, err := app.authSessions.Create(user.ID, user.Username, user.Role, "10.0.0.1", false)
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{"username": {"alice"}, "password": {loginTestPassword}}
	req := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/login")
	req.Host = "example.test"
	req.RemoteAddr = "10.0.0.1:40000"
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: planted.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	issued := authCookieFrom(w)
	if issued == "" {
		t.Fatal("login did not issue a cookie")
	}
	if issued == planted.ID {
		t.Fatal("login adopted the pre-existing session id")
	}
	if got := getWithAuth(r, "/api/whoami", planted.ID, "10.0.0.1"); got.Code != http.StatusUnauthorized {
		t.Errorf("the planted session still works: %d", got.Code)
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	if cookie == "" {
		t.Fatal("no cookie from login")
	}

	req := httptest.NewRequest(http.MethodPost, logoutPath, nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/")
	req.Host = "example.test"
	req.RemoteAddr = "10.0.0.1:40000"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := getWithAuth(r, "/api/whoami", cookie, "10.0.0.1"); got.Code != http.StatusUnauthorized {
		t.Errorf("the session survived logout: %d", got.Code)
	}
}

func TestLoginThrottlesRepeatedFailures(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)

	var throttled bool
	for i := 0; i < loginFreeAttempts+3; i++ {
		w := doLogin(t, r, "alice", "wrong-password-entirely", "10.0.0.9")
		if w.Code == http.StatusTooManyRequests {
			throttled = true
			break
		}
	}
	if !throttled {
		t.Fatal("repeated failures were never throttled")
	}

	// The lockout must hold even for the correct password, or it is trivially
	// bypassed by the attacker's eventual correct guess.
	w := doLogin(t, r, "alice", loginTestPassword, "10.0.0.9")
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("correct password during lockout: status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if authCookieFrom(w) != "" {
		t.Error("a session was issued during a lockout")
	}

	// A successful login from elsewhere clears that key's counters.
	app.loginLimiter.Reset(loginLimitKeys("alice", "10.0.0.9")...)
	if w := doLogin(t, r, "alice", loginTestPassword, "10.0.0.9"); w.Code != http.StatusFound {
		t.Errorf("after reset: status = %d, want %d", w.Code, http.StatusFound)
	}
}

func TestSessionIPBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "local")
	t.Setenv("AUTH_BIND_SESSION_IP", "true")

	app := newApp(t.TempDir())
	r, err := buildRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	app.setup.complete()
	addUser(t, app, "alice", authz.RoleUser, false)

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	if cookie == "" {
		t.Fatal("no cookie from login")
	}

	if w := getWithAuth(r, "/api/whoami", cookie, "10.0.0.1"); w.Code != http.StatusOK {
		t.Fatalf("original address rejected: %d", w.Code)
	}
	if w := getWithAuth(r, "/api/whoami", cookie, "10.0.0.77"); w.Code != http.StatusUnauthorized {
		t.Errorf("a replay from another address was accepted: %d", w.Code)
	}
	// The failed replay must not have logged the real user out.
	if w := getWithAuth(r, "/api/whoami", cookie, "10.0.0.1"); w.Code != http.StatusOK {
		t.Errorf("the owner lost their session to someone else's replay: %d", w.Code)
	}
}

// A system-issued password may do exactly one thing until it is replaced.
func TestMustChangePasswordPinsTheSession(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, true)

	w := doLogin(t, r, "alice", loginTestPassword, "10.0.0.1")
	cookie := authCookieFrom(w)
	if cookie == "" {
		t.Fatal("no cookie from login")
	}
	if loc := w.Header().Get("Location"); loc != changePasswordPath {
		t.Errorf("login redirect = %q, want %q", loc, changePasswordPath)
	}

	// Everything else is refused until the password changes.
	got := getWithAuth(r, "/", cookie, "10.0.0.1")
	if got.Code != http.StatusFound || got.Header().Get("Location") != changePasswordPath {
		t.Errorf("/ = %d %q, want a redirect to %q", got.Code, got.Header().Get("Location"), changePasswordPath)
	}
	if api := getWithAuth(r, "/api/whoami", cookie, "10.0.0.1"); api.Code != http.StatusForbidden {
		t.Errorf("api during forced change = %d, want %d", api.Code, http.StatusForbidden)
	}

	// The change page itself must be reachable, or there is no way out.
	if page := getWithAuth(r, changePasswordPath, cookie, "10.0.0.1"); page.Code != http.StatusOK {
		t.Errorf("change-password page = %d, want 200", page.Code)
	}
}

func TestChangePasswordEndsOtherSessions(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)

	first := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	second := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.2"))
	if first == "" || second == "" {
		t.Fatal("expected two logins")
	}

	const next = "an-entirely-new-password"
	form := url.Values{
		"currentPassword": {loginTestPassword},
		"newPassword":     {next},
		"confirmPassword": {next},
	}
	req := httptest.NewRequest(http.MethodPost, changePasswordPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/account/password")
	req.Host = "example.test"
	req.RemoteAddr = "10.0.0.1:40000"
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: first})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("change password status = %d, want %d (body=%s)", w.Code, http.StatusFound, w.Body.String())
	}

	// The other browser must be signed out — otherwise changing a password
	// revokes nothing, which is usually the whole point of changing it.
	if got := getWithAuth(r, "/api/whoami", second, "10.0.0.2"); got.Code != http.StatusUnauthorized {
		t.Errorf("the other session survived the password change: %d", got.Code)
	}
	// The browser that made the change stays signed in on its new cookie.
	if issued := authCookieFrom(w); issued == "" {
		t.Error("no replacement cookie was issued")
	} else if got := getWithAuth(r, "/api/whoami", issued, "10.0.0.1"); got.Code != http.StatusOK {
		t.Errorf("the changing browser was signed out: %d", got.Code)
	}

	// Old password no longer works, new one does.
	if bad := doLogin(t, r, "alice", loginTestPassword, "10.0.0.3"); bad.Code != http.StatusUnauthorized {
		t.Errorf("the old password still logs in: %d", bad.Code)
	}
	if ok := doLogin(t, r, "alice", next, "10.0.0.3"); ok.Code != http.StatusFound {
		t.Errorf("the new password does not log in: %d", ok.Code)
	}
}

func TestChangePasswordRequiresTheCurrentOne(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)
	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))

	post := func(current, next, confirm string) *httptest.ResponseRecorder {
		form := url.Values{
			"currentPassword": {current},
			"newPassword":     {next},
			"confirmPassword": {confirm},
		}
		req := httptest.NewRequest(http.MethodPost, changePasswordPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Referer", "http://example.test/account/password")
		req.Host = "example.test"
		req.RemoteAddr = "10.0.0.1:40000"
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Borrowing a signed-in browser must not be enough to take the account
	// over; the current password is the check that stops that.
	if w := post("not-the-current-one", "a-new-long-password", "a-new-long-password"); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong current password: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if w := post(loginTestPassword, "a-new-long-password", "different-confirmation"); w.Code != http.StatusBadRequest {
		t.Errorf("mismatched confirmation: status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if w := post(loginTestPassword, "short", "short"); w.Code != http.StatusBadRequest {
		t.Errorf("too-short password: status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if w := post(loginTestPassword, loginTestPassword, loginTestPassword); w.Code != http.StatusBadRequest {
		t.Errorf("unchanged password: status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	// After all that, the original password must still work.
	if _, err := app.userStore().Authenticate("alice", loginTestPassword); err != nil {
		t.Errorf("a rejected change altered the password: %v", err)
	}
}

// Turning authentication on with no accounts must not invent one. The
// instance waits in first-run setup instead, which is covered in setup_test.go;
// what matters here is that nothing is created behind the operator's back.
func TestNoAccountIsCreatedAutomatically(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "local")

	base := t.TempDir()
	app := newApp(base)
	if _, err := buildRouter(app); err != nil {
		t.Fatalf("buildRouter: %v", err)
	}

	n, err := app.userStore().Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("account count = %d, want 0", n)
	}
	if _, err := os.Stat(filepath.Join(base, "users.json")); err == nil {
		t.Error("a user store was written before any account was created")
	}
}

// Turning authentication on must not break the token surface. /api/v1 — and
// MCP over HTTP inside it — authenticates a bearer token, not a browser
// session; if the login gate judged it too, an operator enabling AUTH_MODE
// would silently take every API and MCP client offline at the same moment.
func TestTokenAPIIsNotGatedByLogin(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	_, secret, err := app.tokenStore().Issue(alice.ID, "ci", nil, nil)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	get := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := get("/api/v1/sessions", secret); w.Code != http.StatusOK {
		t.Errorf("valid token = %d, want 200 (body=%s)", w.Code, w.Body.String())
	}

	// The API's own gate must still be the one refusing bad tokens, and it
	// must not fall back to the login page.
	for _, tc := range []struct{ name, token string }{
		{"no token", ""},
		{"wrong token", "not-the-token"},
	} {
		w := get("/api/v1/sessions", tc.token)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want %d", tc.name, w.Code, http.StatusUnauthorized)
		}
		if strings.Contains(w.Body.String(), "authentication required") {
			t.Errorf("%s was answered by the login gate, not RequireAPIToken: %s", tc.name, w.Body.String())
		}
	}
}

// The same applies during first-run setup: a token client cannot complete
// setup, so it must get a machine-readable answer rather than a redirect.
func TestTokenAPIDuringSetupAnswersJSON(t *testing.T) {
	_, r := newSetupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer 3270w_abcd1234_notarealtoken")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusFound {
		t.Fatalf("a token client was redirected to a web page: %q", w.Header().Get("Location"))
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(w.Body.String(), "not set up yet") {
		t.Errorf("body = %q", w.Body.String())
	}
}
