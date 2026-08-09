package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
)

// A role assigned to a group must reach the people in it — including the
// people already signed in, whose sessions carry the role they logged in
// with. This walks the whole arc: refused, granted, entitled, revoked,
// refused again, without bob ever signing in twice.
func TestGroupRoleGrantsAndRevokesAdministration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	rootCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	bobCookie, _ := signIn(t, app, r, "bob", authz.RoleUser)
	if err := app.userStore().SetGroups("bob", []string{"ops"}); err != nil {
		t.Fatal(err)
	}

	if w := adminRequest(r, http.MethodGet, "/api/admin/users", bobCookie, ""); w.Code != http.StatusForbidden {
		t.Fatalf("bob reached administration before any grant: %d", w.Code)
	}

	// Root assigns the administrator role to ops.
	w := adminRequest(r, http.MethodPost, "/api/admin/group-roles", rootCookie,
		`{"group":"ops","role":"admin"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}

	// Bob's existing login now carries the role — no fresh sign-in.
	if w := adminRequest(r, http.MethodGet, "/api/admin/users", bobCookie, ""); w.Code != http.StatusOK {
		t.Errorf("bob refused administration after the group grant: %d %s", w.Code, w.Body.String())
	}

	// The listing says what he holds and where it came from.
	w = adminRequest(r, http.MethodGet, "/api/admin/users", rootCookie, "")
	var payload struct {
		Users      []adminUserView   `json:"users"`
		GroupRoles map[string]string `json:"groupRoles"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.GroupRoles["ops"] != "admin" {
		t.Errorf("groupRoles = %v, want ops->admin", payload.GroupRoles)
	}
	for _, u := range payload.Users {
		if u.Username != "bob" {
			continue
		}
		if u.Role != "user" || u.EffectiveRole != "admin" {
			t.Errorf("bob role=%s effective=%s, want user/admin", u.Role, u.EffectiveRole)
		}
		if len(u.RoleGroups) != 1 || u.RoleGroups[0] != "ops" {
			t.Errorf("bob roleGroups = %v, want [ops]", u.RoleGroups)
		}
	}

	// Revoking demotes the live session the same way.
	w = adminRequest(r, http.MethodPost, "/api/admin/group-roles", rootCookie,
		`{"group":"ops","role":"user"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke = %d: %s", w.Code, w.Body.String())
	}
	if w := adminRequest(r, http.MethodGet, "/api/admin/users", bobCookie, ""); w.Code != http.StatusForbidden {
		t.Errorf("bob still an administrator after the grant was revoked: %d", w.Code)
	}
}

// The grant survives a fresh sign-in — the login path resolves the effective
// role, not just the live-session push.
func TestGroupRoleAppliesAtLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	rootCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	addUser(t, app, "carol", authz.RoleUser, false)
	if err := app.userStore().SetGroups("carol", []string{"ops"}); err != nil {
		t.Fatal(err)
	}
	w := adminRequest(r, http.MethodPost, "/api/admin/group-roles", rootCookie,
		`{"group":"ops","role":"admin"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}

	carolCookie := authCookieFrom(doLogin(t, r, "carol", loginTestPassword, "10.0.0.9"))
	if w := adminRequest(r, http.MethodGet, "/api/admin/users", carolCookie, ""); w.Code != http.StatusOK {
		t.Errorf("carol refused administration after signing in: %d %s", w.Code, w.Body.String())
	}
}

// Somebody whose administration comes only from a group cannot saw off the
// branch they are standing on — neither the assignment nor their own
// membership of the group.
func TestGroupRoleSelfLockoutGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	rootCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	bobCookie, bobID := signIn(t, app, r, "bob", authz.RoleUser)
	if err := app.userStore().SetGroups("bob", []string{"ops"}); err != nil {
		t.Fatal(err)
	}
	w := adminRequest(r, http.MethodPost, "/api/admin/group-roles", rootCookie,
		`{"group":"ops","role":"admin"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}

	// Bob (administrator via ops) may not clear the assignment himself.
	w = adminRequest(r, http.MethodPost, "/api/admin/group-roles", bobCookie,
		`{"group":"ops","role":"user"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bob cleared the assignment his own role depends on: %d %s", w.Code, w.Body.String())
	}

	// Nor may he leave the group.
	w = adminRequest(r, http.MethodPatch, "/api/admin/users/"+bobID, bobCookie, `{"groups":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bob left the group his own role depends on: %d %s", w.Code, w.Body.String())
	}

	// Root, an administrator in his own right, may do both.
	w = adminRequest(r, http.MethodPatch, "/api/admin/users/"+bobID, rootCookie, `{"groups":[]}`)
	if w.Code != http.StatusOK {
		t.Errorf("root could not change bob's groups: %d %s", w.Code, w.Body.String())
	}
	// With bob out of the group, his live session loses the role.
	if w := adminRequest(r, http.MethodGet, "/api/admin/users", bobCookie, ""); w.Code != http.StatusForbidden {
		t.Errorf("bob kept administration after leaving the group: %d", w.Code)
	}
}

// The store refuses to remove the assignment that keeps the only enabled
// administrator, the same way it refuses to demote or disable them.
func TestGroupRoleLastAdminGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	rootCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	addUser(t, app, "bob", authz.RoleUser, false)
	if err := app.userStore().SetGroups("bob", []string{"ops"}); err != nil {
		t.Fatal(err)
	}
	w := adminRequest(r, http.MethodPost, "/api/admin/group-roles", rootCookie,
		`{"group":"ops","role":"admin"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("assign = %d: %s", w.Code, w.Body.String())
	}

	// With the group grant in place, the one direct administrator can be
	// demoted — bob keeps the instance administrable.
	if err := app.userStore().SetRole("root", authz.RoleUser); err != nil {
		t.Fatalf("could not demote root while a group grants admin: %v", err)
	}

	// Now the assignment is what keeps the only administrator; removing it
	// must be refused at the store, whoever asks.
	if err := app.userStore().SetGroupRole("ops", authz.RoleUser); err == nil {
		t.Error("removing the assignment that keeps the only admin was allowed")
	}
}

// A published preset carrying an audience reaches exactly the people the
// audience names — the RBAC control the session-selection screen runs on.
func TestAdminPresetsRespectAudience(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	rootCookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	bobCookie, _ := signIn(t, app, r, "bob", authz.RoleUser)
	carolCookie, _ := signIn(t, app, r, "carol", authz.RoleUser)
	if err := app.userStore().SetGroups("bob", []string{"payments"}); err != nil {
		t.Fatal(err)
	}

	w := adminRequest(r, http.MethodPost, "/api/admin/profiles", rootCookie,
		`{"name":"Payments CICS","host":"cics.example","port":23,"groups":["payments"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("publish preset = %d: %s", w.Code, w.Body.String())
	}

	sees := func(cookie string) bool {
		w := adminRequest(r, http.MethodGet, "/api/profiles", cookie, "")
		if w.Code != http.StatusOK {
			t.Fatalf("profiles list = %d: %s", w.Code, w.Body.String())
		}
		return strings.Contains(w.Body.String(), "Payments CICS")
	}
	if !sees(bobCookie) {
		t.Error("bob (in payments) was not offered the preset")
	}
	if sees(carolCookie) {
		t.Error("carol (not in payments) was offered the preset")
	}

	// The management listing shows it with its audience.
	w = adminRequest(r, http.MethodGet, "/api/admin/profiles", rootCookie, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "payments") {
		t.Errorf("admin listing = %d: %s", w.Code, w.Body.String())
	}

	// Removing it takes it away from everybody.
	w = adminRequest(r, http.MethodPost, "/api/admin/profiles/delete", rootCookie,
		`{"name":"Payments CICS"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("delete preset = %d: %s", w.Code, w.Body.String())
	}
	if sees(bobCookie) {
		t.Error("the preset survived deletion")
	}
}
