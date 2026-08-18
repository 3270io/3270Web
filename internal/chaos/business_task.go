// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"fmt"
	"strings"

	"github.com/jnnngs/3270Web/internal/task"
)

// Converting a discovered BusinessFunction into a runnable business task.
//
// This is what makes chaos exploration worth something beyond testing: the
// mind-map is the asset, and a task is the product. A run discovers screens,
// transitions and the fields that drive them; a BusinessFunction names one
// path through that graph. Turning it into a task means anyone can run that
// path from a form.
//
// The two models identify a screen differently, and that is the whole
// difficulty. Chaos keys screens by hash, which is exact and useless to a
// person: when it stops matching, all it can say is "a different screen". A
// task guards with positional text from the screen's own labels, which
// survives a repainted date and can say what it wanted and what was there.
// The bridge is MindMapArea.PreviewText — the captured screen, clipped to
// 24x80 from the top-left, so positions inside that window are exact.
//
// As with the recording importer, this proposes and reports rather than
// guessing: it returns a task.Draft whose notes list every assumption, and it
// never invents outputs.

// BusinessFunctionToTask converts a cataloged business function into a task
// draft. The draft is deliberately incomplete — it has no outputs — because
// which part of the final screen is the answer is a business judgement that
// neither a recording nor a chaos run reveals.
func BusinessFunctionToTask(mm *MindMap, name string) (*task.Draft, error) {
	if mm == nil || len(mm.BusinessFunctions) == 0 {
		return nil, fmt.Errorf("no business functions are cataloged in this run")
	}
	fn := mm.BusinessFunctions[normalizeBusinessFunctionName(name)]
	if fn == nil {
		return nil, fmt.Errorf("business function %q not found; available: %s",
			name, strings.Join(businessFunctionNames(mm), ", "))
	}

	draft := &task.Draft{Task: task.Task{
		Name:        fn.Name,
		Description: fn.Description,
		Source:      "chaos",
	}}

	// Map each business parameter onto a task parameter, keeping the original
	// name as the label. A chaos parameter name is free text and a task
	// parameter name has to be an identifier.
	used := make(map[string]bool)
	taskNameFor := make(map[string]string, len(fn.Parameters))
	declared := make(map[string]task.Parameter, len(fn.Parameters))
	for _, p := range fn.Parameters {
		original := strings.TrimSpace(p.Name)
		if original == "" {
			continue
		}
		machine := task.SuggestParameterName(original, used)
		used[machine] = true
		taskNameFor[original] = machine
		declared[original] = task.Parameter{
			Name:        machine,
			Label:       original,
			Description: p.Description,
			Example:     p.Example,
			Required:    p.Required,
			Sensitive:   isSensitiveParameter(mm, fn, p),
		}
	}

	steps := fn.Steps
	if len(steps) == 0 {
		// A function recorded only an entry screen: the workflow generator
		// synthesises a single step that fills every parameter there, and this
		// mirrors that so both paths produce the same shape.
		steps = []BusinessFunctionStep{{ScreenHash: fn.EntryScreenHash, AIDKey: "Enter"}}
		for _, p := range fn.Parameters {
			if strings.TrimSpace(p.FieldKey) == "" {
				continue
			}
			steps[0].Inputs = append(steps[0].Inputs,
				BusinessFunctionInput{FieldKey: p.FieldKey, Parameter: p.Name})
		}
		draft.Notes = append(draft.Notes,
			"This function recorded only an entry screen, so it became a single step filling every parameter there.")
	}

	referenced := make(map[string]bool)
	for i, step := range steps {
		stepNo := i + 1
		key := strings.TrimSpace(step.AIDKey)
		if key == "" {
			key = "Enter"
		}
		canonical, ok := task.CanonicalAIDKey(key)
		if !ok {
			draft.Notes = append(draft.Notes, fmt.Sprintf(
				"Step %d pressed %q, which a task cannot send; the step was dropped.", stepNo, key))
			continue
		}

		taskStep := task.Step{AIDKey: canonical}
		area := mm.Areas[strings.TrimSpace(step.ScreenHash)]
		if area != nil && strings.TrimSpace(area.PreviewText) != "" {
			if guard, found := task.DeriveGuard(area.PreviewText); found {
				taskStep.Expect = []task.Expect{guard}
			} else {
				draft.Notes = append(draft.Notes, fmt.Sprintf(
					"Step %d has no guard: no distinctive text was found on its screen. Add one, or the task will act on whatever screen it finds.", stepNo))
			}
			if purpose := strings.TrimSpace(area.BusinessPurpose); purpose != "" {
				taskStep.Description = purpose
			}
		} else {
			// A hash with no captured preview cannot become a guard. Chaos
			// would have matched on the hash; a task cannot, and pretending
			// otherwise would produce a task that acts on any screen at all.
			draft.Notes = append(draft.Notes, fmt.Sprintf(
				"Step %d has no guard: the run recorded no screen text for %s. Add one before relying on this task.",
				stepNo, screenLabel(step.ScreenHash)))
		}

		for _, in := range step.Inputs {
			row, col, length, parsed := ParseMindMapFieldKey(in.FieldKey)
			if !parsed {
				draft.Notes = append(draft.Notes, fmt.Sprintf(
					"Step %d: field key %q could not be read, so that input was dropped.", stepNo, in.FieldKey))
				continue
			}
			input := task.Input{Row: row, Column: col, Length: length}
			if ref := strings.TrimSpace(in.Parameter); ref != "" {
				machine, known := taskNameFor[ref]
				if !known {
					draft.Notes = append(draft.Notes, fmt.Sprintf(
						"Step %d referenced parameter %q, which the function does not declare; that input was dropped.", stepNo, ref))
					continue
				}
				input.Parameter = machine
				referenced[ref] = true
			} else if in.Value != "" {
				input.Value = in.Value
			} else {
				// Neither a value nor a parameter is not an input.
				continue
			}
			taskStep.Inputs = append(taskStep.Inputs, input)
		}

		draft.Task.Steps = append(draft.Task.Steps, taskStep)
	}

	// Only parameters something actually fills survive. A task with a
	// parameter no step uses is refused by Validate — and rightly: it would
	// put a box on the form that silently does nothing.
	for _, p := range fn.Parameters {
		original := strings.TrimSpace(p.Name)
		declaredParam, ok := declared[original]
		if !ok {
			continue
		}
		if !referenced[original] {
			draft.Notes = append(draft.Notes, fmt.Sprintf(
				"Parameter %q is not filled by any step, so it was left out; it would have been a form field that does nothing.", original))
			continue
		}
		draft.Task.Parameters = append(draft.Task.Parameters, declaredParam)
	}

	if len(draft.Task.Steps) == 0 {
		return nil, fmt.Errorf("business function %q has no steps a task can run", fn.Name)
	}

	// The screen the function ends on is where the answer lives, and it is the
	// one the last step expects to land on.
	if final := finalPreview(mm, steps); final != "" {
		draft.FinalScreen = final
	} else {
		draft.Notes = append(draft.Notes,
			"The run recorded no screen text for the screen this function ends on, so there is nothing to pick outputs from. Run the flow once and save it as a task from the recording instead.")
	}
	draft.Notes = append(draft.Notes,
		"No outputs were proposed: which part of the final screen is the answer is a business judgement, not something a chaos run reveals. Pick them before saving.")

	return draft, nil
}

