package host

import (
	"os"
	"strings"
	"testing"
)

func TestParseDump(t *testing.T) {
	// Adjust path relative to where test is run (internal/host)
	dumpPath := "../../webapp/WEB-INF/dump/advantis.dump"
	f, err := os.Open(dumpPath)
	if err != nil {
		t.Skipf("Skipping test, dump file not found at %s: %v", dumpPath, err)
	}
	defer f.Close()

	screen, err := NewScreenFromDump(f)
	if err != nil {
		t.Fatalf("Failed to parse dump: %v", err)
	}

	// Verify dimensions from status line: "I 3 32 80 ..." => 32 rows, 80 cols
	// Note: The actual buffer height depends on the number of "data:" lines provided.
	if screen.Width != 80 {
		t.Errorf("Expected width 80, got %d", screen.Width)
	}

	// Check Cursor
	// Status: ... 28 12 ...
	if screen.CursorX != 12 {
		t.Errorf("Expected cursor X 12, got %d", screen.CursorX)
	}
	if screen.CursorY != 28 {
		t.Errorf("Expected cursor Y 28, got %d", screen.CursorY)
	}

	// Check Content
	// Line 1 should contain "SYSTEM:"
	// 53 59 53 54 45 4d 3a = SYSTEM:
	row1 := screen.Substring(0, 1, 79, 1)
	if !strings.Contains(row1, "SYSTEM:") {
		t.Errorf("Expected 'SYSTEM:' in row 1, got '%s'", row1)
	}

	// Check Fields
	if len(screen.Fields) == 0 {
		t.Fatal("Expected fields to be parsed, got 0")
	}

	// Verify some field properties
	// The dump has SF(c0=e0) which is Protected (0x20) | Numeric (0x10) | ...?
	// e0 = 1110 0000 = Protected (0x20) ?
	// 0x20 = 0010 0000.
	// e0 & 0x20 = 0x20. Yes, Protected.
	// e0 & 0x10 = 0 (No).
	// Wait. 0xe0 = 1110 0000.
	// 0x20 = 0010 0000. Yes.

	firstField := screen.Fields[0]
	if !firstField.IsProtected() {
		t.Errorf("Expected first field to be protected (code 0xe0), got code 0x%x", firstField.FieldCode)
	}
}
