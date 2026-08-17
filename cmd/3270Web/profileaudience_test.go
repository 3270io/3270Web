// SPDX-License-Identifier: AGPL-3.0-or-later

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

// Who a published host profile reaches.
//
// The rule these all turn on is that a profile naming nobody is for everybody:
// switching this on must not take a host list away from an instance that was
// already using one.

func account(name string, role authz.Role, groups ...string) users.User {
	return users.User{Username: name, Role: role, Groups: users.NormaliseGroups(groups)}
}

func TestAProfileWithNoAudienceIsForEveryone(t *testing.T) {
	p := ConnectionProfile{Name: "CICSPROD"}
	for _, who := range []users.User{
		account("alice", authz.RoleUser),
		account("bob", authz.RoleAdmin, "ops"),
		{}, // even an account with nothing on it
	} {
		if !p.visibleTo(who) {
			t.Errorf("%q could not see an unassigned profile", who.Username)
		}
	}
}

func TestAnAudienceIsAnOrAcrossUsersGroupsAndRoles(t *testing.T) {
	p := ConnectionProfile{
		Name:   "CICSPROD",
		Users:  []string{"dave"},
		Groups: []string{"payments"},
		Roles:  []string{"admin"},
	}

	for _, tc := range []struct {
		who  users.User
		want bool
		why  string
	}{
		{account("dave", authz.RoleUser), true, "named directly"},
		{account("DAVE", authz.RoleUser), true, "named, differently capitalised"},
		{account("erin", authz.RoleUser, "payments"), true, "in a named group"},
		{account("erin", authz.RoleUser, "PAYMENTS"), true, "group capitalised differently"},
		{account("root", authz.RoleAdmin), true, "holds a named role"},
		{account("frank", authz.RoleUser, "shipping"), false, "matches none of the three"},
		{account("", authz.RoleUser), false, "nobody in particular"},
	} {
		if got := p.visibleTo(tc.who); got != tc.want {
			t.Errorf("%s (%s): visibleTo = %v, want %v", tc.who.Username, tc.why, got, tc.want)
		}
	}
}

// A role that does not exist would make a profile look assigned and reach
// nobody — the failure nobody notices, because the person who cannot see the
// host does not know it is there.
func TestAnUnknownRoleIsDroppedRatherThanKept(t *testing.T) {
	p := ConnectionProfile{Roles: []string{"admin", "supervisor", "USER", ""}}
	normaliseAudience(&p)

	if len(p.Roles) != 2 || p.Roles[0] != "admin" || p.Roles[1] != "user" {
		t.Fatalf("Roles = %v, want only the roles that exist", p.Roles)
	}

	// And a profile left with no audience at all is for everyone again, rather
	// than for nobody.
	only := ConnectionProfile{Roles: []string{"supervisor"}}
	normaliseAudience(&only)
	if only.hasAudience() {
		t.Errorf("Roles = %v, want the audience cleared", only.Roles)
	}
	if !only.visibleTo(account("alice", authz.RoleUser)) {
		t.Error("a profile whose only audience was a typo reaches nobody")
	}
}

func TestAudienceListsAreCleanedLikeGroupNames(t *testing.T) {
	p := ConnectionProfile{
		Users:  []string{" alice ", "ALICE", "", "bob"},
		Groups: []string{"ops", "ops", " Ops "},
	}
	normaliseAudience(&p)

	if len(p.Users) != 2 || p.Users[0] != "alice" || p.Users[1] != "bob" {
		t.Errorf("Users = %v, want trimmed and de-duplicated", p.Users)
	}
	if len(p.Groups) != 1 || p.Groups[0] != "ops" {
		t.Errorf("Groups = %v, want one group", p.Groups)
	}
}

// audienceTestApp gives an instance with accounts, one shared profile per
// audience, and a signed-in caller.
func audienceTestApp(t *testing.T) (*App, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	app, r := newAuthTestApp(t, "local")
	addUser(t, app, "root", authz.RoleAdmin, false)
	addUser(t, app, "alice", authz.RoleUser, false)
	addUser(t, app, "bob", authz.RoleUser, false)
	if err := app.userStore().SetGroups("alice", []string{"payments"}); err != nil {
		t.Fatal(err)
	}
	return app, r
}

