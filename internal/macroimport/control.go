// SPDX-License-Identifier: AGPL-3.0-or-later

package macroimport

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jnnngs/3270Web/internal/session"
)

// Blocks and conditions.
//
// A macro is not a straight line. It reads the balance and takes one path or
// the other; it presses a key until the host stops saying MORE; it skips the
// confirmation panel on the days the host does not draw one. A recording can
// do all three — see the If, While and SetVariable steps — so the question is
// no longer whether the shape exists on this side, but whether the file says
// enough for the translation to be a translation rather than a guess.
//
// It says enough in exactly two places, and nowhere else:
//
//   - **Which lines are inside the branch.** The language brackets its own
//     blocks. `If … Then` runs to `End If`, `Do While …` to `Loop`, and the
//     nesting between them is the file's own structure rather than an
//     inference about the host. Reading it is reading the file.
//
//   - **What the condition compares.** `Session.Screen.GetString(5, 20, 8)`
//     names a row, a column and a length, which is precisely what an `If`
//     step needs. A condition that instead calls a function, concatenates two
//     things, or searches the whole screen names none of that, and no amount
//     of care turns it into coordinates.
//
// So the first is read and the second is required. What holds the two
// together is the rule this file exists for:
//
//	A branch whose condition did not translate takes its body with it.
//
// The alternative is the worst outcome available: the `If` is dropped, the
// statements inside it survive, and steps that ran on some days now run on
// every day — against a live host, confidently, with a report that said the
// branch needed a human but not that its contents had escaped it. One note
// naming the branch and the number of statements left with it is worth more
// than a hundred steps nobody asked to be unconditional.
//
// The same rule is why a loop with an `Exit Do` in it does not translate: a
// recording's `While` has no way out but its own condition, so a translated
// loop would keep going where the macro left. And it is why a file that jumps
// with `GoTo` gets no blocks at all — where a jump lands is not something a
// list of steps can say, and a branch translated around one is a branch whose
// boundaries are fiction.

// blockRole is what a line does to the shape of the file, as opposed to what
// it asks the terminal to do.
type blockRole int

const (
	roleStatement blockRole = iota
	roleOpenIf
	roleInlineIf
	roleElseIf
	roleElse
	roleCloseIf
	roleOpenLoop
	roleCloseLoop
	roleOpenOpaque
	roleCloseOpaque
	roleExitFlow
	roleExitLoop
	roleJump
)

// The kinds of block, named as the file names them. A closer has to match the
// kind of the opener it claims to close: a `Wend` does not end a `Do`, and a
// file where it appears to is a file whose structure this importer will not
// pretend to have understood.
const (
	kindIf     = "If"
	kindDo     = "Do"
	kindWhile  = "While"
	kindFor    = "For"
	kindSelect = "Select Case"
)

// maxScreenRegionLength bounds the length of a screen read, at the largest
// 3270 model's worth of characters.
const maxScreenRegionLength = 62 * 160

func closerFor(kind string) string {
	switch kind {
	case kindIf:
		return "End If"
	case kindDo:
		return "Loop"
	case kindWhile:
		return "Wend"
	case kindFor:
		return "Next"
	case kindSelect:
		return "End Select"
	}
	return "its closing statement"
}

func closingStepFor(kind string) string {
	if kind == kindIf {
		return "EndIf"
	}
	return "EndWhile"
}

// lineForm is one line's structural reading: what it opens or closes, the
// condition it tests, and — for the blocks that cannot translate whatever
// their condition says — the reason to report.
type lineForm struct {
	role      blockRole
	kind      string
	condition string
	// negate is set by `Do Until`, which tests for the opposite of what a
	// While does.
	negate bool
	// then and otherwise carry the statements of a single-line `If … Then …`.
	then      string
	otherwise string
	reason    string
}

