package macroimport

import (
	"strings"
	"testing"
)

// The tests in this file are about one rule above all others: a branch whose
// condition did not translate takes its body with it. Everything else here is
// a shape that either translates exactly or is reported, and the difference
// between those two is the whole value of the importer.

func TestBranchAndLoopTranslate(t *testing.T) {
	src := `Sub Main
    balance = Session.Screen.GetString(12, 20, 10)
    If balance > 1000 Then
        Session.Screen.PutString "REFER", 5, 20
    Else
        Session.Screen.PutString "PAY", 5, 20
    End If
    Do While Session.Screen.GetString(24, 2, 4) = "MORE"
        Session.Screen.SendKeys "<Enter>"
    Loop
End Sub
`
	res := Translate(src)
	if len(res.Notes) != 0 {
		t.Fatalf("expected a clean translation, got notes: %+v", res.Notes)
	}
	want := strings.Join([]string{
		"SetVariable balance@12,20,10",
		"If balance greaterThan=1000",
		"FillString@5,20=REFER",
		"Else",
		"FillString@5,20=PAY",
		"EndIf",
		"While@24,2,4 equals=MORE",
		"PressEnter",
		"EndWhile",
	}, " | ")
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s", got, want)
	}
	if res.Statements != res.Translated {
		t.Fatalf("Statements = %d, Translated = %d; a file that came across whole should say so", res.Statements, res.Translated)
	}
}

func TestABranchThatDidNotTranslateTakesItsBodyWithIt(t *testing.T) {
	// The failure this rule exists to prevent: the If is dropped, the steps
	// inside it survive, and a flow that ran on some days runs on every day.
	src := `If Session.Screen.Search("APPROVED") Then
    Session.Screen.PutString "Y", 5, 20
    Session.Screen.SendKeys "<Enter>"
End If
Session.Screen.MoveTo 1, 1
`
	res := Translate(src)
	if len(res.Steps) != 0 {
		t.Fatalf("steps = %q; nothing inside an untranslated branch may be emitted", stepSummary(res.Steps))
	}
	if len(res.Notes) != 1 {
		t.Fatalf("notes = %+v, want one note naming the branch", res.Notes)
	}
	note := res.Notes[0]
	if note.Line != 1 {
		t.Errorf("note is against line %d, want the line that opened the branch", note.Line)
	}
	if !strings.Contains(note.Reason, "2 statements") {
		t.Errorf("the note must say how many statements went with the branch: %q", note.Reason)
	}
	// The lines inside are counted, so the ratio in the report stays honest,
	// and the line after the branch is unaffected.
	if res.Statements != 5 || res.Translated != 1 {
		t.Fatalf("Statements = %d, Translated = %d; want 5 and 1", res.Statements, res.Translated)
	}
}

func TestNestedBlocksInsideOneThatDidNotTranslate(t *testing.T) {
	src := `If Session.Screen.Search("X") Then
    Do While Session.Screen.GetString(1, 1, 3) = "ABC"
        Session.Screen.SendKeys "<Enter>"
    Loop
End If
Session.Screen.SendKeys "<PF3>"
`
	res := Translate(src)
	if got := stepSummary(res.Steps); got != "PressPF3" {
		t.Fatalf("steps = %q; a loop inside an untranslated branch is still inside it", got)
	}
	if len(res.Notes) != 1 {
		t.Fatalf("notes = %+v, want one — the outer branch reports for everything it took", res.Notes)
	}
}

func TestUntilBecomesTheOppositeWhile(t *testing.T) {
	res := Translate(`Do Until Session.Screen.GetString(24, 2, 4) = "DONE"
    Session.Screen.SendKeys "<PF8>"
Loop
`)
	want := "While@24,2,4 notEquals=DONE | PressPF8 | EndWhile"
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s\nnotes: %+v", got, want, res.Notes)
	}
}