// publishProfiles writes profiles straight into the published store, as an
// administrator's save would.
func publishProfiles(t *testing.T, app *App, c *gin.Context, profiles ...ConnectionProfile) {
	t.Helper()
	store := app.publishedProfiles(c)
	if store == nil {
		t.Fatal("no published profile store")
	}
	for _, p := range profiles {
		if _, err := store.upsert(p); err != nil {
			t.Fatal(err)
		}
	}
}

// requestAs builds a request context carrying a signed-in principal, so the
// profile code can be driven without going through a route.
func requestAs(t *testing.T, app *App, username string) *gin.Context {
	t.Helper()
	list, err := app.userStore().List()
	if err != nil {
		t.Fatal(err)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	for _, u := range list {
		if u.Username == username {
			c.Set(principalContextKey, authz.Principal{UserID: u.ID, Role: u.Role, Kind: authz.KindWeb})
			return c
		}
	}
	t.Fatalf("no account named %q", username)
	return nil
}

func TestAssignedProfilesAreWhatTheAccountMayReach(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "OPEN", Host: "open.example", Port: 23},
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Groups: []string{"payments"}},
		ConnectionProfile{Name: "ADMINONLY", Host: "adm.example", Port: 23, Roles: []string{"admin"}},
		ConnectionProfile{Name: "BOBONLY", Host: "bob.example", Port: 23, Users: []string{"bob"}},
	)

	for username, want := range map[string][]string{
		"alice": {"OPEN", "PAYMENTS"},
		"bob":   {"BOBONLY", "OPEN"},
		"root":  {"ADMINONLY", "OPEN"},
	} {
		got, err := app.assignedProfiles(requestAs(t, app, username))
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, 0, len(got))
		for _, p := range got {
			names = append(names, p.Name)
		}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Errorf("%s reaches %v, want %v", username, names, want)
		}
	}
}

// The audience has to be a restriction, not a display filter: connecting by
// name is the path that would otherwise walk straight past it.
func TestAProfileYouWereNotGivenCannotBeConnectedToByName(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Groups: []string{"payments"}})

	if _, found := app.findVisibleProfile(requestAs(t, app, "alice"), "PAYMENTS"); !found {
		t.Error("a member of the group could not resolve the profile")
	}
	if _, found := app.findVisibleProfile(requestAs(t, app, "bob"), "PAYMENTS"); found {
		t.Error("an account outside the audience resolved the profile by name")
	}
}

// An administrator has to see the whole published set or they cannot edit the
// profile they are assigning.
func TestAnAdministratorSeesEveryPublishedProfile(t *testing.T) {
	app, r := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23, Groups: []string{"payments"}})

	for username, wantVisible := range map[string]bool{"root": true, "bob": false} {
		cookie := authCookieFrom(doLogin(t, r, username, loginTestPassword, "10.0.0.1"))
		if cookie == "" {
			t.Fatalf("%s could not sign in", username)
		}
		w := getWithAuth(r, "/api/profiles", cookie, "10.0.0.1")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: /api/profiles returned %d", username, w.Code)
		}
		var body struct {
			Profiles []ConnectionProfile `json:"profiles"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		var saw bool
		for _, p := range body.Profiles {
			if p.Name == "PAYMENTS" {
				saw = true
			}
		}
		if saw != wantVisible {
			t.Errorf("%s sees PAYMENTS = %v, want %v", username, saw, wantVisible)
		}
	}
}

// An audience on a profile somebody saved for themselves would be a list
// nothing reads, waiting to be applied by surprise if it were ever published.
func TestAPrivateSaveKeepsNoAudience(t *testing.T) {
	app, r := audienceTestApp(t)
	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))

	body := `{"name":"MINE","host":"mine.example","port":23,"groups":["payments"],"roles":["admin"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/")
	req.Host = "example.test"
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d (%s)", w.Code, w.Body.String())
	}

	saved, err := app.ownProfiles(requestAs(t, app, "alice")).load()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range saved {
		if p.Name == "MINE" && p.hasAudience() {
			t.Errorf("a private profile kept an audience: users=%v groups=%v roles=%v", p.Users, p.Groups, p.Roles)
		}
	}
}
