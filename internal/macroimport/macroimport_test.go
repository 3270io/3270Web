package macroimport

import (
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/session"
)

// stepSummary renders the steps in a form a failure message can show in one
// line, so a broken translation says what it produced rather than how many.
func stepSummary(steps []session.WorkflowStep) string {
	parts := make([]string, 0, len(steps))
	for _, step := range steps {
		part := step.Type
		if step.Coordinates != nil {
			part += "@" + itoa(step.Coordinates.Row) + "," + itoa(step.Coordinates.Column)
		}
		if step.Text != "" {
			part += "=" + step.Text
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " | ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}

func TestTranslateWholeMacro(t *testing.T) {
	src := `' Account inquiry
Option Explicit
Sub Main
    Session.Connect
    Session.Screen.WaitHostQuiet
    Session.Screen.PutString "USER01", 5, 20
    Session.Screen.SendKeys "<Enter>"
    Session.Screen.WaitForString "MAIN MENU", 1, 30
    Session.Screen.MoveTo 10, 15
    Session.Screen.SendKeys "ACCT100<Enter>"
    Session.Screen.Sendkeys("<PF3>")
    Session.Disconnect
End Sub
`
	res := Translate(src)
	if len(res.Notes) != 0 {
		t.Fatalf("expected a clean translation, got notes: %+v", res.Notes)
	}
	want := "Connect | FillString@5,20=USER01 | PressEnter | CheckValue@1,30=MAIN MENU | FillString@10,15=ACCT100 | PressEnter | PressPF3 | Disconnect"
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s", got, want)
	}
	if res.Statements != res.Translated {
		t.Fatalf("Statements = %d, Translated = %d; want them equal on a clean file", res.Statements, res.Translated)
	}
	if res.Statements != 9 {
		t.Fatalf("Statements = %d, want 9 (the scaffolding must not count)", res.Statements)
	}
}

func TestTranslateReportsWhatItCannotDo(t *testing.T) {
	src := `Sub Main
    If Session.Screen.GetString(1, 1, 5) = "READY" Then
        Session.Screen.SendKeys "<Enter>"
    End If
    For i = 1 To 10
    Next
    MsgBox "done"
    balance = Session.Screen.GetString(12, 20, 10)
    Session.Screen.WaitForString "BALANCE"
    Session.Screen.WaitForCursor 5, 20
    Session.Screen.SendKeys "<Wibble>"
    Session.Screen.PutString userName, 5, 20
    Session.Screen.Frobnicate 1
End Sub
`
	res := Translate(src)
	byLine := map[int]Note{}
	for _, n := range res.Notes {
		byLine[n.Line] = n
	}
	checks := []struct {
		line   int
		reason string
	}{
		{2, reasonControlFlow}, // If ... Then
		{4, reasonControlFlow}, // End If
		{5, reasonControlFlow}, // For
		{6, reasonControlFlow}, // Next
		{7, reasonDialog},      // MsgBox
		{8, reasonVariable},    // assignment
		{9, reasonWholeScreen}, // WaitForString with no position
		{10, reasonCursorWait}, // WaitForCursor
		{12, reasonExpression}, // PutString with a variable
	}
	for _, want := range checks {
		got, ok := byLine[want.line]
		if !ok {
			t.Fatalf("line %d produced no note; notes were %+v", want.line, res.Notes)
		}
		if got.Reason != want.reason {
			t.Errorf("line %d reason = %q, want %q", want.line, got.Reason, want.reason)
		}
		if strings.TrimSpace(got.Source) == "" {
			t.Errorf("line %d note carries no source text", want.line)
		}
	}
	if note, ok := byLine[11]; !ok || !strings.Contains(note.Reason, "Wibble") {
		t.Errorf("an unknown key mnemonic should name itself, got %+v", byLine[11])
	}
	if note, ok := byLine[13]; !ok || !strings.Contains(note.Reason, "Frobnicate") {
		t.Errorf("an unknown method should name itself, got %+v", byLine[13])
	}
	// The one thing inside the untranslatable branch that could be translated
	// still is: the report is per line, not per file.
	if got := stepSummary(res.Steps); got != "PressEnter" {
		t.Fatalf("steps = %q, want the one translatable line to survive", got)
	}
}

func TestTypingWithNoKnownPositionIsReportedNotGuessed(t *testing.T) {
	src := `Session.Screen.MoveTo 5, 20
Session.Screen.SendKeys "USER01<Tab>SECRET<Enter>"
`
	res := Translate(src)
	want := "FillString@5,20=USER01 | PressTab | PressEnter"
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s", got, want)
	}
	if len(res.Notes) != 1 {
		t.Fatalf("expected one note for the text after the tab, got %+v", res.Notes)
	}
	if res.Notes[0].Reason != reasonCursor {
		t.Fatalf("note reason = %q, want the unknown-cursor reason", res.Notes[0].Reason)
	}
	// Nothing may be emitted with the coordinates left off: a reader that
	// takes an absent position as row zero would type into the wrong field.
	for _, step := range res.Steps {
		if step.Type == "FillString" && step.Coordinates == nil {
			t.Fatal("a FillString step was emitted with no coordinates")
		}
	}
}

func TestCursorAdvancesAcrossTextOnTheSameLine(t *testing.T) {
	src := `Session.Screen.PutString "AB", 3, 10
Session.Screen.SendKeys "CD"
`
	res := Translate(src)
	want := "FillString@3,10=AB | FillString@3,12=CD"
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s", got, want)
	}
}

