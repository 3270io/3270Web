// SPDX-License-Identifier: AGPL-3.0-or-later

package task

import (
	"encoding/json"
	"strings"
	"testing"
)

func pad80(line string) string {
	if len(line) >= 80 {
		return line[:80]
	}
	return line + strings.Repeat(" ", 80-len(line))
}

func draftScreen(lines ...string) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, pad80(l))
	}
	return strings.Join(out, "\n")
}

// The entry screen of a name-and-balance style panel. Column positions here
// are the ones the assertions use, so they are written out deliberately.
func entryScreen() string {
	return draftScreen(
		"                            ACCOUNT ENQUIRY",
		"",
		" Enter the account details below.",
		"",
		" Account number  . . .",
		" Branch code . . . . .",
	)
}

func resultScreen() string {
	return draftScreen(
		"                            ACCOUNT DETAIL",
		"",
		" Name             J MARGOLIS",
		"",
		" Cleared balance   1,240.55 CR",
	)
}

func TestDraftFromRecordingBuildsStepsAndParameters(t *testing.T) {
	steps := []RecordedStep{
		{Type: "Connect"},
		{Type: "FillString", Row: 5, Column: 25, Length: 8, Text: "40218855"},
		{Type: "FillString", Row: 6, Column: 25, Length: 4, Text: "0012"},
		{Type: "PressEnter"},
		{Type: "Disconnect"},
	}
	screens := []RecordedScreen{
		{StepIndex: 1, Text: entryScreen()},
		{StepIndex: 4, Text: resultScreen()},
	}

	draft, err := DraftFromRecording("Account enquiry", steps, screens, resultScreen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Connect and Disconnect are workflow scaffolding, not task steps.
	if len(draft.Task.Steps) != 1 {
		t.Fatalf("expected one step, got %d: %+v", len(draft.Task.Steps), draft.Task.Steps)
	}
	step := draft.Task.Steps[0]
	if step.AIDKey != "Enter" {
		t.Errorf("AIDKey = %q", step.AIDKey)
	}
	if len(step.Inputs) != 2 {
		t.Fatalf("expected two inputs, got %+v", step.Inputs)
	}

	// Parameter names and labels come from the text to each field's left.
	if len(draft.Task.Parameters) != 2 {
		t.Fatalf("expected two parameters, got %+v", draft.Task.Parameters)
	}
	if draft.Task.Parameters[0].Name != "account_number" {
		t.Errorf("first parameter name = %q, want account_number", draft.Task.Parameters[0].Name)
	}
	if draft.Task.Parameters[0].Label != "Account number" {
		t.Errorf("first parameter label = %q", draft.Task.Parameters[0].Label)
	}
	if draft.Task.Parameters[1].Name != "branch_code" {
		t.Errorf("second parameter name = %q, want branch_code", draft.Task.Parameters[1].Name)
	}
	// The recorded value becomes the example, not a default — a default would
	// quietly reuse one account number for everyone.
	if draft.Task.Parameters[0].Example != "40218855" {
		t.Errorf("example = %q", draft.Task.Parameters[0].Example)
	}
	if draft.Task.Parameters[0].Default != "" {
		t.Error("a recorded value must not become a default")
	}

	// The guard should be the panel title.
	if len(step.Expect) != 1 {
		t.Fatalf("expected one guard, got %+v", step.Expect)
	}
	if !strings.Contains(step.Expect[0].Text, "ACCOUNT ENQUIRY") {
		t.Errorf("guard text = %q, want the panel title", step.Expect[0].Text)
	}
	if step.Expect[0].Row != 1 {
		t.Errorf("guard row = %d, want 1", step.Expect[0].Row)
	}

	// The draft must be valid, or the wizard would offer something unsavable.
	if err := draft.Task.Validate(); err != nil {
		t.Errorf("the draft does not validate: %v", err)
	}
	if draft.Task.Source != "recording" {
		t.Errorf("Source = %q", draft.Task.Source)
	}
}

// A guard is only useful if it points at the row and column the text is
// really on — an off-by-one makes every run fail.
func TestDraftGuardPositionIsExact(t *testing.T) {
	screen := entryScreen()
	anchor, ok := deriveAnchor(screen)
	if !ok {
		t.Fatal("expected an anchor")
	}
	line := strings.Split(screen, "\n")[anchor.Row-1]
	got := line[anchor.Column-1 : anchor.Column-1+len(anchor.Text)]
	if got != anchor.Text {
		t.Errorf("anchor points at %q but claims %q", got, anchor.Text)
	}
}

func TestDraftProposesNoOutputsAndSaysSo(t *testing.T) {
	draft, err := DraftFromRecording("X", []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Text: "1"},
		{Type: "PressEnter"},
	}, []RecordedScreen{{StepIndex: 0, Text: entryScreen()}, {StepIndex: 2, Text: resultScreen()}}, resultScreen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Task.Outputs) != 0 {
		t.Error("a draft must not invent outputs")
	}
	if !noteMatching(draft.Notes, "which part of the final screen is the answer") {
		t.Errorf("expected a note explaining why, got %v", draft.Notes)
	}
	// The final screen must be handed over so outputs can be picked from it.
	if !strings.Contains(draft.FinalScreen, "Cleared balance") {
		t.Error("the draft should carry the final screen")
	}
}