// classify reads one logical line's structural role. It is deliberately the
// only place that decides what opens and closes a block, because the
// structural pass and the translating pass have to agree exactly: a closer
// one of them sees and the other does not would misplace every block after
// it.
func classify(text string) lineForm {
	words := topLevelWords(text)
	if len(words) == 0 {
		return lineForm{}
	}
	switch words[0].lower {
	case "if":
		ti := wordAt(words, "then", 1)
		if ti < 0 {
			// An `If` with no `Then` on the same line is not a shape this
			// language writes; the ordinary reporting path names it.
			return lineForm{}
		}
		condition := strings.TrimSpace(text[words[0].end:words[ti].start])
		tail := strings.TrimSpace(text[words[ti].end:])
		if tail == "" {
			return lineForm{role: roleOpenIf, kind: kindIf, condition: condition}
		}
		form := lineForm{role: roleInlineIf, kind: kindIf, condition: condition, then: tail}
		tailWords := topLevelWords(tail)
		if ei := wordAt(tailWords, "else", 0); ei >= 0 {
			form.then = strings.TrimSpace(tail[:tailWords[ei].start])
			form.otherwise = strings.TrimSpace(tail[tailWords[ei].end:])
		}
		return form
	case "elseif":
		ti := wordAt(words, "then", 1)
		if ti < 0 {
			return lineForm{}
		}
		return lineForm{
			role:      roleElseIf,
			kind:      kindIf,
			condition: strings.TrimSpace(text[words[0].end:words[ti].start]),
		}
	case "else":
		return lineForm{role: roleElse, kind: kindIf, then: strings.TrimSpace(text[words[0].end:])}
	case "end":
		if len(words) > 1 {
			switch words[1].lower {
			case "if":
				return lineForm{role: roleCloseIf, kind: kindIf}
			case "select":
				return lineForm{role: roleCloseOpaque, kind: kindSelect}
			}
		}
		return lineForm{}
	case "wend":
		return lineForm{role: roleCloseLoop, kind: kindWhile}
	case "loop":
		// `Loop`, `Loop While …` and `Loop Until …` all close a `Do`. The
		// post-test forms test after the body has already run, which is not
		// what a While does, and the `Do` they belong to was opened as
		// untranslatable for exactly that reason.
		return lineForm{role: roleCloseLoop, kind: kindDo}
	case "do":
		if len(words) > 1 && (words[1].lower == "while" || words[1].lower == "until") {
			return lineForm{
				role:      roleOpenLoop,
				kind:      kindDo,
				condition: strings.TrimSpace(text[words[1].end:]),
				negate:    words[1].lower == "until",
			}
		}
		return lineForm{role: roleOpenOpaque, kind: kindDo, reason: reasonPostTestLoop}
	case "while":
		return lineForm{role: roleOpenLoop, kind: kindWhile, condition: strings.TrimSpace(text[words[0].end:])}
	case "for":
		return lineForm{role: roleOpenOpaque, kind: kindFor, reason: reasonCountedLoop}
	case "next":
		return lineForm{role: roleCloseOpaque, kind: kindFor}
	case "select":
		return lineForm{role: roleOpenOpaque, kind: kindSelect, reason: reasonSelectCase}
	case "exit":
		if len(words) > 1 {
			switch words[1].lower {
			case "sub", "function":
				return lineForm{role: roleExitFlow}
			case "do", "while", "for":
				return lineForm{role: roleExitLoop}
			}
		}
		return lineForm{}
	case "goto", "gosub", "resume", "return", "on":
		return lineForm{role: roleJump}
	}
	return lineForm{}
}

/* -------------------------------------------------------------------- */
/* The structural pass                                                   */
/* -------------------------------------------------------------------- */

// plannedBlock is what the structural pass learned about one block before a
// single step was emitted: whether it closes at all, and whether anything
// inside it leaves early.
type plannedBlock struct {
	kind    string
	closed  bool
	hasExit bool
}

// blockPlan is the whole file's structure, read before any of it is
// translated. The opener of a block has to know what is inside it — an
// `Exit Do` three lines down decides whether the loop can translate at all —
// and the opener is the line that arrives first.
type blockPlan struct {
	blocks map[int]*plannedBlock
	// jumps records that the file transfers control somewhere a list of steps
	// cannot follow. It disqualifies every block in the file rather than the
	// one holding the jump: skipping the block a `GoTo` sits in removes the
	// jump but not the lines it was jumping over.
	jumps bool
	// routines counts the subroutines and functions declared in the file. A
	// recording is one flow, and `Exit Sub` means something different in a
	// file that has several of them.
	routines int
}

