// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Marking a field changed and writing the changed fields used to be two
// separate holds of the session lock, with the caller's own work in between —
// and the screen those marks live on is rebuilt in place by every read from the
// host. The live screen stream reads every 700 ms, so a refresh landing in that
// window handed back a screen with nothing marked on it, and the submit that
// followed wrote nothing. It did not fail. The AID key went out regardless, so
// the operator watched their typing disappear and the transaction go through
// without it.
//
// This drives a refresh against the submit as hard as it can while the edit is
// being made. What the edit marked has to be what gets written, every time.
func TestSubmitOperatorInputSurvivesAConcurrentRefresh(t *testing.T) {
	h, logPath := startFakeS3270(t, "polite")
	defer h.Stop()
	h.screen = blankScreen(24, 80)
	h.screen.IsFormatted = true

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				// The fake answers a read with a status line and no screen data,
				// which rebuilds the screen with an empty field list — exactly
				// what the operator's marks used to be lost to.
				_ = h.UpdateScreen()
			}
		}
	}()

	const rounds = 20
	for i := 0; i < rounds; i++ {
		err := h.SubmitOperatorInput(func(screen *Screen) string {
			f := NewField(screen, 0x00, 10, 5, 20, 5, AttrColDefault, AttrEhDefault)
			f.Value = "HELLO"
			f.Changed = true
			screen.Fields = []*Field{f}
			screen.IsFormatted = true
			return ""
		})
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	writes := 0
	for _, cmd := range recordedCommands(t, logPath) {
		if strings.Contains(cmd, `String("HELLO")`) {
			writes++
		}
	}
	if writes != rounds {
		t.Errorf("%d of %d edits reached the host; the rest were marked on a screen that was rebuilt before they could be written", writes, rounds)
	}
}

// Which way the input is written is a property of the screen, so the decision
// belongs inside the same lock as the write. Deciding it outside meant reading
// one screen and writing to another.
func TestSubmitOperatorInputPicksTheWriteFromTheScreen(t *testing.T) {
	t.Run("unformatted takes the returned text", func(t *testing.T) {
		h, logPath := startFakeS3270(t, "polite")
		defer h.Stop()
		h.screen = blankScreen(2, 10)
		h.screen.IsFormatted = false

		if err := h.SubmitOperatorInput(func(*Screen) string { return "HELLO" }); err != nil {
			t.Fatalf("SubmitOperatorInput: %v", err)
		}
		if !strings.Contains(strings.Join(recordedCommands(t, logPath), " "), `String("HELLO")`) {
			t.Error("the returned text was not written to an unformatted screen")
		}
	})

	t.Run("formatted ignores the returned text", func(t *testing.T) {
		h, logPath := startFakeS3270(t, "polite")
		defer h.Stop()
		h.screen = blankScreen(2, 10)
		h.screen.IsFormatted = true

		if err := h.SubmitOperatorInput(func(*Screen) string { return "HELLO" }); err != nil {
			t.Fatalf("SubmitOperatorInput: %v", err)
		}
		// A formatted screen with nothing marked writes nothing at all, so the
		// fake may never have been asked for anything and the log may not exist.
		logged, err := os.ReadFile(logPath)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read command log: %v", err)
		}
		if strings.Contains(string(logged), `String(`) {
			t.Error("text returned for an unformatted screen was typed onto a formatted one")
		}
	})
}

// An interactive caller waits for the session and then gives up, because the
// one thing that holds it for minutes is a file transfer and a browser polling
// the screen behind it would queue a stalled request per poll. This checks the
// waiting rather than the giving up: the budget is deliberately longer than any
// ordinary hold, so waiting it out here would only be waiting.
func TestLockForInteractionWaitsForTheSession(t *testing.T) {
	h, _ := startFakeS3270(t, "polite")
	defer h.Stop()

	h.mu.Lock()
	got := make(chan error, 1)
	go func() {
		err := h.lockForInteraction()
		if err == nil {
			h.mu.Unlock()
		}
		got <- err
	}()

	select {
	case err := <-got:
		h.mu.Unlock()
		t.Fatalf("the session was taken while it was already held (%v)", err)
	case <-time.After(200 * time.Millisecond):
	}

	h.mu.Unlock()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("the session was free and was not taken: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the session was released and the waiting caller never took it")
	}
}

// A screen read gives up its hold on the session between attempts. Holding it
// across the waits meant a read could sit on the session's one pipe for five
// seconds doing nothing, with the operator's next keystroke queued behind a
// lock whose holder was asleep.
func TestScreenReadLetsGoBetweenAttempts(t *testing.T) {
	h, _ := startFakeS3270(t, "lockedkeyboard")
	defer h.Stop()
	h.screen = blankScreen(24, 80)

	done := make(chan error, 1)
	go func() { done <- h.UpdateScreen() }()

	// The read is retrying and will keep retrying. If it held the session while
	// it waited, nothing else could take it.
	deadline := time.Now().Add(3 * time.Second)
	took := false
	for time.Now().Before(deadline) && !took {
		if h.mu.TryLock() {
			h.mu.Unlock()
			took = true
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !took {
		t.Error("nothing else could take the session while a screen read was waiting to try again")
	}
	<-done
}
