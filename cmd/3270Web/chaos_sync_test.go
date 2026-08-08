package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
)

// The chaos status goroutine used to have no exit condition once its session
// went away.
//
// It stopped on two things: the run finishing, and the session's engine being
// marked removed. cleanupSession clears that mark on its way out, so a
// session ended mid-run left the goroutine polling a detached engine for the
// life of the process — and when the run did finish, it wrote the saved-run
// file into a directory whose owner was long gone. Under test that owner is
// t.TempDir(), which is why this surfaced as
// "TempDir RemoveAll cleanup: directory not empty" in whichever test was
// unlucky on a loaded runner, rather than as anything reproducible.
//
// These tests pin the exit conditions rather than the symptom, because the
// symptom lands in a different test every time.

// startChaosRun starts a run, so the status goroutine is definitely up before
// a test tries to stop it.
func startChaosRun(t *testing.T, r *gin.Engine, sessID string, maxSteps int) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"maxSteps":     maxSteps,
		"stepDelaySec": 0,
		"seed":         31,
	})
	if w := chaosRequest(r, http.MethodPost, "/chaos/start", payload, sessID); w.Code != http.StatusOK {
		t.Fatalf("chaos start: want 200, got %d — body %s", w.Code, w.Body.String())
	}
}

// TestChaosSyncStopsWhenTheSessionIsCleanedUp is the leak itself: end the
// session and the goroutine must end too, promptly and without being told
// twice.
func TestChaosSyncStopsWhenTheSessionIsCleanedUp(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mockHost.Screen = buildSampleApp1Screen()
	mockHost.Connected = true

	app, r, sessID := setupFullChaosTestApp(t, mockHost)
	// A run long enough that it is still going when the session ends, which
	// is the case the old code could not get out of.
	startChaosRun(t, r, sessID, 5000)

	sess, ok := app.SessionManager.GetSession(sessID)
	if !ok {
		t.Fatal("session went missing before it could be cleaned up")
	}
	app.cleanupSession(sess)

	if !app.waitForChaosSync(5 * time.Second) {
		t.Fatal("the status goroutine outlived its session; it will write into a directory nobody owns")
	}
}

// The explicit remove path stops it too, and does not have to wait out a
// poll interval to do so — it is the write on the way out that a caller
// removing a run does not want landing afterwards.
func TestChaosSyncStopsOnRemove(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mockHost.Screen = buildSampleApp1Screen()
	mockHost.Connected = true

	app, r, sessID := setupFullChaosTestApp(t, mockHost)
	startChaosRun(t, r, sessID, 2)

	// The run has to finish, or /chaos/remove is refused as still running.
	waitForChaosIdle(t, r, sessID)

	if w := chaosRequest(r, http.MethodPost, "/chaos/remove", nil, sessID); w.Code != http.StatusOK {
		t.Fatalf("remove: want 200, got %d — body %s", w.Code, w.Body.String())
	}
	if !app.waitForChaosSync(5 * time.Second) {
		t.Fatal("the status goroutine survived an explicit remove")
	}
}

// The ordinary path: a run that finishes ends its own goroutine, with nobody
// telling it to. Sequential runs must not accumulate them — the app refuses a
// second concurrent run, so this is the shape a long-lived session actually
// takes.
func TestChaosSyncEndsItselfWhenARunCompletes(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mockHost.Screen = buildSampleApp1Screen()
	mockHost.Connected = true

	app, r, sessID := setupFullChaosTestApp(t, mockHost)

	for i := 0; i < 2; i++ {
		startChaosRun(t, r, sessID, 2)
		waitForChaosIdle(t, r, sessID)

		// No stopSync, no stopAllSync: a completed run cleans up after itself
		// or the goroutine is a leak.
		if !app.waitForChaosSync(5 * time.Second) {
			t.Fatalf("run %d: the status goroutine did not end when the run did", i+1)
		}
	}
}

// waitForChaosIdle blocks until the session reports the run finished.
func waitForChaosIdle(t *testing.T, r *gin.Engine, sessID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		w := chaosRequest(r, http.MethodGet, "/chaos/status", nil, sessID)
		var status map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatalf("status not JSON: %v", err)
		}
		if active, _ := status["active"].(bool); !active {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the chaos run never finished")
}

// waitForChaosSync is called on the shutdown path and from test cleanup, so
// it must come back even when a goroutine will not.
func TestWaitForChaosSyncIsBounded(t *testing.T) {
	app := &App{chaosEngines: newChaosEngineStore()}
	app.chaosSync.Add(1)
	defer app.chaosSync.Done()

	start := time.Now()
	if app.waitForChaosSync(150 * time.Millisecond) {
		t.Fatal("reported success while a goroutine was still counted")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waited %s for a 150ms timeout", elapsed)
	}
}

// endSync must not close the channel it forgets. A goroutine ending because a
// newer run replaced it would otherwise close the new run's channel on its
// way out and stop the run that just started.
func TestEndSyncLeavesANewerRunAlone(t *testing.T) {
	store := newChaosEngineStore()

	first := store.beginSync("session-a")
	second := store.beginSync("session-a")

	select {
	case <-first:
	default:
		t.Fatal("starting a second run should have stopped the first")
	}

	store.endSync("session-a", first)

	select {
	case <-second:
		t.Fatal("the older goroutine's cleanup closed the newer run's stop channel")
	default:
	}

	store.stopSync("session-a")
	select {
	case <-second:
	default:
		t.Fatal("stopSync did not stop the run that was actually registered")
	}
}

// Stopping a session that never ran chaos is a no-op, because cleanupSession
// calls it for every session.
func TestStopSyncOnASessionThatNeverRanChaos(t *testing.T) {
	store := newChaosEngineStore()
	store.stopSync("never-ran")
	store.stopAllSync()
}