func TestWhileWendAndComparisonSpellings(t *testing.T) {
	cases := map[string]string{
		`= "END"`:  "equals=END",
		`<> "END"`: "notEquals=END",
		`>= 100`:   "greaterOrEqual=100",
		`< 100`:    "lessThan=100",
	}
	for tail, want := range cases {
		res := Translate("While Session.Screen.GetString(1, 1, 3) " + tail + "\n    Session.Screen.SendKeys \"<Enter>\"\nWend\n")
		got := stepSummary(res.Steps)
		if !strings.HasPrefix(got, "While@1,1,3 "+want) {
			t.Errorf("%q → %q, want a While comparing %s (notes %+v)", tail, got, want, res.Notes)
		}
		if !strings.HasSuffix(got, "EndWhile") {
			t.Errorf("%q → %q, want it closed", tail, got)
		}
	}
}

func TestConditionWrittenValueFirst(t *testing.T) {
	// `"READY" = GetString(…)` is the same comparison with its sides swapped,
	// and a comparison mirrored is not a comparison guessed at.
	res := Translate(`If 1000 < Session.Screen.GetString(5, 20, 8) Then
    Session.Screen.SendKeys "<Enter>"
End If
`)
	want := "If@5,20,8 greaterThan=1000 | PressEnter | EndIf"
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s\nnotes: %+v", got, want, res.Notes)
	}
}

func TestElseIfBecomesAnElseHoldingAnIf(t *testing.T) {
	res := Translate(`If Session.Screen.GetString(1, 1, 1) = "A" Then
    Session.Screen.SendKeys "<PF1>"
ElseIf Session.Screen.GetString(1, 1, 1) = "B" Then
    Session.Screen.SendKeys "<PF2>"
Else
    Session.Screen.SendKeys "<PF3>"
End If
`)
	want := strings.Join([]string{
		"If@1,1,1 equals=A", "PressPF1",
		"Else", "If@1,1,1 equals=B", "PressPF2",
		"Else", "PressPF3",
		"EndIf", "EndIf",
	}, " | ")
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s\nnotes: %+v", got, want, res.Notes)
	}
}

func TestAnElseIfThatDoesNotTranslateEndsTheBlock(t *testing.T) {
	// The first branch is real and stays. The rest of the block goes, because
	// letting it through would run the later branches whenever the first
	// condition was false — a different flow from the one the macro describes.
	res := Translate(`If Session.Screen.GetString(1, 1, 1) = "A" Then
    Session.Screen.SendKeys "<PF1>"
ElseIf Session.Screen.Search("B") Then
    Session.Screen.SendKeys "<PF2>"
Else
    Session.Screen.SendKeys "<PF3>"
End If
`)
	want := "If@1,1,1 equals=A | PressPF1 | EndIf"
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s", got, want)
	}
	if len(res.Notes) != 1 || res.Notes[0].Line != 3 {
		t.Fatalf("notes = %+v, want one against the ElseIf", res.Notes)
	}
	if !strings.Contains(res.Notes[0].Reason, "3 statements") {
		t.Errorf("the note should count everything the abandoned tail took: %q", res.Notes[0].Reason)
	}
}

func TestInlineIfIsAWholeBranch(t *testing.T) {
	res := Translate(`If Session.Screen.GetString(1, 1, 2) = "OK" Then Session.Screen.SendKeys "<Enter>" Else Session.Screen.SendKeys "<PF3>"` + "\n")
	want := "If@1,1,2 equals=OK | PressEnter | Else | PressPF3 | EndIf"
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s\nnotes: %+v", got, want, res.Notes)
	}
}

func TestInlineIfWithAnUnreadableConditionKeepsItsStatement(t *testing.T) {
	res := Translate(`If x > 1 Then Session.Screen.SendKeys "<Enter>"` + "\n")
	if len(res.Steps) != 0 {
		t.Fatalf("steps = %q; the statement a branch guards is not unconditional", stepSummary(res.Steps))
	}
	if len(res.Notes) != 1 || res.Notes[0].Reason != reasonConditionShape {
		t.Fatalf("notes = %+v, want the condition-shape reason", res.Notes)
	}
}

