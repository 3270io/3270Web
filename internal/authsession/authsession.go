// Package authsession tracks logged-in browsers.
//
// This is deliberately separate from internal/session, which tracks 3270
// terminals. One person may have several terminals open and still be one
// logged-in browser, and logging out must end the login without discarding
// the terminals' host connections. Keeping the two lifetimes apart is what
// makes that possible.
package authsession

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/jnnngs/3270Web/internal/authz"
)

// Defaults chosen for a deployment that may be running over plain HTTP, where
// a captured cookie is a live credential until it expires. Short windows are
// one of the few controls available in that setting.
const (
	DefaultIdleTimeout     = 30 * time.Minute
	DefaultAbsoluteTimeout = 12 * time.Hour
)

// Session is one logged-in browser.
type Session struct {
	ID     string
	UserID string
	Role   authz.Role
	// Username is carried for display and audit lines so routine requests do
	// not have to re-read the account store.
	Username  string
	CreatedAt time.Time
	LastSeen  time.Time
	// ClientIP is the address the session was established from, compared on
	// each request when binding is enabled.
	ClientIP string
	// MustChangePassword mirrors the account flag at login time, so the
	// middleware can force the change without a store read per request.
	MustChangePassword bool
}

// Principal converts the login into the actor used for authorization.
func (s *Session) Principal() authz.Principal {
	return authz.Principal{UserID: s.UserID, Role: s.Role, Kind: authz.KindWeb}
}

// Store holds live logins in memory.
//
// Logins deliberately do not survive a restart. Persisting them would mean
// writing bearer-equivalent tokens to disk for a benefit — not having to log
// in again after an upgrade — that does not justify it.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*Session
	idle     time.Duration
	absolute time.Duration
	now      func() time.Time
}

// NewStore creates a store. Non-positive timeouts fall back to the defaults.
func NewStore(idle, absolute time.Duration) *Store {
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	if absolute <= 0 {
		absolute = DefaultAbsoluteTimeout
	}
	return &Store{
		sessions: make(map[string]*Session),
		idle:     idle,
		absolute: absolute,
		now:      time.Now,
	}
}

// Create starts a login and returns it. The returned ID is the cookie value.
func (s *Store) Create(userID, username string, role authz.Role, clientIP string, mustChange bool) (*Session, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	sess := &Session{
		ID:                 id,
		UserID:             userID,
		Username:           username,
		Role:               role,
		CreatedAt:          now,
		LastSeen:           now,
		ClientIP:           clientIP,
		MustChangePassword: mustChange,
	}
	s.sessions[id] = sess
	return sess, nil
}

// Get returns the live session for id and refreshes its idle clock.
//
// When bindIP is set, a session presented from a different address is refused
// but deliberately left in place: deleting it would turn a stolen cookie into
// a way to log the rightful owner out, converting theft into denial of service
// on top of it.
func (s *Store) Get(id, clientIP string, bindIP bool) (*Session, bool) {
	if id == "" {
		return nil, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}

	now := s.now()
	if s.expiredLocked(sess, now) {
		delete(s.sessions, id)
		return nil, false
	}
	if bindIP && sess.ClientIP != clientIP {
		return nil, false
	}

	sess.LastSeen = now
	// Return a copy so a caller cannot mutate live state by accident.
	out := *sess
	return &out, true
}

// Delete ends one login.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// DeleteAllFor ends every login belonging to a user, and reports how many were
// ended. Used when a password changes or an account is disabled: the point of
// changing a password is that sessions opened with the old one stop working.
func (s *Store) DeleteAllFor(userID string) int {
	if userID == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, sess := range s.sessions {
		if sess.UserID == userID {
			delete(s.sessions, id)
			n++
		}
	}
	return n
}

// SetRoleFor changes the role carried by every live login belonging to a user,
// and reports how many were changed.
//
// A login copies the account's role when it is created, so without this a role
// change would not reach the browser the person is already in: a demoted
// administrator would keep administering until their session expired — and
// could restore their own role through the page they were still standing on,
// so the demotion would never take effect at all.
//
// Ending their sessions instead would revoke it just as surely, and is what
// disabling an account does. It is the wrong answer here because it is not
// only a revocation: the same call promotes, and signing somebody out for
// being given more rights is a strange thing to do to them. Changing the role
// in place is what "your role changed" actually means.
func (s *Store) SetRoleFor(userID string, role authz.Role) int {
	if userID == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, sess := range s.sessions {
		if sess.UserID == userID && sess.Role != role {
			sess.Role = role
			n++
		}
	}
	return n
}

// UserIDs returns the distinct accounts that currently hold a login.
//
// It exists so a periodic sweep can re-check those accounts against the
// account store without this package having to know what an account store is.
// Distinct rather than one entry per session, because the caller reads a file
// per ID and one person with six tabs open is still one account.
func (s *Store) UserIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool, len(s.sessions))
	out := make([]string, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if sess.UserID == "" || seen[sess.UserID] {
			continue
		}
		seen[sess.UserID] = true
		out = append(out, sess.UserID)
	}
	return out
}

// Reap drops expired sessions and reports how many were removed. Get expires
// lazily; this bounds memory for logins that are simply abandoned.
func (s *Store) Reap() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	n := 0
	for id, sess := range s.sessions {
		if s.expiredLocked(sess, now) {
			delete(s.sessions, id)
			n++
		}
	}
	return n
}

// Count reports how many live sessions are held.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func (s *Store) expiredLocked(sess *Session, now time.Time) bool {
	if now.Sub(sess.LastSeen) >= s.idle {
		return true
	}
	return now.Sub(sess.CreatedAt) >= s.absolute
}

// newID returns a 256-bit random session identifier.
//
// The identifier is the whole credential once issued, so it fails closed: a
// store that cannot produce randomness must not hand out a guessable login.
func newID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("authsession: generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