func TestWaitBecomesTheDelayOnTheNextStep(t *testing.T) {
	src := `Session.Screen.MoveTo 1, 1
Session.Screen.WaitHostQuiet 2500
Session.Screen.SendKeys "<Enter>"
`
	res := Translate(src)
	if len(res.Steps) != 1 {
		t.Fatalf("steps = %s, want one", stepSummary(res.Steps))
	}
	delay := res.Steps[0].StepDelay
	if delay == nil {
		t.Fatal("the step after a wait carries no delay")
	}
	if delay.Min != 2.5 || delay.Max != 2.5 {
		t.Fatalf("delay = %v..%v, want 2.5..2.5 seconds", delay.Min, delay.Max)
	}
	if len(res.Advisories) == 0 {
		t.Fatal("a timed wait should raise the milliseconds advisory")
	}
}

func TestWaitWithNothingAfterItIsReported(t *testing.T) {
	res := Translate("Session.Screen.WaitHostQuiet 1000\n")
	if len(res.Notes) != 1 || res.Notes[0].Reason != reasonDanglingWait {
		t.Fatalf("notes = %+v, want one dangling-wait note", res.Notes)
	}
}

func TestUntimedHostWaitTranslatesToNothing(t *testing.T) {
	// Playback waits for the keyboard to unlock at every step, so a wait for
	// the host to settle is already covered; adding a delay for it would
	// invent a pause the macro never asked for.
	res := Translate("Session.Screen.WaitHostQuiet\n")
	if len(res.Steps) != 0 || len(res.Notes) != 0 {
		t.Fatalf("steps = %s, notes = %+v; want neither", stepSummary(res.Steps), res.Notes)
	}
	if res.Translated != 1 {
		t.Fatalf("Translated = %d, want 1 — it translated cleanly to nothing", res.Translated)
	}
}

func TestObjectNamesAndParenthesesDoNotMatter(t *testing.T) {
	variants := []string{
		`Session.Screen.PutString "A", 1, 2`,
		`Sess0.Screen.PutString("A", 1, 2)`,
		`sess.screen.putstring "A", 1, 2`,
		`objSession.Screen.PutText "A", 1, 2`,
	}
	for _, src := range variants {
		res := Translate(src + "\n")
		if got := stepSummary(res.Steps); got != "FillString@1,2=A" {
			t.Errorf("%s → %q (notes %+v)", src, got, res.Notes)
		}
	}
}

