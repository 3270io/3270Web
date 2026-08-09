// Package users stores local accounts and verifies their passwords.
//
// This is the account database for AUTH_MODE=local: a JSON file the server
// owns, with no external directory service to reach. That choice is what makes
// authentication usable on an isolated network, which is where 3270Web
// commonly runs.
package users

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jnnngs/3270Web/internal/authz"
)

// Errors callers distinguish between.
var (
	// ErrInvalidCredentials covers both "no such user" and "wrong password".
	// They are deliberately one error: telling a caller which of the two
	// happened turns the login form into a list of valid usernames.
	ErrInvalidCredentials = errors.New("users: invalid username or password")
	ErrUserExists         = errors.New("users: a user with that name already exists")
	ErrUserNotFound       = errors.New("users: no such user")
	ErrUserDisabled       = errors.New("users: account is disabled")
)

// MinPasswordLength is the shortest password the store will accept.
//
// Length is the only property enforced. Composition rules ("one digit, one
// symbol") shrink the search space more than they enlarge it and push people
// towards predictable substitutions; a length floor does not.
const MinPasswordLength = 12

// MaxPasswordLength bounds the input so a huge body cannot turn one login
// attempt into an expensive hash.
const MaxPasswordLength = 1024

// maxUsernameLength keeps a username usable as a display string and a log
// field.
const maxUsernameLength = 64

// User is one local account. It is safe to hand to a template or a log except
// for PasswordHash, which Redacted() strips.
type User struct {
	// ID is an opaque, stable identifier. It is what ownership labels and
	// per-user directories key on, so it must not be the username: people get
	// renamed, and a name is not safe as a path component.
	ID       string     `json:"id"`
	Username string     `json:"username"`
	Role     authz.Role `json:"role"`
	// PasswordHash is a PHC-format Argon2id string. Empty on an account that
	// signs in through an identity provider: there is no local password to
	// hash, and an empty hash verifies nothing rather than verifying anything.
	PasswordHash string `json:"passwordHash"`
	// Issuer and Subject link the account to an identity provider. Both empty
	// on a local account.
	//
	// The pair is the identity, not the username: a directory may rename
	// somebody, and the subject claim is the one thing an issuer promises not
	// to reuse or recycle. Matching on the name instead would hand a renamed
	// person a brand-new account — and, worse, hand the next person given that
	// name the previous holder's saved work.
	Issuer  string `json:"issuer,omitempty"`
	Subject string `json:"subject,omitempty"`
	// Groups the account belongs to. Groups say nothing about permission on
	// their own — the role does that — and exist so that what somebody may
	// reach can be decided for a team rather than a person at a time.
	//
	// On an account that signs in through an identity provider these are the
	// directory's own groups, refreshed at each sign-in where the deployment
	// maps a claim to them. That is the point: the team somebody is on is
	// already recorded somewhere, and it is not here.
	Groups   []string `json:"groups,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
	// MustChangePassword marks an account whose password was issued by the
	// system rather than chosen by its owner.
	MustChangePassword bool      `json:"mustChangePassword,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	PasswordChangedAt  time.Time `json:"passwordChangedAt"`
}

// Redacted returns a copy with the hash removed, for logging or serving.
func (u User) Redacted() User {
	u.PasswordHash = ""
	return u
}

// External reports whether the account is authenticated by an identity
// provider rather than by a password held here.
func (u User) External() bool { return u.Issuer != "" && u.Subject != "" }

// InGroup reports whether the account belongs to the named group.
//
// Compared without regard to case, because a group name arrives from three
// places — an administrator typing it, a directory claim, and a profile's
// audience — and none of them agrees with the others about capitals.
func (u User) InGroup(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, g := range u.Groups {
		if strings.EqualFold(strings.TrimSpace(g), name) {
			return true
		}
	}
	return false
}

// maxGroupsPerUser and maxGroupNameLength bound what a directory claim can
// write into the account file. A provider is not this server's to trust with
// unbounded input, however friendly it is.
const (
	maxGroupsPerUser   = 64
	maxGroupNameLength = 64
)

