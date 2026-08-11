// Package macroimport translates a terminal-emulator macro file into a
// 3270Web recording.
//
// An organisation with three hundred recorded macros has three hundred
// reasons not to move. This package is the answer to that: it reads the macro
// file an installed emulator runs and produces the JSON that
// [workflow playback] already replays, plus a report of every line it could
// not translate and why.
//
// # The dialect
//
// Macro files in this category are written in a BASIC-family scripting
// language against a session automation object model — statements like
// `Session.Screen.PutString "USER01", 5, 20` and
// `Session.Screen.SendKeys "<Enter>"`. The object names vary between the
// products that write these files (`Session`, `Sess0`, a variable somebody
// chose), the method names are shared and the argument order with them, so
// the parser ignores the object path entirely and matches on the method.
// Parentheses are optional, as they are in the language itself.
//
// # Two rules
//
// Everything else here follows from these:
//
//  1. It never guesses. A statement that cannot be translated faithfully is
//     reported with its line number and a reason, not approximated. A partial
//     conversion that says which twelve lines need a human is worth more than
//     a confident-looking one that quietly drops four of them — the second
//     kind is discovered in production, by the flow doing something nobody
//     asked it to.
//
//  2. It stays inside the published recording schema. No step type and no
//     field that playback, the chaos engine and the other tools that read the
//     same file would not recognise. That is why text typed at an unknown
//     cursor position is reported rather than emitted with the coordinates
//     left off: a step whose coordinates are absent means row zero, column
//     zero to a reader that has not been taught otherwise, and typing into
//     the wrong field is exactly the failure this rule exists to prevent.
//
// [workflow playback]: https://3270Web.3270.io/workflow/
package macroimport

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jnnngs/3270Web/internal/session"
)

