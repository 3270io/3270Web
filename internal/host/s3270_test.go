package host

import "testing"

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
