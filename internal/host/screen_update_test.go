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
		{"3279", 0, 0, false}, // Incomplete model string
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

// TestScreenDimensionsFromStatusEnforcesLimits verifies that screen dimensions are clamped
// to the standard limits for the detected model.
// See docs/terminal-model-limits.md for details and examples.
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

func TestParseHexByte(t *testing.T) {
	tests := []struct {
		input    string
		expected byte
		hasError bool
	}{
		{"00", 0x00, false},
		{"FF", 0xFF, false},
		{"ff", 0xFF, false},
		{"A1", 0xA1, false},
		{"1a", 0x1A, false},
		{"9F", 0x9F, false},
		{"G1", 0, true},
		{"1G", 0, true},
		{"1", 0x01, false}, // Current implementation allows "1", checking correctness for now
		{"123", 0, true},
		{"", 0, true},
		{"-1", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseHexByte(tt.input)
			if tt.hasError {
				if err == nil {
					t.Errorf("parseHexByte(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseHexByte(%q) expected no error, got %v", tt.input, err)
				}
				if got != tt.expected {
					t.Errorf("parseHexByte(%q) = %x, want %x", tt.input, got, tt.expected)
				}
			}
		})
	}
}

// TestScreenWidthTruncation verifies that the screen width is properly truncated
// to the model-specific dimension limits, even when the buffer contains more data.
func TestScreenWidthTruncation(t *testing.T) {
// Create a status line for model 2 (24x80) but with the backend reporting 24x80
status := "U F P C(localhost) I 2 24 80 0 0 0x0 0.000"

// Create a data line with exactly 80 columns worth of data
tokens := make([]string, 80)
for i := 0; i < 80; i++ {
tokens[i] = "41" // Character 'A'
}
dataLine := "data: " + strings.Join(tokens, " ")

screen := &Screen{}
err := screen.Update(status, []string{dataLine})
if err != nil {
t.Fatalf("Update failed: %v", err)
}

// The screen width should be exactly 80, matching the model 2 limit
if screen.Width != 80 {
t.Errorf("expected screen width to be 80 for model 2, got %d", screen.Width)
}

// Now test with a wider buffer (simulating what happens when s3270 reports wrong dims)
// Create status for model 2, but with buffer containing data beyond 80 columns
wideTokens := make([]string, 100)
for i := 0; i < 100; i++ {
wideTokens[i] = "42" // Character 'B'
}
wideDataLine := "data: " + strings.Join(wideTokens, " ")

screen2 := &Screen{}
err = screen2.Update(status, []string{wideDataLine})
if err != nil {
t.Fatalf("Update with wide buffer failed: %v", err)
}

// Even though we have 100 tokens, the width should be clamped to 80 for model 2
if screen2.Width != 80 {
t.Errorf("expected screen width to be truncated to 80 for model 2, got %d", screen2.Width)
}
}
