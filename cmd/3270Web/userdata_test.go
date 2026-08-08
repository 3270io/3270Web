package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/session"
)

// With one operator the layout is left exactly as it was. Introducing a
// users/ level for a deployment that cannot have a second user would be churn
// with nothing to show for it, and would need a migration nobody asked for.
func TestDataScopeIsFlatForASingleOperator(t *testing.T) {
	base := t.TempDir()
	for _, mode := range []authz.Mode{"", authz.ModeNone} {
		app := &App{baseDir: base, authMode: mode}
		for _, owner := range []string{"", authz.LocalUserID, "some-account-id"} {
			got := app.dataScopeFor(owner).chaosRunsDir()
			want := filepath.Join(base, "chaos-runs")
			if got != want {
				t.Errorf("mode %q owner %q: runs dir = %q, want %q", mode, owner, got, want)
			}
		}
	}
}

func TestDataScopeSeparatesUsers(t *testing.T) {
	base := t.TempDir()
	app := &App{baseDir: base, authMode: authz.ModeLocal}

	alice := app.dataScopeFor("aaaa1111")
	bob := app.dataScopeFor("bbbb2222")

	if alice.chaosRunsDir() == bob.chaosRunsDir() {
		t.Fatal("two accounts share a runs directory")
	}
	for _, p := range []string{alice.chaosRunsDir(), alice.chaosHintsPath()} {
		if !strings.HasPrefix(p, filepath.Join(base, userDataDirName, "aaaa1111")) {
			t.Errorf("%q is not inside alice's directory", p)
		}
	}
	// The reserved single-operator name must not become a directory that a
	// real account could also be routed to.
	if got := app.dataScopeFor(authz.LocalUserID).chaosRunsDir(); got != filepath.Join(base, "chaos-runs") {
		t.Errorf("the local operator resolved to %q, want the flat directory", got)
	}
}

