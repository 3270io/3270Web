package chaos

import (
	"strings"
	"testing"
)

// inquiryFunction is the same shape the workflow generator is tested against:
// a menu selection, then a parameterised form, landing on a result screen.
func inquiryFunction(t *testing.T, mm *MindMap) {
	t.Helper()
	err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:        "Account Inquiry",
		Description: "Look up an account balance",
		Steps: []BusinessFunctionStep{
			{ScreenHash: "menu", Inputs: []BusinessFunctionInput{{FieldKey: "R20C1L2", Value: "1"}}, AIDKey: "Enter", ExpectHash: "form"},
			{ScreenHash: "form", Inputs: []BusinessFunctionInput{{FieldKey: "R2C13L8", Parameter: "account number"}}, AIDKey: "PF5", ExpectHash: "confirm"},
		},
		Parameters: []BusinessParameter{
			{Name: "account number", Description: "The account to look up", ScreenHash: "form", FieldKey: "R2C13L8", Example: "40218855", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

func TestBusinessFunctionToTask(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	inquiryFunction(t, mm)

	draft, err := BusinessFunctionToTask(mm, "account inquiry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if draft.Task.Name != "Account Inquiry" || draft.Task.Source != "chaos" {
		t.Errorf("name/source = %q/%q", draft.Task.Name, draft.Task.Source)
	}
	if len(draft.Task.Steps) != 2 {
		t.Fatalf("expected two steps, got %+v", draft.Task.Steps)
	}

	// A chaos parameter name is free text; a task parameter name is an
	// identifier. The original has to survive as the label or the form becomes
	// unreadable.
	if len(draft.Task.Parameters) != 1 {
		t.Fatalf("expected one parameter, got %+v", draft.Task.Parameters)
	}
	p := draft.Task.Parameters[0]
	if p.Name != "account_number" {
		t.Errorf("parameter name = %q, want account_number", p.Name)
	}
	if p.Label != "account number" {
		t.Errorf("parameter label = %q, want the original name", p.Label)
	}
	if p.Example != "40218855" || !p.Required {
		t.Errorf("parameter = %+v; example and required should carry over", p)
	}

	// Field keys become 1-based coordinates.
	first := draft.Task.Steps[0]
	if len(first.Inputs) != 1 || first.Inputs[0].Row != 20 || first.Inputs[0].Column != 1 || first.Inputs[0].Length != 2 {
		t.Errorf("step 1 input = %+v; want R20C1L2 as row 20 col 1 len 2", first.Inputs)
	}
	if first.Inputs[0].Value != "1" {
		t.Errorf("a literal menu selection should stay a literal, got %+v", first.Inputs[0])
	}
	second := draft.Task.Steps[1]
	if len(second.Inputs) != 1 || second.Inputs[0].Parameter != "account_number" {
		t.Errorf("step 2 input = %+v; want the renamed parameter", second.Inputs)
	}
	if second.AIDKey != "PF5" {
		t.Errorf("step 2 key = %q, want PF5", second.AIDKey)
	}

	// The guard is the difference between the two models: chaos matched a
	// hash, a task has to match text that a person can read.
	if len(first.Expect) != 1 || !strings.Contains(first.Expect[0].Text, "MAIN MENU") {
		t.Errorf("step 1 guard = %+v; want the menu title", first.Expect)
	}
	if len(second.Expect) != 1 || !strings.Contains(second.Expect[0].Text, "ACCOUNT INQUIRY") {
		t.Errorf("step 2 guard = %+v; want the form title", second.Expect)
	}

	// The screen the function ends on comes from the last step's landing
	// screen, which is where the answer is.
	if !strings.Contains(draft.FinalScreen, "INQUIRY RESULT") {
		t.Errorf("final screen = %q; want the confirm screen", draft.FinalScreen)
	}
	if len(draft.Task.Outputs) != 0 {
		t.Error("a draft must not invent outputs")
	}
	if !hasNote(draft.Notes, "which part of the final screen is the answer") {
		t.Errorf("expected a note explaining the missing outputs, got %v", draft.Notes)
	}

	// The whole point: what comes out has to be runnable.
	if err := draft.Task.Validate(); err != nil {
		t.Errorf("the converted task does not validate: %v", err)
	}
}

// A hash with no captured screen text cannot become a guard. Chaos matched on
// the hash; a task cannot, and a step with no guard acts on whatever screen it
// finds — so this has to be said out loud.
func TestBusinessFunctionToTaskReportsScreensWithNoText(t *testing.T) {
	mm := newMindMap()
	mm.ensureArea("blank") // no PreviewText
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:  "Blind step",
		Steps: []BusinessFunctionStep{{ScreenHash: "blank", AIDKey: "Enter"}},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	draft, err := BusinessFunctionToTask(mm, "blind step")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Task.Steps[0].Expect) != 0 {
		t.Error("no screen text means no guard can be derived")
	}
	if !hasNote(draft.Notes, "no screen text") {
		t.Errorf("expected a note about the missing guard, got %v", draft.Notes)
	}
}

// Validate refuses a parameter no step fills, and rightly — it would be a form
// field that silently does nothing. Dropping it must be reported, not silent.
func TestBusinessFunctionToTaskDropsUnusedParameters(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:  "Half wired",
		Steps: []BusinessFunctionStep{{ScreenHash: "menu", Inputs: []BusinessFunctionInput{{FieldKey: "R20C1L2", Value: "1"}}, AIDKey: "Enter"}},
		Parameters: []BusinessParameter{
			{Name: "orphan", FieldKey: "R2C13L8"},
		},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	draft, err := BusinessFunctionToTask(mm, "half wired")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Task.Parameters) != 0 {
		t.Errorf("an unused parameter must be dropped, got %+v", draft.Task.Parameters)
	}
	if !hasNote(draft.Notes, "not filled by any step") {
		t.Errorf("expected a note about the dropped parameter, got %v", draft.Notes)
	}
	if err := draft.Task.Validate(); err != nil {
		t.Errorf("dropping it is what makes the task valid: %v", err)
	}
}

// A function that only names an entry screen is synthesised into one step, the
// same way the workflow generator does it.
func TestBusinessFunctionToTaskHandlesEntryScreenOnly(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:            "Entry only",
		EntryScreenHash: "form",
		Parameters:      []BusinessParameter{{Name: "account number", FieldKey: "R2C13L8", Required: true}},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	draft, err := BusinessFunctionToTask(mm, "entry only")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Task.Steps) != 1 || draft.Task.Steps[0].AIDKey != "Enter" {
		t.Fatalf("steps = %+v", draft.Task.Steps)
	}
	if len(draft.Task.Steps[0].Inputs) != 1 || draft.Task.Steps[0].Inputs[0].Parameter != "account_number" {
		t.Errorf("inputs = %+v", draft.Task.Steps[0].Inputs)
	}
	if !hasNote(draft.Notes, "only an entry screen") {
		t.Errorf("expected a note about the synthesised step, got %v", draft.Notes)
	}
	if err := draft.Task.Validate(); err != nil {
		t.Errorf("does not validate: %v", err)
	}
}

