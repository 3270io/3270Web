package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// Decisions in a recording.
//
// A recording is a straight line of steps: fill this field, press that key,
// check that the screen says what it should. That is enough for a flow whose
// answer is the same every time, and not enough for the flow somebody actually
// has — read the balance, and if it is below the limit take the other path;
// press Enter until the host stops saying MORE; skip the confirmation screen
// on the days the host does not ask for one.
//
// What this file adds is the smallest vocabulary that covers those:
//
//	SetVariable   remember a literal, or the text at a place on the screen
//	If / Else / EndIf     take one path or the other
//	While / EndWhile      repeat while something is still true
//	Stop                  end the run here, and say why
//
// Three rules hold it together, and each is a decision that could have gone
// the other way:
//
//  1. **Flat, not nested.** The blocks are marked by their own steps rather
//     than by nesting steps inside each other, because every reader of a
//     recording — playback, the chaos engine, the tools an assistant is handed,
//     the JSON somebody edits by hand — walks one flat list of steps. Nesting
//     would mean teaching all of them a second shape, and a recording that
//     makes no decisions would still have to be read by the new reader.
//
//  2. **Refused at load, not part-way through a run.** A recording whose
//     blocks do not close, or whose comparison names an operator that does not
//     exist, is rejected when it is loaded — before it has typed anything into
//     a host. Half a flow is worse than none of it.
//
//  3. **A decision that cannot be made stops the run.** Everywhere else,
//     playback logs a failed step and carries on, which is right for a step
//     that acts. It is wrong for a step that decides: carrying on past an `If`
//     whose condition could not be evaluated means guessing which branch the
//     operator wanted, and the wrong guess types real values into a real host.
//     So control-flow failures end the run and say what could not be decided.
const (
	stepSetVariable = "SetVariable"
	stepIf          = "If"
	stepElse        = "Else"
	stepEndIf       = "EndIf"
	stepWhile       = "While"
	stepEndWhile    = "EndWhile"
	stepStop        = "Stop"
)

// The comparisons a condition can make. The names are the canonical spelling;
// the symbolic aliases below are accepted because a person hand-editing the
// JSON reaches for `>=` before `greaterOrEqual`, and refusing that spelling
// would teach nobody anything.
const (
	opEquals         = "equals"
	opNotEquals      = "notequals"
	opContains       = "contains"
	opNotContains    = "notcontains"
	opStartsWith     = "startswith"
	opEndsWith       = "endswith"
	opIsEmpty        = "isempty"
	opIsNotEmpty     = "isnotempty"
	opGreaterThan    = "greaterthan"
	opGreaterOrEqual = "greaterorequal"
	opLessThan       = "lessthan"
	opLessOrEqual    = "lessorequal"
)

// defaultWorkflowLoopLimit bounds a While that does not name its own limit. A
// loop waiting for a host to finish drawing is a handful of turns; a hundred
// is far past that and still stops long before a stuck loop becomes a session
// that has to be killed.
const defaultWorkflowLoopLimit = 100

// maxWorkflowLoopLimit is the ceiling on a step's own MaxIterations. A
// recording may ask for more than the default and may not ask for forever.
const maxWorkflowLoopLimit = 10000

// maxWorkflowStepsExecuted backstops the loop limits. Nested loops multiply,
// and this is the point at which a run is not going to finish and should say
// so rather than occupy a session goroutine indefinitely.
const maxWorkflowStepsExecuted = 100000

// maxWorkflowVariableNameLength keeps a variable name to something a report
// line and an error message can carry.
const maxWorkflowVariableNameLength = 64

// isControlFlowStepType reports whether a step decides rather than acts.
// Control-flow steps never reach the host.
func isControlFlowStepType(stepType string) bool {
	switch canonicalWorkflowStepType(stepType) {
	case stepSetVariable, stepIf, stepElse, stepEndIf, stepWhile, stepEndWhile, stepStop:
		return true
	}
	return false
}

