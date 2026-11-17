// SPDX-License-Identifier: AGPL-3.0-or-later

package host

// Writing into one field by position, checked against what the host receives.
//
// WriteField exists for a caller that cannot see the screen — an AI model
// driving the terminal by row and column. Each test here is a thing such a
// caller got wrong without being told: the tail of an old value left behind a
// shorter new one, a field it could not clear, a keyboard left locked by a
// write into a label. The assertions are on the inbound record wherever the
// claim is about what was typed, because the screen this side shows is the one
// thing a write that went wrong still gets right.

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// prefilledScreen is a label, an entry field already holding SMITH, a numeric
// field and a hidden one.
//
//	row 0: NAME  [SMITH               ]
//	row 1: AGE   [    ]  (numeric)
//	row 2: PIN   [        ]  (hidden)
func prefilledScreen() []byte {
	return newScreen(80).
		at(0, 0).field(faProtected).text("NAME").
		at(0, 10).field(0).cursor().text("SMITH").
		at(0, 31).field(faProtected).
		at(1, 0).field(faProtected).text("AGE").
		at(1, 10).field(faNumeric).
		at(1, 15).field(faProtected).
		at(2, 0).field(faProtected).text("PIN").
		at(2, 10).field(faHidden).
		at(2, 19).field(faProtected).
		bytes()
}

func writeFieldTerminal(t *testing.T) (*conformanceHost, *S3270) {
	t.Helper()
	host := startConformanceHost(t, prefilledScreen(), prefilledScreen())
	return host, connectTerminal(t, host, "-model", "2")
}

func inboundFieldAt(rec inboundRecord, addr int) (string, bool) {
	for _, f := range rec.Fields {
		if f.Address == addr {
			return strings.TrimRight(f.Text, " \x00"), true
		}
	}
	return "", false
}

// A shorter value written over a longer one replaces it. This is the bug the
// method was written for: LEE typed over SMITH reached the host as LEETH.
func TestConformanceWriteFieldErasesTheOldValue(t *testing.T) {
	host, term := writeFieldTerminal(t)

	got, err := term.WriteField(0, 11, "LEE", true)
	if err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if got.Value != "LEE" {
		t.Errorf("the write reported the field as %q, want %q", got.Value, "LEE")
	}
	if got.Row != 0 || got.Col != 11 || got.Length != 20 {
		t.Errorf("field reported at (%d,%d) length %d, want (0,11) length 20", got.Row, got.Col, got.Length)
	}

	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	rec := host.awaitRecord(1)
	text, ok := inboundFieldAt(rec, addressOf(0, 11, 80))
	if !ok {
		t.Fatalf("the name field was not reported as modified: %+v", rec.Fields)
	}
	if text != "LEE" {
		t.Errorf("the host received %q, want %q", text, "LEE")
	}
}

// Overtyping is still available, and does what the word says.
func TestConformanceWriteFieldCanOvertype(t *testing.T) {
	host, term := writeFieldTerminal(t)

	got, err := term.WriteField(0, 11, "LEE", false)
	if err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if got.Value != "LEETH" {
		t.Errorf("the write reported the field as %q, want %q", got.Value, "LEETH")
	}
	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	rec := host.awaitRecord(1)
	if text, _ := inboundFieldAt(rec, addressOf(0, 11, 80)); text != "LEETH" {
		t.Errorf("the host received %q, want %q", text, "LEETH")
	}
}

// Empty text clears the field. It used to do nothing and say it had worked.
func TestConformanceWriteFieldEmptyTextClears(t *testing.T) {
	host, term := writeFieldTerminal(t)

	got, err := term.WriteField(0, 11, "", true)
	if err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if got.Value != "" {
		t.Errorf("the cleared field reported %q", got.Value)
	}
	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	rec := host.awaitRecord(1)
	text, ok := inboundFieldAt(rec, addressOf(0, 11, 80))
	if !ok {
		t.Fatalf("a cleared field must still be reported as modified, got %+v", rec.Fields)
	}
	if text != "" {
		t.Errorf("the host received %q from a cleared field", text)
	}
}

