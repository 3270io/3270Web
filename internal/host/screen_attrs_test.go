// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"strings"
	"testing"
)

// The lines below are what a terminal actually answered a ReadBuffer with,
// against a host that wrote a blinking field, a reverse-video field, an
// underscored field, a field with an SA order colouring part of it, and a
// field with a background colour. They are kept verbatim rather than
// paraphrased: the point of these tests is the spelling the terminal uses, and
// a paraphrase is exactly where the spelling gets lost.
const (
	blinkLine   = "SF(c0=f0,42=f2,41=f1) 42 4c 49 4e 4b"
	runsLine    = "SF(c0=f0) 41 42 SA(42=f4) 43 44 SA(41=f1) 45 46 SA(42=00,41=f0) 47 48"
	bgLine      = "SF(c0=f0,45=f2) 42 47"
	plainLine   = "SF(c0=f0) 50 4c"
	statusForRC = "U F P C(localhost) I 2 %d %d 0 0 0x0 0.000"
)

func screenFromLines(t *testing.T, rows, cols int, lines ...string) *Screen {
	t.Helper()
	data := make([]string, 0, len(lines))
	for _, line := range lines {
		tokens := strings.Fields(line)
		for countPositions(tokens) < cols {
			tokens = append(tokens, "00")
		}
		data = append(data, "data: "+strings.Join(tokens, " "))
	}
	status := strings.Replace(strings.Replace(statusForRC, "%d", itoa(rows), 1), "%d", itoa(cols), 1)
	s := &Screen{}
	if err := s.Update(status, data); err != nil {
		t.Fatalf("Update: %v", err)
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// A blinking field arrives as 41=f1. It used to be compared against a value the
// attribute cannot hold, so it decoded cleanly and then named no style at all.
func TestBlinkingFieldKeepsItsHighlight(t *testing.T) {
	s := screenFromLines(t, 1, 20, blinkLine)
	if len(s.Fields) == 0 {
		t.Fatal("expected a field")
	}
	f := s.Fields[0]
	if f.ExtendedHighlight != AttrEhBlink {
		t.Errorf("expected blink highlight %#x, got %#x", AttrEhBlink, f.ExtendedHighlight)
	}
	if f.Color != AttrColRed {
		t.Errorf("expected red, got %#x", f.Color)
	}
}

// The explicit "no highlighting" value means the same as no attribute at all,
// and collapsing the two is what keeps every comparison downstream a single
// test against the default.
func TestNormalHighlightReadsAsNoHighlight(t *testing.T) {
	s := screenFromLines(t, 1, 20, "SF(c0=f0,41=f0) 41 42")
	if len(s.Fields) == 0 {
		t.Fatal("expected a field")
	}
	if got := s.Fields[0].ExtendedHighlight; got != AttrEhDefault {
		t.Errorf("expected the default highlight, got %#x", got)
	}
}

// An SA order colours a run inside a field. Reading only the field attribute
// shows the run in the field's colour, which is the wrong colour by exactly the
// amount the application was trying to say something.
func TestSetAttributeColoursARunInsideAField(t *testing.T) {
	s := screenFromLines(t, 1, 20, runsLine)
	if len(s.Fields) == 0 {
		t.Fatal("expected a field")
	}
	f := s.Fields[0]

	runs := f.AttrRuns()
	var joined strings.Builder
	for _, run := range runs {
		joined.WriteString(run.Text)
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
	}
	if len(runs) < len(want) {
		t.Fatalf("expected at least %d runs, got %d: %+v", len(want), len(runs), runs)
	}
	for i, w := range want {
		if runs[i].Text != w.text {
			t.Errorf("run %d text: got %q want %q", i, runs[i].Text, w.text)
		}
		if runs[i].Color != w.color {
			t.Errorf("run %d colour: got %#x want %#x", i, runs[i].Color, w.color)
		}
		if runs[i].Highlight != w.highlight {
			t.Errorf("run %d highlight: got %#x want %#x", i, runs[i].Highlight, w.highlight)
		}
	}
	// The closing SA resets both, so the tail of the field is back to the
	// field's own attributes.
	last := runs[len(runs)-1]
	if last.Color != AttrColDefault || last.Highlight != AttrEhDefault {
		t.Errorf("expected the reset run to carry the field's attributes, got %+v", last)
	}
}

// The character attribute an SA sets stays set: across the end of the row it
// was written on, and across the field attribute that opens the next field.
// Resetting it per row would recolour the screen at every row boundary.
func TestSetAttributeCarriesPastTheEndOfTheRow(t *testing.T) {
	s := screenFromLines(t, 2, 6, "SF(c0=f0) 41 SA(42=f4) 42", "SF(c0=f0) 43 44")
	if len(s.Fields) < 2 {
		t.Fatalf("expected two fields, got %d", len(s.Fields))
	}
	if got := s.CellAttrAt(1, 1); got.Color != AttrColGreen {
		t.Errorf("expected the colour to carry into the next row, got %+v", got)
	}
	runs := s.Fields[1].AttrRuns()
	if len(runs) == 0 || runs[0].Color != AttrColGreen {
		t.Errorf("expected the second field to open in the carried colour, got %+v", runs)
	}
}

// A screen with no SA orders on it carries no attribute grid at all, and its
// fields are a single run each — the shape the renderer has always been given.
func TestScreenWithoutCharacterAttributesAllocatesNoGrid(t *testing.T) {
	s := screenFromLines(t, 1, 10, plainLine)
	if s.CellAttrs != nil {
		t.Errorf("expected no attribute grid, got %d rows", len(s.CellAttrs))
	}
	runs := s.Fields[0].AttrRuns()
	if len(runs) != 1 {
		t.Fatalf("expected one run, got %d: %+v", len(runs), runs)
	}
	if runs[0].Text != s.Fields[0].GetValue() {
		t.Errorf("run text: got %q want %q", runs[0].Text, s.Fields[0].GetValue())
	}
}

// Background colour is a separate attribute from the foreground, and the
// terminal reports it on the start field.
func TestBackgroundColourReachesTheField(t *testing.T) {
	s := screenFromLines(t, 1, 10, bgLine)
	if len(s.Fields) == 0 {
		t.Fatal("expected a field")
	}
	if got := s.Fields[0].Background; got != AttrColRed {
		t.Errorf("expected a red background, got %#x", got)
	}
}

// An SA order occupies no cell. Counting it as one puts every row break a
// column early, which wraps the rest of the screen in the wrong place.
func TestSetAttributeOccupiesNoScreenPosition(t *testing.T) {
	s := screenFromLines(t, 1, 6, "SF(c0=f0) 41 SA(42=f4) 42 43 44 45")
	if s.Width != 6 {
		t.Fatalf("expected width 6, got %d", s.Width)
	}
	if got := s.Text(); strings.TrimRight(strings.Split(got, "\n")[0], " \x00") != " ABCDE" {
		t.Errorf("expected the SA to take no column, got %q", got)
	}
}

// A single-line buffer is split into rows by counting cells, not tokens.
func TestSingleLineBufferSplitsOnPositionsNotTokens(t *testing.T) {
	line := "data: SF(c0=f0) 41 SA(42=f4) 42 43 44 45"
	rows := normalizeScreenTokens([]string{line}, 2, 3)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}
	for i, row := range rows {
		if got := countPositions(row); got != 3 {
			t.Errorf("row %d holds %d positions, want 3: %+v", i, got, row)
		}
	}
}

// A character outside ASCII in the last column of a row is spelled as several
// bytes like any other, and the run that closes a line is read the same way as
// one in the middle of it.
func TestMultibyteCharacterInTheFinalColumnSurvives(t *testing.T) {
	s := screenFromLines(t, 1, 3, "SF(c0=f0) 41 c2a3")
	if got := s.CharAt(2, 0); got != '£' {
		t.Errorf("expected a pound sign in the last column, got %q", got)
	}
}

// A graphic-escape names one character of the alternate set — the box drawing
// and APL glyphs a panel is ruled with. It occupies a cell like any other
// character, and a decoder that does not unwrap the notation draws a hole
// where the line should be.
func TestGraphicEscapeCharacterIsDrawn(t *testing.T) {
	s := screenFromLines(t, 1, 4, "SF(c0=f0) GE(e29480) GE(2d) 41")
	if got := s.CharAt(1, 0); got != '─' {
		t.Errorf("expected a box-drawing character, got %q", got)
	}
	if got := s.CharAt(2, 0); got != '-' {
		t.Errorf("expected a single-byte graphic escape to decode, got %q", got)
	}
	if got := s.CharAt(3, 0); got != 'A' {
		t.Errorf("expected the character after a graphic escape to keep its column, got %q", got)
	}
}
