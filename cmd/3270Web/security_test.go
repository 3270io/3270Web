package main

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func TestApplyWorkflowStep_Sanitization(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("failed to create mock host: %v", err)
	}
	sess := &session.Session{Host: mockHost, Playback: &session.WorkflowPlayback{Active: true}}
	app := &App{}

	// Malicious step: Try to execute a "Connect" command via "Press..."
	// Without sanitization, this would execute "Connect(evil.com)"
	step := session.WorkflowStep{
		Type: "PressConnect(evil.com)",
	}

	if err := app.applyWorkflowStep(sess, step); err != nil {
		t.Fatalf("applyWorkflowStep failed: %v", err)
	}

	// Check commands sent to mock host
	// normalizeKey("Connect(evil.com)") returns "Enter" (whitelisted default)
	// So we expect "Enter" command.
	if len(mockHost.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(mockHost.Commands))
	}

	// mockHost.SendKey adds "key:" prefix
	expected := "key:Enter"
	if mockHost.Commands[0] != expected {
		t.Errorf("Security check failed: expected sanitized command %q, got %q. The key was not whitelisted!", expected, mockHost.Commands[0])
	}
}
