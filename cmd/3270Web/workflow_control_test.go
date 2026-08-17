// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func controlStep(stepType string) session.WorkflowStep {
	return session.WorkflowStep{Type: stepType}
}

func regionCondition(stepType, operator, text string) session.WorkflowStep {
	return session.WorkflowStep{
		Type:        stepType,
		Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 8},
		Operator:    operator,
		Text:        text,
	}
}

func TestCompileWorkflowSteps_ResolvesBlocks(t *testing.T) {
	steps := []session.WorkflowStep{
		regionCondition("If", "equals", "APPROVED"),  // 0
		controlStep("PressEnter"),                    // 1
		controlStep("Else"),                          // 2
		controlStep("PressPF3"),                      // 3
		controlStep("EndIf"),                         // 4
		regionCondition("While", "contains", "MORE"), // 5
		controlStep("PressEnter"),                    // 6
		controlStep("EndWhile"),                      // 7
	}

	program, err := compileWorkflowSteps(steps)
	if err != nil {
		t.Fatalf("compileWorkflowSteps failed: %v", err)
	}
	if !program.hasControlFlow {
		t.Fatalf("expected the program to report control flow")
	}
	if got := program.ifFalse[0]; got != 3 {
		t.Fatalf("If should fall to the step after Else (3), got %d", got)
	}
	if got := program.elseEnd[2]; got != 5 {
		t.Fatalf("the then-branch should leave at the step after EndIf (5), got %d", got)
	}
	if got := program.whileFalse[5]; got != 8 {
		t.Fatalf("While should leave to the step after EndWhile (8), got %d", got)
	}
	if got := program.whileStart[7]; got != 5 {
		t.Fatalf("EndWhile should return to its While (5), got %d", got)
	}
}

func TestCompileWorkflowSteps_IfWithoutElse(t *testing.T) {
	steps := []session.WorkflowStep{
		regionCondition("If", "contains", "ERROR"),
		controlStep("PressPF3"),
		controlStep("EndIf"),
		controlStep("PressEnter"),
	}
	program, err := compileWorkflowSteps(steps)
	if err != nil {
		t.Fatalf("compileWorkflowSteps failed: %v", err)
	}
	if got := program.ifFalse[0]; got != 3 {
		t.Fatalf("If with no Else should fall past its EndIf (3), got %d", got)
	}
}

func TestCompileWorkflowSteps_NoControlFlowStaysPlain(t *testing.T) {
	steps := []session.WorkflowStep{
		{Type: "FillString", Coordinates: &session.WorkflowCoordinates{Row: 5, Column: 20}, Text: "USER01"},
		{Type: "PressEnter"},
	}
	program, err := compileWorkflowSteps(steps)
	if err != nil {
		t.Fatalf("compileWorkflowSteps failed: %v", err)
	}
	if program.hasControlFlow {
		t.Fatalf("a recording with no decisions should not report control flow")
	}
}

