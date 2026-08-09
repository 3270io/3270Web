package users

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/jnnngs/3270Web/internal/authz"
)

// A group an administrator can make, name, fill, rename and remove.
//
// The behaviour these turn on is that a group exists because it was declared,
// not because somebody happens to be in it: an instance is set up teams-first,
// and a group that cannot be empty cannot be prepared before the accounts
// arrive.

func groupStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "users.json"))
}

func names(list []GroupInfo) []string {
	out := make([]string, 0, len(list))
	for _, g := range list {
		out = append(out, g.Name)
	}
	return out
}

func TestAGroupCanBeCreatedEmpty(t *testing.T) {
	s := groupStore(t)

	if _, err := s.CreateGroup("payments", "Covers the overnight batch"); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	list, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "payments" {
		t.Fatalf("ListGroups = %v, want the group that was just made", names(list))
	}
	if !list[0].Declared {
		t.Error("a group that was created is not marked as declared")
	}
	if len(list[0].Members) != 0 {
		t.Errorf("Members = %v, want none", list[0].Members)
	}
	if list[0].Description != "Covers the overnight batch" {
		t.Errorf("Description = %q", list[0].Description)
	}

	// And it is offered everywhere a group name can be chosen, which is the
	// whole point of letting it exist while empty.
	plain, err := s.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 1 || plain[0] != "payments" {
		t.Fatalf("Groups = %v, want the empty group offered", plain)
	}
}