func TestDraftNotesEverySuggestion(t *testing.T) {
	draft, err := DraftFromRecording("X", []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Text: "40218855"},
		{Type: "PressEnter"},
	}, []RecordedScreen{{StepIndex: 0, Text: entryScreen()}}, resultScreen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !noteMatching(draft.Notes, "became inputs the operator is asked for") {
		t.Errorf("expected a note about values becoming inputs, got %v", draft.Notes)
	}
}

// A step with no captured screen has no guard, and that must be stated
// rather than left for someone to discover when the task acts on the wrong
// screen.
func TestDraftSaysWhenAStepHasNoGuard(t *testing.T) {
	draft, err := DraftFromRecording("X", []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Text: "1"},
		{Type: "PressEnter"},
	}, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Task.Steps[0].Expect) != 0 {
		t.Error("no screen means no guard can be derived")
	}
	if !noteMatching(draft.Notes, "no screen was captured") {
		t.Errorf("expected a note about the missing guard, got %v", draft.Notes)
	}
}

func TestDraftHandlesUnlabelledFields(t *testing.T) {
	bare := draftScreen("                            ACCOUNT ENQUIRY", "", "", "", "")
	draft, err := DraftFromRecording("X", []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Text: "1"},
		{Type: "PressEnter"},
	}, []RecordedScreen{{StepIndex: 0, Text: bare}}, resultScreen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := draft.Task.Parameters[0]
	if p.Name != "field_r5c25" {
		t.Errorf("name = %q, want a positional fallback", p.Name)
	}
	if !noteMatching(draft.Notes, "No label was found") {
		t.Errorf("expected a note about the missing label, got %v", draft.Notes)
	}
	if err := draft.Task.Validate(); err != nil {
		t.Errorf("the fallback name must still validate: %v", err)
	}
}

// Two fields whose labels slugify the same must not collide, or the second
// would silently overwrite the first.
func TestDraftDisambiguatesRepeatedLabels(t *testing.T) {
	screen := draftScreen(
		"                            TRANSFER",
		"",
		"",
		"",
		" Amount  . . . . . . .",
		" Amount  . . . . . . .",
	)
	draft, err := DraftFromRecording("X", []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Text: "10"},
		{Type: "FillString", Row: 6, Column: 25, Text: "20"},
		{Type: "PressEnter"},
	}, []RecordedScreen{{StepIndex: 0, Text: screen}}, resultScreen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := []string{draft.Task.Parameters[0].Name, draft.Task.Parameters[1].Name}
	if names[0] == names[1] {
		t.Fatalf("both parameters are called %q", names[0])
	}
	if err := draft.Task.Validate(); err != nil {
		t.Errorf("draft does not validate: %v", err)
	}
}

func TestDraftMapsFunctionKeys(t *testing.T) {
	draft, err := DraftFromRecording("X", []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Text: "1"},
		{Type: "PressPF3"},
	}, []RecordedScreen{{StepIndex: 0, Text: entryScreen()}}, resultScreen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if draft.Task.Steps[0].AIDKey != "PF3" {
		t.Errorf("AIDKey = %q, want PF3", draft.Task.Steps[0].AIDKey)
	}
}

func TestDraftRejectsARecordingWithNothingUsable(t *testing.T) {
	if _, err := DraftFromRecording("X", []RecordedStep{{Type: "Connect"}, {Type: "Disconnect"}}, nil, ""); err == nil {
		t.Fatal("expected an error for a recording with no key presses")
	}
}

