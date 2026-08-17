// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
)

// The whole sign-in, driven through the real router against a real identity
// provider that happens to be in this process.
//
// The unit tests either side of this one check the provider client and the
// server's own decisions separately. This is the one that would notice if the
// two were wired together wrongly — a nonce that never reaches the token, a
// state the callback cannot find, an account created under the wrong key —
// none of which either half can see on its own.

// flowIDP is a minimal identity provider: discovery, one signing key, and a
// token endpoint that hands back whatever the test asked it to mint.
type flowIDP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	// claims is what the next ID token will say, minus the nonce, which comes
	// from the authorization request the server made.
	claims map[string]any
	// nonce is captured from the authorize call and put into the token, which
	// is exactly what a real provider does.
	nonce string
}

func newFlowIDP(t *testing.T) *flowIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &flowIDP{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		flowJSON(w, map[string]any{
			"issuer":                 idp.server.URL,
			"authorization_endpoint": idp.server.URL + "/authorize",
			"token_endpoint":         idp.server.URL + "/token",
			"jwks_uri":               idp.server.URL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		flowJSON(w, map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": flowB64(key.N.Bytes()),
			"e": flowB64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		flowJSON(w, map[string]any{"id_token": idp.mint(t), "token_type": "Bearer"})
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// mint signs an ID token for the sign-in currently in flight.
func (i *flowIDP) mint(t *testing.T) string {
	t.Helper()
	claims := map[string]any{
		"iss":   i.server.URL,
		"aud":   "3270web",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
		"iat":   float64(time.Now().Unix()),
		"nonce": i.nonce,
	}
	for k, v := range i.claims {
		claims[k] = v
	}
	header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	signing := flowB64(header) + "." + flowB64(payload)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + flowB64(sig)
}

func flowB64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func flowJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newFlowApp builds a real router pointed at idp, with a local administrator
// already in place so setup is closed.
func newFlowApp(t *testing.T, idp *flowIDP, env map[string]string) (*App, *gin.Engine) {
	t.Helper()
	t.Setenv("OIDC_ISSUER", idp.server.URL)
	t.Setenv("OIDC_CLIENT_ID", "3270web")
	t.Setenv("OIDC_CLIENT_SECRET", "secret")
	t.Setenv("OIDC_REDIRECT_URL", "http://3270web.test/auth/sso/callback")
	for k, v := range env {
		t.Setenv(k, v)
	}
	app, r := newAuthTestApp(t, "oidc")
	addUser(t, app, "root", authz.RoleAdmin, false)
	return app, r
}

// signInThroughTheProvider walks the whole flow and returns the login cookie.
func signInThroughTheProvider(t *testing.T, app *App, r *gin.Engine, idp *flowIDP) *httptest.ResponseRecorder {
	t.Helper()

	start := httptest.NewRecorder()
	r.ServeHTTP(start, httptest.NewRequest(http.MethodGet, ssoStartPath, nil))
	if start.Code != http.StatusFound {
		t.Fatalf("/auth/sso returned %d, want a redirect to the provider (body %s)", start.Code, start.Body.String())
	}
	authorize, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authorize.Query().Get("state")
	idp.nonce = authorize.Query().Get("nonce")
	if state == "" || idp.nonce == "" {
		t.Fatalf("the authorization request carried state=%q nonce=%q", state, idp.nonce)
	}

	back := httptest.NewRecorder()
	r.ServeHTTP(back, httptest.NewRequest(http.MethodGet,
		ssoCallbackPath+"?state="+url.QueryEscape(state)+"&code=any-code", nil))
	return back
}

func TestSignInThroughAProviderCreatesAndKeepsOneAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idp := newFlowIDP(t)
	idp.claims = map[string]any{"sub": "subject-abc", "preferred_username": "alice@corp.example"}
	app, r := newFlowApp(t, idp, nil)

	back := signInThroughTheProvider(t, app, r, idp)
	if back.Code != http.StatusFound {
		t.Fatalf("the callback returned %d (body %s)", back.Code, back.Body.String())
	}
	cookie := authCookieFrom(back)
	if cookie == "" {
		t.Fatal("the callback issued no login cookie")
	}
	if got := back.Header().Get("Location"); got != "/" {
		t.Errorf("landed at %q, want /", got)
	}

	// The cookie is a working login, not just a cookie.
	if w := getWithAuth(r, "/api/whoami", cookie, ""); w.Code != http.StatusOK {
		t.Fatalf("/api/whoami with the new login returned %d", w.Code)
	}

	list, err := app.userStore().List()
	if err != nil {
		t.Fatal(err)
	}
	var found int
	var id string
	for _, u := range list {
		if u.Subject == "subject-abc" {
			found++
			id = u.ID
			if u.Issuer != idp.server.URL {
				t.Errorf("Issuer = %q, want the provider", u.Issuer)
			}
			// The claim had an @ in it, which the account store does not accept.
			if u.Username != "alice-corp.example" {
				t.Errorf("Username = %q, want the claim sanitised", u.Username)
			}
			if u.Role != authz.RoleUser {
				t.Errorf("Role = %q, want the default", u.Role)
			}
		}
	}
	if found != 1 {
		t.Fatalf("%d accounts carry that subject, want exactly 1", found)
	}

	// Signing in again is the same account, not a second one — and still is
	// after the provider renames them.
	idp.claims["preferred_username"] = "alice.smith@corp.example"
	if back := signInThroughTheProvider(t, app, r, idp); authCookieFrom(back) == "" {
		t.Fatal("the second sign-in failed")
	}
	list, _ = app.userStore().List()
	found = 0
	for _, u := range list {
		if u.Subject == "subject-abc" {
			found++
			if u.ID != id {
				t.Errorf("the account ID changed from %s to %s; its data directory would move with it", id, u.ID)
			}
			if u.Username != "alice.smith-corp.example" {
				t.Errorf("Username = %q, want the rename followed through", u.Username)
			}
		}
	}
	if found != 1 {
		t.Fatalf("a rename produced %d accounts, want 1", found)
	}
}

// The role the provider's groups map to has to be the role the session
// actually carries, not just what is written to the store.
func TestGroupsFromTheProviderDecideTheRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idp := newFlowIDP(t)
	idp.claims = map[string]any{
		"sub": "subject-admin", "preferred_username": "carol",
		"groups": []any{"mainframe-admins", "staff"},
	}
	app, r := newFlowApp(t, idp, map[string]string{"OIDC_ADMIN_GROUPS": "mainframe-admins"})

	cookie := authCookieFrom(signInThroughTheProvider(t, app, r, idp))
	if cookie == "" {
		t.Fatal("sign-in failed")
	}
	if w := getWithAuth(r, "/api/admin/users", cookie, ""); w.Code != http.StatusOK {
		t.Fatalf("a mapped administrator was refused administration: %d (%s)", w.Code, w.Body.String())
	}

	// Out of the group at the next sign-in: the role has to follow, or leaving
	// the group would revoke nothing.
	idp.claims["groups"] = []any{"staff"}
	cookie = authCookieFrom(signInThroughTheProvider(t, app, r, idp))
	if cookie == "" {
		t.Fatal("the second sign-in failed")
	}
	if w := getWithAuth(r, "/api/admin/users", cookie, ""); w.Code != http.StatusForbidden {
		t.Errorf("leaving the admin group left them administering: %d", w.Code)
	}
}

// An account disabled here must stay out, however happily the provider goes on
// authenticating them. The instance's own veto is the one that has to win.
func TestADisabledAccountCannotSignInThroughTheProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idp := newFlowIDP(t)
	idp.claims = map[string]any{"sub": "subject-blocked", "preferred_username": "dave"}
	app, r := newFlowApp(t, idp, nil)

	if authCookieFrom(signInThroughTheProvider(t, app, r, idp)) == "" {
		t.Fatal("the first sign-in failed")
	}
	if err := app.userStore().SetDisabled("dave", true); err != nil {
		t.Fatal(err)
	}

	back := signInThroughTheProvider(t, app, r, idp)
	if cookie := authCookieFrom(back); cookie != "" {
		t.Error("a disabled account signed in through the provider")
	}
}

// A group restriction keeps somebody the provider authenticates from reaching
// this instance at all — the case where one directory serves many services.
func TestAnAllowedGroupIsRequiredWhenOneIsConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idp := newFlowIDP(t)
	idp.claims = map[string]any{"sub": "subject-outside", "preferred_username": "erin",
		"groups": []any{"accounts-payable"}}
	app, r := newFlowApp(t, idp, map[string]string{"OIDC_ALLOWED_GROUPS": "mainframe-users"})

	if cookie := authCookieFrom(signInThroughTheProvider(t, app, r, idp)); cookie != "" {
		t.Fatal("somebody outside every allowed group was signed in")
	}
	// And no account was created for them, or the account list would fill with
	// everybody in the directory who ever pressed the button.
	list, _ := app.userStore().List()
	for _, u := range list {
		if u.Subject == "subject-outside" {
			t.Error("an account was created for somebody who may not sign in")
		}
	}
	_ = app
}