func TestCompileWorkflowSteps_RejectsMalformedRecordings(t *testing.T) {
	cases := []struct {
		name  string
		steps []session.WorkflowStep
		want  string
	}{
		{
			name:  "Else with no If",
			steps: []session.WorkflowStep{controlStep("Else")},
			want:  "Else without an If",
		},
		{
			name:  "EndIf with no If",
			steps: []session.WorkflowStep{controlStep("EndIf")},
			want:  "EndIf without an If",
		},
		{
			name:  "EndWhile with no While",
			steps: []session.WorkflowStep{controlStep("EndWhile")},
			want:  "EndWhile without a While",
		},
		{
			name:  "If never closed",
			steps: []session.WorkflowStep{regionCondition("If", "equals", "OK"), controlStep("PressEnter")},
			want:  "never closed",
		},
		{
			name: "While closed by EndIf",
			steps: []session.WorkflowStep{
				regionCondition("While", "contains", "MORE"),
				controlStep("EndIf"),
			},
			want: "EndIf without an If",
		},
		{
			name: "two Elses",
			steps: []session.WorkflowStep{
				regionCondition("If", "equals", "OK"),
				controlStep("Else"),
				controlStep("Else"),
				controlStep("EndIf"),
			},
			want: "a second Else",
		},
		{
			name: "comparison nobody implements",
			steps: []session.WorkflowStep{
				regionCondition("If", "soundsLike", "OK"),
				controlStep("EndIf"),
			},
			want: "no comparison this build understands",
		},
		{
			name: "condition compares nothing",
			steps: []session.WorkflowStep{
				{Type: "If", Operator: "equals", Text: "OK"},
				controlStep("EndIf"),
			},
			want: "compares nothing",
		},
		{
			name: "condition compares two things",
			steps: []session.WorkflowStep{
				{
					Type:        "If",
					Variable:    "status",
					Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 4},
					Operator:    "equals",
					Text:        "OK",
				},
				controlStep("EndIf"),
			},
			want: "only one of them",
		},
		{
			name: "screen region with no length",
			steps: []session.WorkflowStep{
				{
					Type:        "If",
					Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1},
					Operator:    "equals",
					Text:        "OK",
				},
				controlStep("EndIf"),
			},
			want: "needs a Length",
		},
		{
			name: "variable name that is not one",
			steps: []session.WorkflowStep{
				{Type: "SetVariable", Variable: "2fast", Text: "x"},
			},
			want: "is not a letter followed by",
		},
		{
			name: "SetVariable with no name",
			steps: []session.WorkflowStep{
				{Type: "SetVariable", Text: "x"},
			},
			want: "needs a variable name",
		},
		{
			name: "a loop that asks for too many passes",
			steps: []session.WorkflowStep{
				func() session.WorkflowStep {
					step := regionCondition("While", "contains", "MORE")
					step.MaxIterations = maxWorkflowLoopLimit + 1
					return step
				}(),
				controlStep("EndWhile"),
			},
			want: "may repeat at most",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compileWorkflowSteps(tc.steps)
			if err == nil {
				t.Fatalf("expected %s to be refused", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to mention %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestParseWorkflowPayload_RefusesUnclosedBlock(t *testing.T) {
	payload, err := json.Marshal(WorkflowConfig{
		Host: "example",
		Port: 3270,
		Steps: []session.WorkflowStep{
			regionCondition("If", "equals", "OK"),
			{Type: "PressEnter"},
		},
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if _, err := parseWorkflowPayload(payload); err == nil {
		t.Fatalf("expected an unclosed If to be refused at load")
	} else if !strings.Contains(err.Error(), "never closed") {
		t.Fatalf("expected the reason to name the unclosed block, got %q", err.Error())
	}
}

func TestParseWorkflowPayload_AcceptsBalancedBlocks(t *testing.T) {
	payload, err := json.Marshal(WorkflowConfig{
		Host: "example",
		Port: 3270,
		Steps: []session.WorkflowStep{
			regionCondition("If", "equals", "OK"),
			{Type: "PressEnter"},
			{Type: "EndIf"},
		},
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if _, err := parseWorkflowPayload(payload); err != nil {
		t.Fatalf("expected a balanced recording to load, got %v", err)
	}
}

func TestSubstituteWorkflowVariables(t *testing.T) {
	vars := workflowVariables{"account": "12345", "empty": ""}
	cases := []struct {
		name    string
		text    string
		want    string
		wantErr string
	}{
		{name: "no variables", text: "USER01", want: "USER01"},
		{name: "one variable", text: "${account}", want: "12345"},
		{name: "surrounded", text: "A/${account}/B", want: "A/12345/B"},
		{name: "empty value is a value", text: "[${empty}]", want: "[]"},
		{name: "a literal dollar", text: "COST $5", want: "COST $5"},
		{name: "an escaped opener", text: "$${account}", want: "${account}"},
		{name: "unknown name", text: "${balance}", wantErr: "no variable named"},
		{name: "no closing brace", text: "${account", wantErr: "no closing brace"},
		{name: "not a name", text: "${2fast}", wantErr: "is not a letter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := substituteWorkflowVariables(tc.text, vars)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error mentioning %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error to mention %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("substituteWorkflowVariables failed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestParseWorkflowNumber(t *testing.T) {
	cases := []struct {
		text string
		want float64
		ok   bool
	}{
		{text: "42", want: 42, ok: true},
		{text: " 1,234.56 ", want: 1234.56, ok: true},
		{text: "-7", want: -7, ok: true},
		{text: "45.00-", want: -45, ok: true},
		{text: "(12)", want: -12, ok: true},
		{text: "1,000CR", want: -1000, ok: true},
		{text: "", ok: false},
		{text: "APPROVED", ok: false},
		{text: "12 34", ok: false},
	}
	for _, tc := range cases {
		got, ok := parseWorkflowNumber(tc.text)
		if ok != tc.ok {
			t.Fatalf("parseWorkflowNumber(%q) ok=%v want %v", tc.text, ok, tc.ok)
		}
		if ok && got != tc.want {
			t.Fatalf("parseWorkflowNumber(%q) = %v want %v", tc.text, got, tc.want)
		}
	}
}

// screenWith paints one line of text at row 1 and hands back the screen a
// condition would read.
func screenWith(t *testing.T, text string) *host.Screen {
	t.Helper()
	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost failed: %v", err)
	}
	if err := mock.WriteStringAt(0, 0, text); err != nil {
		t.Fatalf("WriteStringAt failed: %v", err)
	}
	return mock.GetScreenSnapshot()
}

func TestEvaluateWorkflowCondition_ScreenRegion(t *testing.T) {
	screen := screenWith(t, "BALANCE   1,240.55 APPROVED")
	cases := []struct {
		name     string
		step     session.WorkflowStep
		want     bool
		wantErr  string
		wantSkip bool
	}{
		{
			name: "equals a trimmed field",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 10},
				Operator:    "equals",
				Text:        "BALANCE",
			},
			want: true,
		},
		{
			name: "contains",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 27},
				Operator:    "contains",
				Text:        "APPROVED",
			},
			want: true,
		},
		{
			name: "case folded on request",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 20, Length: 8},
				Operator:    "equals",
				Text:        "approved",
				IgnoreCase:  true,
			},
			want: true,
		},
		{
			name: "case matters by default",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 20, Length: 8},
				Operator:    "equals",
				Text:        "approved",
			},
			want: false,
		},
		{
			name: "a grouped number is a number",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 11, Length: 8},
				Operator:    "greaterThan",
				Text:        "1000",
			},
			want: true,
		},
		{
			name: "below the limit",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 11, Length: 8},
				Operator:    "lessOrEqual",
				Text:        "1000",
			},
			want: false,
		},
		{
			name: "an untouched region is empty",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 2, Column: 1, Length: 20},
				Operator:    "isEmpty",
			},
			want: true,
		},
		{
			name: "text is not a number",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 20, Length: 8},
				Operator:    "greaterThan",
				Text:        "10",
			},
			wantErr: "is not one",
		},
		{
			name: "off the screen",
			step: session.WorkflowStep{
				Coordinates: &session.WorkflowCoordinates{Row: 99, Column: 1, Length: 4},
				Operator:    "equals",
				Text:        "X",
			},
			wantErr: "out of bounds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evaluateWorkflowCondition(tc.step, workflowVariables{}, screen)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error mentioning %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error to mention %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("evaluateWorkflowCondition failed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("condition = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateWorkflowCondition_Variable(t *testing.T) {
	vars := workflowVariables{"status": "OPEN", "balance": "250.00"}

	got, err := evaluateWorkflowCondition(session.WorkflowStep{
		Variable: "status",
		Operator: "notEquals",
		Text:     "CLOSED",
	}, vars, nil)
	if err != nil {
		t.Fatalf("evaluateWorkflowCondition failed: %v", err)
	}
	if !got {
		t.Fatalf("OPEN != CLOSED should be true")
	}

	got, err = evaluateWorkflowCondition(session.WorkflowStep{
		Variable: "balance",
		Operator: "<",
		Text:     "300",
	}, vars, nil)
	if err != nil {
		t.Fatalf("evaluateWorkflowCondition failed: %v", err)
	}
	if !got {
		t.Fatalf("250.00 < 300 should be true")
	}

	if _, err := evaluateWorkflowCondition(session.WorkflowStep{
		Variable: "limit",
		Operator: "equals",
		Text:     "1",
	}, vars, nil); err == nil {
		t.Fatalf("expected a condition on an unset variable to fail rather than guess")
	}
}

// playControlWorkflow runs a recording against a mock host and hands back the
// host and the events the run produced.
func playControlWorkflow(t *testing.T, screenText string, steps []session.WorkflowStep) (*host.MockHost, []string) {
	t.Helper()
	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost failed: %v", err)
	}
	mock.Connected = true
	if screenText != "" {
		if err := mock.WriteStringAt(0, 0, screenText); err != nil {
			t.Fatalf("WriteStringAt failed: %v", err)
		}
	}
	sess := &session.Session{Host: mock}
	app := &App{}
	app.playWorkflow(sess, &WorkflowConfig{Steps: steps})

	var events []string
	withSessionLock(sess, func() {
		for _, ev := range sess.PlaybackEvents {
			events = append(events, ev.Message)
		}
	})
	return mock, events
}