func TestDraftReportsClearedFieldsRatherThanGuessing(t *testing.T) {
	draft, err := DraftFromRecording("X", []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Text: ""},
		{Type: "FillString", Row: 6, Column: 25, Text: "0012"},
		{Type: "PressEnter"},
	}, []RecordedScreen{{StepIndex: 0, Text: entryScreen()}}, resultScreen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Task.Steps[0].Inputs) != 1 {
		t.Errorf("a cleared field must not become an input: %+v", draft.Task.Steps[0].Inputs)
	}
	if !noteMatching(draft.Notes, "was cleared during recording") {
		t.Errorf("expected a note about the cleared field, got %v", draft.Notes)
	}
}

func TestLabelLeftOfKeepsTheNearestPhrase(t *testing.T) {
	line := draftScreen(" Surname . . . .        Given name . . . .")
	// The field after "Given name" must not inherit "Surname" as well.
	label, ok := labelLeftOf(line, 1, 42)
	if !ok {
		t.Fatal("expected a label")
	}
	if label != "Given name" {
		t.Errorf("label = %q, want %q", label, "Given name")
	}
}

func noteMatching(notes []string, substr string) bool {
	for _, n := range notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}

// A panel title starting with a digit is ordinary on a 3270 — "3270 Example
// Application", or a bare transaction code. An earlier pattern required a
// leading letter and clipped those to their first alphabetic word, throwing
// away the most identifying part of the anchor.
func TestDraftAnchorKeepsALeadingNumber(t *testing.T) {
	screen := draftScreen("                            3270 Example Application", "", "", "", " Name . . . .")
	anchor, ok := deriveAnchor(screen)
	if !ok {
		t.Fatal("expected an anchor")
	}
	if anchor.Text != "3270 Example Application" {
		t.Errorf("anchor = %q, want the whole title", anchor.Text)
	}
	line := strings.Split(screen, "\n")[anchor.Row-1]
	if got := line[anchor.Column-1 : anchor.Column-1+len(anchor.Text)]; got != anchor.Text {
		t.Errorf("anchor points at %q but claims %q", got, anchor.Text)
	}
}

// A row of digits is a date or a figure, not a panel title.
func TestDraftAnchorSkipsRowsWithNoLetters(t *testing.T) {
	screen := draftScreen("  2026-08-08   14:32:01", "  ACCOUNT ENQUIRY", "", "", " Name . . . .")
	anchor, ok := deriveAnchor(screen)
	if !ok {
		t.Fatal("expected an anchor")
	}
	if anchor.Row != 2 || !strings.Contains(anchor.Text, "ACCOUNT ENQUIRY") {
		t.Errorf("anchor = %+v, want the title on row 2 rather than the timestamp", anchor)
	}
}

// The recorder captures each screen BEFORE its submit, so the screen holding
// the answer is never in the recording. The caller has to supply it, and a
// draft built without it must say the screen it is showing is the wrong one.
func TestDraftUsesTheCallerSuppliedFinalScreen(t *testing.T) {
	steps := []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Text: "40218855"},
		{Type: "PressEnter"},
	}
	screens := []RecordedScreen{{StepIndex: 0, Text: entryScreen()}}

	withFinal, err := DraftFromRecording("X", steps, screens, resultScreen())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(withFinal.FinalScreen, "Cleared balance") {
		t.Error("the supplied final screen should be the one offered for outputs")
	}
	if noteMatching(withFinal.Notes, "was not available") {
		t.Error("no warning is warranted when the final screen was supplied")
	}

	without, err := DraftFromRecording("X", steps, screens, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Falling back to the pre-submit screen is a real limitation, so it is
	// stated rather than passed off as the answer screen.
	if !strings.Contains(without.FinalScreen, "Account number") {
		t.Error("expected the fallback to be the last recorded screen")
	}
	if !noteMatching(without.Notes, "was not available") {
		t.Errorf("expected a note about the fallback, got %v", without.Notes)
	}
}

