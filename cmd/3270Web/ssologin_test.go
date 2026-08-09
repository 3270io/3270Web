package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/oidc"
)

// The provider itself is exercised in internal/oidc against a stub that can be
// made to misbehave. These tests are about what the server does with the
// answer: which account it resolves, what it refuses, and what a browser
// carries between the two halves of the flow.

func TestSanitiseUsernameKeepsClaimsDistinguishable(t *testing.T) {
	for in, want := range map[string]string{
		"alice":                   "alice",
		"alice@example.com":       "alice-example.com",
		"Alice Smith":             "Alice-Smith",
		"  padded  ":              "padded",
		"!!!":                     "",
		"":                        "",
		"a..b":                    "a..b",
		"CN=alice,OU=ops,DC=corp": "CN-alice-OU-ops-DC-corp",
		strings.Repeat("x", 100):  strings.Repeat("x", 64),
		"user+tag@example.com":    "user-tag-example.com",
	} {
		if got := sanitiseUsername(in); got != want {
			t.Errorf("sanitiseUsername(%q) = %q, want %q", in, got, want)
		}
	}

	// Two different claims must not collapse into one name, or two people
	// would share an audit line.
	if sanitiseUsername("a.b@c") == sanitiseUsername("a.bc") {
		t.Error("two distinct claims produced the same username")
	}
}