// Note is one line the translator would not translate, with the reason it
// gives the operator. Source is the line as it was written, because a line
// number on its own sends somebody back to the file to find out what it was
// about.
type Note struct {
	Line   int    `json:"line"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// Result is what a translation produced: the steps, the lines that need a
// human, and remarks that belong to the translation as a whole rather than to
// any one line.
type Result struct {
	Steps []session.WorkflowStep `json:"steps"`
	Notes []Note                 `json:"notes"`
	// Advisories are true of the whole file — a difference in meaning
	// between the macro and the recording that is worth knowing once, rather
	// than being repeated against every line it applies to.
	Advisories []string `json:"advisories,omitempty"`
	// Statements counts the lines that asked for something. Declarations,
	// comments and the scaffolding around a subroutine are not statements
	// here: reporting `End Sub` as untranslated would bury the lines that
	// matter.
	Statements int `json:"statements"`
	// Translated counts statements that came across cleanly, whether or not
	// they produced a step — a wait for the host to settle translates to
	// nothing, because playback waits for the keyboard at every step anyway.
	Translated int `json:"translated"`
}

// Reasons a line did not come across. Each is written to be read by the
// person who has to fix it, so it says what the recording format can do
// instead rather than only what it cannot.
const (
	reasonControlFlow  = "a recording is a straight line of steps, with no branching, loops or subroutine calls"
	reasonVariable     = "a recording carries literal values, not variables"
	reasonExpression   = "its arguments are an expression rather than literal values"
	reasonDialog       = "a replayed recording has nobody at the keyboard to answer a dialog"
	reasonScreenRead   = "reads screen text into a variable — a Guided Business Task names an output instead of storing one"
	reasonExternal     = "reaches outside the terminal session"
	reasonCursor       = "text typed with no known cursor position — a recording step names the row and column, and where the host left the cursor is not knowable from the file"
	reasonWholeScreen  = "searches the whole screen — a recording checks the text at a named row and column"
	reasonCursorWait   = "waits for the cursor to arrive somewhere, and a recording has no step that waits on the cursor"
	reasonCoordinates  = "row and column are counted from 1, and this names something outside the screen"
	reasonControlChar  = "the text holds a tab or a line break, which a recording step cannot carry as field text"
	reasonDanglingWait = "a wait with no step after it, so there is nothing for the delay to belong to"
)

// Translate reads a macro file and returns the recording it becomes.
//
// It never fails: a file with nothing translatable in it comes back with no
// steps and a note per line, which is the honest answer to "can this be
// imported" and a more useful one than an error.
func Translate(src string) Result {
	t := &translator{}
	for _, ln := range logicalLines(src) {
		t.statement(ln)
	}
	if t.pendingDelay > 0 {
		t.note(t.pendingDelayLine, reasonDanglingWait)
	}
	return t.result()
}

type translator struct {
	steps      []session.WorkflowStep
	notes      []Note
	statements int
	translated int

	// The cursor as the macro leaves it. Known only while it can be derived
	// from the file itself: a statement that names a position sets it, and
	// any key press clears it, because where the host puts the cursor after
	// an Enter or a Tab depends on the screen it draws next.
	cursorKnown bool
	row, col    int

	// A wait becomes the delay on the step that follows it, which is what a
	// wait in a macro is for.
	pendingDelay     float64
	pendingDelayLine line

	sawWaitForString bool
	sawTimedWait     bool
	sawConnect       bool
}

type line struct {
	Number int
	Text   string
}

func (t *translator) result() Result {
	res := Result{
		Steps:      t.steps,
		Notes:      t.notes,
		Statements: t.statements,
		Translated: t.translated,
	}
	if res.Steps == nil {
		res.Steps = []session.WorkflowStep{}
	}
	if res.Notes == nil {
		res.Notes = []Note{}
	}
	if t.sawWaitForString {
		res.Advisories = append(res.Advisories, "Waits for text became value checks at the same position. Playback waits for the keyboard to unlock before every step, but it does not poll for text — give those steps a delay if the screen behind them is slow to draw.")
	}
	if t.sawTimedWait {
		res.Advisories = append(res.Advisories, "Wait times were read as milliseconds. A macro that counted in seconds will replay faster than it ran; the step delays in the recording are the place to correct it.")
	}
	if t.sawConnect {
		res.Advisories = append(res.Advisories, "The recording connects to the host this session is pointed at. Whatever the macro named as its session is not carried over — it belongs to the machine the macro ran on.")
	}
	return res
}

// note records a line the operator has to deal with. One line can raise the
// same objection twice — a send-keys string that types two values with no
// position for either — and saying it twice about the same line adds nothing
// but length to a report whose whole job is to be read.
func (t *translator) note(l line, reason string) {
	for _, existing := range t.notes {
		if existing.Line == l.Number && existing.Reason == reason {
			return
		}
	}
	t.notes = append(t.notes, Note{Line: l.Number, Source: l.Text, Reason: reason})
}

// emit appends a step, giving it any delay a preceding wait asked for.
func (t *translator) emit(step session.WorkflowStep) {
	if t.pendingDelay > 0 {
		step.StepDelay = &session.WorkflowDelayRange{Min: t.pendingDelay, Max: t.pendingDelay}
		t.pendingDelay = 0
	}
	t.steps = append(t.steps, step)
}

// statement translates one logical line.
func (t *translator) statement(l line) {
	if l.Text == "" || isScaffolding(l.Text) {
		return
	}
	t.statements++
	noted := len(t.notes)
	t.dispatch(l)
	if len(t.notes) == noted {
		t.translated++
	}
}

func (t *translator) dispatch(l line) {
	if reason, ok := languageConstruct(l.Text); ok {
		t.note(l, reason)
		return
	}
	head, rawArgs, ok := splitCall(l.Text)
	if !ok {
		t.note(l, fmt.Sprintf("%q is not a statement this importer understands", firstWord(l.Text)))
		return
	}
	method := strings.ToLower(lastSegment(head))
	args, literal := parseArgs(rawArgs)

	switch method {
	case "putstring", "puttext", "setstring":
		if !literal {
			t.note(l, reasonExpression)
			return
		}
		t.putString(l, args)
	case "sendkeys", "sendkey", "type":
		if !literal {
			t.note(l, reasonExpression)
			return
		}
		if len(args) != 1 || !args[0].isStr {
			t.note(l, "expected one string of keys to send")
			return
		}
		t.sendKeys(l, args[0].str)
	case "moveto", "movecursor", "setcursor", "setcursorpos":
		if !literal {
			t.note(l, reasonExpression)
			return
		}
		if len(args) != 2 || !args[0].isNum || !args[1].isNum {
			t.note(l, "expected a row and a column")
			return
		}
		if !t.setCursor(args[0].num, args[1].num) {
			t.note(l, reasonCoordinates)
		}
	case "waitforstring", "waitfortext":
		if !literal {
			t.note(l, reasonExpression)
			return
		}
		t.waitForString(l, args)
	case "waithostquiet", "waitforhostsettle", "wait", "sleep", "pause", "delay":
		if !literal {
			t.note(l, reasonExpression)
			return
		}
		t.wait(l, args)
	case "waitforcursor":
		t.note(l, reasonCursorWait)
	case "connect":
		// Whatever the macro named as its session belongs to the machine it
		// ran on; the recording connects to the host this session is
		// pointed at, which the advisory says out loud.
		t.sawConnect = true
		t.emit(session.WorkflowStep{Type: "Connect"})
		t.cursorKnown = false
	case "disconnect":
		t.emit(session.WorkflowStep{Type: "Disconnect"})
		t.cursorKnown = false
	case "getstring", "gettext", "readscreen", "getdata", "area", "search", "findstring":
		t.note(l, reasonScreenRead)
	case "msgbox", "inputbox":
		t.note(l, reasonDialog)
	case "createobject", "getobject", "shell", "run", "open", "print", "write":
		t.note(l, reasonExternal)
	default:
		t.note(l, fmt.Sprintf("%q is not a statement this importer understands", lastSegment(head)))
	}
}

// putString handles a positioned write, and the one-argument form that types
// wherever the cursor happens to be.
func (t *translator) putString(l line, args []arg) {
	switch len(args) {
	case 1:
		if !args[0].isStr {
			t.note(l, "expected the text to write")
			return
		}
		t.typeText(l, args[0].str)
	case 3:
		if !args[0].isStr || !args[1].isNum || !args[2].isNum {
			t.note(l, "expected text, a row and a column")
			return
		}
		if !t.setCursor(args[1].num, args[2].num) {
			t.note(l, reasonCoordinates)
			return
		}
		t.typeText(l, args[0].str)
	default:
		t.note(l, "expected text, or text with a row and a column")
	}
}

func (t *translator) waitForString(l line, args []arg) {
	switch len(args) {
	case 1:
		if !args[0].isStr {
			t.note(l, "expected the text to wait for")
			return
		}
		// The text is somewhere on the screen and the file does not say
		// where. Guessing a position is the one thing this importer will not
		// do, so the line goes to the operator, who has the screen in front
		// of them.
		t.note(l, reasonWholeScreen)
	case 3:
		if !args[0].isStr || !args[1].isNum || !args[2].isNum {
			t.note(l, "expected text, a row and a column")
			return
		}
		row, col := args[1].num, args[2].num
		if !validPosition(row, col) {
			t.note(l, reasonCoordinates)
			return
		}
		t.sawWaitForString = true
		t.emit(session.WorkflowStep{
			Type:        "CheckValue",
			Coordinates: &session.WorkflowCoordinates{Row: row, Column: col},
			Text:        args[0].str,
		})
	default:
		t.note(l, "expected text, or text with a row and a column")
	}
}

// wait turns a pause into the delay on the step that follows it.
//
// A wait with no time on it is a wait for the host to settle, and that one
// translates to nothing on purpose: playback waits for the keyboard to unlock
// before every step already, so emitting a delay for it would add a pause the
// macro never asked for.
func (t *translator) wait(l line, args []arg) {
	if len(args) == 0 {
		return
	}
	if len(args) != 1 || !args[0].isNum {
		t.note(l, "expected a wait time in milliseconds")
		return
	}
	ms := args[0].num
	if ms < 0 {
		t.note(l, "a wait cannot be negative")
		return
	}
	if ms == 0 {
		return
	}
	t.sawTimedWait = true
	seconds := math.Round(float64(ms)) / 1000
	t.pendingDelay += math.Round(seconds*1000) / 1000
	t.pendingDelayLine = l
}

// sendKeys splits a send-keys string into the text it types and the keys it
// presses, in the order they appear.
func (t *translator) sendKeys(l line, keys string) {
	for _, tok := range tokenizeKeys(keys) {
		if !tok.mnemonic {
			t.typeText(l, tok.value)
			continue
		}
		stepType, ok := stepTypeForMnemonic(tok.value)
		if !ok {
			t.note(l, fmt.Sprintf("<%s> is not a key a 3270 terminal has", tok.value))
			continue
		}
		t.emit(session.WorkflowStep{Type: stepType})
		// Where the cursor lands after a key is the host's decision, and it
		// is made after this file was written. Anything typed from here on
		// needs a position of its own.
		t.cursorKnown = false
	}
}

func (t *translator) typeText(l line, text string) {
	if text == "" {
		return
	}
	// Tabs and line breaks are actions to the terminal, not field text; the
	// layers that accept field input reject them for the same reason.
	if strings.ContainsAny(text, "\r\n\t") {
		t.note(l, reasonControlChar)
		return
	}
	if !t.cursorKnown {
		t.note(l, reasonCursor)
		return
	}
	t.emit(session.WorkflowStep{
		Type:        "FillString",
		Coordinates: &session.WorkflowCoordinates{Row: t.row, Column: t.col},
		Text:        text,
	})
	t.col += utf8.RuneCountInString(text)
}

func (t *translator) setCursor(row, col int) bool {
	if !validPosition(row, col) {
		t.cursorKnown = false
		return false
	}
	t.row, t.col = row, col
	t.cursorKnown = true
	return true
}

// validPosition accepts 1-based positions inside the largest 3270 model.
// Anything outside it is a mistake in the file or a misread argument order,
// and both are worth telling somebody about.
func validPosition(row, col int) bool {
	return row >= 1 && row <= 62 && col >= 1 && col <= 160
}

/* -------------------------------------------------------------------- */
/* Reading the file                                                      */
/* -------------------------------------------------------------------- */

// logicalLines splits the source into statements, joining continuations and
// dropping trailing comments. The line number reported is the first physical
// line of the statement, which is where an operator will look.
func logicalLines(src string) []line {
	src = strings.TrimPrefix(src, "\ufeff")
	physical := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	out := make([]line, 0, len(physical))
	var pending strings.Builder
	pendingNumber := 0
	for i, raw := range physical {
		text := strings.TrimSpace(stripComment(raw))
		if pending.Len() > 0 {
			pending.WriteString(" ")
		} else {
			pendingNumber = i + 1
		}
		if strings.HasSuffix(text, "_") && !strings.HasSuffix(text, "__") {
			pending.WriteString(strings.TrimSpace(strings.TrimSuffix(text, "_")))
			continue
		}
		pending.WriteString(text)
		out = append(out, line{Number: pendingNumber, Text: strings.TrimSpace(pending.String())})
		pending.Reset()
	}
	if pending.Len() > 0 {
		out = append(out, line{Number: pendingNumber, Text: strings.TrimSpace(pending.String())})
	}
	return out
}

// stripComment removes a trailing comment, respecting quotes: an apostrophe
// inside a string is part of the value a field is meant to receive.
func stripComment(text string) string {
	inQuote := false
	for i, r := range text {
		switch r {
		case '"':
			inQuote = !inQuote
		case '\'':
			if !inQuote {
				return text[:i]
			}
		}
	}
	return text
}

// isScaffolding reports whether a line is structure rather than an
// instruction — the shape of the file rather than something it does.
func isScaffolding(text string) bool {
	lower := strings.ToLower(text)
	switch {
	case lower == "":
		return true
	case lower == "rem" || strings.HasPrefix(lower, "rem "):
		return true
	case strings.HasPrefix(lower, "option "):
		return true
	case strings.HasPrefix(lower, "dim ") || strings.HasPrefix(lower, "redim "):
		return true
	case strings.HasPrefix(lower, "sub ") || strings.HasPrefix(lower, "function "):
		return true
	case strings.HasPrefix(lower, "private sub ") || strings.HasPrefix(lower, "public sub "):
		return true
	case lower == "end sub" || lower == "end function":
		return true
	case strings.HasPrefix(lower, "set "):
		// Binding a name to the session object is plumbing for the machine
		// the macro ran on. The statements that use the name are matched on
		// the method, so the binding itself carries nothing.
		return strings.Contains(lower, "=")
	}
	return false
}

// languageConstruct recognises the parts of the scripting language that have
// no counterpart in a list of steps, so each gets a reason of its own rather
// than "not understood".
func languageConstruct(text string) (string, bool) {
	lower := strings.ToLower(text)
	first := firstWord(lower)
	switch first {
	case "if", "else", "elseif", "for", "next", "do", "loop", "while", "wend",
		"select", "case", "goto", "gosub", "return", "exit", "call", "on":
		return reasonControlFlow, true
	case "end":
		// `End Sub` is scaffolding and never reaches here; `End If` and the
		// rest close a construct that could not be translated either.
		return reasonControlFlow, true
	}
	if isAssignment(text) {
		return reasonVariable, true
	}
	return "", false
}

// isAssignment reports whether the statement assigns to something. A call
// with an equals sign inside a string argument is not one, so quotes are
// respected; an equals sign after an opening parenthesis belongs to the
// arguments.
func isAssignment(text string) bool {
	inQuote := false
	for i, r := range text {
		switch r {
		case '"':
			inQuote = !inQuote
		case '(':
			if !inQuote {
				return false
			}
		case '=':
			if !inQuote {
				return i > 0
			}
		}
	}
	return false
}

func firstWord(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexAny(text, " \t("); i >= 0 {
		return text[:i]
	}
	return text
}

func lastSegment(head string) string {
	if i := strings.LastIndex(head, "."); i >= 0 {
		return head[i+1:]
	}
	return head
}

// splitCall separates the dotted path of a statement from its arguments.
// Parentheses around the arguments are optional in the language and so they
// are here.
func splitCall(text string) (head, args string, ok bool) {
	end := 0
	for end < len(text) {
		r := text[end]
		if r == '.' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			end++
			continue
		}
		break
	}
	head = strings.TrimSuffix(text[:end], ".")
	if head == "" {
		return "", "", false
	}
	rest := strings.TrimSpace(text[end:])
	if rest == "" {
		return head, "", true
	}
	if strings.HasPrefix(rest, "(") {
		if !strings.HasSuffix(rest, ")") {
			return "", "", false
		}
		return head, strings.TrimSpace(rest[1 : len(rest)-1]), true
	}
	// Arguments have to be separated from the method by whitespace; anything
	// else is an operator, and an operator means an expression.
	if !strings.HasPrefix(text[end:], " ") && !strings.HasPrefix(text[end:], "\t") {
		return "", "", false
	}
	return head, rest, true
}

/* -------------------------------------------------------------------- */
/* Arguments                                                             */
/* -------------------------------------------------------------------- */

type arg struct {
	str   string
	num   int
	isStr bool
	isNum bool
}

// parseArgs splits an argument list into literals. It reports false the
// moment an argument is anything else — a variable, a concatenation, a call —
// because a recording stores values, and a value that has to be computed is
// not one this file can supply.
func parseArgs(raw string) ([]arg, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	parts := splitOutsideQuotes(raw, ',')
	out := make([]arg, 0, len(parts))
	literal := true
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case len(part) >= 2 && strings.HasPrefix(part, `"`) && strings.HasSuffix(part, `"`):
			inner := part[1 : len(part)-1]
			// A doubled quote inside a string is one quote, the way the
			// language writes it.
			if strings.Contains(inner, `"`) && !strings.Contains(inner, `""`) {
				literal = false
				out = append(out, arg{})
				continue
			}
			out = append(out, arg{str: strings.ReplaceAll(inner, `""`, `"`), isStr: true})
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				literal = false
				out = append(out, arg{})
				continue
			}
			out = append(out, arg{num: n, isNum: true})
		}
	}
	return out, literal
}

