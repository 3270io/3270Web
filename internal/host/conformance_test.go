// SPDX-License-Identifier: AGPL-3.0-or-later

package host

// Conformance: what this package decides about a screen, checked against a
// real terminal reading a real 3270 data stream. See tn3270host_test.go for
// the harness and for why a captured transcript is not enough on its own.

import (
	"strings"
	"testing"
)

// A first screen, end to end: the host writes it, the terminal draws it, and
// this package decodes it. If this test fails nothing below it means anything.
func TestConformanceDecodesAWrittenScreen(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("LABEL").
		at(1, 0).field(0).cursor().text("VALUE").
		at(1, 20).field(faProtected).
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")

	s := term.GetScreenSnapshot()
	if s.Width != 80 || s.Height != 24 {
		t.Fatalf("expected a 24x80 display, got %dx%d", s.Height, s.Width)
	}
	if got := screenRow(s, 0); got != " LABEL" {
		t.Errorf("row 0: got %q want %q", got, " LABEL")
	}
	if !s.IsFormatted {
		t.Error("a screen with fields on it should decode as formatted")
	}
	if f := fieldStartingAt(s, 1, 0); f == nil || !f.IsProtected() {
		t.Errorf("the label field should be protected, got %+v", f)
	}
	if f := fieldStartingAt(s, 1, 1); f == nil || f.IsProtected() {
		t.Errorf("the entry field should be unprotected, got %+v", f)
	}
	if s.CursorY != 1 || s.CursorX != 1 {
		t.Errorf("expected the cursor where the host put it (1,1), got (%d,%d)", s.CursorY, s.CursorX)
	}
}

// Every field attribute bit, read back off a real screen. These are the bits
// the renderer turns into a read-only label, a password box, a numeric-only
// input and a tab stop that the cursor runs through, so a bit read wrongly
// here is a field that behaves as the wrong kind of field.
func TestConformanceReadsEveryFieldAttribute(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(0).text("PLAIN").
		at(1, 0).field(faProtected).text("PROT").
		at(2, 0).field(faNumeric).text("12345").
		at(3, 0).field(faHidden).text("SECRET").
		at(4, 0).field(faIntensified).text("BRIGHT").
		at(5, 0).field(faSkip).text("SKIP").
		at(6, 0).field(0).cursor().
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	cases := []struct {
		row                                       int
		protected, numeric, hidden, intense, skip bool
	}{
		{0, false, false, false, false, false},
		{1, true, false, false, false, false},
		{2, false, true, false, false, false},
		{3, false, false, true, false, false},
		{4, false, false, false, true, false},
		{5, true, true, false, false, true},
	}
	for _, c := range cases {
		f := fieldStartingAt(s, 1, c.row)
		if f == nil {
			t.Errorf("row %d: no field", c.row)
			continue
		}
		if f.IsProtected() != c.protected {
			t.Errorf("row %d: protected %v, want %v (fa %#02x)", c.row, f.IsProtected(), c.protected, f.FieldCode)
		}
		if f.IsNumeric() != c.numeric {
			t.Errorf("row %d: numeric %v, want %v (fa %#02x)", c.row, f.IsNumeric(), c.numeric, f.FieldCode)
		}
		if f.IsHidden() != c.hidden {
			t.Errorf("row %d: hidden %v, want %v (fa %#02x)", c.row, f.IsHidden(), c.hidden, f.FieldCode)
		}
		if f.IsIntensified() != c.intense {
			t.Errorf("row %d: intensified %v, want %v (fa %#02x)", c.row, f.IsIntensified(), c.intense, f.FieldCode)
		}
		if c.skip && !(f.IsProtected() && f.IsNumeric()) {
			t.Errorf("row %d: an auto-skip field is protected+numeric, got fa %#02x", c.row, f.FieldCode)
		}
	}
}

