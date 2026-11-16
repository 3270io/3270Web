// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
)

// An account that is not an administrator and opens an administration page is
// refused, which is right, and was told "Something went wrong" under the
// administrator-only sentence folded away as "Technical details": a refusal
// laid out as a crash. It is a statement about the account, and says so.
func TestAdminPagesRefuseANonAdminInPlainWords(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "bob", authz.RoleUser, false)
	cookie := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.2"))
	if cookie == "" {
		t.Fatal("bob could not sign in")
	}

	for _, path := range []string{"/admin", "/admin/users", "/admin/logs", "/admin/session-screen"} {
		w := getWithAuth(r, path, cookie, "10.0.0.2")
		body := w.Body.String()

		if w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", path, w.Code)
		}
		if !strings.Contains(body, "You don't have access to administration") && !strings.Contains(body, "You don&#39;t have access to administration") {
			t.Errorf("%s: the page does not say the account lacks access:\n%s", path, body)
		}
		if strings.Contains(body, "Something went wrong") {
			t.Errorf("%s: a permission refusal is titled as a crash", path)
		}
		if strings.Contains(strings.ToLower(body), "has not been disconnected") {
			t.Errorf("%s: the page makes a claim about the session", path)
		}
		// A way back, to where this account does belong.
		for _, want := range []string{`href="/screen"`, `href="/"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no way back (missing %s)", path, want)
			}
		}
	}
}

// Script callers keep the machine-readable refusal.
func TestAdminAPIStillRefusesANonAdminWithJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "bob", authz.RoleUser, false)
	cookie := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.2"))

	w := getWithAuth(r, "/api/admin/users", cookie, "10.0.0.2")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"forbidden"`) {
		t.Errorf("status = %d body = %s, want a 403 with code forbidden", w.Code, w.Body.String())
	}
}
