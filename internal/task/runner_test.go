package task

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// fakeTerminal replays a scripted sequence of screens: screens[0] is what the
// first step sees, and each key press advances to the next. It records every
// write so a test can assert what was actually typed where.
type fakeTerminal struct {
	screens []string
	at      int
	writes  []writeCall
	keys    []string
	failKey error
	failAt  int // 1-based key press that should fail; 0 means none
}

type writeCall struct {
	Row, Col int
	Text     string
}

func newFakeTerminal(screens ...string) *fakeTerminal {
	return &fakeTerminal{screens: screens}
}

func (f *fakeTerminal) UpdateScreen() error { return nil }

func (f *fakeTerminal) GetScreen() *host.Screen {
	idx := f.at
	if idx >= len(f.screens) {
		idx = len(f.screens) - 1
	}
	s := &host.Screen{}
	s.UpdateFromText(f.screens[idx])
	return s
}

func (f *fakeTerminal) MoveCursor(row, col int) error { return nil }

func (f *fakeTerminal) WriteStringAt(row, col int, text string) error {
	f.writes = append(f.writes, writeCall{Row: row, Col: col, Text: text})
	return nil
}

func (f *fakeTerminal) SendKey(key string) error {
	f.keys = append(f.keys, key)
	if f.failAt > 0 && len(f.keys) == f.failAt {
		return f.failKey
	}
	if f.at < len(f.screens)-1 {
		f.at++
	}
	return nil
}

// pad right-fills a line so column positions in the fixtures line up with a
// real 80-column screen.
func pad(line string) string {
	if len(line) >= 80 {
		return line[:80]
	}
	return line + strings.Repeat(" ", 80-len(line))
}

func screenOf(lines ...string) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, pad(l))
	}
	return strings.Join(out, "\n")
}

const (
	//                     1234567890123456789012345678901234567890
	menuScreen = "ACCOUNT ENQUIRY"
	entryLine  = "  Account number . . "
)

func balanceScreens() (string, string) {
	first := screenOf(
		menuScreen,
		"",
		"",
		"",
		entryLine,
	)
	// Column 20 (1-based) on row 8 holds the balance.
	second := screenOf(
		"ACCOUNT DETAIL",
		"",
		"  Name             J MARGOLIS",
		"",
		"",
		"",
		"",
		"  Cleared balance   1,240.55 CR",
	)
	return first, second
}

func balanceTask() Task {
	t := Task{
		Name: "Account balance enquiry",
		Parameters: []Parameter{{
			Name: "account_number", Label: "Account number", Required: true,
		}},
		Steps: []Step{{
			Description: "Enter the account number",
			Expect:      []Expect{{Row: 1, Column: 1, Text: "ACCOUNT ENQUIRY"}},
			Inputs:      []Input{{Row: 5, Column: 22, Length: 8, Parameter: "account_number"}},
			AIDKey:      "Enter",
		}},
		Outputs: []Output{{
			Name: "cleared_balance", Label: "Cleared balance",
			Row: 8, Column: 21, Length: 12,
		}},
	}
	if err := t.Validate(); err != nil {
		panic("balanceTask fixture does not validate: " + err.Error())
	}
	return t
}

func newRunner(term Terminal) *Runner {
	// No settle delay: the fake host is synchronous, and a real sleep would
	// only make the suite slow.
	return &Runner{Terminal: term, SettleDelay: time.Nanosecond}
}

