// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import "testing"

func TestSnapshotLinesDropTrailingBlanks(t *testing.T) {
	snap := &Snapshot{Text: "HELLO   \nWORLD\n        "}
	got := snap.Lines()
	want := []string{"HELLO", "WORLD", ""}
	if len(got) != len(want) {
		t.Fatalf("Lines() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Lines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A screen is a rectangle padded with spaces. If the padding were compared,
// two screens that differ in one character would report every row that had a
// different amount of trailing whitespace as a difference too.
func TestDiffSnapshotsIgnoresTrailingPadding(t *testing.T) {
	left := &Snapshot{Text: "SIGN ON\nUSERID:"}
	right := &Snapshot{Text: "SIGN ON     \nUSERID:  "}
	diff := DiffSnapshots(left, right)
	if !diff.Identical {
		t.Fatalf("padding-only difference reported as a change: %+v", diff.Lines)
	}
}

func TestDiffSnapshotsReportsChangedRowsOneIndexed(t *testing.T) {
	left := &Snapshot{Text: "SIGN ON\nBALANCE 100\nREADY"}
	right := &Snapshot{Text: "SIGN ON\nBALANCE 250\nREADY"}
	diff := DiffSnapshots(left, right)
	if diff.Identical {
		t.Fatal("a changed row was reported as identical")
	}
	if len(diff.Lines) != 1 {
		t.Fatalf("Lines = %+v, want exactly one changed row", diff.Lines)
	}
	line := diff.Lines[0]
	if line.Row != 2 {
		t.Errorf("Row = %d, want 2 (rows are counted the way an operator counts them)", line.Row)
	}
	if line.Left != "BALANCE 100" || line.Right != "BALANCE 250" {
		t.Errorf("Left/Right = %q/%q, want the two versions of the row", line.Left, line.Right)
	}
}

// A screen that grew or shrank is a difference, not a match on the common
// prefix — the missing rows are exactly what a regression test is looking for.
func TestDiffSnapshotsReportsRowsPresentInOnlyOne(t *testing.T) {
	left := &Snapshot{Text: "ROW1\nROW2\nROW3"}
	right := &Snapshot{Text: "ROW1"}
	diff := DiffSnapshots(left, right)
	if len(diff.Lines) != 2 {
		t.Fatalf("Lines = %+v, want the two rows that only one side has", diff.Lines)
	}
	if diff.Lines[0].Right != "" || diff.Lines[0].Left != "ROW2" {
		t.Errorf("missing row reported as %+v, want it against an empty string", diff.Lines[0])
	}
}

func TestDataLinesStripsThePrefix(t *testing.T) {
	got := dataLines([]string{"data: one", "data: ", "two"})
	want := []string{"one", "", "two"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dataLines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
