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
