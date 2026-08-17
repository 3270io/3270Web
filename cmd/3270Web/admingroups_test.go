// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/host"
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

// Unticking a group's last host must not hand the mainframe to everybody.
//
// A published preset naming nobody is offered to everyone — the rule that
// stops turning audiences on from taking a host list away from a deployment
// that never had one. It also makes removal the dangerous direction: unticking
// the one box that restricted a preset reads as "take this away from them" and
// used to land as "give this to everyone", reported afterwards in a note
// nobody had asked a question of.
func TestAGroupCannotSilentlyMakeAPresetAvailableToEveryone(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	signIn(t, app, r, "bob", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23})

	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments","members":["alice"],"hosts":["PAYMENTS"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}
	if got := reachableBy(t, app, "bob"); len(got) != 0 {
		t.Fatalf("bob reaches %v before anything is removed, so the test proves nothing", got)
	}

	w := adminRequest(r, http.MethodPatch, "/api/admin/groups/payments", admin, `{"hosts":[]}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("removing the group's last host returned %d, want 409: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "everyone") || !strings.Contains(body, "PAYMENTS") {
		t.Errorf("the refusal does not say which preset would be offered to everyone: %s", body)
	}
	if !strings.Contains(body, "Session screen") {
		t.Errorf("the refusal does not say where Everyone can be chosen on purpose: %s", body)
	}

	// Nothing moved: the group still has the host, and the people who could and
	// could not reach it still can and cannot.
	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	group := groupNamed(t, list, "payments")
	if len(group.Hosts) != 1 || group.Hosts[0] != "PAYMENTS" {
		t.Errorf("Hosts = %v, want the assignment left as it was", group.Hosts)
	}
	if got := reachableBy(t, app, "alice"); len(got) != 1 || got[0] != "PAYMENTS" {
		t.Errorf("alice reaches %v after the refusal, want PAYMENTS", got)
	}
	if got := reachableBy(t, app, "bob"); len(got) != 0 {
		t.Errorf("bob reaches %v after the refusal, want nothing", got)
	}

	// The same removal is ordinary once something else names the preset, which
	// is the whole distinction: what is refused is the *implicit* opening up.
	if w := adminRequest(r, http.MethodPost, "/api/admin/profiles", admin,
		`{"name":"PAYMENTS","host":"pay.example","port":23,"users":["root"],"groups":["payments"]}`); w.Code != http.StatusOK {
		t.Fatalf("naming a user on the preset returned %d: %s", w.Code, w.Body.String())
	}
	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups/payments", admin,
		`{"hosts":[]}`); w.Code != http.StatusOK {
		t.Fatalf("removing the host with a user still named returned %d: %s", w.Code, w.Body.String())
	}
	if got := reachableBy(t, app, "bob"); len(got) != 0 {
		t.Errorf("bob reaches %v once the group was removed, want the preset still restricted", got)
	}
}

// The same trap by the other route: deleting the group a preset names.
func TestDeletingAGroupCannotSilentlyMakeAPresetAvailableToEveryone(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	signIn(t, app, r, "bob", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23},
		ConnectionProfile{Name: "SHIPPING", Host: "ship.example", Port: 23, Users: []string{"root"}})

	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments","members":["alice"],"hosts":["PAYMENTS","SHIPPING"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}

	w := adminRequest(r, http.MethodDelete, "/api/admin/groups/payments", admin, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("deleting a preset's only audience returned %d, want 409: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "PAYMENTS") || !strings.Contains(body, "everyone") {
		t.Errorf("the refusal does not name the preset that would open up: %s", body)
	}
	// SHIPPING would have survived the deletion, so it must not be named as a
	// casualty of it.
	if strings.Contains(w.Body.String(), "SHIPPING") {
		t.Errorf("the refusal names a preset that would have kept an audience: %s", w.Body.String())
	}

	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	group := groupNamed(t, list, "payments")
	if len(group.Members) != 1 || len(group.Hosts) != 2 {
		t.Errorf("group = %+v, want it untouched by the refusal", group)
	}
	if got := reachableBy(t, app, "bob"); len(got) != 0 {
		t.Errorf("bob reaches %v after the refusal, want nothing", got)
	}
	store := app.publishedProfiles(requestAs(t, app, "root"))
	if p, _ := store.find("PAYMENTS"); len(p.Groups) != 1 {
		t.Errorf("PAYMENTS names %v, want the group still on it", p.Groups)
	}

	// Give the preset an audience of its own and the deletion is ordinary.
	if w := adminRequest(r, http.MethodPost, "/api/admin/profiles", admin,
		`{"name":"PAYMENTS","host":"pay.example","port":23,"users":["root"],"groups":["payments"]}`); w.Code != http.StatusOK {
		t.Fatalf("naming a user on the preset returned %d: %s", w.Code, w.Body.String())
	}
	if w := adminRequest(r, http.MethodDelete, "/api/admin/groups/payments", admin, ""); w.Code != http.StatusOK {
		t.Fatalf("deleting the group returned %d: %s", w.Code, w.Body.String())
	}
	if got := reachableBy(t, app, "bob"); len(got) != 0 {
		t.Errorf("bob reaches %v after the deletion, want the presets still restricted", got)
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

// The hosts offered to a group can be the bundled sample apps. An instance
// being evaluated or taught on has those and no mainframe, and a host list
// assigned to a team is exactly as useful there.
func TestAGroupCanBeOfferedTheBundledSampleApps(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)

	// Published the way the preset dialog's sample-app picker publishes them.
	options := sampleAppPresetOptions()
	if len(options) < 2 {
		t.Fatal("expected at least two bundled sample apps to assign")
	}
	for _, option := range options {
		body := `{"name":"` + option.Name + `","host":"` + option.Host + `","port":` +
			strconv.Itoa(option.Port) + `,"users":["root"]}`
		if w := adminRequest(r, http.MethodPost, "/api/admin/profiles", admin, body); w.Code != http.StatusOK {
			t.Fatalf("publishing %s returned %d: %s", option.Name, w.Code, w.Body.String())
		}
	}

	// They show up in the group page's host picker, named the way they were
	// chosen rather than by the sampleapp: address they are dialled with.
	w := adminRequest(r, http.MethodGet, "/api/admin/groups", admin, "")
	var listing struct {
		Hosts []adminHostView `json:"hosts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Hosts) != len(options) {
		t.Fatalf("host picker offers %d entries, want %d", len(listing.Hosts), len(options))
	}
	for _, h := range listing.Hosts {
		if h.SampleApp == "" {
			t.Errorf("%q is a sample app but the picker shows %q", h.Name, h.Target)
		}
	}

	// Assign two of them to a group, and its member is offered exactly those.
	body := `{"name":"trainees","members":["alice"],"hosts":["` +
		options[0].Name + `","` + options[1].Name + `"]}`
	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin, body); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}

	got := reachableBy(t, app, "alice")
	want := []string{options[0].Name, options[1].Name}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("alice reaches %v, want %v", got, want)
	}

	// And each is a preset the session manager can actually connect: a
	// sample app is a listener this process starts, not an address to dial,
	// so the host built for it must be the sample-app host.
	profiles, err := app.assignedProfiles(requestAs(t, app, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		built, err := app.newHostFor(p.displayTarget(), &p)
		if err != nil {
			t.Fatalf("%s could not be built: %v", p.Name, err)
		}
		if _, ok := built.(*host.GoSampleAppHost); !ok {
			t.Errorf("%s builds a %T, so choosing it on the session manager would fail", p.Name, built)
		}
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

// A combined change is one change: all of it applies, or none of it does.
//
// The Save button sends the name, the description, the role, the membership
// and the host list together, and they land in two stores and a file. Applying
// them in order and checking each as it went meant a request could rename the
// group, then discover that the demotion it also carried would lock the
// administrator out of the page they were standing on, and answer "no" having
// already renamed it.
func TestARejectedCombinedUpdateDoesNotCommitItsRename(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	alice, _ := signIn(t, app, r, "alice", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Users: []string{"root"}})

	// alice administers this instance only because "ops" says so.
	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"ops","description":"Overnight cover","members":["alice"],"role":"admin","hosts":["PAYMENTS"]}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}
	if got := app.effectiveRoleFor(userNamed(t, app, "alice")); got != authz.RoleAdmin {
		t.Fatalf("alice's effective role is %q, so she cannot demonstrate the lockout", got)
	}

	// Each of these is a rename plus a change that would strand her.
	for _, body := range []string{
		`{"currentName":"ops","name":"renamed-ops","role":"user"}`,
		`{"currentName":"ops","name":"renamed-ops","members":[]}`,
	} {
		w := adminRequest(r, http.MethodPatch, "/api/admin/groups", alice, body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s returned %d, want 400: %s", body, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "administrator") {
			t.Errorf("the refusal does not say why: %s", w.Body.String())
		}
	}

	// Nothing moved: not the name, not the role, not the membership, not the
	// audience of the preset the group reaches.
	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if len(list) != 1 {
		t.Fatalf("groups = %+v, want only the original", list)
	}
	group := groupNamed(t, list, "ops")
	if group.Description != "Overnight cover" {
		t.Errorf("Description = %q, want it untouched", group.Description)
	}
	if group.Role != string(authz.RoleAdmin) {
		t.Errorf("Role = %q, want admin", group.Role)
	}
	if len(group.Members) != 1 || group.Members[0] != "alice" {
		t.Errorf("Members = %v, want alice still in it", group.Members)
	}
	if len(group.Hosts) != 1 || group.Hosts[0] != "PAYMENTS" {
		t.Errorf("Hosts = %v, want the assignment untouched", group.Hosts)
	}
	for _, g := range list {
		if strings.EqualFold(g.Name, "renamed-ops") {
			t.Fatal("the rejected rename was committed anyway")
		}
	}
	if got := app.effectiveRoleFor(userNamed(t, app, "alice")); got != authz.RoleAdmin {
		t.Errorf("alice's effective role is %q after the refusals, want admin", got)
	}
	store := app.publishedProfiles(requestAs(t, app, "root"))
	p, _ := store.find("PAYMENTS")
	if len(p.Groups) != 1 || !strings.EqualFold(p.Groups[0], "ops") {
		t.Errorf("PAYMENTS names %v, want the original spelling", p.Groups)
	}

	// And a rejection that comes from the host list behaves the same way: the
	// rename travelling with it must not survive it either.
	w := adminRequest(r, http.MethodPatch, "/api/admin/groups", admin,
		`{"currentName":"ops","name":"renamed-ops","hosts":["NO SUCH HOST"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("naming a preset that does not exist returned %d, want 400: %s", w.Code, w.Body.String())
	}
	list = decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if len(list) != 1 || !strings.EqualFold(list[0].Name, "ops") {
		t.Errorf("groups = %+v, want the group still called ops", list)
	}
}

// A group the directory feeds is the provider's to name.
//
// Renaming or deleting one here does not remove it: the claim is replayed at
// the member's next sign-in, which recreates the original spelling with the
// membership on it and leaves whatever was attached to the new name stranded
// beside it. Where the original granted administration, that is somebody's
// access on a group the page has stopped showing.
func TestProviderManagedGroupNameCannotBeChangedOrDeletedLocally(t *testing.T) {
	gin.SetMode(gin.TestMode)
	app, r := newSSOTestApp(t)
	app.setup.complete()
	admin, _ := signIn(t, app, r, "root", authz.RoleAdmin)
	if !app.groupsComeFromProvider() {
		t.Fatal("this instance does not take groups from the provider, so there is nothing to protect")
	}

	if _, err := app.userStore().UpsertExternal("https://idp.example", "sub-1", "alice",
		authz.RoleUser, []string{"ops"}); err != nil {
		t.Fatal(err)
	}
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Users: []string{"root"}})

	// The page says so, so the controls can be greyed out rather than offering
	// a change the server will refuse.
	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if !groupNamed(t, list, "ops").ProviderManaged {
		t.Error("the group is not marked as the identity provider's")
	}

	for _, body := range []string{
		`{"currentName":"ops","name":"operations"}`,
		// Capitals only is still a rename: the directory sends the original
		// spelling back either way.
		`{"currentName":"ops","name":"Ops"}`,
	} {
		w := adminRequest(r, http.MethodPatch, "/api/admin/groups", admin, body)
		if w.Code != http.StatusConflict {
			t.Fatalf("%s returned %d, want 409: %s", body, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "identity provider") {
			t.Errorf("the refusal does not say where to make the change: %s", w.Body.String())
		}
	}
	w := adminRequest(r, http.MethodDelete, "/api/admin/groups", admin, `{"currentName":"ops"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("deleting it returned %d, want 409: %s", w.Code, w.Body.String())
	}

	list = decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if len(list) != 1 || list[0].Name != "ops" {
		t.Fatalf("groups = %+v, want the original alone and unrenamed", list)
	}
	if len(list[0].Members) != 1 || list[0].Members[0] != "alice" {
		t.Errorf("Members = %v, want the directory's membership intact", list[0].Members)
	}

	// What is local stays local. A description, the role the group grants and
	// the presets it reaches are an administrator's, not the directory's.
	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups", admin,
		`{"currentName":"ops","description":"Overnight cover","role":"admin","hosts":["PAYMENTS"]}`); w.Code != http.StatusOK {
		t.Fatalf("changing what is local returned %d: %s", w.Code, w.Body.String())
	}
	list = decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	group := groupNamed(t, list, "ops")
	if group.Description != "Overnight cover" {
		t.Errorf("Description = %q, want the local note applied", group.Description)
	}
	if group.Role != string(authz.RoleAdmin) {
		t.Errorf("Role = %q, want admin", group.Role)
	}
	if len(group.Hosts) != 1 || group.Hosts[0] != "PAYMENTS" {
		t.Errorf("Hosts = %v, want the preset assigned", group.Hosts)
	}
	if !group.ProviderManaged {
		t.Error("the group stopped being marked as the provider's once it was described")
	}
}

