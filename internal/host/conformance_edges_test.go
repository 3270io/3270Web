// SPDX-License-Identifier: AGPL-3.0-or-later

package host

// Conformance: the edges of the buffer, where a screen stops being a
// rectangle of independent rows and starts being one address space that wraps.

import (
	"strings"
	"testing"
)

// A field that runs off the end of one row and onto the next. Rows are a way
// of looking at the buffer, not a boundary in it, and a field is a run of
// addresses — so a long entry field is one field spanning two rows rather than
// two fields of one row each.
func TestConformanceReadsAFieldSpanningTwoRows(t *testing.T) {
	screen := newScreen(80).
		at(0, 70).field(0).cursor().text("ABCDEFGHI").
		at(1, 10).field(faProtected).text("END").
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	f := fieldStartingAt(s, 71, 0)
	if f == nil {
		t.Fatalf("no field starting at (71,0); got %d fields", len(s.Fields))
	}
	if !f.IsMultiline() {
		t.Errorf("a field from (71,0) to (9,1) spans two rows, got EndY=%d", f.EndY)
	}
	if f.EndY != 1 || f.EndX != 9 {
		t.Errorf("the field should end at (9,1), got (%d,%d)", f.EndX, f.EndY)
	}
	// Its value carries the row break, which is what the renderer splits on to
	// lay a wrapped entry field out as two boxes.
	if got := f.GetValue(); !strings.Contains(got, "\n") {
		t.Errorf("a field spanning two rows should carry the row break: %q", got)
	}
	if got := strings.ReplaceAll(strings.TrimRight(f.GetValue(), "\x00"), "\n", ""); !strings.HasPrefix(got, "ABCDEFGHI") {
		t.Errorf("the field's text: got %q", got)
	}
}

// The last field on the screen has nothing after it to close it, so it runs to
// the end of the buffer. A screen whose final field is left open is the normal
// case, not an edge one — most panels end with a field and no attribute after
// it.
func TestConformanceClosesTheLastFieldAtTheEndOfTheBuffer(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("FIRST").
		at(23, 70).field(0).cursor().text("LAST").
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	f := fieldStartingAt(s, 71, 23)
	if f == nil {
		t.Fatalf("no field starting at (71,23); got %d fields", len(s.Fields))
	}
	if f.EndY != 23 || f.EndX != 79 {
		t.Errorf("the last field should run to (79,23), got (%d,%d)", f.EndX, f.EndY)
	}
	if got := strings.TrimRight(f.GetValue(), " \x00"); got != "LAST" {
		t.Errorf("the last field's text: got %q want %q", got, "LAST")
	}
}

// Two field attributes written next to each other. The second closes the field
// the first opened before it has reached a single position, which is how a
// screen stops an entry field running on. The field is real — it owns its
// attribute byte, which is a screen position — and dropping it shifts every
// column after it.
func TestConformanceKeepsZeroLengthFields(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("NAME").
		at(0, 10).field(0).
		at(0, 11).field(faProtected).text("AFTER").
		at(1, 0).field(0).cursor().
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	f := fieldStartingAt(s, 11, 0)
	if f == nil {
		t.Fatalf("the zero-length field was dropped; fields: %d", len(s.Fields))
	}
	if !f.IsZeroLength() {
		t.Errorf("the field at (11,0) should hold no positions, ends at (%d,%d)", f.EndX, f.EndY)
	}
	if got := f.GetValue(); got != "" {
		t.Errorf("a zero-length field has no text, got %q", got)
	}
	// And the columns after it land where the host put them.
	if got := screenRow(s, 0); !strings.HasSuffix(strings.TrimRight(got, " "), "AFTER") {
		t.Errorf("row 0: got %q", got)
	}
}

// The whole screen as one field. A host that opens a field at the top left and
// never closes it is describing a display whose every position belongs to it.
func TestConformanceReadsAScreenWideField(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("ONLY FIELD").
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	if len(s.Fields) != 1 {
		t.Fatalf("expected one field, got %d", len(s.Fields))
	}
	f := s.Fields[0]
	if f.StartX != 1 || f.StartY != 0 || f.EndX != 79 || f.EndY != 23 {
		t.Errorf("the field should cover (1,0)-(79,23), got (%d,%d)-(%d,%d)",
			f.StartX, f.StartY, f.EndX, f.EndY)
	}
}

// A screen the host rewrites in place. A Write — as opposed to an Erase/Write
// — changes only what it addresses, so what it does not mention has to still
// be there afterwards.
func TestConformanceLeavesUnwrittenPositionsAlone(t *testing.T) {
	first := newScreen(80).
		at(0, 0).field(faProtected).text("KEEP THIS").
		at(1, 0).field(faProtected).text("REPLACE ME").
		at(2, 0).field(0).cursor().
		bytes()
	update := newUpdate(80).
		at(1, 1).text("OVERWRITTEN").
		bytes()

	host := startConformanceHost(t, first, update)
	term := connectTerminal(t, host, "-model", "2")

	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	host.awaitRecord(1)
	if err := term.UpdateScreen(); err != nil {
		t.Fatalf("UpdateScreen: %v", err)
	}
	s := term.GetScreenSnapshot()

	if got := screenRow(s, 0); got != " KEEP THIS" {
		t.Errorf("a Write should not touch row 0: got %q", got)
	}
	if got := screenRow(s, 1); got != " OVERWRITTEN" {
		t.Errorf("row 1: got %q want %q", got, " OVERWRITTEN")
	}
}

// The buffer wraps. A field runs from its attribute byte to the next one, so
// when the first attribute on the screen is not at address zero, the positions
// in front of it belong to the *last* field — the one that runs off the bottom
// right and continues at the top left.
//
// Decoding left to right cannot see that, and what it used to do instead was
// invent a protected field for them. That is right whenever the last field on
// the screen is protected, which is most of the time and is why it went
// unnoticed. When the last field is an entry field it is wrong in the way that
// matters: the operator is shown a region they cannot type into, on a screen
// where the host is waiting for them to.
func TestConformanceReadsAFieldThatWrapsPastTheEndOfTheBuffer(t *testing.T) {
	screen := newScreen(80).
		at(0, 10).field(faProtected).text("PROTECTED").
		at(23, 60).field(0).cursor().text("TAIL").
		bytes()

	host := startConformanceHost(t, screen, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	lead := fieldStartingAt(s, 0, 0)
	if lead == nil {
		t.Fatalf("nothing owns the positions before the first attribute; fields: %d", len(s.Fields))
	}
	if lead.IsProtected() {
		t.Errorf("the wrapped head of an entry field decoded as protected (fa %#02x)", lead.FieldCode)
	}
	if s.GetInputFieldAt(2, 0) == nil {
		t.Error("the wrapped head of an entry field is not reported as an input field")
	}

	// And what is typed there reaches the host.
	if err := term.SubmitOperatorInput(func(sc *Screen) string {
		if f := fieldStartingAt(sc, 0, 0); f != nil {
			f.SetValue("WRAPPED")
		}
		return ""
	}); err != nil {
		t.Fatalf("SubmitOperatorInput: %v", err)
	}
	if err := term.SendKey("Enter"); err != nil {
		t.Fatalf("Enter: %v", err)
	}

	rec := host.awaitRecord(1)
	found := false
	for _, f := range rec.Fields {
		if strings.Contains(f.Text, "WRAPPED") {
			found = true
		}
	}
	if !found {
		t.Errorf("the host never received what was typed into the wrapped head: %+v", rec.Fields)
	}
}
