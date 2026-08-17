// SPDX-License-Identifier: AGPL-3.0-or-later

// Package task implements Guided Business Tasks: a recorded 3270 flow that
// someone who knows the business question — but not the application — can run
// by filling in a form.
//
// The distinction from a recorded workflow is the point of the package.
// A workflow is a script: it replays keystrokes and, when a step fails, logs
// it and carries on. A task is an operation with a contract — named inputs,
// a guarded path, and named outputs — and when reality diverges from that
// contract it stops and says where. Blind continuation against an unexpected
// screen is worse than no automation at all: it types an account number into
// whatever field happens to be under the cursor.
//
// A task is deliberately independent of the chaos mind-map. Chaos can
// discover tasks, and its BusinessFunction catalogue converts into this one,
// but requiring a chaos run and an annotated screen graph put the feature out
// of reach of the analyst who just wants to record "check a balance".
// Everything here works from a plain recording.
package task

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

const (
	// MaxNameLength bounds a task or parameter name. These end up in a JSON
	// catalogue, a form label and an API path segment.
	MaxNameLength = 64
	// MaxDescriptionLength bounds prose fields.
	MaxDescriptionLength = 500
	// MaxPatternLength bounds a validation or extraction regexp. RE2 has no
	// catastrophic backtracking, so an unbounded pattern is a readability
	// problem rather than a denial-of-service one — but it is still a
	// problem.
	MaxPatternLength = 200
	// MaxSteps bounds a single task. A business operation that needs more
	// than this is not a task, it is an application.
	MaxSteps = 100
	// MaxValueLength bounds a supplied parameter value. The longest thing
	// that fits on a 3270 screen is one full model-5 line.
	MaxValueLength = 160
)

