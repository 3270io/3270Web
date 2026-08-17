// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"fmt"
	"strconv"
	"strings"
)

// The terminal's Snap action takes a copy of the display and keeps it, so the
// screen can be read back later without racing the host. That matters because
// every other read here goes to the *live* buffer: between the moment a caller
// decides to capture a screen and the moment it finishes reading it, the host
// may have written over it, and what comes back is then half of one screen and
// half of another. Snap closes that window — one command freezes the display,
// and the reads that follow are all of the same instant.
//
// The point of freezing it is comparison. A snapshot taken before a
// transaction and one taken after are the two sides of a diff, which is what
// turns "the screen changed" into "these four lines changed, here they are" —
// the raw material for a regression test that asserts a flow still lands on
// the screen it used to.

// Snapshot is a frozen copy of the display: what the terminal was showing at
// one instant, with the geometry and status line that went with it.
type Snapshot struct {
	Rows   int    `json:"rows"`
	Cols   int    `json:"cols"`
	Status string `json:"status"`
	Text   string `json:"text"`
}

// Lines splits a snapshot's text into rows with trailing blanks removed. A
// snapshot is a rectangle padded with spaces, and comparing padded rows makes
// every diff report the whole width of a line for a one-character change.
func (s *Snapshot) Lines() []string {
	if s == nil {
		return nil
	}
	raw := strings.Split(s.Text, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		out = append(out, strings.TrimRight(line, " "))
	}
	return out
}

// dataLines strips the "data: " prefix the terminal puts on each line of an
// action's output and returns what is left.
func dataLines(data []string) []string {
	out := make([]string, 0, len(data))
	for _, line := range data {
		out = append(out, strings.TrimPrefix(line, "data: "))
	}
	return out
}

// Snap freezes the current display in the terminal's snap buffer and reads it
// back. The four reads that follow the save are all answered from that buffer,
// so they describe a single instant even if the host writes over the live
// screen while they are in flight.
func (h *S3270) Snap() (*Snapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, status, err := h.doCommandLocked("Snap(Save)"); err != nil {
		return nil, err
	} else if isS3270Error(status, nil) {
		return nil, fmt.Errorf("s3270 Snap(Save) error: %s", status)
	}

	snap := &Snapshot{}

	// Geometry first. A build that does not answer Rows/Cols is not a reason to
	// fail the capture — the text is the part callers came for, and a zero here
	// says "the terminal did not say" rather than "the screen is empty".
	if n, err := h.snapIntLocked("Rows"); err == nil {
		snap.Rows = n
	}
	if n, err := h.snapIntLocked("Cols"); err == nil {
		snap.Cols = n
	}

	if data, status, err := h.doCommandLocked("Snap(Status)"); err != nil {
		return nil, err
	} else if !isS3270Error(status, data) {
		snap.Status = strings.TrimSpace(strings.Join(dataLines(data), " "))
	}

	data, status, err := h.doCommandLocked("Snap(Ascii)")
	if err != nil {
		return nil, err
	}
	if isS3270Error(status, data) {
		return nil, fmt.Errorf("s3270 Snap(Ascii) error: %s", status)
	}
	snap.Text = strings.Join(dataLines(data), "\n")

	// Fall back to the shape of what came back when the terminal did not answer
	// the geometry queries, so a snapshot always knows its own dimensions.
	lines := strings.Split(snap.Text, "\n")
	if snap.Rows == 0 {
		snap.Rows = len(lines)
	}
	if snap.Cols == 0 {
		for _, line := range lines {
			if n := len([]rune(line)); n > snap.Cols {
				snap.Cols = n
			}
		}
	}
	return snap, nil
}

// snapIntLocked reads one numeric field out of the snap buffer. The caller
// must hold h.mu.
func (h *S3270) snapIntLocked(field string) (int, error) {
	data, status, err := h.doCommandLocked("Snap(" + field + ")")
	if err != nil {
		return 0, err
	}
	if isS3270Error(status, data) {
		return 0, fmt.Errorf("s3270 Snap(%s) error: %s", field, status)
	}
	for _, line := range dataLines(data) {
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("s3270 Snap(%s) returned no number", field)
}

// SnapshotDiffLine is one row that differs between two snapshots. Row is
// 1-indexed, matching how an operator counts lines on a screen and how the
// rest of this codebase quotes screen positions.
type SnapshotDiffLine struct {
	Row   int    `json:"row"`
	Left  string `json:"left"`
	Right string `json:"right"`
}

// SnapshotDiff is the comparison of two snapshots.
type SnapshotDiff struct {
	Identical bool               `json:"identical"`
	Lines     []SnapshotDiffLine `json:"lines"`
}

// DiffSnapshots compares two snapshots row by row and returns the rows that
// differ. Rows present in only one of the two are reported against an empty
// string rather than dropped, so a screen that grew or shrank shows up as a
// difference instead of silently matching on its common prefix.
func DiffSnapshots(left, right *Snapshot) *SnapshotDiff {
	leftLines := left.Lines()
	rightLines := right.Lines()
	n := len(leftLines)
	if len(rightLines) > n {
		n = len(rightLines)
	}
	diff := &SnapshotDiff{Lines: []SnapshotDiffLine{}}
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		if i < len(rightLines) {
			r = rightLines[i]
		}
		if l != r {
			diff.Lines = append(diff.Lines, SnapshotDiffLine{Row: i + 1, Left: l, Right: r})
		}
	}
	diff.Identical = len(diff.Lines) == 0
	return diff
}
