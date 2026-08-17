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

// newSetupTestApp builds a real router with authentication on and no
// accounts, which is the state first-run setup exists for.
func newSetupTestApp(t *testing.T) (*App, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "local")
	t.Setenv("AUTH_BIND_SESSION_IP", "false")

	app := newApp(t.TempDir())
	r, err := buildRouter(app)
	if err != nil {
		t.Fatalf("buildRouter: %v", err)
	}
	return app, r
}

func postSetup(r *gin.Engine, form url.Values, clientIP string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, setupPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/setup")
	req.Host = "example.test"
	if clientIP != "" {
		req.RemoteAddr = clientIP + ":40000"
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSetupIsArmedWhenNoAccountsExist(t *testing.T) {
	app, r := newSetupTestApp(t)

	if !app.setup.pending() {
		t.Fatal("setup was not armed on an empty instance")
	}
	if n, _ := app.userStore().Count(); n != 0 {
		t.Errorf("account count = %d, want 0 — setup must not create one by itself", n)
	}

	// Everything funnels to setup, so an operator meets the page that can fix
	// the instance rather than a sign-in form nobody can pass.
	w := getWithAuth(r, "/", "", "10.0.0.1")
	if w.Code != http.StatusFound || w.Header().Get("Location") != setupPath {
		t.Errorf("/ = %d %q, want a redirect to %s", w.Code, w.Header().Get("Location"), setupPath)
	}
	w = getWithAuth(r, loginPath, "", "10.0.0.1")
	if w.Code != http.StatusFound || w.Header().Get("Location") != setupPath {
		t.Errorf("login = %d %q, want a redirect to %s", w.Code, w.Header().Get("Location"), setupPath)
	}
	if w := getWithAuth(r, setupPath, "", "10.0.0.1"); w.Code != http.StatusOK {
		t.Errorf("setup page = %d, want 200", w.Code)
	}
	// A scripted caller gets a machine-readable answer, not an HTML redirect.
	if w := getWithAuth(r, "/api/whoami", "", "10.0.0.1"); w.Code != http.StatusServiceUnavailable {
		t.Errorf("api during setup = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	// Health must answer during setup or a container never reports ready.
	if w := getWithAuth(r, "/healthz", "", "10.0.0.1"); w.Code == http.StatusFound {
		t.Error("/healthz was redirected to setup")
	}
}

func TestSetupRejectsWrongCode(t *testing.T) {
	app, r := newSetupTestApp(t)

	w := postSetup(r, url.Values{
		"setupCode":       {"AAAA-BBBB-CCCC-DDDD"},
		"username":        {"alice"},
		"password":        {loginTestPassword},
		"confirmPassword": {loginTestPassword},
	}, "10.0.0.1")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if n, _ := app.userStore().Count(); n != 0 {
		t.Error("an account was created despite the wrong code")
	}
	if !app.setup.pending() {
		t.Error("setup was closed by a failed attempt")
	}
}

func TestSetupCreatesAdminAndSignsIn(t *testing.T) {
	app, r := newSetupTestApp(t)
	code := app.setup.code

	w := postSetup(r, url.Values{
		// Submitted in the shape a person copies out of a log, to prove the
		// grouping and case are tolerated.
		"setupCode":       {strings.ToLower(formatSetupCode(code))},
		"username":        {"alice"},
		"password":        {loginTestPassword},
		"confirmPassword": {loginTestPassword},
	}, "10.0.0.1")

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (body=%s)", w.Code, http.StatusFound, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}

	cookie := authCookieFrom(w)
	if cookie == "" {
		t.Fatal("setup did not sign the new administrator in")
	}
	got := getWithAuth(r, "/api/whoami", cookie, "10.0.0.1")
	if got.Code != http.StatusOK {
		t.Fatalf("whoami = %d, want 200", got.Code)
	}
	if !strings.Contains(got.Body.String(), `"isAdmin":true`) {
		t.Errorf("the created account is not an admin: %s", got.Body.String())
	}

	list, err := app.userStore().List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Username != "alice" || list[0].Role != authz.RoleAdmin {
		t.Fatalf("accounts = %+v, want one admin named alice", list)
	}
	// A self-chosen password does not need replacing.
	if list[0].MustChangePassword {
		t.Error("the operator was asked to change the password they just chose")
	}
}

// Setup is a one-time door. Leaving it open would be a second, unauthenticated
// way to create an administrator.
func TestSetupClosesAfterFirstAdmin(t *testing.T) {
	app, r := newSetupTestApp(t)
	code := app.setup.code

	if w := postSetup(r, url.Values{
		"setupCode":       {code},
		"username":        {"alice"},
		"password":        {loginTestPassword},
		"confirmPassword": {loginTestPassword},
	}, "10.0.0.1"); w.Code != http.StatusFound {
		t.Fatalf("first setup failed: %d %s", w.Code, w.Body.String())
	}

	if app.setup.pending() {
		t.Fatal("setup is still pending after an administrator was created")
	}
	if w := getWithAuth(r, setupPath, "", "10.0.0.2"); w.Code != http.StatusFound ||
		w.Header().Get("Location") != loginPath {
		t.Errorf("setup page after completion = %d %q, want a redirect to login",
			w.Code, w.Header().Get("Location"))
	}

	// Even with the original code, a second attempt must not add an account.
	w := postSetup(r, url.Values{
		"setupCode":       {code},
		"username":        {"mallory"},
		"password":        {loginTestPassword},
		"confirmPassword": {loginTestPassword},
	}, "10.0.0.2")
	if w.Code != http.StatusFound || w.Header().Get("Location") != loginPath {
		t.Errorf("second setup = %d %q, want a redirect to login", w.Code, w.Header().Get("Location"))
	}
	if n, _ := app.userStore().Count(); n != 1 {
		t.Errorf("account count = %d, want 1", n)
	}
}

func TestSetupValidatesInput(t *testing.T) {
	app, r := newSetupTestApp(t)
	code := app.setup.code

	cases := []struct {
		name              string
		username          string
		password          string
		confirm           string
		wantStatusAtLeast int
	}{
		{"mismatched confirmation", "alice", loginTestPassword, "something-else-entirely", http.StatusBadRequest},
		{"short password", "alice", "short", "short", http.StatusBadRequest},
		{"unsafe username", "../root", loginTestPassword, loginTestPassword, http.StatusBadRequest},
		{"empty username", "", loginTestPassword, loginTestPassword, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postSetup(r, url.Values{
				"setupCode":       {code},
				"username":        {tc.username},
				"password":        {tc.password},
				"confirmPassword": {tc.confirm},
			}, "10.0.0.1")
			if w.Code < tc.wantStatusAtLeast {
				t.Errorf("status = %d, want at least %d", w.Code, tc.wantStatusAtLeast)
			}
			if n, _ := app.userStore().Count(); n != 0 {
				t.Fatalf("an account was created for invalid input %q", tc.name)
			}
		})
	}
}

// Pre-seeding an account (with the CLI, say) means the instance is already
// configured and setup must not arm at all.
func TestSetupNotArmedWhenAccountsAlreadyExist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "local")

	base := t.TempDir()
	app := newApp(base)
	if _, err := app.userStore().Add("preseeded", loginTestPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	r, err := buildRouter(app)
	if err != nil {
		t.Fatal(err)
	}

	if app.setup.pending() {
		t.Error("setup armed even though an account exists")
	}
	if w := getWithAuth(r, "/", "", "10.0.0.1"); w.Header().Get("Location") == setupPath {
		t.Error("requests were redirected to setup on a configured instance")
	}
}

func TestSetupNotArmedWithoutAuth(t *testing.T) {
	app, _ := newAuthTestApp(t, "none")
	if app.setup.pending() {
		t.Error("setup armed under AUTH_MODE=none, where there are no accounts to create")
	}
}

func TestNormaliseSetupCode(t *testing.T) {
	cases := map[string]string{
		"ABCD-2345":      "ABCD2345",
		"abcd 2345":      "ABCD2345",
		"  AbCd-23 45  ": "ABCD2345",
		// Base32 excludes 0, 1, 8 and 9; stripping them keeps a misread from
		// silently becoming a different valid-looking code.
		"ABCD01892345": "ABCD2345",
		"":             "",
	}
	for in, want := range cases {
		if got := normaliseSetupCode(in); got != want {
			t.Errorf("normaliseSetupCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetupCodeIsRandomAndReadable(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		code, err := newSetupCode()
		if err != nil {
			t.Fatal(err)
		}
		if seen[code] {
			t.Fatal("newSetupCode repeated a value")
		}
		seen[code] = true
		if len(code) != 16 {
			t.Errorf("code %q is %d characters, want 16", code, len(code))
		}
		if normaliseSetupCode(code) != code {
			t.Errorf("code %q does not survive its own normalisation", code)
		}
	}
}
