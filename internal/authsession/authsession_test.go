// SPDX-License-Identifier: AGPL-3.0-or-later

package authsession

import (
	"sync"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/authz"
)

func newTestStore(idle, absolute time.Duration) (*Store, *time.Time) {
	s := NewStore(idle, absolute)
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	return s, &clock
}

func create(t *testing.T, s *Store, ip string) *Session {
	t.Helper()
	sess, err := s.Create("user-1", "alice", authz.RoleUser, ip, false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return sess
}

func TestCreateAndGet(t *testing.T) {
	s, _ := newTestStore(time.Hour, 12*time.Hour)
	sess := create(t, s, "10.0.0.5")

	if len(sess.ID) != 64 {
		t.Errorf("session id is %d hex chars, want 64 (256 bits)", len(sess.ID))
	}

	got, ok := s.Get(sess.ID, "10.0.0.5", false)
	if !ok {
		t.Fatal("a fresh session did not resolve")
	}
	if got.UserID != "user-1" || got.Username != "alice" {
		t.Errorf("resolved %+v, want user-1/alice", got)
	}

	p := got.Principal()
	if p.UserID != "user-1" || p.Kind != authz.KindWeb {
		t.Errorf("Principal() = %+v", p)
	}
	if p.IsAdmin() {
		t.Error("a RoleUser session produced an admin principal")
	}
}

func TestIDsAreUnique(t *testing.T) {
	s, _ := newTestStore(time.Hour, 12*time.Hour)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		sess := create(t, s, "10.0.0.5")
		if seen[sess.ID] {
			t.Fatal("session ID repeated")
		}
		seen[sess.ID] = true
	}
}

func TestUnknownSession(t *testing.T) {
	s, _ := newTestStore(time.Hour, 12*time.Hour)
	if _, ok := s.Get("nope", "10.0.0.5", false); ok {
		t.Error("an unknown id resolved")
	}
	if _, ok := s.Get("", "10.0.0.5", false); ok {
		t.Error("an empty id resolved")
	}
}

func TestIdleTimeout(t *testing.T) {
	s, clock := newTestStore(30*time.Minute, 12*time.Hour)
	sess := create(t, s, "10.0.0.5")

	// Activity inside the window keeps it alive and pushes the deadline out.
	*clock = clock.Add(29 * time.Minute)
	if _, ok := s.Get(sess.ID, "10.0.0.5", false); !ok {
		t.Fatal("session expired inside the idle window")
	}
	*clock = clock.Add(29 * time.Minute)
	if _, ok := s.Get(sess.ID, "10.0.0.5", false); !ok {
		t.Fatal("activity did not refresh the idle clock")
	}

	*clock = clock.Add(30 * time.Minute)
	if _, ok := s.Get(sess.ID, "10.0.0.5", false); ok {
		t.Error("session survived past the idle timeout")
	}
}

// The absolute cap must apply however active the session is, so a stolen
// cookie cannot be kept alive indefinitely by using it.
func TestAbsoluteTimeoutIgnoresActivity(t *testing.T) {
	s, clock := newTestStore(30*time.Minute, 2*time.Hour)
	sess := create(t, s, "10.0.0.5")

	for i := 0; i < 7; i++ {
		*clock = clock.Add(15 * time.Minute)
		if _, ok := s.Get(sess.ID, "10.0.0.5", false); !ok {
			t.Fatalf("session ended early, at %d minutes", (i+1)*15)
		}
	}

	*clock = clock.Add(15 * time.Minute) // now two hours old
	if _, ok := s.Get(sess.ID, "10.0.0.5", false); ok {
		t.Error("a continuously active session outlived the absolute cap")
	}
}

func TestIPBinding(t *testing.T) {
	s, _ := newTestStore(time.Hour, 12*time.Hour)
	sess := create(t, s, "10.0.0.5")

	if _, ok := s.Get(sess.ID, "10.0.0.9", false); !ok {
		t.Error("binding disabled should accept another address")
	}
	if _, ok := s.Get(sess.ID, "10.0.0.9", true); ok {
		t.Error("binding enabled accepted a replay from another address")
	}
	if _, ok := s.Get(sess.ID, "10.0.0.5", true); !ok {
		t.Error("binding enabled rejected the original address")
	}
}

// A rejected replay must not destroy the session, or stealing a cookie also
// becomes a way to log the rightful owner out.
func TestRejectedReplayDoesNotEndTheSession(t *testing.T) {
	s, _ := newTestStore(time.Hour, 12*time.Hour)
	sess := create(t, s, "10.0.0.5")

	for i := 0; i < 3; i++ {
		if _, ok := s.Get(sess.ID, "10.0.0.99", true); ok {
			t.Fatal("replay from another address was accepted")
		}
	}
	if _, ok := s.Get(sess.ID, "10.0.0.5", true); !ok {
		t.Error("the owner was logged out by someone else's failed replay")
	}
}

func TestDelete(t *testing.T) {
	s, _ := newTestStore(time.Hour, 12*time.Hour)
	sess := create(t, s, "10.0.0.5")

	s.Delete(sess.ID)
	if _, ok := s.Get(sess.ID, "10.0.0.5", false); ok {
		t.Error("a deleted session still resolves")
	}
}

