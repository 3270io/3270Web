// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/host"
)

// TestStartIfAbsentSerialisesConcurrentStarts verifies the fix for the
// TOCTOU window in ChaosStartHandler: two concurrent /chaos/start requests
// for the same session must not both build and start an engine, because
// only one can be tracked in the store and any other would leak its
// background goroutine and any open transition log.
//
// startIfAbsent must hold the store mutex across the active-engine check,
// the build callback, and Start(), so that exactly one goroutine becomes
// "started" and the rest observe "already running".
func TestStartIfAbsentSerialisesConcurrentStarts(t *testing.T) {
	store := newChaosEngineStore()

	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	mockHost.Connected = true
	t.Cleanup(func() { _ = mockHost.Stop() })

	cfg := chaos.Config{
		MaxSteps:   1,
		TimeBudget: 100 * time.Millisecond,
		StepDelay:  10 * time.Millisecond,
		Seed:       1,
	}

	const goroutines = 16
	var (
		builds   int64
		started  int64
		conflict int64
		wg       sync.WaitGroup
		ready    = make(chan struct{})
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-ready
			_, err, ok := store.startIfAbsent("session-A", func() (*chaos.Engine, error) {
				atomic.AddInt64(&builds, 1)
				return chaos.New(mockHost, cfg), nil
			})
			if err != nil {
				t.Errorf("startIfAbsent: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&started, 1)
			} else {
				atomic.AddInt64(&conflict, 1)
			}
		}()
	}
	close(ready)
	wg.Wait()

	t.Cleanup(func() {
		if eng, ok := store.get("session-A"); ok {
			eng.Stop()
		}
	})

	if got := atomic.LoadInt64(&started); got != 1 {
		t.Fatalf("expected exactly 1 successful start, got %d", got)
	}
	if got := atomic.LoadInt64(&builds); got != 1 {
		t.Fatalf("expected build callback to run exactly once, got %d", got)
	}
	if got := atomic.LoadInt64(&conflict); got != goroutines-1 {
		t.Fatalf("expected %d conflicts, got %d", goroutines-1, got)
	}
}

// TestResumeIfAbsentSerialisesConcurrentResumes verifies the fix for the
// TOCTOU window in ChaosResumeHandler: two concurrent /chaos/resume requests
// (or a /chaos/start racing a /chaos/resume) for the same session must not
// both install an engine in the store, because the loser's engine would keep
// running unreferenced — an orphaned goroutine that /chaos/stop can no
// longer reach, still driving the shared host connection.
func TestResumeIfAbsentSerialisesConcurrentResumes(t *testing.T) {
	store := newChaosEngineStore()

	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	mockHost.Connected = true
	t.Cleanup(func() { _ = mockHost.Stop() })

	cfg := chaos.Config{
		MaxSteps:   1,
		TimeBudget: 100 * time.Millisecond,
		StepDelay:  10 * time.Millisecond,
		Seed:       1,
	}
	saved := &chaos.SavedRun{}

	const goroutines = 16
	var (
		resumed  int64
		conflict int64
		wg       sync.WaitGroup
		ready    = make(chan struct{})
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-ready
			_, err, ok := store.resumeIfAbsent(
				"session-A",
				func() (*chaos.Engine, error) { return chaos.New(mockHost, cfg), nil },
				func(eng *chaos.Engine) error { return eng.Resume(saved) },
			)
			if err != nil {
				t.Errorf("resumeIfAbsent: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&resumed, 1)
			} else {
				atomic.AddInt64(&conflict, 1)
			}
		}()
	}
	close(ready)
	wg.Wait()

	t.Cleanup(func() {
		if eng, ok := store.get("session-A"); ok {
			eng.Stop()
		}
	})

	if got := atomic.LoadInt64(&resumed); got != 1 {
		t.Fatalf("expected exactly 1 successful resume, got %d", got)
	}
	if got := atomic.LoadInt64(&conflict); got != goroutines-1 {
		t.Fatalf("expected %d conflicts, got %d", goroutines-1, got)
	}
}

// TestLoadRecordingIfInactiveRejectsWhenEngineActive verifies the fix for
// ChaosLoadRecordingHandler's TOCTOU window: loading a recording must be
// rejected atomically with the active-engine check, so a /chaos/start that
// wins the race can never have its just-started run silently wiped out by a
// concurrent /chaos/load-recording.
func TestLoadRecordingIfInactiveRejectsWhenEngineActive(t *testing.T) {
	store := newChaosEngineStore()

	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	mockHost.Connected = true
	t.Cleanup(func() { _ = mockHost.Stop() })

	cfg := chaos.Config{
		MaxSteps:   1,
		TimeBudget: 5 * time.Second,
		StepDelay:  time.Second,
		Seed:       1,
	}
	eng, err, started := store.startIfAbsent("session-A", func() (*chaos.Engine, error) {
		return chaos.New(mockHost, cfg), nil
	})
	if err != nil || !started {
		t.Fatalf("startIfAbsent: eng=%v err=%v started=%v", eng, err, started)
	}
	t.Cleanup(eng.Stop)

	if ok := store.loadRecordingIfInactive("session-A", &chaos.SavedRun{SavedRunMeta: chaos.SavedRunMeta{ID: "r1"}}); ok {
		t.Fatalf("loadRecordingIfInactive succeeded while an engine is active")
	}
	if _, ok := store.getLoadedRun("session-A"); ok {
		t.Fatalf("loaded run was installed despite the active-engine conflict")
	}

	eng.Stop()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && eng.Active() {
		time.Sleep(10 * time.Millisecond)
	}

	if ok := store.loadRecordingIfInactive("session-A", &chaos.SavedRun{SavedRunMeta: chaos.SavedRunMeta{ID: "r2"}}); !ok {
		t.Fatalf("loadRecordingIfInactive failed once the engine was inactive")
	}
	run, ok := store.getLoadedRun("session-A")
	if !ok || run.ID != "r2" {
		t.Fatalf("getLoadedRun = %+v, %v; want r2, true", run, ok)
	}
}