func splitOutsideQuotes(text string, sep rune) []string {
	var out []string
	var current strings.Builder
	inQuote := false
	for _, r := range text {
		if r == '"' {
			inQuote = !inQuote
		}
		if r == sep && !inQuote {
			out = append(out, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	out = append(out, current.String())
	return out
}

/* -------------------------------------------------------------------- */
/* Keys                                                                  */
/* -------------------------------------------------------------------- */

type keyToken struct {
	value    string
	mnemonic bool
}

// tokenizeKeys splits a send-keys string into text runs and the <mnemonics>
// between them. An unclosed angle bracket is text: the operator typed a
// less-than sign into a field.
func tokenizeKeys(keys string) []keyToken {
	var out []keyToken
	for len(keys) > 0 {
		open := strings.IndexByte(keys, '<')
		if open < 0 {
			out = append(out, keyToken{value: keys})
			break
		}
		closeIdx := strings.IndexByte(keys[open:], '>')
		if closeIdx < 0 {
			out = append(out, keyToken{value: keys})
			break
		}
		closeIdx += open
		if open > 0 {
			out = append(out, keyToken{value: keys[:open]})
		}
		out = append(out, keyToken{value: keys[open+1 : closeIdx], mnemonic: true})
		keys = keys[closeIdx+1:]
	}
	return out
}

// keyMnemonics maps what these files write to the step types playback knows.
// The spellings vary between the products that produce them, which is why
// several names arrive at the same step.
var keyMnemonics = map[string]string{
	"enter":      "PressEnter",
	"tab":        "PressTab",
	"backtab":    "PressBackTab",
	"btab":       "PressBackTab",
	"shift+tab":  "PressBackTab",
	"clear":      "PressClear",
	"reset":      "PressReset",
	"eraseeof":   "PressEraseEOF",
	"erof":       "PressEraseEOF",
	"eraseinput": "PressEraseInput",
	"erinp":      "PressEraseInput",
	"dup":        "PressDup",
	"fieldmark":  "PressFieldMark",
	"fm":         "PressFieldMark",
	"sysreq":     "PressSysReq",
	"attn":       "PressAttn",
	"newline":    "PressNewline",
	"nl":         "PressNewline",
	"backspace":  "PressBackspace",
	"bksp":       "PressBackspace",
	"delete":     "PressDelete",
	"del":        "PressDelete",
	"insert":     "PressInsert",
	"ins":        "PressInsert",
	"home":       "PressHome",
	"up":         "PressUp",
	"curup":      "PressUp",
	"down":       "PressDown",
	"curdown":    "PressDown",
	"left":       "PressLeft",
	"curleft":    "PressLeft",
	"right":      "PressRight",
	"curright":   "PressRight",
}

func stepTypeForMnemonic(raw string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.ReplaceAll(name, " ", "")
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	if name == "" {
		return "", false
	}
	if step, ok := keyMnemonics[name]; ok {
		return step, true
	}
	// Function keys are written as PF3 or F3 depending on the file; both mean
	// the same key to the host.
	digits := ""
	switch {
	case strings.HasPrefix(name, "pf"):
		digits = name[2:]
	case strings.HasPrefix(name, "f"):
		digits = name[1:]
	case strings.HasPrefix(name, "pa"):
		if n, err := strconv.Atoi(name[2:]); err == nil && n >= 1 && n <= 3 {
			return fmt.Sprintf("PressPA%d", n), true
		}
		return "", false
	}
	if digits == "" {
		return "", false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 || n > 24 {
		return "", false
	}
	return fmt.Sprintf("PressPF%d", n), true
}
