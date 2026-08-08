package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/apitoken"
	"github.com/jnnngs/3270Web/internal/authz"
)

// issueToken creates an account's token and returns the secret to present.
func issueToken(t *testing.T, app *App, userID, name string, scopes []string) string {
	t.Helper()
	_, secret, err := app.tokenStore().Issue(userID, name, scopes, nil)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return secret
}

func apiGet(r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	return apiCall(r, http.MethodGet, path, token, "")
}

func apiCall(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// A shared credential every automated client holds would reach every
// account's sessions, which is exactly what AUTH_MODE=local was turned on to
// prevent. Starting anyway would leave the operator believing otherwise.
func TestSharedAPITokenRefusesToStartWithAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "local")
	t.Setenv("API_TOKEN", "one-token-for-everybody")

	_, err := buildRouter(newApp(t.TempDir()))
	if err == nil {
		t.Fatal("the instance started with a shared token and separated users")
	}
	for _, want := range []string{"API_TOKEN", "token add"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	// The same variable is the whole API surface where there is one operator,
	// so it must keep working there.
	t.Setenv(authz.ModeEnv, "none")
	if _, err := buildRouter(newApp(t.TempDir())); err != nil {
		t.Errorf("a single-operator instance refused a shared token: %v", err)
	}
}

// The point of the phase: a token reaches its owner's sessions and nobody
// else's, over the API surface a real client uses.
func TestTokenSeesOnlyItsOwnersSessions(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	bob := addUser(t, app, "bob", authz.RoleUser, false)

	mine := app.SessionManager.CreateSessionFor(alice.ID, mustMockHost(t))
	theirs := app.SessionManager.CreateSessionFor(bob.ID, mustMockHost(t))
	token := issueToken(t, app, alice.ID, "ci", nil)

	w := apiGet(r, "/api/v1/sessions", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d (%s)", w.Code, w.Body.String())
	}
	var listed struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sessions) != 1 || listed.Sessions[0].ID != mine.ID {
		t.Errorf("listed %+v, want only alice's session", listed.Sessions)
	}

	// Naming the other session directly is the other way in, and gets the
	// same answer as an ID that does not exist.
	if w := apiGet(r, "/api/v1/sessions/"+theirs.ID+"/screen.json", token); w.Code != http.StatusNotFound {
		t.Errorf("bob's session by id = %d, want 404 (%s)", w.Code, w.Body.String())
	}
}

// An admin's token administers the instance; it does not become everybody's
// hands on everybody's terminal.
func TestAdminTokenIsStillConfinedToItsOwnSessions(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	root := addUser(t, app, "root", authz.RoleAdmin, false)
	bob := addUser(t, app, "bob", authz.RoleUser, false)
	theirs := app.SessionManager.CreateSessionFor(bob.ID, mustMockHost(t))

	token := issueToken(t, app, root.ID, "admin's ci", nil)
	if w := apiGet(r, "/api/v1/sessions/"+theirs.ID+"/screen.json", token); w.Code != http.StatusNotFound {
		t.Errorf("an admin's token reached another account's session: %d", w.Code)
	}
}

