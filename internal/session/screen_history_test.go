// SPDX-License-Identifier: AGPL-3.0-or-later

package session

import (
	"fmt"
	"testing"
	"time"
)

func entry(text string) ScreenHistoryEntry {
	return ScreenHistoryEntry{Text: text, Rows: 24, Cols: 80, At: time.Now()}
}

func TestRecordScreenAppendsInOrder(t *testing.T) {
	s := &Session{}
	s.RecordScreen(entry("SCREEN ONE"))
	s.RecordScreen(entry("SCREEN TWO"))

	got := s.ScreenHistorySnapshot()
	if len(got) != 2 {
		t.Fatalf("history length = %d, want 2", len(got))
	}
	if got[0].Text != "SCREEN ONE" || got[1].Text != "SCREEN TWO" {
		t.Errorf("history out of order: %q, %q", got[0].Text, got[1].Text)
	}
}

// Several display paths can fire for the same screen — a submit's refresh and
// then the idle SSE poll — so without dedup the history would record how often
// the client asked rather than which screens the operator saw.
func TestRecordScreenIgnoresConsecutiveRepeats(t *testing.T) {
	s := &Session{}
	s.RecordScreen(entry("MENU"))
	s.RecordScreen(entry("MENU"))
	s.RecordScreen(entry("MENU"))

	if got := s.ScreenHistorySnapshot(); len(got) != 1 {
		t.Fatalf("history length = %d, want 1", len(got))
	}
}

// A screen the operator navigates back to is a real event, not a repeat.
func TestRecordScreenKeepsNonConsecutiveRepeats(t *testing.T) {
	s := &Session{}
	s.RecordScreen(entry("MENU"))
	s.RecordScreen(entry("DETAIL"))
	s.RecordScreen(entry("MENU"))

	got := s.ScreenHistorySnapshot()
	if len(got) != 3 {
		t.Fatalf("history length = %d, want 3", len(got))
	}
}

func TestRecordScreenIgnoresBlankScreens(t *testing.T) {
	s := &Session{}
	s.RecordScreen(entry(""))
	s.RecordScreen(entry("   \n   "))

	if got := s.ScreenHistorySnapshot(); len(got) != 0 {
		t.Fatalf("history length = %d, want 0 — a blank screen is not worth keeping", len(got))
	}
}

func TestRecordScreenEvictsOldestPastLimit(t *testing.T) {
	s := &Session{}
	total := ScreenHistoryLimit + 10
	for i := 0; i < total; i++ {
		s.RecordScreen(entry(fmt.Sprintf("SCREEN %03d", i)))
	}

	got := s.ScreenHistorySnapshot()
	if len(got) != ScreenHistoryLimit {
		t.Fatalf("history length = %d, want %d", len(got), ScreenHistoryLimit)
	}
	wantFirst := fmt.Sprintf("SCREEN %03d", total-ScreenHistoryLimit)
	if got[0].Text != wantFirst {
		t.Errorf("oldest kept = %q, want %q", got[0].Text, wantFirst)
	}
	wantLast := fmt.Sprintf("SCREEN %03d", total-1)
	if got[len(got)-1].Text != wantLast {
		t.Errorf("newest = %q, want %q", got[len(got)-1].Text, wantLast)
	}
}

// The snapshot must not alias the session's slice, or a caller ranging over it
// while the host pushes a new screen would race.
func TestScreenHistorySnapshotIsACopy(t *testing.T) {
	s := &Session{}
	s.RecordScreen(entry("ORIGINAL"))

	got := s.ScreenHistorySnapshot()
	got[0].Text = "MUTATED"

	if again := s.ScreenHistorySnapshot(); again[0].Text != "ORIGINAL" {
		t.Errorf("snapshot aliases session state: got %q", again[0].Text)
	}
}

func TestRecordScreenOnNilSessionIsSafe(t *testing.T) {
	var s *Session
	s.RecordScreen(entry("SCREEN"))
	if got := s.ScreenHistorySnapshot(); got != nil {
		t.Errorf("nil session returned %v", got)
	}
}