func planBlocks(lines []line) *blockPlan {
	plan := &blockPlan{blocks: make(map[int]*plannedBlock)}
	var stack []int
	for i, l := range lines {
		if l.Text == "" {
			continue
		}
		if isScaffolding(l.Text) {
			if isRoutineDeclaration(l.Text) {
				plan.routines++
			}
			continue
		}
		form := classify(l.Text)
		switch form.role {
		case roleOpenIf, roleOpenLoop, roleOpenOpaque:
			plan.blocks[i] = &plannedBlock{kind: form.kind}
			stack = append(stack, i)
		case roleCloseIf, roleCloseLoop, roleCloseOpaque:
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			if plan.blocks[top].kind != form.kind {
				// The blocks have come apart. Leaving the opener on the stack
				// leaves it unclosed, which is what disqualifies it and
				// everything still open around it — the safe reading of a file
				// whose structure does not add up.
				continue
			}
			plan.blocks[top].closed = true
			stack = stack[:len(stack)-1]
		case roleExitLoop:
			plan.markEnclosingLoop(stack)
		case roleJump:
			plan.jumps = true
		}
	}
	return plan
}

// markEnclosingLoop records that the innermost loop still open leaves early.
// A recording's While has no way out but its own condition, so a loop holding
// an `Exit Do` would keep going where the macro stopped — and a loop that runs
// on past its exit is a flow doing something nobody asked for, which is worse
// than a loop reported as untranslated.
func (p *blockPlan) markEnclosingLoop(stack []int) {
	for j := len(stack) - 1; j >= 0; j-- {
		switch p.blocks[stack[j]].kind {
		case kindDo, kindWhile, kindFor:
			p.blocks[stack[j]].hasExit = true
			return
		}
	}
}

