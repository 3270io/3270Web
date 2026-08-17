// SPDX-License-Identifier: AGPL-3.0-or-later

package macroimport

import (
	"fmt"
	"strings"

	"github.com/jnnngs/3270Web/internal/session"
)

// Translating the shape of the file: what happens to each line that opens,
// closes or leaves a block. See control.go for the rule these all serve —
// a branch whose condition did not translate takes its body with it.

// structural handles a line that does something to the shape of the file
// rather than to the terminal.
func (t *translator) structural(l line, form lineForm) {
	if t.skipping() {
		t.structuralWhileSkipping(l, form)
		return
	}
	switch form.role {
	case roleOpenIf:
		t.openIf(l, form)
	case roleInlineIf:
		t.inlineIf(l, form)
	case roleElseIf:
		t.elseIf(l, form)
	case roleElse:
		t.elseBranch(l, form)
	case roleCloseIf, roleCloseLoop, roleCloseOpaque:
		t.closeBlock(l, form)
	case roleOpenLoop:
		t.openLoop(l, form)
	case roleOpenOpaque:
		t.beginSkip(l, form, form.reason)
	case roleExitFlow:
		noted := len(t.notes)
		t.exitFlow(l)
		if len(t.notes) == noted {
			t.translated++
		}
	case roleExitLoop:
		// Only reachable outside any loop this importer opened, since a loop
		// holding one never translates and takes its body with it.
		t.note(l, reasonLoopExit)
	case roleJump:
		t.note(l, reasonJump)
	}
}

// structuralWhileSkipping keeps the block structure straight inside a block
// that did not translate. Nothing is emitted and nothing is noted; what the
// nesting is for is finding the closer that ends the skip.
func (t *translator) structuralWhileSkipping(l line, form lineForm) {
	switch form.role {
	case roleOpenIf, roleOpenLoop, roleOpenOpaque:
		t.pushFrame(frameState{kind: form.kind, line: l})
		t.countSkipped()
	case roleCloseIf, roleCloseLoop, roleCloseOpaque:
		t.closeBlock(l, form)
	default:
		t.countSkipped()
	}
}

func (t *translator) openIf(l line, form lineForm) {
	step, reason := t.blockCondition(form)
	if reason != "" {
		t.beginSkip(l, form, reason)
		return
	}
	step.Type = "If"
	t.emitControl(step)
	t.pushFrame(frameState{kind: kindIf, line: l, translated: true, openIfs: 1})
	t.translated++
}

func (t *translator) openLoop(l line, form lineForm) {
	step, reason := t.blockCondition(form)
	if reason != "" {
		t.beginSkip(l, form, reason)
		return
	}
	step.Type = "While"
	t.emitControl(step)
	t.pushFrame(frameState{kind: form.kind, line: l, translated: true})
	t.translated++
	t.sawTranslatedLoop = true
	// The body may run any number of times, so where the cursor was left
	// before the loop says nothing about where it is on the second pass.
	t.cursorKnown = false
}

// inlineIf handles `If … Then <statement>` — one line that is a whole branch.
// Its condition is under the same rule as any other: if it does not
// translate, neither does the statement it guards.
func (t *translator) inlineIf(l line, form lineForm) {
	if t.plan.jumps {
		t.note(l, reasonJumpInFile)
		return
	}
	step, reason := t.condition(form.condition, form.negate)
	if reason != "" {
		t.note(l, reason)
		return
	}
	noted := len(t.notes)
	step.Type = "If"
	t.emitControl(step)
	row, column, known := t.row, t.col, t.cursorKnown
	t.subStatement(l, form.then)
	if form.otherwise != "" {
		t.emitControl(session.WorkflowStep{Type: "Else"})
		t.row, t.col, t.cursorKnown = row, column, known
		t.subStatement(l, form.otherwise)
	}
	t.emitControl(session.WorkflowStep{Type: "EndIf"})
	t.cursorKnown = false
	if len(t.notes) == noted {
		t.translated++
	}
}

// elseIf becomes an Else holding a further If, which is how a flat list of
// steps writes a chain of branches. Each one opened here is closed by its own
// EndIf when the block ends.
func (t *translator) elseIf(l line, form lineForm) {
	f := t.top()
	if f == nil || f.kind != kindIf || !f.translated {
		t.note(l, reasonOrphanCloser)
		return
	}
	step, reason := t.condition(form.condition, false)
	if reason != "" {
		// The If translated and this branch did not. Letting the rest of the
		// block through would run the later branches whenever the first
		// condition was false, which is a different flow from the one the
		// macro describes — so the tail of the block goes with this branch.
		t.beginTailSkip(f, l, reason)
		return
	}
	t.forgetVariables(len(t.frames) - 1)
	t.emitControl(session.WorkflowStep{Type: "Else"})
	step.Type = "If"
	t.emitControl(step)
	f.openIfs++
	t.restoreCursor(f)
	t.translated++
}

