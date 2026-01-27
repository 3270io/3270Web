package host

import (
	"strings"
	"testing"
)

func TestNormalizeScreenLinesSplitsSingleRowBuffers(t *testing.T) {
	rows := 3
	cols := 4
	tokens := []string{
		"00", "01", "02", "03",
		"04", "05", "06", "07",
		"08", "09", "0A", "0B",
	}
	line := "data: " + strings.Join(tokens, " ")
	lines := normalizeScreenLinesForTest([]string{line}, rows, cols)
	if got := len(lines); got != rows {
		t.Fatalf("expected %d rows after normalization, got %d", rows, got)
	}
	for i := 0; i < rows; i++ {
		expected := "data: " + strings.Join(tokens[i*cols:(i+1)*cols], " ")
		if lines[i] != expected {
			t.Errorf("row %d mismatch: got %q want %q", i, lines[i], expected)
		}
	}
}

func TestNormalizeScreenLinesSkipsDuplicateRows(t *testing.T) {
	rows := 2
	cols := 3
	tokens := []string{
		"01", "02", "03",
		"01", "02", "03",
		"01", "02", "03",
	}
	line := "data: " + strings.Join(tokens, " ")
	lines := normalizeScreenLinesForTest([]string{line}, rows, cols)
	if got := len(lines); got != 1 {
		t.Fatalf("expected normalization to be skipped, got %d lines", got)
	}
	if lines[0] != line {
		t.Fatalf("expected original line preserved, got %q", lines[0])
	}
	if !repeatsScreenForTest(tokens, rows, cols, len(tokens)/cols) {
		t.Fatalf("expected duplicatesOnly to report true")
	}
}
