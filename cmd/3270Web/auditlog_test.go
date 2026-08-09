package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
)

// trailOf reads the recorded events, newest first.
func trailOf(t *testing.T, app *App) []audit.Entry {
	t.Helper()
	entries, err := app.auditRecorder().Read(500)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	return entries
}

// findEvent returns the most recent entry of an event, and whether there was
// one.
func findEvent(entries []audit.Entry, event audit.Event) (audit.Entry, bool) {
	for _, e := range entries {
		if e.Event == event {
			return e, true
		}
	}
	return audit.Entry{}, false
}

func mustFind(t *testing.T, app *App, event audit.Event) audit.Entry {
	t.Helper()
	entry, ok := findEvent(trailOf(t, app), event)
	if !ok {
		t.Fatalf("no %s in the trail; recorded: %s", event, eventNames(trailOf(t, app)))
	}
	return entry
}

func eventNames(entries []audit.Entry) string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, string(e.Event))
	}
	return strings.Join(names, ", ")
}

// A sign-in and a sign-out are the two events every other question starts
// from, so they are recorded with who, from where, and whether it worked.
func TestLoginIsRecorded(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)

	doLogin(t, r, "alice", loginTestPassword, "10.0.0.7")

	entry := mustFind(t, app, audit.EventLoginSucceeded)
	if entry.Actor.Username != "alice" {
		t.Errorf("actor = %+v, want alice", entry.Actor)
	}
	if entry.ClientIP != "10.0.0.7" {
		t.Errorf("clientIp = %q, want the address it came from", entry.ClientIP)
	}
	if entry.Outcome != audit.Success {
		t.Errorf("outcome = %q", entry.Outcome)
	}
}

// The reply to a failed sign-in says nothing about why. The trail may, because
// an administrator reading it is entitled to the distinction the caller is not.
func TestFailedLoginIsRecordedWithTheReason(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)
	if err := app.userStore().SetDisabled("alice", true); err != nil {
		t.Fatal(err)
	}

	w := doLogin(t, r, "alice", loginTestPassword, "10.0.0.9")
	if strings.Contains(strings.ToLower(w.Body.String()), "disabled") {
		t.Error("the reply told the caller the account was disabled")
	}

	entry := mustFind(t, app, audit.EventLoginFailed)
	if entry.Outcome != audit.Failure || entry.Target != "alice" {
		t.Errorf("entry = %+v", entry)
	}
	if entry.Detail["reason"] != "account disabled" {
		t.Errorf("reason = %q, want the real one", entry.Detail["reason"])
	}

	// A username that does not exist is recorded too — that is the shape of
	// somebody guessing.
	doLogin(t, r, "nobody", "wrong-password-here", "10.0.0.9")
	if got := mustFind(t, app, audit.EventLoginFailed); got.Target != "nobody" {
		t.Errorf("target = %q, want the attempted username", got.Target)
	}
}

// Whoever the password was correct for is not the only thing worth recording:
// a run of throttled attempts is the signature of a password list.
func TestLockoutIsRecorded(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "alice", authz.RoleUser, false)

	for i := 0; i < loginFreeAttempts+2; i++ {
		doLogin(t, r, "alice", "not-the-password", "10.0.0.3")
	}

	entry := mustFind(t, app, audit.EventLoginLockedOut)
	if entry.Outcome != audit.Denied || entry.Target != "alice" {
		t.Errorf("entry = %+v", entry)
	}
}

// freeSampleAppPort returns a port from the allowed sample-app list that
// nothing is currently listening on, so a test that needs a real sample app
// does not fail because of what else happens to be running on the machine.
func freeSampleAppPort(t *testing.T) int {
	t.Helper()
	for _, port := range allowedSampleAppPortsList {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		// Released straight away: this asks whether the port is free, and the
		// sample app is what actually binds it a moment later.
		_ = ln.Close()
		return port
	}
	t.Skip("no sample app port is free on this machine")
	return 0
}

