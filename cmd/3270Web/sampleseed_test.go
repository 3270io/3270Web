package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// decodeAdminPresets reads the presets page's listing.
func decodeAdminPresets(t *testing.T, w *httptest.ResponseRecorder) []ConnectionProfile {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("/api/admin/profiles returned %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Profiles []ConnectionProfile `json:"profiles"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Profiles
}

// The bundled sample apps arriving as presets nobody has been given yet.
//
// The whole point of the state is that seeding changes what an administrator
// sees and nothing else. An instance that was auto-connecting one mainframe
// must still auto-connect it, not meet a selection screen with nine entries
// on it.

func seedInto(t *testing.T, app *App, username string) int {
	t.Helper()
	return app.seedSampleAppPresets(requestAs(t, app, username))
}

func presetNamed(profiles []ConnectionProfile, name string) (ConnectionProfile, bool) {
	for _, p := range profiles {
		if p.Name == name {
			return p, true
		}
	}
	return ConnectionProfile{}, false
}

func TestSeedingAddsEveryBundledSampleAppNotOffered(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")

	added := seedInto(t, app, "root")
	options := sampleAppPresetOptions()
	if added != len(options) {
		t.Fatalf("seeded %d presets, want %d", added, len(options))
	}

	stored, err := app.publishedProfiles(admin).load()
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range options {
		p, ok := presetNamed(stored, option.Name)
		if !ok {
			t.Errorf("%s was not seeded", option.Name)
			continue
		}
		if p.Host != option.Host || p.Port != option.Port {
			t.Errorf("%s seeded as %s:%d, want %s:%d", option.Name, p.Host, p.Port, option.Host, option.Port)
		}
		if !p.NotOffered || p.offered() {
			t.Errorf("%s was seeded already offered", option.Name)
		}
	}
}

// The one thing seeding must not do.
func TestASeededPresetIsOnNobodysHostList(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23})
	seedInto(t, app, "root")

	// Including the administrator's: this is the host list, not the
	// administration of it, and a preset shown in a picker that then refuses
	// to connect to it is worse than one that is not shown.
	for _, username := range []string{"root", "alice", "bob"} {
		assigned, err := app.assignedProfiles(requestAs(t, app, username))
		if err != nil {
			t.Fatal(err)
		}
		if len(assigned) != 1 || assigned[0].Name != "PAYMENTS" {
			t.Errorf("%s is offered %d host(s), want only PAYMENTS: %+v", username, len(assigned), assigned)
		}
		visible, err := app.visibleProfiles(requestAs(t, app, username))
		if err != nil {
			t.Fatal(err)
		}
		if len(visible) != 1 {
			t.Errorf("%s can see %d host(s) on the connect page, want only PAYMENTS", username, len(visible))
		}
	}

	// And connecting to one by name gets the same answer as naming a preset
	// that does not exist.
	name := sampleAppPresetOptions()[0].Name
	if _, found := app.findVisibleProfile(admin, name); found {
		t.Errorf("an administrator connected by name to %q, which is offered to nobody", name)
	}
}

// The administrator has to see them, or there is nothing to offer.
func TestTheAdministratorSeesTheSeededPresets(t *testing.T) {
	_, r := audienceTestApp(t)
	admin := authCookieFrom(doLogin(t, r, "root", loginTestPassword, "10.0.0.1"))
	if admin == "" {
		t.Fatal("root could not sign in")
	}

	// The presets page seeds on its own, so opening it is enough.
	listing := decodeAdminPresets(t, adminRequest(r, "GET", "/api/admin/profiles", admin, ""))
	for _, option := range sampleAppPresetOptions() {
		p, ok := presetNamed(listing, option.Name)
		if !ok {
			t.Fatalf("%s is missing from the presets page", option.Name)
		}
		if !p.NotOffered {
			t.Errorf("%s is on the presets page already offered", option.Name)
		}
	}
}

