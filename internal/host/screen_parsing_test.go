package host

import (
	"strings"
	"testing"
)

func TestScreenParsing(t *testing.T) {
	tests := []struct {
		name          string
		dump          string
		wantStatus    bool // whether we expect status to be captured
		wantFormatted bool
		wantContent   string // partial content match
	}{
		{
			name: "Standard 24x80 Formatted",
			dump: `data: 41 42 43
U F U C(127.0.0.1) I 4 24 80 0 0 0x0 0.000
ok`,
			wantStatus:    true,
			wantFormatted: true,
			wantContent:   "ABC",
		},
		{
			name: "Standard Unformatted",
			dump: `data: 41 42 43
U U U N N 4 24 80 0 0 0x0 0.000
ok`,
			wantStatus:    true,
			wantFormatted: false,
			wantContent:   "ABC",
		},
		{
			name: "Status with Extra Field (Future Proofing Risk)",
			// If s3270 adds a field, our regex with $ anchor fails.
			dump: `data: 41 42 43
U F U C(127.0.0.1) I 4 24 80 0 0 0x0 0.000 EXTRA
ok`,
			wantStatus:    true, // We want this to pass, but it will fail initially
			wantFormatted: true,
			wantContent:   "ABC",
		},
		{
			name: "Data Corruption - Invalid Hex",
			dump: `data: 41 ZZ 42
U F U C(127.0.0.1) I 4 24 80 0 0 0x0 0.000
ok`,
			wantStatus:    true,
			wantFormatted: true,
			wantContent:   "AB", // ZZ is dropped
		},
		{
			name: "Mixed valid and invalid tokens",
			dump: "data: 41 42 43 ZZ 44\nU F U C(1.2.3.4) I 4 24 80 0 0 0x0 0.0\nok",
			wantStatus:    true,
			wantFormatted: true,
			wantContent:   "ABCD",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := strings.NewReader(tc.dump)
			s, err := NewScreenFromDump(r)
			if err != nil {
				t.Fatalf("NewScreenFromDump failed: %v", err)
			}

			hasStatus := s.Status != ""
			if hasStatus != tc.wantStatus {
				t.Errorf("Status captured: got %v, want %v. Status content: %q", hasStatus, tc.wantStatus, s.Status)
			}

			if s.IsFormatted != tc.wantFormatted {
				t.Errorf("IsFormatted: got %v, want %v", s.IsFormatted, tc.wantFormatted)
			}

			var sb strings.Builder
			if s.Buffer != nil {
				for _, row := range s.Buffer {
					sb.WriteString(string(row))
				}
			}
			content := sb.String()
			if !strings.Contains(content, tc.wantContent) {
				t.Errorf("Content mismatch: got %q, want substring %q", content, tc.wantContent)
			}
		})
	}
}
