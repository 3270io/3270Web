// SPDX-License-Identifier: AGPL-3.0-or-later

package task

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Building a task from a recording.
//
// A recording says which keys were pressed and which fields were filled. A
// task needs more than that: which of those filled values are *inputs* rather
// than fixed choices, what each one should be called, and what on the final
// screen is the answer.
//
// Nothing here decides any of that. It produces a draft — a proposal with
// suggested guards and suggested parameter names — for a human to confirm or
// change. Every suggestion carries a note saying what was assumed, because a
// wizard that silently guesses is worse than one that asks: the operator ends
// up with a task that runs and is wrong.

// RecordedStep is one step from a recording, in the shape the draft builder
// needs. It mirrors session.WorkflowStep without importing it, so this package
// stays free of the session model.
type RecordedStep struct {
	Type   string
	Row    int
	Column int
	Length int
	Text   string
}

// RecordedScreen is a screen captured during recording, marking where its
// group of steps begins.
type RecordedScreen struct {
	StepIndex int
	Text      string
}

// Draft is a proposed task plus the reasoning behind it.
type Draft struct {
	Task Task `json:"task"`
	// Notes explain every suggestion, so the person confirming the draft can
	// see what was assumed rather than having to reverse-engineer it.
	Notes []string `json:"notes,omitempty"`
	// FinalScreen is the screen the flow ended on, for picking outputs from.
	// A draft never proposes outputs: which part of a screen is "the answer"
	// is a judgement about the business, not something derivable from a
	// recording.
	FinalScreen string `json:"finalScreen,omitempty"`
}

// aidStepTypes maps a recording's step type onto the key a task step presses.
var aidStepTypes = func() map[string]string {
	m := map[string]string{
		"pressenter": "Enter",
		"enter":      "Enter",
		"clear":      "Clear",
		"attn":       "Attn",
		"sysreq":     "SysReq",
	}
	for i := 1; i <= 24; i++ {
		m[fmt.Sprintf("presspf%d", i)] = fmt.Sprintf("PF%d", i)
		m[fmt.Sprintf("pf%d", i)] = fmt.Sprintf("PF%d", i)
	}
	for i := 1; i <= 3; i++ {
		m[fmt.Sprintf("presspa%d", i)] = fmt.Sprintf("PA%d", i)
		m[fmt.Sprintf("pa%d", i)] = fmt.Sprintf("PA%d", i)
	}
	return m
}()

func aidKeyForStepType(stepType string) (string, bool) {
	key, ok := aidStepTypes[strings.ToLower(strings.TrimSpace(stepType))]
	return key, ok
}