// A read-only credential is one that cannot change anything, and the method
// is what says whether a request changes something.
func TestReadOnlyTokenCannotWrite(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	readOnly := issueToken(t, app, alice.ID, "scraper", []string{apitoken.ScopeRead})
	full := issueToken(t, app, alice.ID, "driver", nil)

	if w := apiGet(r, "/api/v1/sessions", readOnly); w.Code != http.StatusOK {
		t.Errorf("read-only GET = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	w := apiCall(r, http.MethodPost, "/api/v1/sessions", readOnly, `{"host":"mock"}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("read-only POST = %d, want 403 (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "write") {
		t.Errorf("the refusal does not say what is missing: %s", w.Body.String())
	}

	// A DELETE is a write too, whatever it is deleting.
	if w := apiCall(r, http.MethodDelete, "/api/v1/sessions/whatever", readOnly, ""); w.Code != http.StatusForbidden {
		t.Errorf("read-only DELETE = %d, want 403", w.Code)
	}

	// The same request with a full token gets past the scope check — so the
	// refusal above is the scope and not something else.
	if w := apiCall(r, http.MethodDelete, "/api/v1/sessions/whatever", full, ""); w.Code == http.StatusForbidden {
		t.Errorf("a full token was refused by the scope check: %s", w.Body.String())
	}
}

// A token that outlived its account, or survived it being disabled, would
// make both of those things mean less than they say.
func TestTokensFollowTheirAccount(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	token := issueToken(t, app, alice.ID, "ci", nil)

	if w := apiGet(r, "/api/v1/sessions", token); w.Code != http.StatusOK {
		t.Fatalf("baseline = %d (%s)", w.Code, w.Body.String())
	}

	if err := app.userStore().SetDisabled("alice", true); err != nil {
		t.Fatal(err)
	}
	if w := apiGet(r, "/api/v1/sessions", token); w.Code != http.StatusUnauthorized {
		t.Errorf("a disabled account's token = %d, want 401", w.Code)
	}

	// Re-enabling gives the automated clients back rather than making
	// everything be reissued.
	if err := app.userStore().SetDisabled("alice", false); err != nil {
		t.Fatal(err)
	}
	if w := apiGet(r, "/api/v1/sessions", token); w.Code != http.StatusOK {
		t.Errorf("a re-enabled account's token = %d, want 200", w.Code)
	}

	if err := app.userStore().Delete("alice"); err != nil {
		t.Fatal(err)
	}
	if w := apiGet(r, "/api/v1/sessions", token); w.Code != http.StatusUnauthorized {
		t.Errorf("a deleted account's token = %d, want 401", w.Code)
	}
}

func TestRevokedAndExpiredTokensAreRefused(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)

	record, revoked, err := app.tokenStore().Issue(alice.ID, "old ci", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.tokenStore().Revoke(record.ID); err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-time.Hour)
	_, expired, err := app.tokenStore().Issue(alice.ID, "short-lived", nil, &past)
	if err != nil {
		t.Fatal(err)
	}

	for name, secret := range map[string]string{"revoked": revoked, "expired": expired} {
		w := apiGet(r, "/api/v1/sessions", secret)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s token = %d, want 401", name, w.Code)
		}
		// The refusal must not confirm that the token is real; that is a
		// working credential and a date away from being useful again.
		for _, leak := range []string{"revoked", "expired"} {
			if strings.Contains(strings.ToLower(w.Body.String()), leak) {
				t.Errorf("the %s refusal says why: %s", name, w.Body.String())
			}
		}
	}
}

// Somebody else's token is not a credential, however well-formed it looks.
func TestBadTokensAreRefused(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	real := issueToken(t, app, alice.ID, "ci", nil)

	id, _, err := apitoken.Parse(real)
	if err != nil {
		t.Fatal(err)
	}

	for name, presented := range map[string]string{
		"nothing":       "",
		"not a token":   "hunter2",
		"right id only": apitoken.Format(id, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		"invented":      apitoken.Format("0123456789abcdef", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	} {
		if w := apiGet(r, "/api/v1/sessions", presented); w.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", name, w.Code)
		}
	}
}

// With accounts on, the shared variable must not be a second way in. It
// cannot normally be set at all — startup refuses — so this covers the case
// where something sets it afterwards.
func TestSharedTokenIsIgnoredWhereUsersAreSeparated(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)
	t.Setenv("API_TOKEN", "one-token-for-everybody")

	if w := apiGet(r, "/api/v1/sessions", "one-token-for-everybody"); w.Code != http.StatusUnauthorized {
		t.Errorf("the shared token was accepted: %d (%s)", w.Code, w.Body.String())
	}
}