func (t *translator) elseBranch(l line, form lineForm) {
	f := t.top()
	if f == nil || f.kind != kindIf || !f.translated {
		t.note(l, reasonOrphanCloser)
		return
	}
	t.forgetVariables(len(t.frames) - 1)
	t.emitControl(session.WorkflowStep{Type: "Else"})
	t.restoreCursor(f)
	if form.then == "" {
		t.translated++
		return
	}
	// `Else <statement>` on one line: the statement belongs to this branch.
	noted := len(t.notes)
	t.subStatement(l, form.then)
	if len(t.notes) == noted {
		t.translated++
	}
}

// subStatement translates the statement written on the same line as the
// branch that guards it. `If … Then Exit Sub` is the shape this exists for:
// the tail is a whole statement, and one of the statements it can be leaves
// the flow rather than acting on the terminal.
func (t *translator) subStatement(l line, text string) {
	if classify(text).role == roleExitFlow {
		t.exitFlow(l)
		return
	}
	t.dispatch(l, text)
}

func (t *translator) closeBlock(l line, form lineForm) {
	f := t.top()
	if f == nil || f.kind != form.kind {
		if t.skipping() {
			t.countSkipped()
			return
		}
		t.note(l, reasonOrphanCloser)
		return
	}
	frame := *f
	t.frames = t.frames[:len(t.frames)-1]
	t.forgetVariables(len(t.frames))
	nested := t.skipping()

	if frame.skipping && !nested {
		t.noteSkippedBlock(frame)
		// A wait inside a block nothing came out of has nothing left to
		// belong to, and letting it out would delay the next step the macro
		// never asked to delay.
		t.pendingDelay = 0
	}
	switch {
	case frame.translated:
		for i := 0; i < t.closerCount(frame); i++ {
			t.emitControl(session.WorkflowStep{Type: closingStepFor(frame.kind)})
		}
		t.translated++
	case nested:
		t.countSkipped()
	}
	t.cursorKnown = false
}

// closerCount is how many closing steps a frame needs: one per If it opened,
// which is one plus one for every ElseIf, and one for a loop.
func (t *translator) closerCount(frame frameState) int {
	if frame.kind == kindIf {
		if frame.openIfs < 1 {
			return 1
		}
		return frame.openIfs
	}
	return 1
}

// exitFlow turns `Exit Sub` into a Stop step, which is what it means when the
// file describes one flow. In a file of several routines it does not mean
// that: what it returns to is whatever called it, and that is not something
// the file says.
func (t *translator) exitFlow(l line) {
	if t.plan.routines > 1 {
		t.note(l, reasonExitAmongRoutines)
		return
	}
	t.emitControl(session.WorkflowStep{Type: "Stop", Text: "the macro ended the flow here"})
	t.cursorKnown = false
}

// blockCondition answers whether a block converts, and into what. Everything
// it refuses is refused for the whole block, body included.
func (t *translator) blockCondition(form lineForm) (session.WorkflowStep, string) {
	if form.reason != "" {
		return session.WorkflowStep{}, form.reason
	}
	block := t.plan.blocks[t.index]
	if block == nil || !block.closed {
		return session.WorkflowStep{}, fmt.Sprintf(reasonUnclosedBlock, closerFor(form.kind))
	}
	if t.plan.jumps {
		return session.WorkflowStep{}, reasonJumpInFile
	}
	if block.hasExit {
		return session.WorkflowStep{}, reasonLoopExit
	}
	return t.condition(form.condition, form.negate)
}

/* -------------------------------------------------------------------- */
/* The frame stack                                                       */
/* -------------------------------------------------------------------- */

func (t *translator) top() *frameState {
	if len(t.frames) == 0 {
		return nil
	}
	return &t.frames[len(t.frames)-1]
}

// skipping reports whether the innermost open block is one whose statements
// are being left out.
func (t *translator) skipping() bool {
	f := t.top()
	return f != nil && f.skipping
}

