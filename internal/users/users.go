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
	// PasswordHash is a PHC-format Argon2id string.
	PasswordHash string `json:"passwordHash"`
	Disabled     bool   `json:"disabled,omitempty"`
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
}

// load reads the file. A missing file is an empty store, not an error: that is
// the state before the first account is created.
func (s *Store) load() ([]User, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read user store: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var parsed fileFormat
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse user store: %w", err)
	}
	return parsed.Users, nil
}

// save writes the file atomically at 0600.
//
// A torn write here would lock every account out at once, so the temp-file
// and rename dance is not optional.
func (s *Store) save(list []User) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create user store dir: %w", err)
	}
	data, err := json.MarshalIndent(fileFormat{Users: list}, "", "  ")
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
	list, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(list))
	for _, u := range list {
		out = append(out, u.Redacted())
	}
	return out, nil
}

// Count reports how many accounts exist. Used to detect first run.
func (s *Store) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return 0, err
	}
	return len(list), nil
}

// ByID looks up an account. The hash is stripped.
func (s *Store) ByID(id string) (User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return User{}, false, err
	}
	for _, u := range list {
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
	list, err := s.load()
	if err != nil {
		return User{}, err
	}
	for _, u := range list {
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
	if err := s.save(append(list, u)); err != nil {
		return User{}, err
	}
	return u.Redacted(), nil
}

// Authenticate verifies a username and password.
//
// A missing user still costs a full hash comparison. Returning early would
// make "no such user" measurably faster than "wrong password", which turns the
// login endpoint into an oracle for which accounts exist.
func (s *Store) Authenticate(username, password string) (User, error) {
	s.mu.Lock()
	list, err := s.load()
	s.mu.Unlock()
	if err != nil {
		return User{}, err
	}

	var found *User
	for i := range list {
		if strings.EqualFold(list[i].Username, username) {
			found = &list[i]
			break
		}
	}

	if found == nil {
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
	list, err := s.load()
	if err != nil {
		return err
	}
	for i := range list {
		if strings.EqualFold(list[i].Username, username) {
			list[i].PasswordHash = hash
			list[i].PasswordChangedAt = s.now()
			list[i].MustChangePassword = false
			return s.save(list)
		}
	}
	return ErrUserNotFound
}

// SetDisabled enables or disables an account.
//
// Disabling the last enabled admin is refused: an instance with no way to
// administer it can only be repaired by editing the file by hand.
func (s *Store) SetDisabled(username string, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}

	idx := -1
	for i := range list {
		if strings.EqualFold(list[i].Username, username) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrUserNotFound
	}
	if disabled && list[idx].Role == authz.RoleAdmin && !list[idx].Disabled {
		enabledAdmins := 0
		for _, u := range list {
			if u.Role == authz.RoleAdmin && !u.Disabled {
				enabledAdmins++
			}
		}
		if enabledAdmins <= 1 {
			return errors.New("users: refusing to disable the only enabled admin")
		}
	}

	list[idx].Disabled = disabled
	return s.save(list)
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