// Extended attributes, as the terminal reports them. The values are the whole
// point: a constant that does not match what arrives decodes cleanly and shows
// nothing, which is the failure this suite exists to catch.
func TestConformanceReadsExtendedAttributes(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).fieldExtended(faProtected, 0x42, AttrColRed, 0x41, AttrEhBlink).text("BLINK").
		at(1, 0).fieldExtended(faProtected, 0x41, AttrEhRevVideo).text("REVERSE").
		at(2, 0).fieldExtended(faProtected, 0x41, AttrEhUnderscore).text("UNDER").
		at(3, 0).fieldExtended(faProtected, 0x45, AttrColBlue).text("ONBLUE").
		at(4, 0).fieldExtended(faProtected, 0x42, AttrColTurquoise).text("TURQ").
		at(5, 0).field(0).cursor().
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	cases := []struct {
		row                          int
		color, highlight, background int
	}{
		{0, AttrColRed, AttrEhBlink, AttrColDefault},
		{1, AttrColDefault, AttrEhRevVideo, AttrColDefault},
		{2, AttrColDefault, AttrEhUnderscore, AttrColDefault},
		{3, AttrColDefault, AttrEhDefault, AttrColBlue},
		{4, AttrColTurquoise, AttrEhDefault, AttrColDefault},
	}
	for _, c := range cases {
		f := fieldStartingAt(s, 1, c.row)
		if f == nil {
			t.Errorf("row %d: no field", c.row)
			continue
		}
		if f.Color != c.color {
			t.Errorf("row %d: colour %#02x, want %#02x", c.row, f.Color, c.color)
		}
		if f.ExtendedHighlight != c.highlight {
			t.Errorf("row %d: highlight %#02x, want %#02x", c.row, f.ExtendedHighlight, c.highlight)
		}
		if f.Background != c.background {
			t.Errorf("row %d: background %#02x, want %#02x", c.row, f.Background, c.background)
		}
	}
}

// A Set Attribute order colours a run inside a field, and keeps colouring
// until something changes it — across the end of the row and across the field
// attribute that opens the next field.
func TestConformanceReadsCharacterAttributes(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("AB").
		setAttribute(0x42, AttrColGreen).text("CD").
		setAttribute(0x41, AttrEhBlink).text("EF").
		setAttribute(0x42, 0x00).setAttribute(0x41, 0x00).text("GH").
		at(1, 0).field(0).cursor().
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	f := fieldStartingAt(s, 1, 0)
	if f == nil {
		t.Fatal("no field on row 0")
	}
	runs := f.AttrRuns()
	var joined strings.Builder
	for _, r := range runs {
		joined.WriteString(r.Text)
	}
	if got, want := joined.String(), f.GetValue(); got != want {
		t.Fatalf("runs must rejoin into the field's value:\n got %q\nwant %q", got, want)
	}

	want := []struct {
		text      string
		color     int
		highlight int
	}{
		{"AB", AttrColDefault, AttrEhDefault},
		{"CD", AttrColGreen, AttrEhDefault},
		{"EF", AttrColGreen, AttrEhBlink},
		{"GH", AttrColDefault, AttrEhDefault},
	}
	if len(runs) < len(want) {
		t.Fatalf("expected at least %d runs, got %d: %+v", len(want), len(runs), runs)
	}
	for i, w := range want {
		// The last run runs on to the end of the field, which is where the
		// host stopped writing and the buffer is still empty. Only the text
		// the host put there is asserted.
		if got := strings.TrimRight(runs[i].Text, "\x00"); got != w.text {
			t.Errorf("run %d text: got %q want %q", i, got, w.text)
		}
		if runs[i].Color != w.color {
			t.Errorf("run %d colour: got %#02x want %#02x", i, runs[i].Color, w.color)
		}
		if runs[i].Highlight != w.highlight {
			t.Errorf("run %d highlight: got %#02x want %#02x", i, runs[i].Highlight, w.highlight)
		}
	}
}

// The alternate character set — the box drawing a panel is ruled with. It
// occupies a cell like any other character, and a decoder that does not
// unwrap the notation draws a hole where the line should be.
func TestConformanceDrawsGraphicEscapeCharacters(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).
		graphicEscape(0xC5).graphicEscape(0xA2).graphicEscape(0xD5).
		text("X").
		at(1, 0).field(0).cursor().
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	row := screenRow(s, 0)
	if strings.ContainsRune(row, 0) {
		t.Errorf("a graphic escape left a hole in the row: %q", row)
	}
	if len([]rune(row)) != 5 {
		t.Errorf("expected the attribute, three glyphs and an X, got %q", row)
	}
	if !strings.HasSuffix(row, "X") {
		t.Errorf("the character after a graphic escape should keep its column: %q", row)
	}
	for i, r := range []rune(row)[1:4] {
		if r < 0x80 {
			t.Errorf("glyph %d decoded as plain ASCII %q — the alternate set was not read", i, r)
		}
	}
}

// The Repeat to Address order fills a stretch of the screen with one
// character, which is how a host rules a line without sending eighty bytes of
// it. It has to leave the columns after it where they were.
func TestConformanceExpandsRepeatToAddress(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).repeat(0, 21, ebcdicByASCII['-']).
		at(0, 21).field(faProtected).text("END").
		at(1, 0).field(0).cursor().
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	row := screenRow(s, 0)
	if want := " " + strings.Repeat("-", 20) + " END"; row != want {
		t.Errorf("row 0: got %q want %q", row, want)
	}
}
