package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

func runCLI(t *testing.T, stdinText string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runUserCLI(args, &stdout, &stderr, strings.NewReader(stdinText))
	return code, stdout.String(), stderr.String()
}

func withUsersPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.json")
	t.Setenv("USERS_PATH", path)
	return path
}

func TestUserCLIAddListAndLogin(t *testing.T) {
	path := withUsersPath(t)

	code, out, errOut := runCLI(t, loginTestPassword+"\n", "add", "alice", "--admin")
	if code != 0 {
		t.Fatalf("add exited %d: %s / %s", code, out, errOut)
	}
	if !strings.Contains(out, "created alice") {
		t.Errorf("add output = %q", out)
	}

	code, out, _ = runCLI(t, "", "list")
	if code != 0 {
		t.Fatalf("list exited %d", code)
	}
	for _, want := range []string{"USERNAME", "alice", string(authz.RoleAdmin), "enabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q: %s", want, out)
		}
	}

	// The account the CLI wrote must be usable by the server's own store.
	if _, err := users.NewStore(path).Authenticate("alice", loginTestPassword); err != nil {
		t.Errorf("the CLI-created account cannot log in: %v", err)
	}
}

func TestUserCLIDefaultsToNonAdmin(t *testing.T) {
	path := withUsersPath(t)

	if code, _, e := runCLI(t, loginTestPassword+"\n", "add", "bob"); code != 0 {
		t.Fatalf("add exited %d: %s", code, e)
	}
	list, err := users.NewStore(path).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Role != authz.RoleUser {
		t.Errorf("role = %v, want %q — a new account must not be an admin by default", list, authz.RoleUser)
	}
}

func TestUserCLIRejectsWeakAndDuplicate(t *testing.T) {
	withUsersPath(t)

	if code, _, errOut := runCLI(t, "short\n", "add", "alice"); code == 0 {
		t.Error("a too-short password was accepted")
	} else if !strings.Contains(errOut, "at least") {
		t.Errorf("stderr = %q, want it to state the length rule", errOut)
	}

	if code, _, e := runCLI(t, loginTestPassword+"\n", "add", "alice"); code != 0 {
		t.Fatalf("first add exited %d: %s", code, e)
	}
	code, _, errOut := runCLI(t, loginTestPassword+"\n", "add", "alice")
	if code == 0 {
		t.Error("a duplicate username was accepted")
	}
	if !strings.Contains(errOut, "already exists") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestUserCLIRejectsUnsafeUsername(t *testing.T) {
	withUsersPath(t)
	for _, name := range []string{"../etc/passwd", "a b", "alice@example.com", "café"} {
		if code, _, _ := runCLI(t, loginTestPassword+"\n", "add", name); code == 0 {
			t.Errorf("username %q was accepted", name)
		}
	}
}

func TestUserCLIPasswdAndDisable(t *testing.T) {
	path := withUsersPath(t)
	store := users.NewStore(path)

	if code, _, e := runCLI(t, loginTestPassword+"\n", "add", "root", "--admin"); code != 0 {
		t.Fatalf("add root: %d %s", code, e)
	}
	if code, _, e := runCLI(t, loginTestPassword+"\n", "add", "bob"); code != 0 {
		t.Fatalf("add bob: %d %s", code, e)
	}

	const next = "a-different-long-password"
	if code, out, e := runCLI(t, next+"\n", "passwd", "bob"); code != 0 {
		t.Fatalf("passwd: %d %s %s", code, out, e)
	}
	if _, err := store.Authenticate("bob", next); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}

	if code, out, e := runCLI(t, "", "disable", "bob"); code != 0 {
		t.Fatalf("disable: %d %s %s", code, out, e)
	}
	if _, err := store.Authenticate("bob", next); err != users.ErrUserDisabled {
		t.Errorf("disabled account authenticated: %v", err)
	}

	if code, _, e := runCLI(t, "", "enable", "bob"); code != 0 {
		t.Fatalf("enable: %d %s", code, e)
	}
	if _, err := store.Authenticate("bob", next); err != nil {
		t.Errorf("re-enabled account cannot log in: %v", err)
	}
}

func TestUserCLIRefusesToStrandTheInstance(t *testing.T) {
	withUsersPath(t)
	if code, _, e := runCLI(t, loginTestPassword+"\n", "add", "root", "--admin"); code != 0 {
		t.Fatalf("add: %d %s", code, e)
	}

	code, _, errOut := runCLI(t, "", "disable", "root")
	if code == 0 {
		t.Error("disabling the only admin was allowed")
	}
	if !strings.Contains(errOut, "only enabled admin") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestUserCLIUnknownUserAndUsage(t *testing.T) {
	withUsersPath(t)

	if code, _, errOut := runCLI(t, "", "disable", "ghost"); code == 0 ||
		!strings.Contains(errOut, "no user named") {
		t.Errorf("disable ghost: code=%d stderr=%q", code, errOut)
	}
	if code, _, _ := runCLI(t, "", "wat"); code != 2 {
		t.Errorf("unknown subcommand exit = %d, want 2", code)
	}
	if code, _, _ := runCLI(t, ""); code != 2 {
		t.Errorf("no subcommand exit = %d, want 2", code)
	}
	if code, out, _ := runCLI(t, "", "help"); code != 0 || !strings.Contains(out, "Usage:") {
		t.Errorf("help: code=%d out=%q", code, out)
	}
	// A password on the command line must not be silently accepted, since it
	// would be visible in the process list.
	if code, _, _ := runCLI(t, loginTestPassword+"\n", "add", "alice", "extra-arg"); code != 2 {
		t.Errorf("extra positional argument exit = %d, want 2", code)
	}
}

func TestUserCLIEmptyList(t *testing.T) {
	withUsersPath(t)
	code, out, _ := runCLI(t, "", "list")
	if code != 0 || !strings.Contains(out, "no accounts yet") {
		t.Errorf("list on an empty store: code=%d out=%q", code, out)
	}
}
