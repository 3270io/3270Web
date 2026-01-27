package host

import (
	"errors"
	"fmt"
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

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"simple error", errors.New("something went wrong"), false},
		{"not connected", errors.New("not connected"), true},
		{"Not Connected", errors.New("Not Connected"), true},
		{"terminated", errors.New("s3270 terminated"), true},
		{"no status", errors.New("no status received"), true},
		{"timed out", errors.New("s3270 command timed out"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"pipe closed", errors.New("read: pipe is being closed"), true},
		{"pipe ended", errors.New("pipe has been ended"), true},
		{"closed pipe", errors.New("io: closed pipe"), true},
		{"wrapped error", fmt.Errorf("context: %w", errors.New("broken pipe")), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConnectionError(tt.err); got != tt.expected {
				t.Errorf("isConnectionError(%q) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestIsS3270Error(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		data     []string
		expected bool
	}{
		{
			name:     "clean success",
			status:   "U F ...",
			data:     []string{"ok"},
			expected: false,
		},
		{
			name:     "status error",
			status:   "E F ...",
			data:     []string{"something"},
			expected: true,
		},
		{
			name:     "data error",
			status:   "U F ...",
			data:     []string{"Error: invalid command"},
			expected: true,
		},
		{
			name:     "data error lowercase",
			status:   "U F ...",
			data:     []string{"error: invalid command"},
			expected: true,
		},
		{
			name:     "multi-line data error",
			status:   "U F ...",
			data:     []string{"line 1", "error here", "line 3"},
			expected: true,
		},
		{
			name:     "no error",
			status:   "U F ...",
			data:     []string{"data 1", "data 2"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isS3270Error(tt.status, tt.data); got != tt.expected {
				t.Errorf("isS3270Error(%q, %v) = %v, want %v", tt.status, tt.data, got, tt.expected)
			}
		})
	}
}
