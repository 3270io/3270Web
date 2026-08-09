package users

// Groups as something an administrator maintains.
//
// A group used to exist only for as long as somebody was in it: there was no
// record of one anywhere, just the same name repeated on however many
// accounts happened to carry it. That is enough to *use* a group and not
// nearly enough to *run* one. An administrator setting an instance up has the
// teams before they have the people — the host list is written first, and the
// accounts arrive over the following week — and a group that cannot exist
// while it is empty cannot be prepared in advance, cannot be renamed once
// somebody spells it two ways, and cannot be removed except by visiting every
// account that mentions it.
//
// So a group now has a record of its own: a name, a description, and the date
// it was declared. Membership still lives on the account, because that is
// where it is read from on every request and duplicating it would give the
// two copies a chance to disagree. The record is what makes the name a thing
// rather than a coincidence.
//
// Groups that exist only because an account or a role assignment mentions
// them are still groups — an instance that has been running since before this
// file, and every directory-fed group, is exactly that — so they are listed
// alongside declared ones and can be renamed, described and deleted the same
// way. The Declared flag is the only difference, and it is there to be shown,
// not to gate anything.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jnnngs/3270Web/internal/authz"
)

// Errors group management distinguishes between.
var (
	ErrGroupExists   = errors.New("users: a group with that name already exists")
	ErrGroupNotFound = errors.New("users: no such group")
)

// MaxGroupDescriptionLength bounds the note beside a group name. Long enough
// for "the people who cover the overnight batch", short enough to sit in a
// table cell.
//
// Exported alongside MaxGroupNameLength so the page that types these can bound
// its own inputs to the same numbers. A form that accepts more than the store
// does is a refusal the person only meets after filling the whole dialog in.
const MaxGroupDescriptionLength = 200

// MaxGroupNameLength is the longest a group name may be.
const MaxGroupNameLength = maxGroupNameLength

// Group is a team an administrator has declared.
type Group struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// GroupInfo is a group together with what the administration page has to show
// beside it: who is in it, what role it grants, and whether it has a record of
// its own or exists only because something mentions it.
type GroupInfo struct {
	Group
	Members  []string   `json:"members"`
	Role     authz.Role `json:"role"`
	Declared bool       `json:"declared"`
}

// ValidateGroupName enforces a name that survives the round trip through a
// comma-separated field, a directory claim and a profile audience.
//
// The comma is refused because it is the separator every list of groups in
// this application uses; a name containing one would be saved whole and read
// back as two, and the profile it was written into would quietly stop matching
// anybody.
func ValidateGroupName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("users: a group name is required")
	}
	if name != strings.TrimSpace(name) {
		return errors.New("users: group name must not have leading or trailing spaces")
	}
	if len(name) > maxGroupNameLength {
		return fmt.Errorf("users: group name is longer than %d characters", maxGroupNameLength)
	}
	if strings.Contains(name, ",") {
		return errors.New("users: group name must not contain a comma — commas separate one group from the next")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("users: group name must not contain control characters")
		}
	}
	return nil
}

func validateGroupDescription(description string) error {
	if len(description) > MaxGroupDescriptionLength {
		return fmt.Errorf("users: group description is longer than %d characters", MaxGroupDescriptionLength)
	}
	for _, r := range description {
		if unicode.IsControl(r) {
			return errors.New("users: group description must not contain control characters")
		}
	}
	return nil
}

