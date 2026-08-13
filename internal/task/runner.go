package task

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// Terminal is the slice of the host a task run needs. Narrower than
// host.Host on purpose: a task must not be able to Stop() the session or
// start a file transfer, and a narrow interface makes the runner testable
// against a fake without standing up s3270.
type Terminal interface {
	UpdateScreen() error
	GetScreenSnapshot() *host.Screen
	MoveCursor(row, col int) error
	WriteStringAt(row, col int, text string) error
	SendKey(key string) error
}

// Result is what a run produces: the answer if it got one, and if it did
// not, exactly where it stopped and what it saw.
type Result struct {
	Task      string        `json:"task"`
	StartedAt time.Time     `json:"startedAt"`
	Duration  time.Duration `json:"-"`
	// DurationMS is the wire form; a time.Duration marshals as a nanosecond
	// integer, which is not what a result card wants to render.
	DurationMS int64         `json:"durationMs"`
	Completed  bool          `json:"completed"`
	Steps      []StepResult  `json:"steps"`
	Outputs    []OutputValue `json:"outputs,omitempty"`
	Failure    *Failure      `json:"failure,omitempty"`
}

// StepResult records what happened at one step.
type StepResult struct {
	Index       int    `json:"index"`
	Description string `json:"description,omitempty"`
	AIDKey      string `json:"aidKey,omitempty"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
}

// Step statuses.
const (
	StepOK      = "ok"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// OutputValue is one captured piece of the answer.
type OutputValue struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Value string `json:"value"`
	Found bool   `json:"found"`
}

// Failure says where a run stopped and why, in terms the operator can act
// on. Screen is what the terminal was actually showing at the point it gave
// up — without it, "step 3 failed" is not a diagnosis.
type Failure struct {
	Step     int    `json:"step"`
	Reason   string `json:"reason"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Screen   string `json:"screen,omitempty"`
}

// Runner executes tasks against one terminal.
type Runner struct {
	Terminal Terminal
	// SettleDelay is paused after each key press before the screen is read.
	// s3270 already waits for the keyboard to unlock, so this covers hosts
	// that unlock before they have finished painting.
	SettleDelay time.Duration
	// Now is injectable so tests do not depend on the clock.
	Now func() time.Time
}

const defaultSettleDelay = 150 * time.Millisecond

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run executes the task with the supplied parameter values.
//
// Two behaviours are deliberate and are the reason this is not the workflow
// player. First, a step that fails ends the run: the workflow player logs
// and continues, which for a scripted regression is reasonable and for a
// business transaction is how you type an account number into whatever field
// is under the cursor on a screen you did not expect. Second, the run never
// disconnects and never navigates back. Wherever it stopped is where the
// terminal is left, so the operator can look at it and take over by hand.
func (r *Runner) Run(ctx context.Context, t Task, params map[string]string) (*Result, error) {
	if r.Terminal == nil {
		return nil, fmt.Errorf("no terminal is connected")
	}
	resolved, err := t.ResolveParameters(params)
	if err != nil {
		return nil, err
	}

	started := r.now()
	result := &Result{
		Task:      t.Name,
		StartedAt: started,
		Steps:     make([]StepResult, 0, len(t.Steps)),
	}
	finish := func() *Result {
		result.Duration = r.now().Sub(started)
		result.DurationMS = result.Duration.Milliseconds()
		return result
	}

	for i, step := range t.Steps {
		stepNo := i + 1
		if err := ctx.Err(); err != nil {
			result.Steps = append(result.Steps, StepResult{
				Index: stepNo, Description: step.Description, Status: StepSkipped,
				Detail: "cancelled",
			})
			result.Failure = &Failure{Step: stepNo, Reason: "The task was cancelled."}
			return finish(), nil
		}

		screen, err := r.readScreen()
		if err != nil {
			result.Steps = append(result.Steps, stepResultFailed(stepNo, step, err.Error()))
			result.Failure = &Failure{Step: stepNo, Reason: fmt.Sprintf("Could not read the screen: %v", err)}
			return finish(), nil
		}

		if failure := checkExpectations(screen, step.Expect); failure != nil {
			failure.Step = stepNo
			failure.Screen = screenText(screen)
			result.Steps = append(result.Steps, stepResultFailed(stepNo, step, failure.Reason))
			result.Failure = failure
			return finish(), nil
		}

		if err := r.applyInputs(step, resolved); err != nil {
			result.Steps = append(result.Steps, stepResultFailed(stepNo, step, err.Error()))
			result.Failure = &Failure{
				Step:   stepNo,
				Reason: err.Error(),
				Screen: screenText(screen),
			}
			return finish(), nil
		}

		key := strings.TrimSpace(step.AIDKey)
		if key != "" {
			canonical, ok := CanonicalAIDKey(key)
			if !ok {
				// Validate() rejects these, so reaching here means a task
				// bypassed it — refuse rather than pass an arbitrary string
				// to the host.
				result.Steps = append(result.Steps, stepResultFailed(stepNo, step, "unsupported key "+key))
				result.Failure = &Failure{Step: stepNo, Reason: fmt.Sprintf("%q is not a key this can press.", key)}
				return finish(), nil
			}
			if err := r.Terminal.SendKey(canonical); err != nil {
				result.Steps = append(result.Steps, stepResultFailed(stepNo, step, err.Error()))
				result.Failure = &Failure{
					Step:   stepNo,
					Reason: fmt.Sprintf("The host did not accept %s: %v", canonical, err),
					Screen: screenText(screen),
				}
				return finish(), nil
			}
			r.settle()
		}

		result.Steps = append(result.Steps, StepResult{
			Index: stepNo, Description: step.Description, AIDKey: key, Status: StepOK,
		})
	}

	final, err := r.readScreen()
	if err != nil {
		result.Failure = &Failure{Reason: fmt.Sprintf("Could not read the final screen: %v", err)}
		return finish(), nil
	}

	outputs, missing := captureOutputs(final, t.Outputs)
	result.Outputs = outputs
	if len(missing) > 0 {
		// The navigation worked and the answer is not there. That is a
		// failure, not a success with blanks: a result card showing an empty
		// balance is worse than one saying it could not read the balance.
		result.Failure = &Failure{
			Step:   len(t.Steps),
			Reason: fmt.Sprintf("The task ran but could not read %s from the final screen.", humanList(missing)),
			Screen: screenText(final),
		}
		return finish(), nil
	}

	result.Completed = true
	return finish(), nil
}