// NormaliseGroups trims, de-duplicates and bounds a group list.
//
// Exported because the same cleaning has to happen to a list an administrator
// typed and to one an identity provider sent, and two implementations of
// "what counts as the same group" is exactly how a profile audience stops
// matching the account it was written for.
func NormaliseGroups(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		name := strings.TrimSpace(raw)
		if name == "" || len(name) > maxGroupNameLength {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
		if len(out) >= maxGroupsPerUser {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Principal converts the account into the actor used for authorization.
func (u User) Principal(kind authz.Kind) authz.Principal {
	return authz.Principal{UserID: u.ID, Role: u.Role, Kind: kind}
}

// Store is the on-disk account database.
//
// Every mutation reads the file, applies the change and writes it back under a
// single lock. The file is small and writes are rare — a login does not write
// — so the simplicity is worth more here than avoiding the re-read.
type Store struct {
	mu   sync.Mutex
	path string
	// now is overridable in tests.
	now func() time.Time
}

// NewStore opens the account database at path. The file is created on first
// write, not here, so constructing a Store never has a side effect.
func NewStore(path string) *Store {
	return &Store{path: path, now: time.Now}
}

// Path reports where the store persists.
func (s *Store) Path() string { return s.path }

type fileFormat struct {
	Users []User `json:"users"`
	// GroupRoles maps a group name to the role its members inherit. An account
	// holds the stronger of its own role and any granted by a group it is in;
	// see EffectiveRole. Kept beside the accounts rather than in a file of its
	// own so a backup of one cannot silently disagree with the other.
	GroupRoles map[string]authz.Role `json:"groupRoles,omitempty"`
}

// load reads the file. A missing file is an empty store, not an error: that is
// the state before the first account is created.
func (s *Store) load() (fileFormat, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileFormat{}, nil
		}
		return fileFormat{}, fmt.Errorf("read user store: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return fileFormat{}, nil
	}
	var parsed fileFormat
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fileFormat{}, fmt.Errorf("parse user store: %w", err)
	}
	return parsed, nil
}

// save writes the file atomically at 0600.
//
// A torn write here would lock every account out at once, so the temp-file
// and rename dance is not optional.
func (s *Store) save(f fileFormat) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create user store dir: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode user store: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".users-*")
	if err != nil {
		return fmt.Errorf("create temp user store: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp user store: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp user store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp user store: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp user store: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace user store: %w", err)
	}
	return nil
}

// List returns every account with hashes stripped.
func (s *Store) List() ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(f.Users))
	for _, u := range f.Users {
		out = append(out, u.Redacted())
	}
	return out, nil
}

// Count reports how many accounts exist. Used to detect first run.
func (s *Store) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return 0, err
	}
	return len(f.Users), nil
}

// ByID looks up an account. The hash is stripped.
func (s *Store) ByID(id string) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return User{}, false, err
	}
	for _, u := range f.Users {
		if u.ID == id {
			return u.Redacted(), true, nil
		}
	}
	return User{}, false, nil
}

// Add creates an account and returns it with the hash stripped.
func (s *Store) Add(username, password string, role authz.Role, mustChange bool) (User, error) {
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	if role != authz.RoleAdmin && role != authz.RoleUser {
		return User{}, fmt.Errorf("users: unknown role %q", role)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return User{}, err
	}
	for _, u := range f.Users {
		if strings.EqualFold(u.Username, username) {
			return User{}, ErrUserExists
		}
	}

	id, err := newID()
	if err != nil {
		return User{}, err
	}
	now := s.now()
	u := User{
		ID:                 id,
		Username:           username,
		Role:               role,
		PasswordHash:       hash,
		MustChangePassword: mustChange,
		CreatedAt:          now,
		PasswordChangedAt:  now,
	}
	f.Users = append(f.Users, u)
	if err := s.save(f); err != nil {
		return User{}, err
	}
	return u.Redacted(), nil
}

