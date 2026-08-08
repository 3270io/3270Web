package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// adminRequest issues a JSON API call carrying the given login cookie.
func adminRequest(r *gin.Engine, method, path, cookie, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/admin/users")
	req.Host = "example.test"
	req.RemoteAddr = "10.0.0.1:40000"
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// signIn creates an account and returns its login cookie and id.
func signIn(t *testing.T, app *App, r *gin.Engine, name string, role authz.Role) (string, string) {
	t.Helper()
	user, err := app.userStore().Add(name, loginTestPassword, role, false)
	if err != nil {
		t.Fatalf("add %s: %v", name, err)
	}
	cookie := authCookieFrom(doLogin(t, r, name, loginTestPassword, "10.0.0.1"))
	if cookie == "" {
		t.Fatalf("%s could not sign in", name)
	}
	return cookie, user.ID
}

func decodeUsers(t *testing.T, w *httptest.ResponseRecorder) []adminUserView {
	t.Helper()
	var payload struct {
		Users []adminUserView `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return payload.Users
}

func TestAdminAPIRequiresAdmin(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	userCookie, _ := signIn(t, app, r, "bob", authz.RoleUser)

	// Anonymous.
	if w := adminRequest(r, http.MethodGet, "/api/admin/users", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous list = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Signed in, but not an administrator. Every verb must refuse, not just
	// the read.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/admin/users", ""},
		{http.MethodPost, "/api/admin/users", `{"username":"x","password":"` + loginTestPassword + `"}`},
		{http.MethodPatch, "/api/admin/users/anything", `{"role":"admin"}`},
		{http.MethodDelete, "/api/admin/users/anything", ""},
	} {
		w := adminRequest(r, tc.method, tc.path, userCookie, tc.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s as a regular user = %d, want %d", tc.method, tc.path, w.Code, http.StatusForbidden)
		}
	}

	if w := adminRequest(r, http.MethodGet, "/api/admin/users", adminCookie, ""); w.Code != http.StatusOK {
		t.Errorf("admin list = %d, want 200", w.Code)
	}
}

func TestAdminListMarksSelf(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, adminID := signIn(t, app, r, "root", authz.RoleAdmin)
	signIn(t, app, r, "bob", authz.RoleUser)

	list := decodeUsers(t, adminRequest(r, http.MethodGet, "/api/admin/users", adminCookie, ""))
	if len(list) != 2 {
		t.Fatalf("got %d accounts, want 2", len(list))
	}
	for _, u := range list {
		if (u.ID == adminID) != u.Self {
			t.Errorf("%s self = %v, want %v", u.Username, u.Self, u.ID == adminID)
		}
	}
}

func TestAdminCreateUser(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)

	w := adminRequest(r, http.MethodPost, "/api/admin/users", adminCookie,
		`{"username":"carol","password":"`+loginTestPassword+`","role":"admin"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d, want %d (body=%s)", w.Code, http.StatusCreated, w.Body.String())
	}

	list, err := app.userStore().List()
	if err != nil {
		t.Fatal(err)
	}
	var carol *users.User
	for i := range list {
		if list[i].Username == "carol" {
			carol = &list[i]
		}
	}
	if carol == nil {
		t.Fatal("carol was not created")
	}
	if carol.Role != authz.RoleAdmin {
		t.Errorf("role = %q, want admin", carol.Role)
	}
	// The administrator chose this password, so its owner has to replace it.
	if !carol.MustChangePassword {
		t.Error("an admin-chosen password did not require a change")
	}

	// Duplicates and weak passwords are refused with a usable message.
	if w := adminRequest(r, http.MethodPost, "/api/admin/users", adminCookie,
		`{"username":"carol","password":"`+loginTestPassword+`"}`); w.Code != http.StatusConflict {
		t.Errorf("duplicate = %d, want %d", w.Code, http.StatusConflict)
	}
	if w := adminRequest(r, http.MethodPost, "/api/admin/users", adminCookie,
		`{"username":"dave","password":"short"}`); w.Code != http.StatusBadRequest {
		t.Errorf("short password = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if w := adminRequest(r, http.MethodPost, "/api/admin/users", adminCookie,
		`{"username":"../etc/passwd","password":"`+loginTestPassword+`"}`); w.Code != http.StatusBadRequest {
		t.Errorf("unsafe username = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// Each of these would strand the administrator on a page they can no longer
// use, with no way back except the CLI.
func TestAdminCannotLockThemselvesOut(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, adminID := signIn(t, app, r, "root", authz.RoleAdmin)
	signIn(t, app, r, "second", authz.RoleAdmin)

	for _, tc := range []struct {
		name, method, body string
	}{
		{"self-demote", http.MethodPatch, `{"role":"user"}`},
		{"self-disable", http.MethodPatch, `{"disabled":true}`},
		{"self-delete", http.MethodDelete, ""},
	} {
		w := adminRequest(r, tc.method, "/api/admin/users/"+adminID, adminCookie, tc.body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want %d (body=%s)", tc.name, w.Code, http.StatusBadRequest, w.Body.String())
		}
	}

	// Still an enabled admin afterwards.
	if w := adminRequest(r, http.MethodGet, "/api/admin/users", adminCookie, ""); w.Code != http.StatusOK {
		t.Fatalf("the administrator lost access: %d", w.Code)
	}
}

// The store's last-admin guard has to surface through the API too, not just
// when called directly.
func TestAdminLastAdminIsProtected(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	_, otherAdminID := signIn(t, app, r, "second", authz.RoleAdmin)

	// Demote the other admin; root is now the only one.
	if w := adminRequest(r, http.MethodPatch, "/api/admin/users/"+otherAdminID, adminCookie,
		`{"role":"user"}`); w.Code != http.StatusOK {
		t.Fatalf("demote = %d (body=%s)", w.Code, w.Body.String())
	}

	list := decodeUsers(t, adminRequest(r, http.MethodGet, "/api/admin/users", adminCookie, ""))
	admins := 0
	for _, u := range list {
		if u.Role == string(authz.RoleAdmin) {
			admins++
		}
	}
	if admins != 1 {
		t.Fatalf("admin count = %d, want 1", admins)
	}
}

// Disabling has to end the account's live sessions; otherwise it only takes
// effect the next time they try to sign in, which is not what "disabled"
// means to the administrator clicking it.
func TestAdminDisableEndsLiveSessions(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	bobCookie, bobID := signIn(t, app, r, "bob", authz.RoleUser)

	if w := getWithAuth(r, "/api/whoami", bobCookie, "10.0.0.1"); w.Code != http.StatusOK {
		t.Fatalf("bob is not signed in: %d", w.Code)
	}

	if w := adminRequest(r, http.MethodPatch, "/api/admin/users/"+bobID, adminCookie,
		`{"disabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("disable = %d (body=%s)", w.Code, w.Body.String())
	}

	if w := getWithAuth(r, "/api/whoami", bobCookie, "10.0.0.1"); w.Code != http.StatusUnauthorized {
		t.Errorf("bob's session survived being disabled: %d", w.Code)
	}
	if w := doLogin(t, r, "bob", loginTestPassword, "10.0.0.1"); w.Code != http.StatusUnauthorized {
		t.Errorf("a disabled account signed in again: %d", w.Code)
	}

	// Re-enabling restores the ability to sign in.
	if w := adminRequest(r, http.MethodPatch, "/api/admin/users/"+bobID, adminCookie,
		`{"disabled":false}`); w.Code != http.StatusOK {
		t.Fatalf("enable = %d", w.Code)
	}
	if w := doLogin(t, r, "bob", loginTestPassword, "10.0.0.1"); w.Code != http.StatusFound {
		t.Errorf("re-enabled account cannot sign in: %d", w.Code)
	}
}

func TestAdminResetPassword(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	bobCookie, bobID := signIn(t, app, r, "bob", authz.RoleUser)

	const issued = "an-issued-temporary-password"
	if w := adminRequest(r, http.MethodPatch, "/api/admin/users/"+bobID, adminCookie,
		`{"password":"`+issued+`"}`); w.Code != http.StatusOK {
		t.Fatalf("reset = %d (body=%s)", w.Code, w.Body.String())
	}

	// The old session goes, because the administrator now knows the password.
	if w := getWithAuth(r, "/api/whoami", bobCookie, "10.0.0.1"); w.Code != http.StatusUnauthorized {
		t.Errorf("bob's session survived a password reset: %d", w.Code)
	}
	if _, err := app.userStore().Authenticate("bob", loginTestPassword); err == nil {
		t.Error("the old password still works")
	}

	// And the issued password is pinned to the change-password page.
	got, err := app.userStore().Authenticate("bob", issued)
	if err != nil {
		t.Fatalf("issued password does not work: %v", err)
	}
	if !got.MustChangePassword {
		t.Error("an admin-reset password did not require a change")
	}

	if w := adminRequest(r, http.MethodPatch, "/api/admin/users/"+bobID, adminCookie,
		`{"password":"short"}`); w.Code != http.StatusBadRequest {
		t.Errorf("short reset = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAdminDeleteUser(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	bobCookie, bobID := signIn(t, app, r, "bob", authz.RoleUser)

	if w := adminRequest(r, http.MethodDelete, "/api/admin/users/"+bobID, adminCookie, ""); w.Code != http.StatusOK {
		t.Fatalf("delete = %d (body=%s)", w.Code, w.Body.String())
	}
	if w := getWithAuth(r, "/api/whoami", bobCookie, "10.0.0.1"); w.Code != http.StatusUnauthorized {
		t.Errorf("a deleted account's session survived: %d", w.Code)
	}
	if _, err := app.userStore().Authenticate("bob", loginTestPassword); err == nil {
		t.Error("a deleted account can still authenticate")
	}
	if w := adminRequest(r, http.MethodDelete, "/api/admin/users/"+bobID, adminCookie, ""); w.Code != http.StatusNotFound {
		t.Errorf("deleting a missing account = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAdminUnknownTarget(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)

	if w := adminRequest(r, http.MethodPatch, "/api/admin/users/nope", adminCookie,
		`{"role":"admin"}`); w.Code != http.StatusNotFound {
		t.Errorf("unknown id = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Under AUTH_MODE=none the single operator is an admin, so the page must open
// — but there are no accounts to manage and mutating calls should say so
// rather than write to a store nothing reads.
func TestAdminUnderAuthModeNone(t *testing.T) {
	_, r := newAuthTestApp(t, "none")

	w := adminRequest(r, http.MethodGet, "/api/admin/users", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"authEnabled":false`) {
		t.Errorf("body = %q, want authEnabled false", w.Body.String())
	}

	if w := adminRequest(r, http.MethodPost, "/api/admin/users", "",
		`{"username":"x","password":"`+loginTestPassword+`"}`); w.Code != http.StatusConflict {
		t.Errorf("create = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestAdminPageRendersForAdminOnly(t *testing.T) {
	app, r := newAuthTestApp(t, "local")
	adminCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	userCookie, _ := signIn(t, app, r, "bob", authz.RoleUser)

	if w := getWithAuth(r, adminUsersPath, adminCookie, "10.0.0.1"); w.Code != http.StatusOK {
		t.Errorf("admin page for an admin = %d, want 200", w.Code)
	}
	if w := getWithAuth(r, adminUsersPath, userCookie, "10.0.0.1"); w.Code != http.StatusForbidden {
		t.Errorf("admin page for a regular user = %d, want %d", w.Code, http.StatusForbidden)
	}
}