// Naming an audience is how a preset gets onto a selection screen, and it has
// to work without anybody clearing a flag they were never shown. Otherwise a
// preset assigned to a group reaches nobody — the failure nobody notices.
func TestNamingAnAudienceOffersAWithheldPreset(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	seedInto(t, app, "root")

	option := sampleAppPresetOptions()[0]
	store := app.publishedProfiles(admin)
	stored, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	preset, ok := presetNamed(stored, option.Name)
	if !ok {
		t.Fatal("the preset was not seeded")
	}
	// Exactly what the groups page does: it sets the audience and knows
	// nothing about whether the preset was being withheld.
	preset.Groups = []string{"payments"}
	if _, err := store.upsert(preset); err != nil {
		t.Fatal(err)
	}

	assigned, err := app.assignedProfiles(requestAs(t, app, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := presetNamed(assigned, option.Name); !ok {
		t.Errorf("alice is in payments but was not offered %s", option.Name)
	}
	if _, ok := presetNamed(mustAssigned(t, app, "bob"), option.Name); ok {
		t.Errorf("bob is outside payments but was offered %s", option.Name)
	}
}

func mustAssigned(t *testing.T, app *App, username string) []ConnectionProfile {
	t.Helper()
	list, err := app.assignedProfiles(requestAs(t, app, username))
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// A preset removed by an administrator is a decision, and seeding must not
// argue with it on the next page load.
func TestASeededPresetStaysDeletedOnceRemoved(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	seedInto(t, app, "root")

	option := sampleAppPresetOptions()[0]
	store := app.publishedProfiles(admin)
	if _, err := store.delete(option.Name); err != nil {
		t.Fatal(err)
	}

	if added := seedInto(t, app, "root"); added != 0 {
		t.Fatalf("seeding ran again and added %d preset(s)", added)
	}
	stored, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := presetNamed(stored, option.Name); ok {
		t.Errorf("%s came back after being removed", option.Name)
	}
}

// The store upserts by name, so seeding onto a name somebody else is using
// would replace a real mainframe with a sample app.
func TestSeedingNeverReplacesAPresetThatAlreadyHasTheName(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	option := sampleAppPresetOptions()[0]
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: option.Name, Host: "pay.example", Port: 23})

	seedInto(t, app, "root")

	stored, err := app.publishedProfiles(admin).load()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := presetNamed(stored, option.Name)
	if !ok {
		t.Fatal("the existing preset disappeared")
	}
	if p.Host != "pay.example" {
		t.Errorf("the existing preset now points at %q — seeding overwrote it", p.Host)
	}
	// And it is not retried on the next visit, so the log does not fill up.
	if added := seedInto(t, app, "root"); added != 0 {
		t.Errorf("seeding retried the collision and added %d preset(s)", added)
	}
}

// Every preset written before this state existed has to stay exactly as
// offered as it was: the zero value is "offered", and nothing reads a field
// an older file does not carry.
func TestAProfileFileWithoutTheFieldStaysOffered(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, profilesFileName),
		[]byte(`[{"name":"CICSPROD","host":"cics.example","port":23}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newConnectionProfileStore(dir)
	profiles, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("loaded %d profiles, want 1", len(profiles))
	}
	if profiles[0].NotOffered || !profiles[0].offered() {
		t.Error("a profile written before this field existed is no longer offered")
	}
	if len(offeredOnly(profiles)) != 1 {
		t.Error("offeredOnly dropped a profile that carries no such field")
	}
}

func TestOfferedAgreesWithTheAudience(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile ConnectionProfile
		want    bool
	}{
		{"a plain preset", ConnectionProfile{Name: "A"}, true},
		{"withheld", ConnectionProfile{Name: "A", NotOffered: true}, false},
		{"withheld but named to a group", ConnectionProfile{Name: "A", NotOffered: true, Groups: []string{"ops"}}, true},
		{"withheld but named to a user", ConnectionProfile{Name: "A", NotOffered: true, Users: []string{"dave"}}, true},
		{"withheld but named to a role", ConnectionProfile{Name: "A", NotOffered: true, Roles: []string{"admin"}}, true},
	} {
		if got := tc.profile.offered(); got != tc.want {
			t.Errorf("%s: offered() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A save that names an audience settles the record, so the presets page shows
// back what was actually stored rather than a flag nothing acts on.
func TestSavingWithAnAudienceClearsTheWithheldFlag(t *testing.T) {
	p := ConnectionProfile{Name: "A", NotOffered: true, Groups: []string{"ops"}}
	settleOffered(&p)
	if p.NotOffered {
		t.Error("a preset saved with an audience is still marked as not offered")
	}

	withheld := ConnectionProfile{Name: "A", NotOffered: true}
	settleOffered(&withheld)
	if !withheld.NotOffered {
		t.Error("a preset with no audience lost its withheld state")
	}
}

// A profile somebody saved for themselves has nobody else to be offered to,
// so the flag would only hide it from the one list that shows it.
func TestAPrivateSaveCannotBeWithheld(t *testing.T) {
	app, r := audienceTestApp(t)
	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))

	body := `{"name":"MINE","host":"mine.example","port":23,"notOffered":true}`
	if w := adminRequest(r, http.MethodPost, "/api/profiles", cookie, body); w.Code != http.StatusOK {
		t.Fatalf("save returned %d: %s", w.Code, w.Body.String())
	}

	saved, err := app.ownProfiles(requestAs(t, app, "alice")).load()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := presetNamed(saved, "MINE")
	if !ok {
		t.Fatal("the profile was not saved")
	}
	if p.NotOffered {
		t.Error("a profile saved for one account was stored as not offered")
	}
	if _, ok := presetNamed(mustAssigned(t, app, "alice"), "MINE"); !ok {
		t.Error("alice cannot see the profile she just saved")
	}
}