// ErrNotFirstUser is returned when AddFirstAdmin runs against a store that
// already has accounts.
var ErrNotFirstUser = errors.New("users: accounts already exist")

// ErrNameHeldLocally is returned when an identity provider offers a username
// that a local account already holds.
var ErrNameHeldLocally = errors.New("users: a local account already uses that name")

// UpsertExternal resolves an identity provider's user into a local account,
// creating it on first sign-in and keeping it current afterwards.
//
// The account is found by issuer and subject, never by name. A directory
// renames people, and the subject is the one claim an issuer promises not to
// recycle; matching on the name would give a renamed person an empty new
// account, and would eventually give somebody else's saved work to whoever
// inherited their username.
//
// role empty means "leave it alone" — a new account gets RoleUser, an existing
// one keeps whatever it has. That is what makes an administrator's promotion
// on the Accounts page survive the next sign-in on a deployment that has not
// mapped roles from a claim. Where a claim is mapped, it is passed in and wins
// every time, which is the point of managing roles centrally.
func (s *Store) UpsertExternal(issuer, subject, username string, role authz.Role, groups []string) (User, error) {
	issuer = strings.TrimSpace(issuer)
	subject = strings.TrimSpace(subject)
	if issuer == "" || subject == "" {
		return User{}, errors.New("users: an external account needs both an issuer and a subject")
	}
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if role != "" && role != authz.RoleAdmin && role != authz.RoleUser {
		return User{}, fmt.Errorf("users: unknown role %q", role)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return User{}, err
	}
	list := f.Users

	for i := range list {
		if list[i].Issuer != issuer || list[i].Subject != subject {
			continue
		}
		changed := false
		// A rename at the provider follows through to here, unless the new name
		// is one somebody else already answers to — in which case the account
		// keeps the name it had rather than two accounts sharing one.
		if !strings.EqualFold(list[i].Username, username) && indexOfUsername(list, username) < 0 {
			list[i].Username = username
			changed = true
		}
		// nil means the deployment maps no group claim, so whatever an
		// administrator set here is left alone. A non-nil empty list is
		// different: it means the directory says this person is in nothing.
		if groups != nil {
			cleaned := NormaliseGroups(groups)
			if !sameGroups(list[i].Groups, cleaned) {
				list[i].Groups = cleaned
				changed = true
			}
		}
		if role != "" && list[i].Role != role {
			// Demoting the last administrator is refused here as everywhere: an
			// instance nobody can administer can only be repaired by hand.
			if !(role == authz.RoleUser && list[i].Role == authz.RoleAdmin &&
				!list[i].Disabled && wouldLoseLastAdmin(f, i, func(u *User) { u.Role = authz.RoleUser })) {
				list[i].Role = role
				changed = true
			}
		}
		if changed {
			if err := s.save(f); err != nil {
				return User{}, err
			}
		}
		return list[i].Redacted(), nil
	}

	// First sign-in. A name a local account already holds is refused rather
	// than linked: linking would let anyone who can be named in the directory
	// take over a local account, including the break-glass administrator.
	if indexOfUsername(list, username) >= 0 {
		return User{}, ErrNameHeldLocally
	}

	id, err := newID()
	if err != nil {
		return User{}, err
	}
	if role == "" {
		role = authz.RoleUser
	}
	now := s.now()
	u := User{
		ID:       id,
		Username: username,
		Role:     role,
		Issuer:   issuer,
		Subject:  subject,
		Groups:   NormaliseGroups(groups),
		// No PasswordHash. The account has no local password, and Authenticate
		// refuses one rather than treating an empty hash as a match.
		CreatedAt:         now,
		PasswordChangedAt: now,
	}
	f.Users = append(f.Users, u)
	if err := s.save(f); err != nil {
		return User{}, err
	}
	return u.Redacted(), nil
}

