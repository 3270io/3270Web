// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"errors"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// TestRetryHostOpRecovers verifies a transient host error is retried and the
// operation ultimately succeeds, reporting the number of extra attempts used.
func TestRetryHostOpRecovers(t *testing.T) {
	e := New(&scriptedChaosHost{connected: true}, DefaultConfig())
	calls := 0
	retries, err := e.retryHostOp(func() error {
		calls++
		if calls < 2 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryHostOp err = %v, want nil", err)
	}
	if retries != 1 {
		t.Fatalf("retries = %d, want 1", retries)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

// TestRetryHostOpExhausts verifies a persistently failing op gives up after the
// configured number of attempts and surfaces the final error.
func TestRetryHostOpExhausts(t *testing.T) {
	e := New(&scriptedChaosHost{connected: true}, DefaultConfig())
	calls := 0
	wantErr := errors.New("boom")
	retries, err := e.retryHostOp(func() error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("retryHostOp err = %v, want %v", err, wantErr)
	}
	if calls != hostOpMaxAttempts {
		t.Fatalf("calls = %d, want %d", calls, hostOpMaxAttempts)
	}
	if retries != hostOpMaxAttempts-1 {
		t.Fatalf("retries = %d, want %d", retries, hostOpMaxAttempts-1)
	}
}

// TestRetryHostOpStopCancels verifies that a Stop signal short-circuits the
// backoff wait so a user-requested halt is honoured promptly even mid-retry.
func TestRetryHostOpStopCancels(t *testing.T) {
	e := New(&scriptedChaosHost{connected: true}, DefaultConfig())
	close(e.stopCh) // simulate Stop()
	start := time.Now()
	_, err := e.retryHostOp(func() error { return errors.New("transient") })
	if err == nil {
		t.Fatalf("retryHostOp err = nil, want error")
	}
	// With stopCh closed the backoff select returns immediately, so the whole
	// call must finish far faster than even a single full backoff interval.
	if elapsed := time.Since(start); elapsed > hostOpBaseBackoff {
		t.Fatalf("retryHostOp took %v with stop signalled; expected near-immediate return", elapsed)
	}
}

// TestStartFailsWhenAllKeysBlacklisted verifies the fail-fast guard: if every
// fallback AID key is blacklisted, Start refuses instead of launching a run
// that would immediately halt on the first screen.
func TestStartFailsWhenAllKeysBlacklisted(t *testing.T) {
	h := &scriptedChaosHost{
		screens:   []*host.Screen{buildScriptedChaosScreen("ONLY", true)},
		connected: true,
	}
	cfg := DefaultConfig()
	cfg.KeyBlacklist = []string{"Enter", "PF(1)", "PF(2)", "PF(4)", "PF(7)", "PF(8)", "PF(12)", "Tab"}
	e := New(h, cfg)
	if err := e.Start(); err == nil {
		t.Fatalf("Start() = nil, want error when all usable keys are blacklisted")
	}
	if e.Active() {
		t.Fatalf("engine active after Start() refused to launch")
	}
}

// TestSaturatedNoProgressFlag verifies that a run which saturates without ever
// transitioning sets SaturatedNoProgress so callers can stop the resume loop.
func TestSaturatedNoProgressFlag(t *testing.T) {
	// Single static screen: SendKey never changes it, so no transitions occur.
	h := &scriptedChaosHost{
		screens:   []*host.Screen{buildScriptedChaosScreen("STATIC", true)},
		connected: true,
	}
	cfg := DefaultConfig()
	cfg.MaxSteps = 100
	cfg.TimeBudget = 5 * time.Second
	cfg.StepDelay = 0
	cfg.Seed = 1
	cfg.SaturationSteps = 3
	cfg.AutoBlockExitKeys = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForChaosStop(t, e, 3*time.Second)

	st := e.Status()
	if st.TerminationReason != TerminationReasonSaturated {
		t.Fatalf("TerminationReason = %q, want %q", st.TerminationReason, TerminationReasonSaturated)
	}
	if !st.SaturatedNoProgress {
		t.Fatalf("SaturatedNoProgress = false, want true (run made no transitions)")
	}
	if st.Transitions != 0 {
		t.Fatalf("Transitions = %d, want 0", st.Transitions)
	}
}

// TestRunRecoversFromTransientHostErrors verifies that transient screen-read
// failures are retried rather than killing the run: the run still reaches its
// normal saturation stop instead of TerminationReasonError.
func TestRunRecoversFromTransientHostErrors(t *testing.T) {
	h := &flakyChaosHost{
		scriptedChaosHost: scriptedChaosHost{
			screens:   []*host.Screen{buildScriptedChaosScreen("STATIC", true)},
			connected: true,
		},
		failFirst: 2, // first two UpdateScreen calls fail, then recover
	}
	cfg := DefaultConfig()
	cfg.MaxSteps = 100
	cfg.TimeBudget = 5 * time.Second
	cfg.StepDelay = 0
	cfg.Seed = 1
	cfg.SaturationSteps = 3
	cfg.AutoBlockExitKeys = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForChaosStop(t, e, 3*time.Second)

	st := e.Status()
	if st.TerminationReason == TerminationReasonError {
		t.Fatalf("run terminated with error %q despite transient (recoverable) host failures", st.Error)
	}
	if st.StepsRun == 0 {
		t.Fatalf("StepsRun = 0; run should have progressed after retrying the transient failures")
	}
}

func waitForChaosStop(t *testing.T, e *Engine, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !e.Active() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("engine still active after %v", within)
}

// flakyChaosHost fails the first failFirst UpdateScreen calls (transient) then
// behaves like its embedded scriptedChaosHost.
type flakyChaosHost struct {
	scriptedChaosHost
	failFirst   int
	updateCalls int
}

func (h *flakyChaosHost) UpdateScreen() error {
	h.updateCalls++
	if h.updateCalls <= h.failFirst {
		return errors.New("transient screen read failure")
	}
	return nil
}