// A group named by nothing but a published preset's audience.
//
// It decides who is offered that mainframe, so it is as real as any other, but
// it lives outside the account store as text in a profile — which is how it
// came to be the one kind of group that governs access and cannot be found on
// the page for finding groups.
func TestGroupsUsedOnlyByAProfileAppearAndCanBeMaintained(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "SHIPPING", Host: "ship.example", Port: 23, Groups: []string{"shipping"}})

	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	group := groupNamed(t, list, "shipping")
	if group.Declared {
		t.Error("a group that exists only because a preset names it is marked as declared")
	}
	if len(group.Hosts) != 1 || group.Hosts[0] != "SHIPPING" {
		t.Errorf("Hosts = %v, want the preset that names it", group.Hosts)
	}

	// And it is offered wherever a group can be chosen, so nobody types a
	// second spelling of a group that already governs a host.
	for _, path := range []string{"/api/admin/profiles", "/api/admin/users", "/api/profiles"} {
		w := adminRequest(r, http.MethodGet, path, admin, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, w.Code, w.Body.String())
		}
		var payload struct {
			Groups []string `json:"groups"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if !containsFold(payload.Groups, "shipping") {
			t.Errorf("%s offers %v, want shipping among the group choices", path, payload.Groups)
		}
	}

	// Editing it adopts it: it gets a record of its own and can then carry a
	// description, a role and members like any other.
	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups", admin,
		`{"currentName":"shipping","description":"Warehouse floor","members":["alice"]}`); w.Code != http.StatusOK {
		t.Fatalf("describing it returned %d: %s", w.Code, w.Body.String())
	}
	list = decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if len(list) != 1 {
		t.Fatalf("groups = %+v, want one group rather than the original beside an adopted copy", list)
	}
	group = groupNamed(t, list, "shipping")
	if !group.Declared || group.Description != "Warehouse floor" {
		t.Errorf("group = %+v, want it described and now declared", group)
	}
	if len(group.Hosts) != 1 || group.Hosts[0] != "SHIPPING" {
		t.Errorf("Hosts = %v, want the preset it already reached", group.Hosts)
	}
	if got := reachableBy(t, app, "alice"); len(got) != 1 || got[0] != "SHIPPING" {
		t.Errorf("alice reaches %v, want the host the adopted group offers", got)
	}

	// Renaming it now follows into the audience it came from.
	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups", admin,
		`{"currentName":"shipping","name":"logistics"}`); w.Code != http.StatusOK {
		t.Fatalf("renaming it returned %d: %s", w.Code, w.Body.String())
	}
	store := app.publishedProfiles(requestAs(t, app, "root"))
	p, _ := store.find("SHIPPING")
	if len(p.Groups) != 1 || p.Groups[0] != "logistics" {
		t.Errorf("SHIPPING names %v, want the rename to have followed", p.Groups)
	}
	if got := reachableBy(t, app, "alice"); len(got) != 1 || got[0] != "SHIPPING" {
		t.Errorf("alice reaches %v after the rename, want the host to have come with it", got)
	}
}

// A profile-only group cannot be deleted into making its preset everybody's
// either, and can be deleted once the preset names somebody else.
func TestDeletingAProfileOnlyGroupObeysTheSameExposureRule(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "bob", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "SHIPPING", Host: "ship.example", Port: 23, Groups: []string{"shipping"}},
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23,
			Users: []string{"root"}, Groups: []string{"shipping"}})

	if w := adminRequest(r, http.MethodDelete, "/api/admin/groups", admin,
		`{"currentName":"shipping"}`); w.Code != http.StatusConflict {
		t.Fatalf("deleting it returned %d, want 409: %s", w.Code, w.Body.String())
	}
	store := app.publishedProfiles(requestAs(t, app, "root"))
	if p, _ := store.find("SHIPPING"); len(p.Groups) != 1 {
		t.Errorf("SHIPPING names %v, want the refusal to have changed nothing", p.Groups)
	}
	if p, _ := store.find("PAYMENTS"); len(p.Groups) != 1 {
		t.Errorf("PAYMENTS names %v, want the refusal to have changed nothing", p.Groups)
	}

	// Name somebody on the one that would have opened up, and the deletion is
	// an ordinary tidy-up across both presets at once.
	if w := adminRequest(r, http.MethodPost, "/api/admin/profiles", admin,
		`{"name":"SHIPPING","host":"ship.example","port":23,"users":["root"],"groups":["shipping"]}`); w.Code != http.StatusOK {
		t.Fatalf("naming a user on the preset returned %d: %s", w.Code, w.Body.String())
	}
	if w := adminRequest(r, http.MethodDelete, "/api/admin/groups", admin,
		`{"currentName":"shipping"}`); w.Code != http.StatusOK {
		t.Fatalf("deleting it returned %d: %s", w.Code, w.Body.String())
	}
	for _, name := range []string{"SHIPPING", "PAYMENTS"} {
		if p, _ := store.find(name); len(p.Groups) != 0 {
			t.Errorf("%s still names %v", name, p.Groups)
		}
	}
	if got := reachableBy(t, app, "bob"); len(got) != 0 {
		t.Errorf("bob reaches %v, want both presets still restricted to root", got)
	}
	if list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, "")); len(list) != 0 {
		t.Errorf("groups = %+v, want none", list)
	}
}

// A group name may contain "/". "payments/shipping" is an ordinary way to
// write a sub-team, and no amount of encoding makes it survive as a single
// router path parameter — so it could be created and then never edited or
// deleted again.
func TestAGroupNameWithAPathSeparatorCanStillBeAddressed(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "alice", authz.RoleUser)
	publishProfiles(t, app, requestAs(t, app, "root"),
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Users: []string{"root"}})

	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments/shipping","members":["alice"],"hosts":["PAYMENTS"]}`); w.Code != http.StatusCreated {
		t.Fatalf("creating it returned %d: %s", w.Code, w.Body.String())
	}
	// A second group, so an update that hit the wrong one would show up.
	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments"}`); w.Code != http.StatusCreated {
		t.Fatalf("creating the neighbouring group returned %d: %s", w.Code, w.Body.String())
	}

	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups", admin,
		`{"currentName":"payments/shipping","description":"Both teams"}`); w.Code != http.StatusOK {
		t.Fatalf("updating it returned %d: %s", w.Code, w.Body.String())
	}
	list := decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if got := groupNamed(t, list, "payments/shipping").Description; got != "Both teams" {
		t.Errorf("Description = %q, want the update to have landed on the right group", got)
	}
	if got := groupNamed(t, list, "payments").Description; got != "" {
		t.Errorf("the neighbouring group was described instead: %q", got)
	}

	if w := adminRequest(r, http.MethodDelete, "/api/admin/groups", admin,
		`{"currentName":"payments/shipping"}`); w.Code != http.StatusOK {
		t.Fatalf("deleting it returned %d: %s", w.Code, w.Body.String())
	}
	list = decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if len(list) != 1 || list[0].Name != "payments" {
		t.Fatalf("groups = %+v, want only the neighbour left", list)
	}

	// The path routes are the published API and go on working for every name
	// that can be written into one.
	if w := adminRequest(r, http.MethodPatch, "/api/admin/groups/payments", admin,
		`{"description":"Overnight batch"}`); w.Code != http.StatusOK {
		t.Fatalf("the path route returned %d: %s", w.Code, w.Body.String())
	}
	list = decodeGroups(t, adminRequest(r, http.MethodGet, "/api/admin/groups", admin, ""))
	if got := groupNamed(t, list, "payments").Description; got != "Overnight batch" {
		t.Errorf("Description = %q, want the legacy path route still to work", got)
	}
	if w := adminRequest(r, http.MethodDelete, "/api/admin/groups/payments", admin, ""); w.Code != http.StatusOK {
		t.Fatalf("the path delete route returned %d: %s", w.Code, w.Body.String())
	}
}