// A write aimed at a label is refused before anything is typed, the refusal
// says what is there, and the keyboard is not left locked behind it.
func TestConformanceWriteFieldRefusesAProtectedTarget(t *testing.T) {
	host, term := writeFieldTerminal(t)

	_, err := term.WriteField(0, 2, "X", true)
	if !IsFieldWriteError(err) {
		t.Fatalf("writing into a label returned %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "protected") || !strings.Contains(err.Error(), "NAME") {
		t.Errorf("the refusal should say the target is protected and name what is there, got %q", err)
	}

	// On the attribute byte itself.
	if _, err := term.WriteField(0, 10, "X", true); !IsFieldWriteError(err) {
		t.Errorf("writing onto a field attribute returned %v, want a refusal", err)
	}

	if err := term.UpdateScreen(); err != nil {
		t.Fatalf("UpdateScreen: %v", err)
	}
	if state, _ := term.GetScreenSnapshot().StatusKeyboardState(); state != "U" {
		t.Errorf("the keyboard is %q after a refused write, want unlocked", state)
	}

	// And the session still works: the next key reaches the host with no
	// field reported, because nothing was typed.
	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if rec := host.awaitRecord(1); len(rec.Fields) != 0 {
		t.Errorf("a refused write still modified something: %+v", rec.Fields)
	}
}

// Text longer than what is left of the field is refused whole rather than
// typed until it spills into the next field or locks the keyboard.
func TestConformanceWriteFieldRefusesTextThatDoesNotFit(t *testing.T) {
	host, term := writeFieldTerminal(t)

	_, err := term.WriteField(1, 11, "12345", true)
	if !IsFieldWriteError(err) {
		t.Fatalf("five characters into a four-character field returned %v, want a refusal", err)
	}
	if !strings.Contains(err.Error(), "4") {
		t.Errorf("the refusal should say how much room there is, got %q", err)
	}
	// From the middle of the field the room is what is left, not the length.
	if _, err := term.WriteField(1, 13, "123", true); !IsFieldWriteError(err) {
		t.Errorf("three characters with two cells left returned %v, want a refusal", err)
	}

	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if rec := host.awaitRecord(1); len(rec.Fields) != 0 {
		t.Errorf("a refused write still modified something: %+v", rec.Fields)
	}
}

// What is typed into a non-display field reaches the host and does not reach
// the log, and the write does not echo it back.
func TestConformanceWriteFieldKeepsHiddenInputOutOfTheLog(t *testing.T) {
	host, term := writeFieldTerminal(t)

	var captured bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(previous) })
	term.SetVerboseLogging(true)

	got, err := term.WriteField(2, 11, "HUNTER2", true)
	term.SetVerboseLogging(false)
	log.SetOutput(previous)
	if err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if !got.Hidden {
		t.Error("the field at (2,11) should be reported as hidden")
	}
	if strings.Contains(captured.String(), "HUNTER2") {
		t.Errorf("verbose logging recorded what was typed into a hidden field:\n%s", captured.String())
	}
	if !strings.Contains(captured.String(), `String(\"***\")`) && !strings.Contains(captured.String(), `String("***")`) {
		t.Errorf("the redacted write should still appear in the verbose log, got:\n%s", captured.String())
	}

	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	rec := host.awaitRecord(1)
	if text, _ := inboundFieldAt(rec, addressOf(2, 11, 80)); text != "HUNTER2" {
		t.Errorf("the host received %q from the hidden field, want %q", text, "HUNTER2")
	}
}

// When the terminal itself rejects a keystroke, the keyboard is given back.
// A refused keystroke inhibits input until Reset, and a caller that cannot see
// the indicator would find everything after it refused too.
func TestConformanceWriteFieldResetsAfterAnOperatorError(t *testing.T) {
	// Numeric lock makes a letter in a numeric field an operator error, which
	// is the one rejection a position check cannot predict.
	host := startConformanceHost(t, prefilledScreen(), prefilledScreen())
	term := connectTerminal(t, host, "-model", "2", "-xrm", "s3270.numericLock: true")

	_, err := term.WriteField(1, 11, "AB", true)
	if !IsFieldWriteError(err) {
		t.Fatalf("letters into a locked numeric field returned %v, want a refusal", err)
	}

	if err := term.UpdateScreen(); err != nil {
		t.Fatalf("UpdateScreen: %v", err)
	}
	if state, _ := term.GetScreenSnapshot().StatusKeyboardState(); state != "U" {
		t.Errorf("the keyboard is %q after an operator error, want unlocked again", state)
	}
	// The session is usable: a good write goes through.
	if _, err := term.WriteField(1, 11, "42", true); err != nil {
		t.Errorf("a valid write after the reset failed: %v", err)
	}
}

// The older write path, used by workflow playback, types where it is told and
// checks nothing. It still must not write a password into the log.
func TestConformanceWriteStringAtKeepsHiddenInputOutOfTheLog(t *testing.T) {
	_, term := writeFieldTerminal(t)

	var captured bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(previous) })
	term.SetVerboseLogging(true)
	err := term.WriteStringAt(2, 11, "HUNTER2")
	term.SetVerboseLogging(false)
	log.SetOutput(previous)

	if err != nil {
		t.Fatalf("WriteStringAt: %v", err)
	}
	if strings.Contains(captured.String(), "HUNTER2") {
		t.Errorf("verbose logging recorded what was typed into a hidden field:\n%s", captured.String())
	}
}

// The rules, without a terminal: what a position is and whether text fits.
func TestLocateFieldTarget(t *testing.T) {
	row := func(cells ...bufferCell) []bufferCell { return cells }
	sf := func(attr byte) bufferCell { return bufferCell{startField: true, attr: attr} }
	ch := func(c byte) bufferCell {
		return bufferCell{code: strings.ToLower(string("0123456789abcdef"[c>>4])) + string("0123456789abcdef"[c&0xf])}
	}

	// [P]AB[U]....[P]
	cells := [][]bufferCell{row(
		sf(AttrProtected), ch('A'), ch('B'),
		sf(0), ch(0), ch(0), ch(0), ch(0),
		sf(AttrProtected), ch(0),
	)}

	if _, err := locateFieldTarget(cells, 0, 1, 1); !IsFieldWriteError(err) || !strings.Contains(err.Error(), `"AB"`) {
		t.Errorf("a protected position should be refused naming its text, got %v", err)
	}
	if _, err := locateFieldTarget(cells, 0, 3, 1); !IsFieldWriteError(err) {
		t.Errorf("an attribute position should be refused, got %v", err)
	}
	if _, err := locateFieldTarget(cells, 0, 4, 5); !IsFieldWriteError(err) {
		t.Errorf("five characters into four cells should be refused, got %v", err)
	}
	if _, err := locateFieldTarget(cells, 3, 0, 1); !IsFieldWriteError(err) {
		t.Errorf("a row off the screen should be refused, got %v", err)
	}
	target, err := locateFieldTarget(cells, 0, 5, 3)
	if err != nil {
		t.Fatalf("three characters with three cells left: %v", err)
	}
	if target.offset != 1 || target.remaining != 3 || target.field.Col != 4 || target.field.Length != 4 {
		t.Errorf("got offset %d remaining %d field col %d length %d", target.offset, target.remaining, target.field.Col, target.field.Length)
	}
}