// The plan's phrase was "session open with target host". Every creation path
// funnels through one function, so this is recorded once for all of them.
func TestSessionOpenIsRecordedWithTheHost(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	token := issueToken(t, app, alice.ID, "ci", nil)
	t.Setenv("ALLOW_SAMPLE_APPS", "1")

	// A sample app has to bind a port from the allowed list, so the test takes
	// whichever of them is free rather than always the first. "mock" is the
	// first, and asking for it failed the whole test whenever anything already
	// held it — a development instance running alongside `go test`, most often.
	port := freeSampleAppPort(t)
	body := fmt.Sprintf(`{"host":"sampleapp:app1:%d"}`, port)

	w := apiCall(r, http.MethodPost, "/api/v1/sessions", token, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("could not open a session: %d %s", w.Code, w.Body.String())
	}
	// A session left open outlives the test and the next one to ask for a
	// sample app cannot bind its port. Closing it here is what keeps the
	// package runnable more than once.
	var opened struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &opened); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s, ok := app.SessionManager.PeekSession(opened.ID); ok {
			app.cleanupSession(s)
		}
	})

	entry := mustFind(t, app, audit.EventSessionOpened)
	if entry.Actor.UserID != alice.ID {
		t.Errorf("actor = %+v, want alice's account", entry.Actor)
	}
	if entry.Actor.Kind != string(authz.KindAPIToken) {
		t.Errorf("kind = %q, want the trail to say it arrived on a token", entry.Actor.Kind)
	}
	if entry.Target == "" {
		t.Error("the entry does not say which host was connected to")
	}
	if entry.Detail["sessionId"] == "" {
		t.Error("the entry does not name the session")
	}
}

// A refused connection is as much a record as an accepted one — more so, for
// the limit case, where a run of them says an account is stuck rather than
// that somebody mistyped.
func TestRefusedConnectionIsRecorded(t *testing.T) {
	t.Setenv("MAX_SESSIONS_PER_USER", "1")
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	token := issueToken(t, app, alice.ID, "ci", nil)

	// One already open, so the next attempt is over the cap. The cap is
	// checked before anything is started, so this needs no real host.
	app.SessionManager.CreateSessionFor(alice.ID, mustMockHost(t))

	w := apiCall(r, http.MethodPost, "/api/v1/sessions", token, `{"host":"mainframe.example.test"}`)
	if w.Code == http.StatusCreated {
		t.Fatalf("the cap did not apply: %s", w.Body.String())
	}

	entry := mustFind(t, app, audit.EventSessionDenied)
	if entry.Outcome != audit.Denied {
		t.Errorf("outcome = %q", entry.Outcome)
	}
	if entry.Detail["reason"] != "session limit" {
		t.Errorf("reason = %q", entry.Detail["reason"])
	}
	if entry.Target != "mainframe.example.test" {
		t.Errorf("target = %q, want the host that was asked for", entry.Target)
	}
	if entry.Actor.UserID != alice.ID {
		t.Errorf("actor = %+v", entry.Actor)
	}
}

