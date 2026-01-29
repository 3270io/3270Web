package main

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/session"
)

const (
	playbackPollInterval = 100 * time.Millisecond
	maxPlaybackEvents    = 200
)

func addPlaybackEvent(s *session.Session, message string) {
	if s == nil || strings.TrimSpace(message) == "" {
		return
	}
	withSessionLock(s, func() {
		s.PlaybackEvents = append(s.PlaybackEvents, session.WorkflowEvent{Time: time.Now(), Message: message})
		if len(s.PlaybackEvents) > maxPlaybackEvents {
			s.PlaybackEvents = s.PlaybackEvents[len(s.PlaybackEvents)-maxPlaybackEvents:]
		}
	})
}

func stopWorkflowPlayback(s *session.Session) {
	if s == nil {
		return
	}
	withSessionLock(s, func() {
		if s.Playback == nil {
			return
		}
		s.Playback.StopRequested = true
		s.Playback.Paused = false
		s.Playback.StepRequested = true
	})
	addPlaybackEvent(s, "Playback stop requested")
}

func (app *App) playWorkflow(s *session.Session, workflow *WorkflowConfig) {
	if s == nil || workflow == nil {
		return
	}
	randSource := rand.New(rand.NewSource(time.Now().UnixNano()))
	withSessionLock(s, func() {
		if s.Playback != nil {
			s.Playback.Active = true
			s.Playback.PendingInput = false
			s.Playback.StopRequested = false
			s.Playback.CurrentStep = 0
			s.Playback.CurrentStepType = ""
			s.Playback.TotalSteps = len(workflow.Steps)
			if s.Playback.Mode == "debug" {
				s.Playback.Paused = true
			}
		}
	})

	for i, step := range workflow.Steps {
		if !waitForPlaybackStep(s) {
			finalizeWorkflowPlayback(s, false)
			return
		}
		updatePlaybackStep(s, i+1, step.Type, len(workflow.Steps))
		if err := app.applyWorkflowStep(s, step); err != nil {
			addPlaybackEvent(s, fmt.Sprintf("Step %d failed: %v", i+1, err))
			finalizeWorkflowPlayback(s, false)
			return
		}
		if !applyWorkflowDelay(s, workflow, step, randSource) {
			finalizeWorkflowPlayback(s, false)
			return
		}
	}
	addPlaybackEvent(s, "Playback completed")
	finalizeWorkflowPlayback(s, true)
}

func waitForPlaybackStep(s *session.Session) bool {
	for {
		mode, paused, stepRequested, stopRequested, active := playbackState(s)
		if !active || stopRequested {
			return false
		}
		if mode == "debug" {
			if stepRequested {
				withSessionLock(s, func() {
					if s.Playback != nil {
						s.Playback.StepRequested = false
					}
				})
				return true
			}
			if !paused {
				return true
			}
		} else if !paused {
			return true
		}
		time.Sleep(playbackPollInterval)
	}
}

func playbackState(s *session.Session) (mode string, paused, stepRequested, stopRequested, active bool) {
	if s == nil {
		return "", false, false, true, false
	}
	withSessionLock(s, func() {
		if s.Playback == nil {
			stopRequested = true
			return
		}
		mode = s.Playback.Mode
		paused = s.Playback.Paused
		stepRequested = s.Playback.StepRequested
		stopRequested = s.Playback.StopRequested
		active = s.Playback.Active
	})
	return mode, paused, stepRequested, stopRequested, active
}

func updatePlaybackStep(s *session.Session, index int, stepType string, total int) {
	withSessionLock(s, func() {
		if s.Playback == nil {
			return
		}
		s.Playback.CurrentStep = index
		s.Playback.CurrentStepType = stepType
		s.Playback.TotalSteps = total
	})
}

func applyWorkflowDelay(s *session.Session, workflow *WorkflowConfig, step session.WorkflowStep, randSource *rand.Rand) bool {
	minDelay, maxDelay := workflowDelayRange(workflow, step)
	var delaySeconds float64
	if maxDelay > 0 {
		if maxDelay < minDelay {
			maxDelay = minDelay
		}
		if maxDelay == minDelay {
			delaySeconds = minDelay
		} else {
			delaySeconds = minDelay + randSource.Float64()*(maxDelay-minDelay)
		}
	}
	used := time.Duration(delaySeconds * float64(time.Second))
	withSessionLock(s, func() {
		if s.Playback != nil {
			s.Playback.CurrentDelayMin = minDelay
			s.Playback.CurrentDelayMax = maxDelay
			s.Playback.CurrentDelayUsed = used
		}
	})
	if used <= 0 {
		return !isPlaybackStopRequested(s)
	}
	return sleepWithPlaybackStop(s, used)
}

