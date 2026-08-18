// SPDX-License-Identifier: AGPL-3.0-or-later

package host

// The other half of the conversation.
//
// Everything else in this package is about reading a screen. What the terminal
// sends *back* has never been checked against anything, and it is where a
// compatibility problem is least visible from here: a key that produces the
// wrong AID byte, or a field that is not reported as modified, looks like a
// host application misbehaving rather than like a terminal bug, because the
// screen this side is showing is perfectly correct.
//
// The harness keeps every inbound record, so these tests assert on the bytes
// the host would actually receive.

import (
	"strings"
	"testing"
)

// entryScreen is a plain one-field screen: a label, an entry field the cursor
// starts in, and a field attribute closing it.
func entryScreen(cols int) []byte {
	return newScreen(cols).
		at(0, 0).field(faProtected).text("NAME").
		at(1, 0).field(0).cursor().
		at(1, 21).field(faProtected).text("X").
		bytes()
}

// Every AID key, as the byte the host receives. These are the numbers a host
// application switches on, so a key that sends the wrong one runs the wrong
// transaction — and does it silently, because the screen that comes back is a
// real screen.
func TestConformanceSendsTheRightAIDByte(t *testing.T) {
	cases := []struct {
		key  string
		want byte
	}{
		{"Enter", aidEnter},
		{"PF1", aidPF1},
		{"PF3", aidPF3},
		{"PF12", aidPF12},
		{"PF13", aidPF13},
		{"PF24", aidPF24},
		{"PA1", aidPA1},
		{"PA2", aidPA2},
		{"PA3", aidPA3},
		{"Clear", aidClear},
	}

	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			host := startConformanceHost(t, entryScreen(80), entryScreen(80))
			term := connectTerminal(t, host, "-model", "2")

			if err := term.SendKey(c.key); err != nil {
				t.Fatalf("%s: %v", c.key, err)
			}
			rec := host.awaitRecord(1)
			if rec.AID != c.want {
				t.Errorf("%s produced AID %#02x, want %#02x", c.key, rec.AID, c.want)
			}
		})
	}
}

// The cursor position reaches the host exactly once, in the inbound record.
// Screens that branch on where the cursor was — a menu you select by putting
// the cursor on a line and pressing Enter — are entirely at its mercy.
func TestConformanceReportsTheCursorPosition(t *testing.T) {
	host := startConformanceHost(t, entryScreen(80), entryScreen(80))
	term := connectTerminal(t, host, "-model", "2")

	if err := term.MoveCursor(1, 5); err != nil {
		t.Fatalf("MoveCursor: %v", err)
	}
	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	rec := host.awaitRecord(1)
	if want := addressOf(1, 5, 80); rec.Cursor != want {
		t.Errorf("the host was told the cursor was at %d, want %d", rec.Cursor, want)
	}
}

// A field the operator typed into comes back; one they did not, does not.
// That is the modified-data tag, and it is how a host tells an edit from a
// redisplay. Sending every field every time is not merely wasteful — an
// application that reads "this field was returned" as "the operator touched
// it" acts on input nobody gave.
func TestConformanceReportsOnlyModifiedFields(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("ONE").
		at(0, 20).field(0).cursor().
		at(0, 40).field(faProtected).text("TWO").
		at(1, 0).field(0).
		at(1, 40).field(faProtected).
		bytes()

	host := startConformanceHost(t, screen, screen)
	term := connectTerminal(t, host, "-model", "2")

	if err := term.SubmitOperatorInput(func(s *Screen) string {
		f := fieldStartingAt(s, 21, 0)
		if f == nil {
			t.Fatal("no entry field at (21,0)")
		}
		f.SetValue("HELLO")
		return ""
	}); err != nil {
		t.Fatalf("SubmitOperatorInput: %v", err)
	}
	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	rec := host.awaitRecord(1)
	if len(rec.Fields) != 1 {
		t.Fatalf("expected one modified field, got %d: %+v", len(rec.Fields), rec.Fields)
	}
	got := rec.Fields[0]
	if want := addressOf(0, 21, 80); got.Address != want {
		t.Errorf("the modified field was reported at %d, want %d", got.Address, want)
	}
	if text := strings.TrimRight(got.Text, " "); text != "HELLO" {
		t.Errorf("the host received %q, want %q", text, "HELLO")
	}
}

