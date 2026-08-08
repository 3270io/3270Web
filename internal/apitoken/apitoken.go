// Package apitoken issues and verifies the credentials automated clients use.
//
// It replaces a single API_TOKEN environment variable shared by everybody.
// One shared credential cannot be attributed to a person, cannot be revoked
// without cutting off every client at once, and — now that sessions belong to
// accounts — cannot be confined to anybody's own work. A token that belongs to
// a user inherits that user's boundaries for free.
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Prefix marks a 3270Web token in a log, a config file or a secret scanner.
// Credentials leak; a recognisable shape is what lets tooling notice.
const Prefix = "3270w"

// Errors callers distinguish between.
var (
	ErrNotFound  = errors.New("apitoken: no such token")
	ErrMalformed = errors.New("apitoken: token is not in the expected format")
	ErrRevoked   = errors.New("apitoken: token has been revoked")
	ErrExpired   = errors.New("apitoken: token has expired")
)

// Scope names what a token may do. Deliberately coarse: a scope nobody can
// explain is a scope nobody sets correctly.
const (
	// ScopeRead allows reading — screens, session lists, catalogues.
	ScopeRead = "read"
	// ScopeWrite additionally allows anything that changes state: typing into
	// a field, pressing a key, opening or closing a session.
	ScopeWrite = "write"
)

