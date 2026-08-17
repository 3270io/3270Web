// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
)

// adminJSON drives a JSON route as a signed-in caller and decodes the answer.
func adminJSON(t *testing.T, r *gin.Engine, method, path, cookie string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s %s: not JSON: %v (body=%s)", method, path, err, w.Body.String())
	}
	return w.Code, body
}

// The overview is the page an administrator lands on, so its numbers must be
// the instance's numbers: accounts, live logins, open terminal sessions.
func TestAdminOverviewReportsInstanceState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	addUser(t, app, "bob", authz.RoleUser, false)
	cookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))

	// Two terminals for bob, none for root.
	bobID := userID(t, app, "bob")
	app.SessionManager.CreateSessionFor(bobID, mustMockHost(t))
	app.SessionManager.CreateSessionFor(bobID, mustMockHost(t))

	code, body := adminJSON(t, r, http.MethodGet, "/api/admin/overview", cookie)
	if code != http.StatusOK {
		t.Fatalf("overview = %d, want 200 (%v)", code, body)
	}

	accounts, _ := body["accounts"].(map[string]any)
	if got := accounts["total"]; got != float64(2) {
		t.Errorf("accounts.total = %v, want 2", got)
	}
	if got := accounts["admins"]; got != float64(1) {
		t.Errorf("accounts.admins = %v, want 1", got)
	}

	sessions, _ := body["sessions"].(map[string]any)
	if got := sessions["open"]; got != float64(2) {
		t.Errorf("sessions.open = %v, want 2", got)
	}

	signedIn, _ := body["signedIn"].(map[string]any)
	if got := signedIn["logins"]; got != float64(1) {
		t.Errorf("signedIn.logins = %v, want 1", got)
	}

	// The login above was audited, so the trail section cannot be empty.
	auditPart, _ := body["audit"].(map[string]any)
	recent, _ := auditPart["recent"].([]any)
	if len(recent) == 0 {
		t.Error("audit.recent is empty after a sign-in")
	}
}

// With accounts off the overview still answers — the default single-operator
// deployment has terminal sessions worth seeing even with nobody to count.
func TestAdminOverviewWithAuthDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, r := newAuthTestApp(t, "none")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("overview = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["authEnabled"] != false {
		t.Errorf("authEnabled = %v, want false", body["authEnabled"])
	}
}

// The session list names the owner and the host, and closing a session
// actually ends it — that is the whole of the power the page offers.
func TestAdminSessionsListAndClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	addUser(t, app, "bob", authz.RoleUser, false)
	cookie := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))

	s := app.SessionManager.CreateSessionFor(userID(t, app, "bob"), mustMockHost(t))
	s.TargetHost = "mainframe.example"
	s.TargetPort = 23

	code, body := adminJSON(t, r, http.MethodGet, "/api/admin/sessions", cookie)
	if code != http.StatusOK {
		t.Fatalf("list = %d, want 200 (%v)", code, body)
	}
	list, _ := body["sessions"].([]any)
	if len(list) != 1 {
		t.Fatalf("sessions = %d entries, want 1", len(list))
	}
	row, _ := list[0].(map[string]any)
	if row["owner"] != "bob" {
		t.Errorf("owner = %v, want bob", row["owner"])
	}
	if row["host"] != "mainframe.example" {
		t.Errorf("host = %v, want mainframe.example", row["host"])
	}
	if row["id"] != s.ID {
		t.Errorf("id = %v, want %s", row["id"], s.ID)
	}

	code, body = adminJSON(t, r, http.MethodDelete, "/api/admin/sessions/"+s.ID, cookie)
	if code != http.StatusOK {
		t.Fatalf("close = %d, want 200 (%v)", code, body)
	}
	if app.SessionManager.Count() != 0 {
		t.Errorf("session survived an administrative close")
	}

	// Closing it again is a 404, not a silent success: the caller should
	// learn the ID they are holding no longer names anything.
	code, _ = adminJSON(t, r, http.MethodDelete, "/api/admin/sessions/"+s.ID, cookie)
	if code != http.StatusNotFound {
		t.Errorf("second close = %d, want 404", code)
	}
}
