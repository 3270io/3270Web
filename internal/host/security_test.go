package host

import (
	"strings"
	"testing"
)

func TestSendKey_SecurityValidation(t *testing.T) {
	h := NewS3270("/bin/true") // Path doesn't matter for this test

	dangerousKeys := []string{
		"Enter\nQuit",
		"PF1\r",
		"String(foo)\t",
		"Enter;Quit",
	}

	for _, key := range dangerousKeys {
		err := h.SendKey(key)
		if err == nil {
			t.Errorf("SendKey(%q) expected error, got nil", key)
		} else if !strings.Contains(err.Error(), "security error") {
			t.Errorf("SendKey(%q) expected security error, got %v", key, err)
		}
	}
}