func TestAccountChangesAreRecorded(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	cookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))

	post := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.test")
		req.Header.Set("Referer", "http://example.test/admin/users")
		req.Host = "example.test"
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := post(http.MethodPost, "/api/admin/users", `{"username":"carol","password":"a-long-enough-password","role":"user"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}

	created := mustFind(t, app, audit.EventAccountCreated)
	if created.Target != "carol" || created.Actor.Username != "root" {
		t.Errorf("entry = %+v, want root creating carol", created)
	}

	var body struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	post(http.MethodPatch, "/api/admin/users/"+body.User.ID, `{"disabled":true}`)
	updated := mustFind(t, app, audit.EventAccountUpdated)
	if updated.Detail["change"] != "disabled" || updated.Detail["disabled"] != "true" {
		t.Errorf("update entry = %+v", updated)
	}

	post(http.MethodDelete, "/api/admin/users/"+body.User.ID, "")
	deleted := mustFind(t, app, audit.EventAccountDeleted)
	if deleted.Target != "carol" {
		t.Errorf("delete entry = %+v", deleted)
	}
}

// The trail must never become a second place credentials live.
func TestPasswordsAndTokensAreNeverWritten(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	secret := issueToken(t, app, alice.ID, "ci", nil)

	doLogin(t, r, "alice", loginTestPassword, "10.0.0.1")
	doLogin(t, r, "alice", "a-wrong-password-value", "10.0.0.1")
	apiGet(r, "/api/v1/sessions", secret)
	apiGet(r, "/api/v1/sessions", "3270w_deadbeefdeadbeef_aaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	raw, err := readWholeAudit(app)
	if err != nil {
		t.Fatal(err)
	}
	for name, needle := range map[string]string{
		"the account's password": loginTestPassword,
		"a mistyped password":    "a-wrong-password-value",
		"a valid token":          secret,
		"a rejected token":       "3270w_deadbeefdeadbeef_aaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		if strings.Contains(raw, needle) {
			t.Errorf("%s appears in the audit log", name)
		}
	}
}

// A token presented and refused is the event worth having; one that works is
// not, or the file becomes an access log and buries everything else.
func TestRefusedTokensAreRecordedAndAcceptedOnesAreNot(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	alice := addUser(t, app, "alice", authz.RoleUser, false)
	good := issueToken(t, app, alice.ID, "ci", nil)

	apiGet(r, "/api/v1/sessions", "3270w_0000000000000000_zzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	entry := mustFind(t, app, audit.EventTokenRefused)
	if entry.Outcome != audit.Denied || entry.Detail["path"] != "/api/v1/sessions" {
		t.Errorf("entry = %+v", entry)
	}

	before := len(trailOf(t, app))
	for i := 0; i < 5; i++ {
		apiGet(r, "/api/v1/sessions", good)
	}
	if after := len(trailOf(t, app)); after != before {
		t.Errorf("%d lines were added by successful API calls; the trail is becoming an access log",
			after-before)
	}
}

// The settings a person changed, never the values: one of them is a keyfile
// password and another is a proxy string.
func TestSettingsChangesRecordKeysNotValues(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	root := addUser(t, app, "root", authz.RoleAdmin, false)
	cookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))
	// The settings page needs a terminal session as well as the admin role.
	terminal := app.SessionManager.CreateSessionFor(root.ID, mustMockHost(t))

	const secretValue = "hunter2-the-keyfile-password"
	req := httptest.NewRequest(http.MethodPost, "/api/settings",
		strings.NewReader(`{"settings":{"S3270_KEY_PASSWORD":"`+secretValue+`"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/screen")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: terminal.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save = %d %s", w.Code, w.Body.String())
	}

	entry := mustFind(t, app, audit.EventSettingsSaved)
	if !strings.Contains(entry.Detail["keys"], "S3270_KEY_PASSWORD") {
		t.Errorf("the entry does not name the setting: %+v", entry)
	}
	raw, err := readWholeAudit(app)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, secretValue) {
		t.Error("a setting's value was written to the audit log")
	}
}

// The trail names accounts, addresses and the hosts they reached — the
// material for working out who to impersonate.
func TestAuditIsAdminOnly(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	addUser(t, app, "bob", authz.RoleUser, false)

	get := func(cookie, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "application/json")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := get("", "/api/admin/audit"); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401", w.Code)
	}
	bob := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.1"))
	if w := get(bob, "/api/admin/audit"); w.Code != http.StatusForbidden {
		t.Errorf("a regular account = %d, want 403", w.Code)
	}
	if w := get(bob, "/api/admin/audit/download"); w.Code != http.StatusForbidden {
		t.Errorf("download by a regular account = %d, want 403", w.Code)
	}

	root := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))
	w := get(root, "/api/admin/audit")
	if w.Code != http.StatusOK {
		t.Fatalf("an administrator = %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Entries []audit.Entry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) == 0 {
		t.Error("the trail is empty after three sign-ins")
	}
	if w := get(root, "/api/admin/audit/download"); w.Code != http.StatusOK {
		t.Errorf("download = %d", w.Code)
	}
}

// With one operator there is nobody to attribute anything to a name, but the
// events are still recorded — and must not crash for want of a username.
func TestAuditWorksForASingleOperator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "none")
	t.Setenv("API_TOKEN", "a-test-api-token")

	t.Setenv("MAX_SESSIONS_PER_USER", "1")

	app := newApp(t.TempDir())
	r, err := buildRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	app.SessionManager.CreateSessionFor(authz.LocalUserID, mustMockHost(t))

	apiCall(r, http.MethodPost, "/api/v1/sessions", "a-test-api-token", `{"host":"mainframe.example.test"}`)

	entry := mustFind(t, app, audit.EventSessionDenied)
	if entry.Actor.UserID != authz.LocalUserID {
		t.Errorf("actor = %+v, want the local operator", entry.Actor)
	}
	if entry.Actor.Username == "" {
		t.Error("the entry has no readable actor name")
	}
}

func readWholeAudit(app *App) (string, error) {
	var b strings.Builder
	if err := app.auditRecorder().Dump(&b); err != nil {
		return "", err
	}
	return b.String(), nil
}
