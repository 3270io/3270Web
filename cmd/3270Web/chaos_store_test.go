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