// canonicalWorkflowStepType folds the spelling of a control-flow step name.
// Step types elsewhere in a recording are matched exactly, so this deals only
// in the names this file introduces and hands anything else back untouched.
func canonicalWorkflowStepType(stepType string) string {
	switch strings.ToLower(strings.TrimSpace(stepType)) {
	case "setvariable", "set":
		return stepSetVariable
	case "if":
		return stepIf
	case "else":
		return stepElse
	case "endif", "end if":
		return stepEndIf
	case "while":
		return stepWhile
	case "endwhile", "end while":
		return stepEndWhile
	case "stop":
		return stepStop
	}
	return strings.TrimSpace(stepType)
}

// workflowProgram is a recording with its blocks resolved: which step an `If`
// jumps to when its condition is false, which `While` an `EndWhile` returns
// to, and so on. Resolving it once at load means playback never searches for a
// matching block while a host is waiting.
type workflowProgram struct {
	steps []session.WorkflowStep
	// ifFalse maps an If to the step that runs when its condition is false:
	// the step after its Else, or the step after its EndIf when it has none.
	ifFalse map[int]int
	// elseEnd maps an Else to the step after its EndIf — where the
	// then-branch goes when it runs off the end of itself.
	elseEnd map[int]int
	// whileFalse maps a While to the step after its EndWhile.
	whileFalse map[int]int
	// whileStart maps an EndWhile back to its While.
	whileStart map[int]int
	// hasControlFlow records whether any step decides. A recording with none
	// behaves exactly as it did before this file existed.
	hasControlFlow bool
}

type workflowBlockFrame struct {
	kind      string
	index     int
	elseIndex int
}

// compileWorkflowSteps resolves the block structure of a recording and checks
// every control-flow step it holds. The error it returns is written for the
// person who has to fix the file: it names the step number, because that is
// what they are looking at.
func compileWorkflowSteps(steps []session.WorkflowStep) (*workflowProgram, error) {
	program := &workflowProgram{
		steps:      steps,
		ifFalse:    make(map[int]int),
		elseEnd:    make(map[int]int),
		whileFalse: make(map[int]int),
		whileStart: make(map[int]int),
	}
	var stack []workflowBlockFrame

	for i, step := range steps {
		stepType := canonicalWorkflowStepType(step.Type)
		if isControlFlowStepType(stepType) {
			program.hasControlFlow = true
			if err := validateControlFlowStep(i, stepType, step); err != nil {
				return nil, err
			}
		}
		switch stepType {
		case stepIf:
			stack = append(stack, workflowBlockFrame{kind: stepIf, index: i, elseIndex: -1})
		case stepWhile:
			stack = append(stack, workflowBlockFrame{kind: stepWhile, index: i, elseIndex: -1})
		case stepElse:
			if len(stack) == 0 || stack[len(stack)-1].kind != stepIf {
				return nil, fmt.Errorf("step %d: Else without an If to belong to", i+1)
			}
			frame := &stack[len(stack)-1]
			if frame.elseIndex >= 0 {
				return nil, fmt.Errorf("step %d: a second Else for the If at step %d", i+1, frame.index+1)
			}
			frame.elseIndex = i
		case stepEndIf:
			if len(stack) == 0 || stack[len(stack)-1].kind != stepIf {
				return nil, fmt.Errorf("step %d: EndIf without an If to close", i+1)
			}
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if frame.elseIndex >= 0 {
				program.ifFalse[frame.index] = frame.elseIndex + 1
				program.elseEnd[frame.elseIndex] = i + 1
			} else {
				program.ifFalse[frame.index] = i + 1
			}
		case stepEndWhile:
			if len(stack) == 0 || stack[len(stack)-1].kind != stepWhile {
				return nil, fmt.Errorf("step %d: EndWhile without a While to close", i+1)
			}
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			program.whileFalse[frame.index] = i + 1
			program.whileStart[i] = frame.index
		}
	}

	if len(stack) > 0 {
		frame := stack[len(stack)-1]
		closer := "EndIf"
		if frame.kind == stepWhile {
			closer = "EndWhile"
		}
		return nil, fmt.Errorf("step %d: %s is never closed by a matching %s", frame.index+1, frame.kind, closer)
	}
	return program, nil
}