// Changing a password has to end the sessions opened with the old one,
// otherwise the change does not actually revoke anything.
func TestDeleteAllFor(t *testing.T) {
	s, _ := newTestStore(time.Hour, 12*time.Hour)

	a1, _ := s.Create("alice", "alice", authz.RoleUser, "10.0.0.1", false)
	a2, _ := s.Create("alice", "alice", authz.RoleUser, "10.0.0.2", false)
	b1, _ := s.Create("bob", "bob", authz.RoleUser, "10.0.0.3", false)

	if n := s.DeleteAllFor("alice"); n != 2 {
		t.Errorf("DeleteAllFor removed %d, want 2", n)
	}
	for _, id := range []string{a1.ID, a2.ID} {
		if _, ok := s.Get(id, "10.0.0.1", false); ok {
			t.Error("one of alice's sessions survived")
		}
	}
	if _, ok := s.Get(b1.ID, "10.0.0.3", false); !ok {
		t.Error("bob's session was removed too")
	}

	if n := s.DeleteAllFor(""); n != 0 {
		t.Errorf("DeleteAllFor(\"\") removed %d sessions, want 0", n)
	}
}

func TestReapRemovesOnlyExpired(t *testing.T) {
	s, clock := newTestStore(30*time.Minute, 12*time.Hour)
	old := create(t, s, "10.0.0.1")

	*clock = clock.Add(31 * time.Minute)
	fresh := create(t, s, "10.0.0.2")

	if n := s.Reap(); n != 1 {
		t.Errorf("Reap removed %d, want 1", n)
	}
	if s.Count() != 1 {
		t.Errorf("Count = %d, want 1", s.Count())
	}
	if _, ok := s.Get(old.ID, "10.0.0.1", false); ok {
		t.Error("the expired session survived the reap")
	}
	if _, ok := s.Get(fresh.ID, "10.0.0.2", false); !ok {
		t.Error("the live session was reaped")
	}
}

func TestNewStoreAppliesDefaults(t *testing.T) {
	s := NewStore(0, -1)
	if s.idle != DefaultIdleTimeout {
		t.Errorf("idle = %v, want %v", s.idle, DefaultIdleTimeout)
	}
	if s.absolute != DefaultAbsoluteTimeout {
		t.Errorf("absolute = %v, want %v", s.absolute, DefaultAbsoluteTimeout)
	}
}

// Get hands back a copy; mutating it must not corrupt the live session.
func TestGetReturnsACopy(t *testing.T) {
	s, _ := newTestStore(time.Hour, 12*time.Hour)
	sess := create(t, s, "10.0.0.5")

	got, _ := s.Get(sess.ID, "10.0.0.5", false)
	got.UserID = "mallory"
	got.Role = authz.RoleAdmin

	again, ok := s.Get(sess.ID, "10.0.0.5", false)
	if !ok {
		t.Fatal("session vanished")
	}
	if again.UserID != "user-1" || again.Role != authz.RoleUser {
		t.Errorf("live session was mutated through a returned copy: %+v", again)
	}
}

func TestConcurrentUse(t *testing.T) {
	s := NewStore(time.Hour, 12*time.Hour)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := s.Create("alice", "alice", authz.RoleUser, "10.0.0.1", false)
			if err != nil {
				t.Error(err)
				return
			}
			s.Get(sess.ID, "10.0.0.1", true)
			s.Reap()
			s.Delete(sess.ID)
		}()
	}
	wg.Wait()

	if s.Count() != 0 {
		t.Errorf("Count = %d, want 0", s.Count())
	}
}

func TestSetRoleForTouchesOnlyThatUsersLogins(t *testing.T) {
	s := NewStore(time.Hour, 12*time.Hour)

	first, _ := s.Create("user-1", "alice", authz.RoleAdmin, "10.0.0.1", false)
	// Two logins, because one person signed in from a second browser is the
	// case where changing only the session that happens to be found first
	// would leave the other one administering.
	second, _ := s.Create("user-1", "alice", authz.RoleAdmin, "10.0.0.2", false)
	other, _ := s.Create("user-2", "bob", authz.RoleAdmin, "10.0.0.3", false)

	if n := s.SetRoleFor("user-1", authz.RoleUser); n != 2 {
		t.Errorf("SetRoleFor changed %d logins, want 2", n)
	}
	for _, id := range []string{first.ID, second.ID} {
		got, ok := s.Get(id, "", false)
		if !ok {
			t.Fatalf("login %s was ended rather than changed", id)
		}
		if got.Role != authz.RoleUser {
			t.Errorf("login %s still holds %s", id, got.Role)
		}
	}
	if got, _ := s.Get(other.ID, "", false); got.Role != authz.RoleAdmin {
		t.Errorf("another account's login was changed to %s", got.Role)
	}

	// Re-applying the role it already has changes nothing, so the count means
	// "logins affected" rather than "logins seen".
	if n := s.SetRoleFor("user-1", authz.RoleUser); n != 0 {
		t.Errorf("re-applying the same role reported %d changes, want 0", n)
	}
}
