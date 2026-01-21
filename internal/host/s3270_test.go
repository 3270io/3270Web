package host

import "testing"

func TestWaitUnlockCommandUsesTimeout(t *testing.T) {
	if got := WaitUnlockCommandForTest(nil); got != "Wait(Unlock,10)" {
		t.Fatalf("expected wait unlock command with timeout, got %q", got)
	}
}