// validateControlFlowStep checks one decision step in isolation. Everything it
// refuses is something that could otherwise only be discovered with a host on
// the other end of the connection.
func validateControlFlowStep(index int, stepType string, step session.WorkflowStep) error {
	position := index + 1
	switch stepType {
	case stepSetVariable:
		if err := validateWorkflowVariableName(step.Variable); err != nil {
			return fmt.Errorf("step %d: SetVariable %v", position, err)
		}
		if step.Coordinates != nil {
			if err := validateWorkflowRegion(step.Coordinates); err != nil {
				return fmt.Errorf("step %d: SetVariable %v", position, err)
			}
		}
	case stepIf, stepWhile:
		if step.Variable == "" && step.Coordinates == nil {
			return fmt.Errorf("step %d: %s compares nothing — give it either a Variable to read or Coordinates on the screen", position, stepType)
		}
		if step.Variable != "" && step.Coordinates != nil {
			return fmt.Errorf("step %d: %s names both a Variable and Coordinates, and only one of them can be the value being compared", position, stepType)
		}
		if step.Variable != "" {
			if err := validateWorkflowVariableName(step.Variable); err != nil {
				return fmt.Errorf("step %d: %s %v", position, stepType, err)
			}
		}
		if step.Coordinates != nil {
			if err := validateWorkflowRegion(step.Coordinates); err != nil {
				return fmt.Errorf("step %d: %s %v", position, stepType, err)
			}
		}
		if _, ok := canonicalWorkflowOperator(step.Operator); !ok {
			return fmt.Errorf("step %d: %s has no comparison this build understands (%q) — the operators are %s", position, stepType, step.Operator, workflowOperatorList())
		}
		if stepType == stepWhile {
			if step.MaxIterations < 0 {
				return fmt.Errorf("step %d: While cannot repeat a negative number of times", position)
			}
			if step.MaxIterations > maxWorkflowLoopLimit {
				return fmt.Errorf("step %d: While may repeat at most %d times, and this asks for %d", position, maxWorkflowLoopLimit, step.MaxIterations)
			}
		}
	case stepElse, stepEndIf, stepEndWhile, stepStop:
		// Nothing of their own to check: Else, EndIf and EndWhile are
		// checked by the block structure, and Stop carries only the reason
		// it prints.
	}
	return nil
}

// validateWorkflowRegion checks that a screen region is on a screen. The
// length matters as much as the position: a region with no length is a
// comparison against the empty string, which would quietly be true for
// `isEmpty` and false for everything else.
func validateWorkflowRegion(coords *session.WorkflowCoordinates) error {
	if coords.Row <= 0 || coords.Column <= 0 {
		return errors.New("names a row and column counted from 1, and this names something off the screen")
	}
	if coords.Length <= 0 {
		return errors.New("reads a region of the screen and needs a Length to say how much of it")
	}
	return nil
}

func validateWorkflowVariableName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("needs a variable name")
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf("variable name %q has space around it", name)
	}
	if len(name) > maxWorkflowVariableNameLength {
		return fmt.Errorf("variable name is longer than %d characters", maxWorkflowVariableNameLength)
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return fmt.Errorf("variable name %q is not a letter followed by letters, digits or underscores", name)
		}
	}
	return nil
}

// canonicalWorkflowOperator folds an operator to its canonical name, accepting
// the symbolic spellings somebody editing JSON by hand will reach for.
func canonicalWorkflowOperator(operator string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(operator)) {
	case opEquals, "==", "=", "is":
		return opEquals, true
	case opNotEquals, "!=", "<>", "isnot":
		return opNotEquals, true
	case opContains:
		return opContains, true
	case opNotContains, "doesnotcontain":
		return opNotContains, true
	case opStartsWith:
		return opStartsWith, true
	case opEndsWith:
		return opEndsWith, true
	case opIsEmpty, "empty":
		return opIsEmpty, true
	case opIsNotEmpty, "notempty":
		return opIsNotEmpty, true
	case opGreaterThan, ">":
		return opGreaterThan, true
	case opGreaterOrEqual, ">=":
		return opGreaterOrEqual, true
	case opLessThan, "<":
		return opLessThan, true
	case opLessOrEqual, "<=":
		return opLessOrEqual, true
	}
	return "", false
}