func TestConditionsThatAreReportedRatherThanGuessed(t *testing.T) {
	cases := []struct {
		condition string
		reason    string
	}{
		{`Session.Screen.GetString(1, 1, 5) = "A" And Session.Screen.GetString(2, 1, 5) = "B"`, reasonConditionCombined},
		{`Not Session.Screen.GetString(1, 1, 5) = "A"`, reasonConditionCombined},
		{`Session.Screen.Search("READY")`, reasonConditionShape},
		{`Session.Screen.GetString(1, 1, 5) = someName`, reasonConditionShape},
		{`Session.Screen.GetString(1, 1) = "A"`, reasonConditionShape},
		{`Session.Screen.GetString(0, 1, 5) = "A"`, reasonConditionShape},
		{`Trim(Session.Screen.GetString(1, 1, 5)) = "A"`, reasonConditionShape},
	}
	for _, c := range cases {
		res := Translate("If " + c.condition + " Then\n    Session.Screen.SendKeys \"<Enter>\"\nEnd If\n")
		if len(res.Steps) != 0 {
			t.Errorf("%s produced %q, want nothing", c.condition, stepSummary(res.Steps))
		}
		if len(res.Notes) != 1 || !strings.HasPrefix(res.Notes[0].Reason, c.reason) {
			t.Errorf("%s → notes %+v, want %q", c.condition, res.Notes, c.reason)
		}
	}
}

func TestALoopThatLeavesEarlyDoesNotTranslate(t *testing.T) {
	// A recording's While has no way out but its own condition, so a
	// translated loop would carry on where the macro stopped.
	res := Translate(`Do While Session.Screen.GetString(1, 1, 4) = "MORE"
    Session.Screen.SendKeys "<Enter>"
    If Session.Screen.GetString(2, 1, 3) = "END" Then
        Exit Do
    End If
Loop
`)
	if len(res.Steps) != 0 {
		t.Fatalf("steps = %q, want nothing from a loop with a way out", stepSummary(res.Steps))
	}
	if len(res.Notes) != 1 || !strings.HasPrefix(res.Notes[0].Reason, reasonLoopExit) {
		t.Fatalf("notes = %+v, want the early-exit reason", res.Notes)
	}
}

func TestAFileThatJumpsGetsNoBlocksAtAll(t *testing.T) {
	// Skipping the block a GoTo sits in removes the jump but not the lines it
	// was jumping over, so the whole file's structure is refused rather than
	// the one branch.
	res := Translate(`If Session.Screen.GetString(1, 1, 5) = "READY" Then
    Session.Screen.SendKeys "<Enter>"
End If
If Session.Screen.GetString(2, 1, 5) = "ERROR" Then
    GoTo Finish
End If
Finish:
Session.Screen.SendKeys "<PF3>"
`)
	for _, step := range res.Steps {
		if step.Type == "If" {
			t.Fatalf("a branch was translated in a file that jumps: %s", stepSummary(res.Steps))
		}
	}
	var sawReason bool
	for _, note := range res.Notes {
		if strings.HasPrefix(note.Reason, reasonJumpInFile) {
			sawReason = true
		}
	}
	if !sawReason {
		t.Fatalf("notes = %+v, want the jump reason against the branch", res.Notes)
	}
	var sawAdvisory bool
	for _, a := range res.Advisories {
		if strings.Contains(a, "GoTo") {
			sawAdvisory = true
		}
	}
	if !sawAdvisory {
		t.Fatalf("advisories = %v, want one saying why no branch translated", res.Advisories)
	}
}

func TestABlockThatIsNeverClosed(t *testing.T) {
	res := Translate(`If Session.Screen.GetString(1, 1, 5) = "READY" Then
    Session.Screen.SendKeys "<Enter>"
`)
	if len(res.Steps) != 0 {
		t.Fatalf("steps = %q; which lines are inside an unclosed block is not something the file says", stepSummary(res.Steps))
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0].Reason, "End If") {
		t.Fatalf("notes = %+v, want one naming the closer it needs", res.Notes)
	}
}