// isRoutineDeclaration reports whether a scaffolding line declares a routine.
// The declarations themselves carry nothing, but how many there are decides
// what `Exit Sub` can mean.
func isRoutineDeclaration(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, prefix := range []string{"sub ", "function ", "private sub ", "public sub ", "private function ", "public function "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

/* -------------------------------------------------------------------- */
/* Conditions                                                            */
/* -------------------------------------------------------------------- */

// condition turns the test in an `If`, an `ElseIf` or a `While` into the
// comparison a recording step makes, or says why it will not.
//
// A recording compares one thing — a place on the screen, or a variable the
// run has set — against one value. That is less than the language can write,
// and the gap is the point: a condition calling a function or joining two
// tests with And is one whose meaning lives outside this file, and a branch
// translated from a condition nobody could read is a branch that takes the
// wrong path with complete confidence.
func (t *translator) condition(expr string, negate bool) (session.WorkflowStep, string) {
	expr = stripOuterParens(strings.TrimSpace(expr))
	if expr == "" {
		return session.WorkflowStep{}, reasonConditionShape
	}
	for _, w := range topLevelWords(expr) {
		switch w.lower {
		case "and", "or", "not", "xor", "eqv", "imp":
			return session.WorkflowStep{}, reasonConditionCombined
		}
	}
	idx, symbol := topLevelComparison(expr)
	if idx < 0 {
		return session.WorkflowStep{}, reasonConditionShape
	}
	left := strings.TrimSpace(expr[:idx])
	right := strings.TrimSpace(expr[idx+len(symbol):])

	step, value, ok := t.comparison(left, right)
	if !ok {
		// The other way round means the same thing with the comparison
		// mirrored: `"READY" = GetString(…)` is the condition somebody wrote
		// when they wrote it that way.
		step, value, ok = t.comparison(right, left)
		if !ok {
			return session.WorkflowStep{}, reasonConditionShape
		}
		symbol = mirrorComparison(symbol)
	}
	operator, known := operatorForSymbol(symbol)
	if !known {
		return session.WorkflowStep{}, reasonConditionShape
	}
	if negate {
		operator = negatedOperator(operator)
	}
	step.Operator = operator
	step.Text = value
	if step.Coordinates != nil {
		t.sawScreenCondition = true
	}
	return step, ""
}

// comparison resolves the two sides: one of them has to be something a
// recording can read, and the other something it can hold.
func (t *translator) comparison(read, value string) (session.WorkflowStep, string, bool) {
	step, ok := t.readableSide(read)
	if !ok {
		return session.WorkflowStep{}, "", false
	}
	held, ok := t.valueSide(value)
	if !ok {
		return session.WorkflowStep{}, "", false
	}
	return step, held, true
}

// readableSide is the side a condition reads: a place on the screen, or a
// variable this file has already set to something readable.
func (t *translator) readableSide(expr string) (session.WorkflowStep, bool) {
	if row, column, length, ok := screenRead(expr); ok {
		return session.WorkflowStep{
			Coordinates: &session.WorkflowCoordinates{Row: row, Column: column, Length: length},
		}, true
	}
	if name, ok := t.knownVariable(expr); ok {
		return session.WorkflowStep{Variable: name}, true
	}
	return session.WorkflowStep{}, false
}

// valueSide is the side a condition compares against: a literal, or a
// variable, which becomes the ${name} a recording resolves when it runs.
func (t *translator) valueSide(expr string) (string, bool) {
	if text, ok := stringLiteral(expr); ok {
		return text, true
	}
	if number, ok := numberLiteral(expr); ok {
		return number, true
	}
	if name, ok := t.knownVariable(expr); ok {
		return "${" + name + "}", true
	}
	return "", false
}

// knownVariable reports whether an expression is a plain variable name this
// file has set to something a recording can read. A name it has not set is not
// one: the value would come from wherever the macro got it, which is outside
// this file, and a condition against a variable that never gets a value is a
// run that stops on the host rather than a flow that works.
func (t *translator) knownVariable(expr string) (string, bool) {
	name := strings.TrimSpace(expr)
	if !isVariableName(name) {
		return "", false
	}
	if _, ok := t.vars[name]; !ok {
		return "", false
	}
	return name, true
}

// screenRead reads `Session.Screen.GetString(row, column, length)` — the one
// expression in these files that names a place on the screen precisely enough
// for a recording step to read the same place.
func screenRead(expr string) (row, column, length int, ok bool) {
	head, rawArgs, split := splitCall(strings.TrimSpace(expr))
	if !split {
		return 0, 0, 0, false
	}
	switch strings.ToLower(lastSegment(head)) {
	case "getstring", "gettext", "getdata", "readscreen":
	default:
		return 0, 0, 0, false
	}
	args, literal := parseArgs(rawArgs)
	if !literal || len(args) != 3 {
		return 0, 0, 0, false
	}
	for _, a := range args {
		if !a.isNum {
			return 0, 0, 0, false
		}
	}
	row, column, length = args[0].num, args[1].num, args[2].num
	if !validPosition(row, column) || length < 1 || length > maxScreenRegionLength {
		return 0, 0, 0, false
	}
	return row, column, length, true
}

// valueExpression reads the right-hand side of an assignment into the step
// that remembers it.
func (t *translator) valueExpression(expr string) (session.WorkflowStep, bool) {
	if row, column, length, ok := screenRead(expr); ok {
		return session.WorkflowStep{
			Coordinates: &session.WorkflowCoordinates{Row: row, Column: column, Length: length},
		}, true
	}
	if text, ok := t.valueSide(expr); ok {
		return session.WorkflowStep{Text: text}, true
	}
	return session.WorkflowStep{}, false
}

func stringLiteral(expr string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if len(expr) < 2 || !strings.HasPrefix(expr, `"`) || !strings.HasSuffix(expr, `"`) {
		return "", false
	}
	inner := expr[1 : len(expr)-1]
	// A doubled quote inside a string is one quote, the way the language
	// writes it; a lone one means the literal ended and something else
	// followed, which is an expression rather than a value.
	if strings.Contains(strings.ReplaceAll(inner, `""`, ""), `"`) {
		return "", false
	}
	return strings.ReplaceAll(inner, `""`, `"`), true
}

// numberLiteral keeps the number as the file wrote it. A recording compares
// numbers by reading them, so 1000.50 stays 1000.50 rather than becoming
// whatever a round trip through a float would print.
func numberLiteral(expr string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", false
	}
	if _, err := strconv.ParseFloat(expr, 64); err != nil {
		return "", false
	}
	return expr, true
}

// isVariableName holds a name to what a recording will accept for one, which
// is a letter followed by letters, digits or underscores. The type-suffixed
// names the language allows — `total$`, `count%` — are not among them, and a
// recording that carried one would be refused when it was loaded.
func isVariableName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// operatorForSymbol names the comparison a recording step asks for. The
// canonical names go into the file rather than the symbols, because the
// recording is read by people as often as by playback.
func operatorForSymbol(symbol string) (string, bool) {
	switch symbol {
	case "=":
		return "equals", true
	case "<>", "!=":
		return "notEquals", true
	case ">":
		return "greaterThan", true
	case ">=":
		return "greaterOrEqual", true
	case "<":
		return "lessThan", true
	case "<=":
		return "lessOrEqual", true
	}
	return "", false
}

// mirrorComparison turns a comparison round, for the condition written with
// its value first.
func mirrorComparison(symbol string) string {
	switch symbol {
	case ">":
		return "<"
	case ">=":
		return "<="
	case "<":
		return ">"
	case "<=":
		return ">="
	}
	return symbol
}

// negatedOperator is what `Do Until` becomes: a While testing the opposite.
// Every one of these is an exact opposite rather than an approximation, which
// is why `Until` translates at all.
func negatedOperator(operator string) string {
	switch operator {
	case "equals":
		return "notEquals"
	case "notEquals":
		return "equals"
	case "greaterThan":
		return "lessOrEqual"
	case "greaterOrEqual":
		return "lessThan"
	case "lessThan":
		return "greaterOrEqual"
	case "lessOrEqual":
		return "greaterThan"
	}
	return operator
}

/* -------------------------------------------------------------------- */
/* Reading expressions                                                   */
/* -------------------------------------------------------------------- */

type wordSpan struct {
	start, end int
	lower      string
}

// topLevelWords lists the words of a line that stand outside any string and
// outside any bracket. A keyword inside either is not a keyword: `Then` in
// "PRESS THEN ENTER" is text the host is going to be shown.
func topLevelWords(text string) []wordSpan {
	var out []wordSpan
	inQuote := false
	depth := 0
	for i := 0; i < len(text); {
		c := text[i]
		if c == '"' {
			inQuote = !inQuote
			i++
			continue
		}
		if inQuote {
			i++
			continue
		}
		switch c {
		case '(':
			depth++
			i++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if isWordByte(c) {
			j := i
			for j < len(text) && isWordByte(text[j]) {
				j++
			}
			if depth == 0 {
				out = append(out, wordSpan{start: i, end: j, lower: strings.ToLower(text[i:j])})
			}
			i = j
			continue
		}
		i++
	}
	return out
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// wordAt finds a keyword among the words of a line, from the given index on.
func wordAt(words []wordSpan, word string, from int) int {
	for i := from; i < len(words); i++ {
		if words[i].lower == word {
			return i
		}
	}
	return -1
}

// topLevelComparison finds the comparison a condition makes, ignoring
// anything inside a string or inside the brackets of a call.
func topLevelComparison(expr string) (int, string) {
	inQuote := false
	depth := 0
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch c {
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth != 0 {
			continue
		}
		switch c {
		case '<':
			if i+1 < len(expr) && (expr[i+1] == '>' || expr[i+1] == '=') {
				return i, expr[i : i+2]
			}
			return i, "<"
		case '>':
			if i+1 < len(expr) && expr[i+1] == '=' {
				return i, ">="
			}
			return i, ">"
		case '=':
			return i, "="
		case '!':
			if i+1 < len(expr) && expr[i+1] == '=' {
				return i, "!="
			}
		}
	}
	return -1, ""
}

// stripOuterParens removes brackets that wrap the whole expression, so
// `(x = "Y")` reads as the comparison it is.
func stripOuterParens(expr string) string {
	for len(expr) >= 2 && strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		depth := 0
		wraps := true
		inQuote := false
		for i := 0; i < len(expr); i++ {
			switch expr[i] {
			case '"':
				inQuote = !inQuote
			case '(':
				if !inQuote {
					depth++
				}
			case ')':
				if !inQuote {
					depth--
					if depth == 0 && i != len(expr)-1 {
						wraps = false
					}
				}
			}
		}
		if !wraps || depth != 0 {
			return expr
		}
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	return expr
}

// assignmentIndex reports where the `=` of an assignment is, or -1.
func assignmentIndex(text string) int {
	inQuote := false
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '"':
			inQuote = !inQuote
		case '(':
			if !inQuote {
				return -1
			}
		case '=':
			if !inQuote && i > 0 {
				return i
			}
		}
	}
	return -1
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func statementsPhrase(n int) string {
	return fmt.Sprintf("%d %s", n, plural(n, "statement", "statements"))
}