func TestFunctionKeySpellings(t *testing.T) {
	cases := map[string]string{
		"<Enter>":     "PressEnter",
		"<PF3>":       "PressPF3",
		"<F3>":        "PressPF3",
		"<pf24>":      "PressPF24",
		"<PA1>":       "PressPA1",
		"<Clear>":     "PressClear",
		"<EraseEOF>":  "PressEraseEOF",
		"<erase_eof>": "PressEraseEOF",
		"<BackTab>":   "PressBackTab",
		"<Home>":      "PressHome",
	}
	for mnemonic, want := range cases {
		res := Translate(`Session.Screen.SendKeys "` + mnemonic + `"` + "\n")
		if got := stepSummary(res.Steps); got != want {
			t.Errorf("%s → %q, want %q (notes %+v)", mnemonic, got, want, res.Notes)
		}
	}
	for _, bad := range []string{"<PF25>", "<PF0>", "<PA4>", "<>"} {
		res := Translate(`Session.Screen.SendKeys "` + bad + `"` + "\n")
		if len(res.Steps) != 0 {
			t.Errorf("%s produced %q, want nothing", bad, stepSummary(res.Steps))
		}
		if len(res.Notes) != 1 {
			t.Errorf("%s produced notes %+v, want exactly one", bad, res.Notes)
		}
	}
}

func TestCommentsQuotesAndContinuations(t *testing.T) {
	src := `Session.Screen.MoveTo 4, 8   ' park the cursor
Session.Screen.SendKeys "it's here"
Session.Screen.PutString "SPLIT", _
    9, 12
`
	res := Translate(src)
	want := `FillString@4,8=it's here | FillString@9,12=SPLIT`
	if got := stepSummary(res.Steps); got != want {
		t.Fatalf("steps =\n  %s\nwant\n  %s\nnotes: %+v", got, want, res.Notes)
	}
}

func TestDoubledQuoteIsOneQuote(t *testing.T) {
	res := Translate(`Session.Screen.PutString "SAY ""HI""", 2, 3` + "\n")
	if len(res.Steps) != 1 || res.Steps[0].Text != `SAY "HI"` {
		t.Fatalf("steps = %s (notes %+v)", stepSummary(res.Steps), res.Notes)
	}
}

func TestPositionsOutsideTheScreenAreReported(t *testing.T) {
	for _, src := range []string{
		`Session.Screen.PutString "A", 0, 1`,
		`Session.Screen.PutString "A", 1, 0`,
		`Session.Screen.MoveTo 999, 1`,
	} {
		res := Translate(src + "\n")
		if len(res.Steps) != 0 {
			t.Errorf("%s produced %q, want nothing", src, stepSummary(res.Steps))
		}
		if len(res.Notes) != 1 || res.Notes[0].Reason != reasonCoordinates {
			t.Errorf("%s → notes %+v, want the coordinates reason", src, res.Notes)
		}
	}
}

func TestFieldTextCannotCarryControlCharacters(t *testing.T) {
	res := Translate("Session.Screen.PutString \"A\tB\", 1, 1\n")
	if len(res.Steps) != 0 {
		t.Fatalf("steps = %s, want nothing", stepSummary(res.Steps))
	}
	if len(res.Notes) != 1 || res.Notes[0].Reason != reasonControlChar {
		t.Fatalf("notes = %+v, want the control-character reason", res.Notes)
	}
}

func TestEmptyAndCommentOnlyFiles(t *testing.T) {
	for _, src := range []string{"", "\n\n", "' nothing but a comment\n", "Sub Main\nEnd Sub\n"} {
		res := Translate(src)
		if len(res.Steps) != 0 || len(res.Notes) != 0 || res.Statements != 0 {
			t.Errorf("%q → steps %d, notes %d, statements %d; want zero of each", src, len(res.Steps), len(res.Notes), res.Statements)
		}
		// A caller marshalling this straight to JSON should get [] rather
		// than null for both lists.
		if res.Steps == nil || res.Notes == nil {
			t.Errorf("%q → nil slice in the result", src)
		}
	}
}

func TestOneNotePerLinePerObjection(t *testing.T) {
	// Two values typed with no known position is one thing wrong with one
	// line, said once.
	res := Translate("Session.Screen.SendKeys \"AAA<Tab>BBB\"\n")
	if len(res.Notes) != 1 {
		t.Fatalf("notes = %+v, want one", res.Notes)
	}
}

func TestByteOrderMarkAndCRLF(t *testing.T) {
	res := Translate("\ufeffSession.Screen.MoveTo 1, 1\r\nSession.Screen.SendKeys \"AB\"\r\n")
	if got := stepSummary(res.Steps); got != "FillString@1,1=AB" {
		t.Fatalf("steps = %q, notes %+v", got, res.Notes)
	}
}