// finalPreview returns the captured screen the function ends on: the last
// step's declared landing screen where it has one, otherwise that step's own
// screen.
func finalPreview(mm *MindMap, steps []BusinessFunctionStep) string {
	if len(steps) == 0 {
		return ""
	}
	last := steps[len(steps)-1]
	for _, hash := range []string{last.ExpectHash, last.ScreenHash} {
		if area := mm.Areas[strings.TrimSpace(hash)]; area != nil {
			if text := strings.TrimSpace(area.PreviewText); text != "" {
				return area.PreviewText
			}
		}
	}
	return ""
}

// screenLabel names a screen for a human-readable note. shortHash returns ""
// for an empty hash, which would read as a gap in the sentence rather than as
// the absence it is.
func screenLabel(hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return "this step"
	}
	return "screen " + shortHash(hash)
}

func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

// BusinessFunctionsToTaskDrafts converts every function in the catalogue,
// so a whole discovered application can be reviewed at once.
func BusinessFunctionsToTaskDrafts(mm *MindMap) ([]*task.Draft, error) {
	// A session with no chaos data at all hands in a nil map, and
	// businessFunctionNames dereferences it. Guarding here rather than there
	// keeps the fix local: every other caller of that helper has already
	// checked the map is non-nil, and this was the one that had not.
	if mm == nil {
		return nil, fmt.Errorf("no business functions are cataloged in this run")
	}
	names := businessFunctionNames(mm)
	if len(names) == 0 {
		return nil, fmt.Errorf("no business functions are cataloged in this run")
	}
	out := make([]*task.Draft, 0, len(names))
	for _, name := range names {
		draft, err := BusinessFunctionToTask(mm, name)
		if err != nil {
			// One unconvertible function must not hide the rest.
			out = append(out, &task.Draft{
				Task:  task.Task{Name: name, Source: "chaos"},
				Notes: []string{"Could not be converted: " + err.Error()},
			})
			continue
		}
		out = append(out, draft)
	}
	return out, nil
}
