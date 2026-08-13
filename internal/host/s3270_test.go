package host

import (
	"errors"
	"testing"
)

func TestWaitUnlockCommandUsesTimeout(t *testing.T) {
	if got := WaitUnlockCommandForTest(nil); got != "Wait(Unlock,10)" {
		t.Fatalf("expected wait unlock command with timeout, got %q", got)
	}
}

func TestIsKeyboardUnlocked(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected bool
	}{
		{
			name:     "unlocked status",
			status:   "U F P C(localhost) I 4 24 80 0 0 0x0 0.000",
			expected: true,
		},
		{
			name:     "locked status",
			status:   "L F P C(localhost) I 4 24 80 0 0 0x0 0.000",
			expected: false,
		},
		{
			name:     "empty status",
			status:   "",
			expected: false,
		},
		{
			name:     "short status unlocked",
			status:   "U F",
			expected: true,
		},
		{
			name:     "short status locked",
			status:   "L F",
			expected: false,
		},
		{
			name:     "single character U",
			status:   "U",
			expected: false,
		},
		{
			name:     "single character L",
			status:   "L",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isKeyboardUnlocked(tt.status)
			if got != tt.expected {
				t.Errorf("isKeyboardUnlocked(%q) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestKeyToKeySpec(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "Enter"},
		{"   ", "Enter"},
		{"Enter", "Enter"},
		{"enter", "enter"},
		{"PF(1)", "PF1"},
		{"pf(1)", "PF1"},
		{"PF(12)", "PF12"},
		{"PF(24)", "PF24"},
		{"PA(1)", "PA1"},
		{"pa(2)", "PA2"},
		{"PF1", "PF1"}, // Already correct
		{"Clear", "Clear"},
		{"PF(a)", "PF(a)"}, // Invalid number, returns trimmed input
		{"PA()", "PA()"},   // Invalid format
		{"Something", "Something"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := keyToKeySpec(tt.input)
			if got != tt.expected {
				t.Errorf("keyToKeySpec(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsAidKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"Enter", true},
		{"ENTER", true},
		{"PF1", true},
		{"pf1", true},
		{"PF24", true},
		{"PA1", true},
		{"pa3", true},
		{"Clear", true},
		{"SysReq", true},
		{"Attn", true},
		{"a", false},
		{"1", false},
		{"Tab", false},
		{"BackTab", false},
		{"Reset", false},
		{"String", false},
		{"PF", true}, // "PF" prefix match
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := isAidKey(tt.key)
			if got != tt.expected {
				t.Errorf("isAidKey(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestEscapeForS3270String(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain ascii", "HELLO", "HELLO"},
		{"with space", "A B C", "A B C"},
		{"all printable", "!@#$%^&*()_+-={}[]:;'<>,.?/", "!@#$%^&*()_+-={}[]:;'<>,.?/"},
		{"double quote", `say "hi"`, `say \"hi\"`},
		{"backslash", `C:\path`, `C:\\path`},
		{"both metachars", `\"x"`, `\\\"x\"`},
		{"tab is non-printable", "a\tb", `a\x09b`},
		{"cr is non-printable", "a\rb", `a\x0db`},
		{"del is non-printable", "a\x7fb", `a\x7fb`},
		// Above ASCII goes out as \u — one escape naming one character. Spelling
		// the character out as its UTF-8 bytes types each byte as a character of
		// its own, so "é" arrived as "Ã©" and everything after it in the field
		// moved a cell to the right.
		{"high ascii latin1 rune", "é", `\u00e9`},
		{"multibyte cjk rune", "中", `\u4e2d`},
		{"pound sign", "£", `\u00a3`},
		// Four hex digits is the whole of the escape, and a surrogate pair is
		// dropped rather than composed, so anything above the BMP is replaced
		// with a character that keeps the field's shape and can be seen.
		{"emoji beyond the bmp", "\U0001f600", "�"},
		{"mixed", `A\"B` + "é", `A\\\"B\u00e9`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeForS3270String(tt.input)
			if got != tt.want {
				t.Errorf("escapeForS3270String(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"not connected", errors.New("Not connected"), true},
		{"terminated", errors.New("s3270 terminated unexpectedly"), true},
		{"no status", errors.New("No status received from host"), true},
		{"timed out", errors.New("command timed out"), true},
		{"pipe closed", errors.New("pipe is being closed"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"pipe ended", errors.New("pipe has been ended"), true},
		{"closed pipe", errors.New("read: closed pipe"), true},
		{"other error", errors.New("something went wrong"), false},
		{"empty error", errors.New(""), false},
		{"mixed case", errors.New("NOT CONNECTED"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConnectionError(tt.err)
			if got != tt.expected {
				t.Errorf("isConnectionError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
