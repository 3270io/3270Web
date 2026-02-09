package main

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/session"
)

func (app *App) playWorkflow(s *session.Session, workflow *WorkflowConfig) {
	if s == nil || workflow == nil {
		return
	}

	withSessionLock(s, func() {
		if s.Playback == nil {
			s.Playback = &session.WorkflowPlayback{}
		}
		s.Playback.Active = true
		s.Playback.TotalSteps = len(workflow.Steps)
	})

	defer func() {
		withSessionLock(s, func() {
			if s.Playback == nil {
				return
			}
			s.LastPlaybackStep = s.Playback.CurrentStep
			s.LastPlaybackStepType = s.Playback.CurrentStepType
			s.LastPlaybackStepTotal = s.Playback.TotalSteps
			s.LastPlaybackDelayRange = formatDelayRange(s.Playback.CurrentDelayMin, s.Playback.CurrentDelayMax)
			s.LastPlaybackDelayApplied = formatDelayApplied(s.Playback.CurrentDelayUsed.Seconds())
			s.Playback.Active = false
			s.PlaybackCompletedAt = time.Now()
		})
		addPlaybackEvent(s, "Playback stopped")
	}()

	for i, step := range workflow.Steps {
		if shouldStopPlayback(s) {
			addPlaybackEvent(s, "Playback stop acknowledged")
			return
		}
		if err := waitForDebugPermission(s); err != nil {
			addPlaybackEvent(s, "Playback interrupted")
			return
		}

		delayMin, delayMax := workflowDelayForStep(workflow, step)
		delayUsed := randomDelay(delayMin, delayMax)
		withSessionLock(s, func() {
			if s.Playback == nil {
				return
			}
			s.Playback.CurrentStep = i + 1
			s.Playback.CurrentStepType = step.Type
			s.Playback.CurrentDelayMin = delayMin
			s.Playback.CurrentDelayMax = delayMax
			s.Playback.CurrentDelayUsed = delayUsed
		})

		if delayUsed > 0 {
			if sleepCanceled(s, delayUsed) {
				addPlaybackEvent(s, "Playback stop acknowledged")
				return
			}
		}

		if err := app.applyWorkflowStep(s, step); err != nil {
			addPlaybackEvent(s, fmt.Sprintf("Step %d failed (%s): %v", i+1, step.Type, err))
			return
		}

		addPlaybackEvent(s, fmt.Sprintf("Step %d/%d: %s", i+1, len(workflow.Steps), step.Type))

		withSessionLock(s, func() {
			if s.Playback == nil {
				return
			}
			if s.Playback.Mode == "debug" && s.Playback.Paused {
				s.Playback.StepRequested = false
			}
		})
	}

	addPlaybackEvent(s, "Playback completed")
}

func (app *App) applyWorkflowStep(s *session.Session, step session.WorkflowStep) error {
	if s == nil || s.Host == nil {
		return errors.New("session host is unavailable")
	}
	stepType := strings.TrimSpace(step.Type)
	switch stepType {
	case "", "Connect":
		if s.Host.IsConnected() {
			return nil
		}
		if err := s.Host.Start(); err != nil {
			return err
		}
	case "Disconnect":
		return s.Host.Stop()
	case "FillString":
		if err := app.applyWorkflowFill(s, step); err != nil {
			return err
		}
	default:
		if err := submitWorkflowPendingInput(s); err != nil {
			return err
		}
		key, ok := workflowKeyForStepType(step.Type)
		if !ok {
			return fmt.Errorf("unsupported workflow step type: %s", step.Type)
		}
		if err := s.Host.SendKey(key); err != nil {
			return err
		}
	}

	// Keep host screen cache synchronized so /screen/content reflects playback progress.
	if stepType != "Disconnect" {
		if err := s.Host.UpdateScreen(); err != nil {
			return err
		}
	}
	return nil
}

func (app *App) applyWorkflowFill(s *session.Session, step session.WorkflowStep) error {
	if s == nil || s.Host == nil {
		return errors.New("session host is unavailable")
	}
	if step.Coordinates == nil {
		return errors.New("fill step requires coordinates")
	}
	if step.Coordinates.Row <= 0 || step.Coordinates.Column <= 0 {
		return errors.New("fill coordinates must be 1-based positive values")
	}
	row := step.Coordinates.Row - 1
	col := step.Coordinates.Column - 1

	lines := strings.Split(step.Text, "\n")
	for i, line := range lines {
		if err := s.Host.MoveCursor(row+i, col); err != nil {
			return err
		}
		if err := s.Host.WriteStringAt(row+i, col, line); err != nil {
			return err
		}
	}

	withSessionLock(s, func() {
		if s.Playback != nil {
			s.Playback.PendingInput = true
		}
	})
	return nil
}

func submitWorkflowPendingInput(s *session.Session) error {
	if s == nil || s.Host == nil {
		return nil
	}
	pending := false
	withSessionLock(s, func() {
		pending = s.Playback != nil && s.Playback.PendingInput
	})
	if !pending {
		return nil
	}
	if err := s.Host.SubmitScreen(); err != nil {
		return err
	}
	withSessionLock(s, func() {
		if s.Playback != nil {
			s.Playback.PendingInput = false
		}
	})
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

func workflowDelayForStep(workflow *WorkflowConfig, step session.WorkflowStep) (float64, float64) {
	if step.StepDelay != nil {
		return normalizeDelayRange(step.StepDelay.Min, step.StepDelay.Max)
	}
	if workflow != nil && workflow.EveryStepDelay != nil {
		return normalizeDelayRange(workflow.EveryStepDelay.Min, workflow.EveryStepDelay.Max)
	}
	return 0, 0
}

func normalizeDelayRange(min, max float64) (float64, float64) {
	if min < 0 {
		min = 0
	}
	if max < 0 {
		max = 0
	}
	if max < min {
		max = min
	}
	return min, max
}

func randomDelay(min, max float64) time.Duration {
	if max <= 0 {
		return 0
	}
	seconds := min
	if max > min {
		seconds = min + rand.Float64()*(max-min)
	}
	return time.Duration(seconds * float64(time.Second))
}

func shouldStopPlayback(s *session.Session) bool {
	stop := false
	withSessionLock(s, func() {
		stop = s.Playback == nil || s.Playback.StopRequested
	})
	return stop
}

func waitForDebugPermission(s *session.Session) error {
	for {
		stop := false
		canProceed := true
		withSessionLock(s, func() {
			if s.Playback == nil {
				stop = true
				return
			}
			stop = s.Playback.StopRequested
			if s.Playback.Mode == "debug" && s.Playback.Paused {
				canProceed = s.Playback.StepRequested
			}
		})
		if stop {
			return errors.New("playback stopped")
		}
		if canProceed {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func sleepCanceled(s *session.Session, delay time.Duration) bool {
	if delay <= 0 {
		return false
	}
	deadline := time.Now().Add(delay)
	for {
		if shouldStopPlayback(s) {
			return true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		if remaining > 50*time.Millisecond {
			remaining = 50 * time.Millisecond
		}
		time.Sleep(remaining)
	}
}