// AddFirstAdmin creates the initial administrator, but only while the store is
// empty.
//
// The emptiness check and the write happen under one lock. First-run setup is
// reachable without credentials by design, so two requests arriving together
// must not both succeed — the second would be an unauthenticated stranger
// adding themselves as an administrator to an instance that already has one.
func (s *Store) AddFirstAdmin(username, password string) (User, error) {
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return User{}, err
	}
	if len(f.Users) > 0 {
		return User{}, ErrNotFirstUser
	}

	id, err := newID()
	if err != nil {
		return User{}, err
	}
	now := s.now()
	u := User{
		ID:                id,
		Username:          username,
		Role:              authz.RoleAdmin,
		PasswordHash:      hash,
		CreatedAt:         now,
		PasswordChangedAt: now,
	}
	f.Users = []User{u}
	if err := s.save(f); err != nil {
		return User{}, err
	}
	return u.Redacted(), nil
}

// SetRole changes an account's role.
//
// Demoting the last enabled administrator is refused for the same reason
// disabling them is: it leaves an instance nobody can administer.
func (s *Store) SetRole(username string, role authz.Role) error {
	if role != authz.RoleAdmin && role != authz.RoleUser {
		return fmt.Errorf("users: unknown role %q", role)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	list := f.Users

	idx := indexOfUsername(list, username)
	if idx < 0 {
		return ErrUserNotFound
	}
	if list[idx].Role == role {
		return nil
	}
	if role == authz.RoleUser && list[idx].Role == authz.RoleAdmin && !list[idx].Disabled {
		if wouldLoseLastAdmin(f, idx, func(u *User) { u.Role = authz.RoleUser }) {
			return errors.New("users: refusing to demote the only enabled admin")
		}
	}

	list[idx].Role = role
	return s.save(f)
}

// Delete removes an account permanently.
func (s *Store) Delete(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	list := f.Users

	idx := indexOfUsername(list, username)
	if idx < 0 {
		return ErrUserNotFound
	}
	if !list[idx].Disabled && wouldLoseLastAdmin(f, idx, func(u *User) { u.Disabled = true }) {
		return errors.New("users: refusing to delete the only enabled admin")
	}

	f.Users = append(list[:idx:idx], list[idx+1:]...)
	return s.save(f)
}

func indexOfUsername(list []User, username string) int {
	for i := range list {
		if strings.EqualFold(list[i].Username, username) {
			return i
		}
	}
	return -1
}

func countEnabledAdmins(list []User) int {
	n := 0
	for _, u := range list {
		if u.Role == authz.RoleAdmin && !u.Disabled {
			n++
		}
	}
	return n
}

// wouldLoseLastAdmin reports whether applying change to the account at idx
// takes the store from having an enabled administrator to having none —
// counting the roles that groups grant, so an instance that runs its
// administrators through a group can still demote the one direct
// administrator it started with. A store that already has no administrator
// (a test fixture, a half-built instance) is not made worse by any change,
// so nothing is refused there.
func wouldLoseLastAdmin(f fileFormat, idx int, change func(*User)) bool {
	if countEffectiveAdmins(f.Users, f.GroupRoles) == 0 {
		return false
	}
	trial := make([]User, len(f.Users))
	copy(trial, f.Users)
	change(&trial[idx])
	return countEffectiveAdmins(trial, f.GroupRoles) == 0
}

// Authenticate verifies a username and password.
//
// A missing user still costs a full hash comparison. Returning early would
// make "no such user" measurably faster than "wrong password", which turns the
// login endpoint into an oracle for which accounts exist.
func (s *Store) Authenticate(username, password string) (User, error) {
	s.mu.Lock()
	f, err := s.load()
	s.mu.Unlock()
	if err != nil {
		return User{}, err
	}
	list := f.Users

	var found *User
	for i := range list {
		if strings.EqualFold(list[i].Username, username) {
			found = &list[i]
			break
		}
	}

	// An account with no local password cannot be signed in with one. Spending
	// the decoy's work first, as for a missing account: answering an external
	// account instantly would make the login form a way to ask which names are
	// managed by the identity provider.
	if found == nil || found.PasswordHash == "" {
		// Compare against a real hash of a throwaway value so the work done
		// matches the found-user path.
		_, _ = VerifyPassword(decoyHash(), password)
		return User{}, ErrInvalidCredentials
	}

	ok, err := VerifyPassword(found.PasswordHash, password)
	if err != nil || !ok {
		return User{}, ErrInvalidCredentials
	}
	// Checked after the password so a disabled account is not distinguishable
	// from a wrong password without knowing the password.
	if found.Disabled {
		return User{}, ErrUserDisabled
	}
	return found.Redacted(), nil
}

// SetPassword replaces an account's password and clears the
// must-change marker.
func (s *Store) SetPassword(username, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	list := f.Users
	for i := range list {
		if strings.EqualFold(list[i].Username, username) {
			// Giving an identity provider's account a local password would add
			// a second way in that the provider knows nothing about, and so
			// cannot disable when it disables the person.
			if list[i].External() {
				return ErrExternalAccount
			}
			list[i].PasswordHash = hash
			list[i].PasswordChangedAt = s.now()
			list[i].MustChangePassword = false
			return s.save(f)
		}
	}
	return ErrUserNotFound
}

// ErrExternalAccount is returned by operations that only make sense on an
// account with a local password.
var ErrExternalAccount = errors.New("users: this account signs in through the identity provider")

// SetGroups replaces an account's group membership.
func (s *Store) SetGroups(username string, groups []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	idx := indexOfUsername(f.Users, username)
	if idx < 0 {
		return ErrUserNotFound
	}
	f.Users[idx].Groups = NormaliseGroups(groups)
	return s.save(f)
}

// Groups returns every group name any account belongs to, sorted.
//
// There is no separate registry of groups: a group exists because somebody is
// in it. A registry would be a second place for a name to live and a second
// way for it to be spelled, and the only thing it would add is the ability to
// create an empty group — which reaches nobody by definition.
func (s *Store) Groups() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]string)
	// A group with a role assigned exists even while nobody is in it: the
	// assignment is the membership that keeps the name alive.
	for g := range f.GroupRoles {
		key := strings.ToLower(strings.TrimSpace(g))
		if key != "" && seen[key] == "" {
			seen[key] = strings.TrimSpace(g)
		}
	}
	for _, u := range f.Users {
		for _, g := range u.Groups {
			key := strings.ToLower(strings.TrimSpace(g))
			if key != "" && seen[key] == "" {
				seen[key] = strings.TrimSpace(g)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for _, name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// SetDisabled enables or disables an account.
//
// Disabling the last enabled admin is refused: an instance with no way to
// administer it can only be repaired by editing the file by hand.
func (s *Store) SetDisabled(username string, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	list := f.Users

	idx := indexOfUsername(list, username)
	if idx < 0 {
		return ErrUserNotFound
	}
	if disabled && !list[idx].Disabled {
		if wouldLoseLastAdmin(f, idx, func(u *User) { u.Disabled = true }) {
			return errors.New("users: refusing to disable the only enabled admin")
		}
	}

	list[idx].Disabled = disabled
	return s.save(f)
}

// ValidateUsername enforces a conservative name.
//
// The charset is narrow on purpose: a username appears in logs, in the audit
// trail and in comparisons, and unicode look-alikes make two distinct accounts
// indistinguishable to a human reading any of those.
func ValidateUsername(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("users: username must not be empty")
	}
	if len(name) > maxUsernameLength {
		return fmt.Errorf("users: username is longer than %d characters", maxUsernameLength)
	}
	if name != strings.TrimSpace(name) {
		return errors.New("users: username must not have leading or trailing spaces")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_':
		default:
			return fmt.Errorf("users: username may only contain letters, digits, dot, dash and underscore")
		}
	}
	return nil
}

// ValidatePassword enforces a length floor and rejects control characters,
// which usually mean a paste went wrong rather than a deliberate choice.
func ValidatePassword(password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return fmt.Errorf("users: password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("users: password is longer than %d bytes", MaxPasswordLength)
	}
	for _, r := range password {
		if unicode.IsControl(r) {
			return errors.New("users: password must not contain control characters")
		}
	}
	return nil
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("users: generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GeneratePassword returns a random password for a system-issued account.
func GeneratePassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("users: generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// RequirePasswordChange flags an account so its owner must choose a new
// password before doing anything else.
//
// Used after an administrator resets one: the administrator now knows the
// value, so it has to stop being the account's real password promptly.
func (s *Store) RequirePasswordChange(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	idx := indexOfUsername(f.Users, username)
	if idx < 0 {
		return ErrUserNotFound
	}
	f.Users[idx].MustChangePassword = true
	return s.save(f)
}

/* ---------------------------------------------------------------------
   Group roles: a role an account inherits from a group it is in.

   The role on the account says what that person may do; a group role says
   what everyone on a team may do, and follows people as they join and leave
   the team. An account holds the STRONGER of the two — a group can grant
   administration to its members, but being in a group never takes away a
   role an account holds in its own right. Subtractive inheritance would
   make adding somebody to a team a way to quietly demote them, which is
   never what adding somebody to a team means.
   --------------------------------------------------------------------- */

// GroupRoles returns the group-to-role assignments.
func (s *Store) GroupRoles() (map[string]authz.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make(map[string]authz.Role, len(f.GroupRoles))
	for g, r := range f.GroupRoles {
		out[g] = r
	}
	return out, nil
}

// SetGroupRole assigns a role to a group, or removes the assignment when the
// role is RoleUser — user is what membership means with no assignment, so
// storing it would be a second spelling of "nothing".
//
// Removing the last assignment that keeps any administrator enabled is
// refused, exactly as demoting or disabling the last administrator is: an
// instance nobody can administer can only be repaired by hand.
func (s *Store) SetGroupRole(group string, role authz.Role) error {
	group = strings.TrimSpace(group)
	if group == "" {
		return errors.New("users: a group name is required")
	}
	if len(group) > maxGroupNameLength {
		return fmt.Errorf("users: group name is longer than %d characters", maxGroupNameLength)
	}
	if role != authz.RoleAdmin && role != authz.RoleUser {
		return fmt.Errorf("users: unknown role %q", role)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}

	// One entry per name regardless of how it is capitalised, matching how
	// group names compare everywhere else.
	next := make(map[string]authz.Role, len(f.GroupRoles)+1)
	for g, r := range f.GroupRoles {
		if !strings.EqualFold(g, group) {
			next[g] = r
		}
	}
	if role == authz.RoleAdmin {
		next[group] = role
	}
	if len(next) == 0 {
		next = nil
	}

	if countEffectiveAdmins(f.Users, next) == 0 && countEffectiveAdmins(f.Users, f.GroupRoles) > 0 {
		return errors.New("users: refusing to remove the assignment that keeps the only enabled admin")
	}

	f.GroupRoles = next
	return s.save(f)
}

// EffectiveRole is the role an account actually holds: its own, or one a
// group it belongs to grants, whichever is stronger.
func EffectiveRole(u User, groupRoles map[string]authz.Role) authz.Role {
	if u.Role == authz.RoleAdmin {
		return authz.RoleAdmin
	}
	for g, r := range groupRoles {
		if r == authz.RoleAdmin && u.InGroup(g) {
			return authz.RoleAdmin
		}
	}
	return u.Role
}

// RoleGrantingGroups names the groups that grant this account a role beyond
// its own, so a page can say where an inherited role came from.
func RoleGrantingGroups(u User, groupRoles map[string]authz.Role) []string {
	var out []string
	for g, r := range groupRoles {
		if r == authz.RoleAdmin && u.InGroup(g) {
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

// countEffectiveAdmins counts enabled accounts whose effective role is
// administrator under the given assignments.
func countEffectiveAdmins(list []User, groupRoles map[string]authz.Role) int {
	n := 0
	for _, u := range list {
		if !u.Disabled && EffectiveRole(u, groupRoles) == authz.RoleAdmin {
			n++
		}
	}
	return n
}

// sameGroups reports whether two normalised group lists hold the same names,
// so a sign-in that changes nothing does not rewrite the account file.
func sameGroups(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}