func (t *translator) pushFrame(f frameState) {
	f.enterRow, f.enterCol, f.enterKnown = t.row, t.col, t.cursorKnown
	if t.skipping() {
		// A block inside a block that is being left out is left out with it,
		// however translatable it might have been on its own: its statements
		// are conditional on a branch nothing came out of.
		f.skipping = true
		f.translated = false
	}
	t.frames = append(t.frames, f)
}

// beginSkip opens a block that did not translate. The reason is held until
// the block closes, because the note it becomes says how many statements went
// with it and that is not known yet.
func (t *translator) beginSkip(l line, form lineForm, reason string) {
	t.pushFrame(frameState{kind: form.kind, line: l, skipping: true, skipLine: l, skipReason: reason})
}

// beginTailSkip abandons the rest of a block that had translated up to here.
func (t *translator) beginTailSkip(f *frameState, l line, reason string) {
	f.skipping = true
	f.skipLine = l
	f.skipReason = reason
}

// countSkipped charges a suppressed line to the outermost block that is being
// left out, which is the one whose note will report it.
func (t *translator) countSkipped() {
	for i := range t.frames {
		if t.frames[i].skipping {
			t.frames[i].skipped++
			return
		}
	}
}

// noteSkippedBlock is the whole report of a block that did not translate: the
// line that decided it, the reason, and the number of statements that went
// with it. The count is the part that matters — a reader who knows a branch
// needs a human still has to know that its contents are not in the recording
// either.
func (t *translator) noteSkippedBlock(frame frameState) {
	reason := frame.skipReason
	if reason == "" {
		reason = reasonControlFlow
	}
	if frame.skipped > 0 {
		verb := "were"
		if frame.skipped == 1 {
			verb = "was"
		}
		reason += fmt.Sprintf(" — and the %s inside it %s left out with it, because a step inside a branch that did not translate would otherwise run every time", statementsPhrase(frame.skipped), verb)
	}
	t.note(frame.skipLine, reason)
}

// closeOpenBlocks deals with the blocks a file never closed.
//
// A block that did not translate reports itself and what it took with it. A
// translated one cannot normally still be open — the structural pass refuses
// to translate a block it did not see closed — but if one ever were, the
// recording would hold an If with no EndIf and would be refused as a whole
// when it was loaded. So its closing steps are written anyway: a report that
// names the problem is worth more than a file that cannot be opened.
func (t *translator) closeOpenBlocks() {
	for len(t.frames) > 0 {
		frame := t.frames[len(t.frames)-1]
		t.frames = t.frames[:len(t.frames)-1]
		if frame.skipping && !t.skipping() {
			t.noteSkippedBlock(frame)
		}
		if frame.translated {
			for i := 0; i < t.closerCount(frame); i++ {
				t.emitControl(session.WorkflowStep{Type: closingStepFor(frame.kind)})
			}
			t.note(frame.line, fmt.Sprintf(reasonUnclosedBlock, closerFor(frame.kind)))
		}
	}
}

func (t *translator) restoreCursor(f *frameState) {
	t.row, t.col, t.cursorKnown = f.enterRow, f.enterCol, f.enterKnown
}

// forgetVariables drops everything set deeper than the given block depth. A
// variable set inside a branch is not one the steps after the branch can
// count on: the branch may not run, and a step reading a variable that was
// never set stops the run.
func (t *translator) forgetVariables(depth int) {
	for name, setAt := range t.vars {
		if setAt > depth {
			delete(t.vars, name)
		}
	}
}

/* -------------------------------------------------------------------- */
/* Assignments                                                           */
/* -------------------------------------------------------------------- */

// assignment turns `balance = Session.Screen.GetString(12, 20, 10)` into the
// SetVariable step that remembers the same thing.
//
// What it will take is a literal, a variable already set, or a read of a named
// place on the screen. What it will not take is an expression: a recording has
// no arithmetic and no functions, so a value computed from two others is a
// value this file cannot hand over. The name is then forgotten rather than
// left pointing at whatever it held before, because a later condition reading
// a stale value is worse than one reported as untranslatable.
func (t *translator) assignment(l line, text string, eq int) {
	name := strings.TrimSpace(text[:eq])
	value := strings.TrimSpace(text[eq+1:])
	if !isVariableName(name) {
		t.note(l, reasonVariableTarget)
		return
	}
	step, ok := t.valueExpression(value)
	if !ok {
		delete(t.vars, name)
		t.note(l, reasonVariable)
		return
	}
	step.Type = "SetVariable"
	step.Variable = name
	t.emitControl(step)
	t.vars[name] = len(t.frames)
	if step.Coordinates != nil {
		t.sawScreenCondition = true
	}
}