func hostKeys(mock *host.MockHost) []string {
	var keys []string
	for _, cmd := range mock.Commands {
		if strings.HasPrefix(cmd, "key:") {
			keys = append(keys, strings.TrimPrefix(cmd, "key:"))
		}
	}
	return keys
}

func eventsMentioning(events []string, want string) bool {
	for _, ev := range events {
		if strings.Contains(ev, want) {
			return true
		}
	}
	return false
}

func TestPlayWorkflow_IfTakesOnlyTheBranchThatMatches(t *testing.T) {
	steps := []session.WorkflowStep{
		{
			Type:        "If",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 8},
			Operator:    "equals",
			Text:        "APPROVED",
		},
		{Type: "PressEnter"},
		{Type: "Else"},
		{Type: "PressPF3"},
		{Type: "EndIf"},
		{Type: "PressPF12"},
	}

	mock, events := playControlWorkflow(t, "APPROVED", steps)
	keys := hostKeys(mock)
	want := []string{"Enter", "PF(12)"}
	if len(keys) != len(want) {
		t.Fatalf("expected the then-branch and the step after EndIf, got %v", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys = %v, want %v", keys, want)
		}
	}
	if !eventsMentioning(events, "true, taking this branch") {
		t.Fatalf("expected the run log to say which way the decision went: %v", events)
	}

	mock, events = playControlWorkflow(t, "DECLINED", steps)
	keys = hostKeys(mock)
	want = []string{"PF(3)", "PF(12)"}
	if len(keys) != len(want) {
		t.Fatalf("expected the else-branch and the step after EndIf, got %v", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys = %v, want %v", keys, want)
		}
	}
	if !eventsMentioning(events, "false, taking the other branch") {
		t.Fatalf("expected the run log to say which way the decision went: %v", events)
	}
}

