package main

import (
	"errors"
	"time"

	"github.com/jnnngs/3270Web/internal/session"
)

// playWorkflow is a stub implementation as the original logic was lost.
// It logs an error and stops playback immediately.
func (app *App) playWorkflow(s *session.Session, workflow *WorkflowConfig) {
	msg := "Workflow playback is currently disabled/not implemented."
	addPlaybackEvent(s, msg)

	// Ensure playback is marked as stopped
	withSessionLock(s, func() {
		if s.Playback != nil {
			s.Playback.Active = false
			s.PlaybackCompletedAt = time.Now()
		}
	})
	addPlaybackEvent(s, "Playback stopped")
}

func (app *App) applyWorkflowStep(s *session.Session, step session.WorkflowStep) error {
	return errors.New("workflow playback not implemented")
}

func (app *App) applyWorkflowFill(s *session.Session, step session.WorkflowStep) error {
	return errors.New("workflow playback not implemented")
}

func submitWorkflowPendingInput(s *session.Session) error {
	return nil
}

func stopWorkflowPlayback(s *session.Session) {
	withSessionLock(s, func() {
		if s.Playback != nil && s.Playback.Active {
			s.Playback.StopRequested = true
		}
	})
	addPlaybackEvent(s, "Stop requested")
}

func addPlaybackEvent(s *session.Session, message string) {
	withSessionLock(s, func() {
		s.PlaybackEvents = append(s.PlaybackEvents, session.WorkflowEvent{
			Time:    time.Now(),
			Message: message,
		})
	})
}
