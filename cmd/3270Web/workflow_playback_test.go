// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func TestStopWorkflowPlaybackUpdatesState(t *testing.T) {
	sess := &session.Session{Playback: &session.WorkflowPlayback{Active: true, Paused: true}}

	stopWorkflowPlayback(sess)

	var stopRequested bool
	var events []session.WorkflowEvent
	withSessionLock(sess, func() {
		if sess.Playback != nil {
			stopRequested = sess.Playback.StopRequested
		}
		events = append([]session.WorkflowEvent(nil), sess.PlaybackEvents...)
	})

	if !stopRequested {
		t.Fatalf("expected StopRequested to be true")
	}
	if len(events) == 0 {
		t.Fatalf("expected playback events to include stop message")
	}
	if got := events[len(events)-1].Message; got != "Stop requested" {
		t.Fatalf("expected stop event message, got %q", got)
	}
}

func TestPlayWorkflow_SkipsUnsupportedStepsAndContinues(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost failed: %v", err)
	}
	mockHost.Connected = true
	sess := &session.Session{Host: mockHost}
	app := &App{}

	workflow := &WorkflowConfig{
		Steps: []session.WorkflowStep{
			{Type: "AsciiScreenGrab"},
			{Type: "PressEnter"},
		},
	}

	app.playWorkflow(sess, workflow)

	withSessionLock(sess, func() {
		if sess.Playback != nil && sess.Playback.Active {
			t.Fatalf("playback should be inactive after completion")
		}
	})

	if len(mockHost.Commands) == 0 {
		t.Fatalf("expected playback to continue to supported step")
	}
	foundEnter := false
	for _, cmd := range mockHost.Commands {
		if cmd == "key:Enter" {
			foundEnter = true
			break
		}
	}
	if !foundEnter {
		t.Fatalf("expected PressEnter to execute after unsupported step, commands=%v", mockHost.Commands)
	}

	var sawSkip, sawCompleted bool
	withSessionLock(sess, func() {
		for _, ev := range sess.PlaybackEvents {
			msg := ev.Message
			if strings.Contains(msg, "skipped unsupported step") && strings.Contains(msg, "AsciiScreenGrab") {
				sawSkip = true
			}
			if msg == "Playback completed" {
				sawCompleted = true
			}
		}
	})
	if !sawSkip {
		t.Fatalf("expected skip event for unsupported step")
	}
	if !sawCompleted {
		t.Fatalf("expected playback completion event")
	}
}

func TestApplyWorkflowStep_CheckValue(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost failed: %v", err)
	}
	mockHost.Connected = true
	if err := mockHost.WriteStringAt(0, 28, "3270 Example Application"); err != nil {
		t.Fatalf("WriteStringAt failed: %v", err)
	}
	sess := &session.Session{Host: mockHost}
	app := &App{}

	step := session.WorkflowStep{
		Type: "CheckValue",
		Coordinates: &session.WorkflowCoordinates{
			Row:    1,
			Column: 29,
			Length: 24,
		},
		Text: "3270 Example Application",
	}
	if err := app.applyWorkflowStep(sess, step); err != nil {
		t.Fatalf("applyWorkflowStep(CheckValue) failed: %v", err)
	}
}

func TestApplyWorkflowStep_CheckValueMismatch(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost failed: %v", err)
	}
	mockHost.Connected = true
	if err := mockHost.WriteStringAt(0, 28, "3270 Example Application"); err != nil {
		t.Fatalf("WriteStringAt failed: %v", err)
	}
	sess := &session.Session{Host: mockHost}
	app := &App{}

	step := session.WorkflowStep{
		Type: "CheckValue",
		Coordinates: &session.WorkflowCoordinates{
			Row:    1,
			Column: 29,
			Length: 24,
		},
		Text: "Wrong Header Value Here!!!",
	}
	err = app.applyWorkflowStep(sess, step)
	if err == nil {
		t.Fatalf("expected mismatch error for CheckValue")
	}
	if !strings.Contains(err.Error(), "check value mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlayWorkflow_ContinuesAfterFailedStep(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost failed: %v", err)
	}
	mockHost.Connected = true
	// Write known text so the last CheckValue passes.
	if err := mockHost.WriteStringAt(0, 0, "SCREEN"); err != nil {
		t.Fatalf("WriteStringAt failed: %v", err)
	}
	sess := &session.Session{Host: mockHost}
	app := &App{}

	workflow := &WorkflowConfig{
		Steps: []session.WorkflowStep{
			// This CheckValue will fail (wrong text).
			{
				Type:        "CheckValue",
				Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 6},
				Text:        "NOSUCH",
			},
			// This step should still execute after the failed check.
			{Type: "PressEnter"},
		},
	}

	app.playWorkflow(sess, workflow)

	withSessionLock(sess, func() {
		if sess.Playback != nil && sess.Playback.Active {
			t.Fatalf("playback should be inactive after completion")
		}
	})

	foundEnter := false
	for _, cmd := range mockHost.Commands {
		if cmd == "key:Enter" {
			foundEnter = true
			break
		}
	}
	if !foundEnter {
		t.Fatalf("expected PressEnter to execute after failed CheckValue, commands=%v", mockHost.Commands)
	}

	var sawSkip, sawCompleted bool
	withSessionLock(sess, func() {
		for _, ev := range sess.PlaybackEvents {
			msg := ev.Message
			if strings.Contains(msg, "skipped failed step") && strings.Contains(msg, "CheckValue") {
				sawSkip = true
			}
			if msg == "Playback completed" {
				sawCompleted = true
			}
		}
	})
	if !sawSkip {
		t.Fatalf("expected skip event for failed CheckValue step")
	}
	if !sawCompleted {
		t.Fatalf("expected playback completion event")
	}
}