func TestPlayWorkflow_SetVariableFromScreenFillsALaterField(t *testing.T) {
	steps := []session.WorkflowStep{
		{
			Type:        "SetVariable",
			Variable:    "account",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 12},
		},
		{
			Type:        "FillString",
			Coordinates: &session.WorkflowCoordinates{Row: 5, Column: 10},
			Text:        "ACCT ${account}",
		},
		{Type: "PressEnter"},
	}

	mock, events := playControlWorkflow(t, "  4455667788  ", steps)
	screen := mock.GetScreenSnapshot()
	got, err := screenRegionText(screen, 5, 10, 15)
	if err != nil {
		t.Fatalf("screenRegionText failed: %v", err)
	}
	if got != "ACCT 4455667788" {
		t.Fatalf("expected the field to be filled from the variable, got %q", got)
	}
	if !eventsMentioning(events, `SetVariable account = "4455667788"`) {
		t.Fatalf("expected the run log to carry what was read: %v", events)
	}
	if !eventsMentioning(events, "Playback completed") {
		t.Fatalf("expected the run to complete: %v", events)
	}
}

func TestPlayWorkflow_WhileRepeatsUntilTheScreenChanges(t *testing.T) {
	steps := []session.WorkflowStep{
		{
			Type:        "While",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 4},
			Operator:    "equals",
			Text:        "MORE",
		},
		{Type: "PressEnter"},
		{
			// Stands in for the host redrawing: the loop's own condition
			// stops being true once this has run.
			Type:        "FillString",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1},
			Text:        "DONE",
		},
		{Type: "EndWhile"},
		{Type: "PressPF3"},
	}

	mock, events := playControlWorkflow(t, "MORE", steps)
	keys := hostKeys(mock)
	if len(keys) != 2 || keys[0] != "Enter" || keys[1] != "PF(3)" {
		t.Fatalf("expected one pass through the loop then the step after it, got %v", keys)
	}
	if !eventsMentioning(events, "pass 1") {
		t.Fatalf("expected the run log to count the pass: %v", events)
	}
	if !eventsMentioning(events, "leaving the loop") {
		t.Fatalf("expected the run log to say the loop ended: %v", events)
	}
}