func TestAClosingStatementWithNothingOpen(t *testing.T) {
	res := Translate("Session.Screen.SendKeys \"<Enter>\"\nEnd If\n")
	if len(res.Notes) != 1 || res.Notes[0].Reason != reasonOrphanCloser {
		t.Fatalf("notes = %+v, want the orphan-closer reason", res.Notes)
	}
	if got := stepSummary(res.Steps); got != "PressEnter" {
		t.Fatalf("steps = %q, want the statement before it to survive", got)
	}
}

func TestMismatchedBlocksTranslateNothingStructural(t *testing.T) {
	res := Translate(`If Session.Screen.GetString(1, 1, 5) = "READY" Then
    Session.Screen.SendKeys "<Enter>"
Wend
`)
	for _, step := range res.Steps {
		if step.Type == "If" {
			t.Fatalf("a block was translated in a file whose blocks do not match: %s", stepSummary(res.Steps))
		}
	}
}

func TestVariablesReadWrittenAndForgotten(t *testing.T) {
	res := Translate(`account = Session.Screen.GetString(4, 12, 8)
label = "REF"
copy = account
Session.Screen.PutString account, 9, 20
`)
	want := strings.Join([]string{
		"SetVariable account@4,12,8",
		"SetVariable label=REF",
		"SetVariable copy=${account}",
		"FillString@9,20=${account}",
	}, " | ")
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s\nnotes: %+v", got, want, res.Notes)
	}
}

func TestAVariableSetInsideABranchIsNotKnownAfterIt(t *testing.T) {
	// The branch may not run, and a step reading a variable that was never set
	// stops the run against a live host. Reporting it here is the cheaper
	// place to find out.
	res := Translate(`If Session.Screen.GetString(1, 1, 1) = "Y" Then
    total = Session.Screen.GetString(5, 5, 6)
End If
Session.Screen.PutString total, 9, 20
`)
	if len(res.Notes) != 1 || res.Notes[0].Reason != reasonUnknownVariable {
		t.Fatalf("notes = %+v, want the unknown-variable reason on the line after the branch", res.Notes)
	}
	for _, step := range res.Steps {
		if step.Type == "FillString" {
			t.Fatalf("a field was filled from a variable that may never have been set: %s", stepSummary(res.Steps))
		}
	}
}

func TestAValueThatCannotBeReadForgetsTheName(t *testing.T) {
	// Leaving the name pointing at what it held before would let a later
	// condition compare a stale value and look like it worked.
	res := Translate(`total = Session.Screen.GetString(5, 5, 6)
total = total + 1
If total > 10 Then
    Session.Screen.SendKeys "<Enter>"
End If
`)
	for _, step := range res.Steps {
		if step.Type == "If" {
			t.Fatalf("a condition read a variable whose value stopped being knowable: %s", stepSummary(res.Steps))
		}
	}
	if len(res.Notes) != 2 {
		t.Fatalf("notes = %+v, want the assignment and the branch that depended on it", res.Notes)
	}
}

func TestExitSubBecomesStopOnlyInASingleRoutineFile(t *testing.T) {
	single := Translate(`Sub Main
    If Session.Screen.GetString(1, 1, 1) = "X" Then
        Exit Sub
    End If
    Session.Screen.SendKeys "<Enter>"
End Sub
`)
	want := "If@1,1,1 equals=X | Stop=the macro ended the flow here | EndIf | PressEnter"
	if got := stepSummary(single.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s\nnotes: %+v", got, want, single.Notes)
	}

	several := Translate(`Sub Main
    Exit Sub
End Sub
Sub Helper
    Session.Screen.SendKeys "<Enter>"
End Sub
`)
	if len(several.Notes) != 1 || several.Notes[0].Reason != reasonExitAmongRoutines {
		t.Fatalf("notes = %+v, want Exit Sub reported in a file of several routines", several.Notes)
	}
	var sawAdvisory bool
	for _, a := range several.Advisories {
		if strings.Contains(a, "2 routines") {
			sawAdvisory = true
		}
	}
	if !sawAdvisory {
		t.Fatalf("advisories = %v, want one saying the recording is a single flow", several.Advisories)
	}
}

