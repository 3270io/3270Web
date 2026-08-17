// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jnnngs/3270Web/internal/authz"
)

const testPassword = "correct-horse-battery-staple"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "users.json"))
}

func TestAddAndAuthenticate(t *testing.T) {
	s := newTestStore(t)

	created, err := s.Add("alice", testPassword, authz.RoleAdmin, false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if created.PasswordHash != "" {
		t.Error("Add returned a hash to its caller")
	}
	if created.ID == "" {
		t.Error("Add did not assign an ID")
	}
	if created.ID == created.Username {
		t.Error("ID must not be the username")
	}

	got, err := s.Authenticate("alice", testPassword)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("authenticated ID = %q, want %q", got.ID, created.ID)
	}
	if got.PasswordHash != "" {
		t.Error("Authenticate returned a hash to its caller")
	}

	// Usernames are matched case-insensitively so one person cannot end up
	// with two accounts that look identical in a log.
	if _, err := s.Authenticate("ALICE", testPassword); err != nil {
		t.Errorf("case-insensitive login failed: %v", err)
	}
}

// Both failure modes must be the same error, or the login form reports which
// usernames exist.
func TestAuthenticateIsIndistinguishable(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("alice", testPassword, authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}

	_, wrongPassword := s.Authenticate("alice", "not-the-password-at-all")
	_, noSuchUser := s.Authenticate("mallory", "not-the-password-at-all")

	if wrongPassword != ErrInvalidCredentials {
		t.Errorf("wrong password gave %v, want %v", wrongPassword, ErrInvalidCredentials)
	}
	if noSuchUser != ErrInvalidCredentials {
		t.Errorf("unknown user gave %v, want %v", noSuchUser, ErrInvalidCredentials)
	}
}

