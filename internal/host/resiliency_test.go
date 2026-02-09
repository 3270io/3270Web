package host

import (
	"strings"
	"testing"
)

// TestScreenParsing_Resiliency reproduces a bug where invalid tokens cause the screen
// logic to drop the token, leading to misalignment or incorrect row splitting.
func TestScreenParsing_Resiliency(t *testing.T) {
	// Scenario: 2x2 Screen (4 cells).
	// Input: 4 tokens. One is invalid ("ZZ").
	// s3270 output is typically one long line or broken lines.
	// Here we simulate one line with a hole in it.
	// Expected Behavior: The invalid token should be treated as a placeholder (e.g. space/null)
	// so that the total token count remains 4, allowing correct splitting into 2 rows.
	//
	// Current Behavior (Bug): The invalid token is dropped. Token count becomes 3.
	// 3 is not divisible by 2 (cols). Logic falls back to single row mode.
	// Screen becomes 1x3 instead of 2x2.

	dump := `data: 41 42 ZZ 43
U F U C(127.0.0.1) I 4 2 2 0 0 0x0 0.000
ok`

	r := strings.NewReader(dump)
	s, err := NewScreenFromDump(r)
	if err != nil {
		t.Fatalf("NewScreenFromDump failed: %v", err)
	}

	// 1. Verify Dimensions
	expectedHeight := 2
	expectedWidth := 2 // From status line
	if s.Height != expectedHeight {
		t.Errorf("Resiliency Failure: Expected Height %d, got %d. (Screen collapsed due to dropped token?)", expectedHeight, s.Height)
	}
	if s.Width != expectedWidth {
		t.Errorf("Resiliency Failure: Expected Width %d, got %d.", expectedWidth, s.Width)
	}

	// 2. Verify Content Alignment
	// Row 0 should be "AB" (41 42)
	// Row 1 should be " C" (ZZ 43) -> ZZ becomes space/null, 43 is C.

	if s.Height >= 1 {
		row0 := string(s.Buffer[0])
		if row0 != "AB" {
			t.Errorf("Row 0 content mismatch. Expected %q, got %q", "AB", row0)
		}
	}

	if s.Height >= 2 {
		// Expect space + C. Null byte (0x00) renders as space in string conversion?
		// string(rune(0)) is "\x00".
		// We can check rune values directly.
		if len(s.Buffer[1]) != 2 {
			t.Errorf("Row 1 length mismatch. Expected 2, got %d", len(s.Buffer[1]))
		} else {
			r1c1 := s.Buffer[1][0]
			r1c2 := s.Buffer[1][1]

			// We expect r1c1 to be placeholder (0 or space).
			// We expect r1c2 to be 'C' (0x43).

			if r1c2 != 'C' {
				t.Errorf("Row 1 Col 1 mismatch. Expected 'C', got %q. (Alignment shifted?)", r1c2)
			}

			if r1c1 != 0 && r1c1 != ' ' {
				t.Errorf("Row 1 Col 0 mismatch. Expected 0 or Space, got %q (0x%x)", r1c1, r1c1)
			}
		}
	}
}