// A hidden field is hidden from the person at the keyboard, not from the host.
// The whole point of one is that what was typed into it is transmitted.
func TestConformanceTransmitsHiddenFieldContents(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("PASSWORD").
		at(0, 20).field(faHidden).cursor().
		at(0, 40).field(faProtected).
		bytes()

	host := startConformanceHost(t, screen, screen)
	term := connectTerminal(t, host, "-model", "2")

	if err := term.SubmitOperatorInput(func(s *Screen) string {
		f := fieldStartingAt(s, 21, 0)
		if f == nil {
			t.Fatal("no hidden field at (21,0)")
		}
		if !f.IsHidden() {
			t.Errorf("the field at (21,0) should decode as hidden, fa %#02x", f.FieldCode)
		}
		f.SetValue("SECRET")
		return ""
	}); err != nil {
		t.Fatalf("SubmitOperatorInput: %v", err)
	}
	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	rec := host.awaitRecord(1)
	if len(rec.Fields) != 1 {
		t.Fatalf("expected one modified field, got %+v", rec.Fields)
	}
	if text := strings.TrimRight(rec.Fields[0].Text, " "); text != "SECRET" {
		t.Errorf("the host received %q from the hidden field, want %q", text, "SECRET")
	}
}

// A short-read key sends the AID and nothing else. PA keys and Clear are how
// an operator says "cancel" or "start again", and a terminal that returned the
// screen contents with them would have the host act on a screen the operator
// was trying to abandon.
func TestConformanceShortReadKeysCarryNoFields(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(0).cursor().
		at(0, 20).field(faProtected).
		bytes()

	for _, key := range []string{"PA1", "Clear"} {
		t.Run(key, func(t *testing.T) {
			host := startConformanceHost(t, screen, screen)
			term := connectTerminal(t, host, "-model", "2")

			if err := term.SubmitOperatorInput(func(s *Screen) string {
				if f := fieldStartingAt(s, 1, 0); f != nil {
					f.SetValue("TYPED")
				}
				return ""
			}); err != nil {
				t.Fatalf("SubmitOperatorInput: %v", err)
			}
			if err := term.SendKey(key); err != nil {
				t.Fatalf("%s: %v", key, err)
			}

			rec := host.awaitRecord(1)
			if len(rec.Fields) != 0 {
				t.Errorf("%s returned field contents: %+v", key, rec.Fields)
			}
		})
	}
}

// The host writes a second screen in reply, and the terminal reads it. A flow
// is two screens and the step between them, and the step is where a session
// that decodes one screen perfectly can still stall.
func TestConformanceFollowsAScreenTransition(t *testing.T) {
	first := newScreen(80).
		at(0, 0).field(faProtected).text("FIRST").
		at(1, 0).field(0).cursor().
		at(1, 20).field(faProtected).
		bytes()
	second := newScreen(80).
		at(0, 0).field(faProtected).text("SECOND").
		at(1, 0).field(0).cursor().
		at(1, 20).field(faProtected).
		bytes()

	host := startConformanceHost(t, first, second)
	term := connectTerminal(t, host, "-model", "2")

	if got := screenRow(term.GetScreenSnapshot(), 0); got != " FIRST" {
		t.Fatalf("first screen: got %q", got)
	}
	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	host.awaitRecord(1)
	if err := term.UpdateScreen(); err != nil {
		t.Fatalf("UpdateScreen: %v", err)
	}
	if got := screenRow(term.GetScreenSnapshot(), 0); got != " SECOND" {
		t.Errorf("second screen: got %q want %q", got, " SECOND")
	}
}
