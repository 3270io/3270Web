// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/session"
	"github.com/jnnngs/3270Web/internal/task"
)

// newSharingApp builds an app that separates users, with a router that lets a
// test act as a named principal.
func newSharingApp(t *testing.T) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return &App{
		SessionManager: session.NewManager(),
		baseDir:        t.TempDir(),
		authMode:       authz.ModeLocal,
	}
}

func asPrincipal(p authz.Principal) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set(principalContextKey, p)
	return c
}

func userPrincipal(id string) authz.Principal {
	return authz.Principal{UserID: id, Role: authz.RoleUser, Kind: authz.KindWeb}
}

func adminPrincipal(id string) authz.Principal {
	return authz.Principal{UserID: id, Role: authz.RoleAdmin, Kind: authz.KindWeb}
}

func TestTaskCataloguesArePerAccount(t *testing.T) {
	app := newSharingApp(t)

	alice := app.taskStoreForRequest(asPrincipal(userPrincipal("aaaa1111")))
	bob := app.taskStoreForRequest(asPrincipal(userPrincipal("bbbb2222")))
	if alice == bob {
		t.Fatal("two accounts were handed the same task store")
	}

	if _, err := alice.Upsert(newNamedTask("Alice's balance enquiry")); err != nil {
		t.Fatal(err)
	}

	bobTasks, err := bob.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range bobTasks {
		if strings.Contains(task.Name, "Alice") {
			t.Errorf("bob can see alice's task %q", task.Name)
		}
	}

	// The same account asking twice must get the same store, or a lazily
	// created one would be rebuilt per request.
	if again := app.taskStoreForRequest(asPrincipal(userPrincipal("aaaa1111"))); again != alice {
		t.Error("the same account was handed two different stores")
	}
}