// Token is one issued credential. The secret itself is never stored; only a
// hash of it, so a copy of the file does not yield working credentials.
type Token struct {
	// ID identifies the record and travels in the token, so verification is
	// one lookup rather than a comparison against every token on file.
	ID     string `json:"id"`
	UserID string `json:"userId"`
	// Name is what the person called it — "ci pipeline", "claude desktop" —
	// so a list of tokens is a list of purposes rather than of hex strings.
	Name string `json:"name"`
	// SecretHash is SHA-256 of the secret half.
	//
	// A password needs a slow hash because people choose guessable ones. This
	// secret is 32 bytes from crypto/rand, so there is nothing to guess and
	// nothing for Argon2id to buy — it would only make every API call slower.
	SecretHash string     `json:"secretHash"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// HasScope reports whether the token carries a scope.
func (t Token) HasScope(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Active reports whether the token may still be used, and why not if it may
// not.
func (t Token) Active(now time.Time) error {
	if t.RevokedAt != nil {
		return ErrRevoked
	}
	if t.ExpiresAt != nil && !now.Before(*t.ExpiresAt) {
		return ErrExpired
	}
	return nil
}

// Redacted returns a copy safe to serve or log.
func (t Token) Redacted() Token {
	t.SecretHash = ""
	return t
}

// Store holds issued tokens on disk.
type Store struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

// NewStore opens the token file at path. Nothing is written until a token is
// issued.
func NewStore(path string) *Store {
	return &Store{path: path, now: time.Now}
}

// Path reports where the store persists.
func (s *Store) Path() string { return s.path }

type fileFormat struct {
	Tokens []Token `json:"tokens"`
}

func (s *Store) load() ([]Token, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read token store: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var parsed fileFormat
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse token store: %w", err)
	}
	return parsed.Tokens, nil
}

func (s *Store) save(list []Token) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create token store dir: %w", err)
	}
	data, err := json.MarshalIndent(fileFormat{Tokens: list}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode token store: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".tokens-*")
	if err != nil {
		return fmt.Errorf("create temp token store: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp token store: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp token store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp token store: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod temp token store: %w", err)
	}
	return os.Rename(tmpPath, s.path)
}

// Issue creates a token and returns the record together with the only copy of
// the secret that will ever exist.
//
// The secret is returned rather than stored because a store that can show a
// token later is a store that leaks every token when it is read. Losing it
// means issuing another, which is cheap; recovering it would mean it was
// never really secret.
func (s *Store) Issue(userID, name string, scopes []string, expiresAt *time.Time) (Token, string, error) {
	if strings.TrimSpace(userID) == "" {
		return Token{}, "", errors.New("apitoken: a token must belong to an account")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Token{}, "", errors.New("apitoken: a token needs a name saying what it is for")
	}
	if len(scopes) == 0 {
		scopes = []string{ScopeRead, ScopeWrite}
	}
	for _, scope := range scopes {
		if scope != ScopeRead && scope != ScopeWrite {
			return Token{}, "", fmt.Errorf("apitoken: unknown scope %q", scope)
		}
	}

	id, err := randomHex(8)
	if err != nil {
		return Token{}, "", err
	}
	secret, err := randomSecret()
	if err != nil {
		return Token{}, "", err
	}

	record := Token{
		ID:         id,
		UserID:     userID,
		Name:       name,
		SecretHash: hashSecret(secret),
		Scopes:     scopes,
		CreatedAt:  s.now(),
		ExpiresAt:  expiresAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return Token{}, "", err
	}
	if err := s.save(append(list, record)); err != nil {
		return Token{}, "", err
	}
	return record.Redacted(), Format(id, secret), nil
}

// Verify resolves a presented token.
//
// The ID travels in the credential, so this is one lookup and one constant-
// time comparison rather than a comparison against every token on file — which
// would get slower with each token issued and leak that fact through timing.
func (s *Store) Verify(presented string) (Token, error) {
	id, secret, err := Parse(presented)
	if err != nil {
		return Token{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return Token{}, err
	}

	idx := -1
	for i := range list {
		if list[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Token{}, ErrNotFound
	}
	if subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(list[idx].SecretHash)) != 1 {
		return Token{}, ErrNotFound
	}

	now := s.now()
	if err := list[idx].Active(now); err != nil {
		return Token{}, err
	}

	// Recording use is best-effort: a token that works must not stop working
	// because the disk is full, and "when was this last used" is an aid to
	// tidying up rather than part of the decision.
	stamp := now
	list[idx].LastUsedAt = &stamp
	_ = s.save(list)

	return list[idx].Redacted(), nil
}

// List returns every token, hashes stripped.
func (s *Store) List() ([]Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Token, 0, len(list))
	for _, t := range list {
		out = append(out, t.Redacted())
	}
	return out, nil
}

// Revoke marks a token unusable, keeping the record so the audit trail still
// shows it existed and when it stopped.
func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID != id {
			continue
		}
		if list[i].RevokedAt != nil {
			return nil
		}
		stamp := s.now()
		list[i].RevokedAt = &stamp
		return s.save(list)
	}
	return ErrNotFound
}

// RevokeAllFor revokes every token belonging to a user, and reports how many.
// Used when an account is disabled or deleted: leaving its tokens working
// would make "disabled" mean only that the person cannot sign in.
func (s *Store) RevokeAllFor(userID string) (int, error) {
	if userID == "" {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return 0, err
	}
	n := 0
	stamp := s.now()
	for i := range list {
		if list[i].UserID == userID && list[i].RevokedAt == nil {
			revoked := stamp
			list[i].RevokedAt = &revoked
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.save(list)
}

// Format assembles the wire form of a token.
func Format(id, secret string) string {
	return Prefix + "_" + id + "_" + secret
}

// Parse splits a presented token into its ID and secret.
func Parse(presented string) (id, secret string, err error) {
	parts := strings.Split(strings.TrimSpace(presented), "_")
	if len(parts) != 3 || parts[0] != Prefix || parts[1] == "" || parts[2] == "" {
		return "", "", ErrMalformed
	}
	return parts[1], parts[2], nil
}

// LooksLikeToken reports whether a value has this package's shape, so a caller
// can tell a per-user token from the legacy shared one.
func LooksLikeToken(presented string) bool {
	_, _, err := Parse(presented)
	return err == nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("apitoken: generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// randomSecret returns 32 bytes of entropy in an alphabet that survives being
// copied around.
//
// Base32 rather than base64url: the latter's alphabet includes the underscore
// that separates the token's parts, so roughly half of all secrets would have
// split into the wrong number of pieces and failed to verify. Lower-cased and
// unpadded so the whole credential is one word a person can select, paste into
// a config file, or type from a screenshot.
func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("apitoken: generate secret: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}