// groupIndex finds the declared record for a name, or -1.
func (f fileFormat) groupIndex(name string) int {
	for i := range f.Groups {
		if strings.EqualFold(strings.TrimSpace(f.Groups[i].Name), strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// knowsGroup reports whether the name is a group at all: declared, granted a
// role, or carried by an account. All three make the name appear on the groups
// page, so all three have to count as "taken" when one is created.
func (f fileFormat) knowsGroup(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if f.groupIndex(name) >= 0 {
		return true
	}
	for g := range f.GroupRoles {
		if strings.EqualFold(strings.TrimSpace(g), name) {
			return true
		}
	}
	for _, u := range f.Users {
		if u.InGroup(name) {
			return true
		}
	}
	return false
}

func sortGroups(list []Group) {
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
}

// ListGroups returns every group this instance knows about, sorted by name.
//
// Declared records and names that exist only because something mentions them
// are returned together, because an administrator looking at the page is
// asking "what teams are there", not "which of them did somebody type into
// this particular field first".
func (s *Store) ListGroups() ([]GroupInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	return groupInfos(f), nil
}

// Group returns one group by name.
func (s *Store) Group(name string) (GroupInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return GroupInfo{}, err
	}
	if !f.knowsGroup(name) {
		return GroupInfo{}, ErrGroupNotFound
	}
	for _, info := range groupInfos(f) {
		if strings.EqualFold(info.Name, strings.TrimSpace(name)) {
			return info, nil
		}
	}
	return GroupInfo{}, ErrGroupNotFound
}

// groupInfos assembles the view of every known group from one loaded file.
func groupInfos(f fileFormat) []GroupInfo {
	// Keyed on the lowercased name, because the same team arrives spelled
	// three ways — an administrator's typing, a directory claim and a profile
	// audience — and they are one group.
	byKey := make(map[string]*GroupInfo)
	order := make([]string, 0, len(f.Groups))

	touch := func(name string) *GroupInfo {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		if key == "" {
			return nil
		}
		if existing, ok := byKey[key]; ok {
			return existing
		}
		info := &GroupInfo{Group: Group{Name: name}}
		byKey[key] = info
		order = append(order, key)
		return info
	}

	for _, g := range f.Groups {
		info := touch(g.Name)
		if info == nil {
			continue
		}
		// The declared record owns the spelling: it is the one somebody chose
		// on purpose, rather than the one that happened to be typed first.
		info.Group = g
		info.Declared = true
	}
	for g, role := range f.GroupRoles {
		if info := touch(g); info != nil {
			info.Role = role
		}
	}
	for _, u := range f.Users {
		for _, g := range u.Groups {
			if info := touch(g); info != nil {
				info.Members = append(info.Members, u.Username)
			}
		}
	}

	out := make([]GroupInfo, 0, len(order))
	for _, key := range order {
		info := byKey[key]
		if info.Role == "" {
			info.Role = authz.RoleUser
		}
		sort.Slice(info.Members, func(i, j int) bool {
			return strings.ToLower(info.Members[i]) < strings.ToLower(info.Members[j])
		})
		out = append(out, *info)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// CreateGroup declares a group. It may be empty: preparing the teams before
// the accounts arrive is the ordinary way an instance is set up, and a group
// that only exists once somebody is in it cannot be prepared at all.
func (s *Store) CreateGroup(name, description string) (Group, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if err := ValidateGroupName(name); err != nil {
		return Group{}, err
	}
	if err := validateGroupDescription(description); err != nil {
		return Group{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return Group{}, err
	}
	// Taken counts a name that exists only because an account carries it: the
	// groups page already lists it, so "create" would produce a second row for
	// a group that is plainly there.
	if f.knowsGroup(name) {
		return Group{}, ErrGroupExists
	}

	g := Group{Name: name, Description: description, CreatedAt: s.now()}
	f.Groups = append(f.Groups, g)
	sortGroups(f.Groups)
	if err := s.save(f); err != nil {
		return Group{}, err
	}
	return g, nil
}

// UpdateGroup renames a group or changes its description.
//
// A rename follows the name everywhere this package holds it — every
// membership and the role assignment — so the group keeps its members and its
// role rather than becoming a new empty group beside an orphaned old one.
// Names this package does not hold (a published host preset's audience) are
// the caller's to follow through; see the handler.
//
// Describing a group that was never declared declares it. That is how the
// groups an instance already had, and the ones a directory feeds it, become
// maintainable rather than permanently second-class.
func (s *Store) UpdateGroup(name string, newName, description *string) (Group, error) {
	name = strings.TrimSpace(name)

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return Group{}, err
	}
	if !f.knowsGroup(name) {
		return Group{}, ErrGroupNotFound
	}

	final := name
	if newName != nil {
		candidate := strings.TrimSpace(*newName)
		if err := ValidateGroupName(candidate); err != nil {
			return Group{}, err
		}
		// A rename that only changes capitals is a rename: "sales" to "Sales"
		// is somebody tidying up, and refusing it as a collision with itself
		// would be baffling.
		if !strings.EqualFold(candidate, name) && f.knowsGroup(candidate) {
			return Group{}, ErrGroupExists
		}
		final = candidate
	}
	if description != nil {
		if err := validateGroupDescription(strings.TrimSpace(*description)); err != nil {
			return Group{}, err
		}
	}

	// Covers a change of capitals too: "sales" to "Sales" is a rename, and the
	// membership entries have to follow it or the group keeps two spellings.
	if final != name {
		renameGroupEverywhere(&f, name, final)
	}

	idx := f.groupIndex(final)
	if idx < 0 {
		f.Groups = append(f.Groups, Group{Name: final, CreatedAt: s.now()})
		idx = f.groupIndex(final)
	}
	f.Groups[idx].Name = final
	if description != nil {
		f.Groups[idx].Description = strings.TrimSpace(*description)
	}
	if f.Groups[idx].CreatedAt.IsZero() {
		f.Groups[idx].CreatedAt = s.now()
	}
	sortGroups(f.Groups)

	if err := s.save(f); err != nil {
		return Group{}, err
	}
	return f.Groups[f.groupIndex(final)], nil
}

// renameGroupEverywhere rewrites the name on every membership and on the role
// assignment. Nothing is validated here; UpdateGroup has already done it.
func renameGroupEverywhere(f *fileFormat, from, to string) {
	for i := range f.Users {
		changed := false
		for j, g := range f.Users[i].Groups {
			if strings.EqualFold(strings.TrimSpace(g), from) {
				f.Users[i].Groups[j] = to
				changed = true
			}
		}
		if changed {
			f.Users[i].Groups = NormaliseGroups(f.Users[i].Groups)
		}
	}
	if len(f.GroupRoles) > 0 {
		next := make(map[string]authz.Role, len(f.GroupRoles))
		for g, r := range f.GroupRoles {
			if strings.EqualFold(strings.TrimSpace(g), from) {
				next[to] = r
				continue
			}
			next[g] = r
		}
		f.GroupRoles = next
	}
	for i := range f.Groups {
		if strings.EqualFold(strings.TrimSpace(f.Groups[i].Name), from) {
			f.Groups[i].Name = to
		}
	}
}

// DeleteGroup removes the group: its record, its role assignment and every
// membership of it.
//
// Refused when it would leave the instance with no enabled administrator, for
// the same reason demoting the last one is: an instance nobody can administer
// can only be repaired by editing the account file by hand.
func (s *Store) DeleteGroup(name string) error {
	name = strings.TrimSpace(name)

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	if !f.knowsGroup(name) {
		return ErrGroupNotFound
	}

	next := f
	next.Users = make([]User, len(f.Users))
	copy(next.Users, f.Users)
	for i := range next.Users {
		next.Users[i].Groups = withoutGroup(next.Users[i].Groups, name)
	}
	next.GroupRoles = withoutGroupRole(f.GroupRoles, name)

	if countEffectiveAdmins(f.Users, f.GroupRoles) > 0 &&
		countEffectiveAdmins(next.Users, next.GroupRoles) == 0 {
		return errors.New("users: refusing to delete the group that grants the only enabled admin")
	}

	kept := make([]Group, 0, len(f.Groups))
	for _, g := range f.Groups {
		if strings.EqualFold(strings.TrimSpace(g.Name), name) {
			continue
		}
		kept = append(kept, g)
	}
	next.Groups = kept
	if len(next.Groups) == 0 {
		next.Groups = nil
	}

	return s.save(next)
}

// SetGroupMembers replaces the group's membership with the named accounts.
//
// Membership is stored on each account, so this is a fan-out write rather than
// a single field — which is exactly why it belongs here and not in a caller
// looping over accounts: the last-administrator guard has to see the whole
// result before any of it is saved.
//
// lockExternal leaves accounts an identity provider owns alone and returns
// their names. Writing to one would look like it worked until their next
// sign-in overwrote it, and a change that silently reverts is worse than one
// that is refused out loud.
func (s *Store) SetGroupMembers(group string, usernames []string, lockExternal bool) (skipped []string, err error) {
	group = strings.TrimSpace(group)

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	if !f.knowsGroup(group) {
		return nil, ErrGroupNotFound
	}

	wanted := make(map[string]bool, len(usernames))
	for _, name := range usernames {
		name = strings.TrimSpace(name)
		if name != "" {
			wanted[strings.ToLower(name)] = true
		}
	}

	next := f
	next.Users = make([]User, len(f.Users))
	copy(next.Users, f.Users)

	changed := false
	for i := range next.Users {
		member := next.Users[i].InGroup(group)
		want := wanted[strings.ToLower(next.Users[i].Username)]
		if member == want {
			continue
		}
		if lockExternal && next.Users[i].External() {
			skipped = append(skipped, next.Users[i].Username)
			continue
		}
		if want {
			next.Users[i].Groups = NormaliseGroups(append(append([]string{}, next.Users[i].Groups...), group))
		} else {
			next.Users[i].Groups = withoutGroup(next.Users[i].Groups, group)
		}
		changed = true
	}

	if !changed {
		return skipped, nil
	}
	if countEffectiveAdmins(f.Users, f.GroupRoles) > 0 &&
		countEffectiveAdmins(next.Users, next.GroupRoles) == 0 {
		return nil, errors.New("users: refusing to remove the membership that keeps the only enabled admin")
	}

	if err := s.save(next); err != nil {
		return nil, err
	}
	return skipped, nil
}

// withoutGroup returns the list with the named group removed.
func withoutGroup(list []string, name string) []string {
	out := make([]string, 0, len(list))
	for _, g := range list {
		if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(name)) {
			continue
		}
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// withoutGroupRole returns the assignments with the named group's removed.
func withoutGroupRole(roles map[string]authz.Role, name string) map[string]authz.Role {
	if len(roles) == 0 {
		return nil
	}
	out := make(map[string]authz.Role, len(roles))
	for g, r := range roles {
		if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(name)) {
			continue
		}
		out[g] = r
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