// Task is a named, parameterised business operation.
type Task struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  []Parameter `json:"parameters,omitempty"`
	Steps       []Step      `json:"steps"`
	Outputs     []Output    `json:"outputs,omitempty"`
	// Source records where the task came from ("recording", "chaos",
	// "import"), so a catalogue can show provenance rather than presenting
	// a machine-discovered task and a hand-authored one as equivalent.
	Source    string    `json:"source,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// Parameter is a value the operator supplies before the task runs.
type Parameter struct {
	// Name is the machine identifier steps refer to.
	Name string `json:"name"`
	// Label is what the form shows. Defaults to Name.
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
	MaxLength   int    `json:"maxLength,omitempty"`
	// Pattern is an RE2 expression the value must match. It is anchored at
	// both ends when applied, so "\d{8}" means the whole value is eight
	// digits rather than "contains eight digits somewhere".
	Pattern string `json:"pattern,omitempty"`
	// Sensitive keeps the value out of every result, event and log line.
	// The host still receives it — it has to — but nothing else keeps it.
	Sensitive bool `json:"sensitive,omitempty"`
}

// DisplayLabel is what a form should show for this parameter.
func (p Parameter) DisplayLabel() string {
	if label := strings.TrimSpace(p.Label); label != "" {
		return label
	}
	return p.Name
}

// Step is one screen interaction: check we are where we think we are, fill
// some fields, press a key.
type Step struct {
	// Description is shown in the progress list while the task runs, so the
	// operator can see what it is doing rather than watching an opaque bar.
	Description string `json:"description,omitempty"`
	// Expect guards the step. Every entry must match the current screen
	// before anything is typed.
	Expect []Expect `json:"expect,omitempty"`
	Inputs []Input  `json:"inputs,omitempty"`
	// AIDKey is the key pressed after the inputs are filled. Defaults to
	// Enter. An empty string on the final step means "fill and stop" — used
	// when the answer is already on the screen the last key produced.
	AIDKey string `json:"aidKey,omitempty"`
}

// Expect asserts that a literal string appears at a position on the screen.
//
// Positional text rather than a screen hash, deliberately. A hash changes
// the moment the host paints a different date in the corner, and when it
// mismatches the only thing it can report is "a different screen" — useless
// to the person who has to work out why. An anchor on the screen's own label
// text survives cosmetic change and, when it fails, can say exactly what it
// wanted and what was there instead.
type Expect struct {
	Row    int    `json:"row"`
	Column int    `json:"column"`
	Text   string `json:"text"`
	// Absent inverts the check: the text must NOT be at that position. This
	// is how a step guards against a known error line ("INVALID ACCOUNT")
	// appearing where the answer should be.
	Absent bool `json:"absent,omitempty"`
}

// Input fills one field. Exactly one of Value (a literal, such as a menu
// selection) or Parameter (the name of a Parameter) must be set.
type Input struct {
	Row       int    `json:"row"`
	Column    int    `json:"column"`
	Length    int    `json:"length,omitempty"`
	Value     string `json:"value,omitempty"`
	Parameter string `json:"parameter,omitempty"`
}

// Output names a region of the final screen as part of the answer.
type Output struct {
	Name   string `json:"name"`
	Label  string `json:"label,omitempty"`
	Row    int    `json:"row"`
	Column int    `json:"column"`
	Length int    `json:"length"`
	// Pattern optionally extracts from the region. With one capture group,
	// the group is the value; without, the whole match is. This is what
	// turns "Cleared balance:      1,240.55 CR" into "1,240.55".
	Pattern string `json:"pattern,omitempty"`
	// Optional allows the output to be missing without failing the run.
	//
	// The default is the strict one, unlike Parameter.Required: an output
	// that quietly comes back empty is the failure this whole package exists
	// to prevent. A task that cannot read its answer has not succeeded, and
	// saying so is the only useful thing it can do.
	Optional bool `json:"optional,omitempty"`
}

// DisplayLabel is what a result card should show for this output.
func (o Output) DisplayLabel() string {
	if label := strings.TrimSpace(o.Label); label != "" {
		return label
	}
	return o.Name
}

// NormalizeName produces the catalogue key for a task name: case- and
// whitespace-insensitive, because "Account Balance" and "account  balance"
// being two entries would only ever be a mistake.
func NormalizeName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

var parameterNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// Validate checks a task is well-formed and safe to run, normalising it in
// place. It is the single gate: the HTTP layer, the importer and the store
// all go through it, so a task that reaches the runner cannot be malformed.
func (t *Task) Validate() error {
	t.Name = strings.TrimSpace(t.Name)
	t.Description = strings.TrimSpace(t.Description)
	t.Source = strings.TrimSpace(t.Source)

	if t.Name == "" {
		return fmt.Errorf("a task name is required")
	}
	if len(t.Name) > MaxNameLength {
		return fmt.Errorf("task name is too long (max %d characters)", MaxNameLength)
	}
	if len(t.Description) > MaxDescriptionLength {
		return fmt.Errorf("description is too long (max %d characters)", MaxDescriptionLength)
	}
	if len(t.Steps) == 0 {
		return fmt.Errorf("a task needs at least one step")
	}
	if len(t.Steps) > MaxSteps {
		return fmt.Errorf("a task cannot have more than %d steps", MaxSteps)
	}

	params := make(map[string]bool, len(t.Parameters))
	for i := range t.Parameters {
		p := &t.Parameters[i]
		p.Name = strings.TrimSpace(p.Name)
		p.Label = strings.TrimSpace(p.Label)
		p.Description = strings.TrimSpace(p.Description)
		p.Pattern = strings.TrimSpace(p.Pattern)
		if !parameterNamePattern.MatchString(p.Name) {
			return fmt.Errorf("parameter %d: name must start with a letter and contain only letters, digits and underscores", i+1)
		}
		if len(p.Name) > MaxNameLength {
			return fmt.Errorf("parameter %q: name is too long (max %d characters)", p.Name, MaxNameLength)
		}
		if params[p.Name] {
			return fmt.Errorf("parameter %q is defined twice", p.Name)
		}
		params[p.Name] = true
		if len(p.Description) > MaxDescriptionLength {
			return fmt.Errorf("parameter %q: description is too long", p.Name)
		}
		if p.MaxLength < 0 || p.MaxLength > MaxValueLength {
			return fmt.Errorf("parameter %q: maxLength must be between 0 and %d", p.Name, MaxValueLength)
		}
		if err := validatePattern(p.Pattern); err != nil {
			return fmt.Errorf("parameter %q: %w", p.Name, err)
		}
		// A default for a sensitive parameter would be a password stored in
		// the catalogue in clear text.
		if p.Sensitive && strings.TrimSpace(p.Default) != "" {
			return fmt.Errorf("parameter %q: a sensitive parameter cannot have a default value", p.Name)
		}
		if err := validateFieldText(p.Default); err != nil {
			return fmt.Errorf("parameter %q default: %w", p.Name, err)
		}
	}

	used := make(map[string]bool, len(params))
	for i := range t.Steps {
		step := &t.Steps[i]
		step.Description = strings.TrimSpace(step.Description)
		step.AIDKey = strings.TrimSpace(step.AIDKey)
		if len(step.Description) > MaxDescriptionLength {
			return fmt.Errorf("step %d: description is too long", i+1)
		}
		if step.AIDKey != "" && !IsSupportedAIDKey(step.AIDKey) {
			return fmt.Errorf("step %d: %q is not a 3270 key this can press", i+1, step.AIDKey)
		}
		for j := range step.Expect {
			e := &step.Expect[j]
			if e.Row <= 0 || e.Column <= 0 {
				return fmt.Errorf("step %d expectation %d: row and column are 1-based", i+1, j+1)
			}
			if strings.TrimSpace(e.Text) == "" {
				return fmt.Errorf("step %d expectation %d: text is required", i+1, j+1)
			}
			if len(e.Text) > MaxValueLength {
				return fmt.Errorf("step %d expectation %d: text is too long", i+1, j+1)
			}
		}
		for j := range step.Inputs {
			in := &step.Inputs[j]
			in.Parameter = strings.TrimSpace(in.Parameter)
			if in.Row <= 0 || in.Column <= 0 {
				return fmt.Errorf("step %d input %d: row and column are 1-based", i+1, j+1)
			}
			if in.Length < 0 || in.Length > MaxValueLength {
				return fmt.Errorf("step %d input %d: length must be between 0 and %d", i+1, j+1, MaxValueLength)
			}
			hasValue := in.Value != ""
			hasParam := in.Parameter != ""
			if hasValue == hasParam {
				return fmt.Errorf("step %d input %d: set exactly one of value or parameter", i+1, j+1)
			}
			if hasParam {
				if !params[in.Parameter] {
					return fmt.Errorf("step %d input %d: references undefined parameter %q", i+1, j+1, in.Parameter)
				}
				used[in.Parameter] = true
			}
			if err := validateFieldText(in.Value); err != nil {
				return fmt.Errorf("step %d input %d: %w", i+1, j+1, err)
			}
		}
	}

	// A parameter nothing fills is a form field that does nothing — the
	// operator types into it and it is silently discarded.
	for name := range params {
		if !used[name] {
			return fmt.Errorf("parameter %q is never used by any step", name)
		}
	}

	outputs := make(map[string]bool, len(t.Outputs))
	for i := range t.Outputs {
		o := &t.Outputs[i]
		o.Name = strings.TrimSpace(o.Name)
		o.Label = strings.TrimSpace(o.Label)
		o.Pattern = strings.TrimSpace(o.Pattern)
		if !parameterNamePattern.MatchString(o.Name) {
			return fmt.Errorf("output %d: name must start with a letter and contain only letters, digits and underscores", i+1)
		}
		if outputs[o.Name] {
			return fmt.Errorf("output %q is defined twice", o.Name)
		}
		outputs[o.Name] = true
		if o.Row <= 0 || o.Column <= 0 {
			return fmt.Errorf("output %q: row and column are 1-based", o.Name)
		}
		if o.Length <= 0 || o.Length > MaxValueLength {
			return fmt.Errorf("output %q: length must be between 1 and %d", o.Name, MaxValueLength)
		}
		if err := validatePattern(o.Pattern); err != nil {
			return fmt.Errorf("output %q: %w", o.Name, err)
		}
	}
	return nil
}

func validatePattern(pattern string) error {
	if pattern == "" {
		return nil
	}
	if len(pattern) > MaxPatternLength {
		return fmt.Errorf("pattern is too long (max %d characters)", MaxPatternLength)
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("pattern is not a valid regular expression: %w", err)
	}
	return nil
}

// validateFieldText applies the rule every layer that writes to a field
// applies: CR, LF and TAB would be read by the s3270 protocol as actions
// rather than typed as text.
func validateFieldText(value string) error {
	if host.ContainsForbiddenFieldText(value) {
		return fmt.Errorf("value must not contain CR, LF or TAB")
	}
	if len(value) > MaxValueLength {
		return fmt.Errorf("value is too long (max %d characters)", MaxValueLength)
	}
	return nil
}

// supportedAIDKeys is the set of keys a task step may press. It is an
// allow-list rather than a pass-through to the host: a step is data loaded
// from a catalogue file, and s3270 actions are not the same thing as key
// names.
var supportedAIDKeys = func() map[string]string {
	keys := map[string]string{
		"enter":  "Enter",
		"clear":  "Clear",
		"attn":   "Attn",
		"sysreq": "SysReq",
	}
	for i := 1; i <= 24; i++ {
		keys[fmt.Sprintf("pf%d", i)] = fmt.Sprintf("PF%d", i)
	}
	for i := 1; i <= 3; i++ {
		keys[fmt.Sprintf("pa%d", i)] = fmt.Sprintf("PA%d", i)
	}
	return keys
}()

// IsSupportedAIDKey reports whether a step may press this key.
func IsSupportedAIDKey(key string) bool {
	_, ok := supportedAIDKeys[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

// CanonicalAIDKey returns the catalogue's spelling of a key name, so a task
// written with "pf3" presses the same key as one written with "PF3". This is
// the spelling for people and stored tasks; it is not what the host is sent —
// see HostAIDKey.
func CanonicalAIDKey(key string) (string, bool) {
	canonical, ok := supportedAIDKeys[strings.ToLower(strings.TrimSpace(key))]
	return canonical, ok
}

// HostAIDKey returns the s3270 action for a supported key — the string
// SendKey must be given. PF and PA keys are actions of their own with the
// number as an argument ("PF(3)"), and handing s3270 the canonical "PF3"
// instead makes it fall back to Key(PF3), which it refuses as "Nonexistent
// or invalid name". Enter, Clear, Attn and SysReq are actions under their
// canonical names already.
func HostAIDKey(key string) (string, bool) {
	canonical, ok := CanonicalAIDKey(key)
	if !ok {
		return "", false
	}
	// The suffixes are numeric by construction: canonical spellings come
	// from the allow-list above.
	switch {
	case strings.HasPrefix(canonical, "PF"):
		return "PF(" + strings.TrimPrefix(canonical, "PF") + ")", true
	case strings.HasPrefix(canonical, "PA"):
		return "PA(" + strings.TrimPrefix(canonical, "PA") + ")", true
	}
	return canonical, true
}

// Clone returns a deep copy, so a caller cannot mutate a stored task through
// a slice it was handed.
func (t Task) Clone() Task {
	out := t
	out.Parameters = append([]Parameter(nil), t.Parameters...)
	out.Outputs = append([]Output(nil), t.Outputs...)
	if len(t.Steps) > 0 {
		out.Steps = make([]Step, len(t.Steps))
		for i, step := range t.Steps {
			stepCopy := step
			stepCopy.Expect = append([]Expect(nil), step.Expect...)
			stepCopy.Inputs = append([]Input(nil), step.Inputs...)
			out.Steps[i] = stepCopy
		}
	}
	return out
}

// ResolveParameters validates supplied values against the task's parameter
// declarations and returns the map the runner should use. Missing optional
// parameters fall back to their default.
//
// This runs server-side on every run, not only in the browser. The form is a
// convenience; it is not the gate.
func (t Task) ResolveParameters(supplied map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(t.Parameters))
	for _, p := range t.Parameters {
		value, provided := supplied[p.Name]
		value = strings.TrimSpace(value)
		if !provided || value == "" {
			value = p.Default
		}
		if value == "" {
			if p.Required {
				return nil, fmt.Errorf("%s is required", p.DisplayLabel())
			}
			resolved[p.Name] = ""
			continue
		}
		if err := validateFieldText(value); err != nil {
			return nil, fmt.Errorf("%s: %w", p.DisplayLabel(), err)
		}
		if p.MaxLength > 0 && len([]rune(value)) > p.MaxLength {
			return nil, fmt.Errorf("%s must be %d characters or fewer", p.DisplayLabel(), p.MaxLength)
		}
		if p.Pattern != "" {
			re, err := regexp.Compile("^(?:" + p.Pattern + ")$")
			if err != nil {
				return nil, fmt.Errorf("%s: the validation pattern on this task is broken", p.DisplayLabel())
			}
			if !re.MatchString(value) {
				return nil, fmt.Errorf("%s is not in the expected format", p.DisplayLabel())
			}
		}
		resolved[p.Name] = value
	}

	// A value for a parameter the task does not declare is a caller error
	// worth surfacing: it usually means a renamed parameter and a form that
	// was never updated, and silently ignoring it would send the task off
	// with a field unfilled.
	for name := range supplied {
		if _, ok := resolved[name]; !ok {
			return nil, fmt.Errorf("%q is not a parameter of this task", name)
		}
	}
	return resolved, nil
}

// SensitiveParameters returns the names whose values must never be recorded.
func (t Task) SensitiveParameters() map[string]bool {
	out := make(map[string]bool)
	for _, p := range t.Parameters {
		if p.Sensitive {
			out[p.Name] = true
		}
	}
	return out
}