func stepResultFailed(index int, step Step, detail string) StepResult {
	return StepResult{
		Index:       index,
		Description: step.Description,
		AIDKey:      strings.TrimSpace(step.AIDKey),
		Status:      StepFailed,
		Detail:      detail,
	}
}

func (r *Runner) settle() {
	delay := r.SettleDelay
	if delay <= 0 {
		delay = defaultSettleDelay
	}
	time.Sleep(delay)
}

func (r *Runner) readScreen() (*host.Screen, error) {
	if err := r.Terminal.UpdateScreen(); err != nil {
		return nil, err
	}
	screen := r.Terminal.GetScreenSnapshot()
	if screen == nil {
		return nil, fmt.Errorf("the terminal has no screen")
	}
	return screen, nil
}

func (r *Runner) applyInputs(step Step, resolved map[string]string) error {
	for j, in := range step.Inputs {
		text := in.Value
		if in.Parameter != "" {
			value, ok := resolved[in.Parameter]
			if !ok {
				return fmt.Errorf("input %d refers to parameter %q, which this task does not define", j+1, in.Parameter)
			}
			// An optional parameter left blank means "do not type here",
			// not "type nothing", which on a 3270 are different: the latter
			// would still move the cursor and could trip an auto-skip.
			if value == "" {
				continue
			}
			text = value
		}
		if text == "" {
			continue
		}
		row := in.Row - 1
		col := in.Column - 1
		if err := r.Terminal.MoveCursor(row, col); err != nil {
			return fmt.Errorf("could not put the cursor at row %d column %d: %w", in.Row, in.Column, err)
		}
		if err := r.Terminal.WriteStringAt(row, col, text); err != nil {
			return fmt.Errorf("could not type into the field at row %d column %d: %w", in.Row, in.Column, err)
		}
	}
	return nil
}

// checkExpectations returns nil when every guard holds.
func checkExpectations(screen *host.Screen, expects []Expect) *Failure {
	for _, e := range expects {
		want := strings.TrimRight(e.Text, " ")
		got := strings.TrimRight(regionText(screen, e.Row, e.Column, len([]rune(e.Text))), " ")
		matched := got == want
		if e.Absent {
			if matched {
				return &Failure{
					Reason:   fmt.Sprintf("The screen shows %q at row %d column %d, which means this step cannot run.", want, e.Row, e.Column),
					Expected: "not " + want,
					Actual:   got,
				}
			}
			continue
		}
		if !matched {
			return &Failure{
				Reason:   fmt.Sprintf("This is not the screen the task expected at row %d column %d.", e.Row, e.Column),
				Expected: want,
				Actual:   got,
			}
		}
	}
	return nil
}

// captureOutputs reads the answer off the final screen. It returns the
// values it found and the labels of any non-optional output it could not.
func captureOutputs(screen *host.Screen, outputs []Output) ([]OutputValue, []string) {
	if len(outputs) == 0 {
		return nil, nil
	}
	values := make([]OutputValue, 0, len(outputs))
	var missing []string
	for _, o := range outputs {
		raw := strings.TrimSpace(regionText(screen, o.Row, o.Column, o.Length))
		value := raw
		found := raw != ""
		if o.Pattern != "" {
			// Validate() compiled this already; a failure here means the
			// task was stored by something that skipped validation, so treat
			// it as "cannot read" rather than panicking on a run.
			re, err := regexp.Compile(o.Pattern)
			if err != nil {
				found = false
				value = ""
			} else if match := re.FindStringSubmatch(raw); match != nil {
				if len(match) > 1 {
					value = match[1]
				} else {
					value = match[0]
				}
				found = true
			} else {
				found = false
				value = ""
			}
		}
		values = append(values, OutputValue{
			Name:  o.Name,
			Label: o.DisplayLabel(),
			Value: value,
			Found: found,
		})
		if !found && !o.Optional {
			missing = append(missing, o.DisplayLabel())
		}
	}
	return values, missing
}

// regionText reads length characters from a 1-based row/column, clamped to
// the row. A region is a span on one line: an answer that wraps is two
// outputs, and letting one silently run onto the next line would capture the
// label of whatever sits below it.
func regionText(screen *host.Screen, row, col, length int) string {
	if screen == nil || length <= 0 || row <= 0 || col <= 0 {
		return ""
	}
	y := row - 1
	x := col - 1
	if y >= screen.Height || x >= screen.Width {
		return ""
	}
	end := x + length - 1
	if end >= screen.Width {
		end = screen.Width - 1
	}
	// Positions the host never wrote come back as NUL rather than space.
	// Left alone they would reach a JSON result and a comparison against
	// recorded text, where a rune nobody can see would silently fail every
	// match. Screen.Text() already normalises them; Substring does not.
	return strings.ReplaceAll(screen.Substring(x, y, end, y), "\x00", " ")
}

func screenText(screen *host.Screen) string {
	if screen == nil {
		return ""
	}
	return screen.Text()
}

func humanList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}