func TestRunCapturesTheAnswer(t *testing.T) {
	first, second := balanceScreens()
	term := newFakeTerminal(first, second)
	result, err := newRunner(term).Run(context.Background(), balanceTask(), map[string]string{
		"account_number": "40218855",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Completed {
		t.Fatalf("expected the run to complete, failure = %+v", result.Failure)
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("expected one output, got %d", len(result.Outputs))
	}
	got := result.Outputs[0]
	if !got.Found || got.Value != "1,240.55 CR" {
		t.Errorf("output = %+v; want the balance", got)
	}
	if len(term.writes) != 1 || term.writes[0].Text != "40218855" {
		t.Errorf("writes = %+v; want the account number typed once", term.writes)
	}
	// The runner writes 0-based coordinates to the host, from 1-based task
	// coordinates. Getting this wrong types into the wrong field.
	if term.writes[0].Row != 4 || term.writes[0].Col != 21 {
		t.Errorf("write position = row %d col %d; want row 4 col 21 (0-based)", term.writes[0].Row, term.writes[0].Col)
	}
	if len(term.keys) != 1 || term.keys[0] != "Enter" {
		t.Errorf("keys = %v; want one Enter", term.keys)
	}
}

// A pattern turns a labelled region into just the value.
func TestRunExtractsWithAPattern(t *testing.T) {
	first, second := balanceScreens()
	task := balanceTask()
	task.Outputs[0].Column = 3
	task.Outputs[0].Length = 40
	task.Outputs[0].Pattern = `([\d,]+\.\d{2})`
	if err := task.Validate(); err != nil {
		t.Fatalf("fixture does not validate: %v", err)
	}
	result, err := newRunner(newFakeTerminal(first, second)).Run(context.Background(), task, map[string]string{
		"account_number": "40218855",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Completed {
		t.Fatalf("expected completion, failure = %+v", result.Failure)
	}
	if result.Outputs[0].Value != "1,240.55" {
		t.Errorf("value = %q; want the capture group only", result.Outputs[0].Value)
	}
}

// The whole point of the package: an unexpected screen stops the run rather
// than typing into it.
func TestRunStopsOnAnUnexpectedScreen(t *testing.T) {
	wrong := screenOf("SYSTEM UNAVAILABLE", "", "Please try later")
	term := newFakeTerminal(wrong)
	result, err := newRunner(term).Run(context.Background(), balanceTask(), map[string]string{
		"account_number": "40218855",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Completed {
		t.Fatal("the run must not report success against an unexpected screen")
	}
	if len(term.writes) != 0 {
		t.Errorf("nothing may be typed into an unexpected screen, got %+v", term.writes)
	}
	if len(term.keys) != 0 {
		t.Errorf("no key may be pressed on an unexpected screen, got %v", term.keys)
	}
	if result.Failure == nil {
		t.Fatal("expected a failure")
	}
	if result.Failure.Step != 1 {
		t.Errorf("failure step = %d; want 1", result.Failure.Step)
	}
	if result.Failure.Expected != "ACCOUNT ENQUIRY" {
		t.Errorf("failure.Expected = %q", result.Failure.Expected)
	}
	if !strings.Contains(result.Failure.Actual, "SYSTEM UNAVAIL") {
		t.Errorf("failure.Actual = %q; want what was really there", result.Failure.Actual)
	}
	// Without the screen, "step 1 failed" is not a diagnosis.
	if !strings.Contains(result.Failure.Screen, "Please try later") {
		t.Error("the failure must carry the screen the run stopped on")
	}
}

// An Absent guard is how a task refuses to read an error line as an answer.
func TestRunStopsWhenAForbiddenMessageIsPresent(t *testing.T) {
	first, _ := balanceScreens()
	errScreen := screenOf("ACCOUNT DETAIL", "INVALID ACCOUNT NUMBER")
	task := balanceTask()
	task.Steps = append(task.Steps, Step{
		Description: "Confirm the account was found",
		Expect: []Expect{
			{Row: 2, Column: 1, Text: "INVALID ACCOUNT NUMBER", Absent: true},
		},
	})
	if err := task.Validate(); err != nil {
		t.Fatalf("fixture does not validate: %v", err)
	}
	result, err := newRunner(newFakeTerminal(first, errScreen)).Run(context.Background(), task, map[string]string{
		"account_number": "40218855",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Completed {
		t.Fatal("a task must not succeed when its forbidden message is on screen")
	}
	if !strings.Contains(result.Failure.Reason, "INVALID ACCOUNT NUMBER") {
		t.Errorf("reason = %q; want the message named", result.Failure.Reason)
	}
}

// Navigation succeeding while the answer is unreadable is still a failure —
// a result card with a blank balance is worse than an honest error.
func TestRunFailsWhenTheAnswerIsMissing(t *testing.T) {
	first := screenOf(menuScreen, "", "", "", entryLine)
	blank := screenOf("ACCOUNT DETAIL")
	result, err := newRunner(newFakeTerminal(first, blank)).Run(context.Background(), balanceTask(), map[string]string{
		"account_number": "40218855",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Completed {
		t.Fatal("expected a run with no readable output to fail")
	}
	if !strings.Contains(result.Failure.Reason, "Cleared balance") {
		t.Errorf("reason = %q; want the missing output named", result.Failure.Reason)
	}
}

func TestRunAllowsAnOptionalOutputToBeMissing(t *testing.T) {
	first := screenOf(menuScreen, "", "", "", entryLine)
	blank := screenOf("ACCOUNT DETAIL")
	task := balanceTask()
	task.Outputs[0].Optional = true
	result, err := newRunner(newFakeTerminal(first, blank)).Run(context.Background(), task, map[string]string{
		"account_number": "40218855",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Completed {
		t.Fatalf("an optional output may be missing, failure = %+v", result.Failure)
	}
	if result.Outputs[0].Found {
		t.Error("a missing optional output must not be reported as found")
	}
}

func TestRunReportsAHostKeyFailure(t *testing.T) {
	first, second := balanceScreens()
	term := newFakeTerminal(first, second)
	term.failAt = 1
	term.failKey = errFake("the keyboard is locked")
	result, err := newRunner(term).Run(context.Background(), balanceTask(), map[string]string{
		"account_number": "40218855",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Completed {
		t.Fatal("expected a failure when the host rejects the key")
	}
	if !strings.Contains(result.Failure.Reason, "keyboard is locked") {
		t.Errorf("reason = %q; want the host's own error", result.Failure.Reason)
	}
}

func TestRunRejectsBadParametersBeforeTouchingTheHost(t *testing.T) {
	first, second := balanceScreens()
	term := newFakeTerminal(first, second)
	_, err := newRunner(term).Run(context.Background(), balanceTask(), map[string]string{})
	if err == nil {
		t.Fatal("expected a missing required parameter to be rejected")
	}
	if len(term.writes) != 0 || len(term.keys) != 0 {
		t.Error("nothing may reach the host when the parameters are invalid")
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	first, second := balanceScreens()
	term := newFakeTerminal(first, second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := newRunner(term).Run(ctx, balanceTask(), map[string]string{
		"account_number": "40218855",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Completed {
		t.Fatal("a cancelled run must not report success")
	}
	if len(term.keys) != 0 {
		t.Error("a cancelled run must not press keys")
	}
	if !strings.Contains(result.Failure.Reason, "cancelled") {
		t.Errorf("reason = %q; want it to say the run was cancelled", result.Failure.Reason)
	}
}

// An optional parameter left blank means "leave the field alone". Typing an
// empty string would still move the cursor into the field.
func TestRunSkipsBlankOptionalInputs(t *testing.T) {
	first, second := balanceScreens()
	task := balanceTask()
	task.Parameters = append(task.Parameters, Parameter{Name: "branch"})
	task.Steps[0].Inputs = append(task.Steps[0].Inputs, Input{Row: 6, Column: 22, Parameter: "branch"})
	if err := task.Validate(); err != nil {
		t.Fatalf("fixture does not validate: %v", err)
	}
	term := newFakeTerminal(first, second)
	if _, err := newRunner(term).Run(context.Background(), task, map[string]string{
		"account_number": "40218855",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(term.writes) != 1 {
		t.Errorf("writes = %+v; want only the account number", term.writes)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
