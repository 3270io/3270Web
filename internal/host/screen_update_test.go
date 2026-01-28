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

func TestGetModelDimensions(t *testing.T) {
	tests := []struct {
		model         string
		expectedRows  int
		expectedCols  int
		expectedValid bool
	}{
		{"2", 24, 80, true},
		{"3", 32, 80, true},
		{"4", 43, 80, true},
		{"5", 27, 132, true},
		{"3279-2", 24, 80, true},
		{"3279-2-E", 24, 80, true},
		{"3279-3-E", 32, 80, true},
		{"3279-4-E", 43, 80, true},
		{"3279-5-E", 27, 132, true},
		{"3279", 0, 0, false},      // Incomplete model string
		{"invalid", 0, 0, false},
		{"", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			rows, cols, valid := getModelDimensions(tt.model)
			if rows != tt.expectedRows {
				t.Errorf("model %q: expected rows=%d, got %d", tt.model, tt.expectedRows, rows)
			}
			if cols != tt.expectedCols {
				t.Errorf("model %q: expected cols=%d, got %d", tt.model, tt.expectedCols, cols)
			}
			if valid != tt.expectedValid {
				t.Errorf("model %q: expected valid=%v, got %v", tt.model, tt.expectedValid, valid)
			}
		})
	}
}

func TestScreenDimensionsFromStatusEnforcesLimits(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		expectedRows int
		expectedCols int
		expectedOk   bool
	}{
		{
			name:         "Model 2 with correct dimensions",
			status:       "U F P C(localhost) I 2 24 80 0 0 0x0 0.000",
			expectedRows: 24,
			expectedCols: 80,
			expectedOk:   true,
		},
		{
			name:         "Model 2 with dimensions exceeding limit",
			status:       "U F P C(localhost) I 2 30 90 0 0 0x0 0.000",
			expectedRows: 24, // Enforced to model 2 limit
			expectedCols: 80, // Enforced to model 2 limit
			expectedOk:   true,
		},
		{
			name:         "Model 2 with dimensions below limit (should be preserved)",
			status:       "U F P C(localhost) I 2 20 60 0 0 0x0 0.000",
			expectedRows: 20, // Preserved as-is
			expectedCols: 60, // Preserved as-is
			expectedOk:   true,
		},
		{
			name:         "Model 3 with correct dimensions",
			status:       "U F P C(localhost) I 3 32 80 0 0 0x0 0.000",
			expectedRows: 32,
			expectedCols: 80,
			expectedOk:   true,
		},
		{
			name:         "Model 4 with correct dimensions",
			status:       "U F P C(localhost) I 4 43 80 0 0 0x0 0.000",
			expectedRows: 43,
			expectedCols: 80,
			expectedOk:   true,
		},
		{
			name:         "Model 5 with correct dimensions",
			status:       "U F P C(localhost) I 5 27 132 0 0 0x0 0.000",
			expectedRows: 27,
			expectedCols: 132,
			expectedOk:   true,
		},
		{
			name:         "Unrecognized model (should preserve dimensions)",
			status:       "U F P C(localhost) I 1 30 90 0 0 0x0 0.000",
			expectedRows: 30, // Preserved as-is when model not recognized
			expectedCols: 90, // Preserved as-is when model not recognized
			expectedOk:   true,
		},
		{
			name:         "Empty status",
			status:       "",
			expectedRows: 0,
			expectedCols: 0,
			expectedOk:   false,
		},
		{
			name:         "Invalid status",
			status:       "invalid",
			expectedRows: 0,
			expectedCols: 0,
			expectedOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, cols, ok := screenDimensionsFromStatus(tt.status)
			if rows != tt.expectedRows {
				t.Errorf("expected rows=%d, got %d", tt.expectedRows, rows)
			}
			if cols != tt.expectedCols {
				t.Errorf("expected cols=%d, got %d", tt.expectedCols, cols)
			}
			if ok != tt.expectedOk {
				t.Errorf("expected ok=%v, got %v", tt.expectedOk, ok)
			}
		})
	}
}