func TestAddRejectsDuplicateIgnoringCase(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("alice", testPassword, authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("ALICE", testPassword, authz.RoleUser, false); err != ErrUserExists {
		t.Errorf("duplicate add gave %v, want %v", err, ErrUserExists)
	}
}

func TestDisabledAccountCannotLogIn(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("alice", testPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("bob", testPassword, authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDisabled("bob", true); err != nil {
		t.Fatalf("SetDisabled: %v", err)
	}

	if _, err := s.Authenticate("bob", testPassword); err != ErrUserDisabled {
		t.Errorf("disabled login gave %v, want %v", err, ErrUserDisabled)
	}
	// A disabled account with the wrong password must still look like an
	// ordinary credential failure.
	if _, err := s.Authenticate("bob", "wrong-password-here"); err != ErrInvalidCredentials {
		t.Errorf("disabled + wrong password gave %v, want %v", err, ErrInvalidCredentials)
	}
}

// Locking out the last admin leaves an instance that can only be repaired by
// hand-editing the store.
func TestCannotDisableLastAdmin(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("root", testPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("bob", testPassword, authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}

	if err := s.SetDisabled("root", true); err == nil {
		t.Fatal("disabling the only admin was allowed")
	}

	// With a second admin it becomes allowed.
	if _, err := s.Add("root2", testPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDisabled("root", true); err != nil {
		t.Errorf("disabling one of two admins failed: %v", err)
	}
}

func TestSetPasswordClearsMustChange(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("alice", testPassword, authz.RoleAdmin, true); err != nil {
		t.Fatal(err)
	}

	got, err := s.Authenticate("alice", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !got.MustChangePassword {
		t.Error("system-issued account is not flagged for a password change")
	}

	const next = "a-brand-new-password"
	if err := s.SetPassword("alice", next); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if _, err := s.Authenticate("alice", testPassword); err != ErrInvalidCredentials {
		t.Error("the old password still works")
	}
	got, err = s.Authenticate("alice", next)
	if err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	if got.MustChangePassword {
		t.Error("MustChangePassword survived a password change")
	}
}

func TestSetPasswordUnknownUser(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetPassword("nobody", testPassword); err != ErrUserNotFound {
		t.Errorf("got %v, want %v", err, ErrUserNotFound)
	}
}

func TestStoreFileIsPrivateAndHoldsNoPlaintext(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("alice", testPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), testPassword) {
		t.Fatal("the store contains the plaintext password")
	}
	if !strings.Contains(string(data), "$argon2id$") {
		t.Error("the stored hash is not argon2id")
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.Path())
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("store permissions = %04o, want 0600", perm)
		}
	}
}

func TestLoadMissingAndEmptyFile(t *testing.T) {
	s := newTestStore(t)

	// Missing file: an empty store, not an error. This is the state before
	// the first account exists, which the bootstrap path depends on.
	n, err := s.Count()
	if err != nil {
		t.Fatalf("Count on a missing file: %v", err)
	}
	if n != 0 {
		t.Errorf("Count = %d, want 0", n)
	}

	if err := os.WriteFile(s.Path(), []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err = s.Count(); err != nil || n != 0 {
		t.Errorf("Count on a blank file = %d, %v; want 0, nil", n, err)
	}
}

func TestCorruptStoreIsAnError(t *testing.T) {
	s := newTestStore(t)
	if err := os.WriteFile(s.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Count(); err == nil {
		t.Error("a corrupt store parsed without error")
	}
}

func TestPersistenceAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")

	first := NewStore(path)
	created, err := first.Add("alice", testPassword, authz.RoleAdmin, false)
	if err != nil {
		t.Fatal(err)
	}

	second := NewStore(path)
	got, err := second.Authenticate("alice", testPassword)
	if err != nil {
		t.Fatalf("login against a reopened store: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID changed across reopen: %q vs %q", got.ID, created.ID)
	}
}

func TestByID(t *testing.T) {
	s := newTestStore(t)
	created, err := s.Add("alice", testPassword, authz.RoleUser, false)
	if err != nil {
		t.Fatal(err)
	}

	got, ok, err := s.ByID(created.ID)
	if err != nil || !ok {
		t.Fatalf("ByID(%q) = %v, %v", created.ID, ok, err)
	}
	if got.Username != "alice" {
		t.Errorf("username = %q, want alice", got.Username)
	}

	if _, ok, _ = s.ByID("nope"); ok {
		t.Error("ByID found an account that does not exist")
	}
}

func TestPrincipalConversion(t *testing.T) {
	u := User{ID: "abc", Role: authz.RoleAdmin}
	p := u.Principal(authz.KindWeb)

	if p.UserID != "abc" || !p.IsAdmin() || p.Kind != authz.KindWeb {
		t.Errorf("Principal() = %+v, want the account's id, role and the given kind", p)
	}
	if !p.Owns("abc") || p.Owns("other") {
		t.Error("principal ownership does not follow the account ID")
	}
}

func TestValidateUsername(t *testing.T) {
	for _, ok := range []string{"alice", "a", "A.b-c_1", strings.Repeat("a", maxUsernameLength)} {
		if err := ValidateUsername(ok); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{
		"", "   ", " alice", "alice ", "al ice", "alice/../root", "alice\\root",
		"../etc", "alice\x00", "café", "alice@example.com",
		strings.Repeat("a", maxUsernameLength+1),
	} {
		if err := ValidateUsername(bad); err == nil {
			t.Errorf("ValidateUsername(%q) = nil, want an error", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("x", MinPasswordLength)); err != nil {
		t.Errorf("minimum-length password rejected: %v", err)
	}
	for _, bad := range []string{
		"", "short", strings.Repeat("x", MinPasswordLength-1),
		"has\x00a-null-byte-in-it", "has\na-newline-in-it",
		strings.Repeat("x", MaxPasswordLength+1),
	} {
		if err := ValidatePassword(bad); err == nil {
			t.Errorf("ValidatePassword(%q) = nil, want an error", bad)
		}
	}
}

func TestGeneratePasswordIsUsableAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 25; i++ {
		p, err := GeneratePassword()
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidatePassword(p); err != nil {
			t.Fatalf("generated password fails validation: %v", err)
		}
		if seen[p] {
			t.Fatal("GeneratePassword repeated a value")
		}
		seen[p] = true
	}
}

func TestRedactedStripsHash(t *testing.T) {
	u := User{Username: "alice", PasswordHash: "$argon2id$secret"}
	if u.Redacted().PasswordHash != "" {
		t.Error("Redacted kept the hash")
	}
	if u.PasswordHash == "" {
		t.Error("Redacted mutated its receiver")
	}
}

// The on-disk shape is a compatibility surface: an operator may hand-edit it,
// and a later version has to read what this one wrote.
func TestFileFormatIsStable(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("alice", testPassword, authz.RoleAdmin, true); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("stored file is not the documented shape: %v", err)
	}
	if len(parsed.Users) != 1 {
		t.Fatalf("users length = %d, want 1", len(parsed.Users))
	}
	for _, key := range []string{"id", "username", "role", "passwordHash", "createdAt"} {
		if _, ok := parsed.Users[0][key]; !ok {
			t.Errorf("stored user is missing %q", key)
		}
	}
}

func TestAddFirstAdmin(t *testing.T) {
	s := newTestStore(t)

	created, err := s.AddFirstAdmin("root", testPassword)
	if err != nil {
		t.Fatalf("AddFirstAdmin: %v", err)
	}
	if created.Role != authz.RoleAdmin {
		t.Errorf("role = %q, want %q", created.Role, authz.RoleAdmin)
	}
	// The account is chosen by its owner, so unlike an issued password it does
	// not need replacing before use.
	if created.MustChangePassword {
		t.Error("a self-chosen first password should not require a change")
	}
	if _, err := s.Authenticate("root", testPassword); err != nil {
		t.Errorf("the first admin cannot log in: %v", err)
	}
}

// Setup is reachable without credentials, so a second caller arriving after
// the first must not be able to add themselves as an administrator.
func TestAddFirstAdminRefusesOnceAccountsExist(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.AddFirstAdmin("root", testPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFirstAdmin("mallory", testPassword); err != ErrNotFirstUser {
		t.Errorf("second AddFirstAdmin gave %v, want %v", err, ErrNotFirstUser)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Username != "root" {
		t.Errorf("account list = %v, want only root", list)
	}
}

func TestAddFirstAdminConcurrent(t *testing.T) {
	s := newTestStore(t)

	var wg sync.WaitGroup
	created := make([]bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.AddFirstAdmin(fmt.Sprintf("admin%d", i), testPassword)
			created[i] = err == nil
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, ok := range created {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("%d concurrent callers succeeded, want exactly 1", wins)
	}
	if n, _ := s.Count(); n != 1 {
		t.Errorf("account count = %d, want 1", n)
	}
}

func TestSetRole(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("root", testPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("bob", testPassword, authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}

	if err := s.SetRole("bob", authz.RoleAdmin); err != nil {
		t.Fatalf("promote: %v", err)
	}
	list, _ := s.List()
	for _, u := range list {
		if u.Username == "bob" && u.Role != authz.RoleAdmin {
			t.Errorf("bob role = %q, want admin", u.Role)
		}
	}

	if err := s.SetRole("bob", authz.RoleUser); err != nil {
		t.Errorf("demote with another admin present: %v", err)
	}
	// root is now the only admin.
	if err := s.SetRole("root", authz.RoleUser); err == nil {
		t.Error("demoting the only admin was allowed")
	}
	if err := s.SetRole("root", "wizard"); err == nil {
		t.Error("an unknown role was accepted")
	}
	if err := s.SetRole("ghost", authz.RoleUser); err != ErrUserNotFound {
		t.Errorf("unknown user gave %v, want %v", err, ErrUserNotFound)
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("root", testPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("bob", testPassword, authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete("bob"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Authenticate("bob", testPassword); err != ErrInvalidCredentials {
		t.Error("a deleted account can still log in")
	}
	if err := s.Delete("root"); err == nil {
		t.Error("deleting the only admin was allowed")
	}
	if err := s.Delete("ghost"); err != ErrUserNotFound {
		t.Errorf("unknown user gave %v, want %v", err, ErrUserNotFound)
	}
}

func TestRequirePasswordChange(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Add("bob", testPassword, authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}

	if err := s.RequirePasswordChange("bob"); err != nil {
		t.Fatalf("RequirePasswordChange: %v", err)
	}
	got, err := s.Authenticate("bob", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !got.MustChangePassword {
		t.Error("the account was not flagged")
	}
	if err := s.RequirePasswordChange("ghost"); err != ErrUserNotFound {
		t.Errorf("unknown user gave %v, want %v", err, ErrUserNotFound)
	}
}

func TestExternalAccountIsFoundByIdentityNotByName(t *testing.T) {
	store := newTestStore(t)

	first, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", "", nil)
	if err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	if first.Role != authz.RoleUser {
		t.Errorf("Role = %q, want the default %q", first.Role, authz.RoleUser)
	}
	if !first.External() {
		t.Error("account is not marked external")
	}

	// Renamed at the provider. Same person, same account, same ID — which is
	// what keeps their saved work theirs, since the data directory is the ID.
	renamed, err := store.UpsertExternal("https://idp.example", "sub-1", "alice.smith", "", nil)
	if err != nil {
		t.Fatalf("sign-in after a rename: %v", err)
	}
	if renamed.ID != first.ID {
		t.Errorf("a rename created a second account: %s then %s", first.ID, renamed.ID)
	}
	if renamed.Username != "alice.smith" {
		t.Errorf("Username = %q, want the new name", renamed.Username)
	}

	// A different subject is a different person, whatever they are called.
	other, err := store.UpsertExternal("https://idp.example", "sub-2", "bob", "", nil)
	if err != nil {
		t.Fatalf("second person: %v", err)
	}
	if other.ID == first.ID {
		t.Error("two subjects resolved to one account")
	}
}

// The break-glass administrator is a local account, so an identity provider
// that can be talked into naming somebody "root" must not thereby become a way
// to sign in as them.
func TestExternalSignInWillNotClaimALocalAccountsName(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Add("root", testPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}

	if _, err := store.UpsertExternal("https://idp.example", "sub-9", "root", "", nil); !errors.Is(err, ErrNameHeldLocally) {
		t.Fatalf("err = %v, want ErrNameHeldLocally", err)
	}
	// And a rename into a taken name leaves the account as it was rather than
	// producing two accounts answering to "root".
	u, err := store.UpsertExternal("https://idp.example", "sub-9", "alice", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertExternal("https://idp.example", "sub-9", "root", "", nil); err != nil {
		t.Fatalf("rename into a taken name should be ignored, not fail: %v", err)
	}
	again, _, err := store.ByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Username != "alice" {
		t.Errorf("Username = %q, want it unchanged", again.Username)
	}
}

func TestExternalAccountHasNoLocalPassword(t *testing.T) {
	store := newTestStore(t)
	u, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// No password at all, and no password that happens to hash to nothing.
	for _, attempt := range []string{"", testPassword, "$argon2id$"} {
		if _, err := store.Authenticate("alice", attempt); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("Authenticate(%q) = %v, want ErrInvalidCredentials", attempt, err)
		}
	}
	// Nor can an administrator give it one, which would be a second way in
	// that the provider cannot revoke.
	if err := store.SetPassword("alice", testPassword); !errors.Is(err, ErrExternalAccount) {
		t.Errorf("SetPassword = %v, want ErrExternalAccount", err)
	}
	if _, err := store.Authenticate(u.Username, testPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("a password was set on an external account after all: %v", err)
	}
}

// A mapped role is re-applied on every sign-in, and an unmapped one leaves the
// account as an administrator set it.
func TestExternalRoleFollowsTheMappingOnlyWhenThereIsOne(t *testing.T) {
	store := newTestStore(t)
	// A local administrator, so the store is not being asked to demote its
	// only one below.
	if _, err := store.Add("root", testPassword, authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}

	if _, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", authz.RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	u, _, _ := store.ByID(mustFindID(t, store, "alice"))
	if u.Role != authz.RoleAdmin {
		t.Fatalf("Role = %q, want the mapped admin", u.Role)
	}

	// Mapping now says otherwise: the provider is the authority.
	if _, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", authz.RoleUser, nil); err != nil {
		t.Fatal(err)
	}
	if u, _, _ = store.ByID(u.ID); u.Role != authz.RoleUser {
		t.Errorf("Role = %q, want the mapping to have demoted them", u.Role)
	}

	// No mapping configured: a promotion made on the Accounts page has to
	// survive the next sign-in, or it could never be made at all.
	if err := store.SetRole("alice", authz.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", "", nil); err != nil {
		t.Fatal(err)
	}
	if u, _, _ = store.ByID(u.ID); u.Role != authz.RoleAdmin {
		t.Errorf("Role = %q, want the manual promotion kept", u.Role)
	}
}

func mustFindID(t *testing.T, store *Store, username string) string {
	t.Helper()
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range list {
		if u.Username == username {
			return u.ID
		}
	}
	t.Fatalf("no account named %q", username)
	return ""
}

func TestGroupsAreNormalisedAndDiscoverable(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Add("alice", testPassword, authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}

	// Whitespace, duplicates in different cases, and empties all come from a
	// human typing a comma-separated list.
	if err := store.SetGroups("alice", []string{" ops ", "OPS", "", "payments"}); err != nil {
		t.Fatal(err)
	}
	u, _, _ := store.ByID(mustFindID(t, store, "alice"))
	if len(u.Groups) != 2 || u.Groups[0] != "ops" || u.Groups[1] != "payments" {
		t.Fatalf("Groups = %v, want the list trimmed and de-duplicated", u.Groups)
	}

	// Membership is what a profile audience will ask about, and it must not
	// turn on how the name was capitalised.
	for _, name := range []string{"ops", "OPS", " Ops "} {
		if !u.InGroup(name) {
			t.Errorf("InGroup(%q) = false", name)
		}
	}
	if u.InGroup("finance") || u.InGroup("") {
		t.Error("InGroup matched a group the account is not in")
	}

	groups, err := store.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0] != "ops" || groups[1] != "payments" {
		t.Errorf("Groups() = %v, want every group in use, sorted", groups)
	}
}

// The directory is the authority on which team somebody is on, where the
// deployment says so — and silent about it where it does not.
func TestExternalGroupsFollowTheDirectoryOnlyWhenItSpeaks(t *testing.T) {
	store := newTestStore(t)

	u, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", "", []string{"ops", "ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Groups) != 1 || u.Groups[0] != "ops" {
		t.Fatalf("Groups = %v on first sign-in", u.Groups)
	}

	// Moved teams at the provider.
	if _, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", "", []string{"payments"}); err != nil {
		t.Fatal(err)
	}
	again, _, _ := store.ByID(u.ID)
	if len(again.Groups) != 1 || again.Groups[0] != "payments" {
		t.Errorf("Groups = %v, want the move followed through", again.Groups)
	}

	// Removed from everything: access that was granted by a group has to go
	// with it, so an empty list is applied rather than ignored.
	if _, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", "", []string{}); err != nil {
		t.Fatal(err)
	}
	if again, _, _ = store.ByID(u.ID); len(again.Groups) != 0 {
		t.Errorf("Groups = %v, want them cleared", again.Groups)
	}

	// nil is different: the deployment maps no claim, so what an administrator
	// set here stands.
	if err := store.SetGroups("alice", []string{"ops"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertExternal("https://idp.example", "sub-1", "alice", "", nil); err != nil {
		t.Fatal(err)
	}
	if again, _, _ = store.ByID(u.ID); len(again.Groups) != 1 || again.Groups[0] != "ops" {
		t.Errorf("Groups = %v, want the administrator's list kept", again.Groups)
	}
}
