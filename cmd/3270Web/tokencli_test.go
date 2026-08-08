package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/apitoken"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// runToken drives the CLI against stores in a temporary directory and returns
// its exit code and combined output.
func runToken(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runTokenCLI(args, &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

// tokenCLIDirs points both the account store and the token store at a
// temporary directory, so a test never touches a real installation.
func tokenCLIDirs(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERS_PATH", filepath.Join(dir, "users.json"))
	t.Setenv("API_TOKENS_PATH", filepath.Join(dir, "api-tokens.json"))
}

func seedAccount(t *testing.T, username string, role authz.Role) users.User {
	t.Helper()
	store := users.NewStore(resolveUsersPath())
	u, err := store.Add(username, "a-long-enough-password", role, false)
	if err != nil {
		t.Fatalf("seed %q: %v", username, err)
	}
	return u
}

// The token is shown once. If the CLI does not print something usable, the
// whole feature is unreachable.
func TestTokenCLIIssuesAUsableToken(t *testing.T) {
	tokenCLIDirs(t)
	alice := seedAccount(t, "alice", authz.RoleUser)

	code, out := runToken(t, "add", "alice", "ci pipeline")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, out)
	}

	secret := findToken(t, out)
	verified, err := apitoken.NewStore(resolveTokensPath()).Verify(secret)
	if err != nil {
		t.Fatalf("the printed token does not verify: %v", err)
	}
	if verified.UserID != alice.ID {
		t.Errorf("token belongs to %q, want %q", verified.UserID, alice.ID)
	}
	if !verified.HasScope(apitoken.ScopeRead) || !verified.HasScope(apitoken.ScopeWrite) {
		t.Errorf("scopes = %v, want read and write by default", verified.Scopes)
	}
	if !strings.Contains(out, "only time") {
		t.Errorf("the output does not say the token will not be shown again:\n%s", out)
	}
}

func TestTokenCLIReadOnlyAndExpiry(t *testing.T) {
	tokenCLIDirs(t)
	seedAccount(t, "alice", authz.RoleUser)

	code, out := runToken(t, "add", "alice", "scraper", "--read-only", "--expires", "24h")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, out)
	}
	verified, err := apitoken.NewStore(resolveTokensPath()).Verify(findToken(t, out))
	if err != nil {
		t.Fatal(err)
	}
	if verified.HasScope(apitoken.ScopeWrite) {
		t.Errorf("--read-only issued a writing token: %v", verified.Scopes)
	}
	if verified.ExpiresAt == nil {
		t.Error("--expires did not set an expiry")
	}

	if code, out := runToken(t, "add", "alice", "bad", "--expires", "next tuesday"); code == 0 {
		t.Errorf("an unreadable duration was accepted: %s", out)
	}
}

func TestTokenCLIListAndRevoke(t *testing.T) {
	tokenCLIDirs(t)
	seedAccount(t, "alice", authz.RoleUser)
	seedAccount(t, "bob", authz.RoleUser)

	if code, out := runToken(t, "list"); code != 0 || !strings.Contains(out, "no tokens issued") {
		t.Errorf("empty list = %d %q", code, out)
	}

	_, aliceOut := runToken(t, "add", "alice", "alice's ci")
	runToken(t, "add", "bob", "bob's ci")

	code, out := runToken(t, "list")
	if code != 0 {
		t.Fatalf("list: %s", out)
	}
	for _, want := range []string{"alice", "bob", "alice's ci", "read+write", "active", "never"} {
		if !strings.Contains(out, want) {
			t.Errorf("list does not mention %q:\n%s", want, out)
		}
	}
	// The list is of purposes, not of secrets.
	if strings.Contains(out, findToken(t, aliceOut)) {
		t.Error("the list printed a token")
	}

	// Filtered to one account.
	_, filtered := runToken(t, "list", "alice")
	if !strings.Contains(filtered, "alice's ci") || strings.Contains(filtered, "bob's ci") {
		t.Errorf("filtered list = %s", filtered)
	}

	// Revoking by id.
	id, _, err := apitoken.Parse(findToken(t, aliceOut))
	if err != nil {
		t.Fatal(err)
	}
	if code, out := runToken(t, "revoke", id); code != 0 {
		t.Fatalf("revoke: %d %s", code, out)
	}
	if _, out := runToken(t, "list", "alice"); !strings.Contains(out, "revoked") {
		t.Errorf("the revoked token is not marked:\n%s", out)
	}
	if code, _ := runToken(t, "revoke", "no-such-id"); code == 0 {
		t.Error("revoking an unknown id succeeded")
	}
}

func TestTokenCLIRevokeAll(t *testing.T) {
	tokenCLIDirs(t)
	seedAccount(t, "alice", authz.RoleUser)
	runToken(t, "add", "alice", "one")
	runToken(t, "add", "alice", "two")

	code, out := runToken(t, "revoke-all", "alice")
	if code != 0 || !strings.Contains(out, "2 token") {
		t.Errorf("revoke-all = %d %q", code, out)
	}
}

func TestTokenCLIRejectsNonsense(t *testing.T) {
	tokenCLIDirs(t)
	seedAccount(t, "alice", authz.RoleUser)

	for _, args := range [][]string{
		{},
		{"nonsense"},
		{"add"},
		{"add", "alice"},
		{"add", "nobody", "ci"},
		{"add", "alice", "ci", "--wat"},
		{"list", "nobody"},
		{"revoke"},
		{"revoke-all"},
	} {
		if code, out := runToken(t, args...); code == 0 {
			t.Errorf("`token %s` succeeded: %s", strings.Join(args, " "), out)
		}
	}

	if code, out := runToken(t, "help"); code != 0 || !strings.Contains(out, "Usage:") {
		t.Errorf("help = %d %q", code, out)
	}
}

// findToken pulls the printed credential out of the CLI's output.
func findToken(t *testing.T, out string) string {
	t.Helper()
	for _, field := range strings.Fields(out) {
		if apitoken.LooksLikeToken(field) {
			return field
		}
	}
	t.Fatalf("no token in the output:\n%s", out)
	return ""
}