func workflowDelayRange(workflow *WorkflowConfig, step session.WorkflowStep) (float64, float64) {
	if step.StepDelay != nil {
		return step.StepDelay.Min, step.StepDelay.Max
	}
	if workflow != nil && workflow.EveryStepDelay != nil {
		return workflow.EveryStepDelay.Min, workflow.EveryStepDelay.Max
	}
	return 0, 0
}

func sleepWithPlaybackStop(s *session.Session, delay time.Duration) bool {
	if delay <= 0 {
		return !isPlaybackStopRequested(s)
	}
	deadline := time.Now().Add(delay)
	for time.Now().Before(deadline) {
		if isPlaybackStopRequested(s) {
			return false
		}
		time.Sleep(playbackPollInterval)
	}
	return !isPlaybackStopRequested(s)
}

func isPlaybackStopRequested(s *session.Session) bool {
	if s == nil {
		return true
	}
	stop := false
	withSessionLock(s, func() {
		if s.Playback == nil {
			stop = true
			return
		}
		stop = s.Playback.StopRequested
	})
	return stop
}

func finalizeWorkflowPlayback(s *session.Session, completed bool) {
	if s == nil {
		return
	}
	withSessionLock(s, func() {
		if s.Playback != nil {
			s.LastPlaybackStep = s.Playback.CurrentStep
			s.LastPlaybackStepType = s.Playback.CurrentStepType
			s.LastPlaybackStepTotal = s.Playback.TotalSteps
			s.LastPlaybackDelayRange = formatDelayRange(s.Playback.CurrentDelayMin, s.Playback.CurrentDelayMax)
			s.LastPlaybackDelayApplied = formatDelayApplied(s.Playback.CurrentDelayUsed.Seconds())
		}
		s.Playback = nil
		if completed {
			s.PlaybackCompletedAt = time.Now()
		} else {
			s.PlaybackCompletedAt = time.Time{}
		}
	})
}

func (app *App) applyWorkflowStep(s *session.Session, step session.WorkflowStep) error {
	if s == nil || s.Host == nil {
		return errors.New("missing session host")
	}
	stepType := strings.TrimSpace(step.Type)
	if stepType == "" {
		return errors.New("workflow step missing type")
	}
	switch stepType {
	case "Connect", "Disconnect":
		return nil
	case "FillString":
		return app.applyWorkflowFill(s, step)
	}
	if strings.HasPrefix(stepType, "Press") {
		if err := submitWorkflowPendingInput(s); err != nil {
			return err
		}
		key := strings.TrimPrefix(stepType, "Press")
		if key == "" {
			return errors.New("workflow step missing key")
		}
		normalized := normalizeKey(key)
		return s.Host.SendKey(normalized)
	}
	return fmt.Errorf("unsupported workflow step %q", stepType)
}

func (app *App) applyWorkflowFill(s *session.Session, step session.WorkflowStep) error {
	if s == nil || s.Host == nil {
		return errors.New("missing session host")
	}
	if step.Coordinates == nil {
		return errors.New("workflow step missing coordinates")
	}
	row := step.Coordinates.Row - 1
	col := step.Coordinates.Column - 1
	if row < 0 || col < 0 {
		return fmt.Errorf("invalid workflow coordinates (%d, %d)", step.Coordinates.Row, step.Coordinates.Column)
	}
	if step.Text == "" {
		return nil
	}
	if err := s.Host.WriteStringAt(row, col, step.Text); err != nil {
		return err
	}
	withSessionLock(s, func() {
		if s.Playback != nil {
			s.Playback.PendingInput = false
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
		if s.Playback == nil {
			return
		}
		pending = s.Playback.PendingInput
		s.Playback.PendingInput = false
	})
	if !pending {
		return nil
	}
	screen := s.Host.GetScreen()
	if screen == nil {
		return nil
	}
	if screen.IsFormatted {
		return s.Host.SubmitScreen()
	}
	return s.Host.SubmitUnformatted(screen.Text())
}