// DraftFromRecording turns a recording into a proposed task.
//
// finalScreen is the screen the flow ended on, which the caller must supply
// from the live session: the recorder captures each screen immediately BEFORE
// its submit, so the screen produced by the last key press — the one holding
// the answer — is by construction not in the recording. Passing "" falls back
// to the last recorded screen, which is the screen before the final key and
// therefore the wrong one to pick outputs from.
func DraftFromRecording(name string, steps []RecordedStep, screens []RecordedScreen, finalScreen string) (*Draft, error) {
	draft := &Draft{Task: Task{Name: strings.TrimSpace(name), Source: "recording"}}
	byIndex := make(map[int]string, len(screens))
	for _, s := range screens {
		byIndex[s.StepIndex] = s.Text
	}

	usedNames := make(map[string]bool)
	var pending []RecordedStep
	groupStart := 0

	for i, step := range steps {
		lower := strings.ToLower(strings.TrimSpace(step.Type))
		switch {
		case lower == "fillstring":
			if len(pending) == 0 {
				groupStart = i
			}
			pending = append(pending, step)
			continue
		case lower == "connect" || lower == "disconnect":
			// Connect and Disconnect are workflow scaffolding. A task runs in
			// a session that is already connected, and deliberately never
			// disconnects — it leaves the terminal where it finished.
			continue
		}

		key, ok := aidKeyForStepType(step.Type)
		if !ok {
			draft.Notes = append(draft.Notes,
				fmt.Sprintf("Skipped a recorded %q step, which is not a key a task can press.", step.Type))
			continue
		}

		start := groupStart
		if len(pending) == 0 {
			start = i
		}
		screen := byIndex[start]

		taskStep := Step{AIDKey: key}
		if screen != "" {
			if anchor, ok := deriveAnchor(screen); ok {
				taskStep.Expect = []Expect{anchor}
				taskStep.Description = describeScreen(screen)
			} else {
				draft.Notes = append(draft.Notes, fmt.Sprintf(
					"Step %d has no guard: no distinctive text was found on its screen. Add one, or the task will act on whatever screen it finds.",
					len(draft.Task.Steps)+1))
			}
		} else {
			draft.Notes = append(draft.Notes, fmt.Sprintf(
				"Step %d has no guard: no screen was captured for it. Add one before relying on this task.",
				len(draft.Task.Steps)+1))
		}

		for _, fill := range pending {
			if fill.Text == "" {
				// A field cleared during recording is a real action, but it
				// is not an input and guessing which is which is exactly what
				// this must not do.
				draft.Notes = append(draft.Notes, fmt.Sprintf(
					"A field at row %d column %d was cleared during recording; that was not carried into the task.",
					fill.Row, fill.Column))
				continue
			}
			label, hasLabel := labelLeftOf(screen, fill.Row, fill.Column)
			paramName := uniqueParamName(label, fill.Row, fill.Column, usedNames)
			usedNames[paramName] = true

			p := Parameter{
				Name:      paramName,
				Label:     label,
				Required:  true,
				Example:   fill.Text,
				MaxLength: fill.Length,
			}
			if !hasLabel {
				p.Label = fmt.Sprintf("Value at row %d column %d", fill.Row, fill.Column)
				draft.Notes = append(draft.Notes, fmt.Sprintf(
					"No label was found to the left of the field at row %d column %d, so %q is a placeholder name.",
					fill.Row, fill.Column, paramName))
			}
			draft.Task.Parameters = append(draft.Task.Parameters, p)
			taskStep.Inputs = append(taskStep.Inputs, Input{
				Row: fill.Row, Column: fill.Column, Length: fill.Length, Parameter: paramName,
			})
		}

		// Everything typed becomes a parameter, because that is the reversible
		// assumption: turning one into a fixed value is a click, whereas a
		// value silently hard-coded into a task is a bug nobody sees until the
		// task is run for a different account.
		if len(pending) > 0 {
			draft.Notes = append(draft.Notes, fmt.Sprintf(
				"Step %d: %d recorded value(s) became inputs the operator is asked for. Change any that should always be the same to a fixed value.",
				len(draft.Task.Steps)+1, len(taskStep.Inputs)))
		}

		draft.Task.Steps = append(draft.Task.Steps, taskStep)
		pending = nil
	}

	if len(pending) > 0 {
		draft.Notes = append(draft.Notes,
			"The recording ended with values typed but no key pressed; those were not carried into the task.")
	}
	if len(draft.Task.Steps) == 0 {
		return nil, fmt.Errorf("this recording has no steps a task can use — record a flow that presses at least one key")
	}

	draft.FinalScreen = finalScreen
	if draft.FinalScreen == "" {
		if len(screens) > 0 {
			draft.FinalScreen = screens[len(screens)-1].Text
		}
		draft.Notes = append(draft.Notes,
			"The screen the flow ended on was not available, so the screen shown for picking outputs is the one before the last key press. Re-open this from the session that ran the flow.")
	}
	draft.Notes = append(draft.Notes,
		"No outputs were proposed: which part of the final screen is the answer is a business judgement, not something a recording reveals. Pick them before saving.")

	return draft, nil
}

