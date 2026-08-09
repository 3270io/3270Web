package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// The group administration page, driven end to end.
//
// The complaint these answer is a workflow, not a function: an administrator
// makes a team, says which mainframes it reaches, puts people in it, and the
// people are then offered exactly those mainframes. Every one of those steps
// used to live on a different page, keyed by a name that had to be typed the
// same way in each.

func groupsTestApp(t *testing.T) (*App, *gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	cookie, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	if cookie == "" {
		t.Fatal("the administrator could not sign in")
	}
	return app, r, cookie
}

func decodeGroups(t *testing.T, w *httptest.ResponseRecorder) []adminGroupView {
	t.Helper()
	var payload struct {
		Groups []adminGroupView `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return payload.Groups
}

func groupNamed(t *testing.T, list []adminGroupView, name string) adminGroupView {
	t.Helper()
	for _, g := range list {
		if strings.EqualFold(g.Name, name) {
			return g
		}
	}
	t.Fatalf("no group named %q in %+v", name, list)
	return adminGroupView{}
}

func reachableBy(t *testing.T, app *App, username string) []string {
	t.Helper()
	profiles, err := app.assignedProfiles(requestAs(t, app, username))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

// The whole of the reported gap, in one test: make a group, give it a host,
// put somebody in it, and see that they are offered it.
func TestAnAdministratorCanCreateAGroupGiveItHostsAndFillIt(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)

	// Two published presets, neither of which is for everyone — an unassigned
	// preset reaches every account by design, and would hide the effect.
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Users: []string{"root"}},
		ConnectionProfile{Name: "SHIPPING", Host: "ship.example", Port: 23, Users: []string{"root"}},
	)

	if got := reachableBy(t, app, "alice"); len(got) != 0 {
		t.Fatalf("alice already reaches %v before being given anything", got)
	}

	w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments","description":"Overnight batch","members":["alice"],"hosts":["PAYMENTS"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("creating the group returned %d: %s", w.Code, w.Body.String())
	}

	if got := reachableBy(t, app, "alice"); len(got) != 1 || got[0] != "PAYMENTS" {
		t.Errorf("alice reaches %v, want only the host her group was given", got)
	}

	// And the page says the same thing back: the group, its member and its host.
	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	group := groupNamed(t, list, "payments")
	if group.Description != "Overnight batch" {
		t.Errorf("Description = %q", group.Description)
	}
	if len(group.Members) != 1 || group.Members[0] != "alice" {
		t.Errorf("Members = %v, want alice", group.Members)
	}
	if len(group.Hosts) != 1 || group.Hosts[0] != "PAYMENTS" {
		t.Errorf("Hosts = %v, want PAYMENTS", group.Hosts)
	}
	if !group.Declared {
		t.Error("a group created here is not marked as declared")
	}
}

func TestAnEmptyGroupCanBePreparedBeforeAnybodyIsInIt(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Users: []string{"root"}})

	w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments","hosts":["PAYMENTS"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("creating an empty group returned %d: %s", w.Code, w.Body.String())
	}

	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	group := groupNamed(t, list, "payments")
	if group.MemberCount != 0 {
		t.Errorf("MemberCount = %d, want an empty group", group.MemberCount)
	}
	if len(group.Hosts) != 1 {
		t.Errorf("Hosts = %v, want the host it was prepared with", group.Hosts)
	}

	// Somebody added later inherits what the group was given, with nothing
	// further to configure.
	signIn(t, app, r, "alice", authz.RoleUser)
	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups/payments", admin,
		`{"members":["alice"]}`); w.Code != http.StatusOK {
		t.Fatalf("adding a member returned %d: %s", w.Code, w.Body.String())
	}
	if got := reachableBy(t, app, "alice"); len(got) != 1 || got[0] != "PAYMENTS" {
		t.Errorf("alice reaches %v, want the host her group already had", got)
	}
}

func TestCreatingAGroupTwiceIsRefused(t *testing.T) {
	_, r, admin := groupsTestApp(t)
	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments"}`); w.Code != http.StatusCreated {
		t.Fatalf("first create returned %d: %s", w.Code, w.Body.String())
	}
	w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin, `{"name":"PAYMENTS"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create returned %d, want 409: %s", w.Code, w.Body.String())
	}
}

// A rename that did not follow into the published presets would leave the
// group looking intact and reaching nothing — the failure nobody notices,
// because the people who stop seeing the host do not know it was there.
func TestRenamingAGroupKeepsItsHostsAndItsMembers(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Users: []string{"root"}})

	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments","members":["alice"],"hosts":["PAYMENTS"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}

	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups/payments", admin,
		`{"name":"Payments Team"}`); w.Code != http.StatusOK {
		t.Fatalf("rename returned %d: %s", w.Code, w.Body.String())
	}

	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if len(list) != 1 {
		t.Fatalf("groups = %+v, want the renamed group alone rather than an orphan beside it", list)
	}
	group := groupNamed(t, list, "Payments Team")
	if len(group.Hosts) != 1 || group.Hosts[0] != "PAYMENTS" {
		t.Errorf("Hosts = %v, want the host to have come with the rename", group.Hosts)
	}
	if len(group.Members) != 1 || group.Members[0] != "alice" {
		t.Errorf("Members = %v, want alice to have come with the rename", group.Members)
	}
	if got := reachableBy(t, app, "alice"); len(got) != 1 || got[0] != "PAYMENTS" {
		t.Errorf("alice reaches %v after the rename, want PAYMENTS", got)
	}
}

func TestRemovingAHostFromAGroupTakesItAway(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Users: []string{"root"}},
		ConnectionProfile{Name: "SHIPPING", Host: "ship.example", Port: 23, Users: []string{"root"}},
	)
	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments","members":["alice"],"hosts":["PAYMENTS","SHIPPING"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}
	if got := reachableBy(t, app, "alice"); len(got) != 2 {
		t.Fatalf("alice reaches %v, want both hosts", got)
	}

	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups/payments", admin,
		`{"hosts":["PAYMENTS"]}`); w.Code != http.StatusOK {
		t.Fatalf("host change returned %d: %s", w.Code, w.Body.String())
	}
	if got := reachableBy(t, app, "alice"); len(got) != 1 || got[0] != "PAYMENTS" {
		t.Errorf("alice reaches %v, want only the host still assigned", got)
	}
}

// Dropping the last name from a preset's audience makes it everyone's again.
// That is the existing rule — a preset naming nobody is for everybody — and it
// is a surprising enough consequence of unticking a box that the response says
// so rather than leaving it to be discovered.
func TestAPresetLeftNamingNobodyIsReportedAsOfferedToEveryone(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23})

	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments","hosts":["PAYMENTS"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}
	w := adminRequest(r, http.MethodPatch, "/api/admin/groups/payments", admin, `{"hosts":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("host change returned %d: %s", w.Code, w.Body.String())
	}
	var payload struct {
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Notes) != 1 || !strings.Contains(payload.Notes[0], "everyone") {
		t.Errorf("notes = %v, want one saying the preset is offered to everyone", payload.Notes)
	}
}