// A key a task cannot press must not silently become Enter — that would run a
// different transaction from the one that was discovered.
func TestBusinessFunctionToTaskDropsUnpressableKeys(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name: "Odd keys",
		Steps: []BusinessFunctionStep{
			{ScreenHash: "menu", AIDKey: "Quit()"},
			{ScreenHash: "form", AIDKey: "PF5"},
		},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	draft, err := BusinessFunctionToTask(mm, "odd keys")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Task.Steps) != 1 || draft.Task.Steps[0].AIDKey != "PF5" {
		t.Errorf("steps = %+v; the unpressable step should be dropped", draft.Task.Steps)
	}
	if !hasNote(draft.Notes, "cannot send") {
		t.Errorf("expected a note about the dropped step, got %v", draft.Notes)
	}
}

// A session with no chaos data hands in a nil map. This panicked, and the unit
// tests missed it because they all passed a real (if empty) map — it took
// calling the endpoint against a fresh session to find.
func TestBusinessFunctionConversionSurvivesANilMindMap(t *testing.T) {
	if _, err := BusinessFunctionsToTaskDrafts(nil); err == nil {
		t.Fatal("expected an error, not a panic, for a nil mind map")
	}
	if _, err := BusinessFunctionToTask(nil, "anything"); err == nil {
		t.Fatal("expected an error for a nil mind map")
	}
}

func TestBusinessFunctionToTaskRejectsUnknownNames(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	inquiryFunction(t, mm)
	if _, err := BusinessFunctionToTask(mm, "nothing like it"); err == nil {
		t.Fatal("expected an error for an unknown function")
	}
	if _, err := BusinessFunctionToTask(newMindMap(), "anything"); err == nil {
		t.Fatal("expected an error when nothing is cataloged")
	}
}

// One unconvertible function must not hide the rest of the catalogue.
func TestBusinessFunctionsToTaskDraftsConvertsEverything(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	inquiryFunction(t, mm)
	if err := upsertMindMapBusinessFunction(mm, BusinessFunction{
		Name:  "Unrunnable",
		Steps: []BusinessFunctionStep{{ScreenHash: "menu", AIDKey: "Quit()"}},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	drafts, err := BusinessFunctionsToTaskDrafts(mm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(drafts) != 2 {
		t.Fatalf("expected both functions, got %d", len(drafts))
	}
	var good, bad int
	for _, d := range drafts {
		if len(d.Task.Steps) > 0 {
			good++
		} else if hasNote(d.Notes, "Could not be converted") {
			bad++
		}
	}
	if good != 1 || bad != 1 {
		t.Errorf("expected one converted and one reported, got %d and %d", good, bad)
	}
}

// A parameter landing in a hidden field must be marked sensitive, so the
// wizard masks it and no result or log line keeps it.
func TestBusinessFunctionToTaskCarriesSensitivity(t *testing.T) {
	mm := buildBusinessTestMindMap(t)
	form := mm.ensureArea("form")
	form.FieldMetadata = map[string]MindMapFieldMetadata{
		"R2C13L8": {Hidden: true},
	}
	inquiryFunction(t, mm)
	draft, err := BusinessFunctionToTask(mm, "account inquiry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(draft.Task.Parameters) != 1 || !draft.Task.Parameters[0].Sensitive {
		t.Errorf("parameter = %+v; a hidden field's parameter must be sensitive", draft.Task.Parameters)
	}
	// Validate refuses a default on a sensitive parameter; the converter must
	// not have set one from the recorded example.
	if err := draft.Task.Validate(); err != nil {
		t.Errorf("does not validate: %v", err)
	}
}

func hasNote(notes []string, substr string) bool {
	for _, n := range notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}