func TestAGroupNameIsTakenOnceAnythingUsesIt(t *testing.T) {
	s := groupStore(t)
	if _, err := s.CreateGroup("payments", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup("PAYMENTS", ""); !errors.Is(err, ErrGroupExists) {
		t.Errorf("creating a differently capitalised duplicate: %v, want ErrGroupExists", err)
	}

	// A name that exists only because an account carries it is a group on the
	// page already, so creating it again would produce a second row for a
	// group that is plainly there.
	if _, err := s.Add("alice", "correct-horse-battery", authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroups("alice", []string{"shipping"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup("shipping", ""); !errors.Is(err, ErrGroupExists) {
		t.Errorf("creating a group that is already in use: %v, want ErrGroupExists", err)
	}
}

func TestAGroupNameRefusesACommaAndOverlongText(t *testing.T) {
	if err := ValidateGroupName("payments, shipping"); err == nil {
		t.Error("a name containing a comma was accepted; it would be read back as two groups")
	}
	if err := ValidateGroupName("  padded  "); err == nil {
		t.Error("a name with surrounding space was accepted")
	}
	if err := ValidateGroupName(""); err == nil {
		t.Error("an empty name was accepted")
	}
	long := make([]byte, maxGroupNameLength+1)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateGroupName(string(long)); err == nil {
		t.Error("an overlong name was accepted")
	}
	if err := ValidateGroupName("payments"); err != nil {
		t.Errorf("an ordinary name was refused: %v", err)
	}
}

func TestRenamingAGroupCarriesItsMembersAndItsRole(t *testing.T) {
	s := groupStore(t)
	if _, err := s.Add("alice", "correct-horse-battery", authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup("payments", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetGroupMembers("payments", []string{"alice"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupRole("payments", authz.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if _, err := s.UpdateGroup("payments", strptr("Payments"), nil, false); err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}

	list, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListGroups = %v, want one group rather than an orphan beside it", names(list))
	}
	got := list[0]
	if got.Name != "Payments" {
		t.Errorf("Name = %q, want the new spelling", got.Name)
	}
	if len(got.Members) != 1 || got.Members[0] != "alice" {
		t.Errorf("Members = %v, want alice to have come with it", got.Members)
	}
	if got.Role != authz.RoleAdmin {
		t.Errorf("Role = %q, want the assignment to have come with it", got.Role)
	}

	// And the member is still an administrator, which is the thing a rename
	// that dropped the role would silently take away.
	alice := userNamed(t, s, "alice")
	roles, err := s.GroupRoles()
	if err != nil {
		t.Fatal(err)
	}
	if EffectiveRole(alice, roles) != authz.RoleAdmin {
		t.Error("the member lost the administrator role the group granted")
	}
}

func TestRenamingOntoAnExistingGroupIsRefused(t *testing.T) {
	s := groupStore(t)
	if _, err := s.CreateGroup("payments", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup("shipping", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateGroup("payments", strptr("shipping"), nil, false); !errors.Is(err, ErrGroupExists) {
		t.Errorf("renaming onto an existing group: %v, want ErrGroupExists", err)
	}
}

// A group that exists only because an account carries the name is still a
// group, and has to be maintainable — otherwise every instance from before
// declared groups, and every directory-fed group, is permanently read-only.
func TestAGroupInUseCanBeDescribedAndRenamed(t *testing.T) {
	s := groupStore(t)
	if _, err := s.Add("alice", "correct-horse-battery", authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroups("alice", []string{"shipping"}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Declared {
		t.Fatalf("ListGroups = %+v, want one undeclared group", list)
	}

	if _, err := s.UpdateGroup("shipping", strptr("Logistics"), strptr("Warehouse floor"), false); err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	list, err = s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "Logistics" || !list[0].Declared {
		t.Fatalf("ListGroups = %+v, want the group renamed and now declared", list)
	}
	if got := userNamed(t, s, "alice").Groups; len(got) != 1 || got[0] != "Logistics" {
		t.Errorf("alice's groups = %v, want the rename to have followed", got)
	}
}

func TestDeletingAGroupRemovesItEverywhere(t *testing.T) {
	s := groupStore(t)
	if _, err := s.Add("root", "correct-horse-battery", authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("alice", "correct-horse-battery", authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup("payments", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetGroupMembers("payments", []string{"alice"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupRole("payments", authz.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteGroup("PAYMENTS", false); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}

	list, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("ListGroups = %v, want none", names(list))
	}
	if got := userNamed(t, s, "alice").Groups; len(got) != 0 {
		t.Errorf("alice is still in %v", got)
	}
	roles, err := s.GroupRoles()
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 0 {
		t.Errorf("GroupRoles = %v, want the assignment gone with the group", roles)
	}

	if err := s.DeleteGroup("payments", false); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("deleting it twice: %v, want ErrGroupNotFound", err)
	}
}

// An instance nobody can administer can only be repaired by hand, so the guard
// that refuses demoting the last administrator has to cover the routes that go
// through a group as well.
func TestGroupChangesCannotStrandAnInstanceWithNoAdministrator(t *testing.T) {
	s := groupStore(t)
	if _, err := s.Add("alice", "correct-horse-battery", authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup("ops", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetGroupMembers("ops", []string{"alice"}, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupRole("ops", authz.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetGroupMembers("ops", nil, false); err == nil {
		t.Error("emptying the only group granting administration was allowed")
	}
	if err := s.DeleteGroup("ops", false); err == nil {
		t.Error("deleting the only group granting administration was allowed")
	}

	// With a second administrator in their own right, both become ordinary
	// changes again.
	if _, err := s.Add("root", "correct-horse-battery", authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetGroupMembers("ops", nil, false); err != nil {
		t.Errorf("emptying the group with another administrator present: %v", err)
	}
	if err := s.DeleteGroup("ops", false); err != nil {
		t.Errorf("deleting the group with another administrator present: %v", err)
	}
}

func TestSetGroupMembersReplacesTheWholeMembership(t *testing.T) {
	s := groupStore(t)
	for _, name := range []string{"alice", "bob", "carol"} {
		if _, err := s.Add(name, "correct-horse-battery", authz.RoleUser, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateGroup("payments", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetGroupMembers("payments", []string{"alice", "bob"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetGroupMembers("payments", []string{"BOB", "carol"}, false); err != nil {
		t.Fatal(err)
	}

	group, err := s.Group("payments")
	if err != nil {
		t.Fatal(err)
	}
	if len(group.Members) != 2 || group.Members[0] != "bob" || group.Members[1] != "carol" {
		t.Errorf("Members = %v, want bob and carol", group.Members)
	}
	// Membership of other groups is untouched by a change to this one.
	if got := userNamed(t, s, "alice").Groups; len(got) != 0 {
		t.Errorf("alice = %v, want removed", got)
	}
}

// Writing to an account the directory owns would look like it worked until the
// next sign-in overwrote it, so it is left alone and reported.
func TestDirectoryOwnedAccountsAreLeftAloneWhenLocked(t *testing.T) {
	s := groupStore(t)
	if _, err := s.UpsertExternal("https://idp.example", "sub-1", "erin", authz.RoleUser, []string{"payments"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("alice", "correct-horse-battery", authz.RoleUser, false); err != nil {
		t.Fatal(err)
	}

	skipped, err := s.SetGroupMembers("payments", []string{"alice"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0] != "erin" {
		t.Fatalf("skipped = %v, want erin reported rather than silently changed", skipped)
	}
	group, err := s.Group("payments")
	if err != nil {
		t.Fatal(err)
	}
	if len(group.Members) != 2 {
		t.Errorf("Members = %v, want erin kept and alice added", group.Members)
	}

	// Unlocked — a deployment that maps no group claim — and the same call
	// removes them, because there is nothing to overwrite it.
	if _, err := s.SetGroupMembers("payments", []string{"alice"}, false); err != nil {
		t.Fatal(err)
	}
	group, err = s.Group("payments")
	if err != nil {
		t.Fatal(err)
	}
	if len(group.Members) != 1 || group.Members[0] != "alice" {
		t.Errorf("Members = %v, want only alice", group.Members)
	}
}

func strptr(s string) *string { return &s }

func userNamed(t *testing.T, s *Store, name string) User {
	t.Helper()
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range list {
		if u.Username == name {
			return u
		}
	}
	t.Fatalf("no account named %q", name)
	return User{}
}

// A group the directory feeds is the provider's to name.
//
// Renaming or deleting one locally does not remove it: the claim is replayed
// at the member's next sign-in, which recreates the original spelling with the
// membership on it and leaves whatever an administrator attached to the new
// name stranded beside it. Where the original granted administration, that is
// somebody's access on a group the page has stopped showing.
func TestDirectoryOwnedGroupNamesCannotBeRenamedOrDeletedWhenLocked(t *testing.T) {
	s := groupStore(t)
	if _, err := s.Add("root", "correct-horse-battery", authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertExternal("https://idp.example", "sub-1", "alice",
		authz.RoleUser, []string{"ops"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupRole("ops", authz.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if _, err := s.UpdateGroup("ops", strptr("operations"), nil, true); !errors.Is(err, ErrProviderManagedGroup) {
		t.Errorf("renaming a directory-fed group: %v, want ErrProviderManagedGroup", err)
	}
	// Capitals only is still a rename: the directory sends the original
	// spelling back either way.
	if _, err := s.UpdateGroup("ops", strptr("Ops"), nil, true); !errors.Is(err, ErrProviderManagedGroup) {
		t.Errorf("re-capitalising a directory-fed group: %v, want ErrProviderManagedGroup", err)
	}
	if err := s.DeleteGroup("ops", true); !errors.Is(err, ErrProviderManagedGroup) {
		t.Errorf("deleting a directory-fed group: %v, want ErrProviderManagedGroup", err)
	}

	// Nothing moved: the group is there, spelled as it was, with its member
	// and its role, and no second group beside it.
	list, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "ops" {
		t.Fatalf("ListGroups = %v, want the group unchanged and alone", names(list))
	}
	if len(list[0].Members) != 1 || list[0].Members[0] != "alice" {
		t.Errorf("Members = %v, want the directory's membership intact", list[0].Members)
	}
	if len(list[0].ExternalMembers) != 1 || list[0].ExternalMembers[0] != "alice" {
		t.Errorf("ExternalMembers = %v, want alice named as the directory's", list[0].ExternalMembers)
	}
	if list[0].Role != authz.RoleAdmin {
		t.Errorf("Role = %q, want the role assignment untouched", list[0].Role)
	}

	// What is local stays local: the description is an administrator's note
	// about the group, not something the directory has an opinion on.
	if _, err := s.UpdateGroup("ops", nil, strptr("Overnight cover"), true); err != nil {
		t.Errorf("describing a directory-fed group: %v, want it allowed", err)
	}
	if _, err := s.UpdateGroup("ops", strptr("ops"), strptr("Overnight cover"), true); err != nil {
		t.Errorf("re-sending the same name with a description: %v, want it allowed", err)
	}
	if err := s.SetGroupRole("ops", authz.RoleUser); err != nil {
		t.Errorf("changing the role a directory-fed group grants: %v, want it allowed", err)
	}

	// And without the lock — a deployment that maps no groups claim — the name
	// is an administrator's again.
	if _, err := s.UpdateGroup("ops", strptr("operations"), nil, false); err != nil {
		t.Errorf("renaming with no groups claim mapped: %v, want it allowed", err)
	}
}

// The lock is about the directory's members, not about the group having any.
// A group with only local accounts in it stays fully local even where a groups
// claim is mapped.
func TestALocalGroupIsStillRenamableWhereAGroupsClaimIsMapped(t *testing.T) {
	s := groupStore(t)
	if _, err := s.Add("root", "correct-horse-battery", authz.RoleAdmin, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGroup("payments", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetGroupMembers("payments", []string{"root"}, true); err != nil {
		t.Fatal(err)
	}

	if _, err := s.UpdateGroup("payments", strptr("Payments"), nil, true); err != nil {
		t.Errorf("renaming a group of local accounts: %v, want it allowed", err)
	}
	if err := s.DeleteGroup("Payments", true); err != nil {
		t.Errorf("deleting a group of local accounts: %v, want it allowed", err)
	}
}