// A "next" parameter decides where somebody lands immediately after signing
// in, which makes it the most convincing place in the product to have an open
// redirect.
func TestReturnPathStaysOnThisServer(t *testing.T) {
	for in, want := range map[string]string{
		"/screen":                  "/screen",
		"/admin/users?tab=1":       "/admin/users?tab=1",
		"":                         "/",
		"https://evil.example":     "/",
		"//evil.example":           "/",
		"/\\evil.example":          "/",
		"javascript:alert(1)":      "/",
		"http://evil.example/path": "/",
	} {
		if got := safeReturnPath(in); got != want {
			t.Errorf("safeReturnPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// State is what ties a callback to a sign-in this server started. It has to be
// good for exactly one use, or a callback URL out of a browser's history is a
// second login.
func TestAPendingSignInIsRedeemableOnce(t *testing.T) {
	store := newPendingLoginStore()
	if !store.begin("state-1", newPending(time.Now())) {
		t.Fatal("begin refused the first sign-in")
	}

	if _, ok := store.take("state-1"); !ok {
		t.Fatal("the sign-in could not be redeemed")
	}
	if _, ok := store.take("state-1"); ok {
		t.Error("the same state was redeemed twice")
	}
	if _, ok := store.take(""); ok {
		t.Error("an empty state matched something")
	}
	if _, ok := store.take("never-issued"); ok {
		t.Error("a state this server never issued matched something")
	}
}

// /auth/sso is reachable without credentials, so it is somewhere an anonymous
// caller can make the server allocate. The ceiling is what keeps that from
// being unbounded.
func TestPendingSignInsAreBounded(t *testing.T) {
	store := newPendingLoginStore()
	now := time.Now()
	for i := 0; i < maxPendingLogins; i++ {
		if !store.begin(fmt.Sprintf("state-%d", i), newPending(now)) {
			t.Fatalf("begin refused sign-in %d, below the ceiling", i)
		}
	}
	if store.begin("one-too-many", newPending(now)) {
		t.Error("the ceiling was not enforced")
	}
}

// A sign-in nobody completes must not sit in memory, and must not still be
// redeemable when somebody eventually finds the URL.
func TestAPendingSignInExpires(t *testing.T) {
	store := newPendingLoginStore()
	store.begin("stale", newPending(time.Now().Add(-pendingLoginTTL-time.Minute)))
	store.begin("fresh", newPending(time.Now()))

	if _, ok := store.take("stale"); ok {
		t.Error("a sign-in older than the window was still redeemable")
	}
	if _, ok := store.take("fresh"); !ok {
		t.Error("a sign-in inside the window was dropped")
	}
}

func newPending(at time.Time) pendingLogin {
	return pendingLogin{Nonce: "n", Verifier: "v", ReturnTo: "/", CreatedAt: at}
}

// AUTH_MODE=oidc with nothing configured must stop the server rather than
// present a sign-in button that cannot work.
func TestOIDCModeNeedsItsSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "oidc")

	app := newApp(t.TempDir())
	_, err := buildRouter(app)
	if err == nil {
		t.Fatal("AUTH_MODE=oidc started with no provider configured")
	}
	if !strings.Contains(err.Error(), "OIDC_ISSUER") {
		t.Errorf("error %q does not name the setting that is missing", err)
	}
}

// newSSOTestApp builds an instance configured for a provider that is not
// there. Everything below is about the server's own behaviour, which does not
// need the provider to answer.
func newSSOTestApp(t *testing.T) (*App, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("OIDC_ISSUER", "https://idp.example")
	t.Setenv("OIDC_CLIENT_ID", "3270web")
	t.Setenv("OIDC_CLIENT_SECRET", "secret")
	t.Setenv("OIDC_REDIRECT_URL", "https://3270web.example/auth/sso/callback")
	return newAuthTestApp(t, "oidc")
}

func TestSSOModeStillSeparatesUsersAndRequiresALogin(t *testing.T) {
	app, r := newSSOTestApp(t)
	if !app.separatesUsers() {
		t.Error("AUTH_MODE=oidc does not separate users")
	}
	if !app.ssoEnabled() {
		t.Error("the provider was configured but SSO is not enabled")
	}

	// Nothing behind the login gate is reachable without one, exactly as under
	// local accounts.
	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("an anonymous request got %d, want 401", w.Code)
	}
}

// The callback is the half of the flow an attacker can reach directly. None of
// these may sign anybody in.
func TestCallbacksThatMustNotSignAnybodyIn(t *testing.T) {
	_, r := newSSOTestApp(t)

	for _, query := range []string{
		"",                                // nothing at all
		"?code=stolen",                    // a code with no state
		"?state=never-issued&code=stolen", // a state this server never made
		"?error=access_denied&state=x",    // the provider's own refusal
		"?state=&code=stolen",             // an empty state
	} {
		req := httptest.NewRequest(http.MethodGet, ssoCallbackPath+query, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if cookie := authCookieFrom(w); cookie != "" {
			t.Errorf("callback%q issued a login cookie", query)
		}
		if w.Code == http.StatusFound && strings.HasPrefix(w.Header().Get("Location"), "/") &&
			w.Header().Get("Location") != loginPath {
			t.Errorf("callback%q redirected to %q as though it had worked", query, w.Header().Get("Location"))
		}
	}
}

// A local account and an identity-provider account are told apart by the store,
// not by the sign-in page: a password must never work for an account that has
// none, whichever form it is typed into.
func TestAPasswordDoesNotWorkForAnExternalAccount(t *testing.T) {
	app, r := newSSOTestApp(t)
	addUser(t, app, "root", authz.RoleAdmin, false)
	if _, err := app.userStore().UpsertExternal("https://idp.example", "sub-1", "alice", ""); err != nil {
		t.Fatal(err)
	}

	for _, password := range []string{loginTestPassword, "", "anything-at-all"} {
		form := url.Values{"username": {"alice"}, "password": {password}}
		req := httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Referer", "http://example.test/login")
		req.Host = "example.test"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if cookie := authCookieFrom(w); cookie != "" {
			t.Fatalf("password %q signed in an external account", password)
		}
	}
}

// The local sign-in form has to keep working under AUTH_MODE=oidc. It is the
// way back into an instance whose provider is unreachable, and an instance
// that can be locked out of itself by somebody else's outage is not one to
// deploy.
func TestALocalAccountStillSignsInUnderSSO(t *testing.T) {
	app, r := newSSOTestApp(t)
	addUser(t, app, "root", authz.RoleAdmin, false)

	if cookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1")); cookie == "" {
		t.Fatal("the break-glass local administrator could not sign in")
	}
	_ = app
}

func TestRoleMappingFollowsTheConfiguredGroups(t *testing.T) {
	app, _ := newSSOTestApp(t)

	// No mapping configured: the provider says nothing about roles, so an
	// administrator's decision here is left alone.
	app.sso.AdminGroups = nil
	if got := app.ssoRole(tokenWithGroups("mainframe-admins")); got != "" {
		t.Errorf("with no mapping, role = %q, want it left alone", got)
	}

	app.sso.AdminGroups = []string{"mainframe-admins"}
	if got := app.ssoRole(tokenWithGroups("MAINFRAME-ADMINS", "staff")); got != authz.RoleAdmin {
		t.Errorf("role = %q, want admin (matching is case-insensitive)", got)
	}
	// Out of the group is demoted rather than left, or leaving the group would
	// not revoke anything.
	if got := app.ssoRole(tokenWithGroups("staff")); got != authz.RoleUser {
		t.Errorf("role = %q, want user", got)
	}

	// Restricting who may sign in at all is a separate list.
	app.sso.AllowedGroups = []string{"mainframe-users"}
	if app.ssoGroupsAllowSignIn(tokenWithGroups("staff")) {
		t.Error("somebody outside every allowed group was let in")
	}
	if !app.ssoGroupsAllowSignIn(tokenWithGroups("mainframe-users")) {
		t.Error("a member of an allowed group was refused")
	}
}

// tokenWithGroups builds a verified-token value carrying just the claim the
// role mapping reads.
func tokenWithGroups(groups ...string) *oidc.IDToken {
	list := make([]any, len(groups))
	for i, g := range groups {
		list[i] = g
	}
	return &oidc.IDToken{Claims: map[string]any{"groups": list}}
}

// The username is a display name derived from whichever claim the provider
// fills in; the identity is the subject. A provider that sends nothing usable
// must still produce an account rather than a refusal.
func TestUsernameFallsBackThroughTheClaims(t *testing.T) {
	app, _ := newSSOTestApp(t)

	token := &oidc.IDToken{Subject: "sub-1", Claims: map[string]any{
		"preferred_username": "alice",
		"email":              "alice@example.com",
	}}
	if got := app.ssoUsername(token); got != "alice" {
		t.Errorf("username = %q, want the preferred_username claim", got)
	}

	delete(token.Claims, "preferred_username")
	if got := app.ssoUsername(token); got != "alice-example.com" {
		t.Errorf("username = %q, want the email claim sanitised", got)
	}

	// Nothing but a subject: still a usable account.
	bare := &oidc.IDToken{Subject: "sub-1", Claims: map[string]any{}}
	if got := app.ssoUsername(bare); got != "sub-1" {
		t.Errorf("username = %q, want the subject as a last resort", got)
	}

	// A configured claim wins, and is not silently replaced when absent by one
	// the operator did not choose.
	app.sso.UsernameClaim = "upn"
	token.Claims["upn"] = "ALICE@corp.example"
	if got := app.ssoUsername(token); got != "ALICE-corp.example" {
		t.Errorf("username = %q, want the configured claim", got)
	}
}