func TestPlayWorkflow_WhileStopsAtItsLimit(t *testing.T) {
	steps := []session.WorkflowStep{
		{
			Type:          "While",
			Coordinates:   &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 4},
			Operator:      "equals",
			Text:          "MORE",
			MaxIterations: 3,
		},
		{Type: "PressEnter"},
		{Type: "EndWhile"},
		{Type: "PressPF3"},
	}

	mock, events := playControlWorkflow(t, "MORE", steps)
	keys := hostKeys(mock)
	if len(keys) != 3 {
		t.Fatalf("expected the loop to stop after 3 passes, got %v", keys)
	}
	for _, key := range keys {
		if key != "Enter" {
			t.Fatalf("expected the step after the loop never to run, got %v", keys)
		}
	}
	if !eventsMentioning(events, "not going to end on its own") {
		t.Fatalf("expected the run log to say why it stopped: %v", events)
	}
	if eventsMentioning(events, "Playback completed") {
		t.Fatalf("a run stopped at a loop limit has not completed: %v", events)
	}
}

func TestPlayWorkflow_StopEndsTheRunWithAReason(t *testing.T) {
	steps := []session.WorkflowStep{
		{
			Type:        "If",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 9},
			Operator:    "contains",
			Text:        "NOT FOUND",
		},
		{Type: "Stop", Text: "the host has no record of that account"},
		{Type: "EndIf"},
		{Type: "PressEnter"},
	}

	mock, events := playControlWorkflow(t, "NOT FOUND", steps)
	if keys := hostKeys(mock); len(keys) != 0 {
		t.Fatalf("expected nothing after Stop to run, got %v", keys)
	}
	if !eventsMentioning(events, "the host has no record of that account") {
		t.Fatalf("expected the reason in the run log: %v", events)
	}
	if !eventsMentioning(events, "Playback completed") {
		t.Fatalf("Stop is an ending, not a failure: %v", events)
	}
}

