// SPDX-License-Identifier: AGPL-3.0-or-later

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
	got, err := resolveDataDir(install)
	if err != nil || got != install {
		t.Errorf("resolveDataDir = %q, %v; want %q, nil", got, err, install)
	}
}

func TestDataDirIsCreatedIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not", "there", "yet")
	t.Setenv("DATA_DIR", dir)

	got, err := resolveDataDir(t.TempDir())
	if err != nil {
		t.Fatalf("resolveDataDir: %v", err)
	}
	if got != dir {
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

// A DATA_DIR that is set and unusable is an error rather than something to
// work around. Falling back to the program's directory would put the state
// exactly where a deploy deletes it, and the instance would look healthy right
// up until the day it lost every account.
func TestAnUnusableDataDirIsAnError(t *testing.T) {
	install := t.TempDir()
	// A file where the directory should be.
	blocked := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", blocked)

	got, err := resolveDataDir(install)
	if err == nil {
		t.Fatalf("resolveDataDir = %q, nil; want an error rather than a fallback", got)
	}
	if got == install {
		t.Error("it fell back to the program directory as well as erroring")
	}
	if !strings.Contains(err.Error(), blocked) {
		t.Errorf("the error does not name the directory: %v", err)
	}
}

// The failure people actually hit: the directory exists, and belongs to
// somebody else. MkdirAll succeeds on it, so only attempting a write finds
// out — which is the whole reason for the probe.
func TestADataDirThatCannotBeWrittenIsAnError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, which can write regardless of the mode")
	}
	dir := filepath.Join(t.TempDir(), "owned-by-somebody-else")
	if err := os.Mkdir(dir, 0o500); err != nil { // readable and traversable, not writable
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", dir)

	if _, err := resolveDataDir(t.TempDir()); err == nil {
		t.Error("an unwritable data directory was accepted; state would go somewhere a deploy deletes")
	} else if !strings.Contains(err.Error(), "not writable") {
		t.Errorf("the error does not say what is wrong: %v", err)
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

// Setting DATA_DIR on an instance that already has accounts must not look
// like losing them. Without this the server reads an empty directory, finds
// no accounts, and comes back up in first-run setup — with the real files
// sitting beside the program, which is no comfort to whoever is staring at a
// setup page where their sign-in used to be.
func TestAnOlderInstallationsFilesMoveIntoTheDataDirectory(t *testing.T) {
	install := t.TempDir()
	data := t.TempDir()

	// What an existing install looks like: state beside the program, and the
	// program itself, which must be left where it is.
	write(t, filepath.Join(install, "users.json"), `{"users":[{"username":"alice"}]}`)
	write(t, filepath.Join(install, "audit.log"), `{"event":"login.succeeded"}`)
	write(t, filepath.Join(install, ".env"), "S3270_MODEL=3279-2-E\n")
	write(t, filepath.Join(install, "chaos-runs", "old-run.json"), `{"id":"old"}`)
	write(t, filepath.Join(install, "3270Web"), "the binary")
	write(t, filepath.Join(install, "web", "templates", "login.html"), "<html>")

	if err := migrateStateToDataDir(install, data); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, name := range []string{"users.json", "audit.log", ".env", "chaos-runs/old-run.json"} {
		if _, err := os.Stat(filepath.Join(data, filepath.FromSlash(name))); err != nil {
			t.Errorf("%s did not arrive in the data directory: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(install, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Errorf("%s was copied rather than moved", name)
		}
	}
	// The program and its assets stay put, or the next start has nothing to run.
	for _, name := range []string{"3270Web", "web/templates/login.html"} {
		if _, err := os.Stat(filepath.Join(install, filepath.FromSlash(name))); err != nil {
			t.Errorf("%s was moved out from under the program: %v", name, err)
		}
	}
	if body := read(t, filepath.Join(data, "users.json")); !strings.Contains(body, "alice") {
		t.Errorf("the accounts file arrived empty: %q", body)
	}
}

// A data directory that already holds accounts is authoritative. Shovelling
// another instance's files into it would be worse than doing nothing.
func TestMigrationLeavesAPopulatedDataDirectoryAlone(t *testing.T) {
	install := t.TempDir()
	data := t.TempDir()
	write(t, filepath.Join(install, "users.json"), `{"users":[{"username":"from-the-image"}]}`)
	write(t, filepath.Join(data, "users.json"), `{"users":[{"username":"already-here"}]}`)

	if err := migrateStateToDataDir(install, data); err != nil {
		t.Fatal(err)
	}
	if body := read(t, filepath.Join(data, "users.json")); !strings.Contains(body, "already-here") {
		t.Errorf("the existing accounts were overwritten: %q", body)
	}
	if _, err := os.Stat(filepath.Join(install, "users.json")); err != nil {
		t.Error("the other directory's file was moved anyway")
	}
}

// Nothing to move, and the two being the same directory — a desktop install —
// must both be no-ops rather than errors.
func TestMigrationIsANoOpWhenThereIsNothingToDo(t *testing.T) {
	install := t.TempDir()
	if err := migrateStateToDataDir(install, t.TempDir()); err != nil {
		t.Errorf("an empty installation: %v", err)
	}
	write(t, filepath.Join(install, "users.json"), "{}")
	if err := migrateStateToDataDir(install, install); err != nil {
		t.Errorf("the same directory twice: %v", err)
	}
	if _, err := os.Stat(filepath.Join(install, "users.json")); err != nil {
		t.Error("a desktop install had its own accounts moved")
	}
}

// The old location is usually a mounted volume and the new one is not, so a
// rename fails with EXDEV. Falling back to a copy is what rescues exactly the
// installations this exists for.
func TestMovePathCopiesWhenRenameCannotCrossFilesystems(t *testing.T) {
	from := filepath.Join(t.TempDir(), "tree")
	to := filepath.Join(t.TempDir(), "tree")
	write(t, filepath.Join(from, "nested", "file.json"), `{"kept":true}`)

	if err := copyPath(from, to); err != nil {
		t.Fatalf("copyPath: %v", err)
	}
	if body := read(t, filepath.Join(to, "nested", "file.json")); !strings.Contains(body, "kept") {
		t.Errorf("the copy lost the contents: %q", body)
	}

	// And the move built on it leaves nothing behind.
	other := filepath.Join(t.TempDir(), "moved")
	if err := movePath(from, other); err != nil {
		t.Fatalf("movePath: %v", err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Error("movePath left the original in place")
	}
	if _, err := os.Stat(filepath.Join(other, "nested", "file.json")); err != nil {
		t.Errorf("movePath lost the tree: %v", err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
