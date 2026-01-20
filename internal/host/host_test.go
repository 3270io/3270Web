package host

import (
	"testing"
)

func TestMockHost(t *testing.T) {
	// Adjust path relative to where test is run (internal/host)
	dumpPath := "../../webapp/WEB-INF/dump/advantis.dump"
	h, err := NewMockHost(dumpPath)
	if err != nil {
		t.Skipf("Skipping mock host test, dump not found: %v", err)
	}

	if err := h.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer h.Stop()

	if !h.IsConnected() {
		t.Error("MockHost should be connected")
	}

	if err := h.UpdateScreen(); err != nil {
		t.Errorf("UpdateScreen failed: %v", err)
	}

	s := h.GetScreen()
	if s == nil {
		t.Fatal("Screen is nil")
	}
	if s.Width != 80 {
		t.Errorf("Screen width mismatch: %d", s.Width)
	}

	// Test SendKey
	h.SendKey("Enter")
	if len(h.Commands) == 0 || h.Commands[0] != "key:Enter" {
		t.Errorf("SendKey failed, commands: %v", h.Commands)
	}

	// Test SubmitScreen
	h.SubmitScreen()
	// Should have "submit"
	if h.Commands[len(h.Commands)-1] != "submit" {
		t.Errorf("SubmitScreen failed, last cmd: %s", h.Commands[len(h.Commands)-1])
	}
}