func TestPlayWorkflow_StopsRatherThanTypeAnUnknownVariable(t *testing.T) {
	steps := []session.WorkflowStep{
		{
			Type:        "FillString",
			Coordinates: &session.WorkflowCoordinates{Row: 5, Column: 10},
			Text:        "${balance}",
		},
		{Type: "PressEnter"},
	}

	mock, events := playControlWorkflow(t, "", steps)
	if keys := hostKeys(mock); len(keys) != 0 {
		t.Fatalf("expected the run to stop before pressing anything, got %v", keys)
	}
	screen := mock.GetScreenSnapshot()
	got, err := screenRegionText(screen, 5, 10, 10)
	if err != nil {
		t.Fatalf("screenRegionText failed: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("expected nothing to be typed into the field, got %q", got)
	}
	if !eventsMentioning(events, "no variable named") {
		t.Fatalf("expected the run log to name the missing variable: %v", events)
	}
}

func TestPlayWorkflow_RefusesARecordingWhoseBlocksDoNotClose(t *testing.T) {
	steps := []session.WorkflowStep{
		{
			Type:        "If",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 4},
			Operator:    "equals",
			Text:        "MORE",
		},
		{Type: "PressEnter"},
	}

	mock, events := playControlWorkflow(t, "MORE", steps)
	if keys := hostKeys(mock); len(keys) != 0 {
		t.Fatalf("expected nothing to run, got %v", keys)
	}
	if !eventsMentioning(events, "Playback refused") {
		t.Fatalf("expected the run to be refused before it started: %v", events)
	}
}

func TestPlayWorkflow_ExposesVariablesOnTheStatus(t *testing.T) {
	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost failed: %v", err)
	}
	mock.Connected = true
	if err := mock.WriteStringAt(0, 0, "BALANCE 1,240.55"); err != nil {
		t.Fatalf("WriteStringAt failed: %v", err)
	}
	sess := &session.Session{Host: mock}
	app := &App{}
	app.playWorkflow(sess, &WorkflowConfig{Steps: []session.WorkflowStep{
		{
			Type:        "SetVariable",
			Variable:    "balance",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 9, Length: 8},
		},
	}})

	vars := playbackVariablesSnapshot(sess)
	if got := vars["balance"]; got != "1,240.55" {
		t.Fatalf("expected the status to carry what the run read, got %q", got)
	}
}

func TestPlayWorkflow_SensitiveVariableIsUsedButNotWrittenDown(t *testing.T) {
	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost failed: %v", err)
	}
	mock.Connected = true
	if err := mock.WriteStringAt(0, 0, "HUNTER2  "); err != nil {
		t.Fatalf("WriteStringAt failed: %v", err)
	}
	sess := &session.Session{Host: mock}
	app := &App{}
	app.playWorkflow(sess, &WorkflowConfig{Steps: []session.WorkflowStep{
		{
			Type:        "SetVariable",
			Variable:    "password",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 1, Length: 7},
			Sensitive:   true,
		},
		{
			Type:        "FillString",
			Coordinates: &session.WorkflowCoordinates{Row: 6, Column: 1},
			Text:        "${password}",
		},
	}})

	// The value still does its job.
	screen := mock.GetScreenSnapshot()
	got, err := screenRegionText(screen, 6, 1, 7)
	if err != nil {
		t.Fatalf("screenRegionText failed: %v", err)
	}
	if got != "HUNTER2" {
		t.Fatalf("expected the field to be filled from the variable, got %q", got)
	}

	// And appears in neither of the two places a run is read from.
	var events []string
	withSessionLock(sess, func() {
		for _, ev := range sess.PlaybackEvents {
			events = append(events, ev.Message)
		}
	})
	log := strings.Join(events, "\n")
	if strings.Contains(log, "HUNTER2") {
		t.Fatalf("a sensitive value reached the run log:\n%s", log)
	}
	if !strings.Contains(log, "(hidden)") {
		t.Fatalf("expected the run log to say the variable was set:\n%s", log)
	}
	if got := playbackVariablesSnapshot(sess)["password"]; got != "(hidden)" {
		t.Fatalf("expected the status to hide the value, got %q", got)
	}
}
