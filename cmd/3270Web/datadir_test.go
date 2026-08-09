package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Unset means "beside the program", which is what a desktop install and
// `go run` both have, and changing that would move every existing
// installation's accounts out from under it.
func TestDataDirDefaultsToTheProgramDirectory(t *testing.T) {
	t.Setenv("DATA_DIR", "")
	install := t.TempDir()
	if got := resolveDataDir(install); got != install {
		t.Errorf("resolveDataDir = %q, want %q", got, install)
	}
}

func TestDataDirIsCreatedIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not", "there", "yet")
	t.Setenv("DATA_DIR", dir)

	if got := resolveDataDir(t.TempDir()); got != dir {
		t.Fatalf("resolveDataDir = %q, want %q", got, dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the data directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("the data directory is not a directory")
	}
}

// An unusable DATA_DIR must not take the server down: falling back and saying
// so beats refusing to start over a setting most deployments never touch.
func TestAnUnusableDataDirFallsBack(t *testing.T) {
	install := t.TempDir()
	// A file where the directory should be.
	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", blocked)

	if got := resolveDataDir(install); got != install {
		t.Errorf("resolveDataDir = %q, want the fallback %q", got, install)
	}
}

// The whole point of the split: state goes to the volume, and the program's
// own directory is left alone. A container replaces /app on every deploy.
func TestStateGoesToTheDataDirectoryAndAssetsStayWithTheProgram(t *testing.T) {
	install := t.TempDir()
	data := t.TempDir()
	t.Setenv("DATA_DIR", data)

	app := newAppAt(install, data)

	for name, path := range map[string]string{
		"accounts":     app.usersPath,
		"API tokens":   app.tokensPath(),
		"audit trail":  app.auditPath(),
		"settings":     app.envPath,
		"the log":      app.logFilePath,
		"chaos runs":   app.dataScopeFor("").chaosRunsDir(),
		"saved tasks":  app.dataScopeFor("").tasksPath(),
		"the base dir": app.baseDir,
	} {
		if !strings.HasPrefix(path, data) {
			t.Errorf("%s is at %q, outside the data directory %q", name, path, data)
		}
	}

	// Templates and static files are read from beside the program. Reading
	// them from the state directory would let whatever can write the volume
	// replace the pages this server serves.
	if got := app.assetDir(); got != install {
		t.Errorf("assets resolve to %q, want the program directory %q", got, install)
	}
}

// The CLIs run in the same container as the server — `docker compose exec
// 3270Web /app/3270Web user add alice` — and must edit the files the server
// reads. Writing to the image layer instead would create an account the
// running server never sees.
func TestTheCLIsUseTheSameStateDirectoryAsTheServer(t *testing.T) {
	data := t.TempDir()
	t.Setenv("DATA_DIR", data)
	// The per-file overrides must not be set, or they would mask the bug this
	// is here to catch.
	t.Setenv("USERS_PATH", "")
	t.Setenv("API_TOKENS_PATH", "")
	t.Setenv("AUDIT_LOG_PATH", "")

	for name, path := range map[string]string{
		"3270Web user":  resolveUsersPath(),
		"3270Web token": resolveTokensPath(),
		"the audit log": resolveAuditPath(),
	} {
		if !strings.HasPrefix(path, data) {
			t.Errorf("%s resolves to %q, outside the data directory %q", name, path, data)
		}
	}
}