func workflowOperatorList() string {
	return "equals, notEquals, contains, notContains, startsWith, endsWith, isEmpty, isNotEmpty, greaterThan, greaterOrEqual, lessThan, lessOrEqual"
}

// workflowVariables is what a run has read or been told so far.
type workflowVariables map[string]string

// workflowRunState is one run's memory: what it knows, which of that it must
// not write down, and how many passes each loop has made.
type workflowRunState struct {
	vars      workflowVariables
	sensitive map[string]bool
	loops     map[int]int
}

func newWorkflowRunState() *workflowRunState {
	return &workflowRunState{
		vars:      workflowVariables{},
		sensitive: map[string]bool{},
		loops:     map[int]int{},
	}
}

// loggable renders a value for the run log: masked if the step that set it
// said so, and shortened if it is longer than a log line should carry.
func (r *workflowRunState) loggable(name, value string) string {
	if r.sensitive[name] {
		return "(hidden)"
	}
	const limit = 80
	runes := []rune(value)
	if len(runes) > limit {
		return fmt.Sprintf("%q… (%d characters)", string(runes[:limit]), len(runes))
	}
	return fmt.Sprintf("%q", value)
}

// substituteWorkflowVariables replaces ${name} with what the run knows.
//
// A name the run has not set is an error rather than an empty string. The
// empty string is a perfectly good field value, so substituting one would type
// nothing into a field and press Enter, and the flow would fail somewhere
// later with no sign of where it went wrong. `$${` types a literal `${`, for
// the rare screen that wants those two characters.
func substituteWorkflowVariables(text string, vars workflowVariables) (string, error) {
	if !strings.Contains(text, "$") {
		return text, nil
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '$' {
			b.WriteByte(text[i])
			i++
			continue
		}
		if strings.HasPrefix(text[i:], "$${") {
			b.WriteString("${")
			i += 3
			continue
		}
		if !strings.HasPrefix(text[i:], "${") {
			b.WriteByte(text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i:], '}')
		if end < 0 {
			return "", fmt.Errorf("${ with no closing brace in %q", text)
		}
		name := text[i+2 : i+end]
		if err := validateWorkflowVariableName(name); err != nil {
			return "", fmt.Errorf("${%s}: %v", name, err)
		}
		value, ok := vars[name]
		if !ok {
			return "", fmt.Errorf("no variable named %q has been set by this point in the recording", name)
		}
		b.WriteString(value)
		i += end + 1
	}
	return b.String(), nil
}

// screenRegionText reads a run of characters from a position, wrapping at the
// right edge the way the 3270 buffer itself does.
//
// A null in the buffer comes back as a blank, because that is what it is on
// the display: an unwritten cell of a 3270 screen shows as one, and a
// condition compares what the operator can see.
func screenRegionText(screen *host.Screen, row, column, length int) (string, error) {
	if screen == nil {
		return "", errors.New("screen is unavailable")
	}
	if row <= 0 || column <= 0 {
		return "", fmt.Errorf("coordinates are counted from 1: row=%d col=%d", row, column)
	}
	startRow := row - 1
	startCol := column - 1
	if startRow >= screen.Height || startCol >= screen.Width {
		return "", fmt.Errorf("coordinates out of bounds: row=%d col=%d", row, column)
	}
	if length <= 0 {
		return "", nil
	}
	endOffset := length - 1
	endRow := startRow
	endCol := startCol + endOffset
	if screen.Width > 0 {
		endRow += endCol / screen.Width
		endCol = endCol % screen.Width
	}
	if endRow >= screen.Height {
		return "", fmt.Errorf("range out of bounds: row=%d col=%d length=%d", row, column, length)
	}
	return strings.ReplaceAll(screen.Substring(startCol, startRow, endCol, endRow), "\x00", " "), nil
}

