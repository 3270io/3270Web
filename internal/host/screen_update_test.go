// SPDX-License-Identifier: AGPL-3.0-or-later

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
			// An oversize display keeps its model number and reports the
			// larger screen it is actually showing. The terminal is the
			// authority on its own size.
			name:         "Model 2 running oversize",
			status:       "U F P C(localhost) I 2 30 90 0 0 0x0 0.000",
			expectedRows: 30,
			expectedCols: 90,
			expectedOk:   true,
		},
		{
			// A screen too large for a 3270 buffer address is a misread
			// status line, and the model is the better answer.
			name:         "Dimensions beyond any 3270 buffer fall back to the model",
			status:       "U F P C(localhost) I 2 400 400 0 0 0x0 0.000",
			expectedRows: 24,
			expectedCols: 80,
			expectedOk:   true,
		},
		{
			name:         "Impossible dimensions on an unknown model are refused",
			status:       "U F P C(localhost) I 1 400 400 0 0 0x0 0.000",
			expectedRows: 0,
			expectedCols: 0,
			expectedOk:   false,
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

// TestScreenWidthTruncation verifies that the screen width follows the size the
// terminal reports, even when the buffer read back contains more data than that.
func TestScreenWidthTruncation(t *testing.T) {
	// Create a status line for model 2 (24x80) with matching backend dimensions
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

	// Now test with a buffer wider than the screen the terminal says it is
	// showing. The status line is the authority on the display's size, so the
	// extra columns are trimmed to it.
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

// TestOversizeScreenKeepsTheDisplayTheHostDrew covers the display configured
// larger than its model. The terminal reports the larger screen with the model
// number unchanged, and every row and column of it belongs to the operator —
// cropping back to the model's size would throw away the part of the screen the
// oversize setting was switched on for.
func TestOversizeScreenKeepsTheDisplayTheHostDrew(t *testing.T) {
	status := "U F P C(localhost) I 2 30 100 0 0 0x0 0.000"

	tokens := make([]string, 100)
	for i := 0; i < 100; i++ {
		tokens[i] = "41" // Character 'A'
	}
	dataLine := "data: " + strings.Join(tokens, " ")

	screen := &Screen{}
	err := screen.Update(status, []string{dataLine})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if screen.Width != 100 {
		t.Errorf("expected width 100 on an oversize display, got %d", screen.Width)
	}
	if screen.Height != 30 {
		t.Errorf("expected height 30 on an oversize display, got %d", screen.Height)
	}
	if got := screen.CharAt(99, 0); got != 'A' {
		t.Errorf("expected the last column of the first row to survive, got %q", got)
	}
}

func TestUpdateFromTextNormalizesCRLF(t *testing.T) {
	screen := &Screen{}
	screen.UpdateFromText("AB\r\nCD\r\n")

	if screen.Height != 3 {
		t.Fatalf("expected 3 lines (including trailing blank line), got %d", screen.Height)
	}
	if got := string(screen.Buffer[0][:2]); got != "AB" {
		t.Fatalf("row 0 = %q, want %q", got, "AB")
	}
	if got := string(screen.Buffer[1][:2]); got != "CD" {
		t.Fatalf("row 1 = %q, want %q", got, "CD")
	}
	if len(screen.Buffer[2]) > 0 && screen.Buffer[2][0] == '\r' {
		t.Fatalf("trailing row contains raw carriage return")
	}
}

// Two field attributes side by side open and close a field that never reaches
// a screen position. It is kept — it owns its attribute byte, and the columns
// after it are placed by walking the field list — but it holds no text, and
// reading its value used to run off the end of the field and return the rest of
// the screen.
func TestUpdateKeepsZeroLengthFieldsEmpty(t *testing.T) {
	dump := `data: SF(c0=20) SF(c0=28) 41 42 SF(c0=20) 43
ok`

	s, err := NewScreenFromDump(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("NewScreenFromDump failed: %v", err)
	}

	if len(s.Fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(s.Fields))
	}
	if !s.Fields[0].IsZeroLength() {
		t.Fatalf("field 0 (%d,%d)-(%d,%d) is not reported as zero length",
			s.Fields[0].StartX, s.Fields[0].StartY, s.Fields[0].EndX, s.Fields[0].EndY)
	}
	if got := s.Fields[0].GetValue(); got != "" {
		t.Fatalf("zero-length field value = %q, want empty", got)
	}
	if got := s.Fields[1].GetValue(); got != "AB" {
		t.Fatalf("second field value = %q, want %q", got, "AB")
	}
	if got := s.Fields[2].GetValue(); got != "C" {
		t.Fatalf("third field value = %q, want %q", got, "C")
	}
}

// A zero-length field whose attribute sits in the last column of a row ends on
// the row before it starts, which is the same emptiness reached by a different
// route.
func TestUpdateKeepsZeroLengthFieldAtRowEnd(t *testing.T) {
	dump := `data: SF(c0=20) 41 42 SF(c0=20)
data: SF(c0=20) 43 44 45
ok`

	s, err := NewScreenFromDump(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("NewScreenFromDump failed: %v", err)
	}

	if len(s.Fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(s.Fields))
	}
	if !s.Fields[1].IsZeroLength() {
		t.Fatalf("field 1 (%d,%d)-(%d,%d) is not reported as zero length",
			s.Fields[1].StartX, s.Fields[1].StartY, s.Fields[1].EndX, s.Fields[1].EndY)
	}
	if got := s.Fields[1].GetValue(); got != "" {
		t.Fatalf("zero-length field value = %q, want empty", got)
	}
	if got := s.Fields[2].GetValue(); got != "CDE" {
		t.Fatalf("field after the zero-length one = %q, want %q", got, "CDE")
	}
}
