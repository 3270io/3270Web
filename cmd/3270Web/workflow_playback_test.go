package main

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func TestStopWorkflowPlaybackUpdatesState(t *testing.T) {
	sess := &session.Session{Playback: &session.WorkflowPlayback{Active: true, Paused: true}}

	stopWorkflowPlayback(sess)

	var stopRequested, stepRequested, paused bool
	var events []session.WorkflowEvent
	withSessionLock(sess, func() {
		if sess.Playback != nil {
			stopRequested = sess.Playback.StopRequested
			stepRequested = sess.Playback.StepRequested
			paused = sess.Playback.Paused
		}
		events = append([]session.WorkflowEvent(nil), sess.PlaybackEvents...)
	})

	if !stopRequested {
		t.Fatalf("expected StopRequested to be true")
	}
	if !stepRequested {
		t.Fatalf("expected StepRequested to be true")
	}
	if paused {
		t.Fatalf("expected Paused to be false")
	}
	if len(events) == 0 {
		t.Fatalf("expected playback events to include stop message")
	}
	if got := events[len(events)-1].Message; got != "Playback stop requested" {
		t.Fatalf("expected stop event message, got %q", got)
	}
}

func TestApplyWorkflowFillCoordinates(t *testing.T) {
	mockHost, _ := host.NewMockHost("")
	sess := &session.Session{Host: mockHost}
	app := &App{}

	// Test 1-based to 0-based conversion
	step := session.WorkflowStep{
		Type: "FillString",
		Coordinates: &session.WorkflowCoordinates{
			Row:    24,
			Column: 80,
		},
		Text: "TEST",
	}

	if err := app.applyWorkflowFill(sess, step); err != nil {
		t.Fatalf("applyWorkflowFill failed: %v", err)
	}

	if len(mockHost.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(mockHost.Commands))
	}

	// 24, 80 (1-based) -> 23, 79 (0-based)
	want := "write(23,79,TEST)"
	if mockHost.Commands[0] != want {
		t.Errorf("expected command %q, got %q", want, mockHost.Commands[0])
	}

	// Test validation
	invalidStep := session.WorkflowStep{
		Type: "FillString",
		Coordinates: &session.WorkflowCoordinates{
			Row:    0, // Invalid 1-based
			Column: 1,
		},
		Text: "TEST",
	}
	if err := app.applyWorkflowFill(sess, invalidStep); err == nil {
		t.Error("expected error for row 0, got nil")
	}
}