// The single-operator shape is unchanged: one environment variable, and the
// API drives the session the operator has open in the browser.
func TestSingleOperatorTokenStillDrivesTheBrowsersSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "none")
	t.Setenv("API_TOKEN", "a-test-api-token")

	app := newApp(t.TempDir())
	r, err := buildRouter(app)
	if err != nil {
		t.Fatalf("buildRouter: %v", err)
	}

	// What the browser's connect handler creates: owned by the local operator.
	browser := app.SessionManager.CreateSessionFor(authz.LocalUserID, mustMockHost(t))
	if w := apiGet(r, "/api/v1/sessions/"+browser.ID+"/screen.json", "a-test-api-token"); w.Code == http.StatusNotFound {
		t.Errorf("the API cannot reach the operator's own session: %s", w.Body.String())
	}

	// And a session opened before ownership existed is still reachable.
	legacy := app.SessionManager.CreateSession(mustMockHost(t))
	if w := apiGet(r, "/api/v1/sessions/"+legacy.ID+"/screen.json", "a-test-api-token"); w.Code == http.StatusNotFound {
		t.Errorf("the API cannot reach an unowned session: %s", w.Body.String())
	}
}

// Without any token configured the surface is off rather than open.
func TestAPIIsOffWithoutAnyToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "none")
	t.Setenv("API_TOKEN", "")

	r, err := buildRouter(newApp(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	w := apiGet(r, "/api/v1/sessions", "anything")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// MCP over HTTP replays tool calls into the router in this process. It used
// to attach the instance's own API_TOKEN, which under separated users would
// mean every MCP client acted as the instance rather than as itself — so the
// credential the client presented has to be the one that travels.
func TestMCPHTTPToolCallsActAsTheCallingAccount(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	bob := addUser(t, app, "bob", authz.RoleUser, false)

	mine := app.SessionManager.CreateSessionFor(alice.ID, mustMockHost(t))
	app.SessionManager.CreateSessionFor(bob.ID, mustMockHost(t))
	token := issueToken(t, app, alice.ID, "claude", nil)

	invoker := newEngineInvoker(r, "Bearer "+token)
	res, err := invoker.Do(t.Context(), http.MethodGet, "/api/v1/sessions", nil, nil)
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if res.Status != http.StatusOK {
		t.Fatalf("status = %d (%s)", res.Status, res.Body)
	}
	body := string(res.Body)
	if !strings.Contains(body, mine.ID) {
		t.Errorf("alice's own session is missing from %s", body)
	}
	if strings.Count(body, `"id"`) != 1 {
		t.Errorf("the tool call saw more than alice's session: %s", body)
	}

	// And a connection that presented nothing gets nothing, rather than
	// falling back to a credential of the instance's own.
	res, err = newEngineInvoker(r, "").Do(t.Context(), http.MethodGet, "/api/v1/sessions", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusUnauthorized {
		t.Errorf("an unauthenticated tool call = %d, want 401 (%s)", res.Status, res.Body)
	}
}

func TestTokenScopeAllows(t *testing.T) {
	readOnly := authz.Token("alice", authz.RoleUser, []string{apitoken.ScopeRead})
	full := authz.Token("alice", authz.RoleUser, []string{apitoken.ScopeRead, apitoken.ScopeWrite})
	// A browser and the single operator hold no scopes and are unrestricted
	// within their role, so this must be a no-op for them.
	browser := authz.Principal{UserID: "alice", Role: authz.RoleUser, Kind: authz.KindWeb}

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		for name, p := range map[string]authz.Principal{"read-only": readOnly, "full": full, "browser": browser} {
			if !tokenScopeAllows(method, p) {
				t.Errorf("%s %s was refused", name, method)
			}
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if tokenScopeAllows(method, readOnly) {
			t.Errorf("read-only %s was allowed", method)
		}
		if !tokenScopeAllows(method, full) {
			t.Errorf("full %s was refused", method)
		}
		if !tokenScopeAllows(method, browser) {
			t.Errorf("browser %s was refused", method)
		}
	}
}