// deriveAnchor picks a guard from a screen: the most substantial run of text
// on the *earliest* row that has one, which on a 3270 is the panel title.
//
// Row order beats length, deliberately. A first attempt scored purely on
// length and chose "Enter the account details below" over "ACCOUNT ENQUIRY" —
// the wrong answer twice over. Instruction text is longer but changes between
// releases and says nothing about which panel this is, whereas the title is
// what identifies it. Both are protected text, so neither is affected by what
// the operator types.
func deriveAnchor(screen string) (Expect, bool) {
	lines := strings.Split(screen, "\n")
	for row, line := range lines {
		if row >= 6 {
			// Only the top of the screen. Lower rows carry data, dates and
			// message text, which change between runs.
			break
		}
		best := Expect{}
		bestScore := 0
		for _, m := range wordRunPattern.FindAllStringIndex(line, -1) {
			text := line[m[0]:m[1]]
			trimmed := strings.TrimSpace(text)
			if len(trimmed) < 4 || !strings.ContainsFunc(trimmed, unicode.IsLetter) {
				// A run of digits is a date, a figure or a count, not a title.
				continue
			}
			score := len(text)
			// Prefer text with a space in it: "ACCOUNT ENQUIRY" identifies a
			// screen, "ACCOUNT" could be anywhere.
			if strings.Contains(strings.TrimSpace(text), " ") {
				score += 8
			}
			if score > bestScore {
				bestScore = score
				best = Expect{Row: row + 1, Column: m[0] + 1, Text: text}
			}
		}
		if bestScore > 0 {
			return best, true
		}
	}
	return Expect{}, false
}

// wordRunPattern matches a run of letters, digits and single interior spaces —
// the shape of a panel title. A run may START with a digit, because 3270
// panels routinely do: "3270 Example Application", or a bare transaction
// code. Requiring a leading letter clipped those to their first word, which
// made a weaker anchor than the title actually offers.
var wordRunPattern = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9]*(?: [A-Za-z0-9]+)*`)

func describeScreen(screen string) string {
	if anchor, ok := deriveAnchor(screen); ok {
		return "On " + strings.TrimSpace(anchor.Text)
	}
	return ""
}

// labelLeftOf reads the text immediately to the left of a field and tidies it
// into a human label. This is the same relationship the renderer uses to give
// each input an aria-label, and for the same reason: it is the only thing on
// the screen that says what the field is for.
func labelLeftOf(screen string, row, column int) (string, bool) {
	if screen == "" || row <= 0 || column <= 1 {
		return "", false
	}
	lines := strings.Split(screen, "\n")
	if row > len(lines) {
		return "", false
	}
	line := lines[row-1]
	end := column - 1
	if end > len(line) {
		end = len(line)
	}
	left := line[:end]
	// Strip the leader dots and punctuation 3270 panels use between a label
	// and its field: "First Name  . . . ." -> "First Name".
	left = strings.TrimRight(left, " .:_-")
	if left == "" {
		return "", false
	}
	// Keep only the last phrase, so a row holding two labelled fields does
	// not hand the second one the first one's text as well.
	if idx := strings.LastIndex(left, "  "); idx >= 0 {
		left = left[idx+2:]
	}
	left = strings.TrimSpace(left)
	if len([]rune(left)) < 2 || len(left) > 60 {
		return "", false
	}
	return left, true
}

// uniqueParamName turns a label into a machine name, falling back to the
// field position when the label yields nothing usable.
func uniqueParamName(label string, row, column int, used map[string]bool) string {
	base := slugify(label)
	if base == "" {
		base = fmt.Sprintf("field_r%dc%d", row, column)
	}
	name := base
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	return name
}

func slugify(label string) string {
	var sb strings.Builder
	lastUnderscore := false
	for _, r := range label {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			sb.WriteRune(unicode.ToLower(r))
			lastUnderscore = false
		default:
			if sb.Len() > 0 && !lastUnderscore {
				sb.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(sb.String(), "_")
	if out == "" {
		return ""
	}
	// A name must start with a letter to satisfy Validate.
	if !unicode.IsLetter(rune(out[0])) {
		out = "f_" + out
	}
	if len(out) > MaxNameLength {
		out = strings.Trim(out[:MaxNameLength], "_")
	}
	return out
}

// DeriveGuard exposes the anchor heuristic so other producers of tasks — the
// chaos mind-map importer, for one — derive guards the same way a recording
// does. Two implementations of "what identifies this screen" would drift, and
// the difference would show up as a task that fails only when imported one way.
func DeriveGuard(screenText string) (Expect, bool) {
	return deriveAnchor(screenText)
}

// SuggestParameterName turns a label into a machine name that satisfies
// Validate, uniquely within used. Shared for the same reason as DeriveGuard.
func SuggestParameterName(label string, used map[string]bool) string {
	return uniqueParamName(label, 0, 0, used)
}
