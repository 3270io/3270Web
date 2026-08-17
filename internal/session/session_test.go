// SPDX-License-Identifier: AGPL-3.0-or-later

package session

import (
	"sync"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// TestManager_Concurrency verifies that the Session Manager handles concurrent
// creation, access, and removal of sessions safely. This test is designed to
// be run with the race detector enabled (-race).
func TestManager_Concurrency(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	numRoutines := 50

	wg.Add(numRoutines)
	for i := 0; i < numRoutines; i++ {
		go func() {
			defer wg.Done()

			// Create a mock host
			// We can pass an empty string to create a basic mock host without a dump file
			h, err := host.NewMockHost("")
			if err != nil {
				t.Errorf("failed to create mock host: %v", err)
				return
			}

			// Create session
			s := m.CreateSession(h)
			if s == nil {
				t.Error("CreateSession returned nil")
				return
			}

			// Access session immediately
			retrievedSession, ok := m.GetSession(s.ID)
			if !ok {
				t.Errorf("GetSession failed for ID %s immediately after creation", s.ID)
				return
			}
			if retrievedSession != s {
				t.Errorf("GetSession returned different instance")
			}

			// Simulate some "think time" or network delay
			time.Sleep(time.Millisecond * 10)

			// Access session again before removal
			_, ok = m.GetSession(s.ID)
			if !ok {
				// It's possible another goroutine removed it if we were sharing IDs,
				// but here each goroutine has its own session.
				t.Errorf("GetSession failed for ID %s before removal", s.ID)
				return
			}

			// Remove session
			m.RemoveSession(s.ID)

			// Verify removal
			_, ok = m.GetSession(s.ID)
			if ok {
				t.Errorf("Session %s should have been removed", s.ID)
			}
		}()
	}

	wg.Wait()
}

// TestManager_GetSession_ConcurrentAccess verifies that multiple goroutines
// can safely access the same session simultaneously (Read Lock test).
func TestManager_GetSession_ConcurrentAccess(t *testing.T) {
	m := NewManager()
	h, _ := host.NewMockHost("")
	s := m.CreateSession(h)

	var wg sync.WaitGroup
	numReaders := 100

	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			retrieved, ok := m.GetSession(s.ID)
			if !ok {
				t.Error("failed to get session concurrently")
				return
			}
			if retrieved.ID != s.ID {
				t.Error("retrieved session has wrong ID")
			}
		}()
	}
	wg.Wait()
}

// TestManager_IdleSessionIDs verifies that only sessions whose LastAccess
// predates the cutoff are reported idle, guarding the reaper's core query.
func TestManager_IdleSessionIDs(t *testing.T) {
	m := NewManager()
	h1, _ := host.NewMockHost("")
	h2, _ := host.NewMockHost("")

	idle := m.CreateSession(h1)
	idle.LastAccess = time.Now().Add(-3 * time.Hour)

	fresh := m.CreateSession(h2)
	fresh.LastAccess = time.Now()

	ids := m.IdleSessionIDs(2 * time.Hour)
	if len(ids) != 1 || ids[0] != idle.ID {
		t.Fatalf("IdleSessionIDs(2h) = %v, want only %q", ids, idle.ID)
	}
}

// TestManager_PeekSession_DoesNotUpdateLastAccess guards against the reaper
// itself keeping every session alive forever: unlike GetSession, PeekSession
// must not bump LastAccess, or IdleSessionIDs would never see a session as
// idle once the reaper has looked at it once.
func TestManager_PeekSession_DoesNotUpdateLastAccess(t *testing.T) {
	m := NewManager()
	h, _ := host.NewMockHost("")
	s := m.CreateSession(h)
	stale := time.Now().Add(-3 * time.Hour)
	s.LastAccess = stale

	retrieved, ok := m.PeekSession(s.ID)
	if !ok {
		t.Fatal("PeekSession: session not found")
	}
	if !retrieved.LastAccess.Equal(stale) {
		t.Fatalf("PeekSession updated LastAccess: got %v, want unchanged %v", retrieved.LastAccess, stale)
	}
}

func TestCreateSessionFor_LabelsOwner(t *testing.T) {
	m := NewManager()

	owned := m.CreateSessionFor("alice", nil)
	if owned.OwnerID != "alice" {
		t.Errorf("OwnerID = %q, want %q", owned.OwnerID, "alice")
	}

	// The no-owner constructor stays available for callers with no principal
	// to hand; it must produce an explicitly unowned session rather than
	// inventing one.
	unowned := m.CreateSession(nil)
	if unowned.OwnerID != "" {
		t.Errorf("CreateSession OwnerID = %q, want empty", unowned.OwnerID)
	}
}

func TestListSessionsForAndCountFor(t *testing.T) {
	m := NewManager()
	a1 := m.CreateSessionFor("alice", nil)
	a2 := m.CreateSessionFor("alice", nil)
	b1 := m.CreateSessionFor("bob", nil)
	m.CreateSession(nil) // unowned

	if got := m.CountFor("alice"); got != 2 {
		t.Errorf("CountFor(alice) = %d, want 2", got)
	}
	if got := m.CountFor("bob"); got != 1 {
		t.Errorf("CountFor(bob) = %d, want 1", got)
	}

	ids := map[string]bool{}
	for _, s := range m.ListSessionsFor("alice") {
		ids[s.ID] = true
	}
	if len(ids) != 2 || !ids[a1.ID] || !ids[a2.ID] {
		t.Errorf("ListSessionsFor(alice) = %v, want exactly %s and %s", ids, a1.ID, a2.ID)
	}
	if ids[b1.ID] {
		t.Error("alice's listing included bob's session")
	}
}

// An empty owner must match nothing. Answering "the sessions belonging to
// nobody" with the full set is how enumeration bugs get introduced.
func TestListSessionsFor_EmptyOwnerMatchesNothing(t *testing.T) {
	m := NewManager()
	m.CreateSessionFor("alice", nil)
	m.CreateSession(nil)

	if got := m.ListSessionsFor(""); len(got) != 0 {
		t.Errorf("ListSessionsFor(\"\") returned %d sessions, want 0", len(got))
	}
	if got := m.CountFor(""); got != 0 {
		t.Errorf("CountFor(\"\") = %d, want 0", got)
	}
}