// A recorded sign-on carries the password that was typed. Storing it as the
// parameter's example would write a plain-text password into the catalogue
// file and show it as the placeholder on the form every operator sees.
func TestDraftTreatsAPasswordFieldAsASecretAndKeepsTheValue(t *testing.T) {
	signon := draftScreen(
		"                            SIGN ON",
		"",
		" Userid . . . . . . .",
		" Password . . . . . .",
	)
	steps := []RecordedStep{
		{Type: "FillString", Row: 3, Column: 25, Length: 8, Text: "GHOPPER"},
		{Type: "FillString", Row: 4, Column: 25, Length: 8, Text: "hunter2"},
		{Type: "PressEnter"},
	}
	draft, err := DraftFromRecording("Sign on", steps, []RecordedScreen{{StepIndex: 0, Text: signon}}, resultScreen())
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Task.Parameters) != 2 {
		t.Fatalf("parameters = %d, want 2", len(draft.Task.Parameters))
	}
	userid, password := draft.Task.Parameters[0], draft.Task.Parameters[1]
	if userid.Sensitive {
		t.Errorf("the userid was marked sensitive")
	}
	if userid.Example != "GHOPPER" {
		t.Errorf("userid example = %q, want the recorded value", userid.Example)
	}
	if !password.Sensitive {
		t.Fatalf("the password field was not marked sensitive")
	}
	if password.Example != "" {
		t.Errorf("password example = %q, want the recorded secret to be dropped", password.Example)
	}
	if strings.Contains(mustJSON(t, draft), "hunter2") {
		t.Errorf("the recorded password survived somewhere in the draft")
	}
	if !noteMatching(draft.Notes, "treated as a secret") {
		t.Errorf("nothing told the person that the field was treated as a secret: %v", draft.Notes)
	}
}

func TestLooksSensitiveCoversTheLabelsThatMatter(t *testing.T) {
	for _, label := range []string{"Password", "PASSWORD", "New password", "Passwd", "PIN", "Pass phrase", "API key", "Secret"} {
		if !looksSensitive(label) {
			t.Errorf("looksSensitive(%q) = false, want true", label)
		}
	}
	for _, label := range []string{"Account number", "Branch code", "Passenger name", "Description"} {
		if looksSensitive(label) {
			t.Errorf("looksSensitive(%q) = true, want false", label)
		}
	}
}

// A note saying "step 3 has no guard, add one" is advice with nowhere to go
// unless the wizard can show the screen that step runs against.
func TestDraftCarriesEachStepsScreenAndItsGuardShortlist(t *testing.T) {
	steps := []RecordedStep{
		{Type: "FillString", Row: 5, Column: 25, Length: 8, Text: "40218855"},
		{Type: "PressEnter"},
		{Type: "PressPF3"},
	}
	screens := []RecordedScreen{
		{StepIndex: 0, Text: entryScreen()},
		{StepIndex: 2, Text: resultScreen()},
	}
	draft, err := DraftFromRecording("Balance", steps, screens, resultScreen())
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.StepScreens) != len(draft.Task.Steps) {
		t.Fatalf("stepScreens = %d, steps = %d — they must line up", len(draft.StepScreens), len(draft.Task.Steps))
	}
	if !strings.Contains(draft.StepScreens[0].Screen, "ACCOUNT ENQUIRY") {
		t.Errorf("step 1 carries the wrong screen")
	}
	if !strings.Contains(draft.StepScreens[1].Screen, "ACCOUNT DETAIL") {
		t.Errorf("step 2 carries the wrong screen")
	}
	// The shortlist and the chosen guard cannot disagree: the first candidate
	// IS the guard the draft picked, or the wizard would offer to "change" a
	// guard to the one it already has.
	chosen := draft.Task.Steps[0].Expect[0]
	first := draft.StepScreens[0].GuardCandidates[0]
	if chosen != first {
		t.Errorf("first candidate = %+v, chosen guard = %+v", first, chosen)
	}
	if len(draft.StepScreens[0].GuardCandidates) < 2 {
		t.Errorf("only %d candidate(s) offered for a screen with several runs of text",
			len(draft.StepScreens[0].GuardCandidates))
	}
	for _, c := range draft.StepScreens[0].GuardCandidates {
		if c.Row <= 0 || c.Column <= 0 || strings.TrimSpace(c.Text) == "" {
			t.Errorf("candidate %+v is not usable as a guard", c)
		}
	}
}

func TestGuardCandidatesRespectsItsLimit(t *testing.T) {
	got := GuardCandidates(entryScreen(), 2)
	if len(got) != 2 {
		t.Fatalf("candidates = %d, want 2", len(got))
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