// Profiles are shared infrastructure — everyone connects to the same
// mainframes — so a published host list has to remain visible to everyone.
func TestProfilesMergePublishedAndOwn(t *testing.T) {
	app := newSharingApp(t)
	adminCtx := asPrincipal(adminPrincipal("admin111"))
	userCtx := asPrincipal(userPrincipal("bbbb2222"))

	if _, err := app.publishedProfiles(adminCtx).upsert(ConnectionProfile{
		Name: "Production", Host: "prod.example.test", Port: 23,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ownProfiles(userCtx).upsert(ConnectionProfile{
		Name: "My sandbox", Host: "sandbox.example.test", Port: 23,
	}); err != nil {
		t.Fatal(err)
	}

	visible, err := app.visibleProfiles(userCtx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ConnectionProfile{}
	for _, p := range visible {
		byName[p.Name] = p
	}
	if len(visible) != 2 {
		t.Fatalf("saw %d profiles, want 2: %+v", len(visible), visible)
	}
	if !byName["Production"].Shared {
		t.Error("the published profile is not marked shared")
	}
	if byName["My sandbox"].Shared {
		t.Error("the account's own profile is marked shared")
	}

	// Another account sees the published one but not somebody else's own.
	other, err := app.visibleProfiles(asPrincipal(userPrincipal("cccc3333")))
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].Name != "Production" {
		t.Errorf("a third account saw %+v, want only the published profile", other)
	}
}

// Editing a published profile gives the caller their own copy rather than
// changing it for everybody — the same shape as overriding a setting.
func TestOwnProfileOverridesPublishedByName(t *testing.T) {
	app := newSharingApp(t)
	adminCtx := asPrincipal(adminPrincipal("admin111"))
	userCtx := asPrincipal(userPrincipal("bbbb2222"))

	if _, err := app.publishedProfiles(adminCtx).upsert(ConnectionProfile{
		Name: "Production", Host: "prod.example.test", Port: 23,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ownProfiles(userCtx).upsert(ConnectionProfile{
		Name: "Production", Host: "my-own-prod.example.test", Port: 992,
	}); err != nil {
		t.Fatal(err)
	}

	visible, err := app.visibleProfiles(userCtx)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 {
		t.Fatalf("saw %d profiles, want the override to replace not duplicate: %+v", len(visible), visible)
	}
	if visible[0].Host != "my-own-prod.example.test" || visible[0].Shared {
		t.Errorf("override = %+v, want the account's own to win", visible[0])
	}

	// The published one is untouched for everybody else.
	other, err := app.visibleProfiles(asPrincipal(userPrincipal("cccc3333")))
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].Host != "prod.example.test" {
		t.Errorf("the published profile was changed for others: %+v", other)
	}
}

// Publishing changes what every account sees, so it is an administrator's
// call.
func TestOnlyAdminsPublish(t *testing.T) {
	app := newSharingApp(t)

	if _, err := app.profileStoreForWrite(asPrincipal(userPrincipal("bbbb2222")), true); err == nil {
		t.Error("a regular user was allowed to publish a profile")
	}
	store, err := app.profileStoreForWrite(asPrincipal(adminPrincipal("admin111")), true)
	if err != nil {
		t.Fatalf("an administrator could not publish: %v", err)
	}
	if store != app.publishedProfiles(asPrincipal(adminPrincipal("admin111"))) {
		t.Error("publishing did not resolve to the shared store")
	}

	// Without the flag, even an administrator writes to their own set —
	// saving must not quietly change what everybody sees.
	own, err := app.profileStoreForWrite(asPrincipal(adminPrincipal("admin111")), false)
	if err != nil {
		t.Fatal(err)
	}
	if own == app.publishedProfiles(asPrincipal(adminPrincipal("admin111"))) {
		t.Error("an ordinary save went to the published set")
	}
}

// With one operator there is no second place to put anything, and publishing
// must not become an error for a deployment that has no concept of it.
func TestPublishingIsInertForASingleOperator(t *testing.T) {
	app := &App{baseDir: t.TempDir(), authMode: authz.ModeNone}
	ctx := asPrincipal(authz.Local())

	if app.publishedProfiles(ctx) != nil {
		t.Error("a single-operator instance has a published set")
	}
	store, err := app.profileStoreForWrite(ctx, true)
	if err != nil {
		t.Fatalf("publishing errored for a single operator: %v", err)
	}
	if store != app.ownProfiles(ctx) {
		t.Error("publishing did not fall back to the only store there is")
	}
}

func TestThemesMergePublishedAndOwn(t *testing.T) {
	app := newSharingApp(t)
	adminCtx := asPrincipal(adminPrincipal("admin111"))
	userCtx := asPrincipal(userPrincipal("bbbb2222"))

	writeTheme(t, app.sharedThemesDirFor(adminCtx), "house-style", "House style")
	writeTheme(t, app.themesDirFor(userCtx), "my-theme", "My theme")

	items, err := app.listFileThemes(userCtx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, item := range items {
		names[item.Name] = true
	}
	if !names["House style"] || !names["My theme"] {
		t.Errorf("themes = %+v, want both the published and the account's own", items)
	}

	// A third account sees the published theme and not somebody else's own.
	other, err := app.listFileThemes(asPrincipal(userPrincipal("cccc3333")))
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].Name != "House style" {
		t.Errorf("a third account saw %+v, want only the published theme", other)
	}
}

func TestThemesAndProfilesAreFlatForASingleOperator(t *testing.T) {
	base := t.TempDir()
	app := &App{baseDir: base, authMode: authz.ModeNone}
	ctx := asPrincipal(authz.Local())

	if got, want := app.themesDirFor(ctx), filepath.Join(base, "themes"); got != want {
		t.Errorf("themes dir = %q, want %q", got, want)
	}
	if got := app.sharedThemesDirFor(ctx); got != "" {
		t.Errorf("shared themes dir = %q, want empty", got)
	}
}

// The migration has to leave a team's host list where everybody can still see
// it. Moving it into one person's directory would take it away from everyone
// else on an instance that had been working fine.
func TestMigrationPublishesProfilesAndThemes(t *testing.T) {
	base := t.TempDir()
	app := &App{baseDir: base, authMode: authz.ModeLocal}

	if err := os.WriteFile(filepath.Join(base, profilesFileName),
		[]byte(`[{"name":"Production","host":"prod.example.test","port":23}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTheme(t, filepath.Join(base, "themes"), "house", "House style")
	if err := os.WriteFile(filepath.Join(base, "tasks.json"), []byte(`{"tasks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	const owner = "admin111"
	if err := app.migrateFlatDataToOwner(owner); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	scope := app.dataScopeFor(owner)
	// Shared infrastructure stays shared.
	if _, err := os.Stat(scope.sharedProfilesPath()); err != nil {
		t.Errorf("profiles were not published: %v", err)
	}
	if _, err := os.Stat(filepath.Join(scope.sharedThemesDir(), "house.json")); err != nil {
		t.Errorf("themes were not published: %v", err)
	}
	// Working documents go to the first administrator.
	if _, err := os.Stat(scope.tasksPath()); err != nil {
		t.Errorf("the task catalogue did not go to the administrator: %v", err)
	}

	// And a second account can still see the host list afterwards, which is
	// the whole point of publishing them rather than assigning them.
	visible, err := app.visibleProfiles(asPrincipal(userPrincipal("bbbb2222")))
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].Name != "Production" || !visible[0].Shared {
		t.Errorf("a second account saw %+v, want the published Production profile", visible)
	}
}

func writeTheme(t *testing.T, dir, stem, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"schema":      "3270web.custom-theme.v1",
		"name":        name,
		"theme":       "theme-custom",
		"customTheme": map[string]string{"--bg": "#000000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, stem+".json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newNamedTask(name string) task.Task {
	return task.Task{
		Name:   name,
		Source: "recording",
		Steps:  []task.Step{{Description: "press enter", AIDKey: "Enter"}},
	}
}