// The owner ID becomes a directory name, so this is the one place a bad value
// would put one account's files inside another's tree.
func TestOwnerIDsThatCannotBecomeDirectories(t *testing.T) {
	for _, ok := range []string{"abc123", "AAA-bbb_999", strings.Repeat("a", 64)} {
		if !isSafeOwnerID(ok) {
			t.Errorf("isSafeOwnerID(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{
		"", "..", "../escape", "a/b", "a\\b", "a.b", "a b", "café",
		"a\x00b", strings.Repeat("a", 65),
	} {
		if isSafeOwnerID(bad) {
			t.Errorf("isSafeOwnerID(%q) = true, want false", bad)
		}
	}

	// A rejected ID must not fall back to the shared flat directory, which
	// would put it where everybody else's files are.
	base := t.TempDir()
	app := &App{baseDir: base, authMode: authz.ModeLocal}
	got := app.dataScopeFor("../../etc").chaosRunsDir()
	if !strings.HasPrefix(got, filepath.Join(base, userDataDirName)) {
		t.Errorf("an invalid owner resolved to %q, outside the users directory", got)
	}
	if got == filepath.Join(base, "chaos-runs") {
		t.Error("an invalid owner resolved to the flat directory")
	}
}

// A chaos run keeps writing after the request that started it has returned,
// from a goroutine that holds the session and nothing else — so the path has
// to come from the session's owner, not from a request.
func TestChaosPathsFollowTheSessionOwner(t *testing.T) {
	base := t.TempDir()
	app := &App{baseDir: base, authMode: authz.ModeLocal}

	alice := app.chaosRunsDirForSession("aaaa1111")
	bob := app.chaosRunsDirForSession("bbbb2222")
	if alice == bob {
		t.Fatal("two sessions with different owners share a runs directory")
	}

	cfg := chaosConfigWithPaths("run.json", "log.jsonl")
	app.confineChaosPaths(&cfg, "", "aaaa1111")
	for label, got := range map[string]string{
		"OutputFile":        cfg.OutputFile,
		"TransitionLogPath": cfg.TransitionLogPath,
	} {
		if filepath.Dir(got) != filepath.Clean(alice) {
			t.Errorf("%s = %q, want it inside %q", label, got, alice)
		}
	}
}

// Turning authentication on must not appear to lose an existing instance's
// work. The files are still there; they simply need an owner.
func TestMigrationMovesFlatDataToTheFirstAdmin(t *testing.T) {
	base := t.TempDir()
	app := &App{baseDir: base, authMode: authz.ModeLocal}

	runsDir := filepath.Join(base, "chaos-runs")
	if err := os.MkdirAll(runsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runsDir, "old-run.json"), []byte(`{"id":"old-run"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "chaos-hints.json"), []byte(`{"hints":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	const owner = "aaaa1111"
	if err := app.migrateFlatDataToOwner(owner); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	scope := app.dataScopeFor(owner)
	if _, err := os.Stat(filepath.Join(scope.chaosRunsDir(), "old-run.json")); err != nil {
		t.Errorf("the saved run did not arrive in the owner's directory: %v", err)
	}
	if _, err := os.Stat(scope.chaosHintsPath()); err != nil {
		t.Errorf("the hints file did not arrive: %v", err)
	}
	if _, err := os.Stat(runsDir); !os.IsNotExist(err) {
		t.Error("the flat runs directory is still there; the files were copied, not moved")
	}
}

// Running twice must not scoop up files a user has created since.
func TestMigrationRunsOnce(t *testing.T) {
	base := t.TempDir()
	app := &App{baseDir: base, authMode: authz.ModeLocal}
	const owner = "aaaa1111"

	if err := os.WriteFile(filepath.Join(base, "chaos-hints.json"), []byte(`{"first":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.migrateFlatDataToOwner(owner); err != nil {
		t.Fatal(err)
	}

	// Something new appears in the flat layout afterwards.
	if err := os.WriteFile(filepath.Join(base, "chaos-hints.json"), []byte(`{"second":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.migrateFlatDataToOwner(owner); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(app.dataScopeFor(owner).chaosHintsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "first") {
		t.Errorf("the second run overwrote the migrated file: %s", got)
	}
	if _, err := os.Stat(filepath.Join(base, "chaos-hints.json")); err != nil {
		t.Error("the second run moved a file it should have left alone")
	}
}

func TestMigrationIsANoOpForASingleOperator(t *testing.T) {
	base := t.TempDir()
	app := &App{baseDir: base, authMode: authz.ModeNone}
	if err := os.WriteFile(filepath.Join(base, "chaos-hints.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.migrateFlatDataToOwner("aaaa1111"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "chaos-hints.json")); err != nil {
		t.Error("a single-operator instance had its files moved")
	}
}

// The end-to-end property: two accounts running chaos cannot read each
// other's captured screens.
func TestChaosRunsAreNotSharedBetweenAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := t.TempDir()
	app := &App{
		SessionManager: session.NewManager(),
		chaosEngines:   newChaosEngineStore(),
		baseDir:        base,
		authMode:       authz.ModeLocal,
	}

	aliceDir := app.chaosRunsDirForSession("aaaa1111")
	if err := os.MkdirAll(aliceDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aliceDir, "alice-run.json"), []byte(`{"id":"alice-run"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	bobDir := app.chaosRunsDirForSession("bbbb2222")
	if _, err := os.Stat(filepath.Join(bobDir, "alice-run.json")); !os.IsNotExist(err) {
		t.Error("alice's run is visible in bob's directory")
	}
	if strings.HasPrefix(aliceDir, bobDir) || strings.HasPrefix(bobDir, aliceDir) {
		t.Errorf("one account's directory contains the other: %q vs %q", aliceDir, bobDir)
	}
}

// chaosConfigWithPaths builds the minimal config the path-confinement code
// looks at.
func chaosConfigWithPaths(output, transitionLog string) chaos.Config {
	return chaos.Config{OutputFile: output, TransitionLogPath: transitionLog}
}
