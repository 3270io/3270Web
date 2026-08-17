// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

type unsupportedWorkflowStepError struct {
	StepType string
}

func (e unsupportedWorkflowStepError) Error() string {
	return fmt.Sprintf("unsupported workflow step type: %s", e.StepType)
}

// workflowAbortError ends a run rather than skipping a step. It is raised by
// the decision steps only: everywhere else a failed step is logged and the run
// continues, which is right for a step that acts and wrong for one that
// decides. See workflow_control.go.
type workflowAbortError struct {
	Reason string
}

func (e workflowAbortError) Error() string {
	return e.Reason
}

func (app *App) playWorkflow(s *session.Session, workflow *WorkflowConfig) {
	if s == nil || workflow == nil {
		return
	}

	// A recording is compiled when it is loaded, so a block that does not
	// close has already been refused by then. Compiling again here is for the
	// callers that build a recording in memory and never went through a load,
	// and it costs one pass over the steps.
	program, err := compileWorkflowSteps(workflow.Steps)
	if err != nil {
		withSessionLock(s, func() {
			if s.Playback != nil {
				s.Playback.Active = false
			}
			s.PlaybackCompletedAt = time.Now()
		})
		addPlaybackEvent(s, fmt.Sprintf("Playback refused: %v", err))
		return
	}

	withSessionLock(s, func() {
		if s.Playback == nil {
			s.Playback = &session.WorkflowPlayback{}
		}
		s.Playback.Active = true
		s.Playback.TotalSteps = len(workflow.Steps)
		s.Playback.Variables = nil
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

	// The run is a program counter rather than a range, because a recording
	// that makes decisions does not necessarily visit its steps in order —
	// an If skips a block, an EndWhile goes backwards. A recording with no
	// decisions in it walks 0, 1, 2 … and behaves exactly as it always did.
	total := len(workflow.Steps)
	run := newWorkflowRunState()
	executed := 0

	for pc := 0; pc >= 0 && pc < total; {
		if shouldStopPlayback(s) {
			addPlaybackEvent(s, "Playback stop acknowledged")
			return
		}
		if err := waitForDebugPermission(s); err != nil {
			addPlaybackEvent(s, "Playback interrupted")
			return
		}

		executed++
		if executed > maxWorkflowStepsExecuted {
			addPlaybackEvent(s, fmt.Sprintf("Playback stopped: this recording has run %d steps without finishing, which is further than a flow that is going to finish gets", maxWorkflowStepsExecuted))
			return
		}

		step := workflow.Steps[pc]
		control := isControlFlowStepType(step.Type)

		delayMin, delayMax := workflowDelayForStep(workflow, step)
		if control && step.StepDelay == nil {
			// A decision never reaches the host, so the pacing delay that
			// exists to keep a host comfortable has nothing to pace. A step
			// that names its own delay still gets it.
			delayMin, delayMax = 0, 0
		}
		delayUsed := randomDelay(delayMin, delayMax)
		stepIndex := pc
		withSessionLock(s, func() {
			if s.Playback == nil {
				return
			}
			s.Playback.CurrentStep = stepIndex + 1
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

		if control {
			next, err := app.runWorkflowControlStep(s, program, pc, run)
			if err != nil {
				addPlaybackEvent(s, fmt.Sprintf("Playback stopped at step %d/%d (%s): %v", pc+1, total, canonicalWorkflowStepType(step.Type), err))
				return
			}
			pc = next
			markDebugStepTaken(s)
			continue
		}

		resolved, err := resolveWorkflowStepText(step, run.vars)
		if err == nil {
			err = app.applyWorkflowStep(s, resolved)
		}
		if err != nil {
			var abortErr workflowAbortError
			if errors.As(err, &abortErr) {
				addPlaybackEvent(s, fmt.Sprintf("Playback stopped at step %d/%d (%s): %v", pc+1, total, step.Type, abortErr.Reason))
				return
			}
			var unsupportedErr unsupportedWorkflowStepError
			if errors.As(err, &unsupportedErr) {
				addPlaybackEvent(s, fmt.Sprintf("Step %d/%d skipped unsupported step: %s", pc+1, total, unsupportedErr.StepType))
				pc++
				continue
			}
			addPlaybackEvent(s, fmt.Sprintf("Step %d/%d skipped failed step (%s): %v", pc+1, total, step.Type, err))
			pc++
			continue
		}

		addPlaybackEvent(s, fmt.Sprintf("Step %d/%d: %s", pc+1, total, step.Type))
		markDebugStepTaken(s)
		pc++
	}

	addPlaybackEvent(s, "Playback completed")
}

func markDebugStepTaken(s *session.Session) {
	withSessionLock(s, func() {
		if s.Playback == nil {
			return
		}
		if s.Playback.Mode == "debug" && s.Playback.Paused {
			s.Playback.StepRequested = false
		}
	})
}

// resolveWorkflowStepText fills ${name} into the text an acting step types or
// checks.
//
// A failure here ends the run rather than skipping the step, for the same
// reason a failed decision does: the alternative is typing the characters
// "${balance}" into a field on a live host, which is a wrong value entered
// confidently rather than a step that did not happen.
func resolveWorkflowStepText(step session.WorkflowStep, vars workflowVariables) (session.WorkflowStep, error) {
	if !strings.Contains(step.Text, "${") {
		return step, nil
	}
	switch strings.TrimSpace(step.Type) {
	case "FillString", "CheckValue":
		text, err := substituteWorkflowVariables(step.Text, vars)
		if err != nil {
			return step, workflowAbortError{Reason: err.Error()}
		}
		step.Text = text
	}
	return step, nil
}

// runWorkflowControlStep runs one decision and answers with the step to run
// next. Every error it returns ends the run: see workflow_control.go for why a
// decision that cannot be made is not a step that can be skipped.
func (app *App) runWorkflowControlStep(s *session.Session, program *workflowProgram, pc int, run *workflowRunState) (int, error) {
	step := program.steps[pc]
	stepType := canonicalWorkflowStepType(step.Type)
	total := len(program.steps)

	switch stepType {
	case stepSetVariable:
		value, err := app.workflowVariableValue(s, step, run.vars)
		if err != nil {
			return 0, err
		}
		run.vars[step.Variable] = value
		if step.Sensitive {
			run.sensitive[step.Variable] = true
		}
		publishWorkflowVariables(s, run)
		addPlaybackEvent(s, fmt.Sprintf("Step %d/%d: SetVariable %s = %s", pc+1, total, step.Variable, run.loggable(step.Variable, value)))
		return pc + 1, nil

	case stepIf:
		result, err := app.evaluateWorkflowStepCondition(s, step, run.vars)
		if err != nil {
			return 0, err
		}
		addPlaybackEvent(s, fmt.Sprintf("Step %d/%d: If %s — %s", pc+1, total, describeWorkflowCondition(step), workflowBranchTaken(result)))
		if result {
			return pc + 1, nil
		}
		return workflowJumpTarget(program.ifFalse, pc, stepIf)

	case stepElse:
		// Reached by running off the end of the then-branch, so the rest of
		// this If has already been decided against.
		return workflowJumpTarget(program.elseEnd, pc, stepElse)

	case stepEndIf:
		return pc + 1, nil

	case stepWhile:
		limit := step.MaxIterations
		if limit <= 0 {
			limit = defaultWorkflowLoopLimit
		}
		if run.loops[pc] >= limit {
			return 0, workflowAbortError{Reason: fmt.Sprintf("the loop at step %d is still true after %d passes, so it is not going to end on its own", pc+1, limit)}
		}
		result, err := app.evaluateWorkflowStepCondition(s, step, run.vars)
		if err != nil {
			return 0, err
		}
		if !result {
			// Reset so an enclosing loop that comes back here starts this
			// one's count again rather than inheriting the last pass.
			delete(run.loops, pc)
			addPlaybackEvent(s, fmt.Sprintf("Step %d/%d: While %s — leaving the loop", pc+1, total, describeWorkflowCondition(step)))
			return workflowJumpTarget(program.whileFalse, pc, stepWhile)
		}
		run.loops[pc]++
		addPlaybackEvent(s, fmt.Sprintf("Step %d/%d: While %s — pass %d", pc+1, total, describeWorkflowCondition(step), run.loops[pc]))
		return pc + 1, nil

	case stepEndWhile:
		return workflowJumpTarget(program.whileStart, pc, stepEndWhile)

	case stepStop:
		reason, err := substituteWorkflowVariables(step.Text, run.vars)
		if err != nil {
			return 0, err
		}
		if strings.TrimSpace(reason) == "" {
			reason = "the recording asked to stop here"
		}
		addPlaybackEvent(s, fmt.Sprintf("Step %d/%d: Stop — %s", pc+1, total, reason))
		return total, nil
	}

	return 0, fmt.Errorf("unhandled control step %q", step.Type)
}

// workflowJumpTarget reads a resolved jump. Compilation fills every one of
// these in, so a missing target means the program and the steps have come
// apart — and the wrong answer to that is zero, which would silently restart
// the recording from its first step.
func workflowJumpTarget(targets map[int]int, pc int, stepType string) (int, error) {
	target, ok := targets[pc]
	if !ok {
		return 0, fmt.Errorf("the %s at step %d has no resolved block to jump to", stepType, pc+1)
	}
	return target, nil
}

func workflowBranchTaken(result bool) string {
	if result {
		return "true, taking this branch"
	}
	return "false, taking the other branch"
}

// workflowVariableValue answers with what a SetVariable step should store:
// either a literal with any variables of its own filled in, or the text at a
// place on the screen, trimmed of the spaces a field is padded with.
func (app *App) workflowVariableValue(s *session.Session, step session.WorkflowStep, vars workflowVariables) (string, error) {
	if step.Coordinates == nil {
		return substituteWorkflowVariables(step.Text, vars)
	}
	screen, err := app.workflowScreen(s)
	if err != nil {
		return "", err
	}
	text, err := screenRegionText(screen, step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func (app *App) evaluateWorkflowStepCondition(s *session.Session, step session.WorkflowStep, vars workflowVariables) (bool, error) {
	var screen *host.Screen
	if step.Coordinates != nil {
		var err error
		screen, err = app.workflowScreen(s)
		if err != nil {
			return false, err
		}
	}
	return evaluateWorkflowCondition(step, vars, screen)
}

// workflowScreen reads the screen as it stands now. It refreshes first: a
// condition asked between two host round-trips must be answered against what
// the host has drawn, not against whatever was cached before the last key.
func (app *App) workflowScreen(s *session.Session) (*host.Screen, error) {
	h := app.sessionHost(s)
	if h == nil {
		return nil, errors.New("session host is unavailable")
	}
	if err := h.UpdateScreen(); err != nil {
		return nil, err
	}
	screen := hostScreenSnapshot(h)
	if screen == nil {
		return nil, errors.New("screen is unavailable")
	}
	return screen, nil
}

// publishWorkflowVariables copies what the run knows onto the playback state,
// so /workflow/status can answer "what did it read?" while the run is going.
func publishWorkflowVariables(s *session.Session, run *workflowRunState) {
	snapshot := make(map[string]string, len(run.vars))
	for name, value := range run.vars {
		if run.sensitive[name] {
			// Set, and deliberately not said. The run still has the value;
			// the status endpoint is read by more people than the run is.
			snapshot[name] = "(hidden)"
			continue
		}
		snapshot[name] = value
	}
	withSessionLock(s, func() {
		if s.Playback == nil {
			return
		}
		s.Playback.Variables = snapshot
	})
}

func (app *App) applyWorkflowStep(s *session.Session, step session.WorkflowStep) error {
	if s == nil {
		return errors.New("session host is unavailable")
	}
	h := app.sessionHost(s)
	if h == nil {
		return errors.New("session host is unavailable")
	}
	stepType := strings.TrimSpace(step.Type)
	switch stepType {
	case "", "Connect":
		if h.IsConnected() {
			return nil
		}
		if err := h.Start(); err != nil {
			return err
		}
	case "Disconnect":
		return h.Stop()
	case "FillString":
		if err := app.applyWorkflowFill(s, step); err != nil {
			return err
		}
	case "CheckValue":
		if err := app.applyWorkflowCheckValue(s, step); err != nil {
			return err
		}
	default:
		if err := submitWorkflowPendingInput(s); err != nil {
			return err
		}
		key, ok := workflowKeyForStepType(step.Type)
		if !ok {
			return unsupportedWorkflowStepError{StepType: step.Type}
		}
		if err := h.SendKey(key); err != nil {
			return err
		}
	}

	// Keep host screen cache synchronized so /screen/content reflects playback progress.
	if stepType != "Disconnect" {
		if err := h.UpdateScreen(); err != nil {
			return err
		}
	}
	return nil
}

func (app *App) applyWorkflowFill(s *session.Session, step session.WorkflowStep) error {
	if s == nil {
		return errors.New("session host is unavailable")
	}
	h := app.sessionHost(s)
	if h == nil {
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
		if err := h.MoveCursor(row+i, col); err != nil {
			return err
		}
		if err := h.WriteStringAt(row+i, col, line); err != nil {
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

func (app *App) applyWorkflowCheckValue(s *session.Session, step session.WorkflowStep) error {
	if s == nil {
		return errors.New("session host is unavailable")
	}
	h := app.sessionHost(s)
	if h == nil {
		return errors.New("session host is unavailable")
	}
	if step.Coordinates == nil {
		return errors.New("check value step requires coordinates")
	}
	if step.Coordinates.Row <= 0 || step.Coordinates.Column <= 0 {
		return errors.New("check value coordinates must be 1-based positive values")
	}
	screen := hostScreenSnapshot(h)
	if screen == nil {
		return errors.New("screen is unavailable")
	}
	row := step.Coordinates.Row - 1
	col := step.Coordinates.Column - 1
	if row < 0 || row >= screen.Height || col < 0 || col >= screen.Width {
		return fmt.Errorf("check value coordinates out of bounds: row=%d col=%d", step.Coordinates.Row, step.Coordinates.Column)
	}

	length := step.Coordinates.Length
	if length <= 0 {
		length = len([]rune(step.Text))
	}
	if length < 0 {
		return errors.New("check value length must be non-negative")
	}
	if length == 0 {
		return nil
	}

	endOffset := length - 1
	endRow := row
	endCol := col + endOffset
	if screen.Width > 0 {
		endRow += endCol / screen.Width
		endCol = endCol % screen.Width
	}
	if endRow >= screen.Height {
		return fmt.Errorf("check value range out of bounds: row=%d col=%d length=%d", step.Coordinates.Row, step.Coordinates.Column, length)
	}

	got := screen.Substring(col, row, endCol, endRow)
	want := step.Text
	if got != want {
		return fmt.Errorf("check value mismatch at row=%d col=%d len=%d: got %q want %q", step.Coordinates.Row, step.Coordinates.Column, length, got, want)
	}
	return nil
}

func submitWorkflowPendingInput(s *session.Session) error {
	var h host.Host
	withSessionLock(s, func() { h = s.Host })
	if h == nil {
		return nil
	}
	pending := false
	withSessionLock(s, func() {
		pending = s.Playback != nil && s.Playback.PendingInput
	})
	if !pending {
		return nil
	}
	if err := h.SubmitScreen(); err != nil {
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