// Naming no group at all is a request that cannot be honoured, not a 404 for
// a group called "".
func TestACollectionGroupRequestWithNoTargetIsRefused(t *testing.T) {
	app, r, admin := groupsTestApp(t)
	signIn(t, app, r, "bob", authz.RoleUser)

	for _, call := range []struct{ method, body string }{
		{http.MethodPatch, `{"description":"nothing to describe"}`},
		{http.MethodDelete, `{}`},
	} {
		w := adminRequest(r, call.method, "/api/admin/groups", admin, call.body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s with no target returned %d, want 400: %s", call.method, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "currentName") {
			t.Errorf("%s does not say how to name the group: %s", call.method, w.Body.String())
		}
	}

	// And the new routes are administration, like the ones they sit beside.
	bob := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.1"))
	for _, call := range []struct{ method, body string }{
		{http.MethodPatch, `{"currentName":"payments","role":"admin"}`},
		{http.MethodDelete, `{"currentName":"payments"}`},
	} {
		w := adminRequest(r, call.method, "/api/admin/groups", bob, call.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("a regular account reached %s /api/admin/groups: %d", call.method, w.Code)
		}
	}
	_ = app
}

// Naming an account or a preset that is not there is a mistake worth saying
// out loud, rather than one to be discovered by reading the row back.
func TestAGroupChangeNamingSomethingThatDoesNotExistIsRefused(t *testing.T) {
	_, r, admin := groupsTestApp(t)
	if w := adminRequest(r, http.MethodPost, "/api/admin/groups", admin,
		`{"name":"payments"}`); w.Code != http.StatusCreated {
		t.Fatalf("create returned %d: %s", w.Code, w.Body.String())
	}

	w := adminRequest(r, http.MethodPatch, "/api/admin/groups", admin,
		`{"currentName":"payments","members":["nobody"]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nobody") {
		t.Errorf("naming an account that does not exist returned %d: %s", w.Code, w.Body.String())
	}
	w = adminRequest(r, http.MethodPatch, "/api/admin/groups", admin,
		`{"currentName":"payments","hosts":["NOWHERE"]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "NOWHERE") {
		t.Errorf("naming a preset that does not exist returned %d: %s", w.Code, w.Body.String())
	}
	w = adminRequest(r, http.MethodPatch, "/api/admin/groups", admin,
		`{"currentName":"no-such-group","description":"x"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("changing a group that does not exist returned %d, want 404: %s", w.Code, w.Body.String())
	}
}