func TestAWaitBeforeAScreenReadingDecision(t *testing.T) {
	// A decision that reads the screen is what the macro was waiting for the
	// host to finish drawing, so the delay belongs to it.
	res := Translate(`Session.Screen.WaitHostQuiet 1500
If Session.Screen.GetString(1, 1, 5) = "READY" Then
    Session.Screen.SendKeys "<Enter>"
End If
`)
	if len(res.Steps) == 0 || res.Steps[0].Type != "If" {
		t.Fatalf("steps = %q", stepSummary(res.Steps))
	}
	if res.Steps[0].StepDelay == nil || res.Steps[0].StepDelay.Min != 1.5 {
		t.Fatalf("the decision carries %+v, want the 1.5 second delay the wait asked for", res.Steps[0].StepDelay)
	}
}

func TestKeywordsInsideStringsAreNotStructure(t *testing.T) {
	res := Translate(`Session.Screen.PutString "END IF THEN", 3, 4` + "\n")
	if got := stepSummary(res.Steps); got != "FillString@3,4=END IF THEN" {
		t.Fatalf("steps = %q, notes %+v", got, res.Notes)
	}
}

func TestCursorPositionDoesNotEscapeABranch(t *testing.T) {
	// The MoveTo inside the branch may not have happened, so text typed after
	// the branch has no position this file can supply.
	res := Translate(`Session.Screen.MoveTo 1, 1
If Session.Screen.GetString(2, 2, 1) = "Y" Then
    Session.Screen.MoveTo 10, 10
End If
Session.Screen.SendKeys "ABC"
`)
	for _, step := range res.Steps {
		if step.Type == "FillString" {
			t.Fatalf("text was typed at a position that depended on a branch: %s", stepSummary(res.Steps))
		}
	}
	if len(res.Notes) != 1 || res.Notes[0].Reason != reasonCursor {
		t.Fatalf("notes = %+v, want the unknown-cursor reason", res.Notes)
	}
}

func TestAStatementOnTheSameLineAsItsBranch(t *testing.T) {
	res := Translate(`Sub Main
    If Session.Screen.GetString(1, 1, 1) = "X" Then Exit Sub
    Session.Screen.SendKeys "<Enter>"
End Sub
`)
	want := "If@1,1,1 equals=X | Stop=the macro ended the flow here | EndIf | PressEnter"
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s\nnotes: %+v", got, want, res.Notes)
	}
	if res.Statements != res.Translated {
		t.Fatalf("Statements = %d, Translated = %d; a line is counted once", res.Statements, res.Translated)
	}
}

func TestCountsNeverExceedTheStatementsRead(t *testing.T) {
	// Statements and Translated are what the report leads with, and a count
	// that could exceed the other would make the whole report untrustworthy.
	sources := []string{
		`If Session.Screen.GetString(1, 1, 1) = "X" Then Exit Sub`,
		`If Session.Screen.GetString(1, 1, 1) = "X" Then Session.Screen.SendKeys "<Enter>" Else Exit Sub`,
		"Do While Session.Screen.GetString(1,1,4) = \"MORE\"\n  Session.Screen.SendKeys \"<Enter>\"\nLoop",
		"If Session.Screen.Search(\"X\") Then\n  Session.Screen.SendKeys \"<Enter>\"\nEnd If",
	}
	for _, src := range sources {
		res := Translate(src + "\n")
		if res.Translated > res.Statements {
			t.Errorf("%q → Translated %d of %d statements", src, res.Translated, res.Statements)
		}
	}
}
