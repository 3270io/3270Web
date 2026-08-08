package apitoken

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "tokens.json"))
}

func TestIssueAndVerify(t *testing.T) {
	store := newTestStore(t)

	record, secret, err := store.Issue("aaaa1111", "ci pipeline", []string{ScopeRead}, nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !strings.HasPrefix(secret, Prefix+"_") {
		t.Errorf("secret %q does not carry the recognisable prefix", secret)
	}
	if record.SecretHash != "" {
		t.Error("the returned record still carries the hash")
	}

	got, err := store.Verify(secret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.ID != record.ID || got.UserID != "aaaa1111" {
		t.Errorf("verify returned %+v, want the issued token", got)
	}
	if !got.HasScope(ScopeRead) || got.HasScope(ScopeWrite) {
		t.Errorf("scopes = %v, want read only", got.Scopes)
	}
}

// The whole point of storing a hash is that reading the file yields nothing
// usable.
func TestTheSecretIsNeverStored(t *testing.T) {
	store := newTestStore(t)
	_, secret, err := store.Issue("aaaa1111", "ci", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, rawSecret, err := Parse(secret)
	if err != nil {
		t.Fatal(err)
	}

	onDisk, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(onDisk), rawSecret) {
		t.Error("the secret is recoverable from the token file")
	}

	// Nor does listing hand it back.
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range list {
		if tok.SecretHash != "" {
			t.Error("List returned a hash")
		}
	}
}

func TestFilePermissions(t *testing.T) {
	store := newTestStore(t)
	if _, _, err := store.Issue("aaaa1111", "ci", nil, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
}

// A token whose ID exists but whose secret does not must be indistinguishable
// from one whose ID was invented, or the error tells an attacker which half
// they guessed right.
func TestWrongSecretIsNotFound(t *testing.T) {
	store := newTestStore(t)
	record, _, err := store.Issue("aaaa1111", "ci", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Verify(Format(record.ID, "not-the-secret")); !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong secret gave %v, want ErrNotFound", err)
	}
	if _, err := store.Verify(Format("00000000", "not-the-secret")); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id gave %v, want ErrNotFound", err)
	}
}

func TestMalformedTokens(t *testing.T) {
	store := newTestStore(t)
	for _, bad := range []string{
		"", "   ", "hunter2", "3270w", "3270w_", "3270w_abc",
		"3270w_abc_", "3270w__secret", "other_abc_secret",
		"3270w_abc_secret_extra",
	} {
		if _, err := store.Verify(bad); !errors.Is(err, ErrMalformed) {
			t.Errorf("Verify(%q) = %v, want ErrMalformed", bad, err)
		}
		if LooksLikeToken(bad) {
			t.Errorf("LooksLikeToken(%q) = true", bad)
		}
	}
	if !LooksLikeToken(Format("abc", "secret")) {
		t.Error("a well-formed token was not recognised")
	}
}

// The separator is part of the format, so the alphabet the halves are drawn
// from must not contain it. Base64url does, and roughly half of all secrets
// failed to verify until the alphabet changed — a bug that only shows up some
// of the time, which is the kind worth pinning.
func TestGeneratedTokensNeverContainTheSeparator(t *testing.T) {
	store := newTestStore(t)
	for i := 0; i < 200; i++ {
		record, secret, err := store.Issue("aaaa1111", "ci", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		id, rawSecret, err := Parse(secret)
		if err != nil {
			t.Fatalf("Parse(%q): %v", secret, err)
		}
		if id != record.ID {
			t.Fatalf("round trip lost the id: %q vs %q", id, record.ID)
		}
		if strings.Contains(rawSecret, "_") || strings.Contains(id, "_") {
			t.Fatalf("token %q contains the separator", secret)
		}
	}
}

func TestRevoke(t *testing.T) {
	store := newTestStore(t)
	record, secret, err := store.Issue("aaaa1111", "ci", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(record.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := store.Verify(secret); !errors.Is(err, ErrRevoked) {
		t.Errorf("revoked token verified with %v, want ErrRevoked", err)
	}

	// Revoking twice is not an error — the desired state is already true.
	if err := store.Revoke(record.ID); err != nil {
		t.Errorf("second revoke: %v", err)
	}
	if err := store.Revoke("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking an unknown id gave %v, want ErrNotFound", err)
	}

	// The record survives, so the history of what existed is not erased.
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].RevokedAt == nil {
		t.Errorf("list = %+v, want one revoked record", list)
	}
}

func TestExpiry(t *testing.T) {
	store := newTestStore(t)
	past := time.Now().Add(-time.Minute)
	_, secret, err := store.Issue("aaaa1111", "ci", nil, &past)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Verify(secret); !errors.Is(err, ErrExpired) {
		t.Errorf("expired token verified with %v, want ErrExpired", err)
	}

	future := time.Now().Add(time.Hour)
	_, live, err := store.Issue("aaaa1111", "ci", nil, &future)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Verify(live); err != nil {
		t.Errorf("unexpired token: %v", err)
	}
}

// Disabling an account must stop its automated clients too, or "disabled"
// means only that the person cannot sign in.
func TestRevokeAllFor(t *testing.T) {
	store := newTestStore(t)
	_, aliceOne, _ := store.Issue("aaaa1111", "one", nil, nil)
	_, aliceTwo, _ := store.Issue("aaaa1111", "two", nil, nil)
	_, bob, _ := store.Issue("bbbb2222", "bob's", nil, nil)

	n, err := store.RevokeAllFor("aaaa1111")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("revoked %d, want 2", n)
	}
	for _, secret := range []string{aliceOne, aliceTwo} {
		if _, err := store.Verify(secret); !errors.Is(err, ErrRevoked) {
			t.Errorf("alice's token still works: %v", err)
		}
	}
	if _, err := store.Verify(bob); err != nil {
		t.Errorf("bob's token was caught up in it: %v", err)
	}

	// Running again reports nothing left to do rather than failing.
	if n, err := store.RevokeAllFor("aaaa1111"); err != nil || n != 0 {
		t.Errorf("second sweep = %d, %v, want 0, nil", n, err)
	}
	if n, err := store.RevokeAllFor(""); err != nil || n != 0 {
		t.Errorf("empty user = %d, %v, want 0, nil", n, err)
	}
}

func TestIssueRejectsNonsense(t *testing.T) {
	store := newTestStore(t)
	if _, _, err := store.Issue("", "ci", nil, nil); err == nil {
		t.Error("issued a token belonging to nobody")
	}
	if _, _, err := store.Issue("aaaa1111", "   ", nil, nil); err == nil {
		t.Error("issued a token with no name")
	}
	if _, _, err := store.Issue("aaaa1111", "ci", []string{"root"}, nil); err == nil {
		t.Error("issued a token with an invented scope")
	}

	// No scopes means the account's own reach, which is read and write.
	record, _, err := store.Issue("aaaa1111", "ci", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !record.HasScope(ScopeRead) || !record.HasScope(ScopeWrite) {
		t.Errorf("default scopes = %v, want read and write", record.Scopes)
	}
}

// Two tokens issued at the same moment must both survive: a read-modify-write
// under a shared lock is only safe if every path takes it.
func TestConcurrentIssue(t *testing.T) {
	store := newTestStore(t)
	const n = 12
	var wg sync.WaitGroup
	secrets := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, secret, err := store.Issue("aaaa1111", "concurrent", nil, nil)
			if err != nil {
				t.Errorf("issue %d: %v", i, err)
				return
			}
			secrets[i] = secret
		}(i)
	}
	wg.Wait()

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != n {
		t.Errorf("stored %d tokens, want %d", len(list), n)
	}
	for i, secret := range secrets {
		if secret == "" {
			continue
		}
		if _, err := store.Verify(secret); err != nil {
			t.Errorf("token %d does not verify: %v", i, err)
		}
	}
}

func TestLastUsedIsRecorded(t *testing.T) {
	store := newTestStore(t)
	record, secret, err := store.Issue("aaaa1111", "ci", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := store.List()
	if list[0].LastUsedAt != nil {
		t.Error("a freshly issued token claims to have been used")
	}

	if _, err := store.Verify(secret); err != nil {
		t.Fatal(err)
	}
	list, _ = store.List()
	if list[0].ID != record.ID || list[0].LastUsedAt == nil {
		t.Errorf("last-used was not recorded: %+v", list[0])
	}
}

// An absent file is an instance that has issued nothing, not an error.
func TestEmptyStore(t *testing.T) {
	store := newTestStore(t)
	list, err := store.List()
	if err != nil || len(list) != 0 {
		t.Errorf("List on a fresh store = %v, %v", list, err)
	}
	if _, err := store.Verify(Format("abcd", "secret")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Verify on a fresh store = %v, want ErrNotFound", err)
	}
}
