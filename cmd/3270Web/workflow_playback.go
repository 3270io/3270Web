package main

import (
	"log"
	"time"

	"github.com/jnnngs/3270Web/internal/session"
)

func addPlaybackEvent(s *session.Session, message string) {
	if s == nil {
		return
	}
	s.Lock()
	defer s.Unlock()
	s.PlaybackEvents = append(s.PlaybackEvents, session.WorkflowEvent{
		Time:    time.Now(),
		Message: message,
	})
}

func (app *App) playWorkflow(s *session.Session, workflow *WorkflowConfig) {
	log.Println("playWorkflow: not implemented (restored stub)")
}

func stopWorkflowPlayback(s *session.Session) {
	if s == nil {
		return
	}
	s.Lock()
	defer s.Unlock()
	if s.Playback != nil {
		s.Playback.StopRequested = true
	}
	addPlaybackEvent(s, "Stop requested")
}

func (app *App) applyWorkflowStep(s *session.Session, step session.WorkflowStep) error {
	log.Printf("applyWorkflowStep: %s (stub)", step.Type)
	return nil
}

func (app *App) applyWorkflowFill(s *session.Session, step session.WorkflowStep) error {
	log.Printf("applyWorkflowFill: %s (stub)", step.Text)
	return nil
}

func submitWorkflowPendingInput(s *session.Session) error {
	log.Println("submitWorkflowPendingInput: stub")
	return nil
}