// evaluateWorkflowCondition answers the question an If or a While asks.
//
// The left-hand side is either a variable the run has set or a region of the
// screen; a region is trimmed of the spaces a 3270 field is padded with,
// because a comparison against "APPROVED" should not depend on how wide the
// field happens to be.
func evaluateWorkflowCondition(step session.WorkflowStep, vars workflowVariables, screen *host.Screen) (bool, error) {
	operator, ok := canonicalWorkflowOperator(step.Operator)
	if !ok {
		return false, fmt.Errorf("no comparison this build understands (%q)", step.Operator)
	}

	var left string
	switch {
	case step.Variable != "":
		value, known := vars[step.Variable]
		if !known {
			return false, fmt.Errorf("no variable named %q has been set by this point in the recording", step.Variable)
		}
		left = value
	case step.Coordinates != nil:
		text, err := screenRegionText(screen, step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
		if err != nil {
			return false, err
		}
		left = strings.TrimSpace(text)
	default:
		return false, errors.New("compares nothing")
	}

	right, err := substituteWorkflowVariables(step.Text, vars)
	if err != nil {
		return false, err
	}

	switch operator {
	case opIsEmpty:
		return strings.TrimSpace(left) == "", nil
	case opIsNotEmpty:
		return strings.TrimSpace(left) != "", nil
	}

	if step.IgnoreCase {
		left = strings.ToUpper(left)
		right = strings.ToUpper(right)
	}

	switch operator {
	case opEquals:
		return left == right, nil
	case opNotEquals:
		return left != right, nil
	case opContains:
		return strings.Contains(left, right), nil
	case opNotContains:
		return !strings.Contains(left, right), nil
	case opStartsWith:
		return strings.HasPrefix(left, right), nil
	case opEndsWith:
		return strings.HasSuffix(left, right), nil
	}

	leftNumber, okLeft := parseWorkflowNumber(left)
	rightNumber, okRight := parseWorkflowNumber(right)
	if !okLeft || !okRight {
		which := left
		if okLeft {
			which = right
		}
		return false, fmt.Errorf("%s compares numbers and %q is not one", operator, which)
	}
	switch operator {
	case opGreaterThan:
		return leftNumber > rightNumber, nil
	case opGreaterOrEqual:
		return leftNumber >= rightNumber, nil
	case opLessThan:
		return leftNumber < rightNumber, nil
	case opLessOrEqual:
		return leftNumber <= rightNumber, nil
	}
	return false, fmt.Errorf("unhandled comparison %q", operator)
}

// parseWorkflowNumber reads a number the way a green screen writes one:
// grouped with commas, and negative either by a leading sign, a trailing one,
// or a pair of brackets. Anything else is refused rather than guessed at — a
// value that is not a number is a screen that did not say what the recording
// expected, and comparing it as zero would hide that.
func parseWorkflowNumber(text string) (float64, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0, false
	}
	negative := false
	if strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")") {
		negative = true
		trimmed = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	}
	if strings.HasSuffix(trimmed, "-") {
		negative = !negative
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "-"))
	} else if strings.HasSuffix(trimmed, "CR") {
		// The other way a host writes a credit balance.
		negative = !negative
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "CR"))
	}
	trimmed = strings.ReplaceAll(trimmed, ",", "")
	if trimmed == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	if negative {
		value = -value
	}
	return value, true
}

// describeWorkflowCondition renders a condition the way the run log should
// carry it, so somebody reading the events afterwards can see what was
// compared without opening the file.
func describeWorkflowCondition(step session.WorkflowStep) string {
	left := ""
	switch {
	case step.Variable != "":
		left = step.Variable
	case step.Coordinates != nil:
		left = fmt.Sprintf("row %d col %d len %d", step.Coordinates.Row, step.Coordinates.Column, step.Coordinates.Length)
	default:
		left = "?"
	}
	operator, ok := canonicalWorkflowOperator(step.Operator)
	if !ok {
		operator = step.Operator
	}
	switch operator {
	case opIsEmpty, opIsNotEmpty:
		return fmt.Sprintf("%s %s", left, operator)
	}
	return fmt.Sprintf("%s %s %q", left, operator, step.Text)
}