func TestDeletingAGroupClearsItFromAccountsAndPresets(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Users: []string{"root"}},
		ConnectionProfile{Name: "SHIPPING", Host: "ship.example", Port: 23, Users: []string{"root"}},
	)
	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments","members":["alice"],"hosts":["PAYMENTS"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}

	if w := adminRequest(r, http.MethodDelete, "/api/admin/groups/payments", admin, ""); w.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", w.Code, w.Body.String())
	}

	if list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, "")); len(list) != 0 {
		t.Errorf("groups = %+v, want none", list)
	}

	// The preset is not deleted with the group — it is somebody else's host —
	// but it no longer names a group that does not exist.
	store := app.publishedProfiles(requestAs(t, app, "root"))
	profile, found := store.find("PAYMENTS")
	if !found {
		t.Fatal("the preset was deleted along with the group")
	}
	if len(profile.Groups) != 0 {
		t.Errorf("preset still names %v", profile.Groups)
	}
	if got := reachableBy(t, app, "alice"); len(got) != 0 {
		t.Errorf("alice still reaches %v", got)
	}
}

// A role given to a team has to reach the people already signed in, or the
// administrator watches themselves make a change that does nothing.
func TestAGroupCanGrantTheAdministratorRoleFromThisPage(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)

	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"ops","members":["alice"],"role":"admin"}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}

	alice := userNamed(t, app, "alice")
	if app.effectiveRoleFor(alice) != authz.RoleAdmin {
		t.Error("the member did not inherit the role the group grants")
	}

	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if got := groupNamed(t, list, "ops").Role; got != string(authz.RoleAdmin) {
		t.Errorf("Role = %q, want admin", got)
	}
}

// Groups an instance already had — carried on accounts, with no record of
// their own — are the ones an upgrade arrives with. They must be listed and
// maintainable, not invisible until somebody re-creates them by hand.
func TestGroupsAlreadyInUseAppearAndCanBeMaintained(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	if err := app.userStore().SetGroups("alice", []string{"shipping"}); err != nil {
		t.Fatal(err)
	}

	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	group := groupNamed(t, list, "shipping")
	if group.Declared {
		t.Error("a group that exists only because an account carries it is marked as declared")
	}
	if len(group.Members) != 1 || group.Members[0] != "alice" {
		t.Errorf("Members = %v, want alice", group.Members)
	}

	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups/shipping", admin,
		`{"description":"Warehouse floor"}`); w.Code != http.StatusOK {
		t.Fatalf("describing it returned %d: %s", w.Code, w.Body.String())
	}
	list = decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	group = groupNamed(t, list, "shipping")
	if group.Description != "Warehouse floor" || !group.Declared {
		t.Errorf("group = %+v, want it described and now declared", group)
	}
}

func TestGroupNamesAreRefusedRatherThanSilentlySplit(t *testing.T) {
	_, r, admin := groupsTestApp(t)
	w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments, shipping"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a name with a comma returned %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "comma") {
		t.Errorf("the refusal does not say why: %s", w.Body.String())
	}
}

// userNamed resolves an account for a test that needs the record rather than
// the wire view.
func userNamed(t *testing.T, app *App, name string) users.User {
	t.Helper()
	list, err := app.userStore().List()
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range list {
		if u.Username == name {
			return u
		}
	}
	t.Fatalf("no account named %q", name)
	return users.User{}
}
