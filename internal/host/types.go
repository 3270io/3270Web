// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"strings"
)

// Field Attributes (c0 mask)
const (
	AttrProtected = 0x20
	AttrNumeric   = 0x10
	AttrDisp1     = 0x08
	AttrDisp2     = 0x04
)

// Extended Highlight Attributes (41 mask)
//
// These are the values the 3270 data stream carries and the terminal reports
// back, not a local encoding: a blinking field arrives as "41=f1". Blink used
// to be spelled 0x80 here, which is not a value the attribute can hold, so the
// comparison never matched and a host that asked for a blinking field got a
// steady one — the attribute was decoded, carried the whole way to the
// renderer, and then quietly failed to name a style.
//
// AttrEhNormal is the explicit "no highlighting" value. It means the same as
// the absent-attribute default and is normalised to it on the way in, so that
// nothing downstream has to know there are two spellings of nothing.
const (
	AttrEhDefault    = 0x00
	AttrEhNormal     = 0xF0
	AttrEhBlink      = 0xF1
	AttrEhRevVideo   = 0xF2
	AttrEhUnderscore = 0xF4
)

// Color Attributes (42 and 45 masks)
//
// The same eight values serve both the foreground attribute and the background
// one. AttrColNeutral is the eighth: as a background it is the screen's own
// ground, which is why it has no place in the foreground switch — a character
// painted in it would be invisible, and a host that means "default" says so by
// sending no attribute at all.
const (
	AttrColDefault   = 0x00
	AttrColNeutral   = 0xF0
	AttrColBlue      = 0xF1
	AttrColRed       = 0xF2
	AttrColPink      = 0xF3
	AttrColGreen     = 0xF4
	AttrColTurquoise = 0xF5
	AttrColYellow    = 0xF6
	AttrColWhite     = 0xF7
)

// CellAttr is the character-level extended attribute at one buffer position:
// what an SA order in the data stream set for the characters that followed it.
//
// A field attribute opens a field and colours all of it. An SA order colours a
// run *inside* one — the four words of a message that are red where the rest of
// the line is green, a total picked out in reverse video. The two are set by
// different orders, and a terminal that reads only the first shows the run in
// the field's colour, which is the wrong colour by exactly the amount the
// application was trying to say something.
//
// A zero in any of these means "this position says nothing", and the field's own
// attribute answers instead. That is the architecture's rule rather than a
// convenience: the character attribute overrides the field attribute where it is
// set, and defers to it where it is not.
type CellAttr struct {
	Color      uint8
	Highlight  uint8
	Background uint8
}

// IsZero reports whether the position carries no character-level attribute of
// its own.
func (c CellAttr) IsZero() bool {
	return c.Color == 0 && c.Highlight == 0 && c.Background == 0
}

// Display Modes
const (
	DisplayNormal      = 0
	DisplayIntensified = 1
	DisplayHidden      = 2
)

// Screen represents the state of a 3270 screen.
type Screen struct {
	Width       int
	Height      int
	Buffer      [][]rune // 2D array of characters [row][col]
	Fields      []*Field
	CursorX     int
	CursorY     int
	IsFormatted bool
	Status      string

	// CellAttrs holds the character-level extended attributes, [row][col],
	// parallel to Buffer. It is nil on the ordinary screen — most screens set
	// no character attributes at all, and an empty grid per screen would be
	// paid for on every read and again on every entry in the history.
	CellAttrs [][]CellAttr
}

// Clone returns a deep copy of the screen and its fields.
func (s *Screen) Clone() *Screen {
	if s == nil {
		return nil
	}
	out := &Screen{
		Width:       s.Width,
		Height:      s.Height,
		CursorX:     s.CursorX,
		CursorY:     s.CursorY,
		IsFormatted: s.IsFormatted,
		Status:      s.Status,
	}
	if len(s.Buffer) > 0 {
		out.Buffer = make([][]rune, len(s.Buffer))
		for i := range s.Buffer {
			out.Buffer[i] = append([]rune(nil), s.Buffer[i]...)
		}
	}
	if len(s.CellAttrs) > 0 {
		out.CellAttrs = make([][]CellAttr, len(s.CellAttrs))
		for i := range s.CellAttrs {
			out.CellAttrs[i] = append([]CellAttr(nil), s.CellAttrs[i]...)
		}
	}
	if len(s.Fields) > 0 {
		out.Fields = make([]*Field, 0, len(s.Fields))
		for _, f := range s.Fields {
			if f == nil {
				out.Fields = append(out.Fields, nil)
				continue
			}
			nf := *f
			nf.Screen = out
			out.Fields = append(out.Fields, &nf)
		}
	}
	return out
}

// Field represents a region on the screen with specific attributes.
// It combines both Field and InputField concepts from the Java code.
type Field struct {
	Screen *Screen

	StartX, StartY int
	EndX, EndY     int

	// Attributes
	FieldCode         byte
	Color             int
	ExtendedHighlight int
	Background        int

	// State
	Focused bool
	Changed bool
	Value   string // Cached value
}

// NewField creates a new field.
func NewField(screen *Screen, code byte, startX, startY, endX, endY, color, eh int) *Field {
	return &Field{
		Screen:            screen,
		FieldCode:         code,
		StartX:            startX,
		StartY:            startY,
		EndX:              endX,
		EndY:              endY,
		Color:             color,
		ExtendedHighlight: eh,
	}
}

// IsProtected returns true if the field is protected (read-only).
func (f *Field) IsProtected() bool {
	return (f.FieldCode & AttrProtected) != 0
}

// IsNumeric returns true if the field is numeric-only.
func (f *Field) IsNumeric() bool {
	return (f.FieldCode & AttrNumeric) != 0
}

// IsHidden returns true if the field is hidden (e.g. password).
func (f *Field) IsHidden() bool {
	return f.DisplayMode() == DisplayHidden
}

// IsIntensified returns true if the field is high intensity.
func (f *Field) IsIntensified() bool {
	return f.DisplayMode() == DisplayIntensified
}

// DisplayMode calculates the display mode from the field code.
func (f *Field) DisplayMode() int {
	if (f.FieldCode & AttrDisp1) == 0 {
		return DisplayNormal
	} else if (f.FieldCode & AttrDisp2) == 0 {
		return DisplayIntensified
	} else {
		return DisplayHidden
	}
}

// GetValue returns the text content of the field.
// It lazily extracts it from the screen buffer if not already set.
func (f *Field) GetValue() string {
	if f.Value == "" {
		if f.IsZeroLength() {
			return ""
		}
		f.Value = f.Screen.Substring(f.StartX, f.StartY, f.EndX, f.EndY)
	}
	return f.Value
}

// SetValue updates the field value and marks it as changed.
// This does not update the screen buffer, only the local field state.
func (f *Field) SetValue(newValue string) {
	// Simple trim or logic? Java does trim.
	// We'll trust the caller or just set it.
	if f.Value != newValue {
		f.Value = newValue
		f.Changed = true
	}
}

// GetValueLines returns the value split by lines.
func (f *Field) GetValueLines() []string {
	return strings.Split(f.GetValue(), "\n")
}

// IsZeroLength reports whether the field holds no screen positions of its own.
//
// Two field attributes written side by side produce one: the second closes the
// field the first opened before it has reached a single position, so the end
// lands one place before the start. Hosts do this deliberately — a zero-length
// field is how a screen stops an entry field from running on, and a map with a
// column of them is ordinary rather than malformed.
//
// The field is still real. It owns its attribute byte, which is a screen
// position like any other, so it has to stay in the field list for the columns
// after it to land where the host put them. What it does not have is text, and
// asking for its value walks the buffer from a start that is already past the
// end — which is what Substring is asked not to do.
func (f *Field) IsZeroLength() bool {
	return f.EndY < f.StartY || (f.EndY == f.StartY && f.EndX < f.StartX)
}

// IsMultiline returns true if the field spans multiple lines.
func (f *Field) IsMultiline() bool {
	return f.EndY > f.StartY
}

// Height returns the number of lines the field spans.
func (f *Field) Height() int {
	return f.EndY - f.StartY + 1
}

// Substring extracts text from the screen buffer.
// Note: This matches the Java implementation logic.
func (s *Screen) Substring(startX, startY, endX, endY int) string {
	var sb strings.Builder

	// Traverse from start to end
	curX, curY := startX, startY
	for {
		// Append char at current pos
		if curY < len(s.Buffer) && curX < len(s.Buffer[curY]) {
			sb.WriteRune(s.Buffer[curY][curX])
		}

		// Check if we reached the end
		if curX == endX && curY == endY {
			break
		}

		// Advance
		curX++
		if curX >= s.Width {
			curX = 0
			curY++
			// Wrap around check (though endY should prevent going out of bounds)
			if curY >= s.Height {
				break
			}
			// Add newline for multiline fields if crossing line boundary?
			// Java code implementation of substring(startx, starty, endx, endy):
			// "return the region as a String, with line breaks (newline characters) inserted"
			if curY <= endY {
				sb.WriteRune('\n')
			}
		}
	}
	return sb.String()
}

// CellAttrAt returns the character-level attribute at a position, or the zero
// value where the screen carries none — which is the ordinary case and means
// "the field decides".
func (s *Screen) CellAttrAt(x, y int) CellAttr {
	if y < 0 || y >= len(s.CellAttrs) {
		return CellAttr{}
	}
	row := s.CellAttrs[y]
	if x < 0 || x >= len(row) {
		return CellAttr{}
	}
	return row[x]
}

// AttrRun is a stretch of one field's text whose display attributes are all the
// same. A field with no character attributes inside it is one run, which is the
// field itself and renders exactly as it always did.
type AttrRun struct {
	Text       string
	Color      int
	Highlight  int
	Background int
}

// attrAt resolves the attributes in force at one position of f: the character
// attribute where the position sets one, and the field's own where it does not.
func (f *Field) attrAt(x, y int) AttrRun {
	run := AttrRun{Color: f.Color, Highlight: f.ExtendedHighlight, Background: f.Background}
	if f.Screen == nil {
		return run
	}
	cell := f.Screen.CellAttrAt(x, y)
	if cell.Color != 0 {
		run.Color = int(cell.Color)
	}
	if cell.Highlight != 0 {
		run.Highlight = int(cell.Highlight)
	}
	if cell.Background != 0 {
		run.Background = int(cell.Background)
	}
	return run
}

// AttrRuns splits the field's text at every point its display attributes
// change.
//
// Concatenating the runs gives back exactly GetValue(), row separators
// included, so a caller that ignores the attributes and joins them is where it
// was before. A row separator belongs to the run it ends rather than to the one
// that follows, which keeps the common case — no character attributes anywhere
// — a single run holding the whole value.
func (f *Field) AttrRuns() []AttrRun {
	base := AttrRun{Color: f.Color, Highlight: f.ExtendedHighlight, Background: f.Background}
	if f.IsZeroLength() {
		return nil
	}
	if f.Screen == nil || len(f.Screen.CellAttrs) == 0 {
		base.Text = f.GetValue()
		if base.Text == "" {
			return nil
		}
		return []AttrRun{base}
	}

	s := f.Screen
	var runs []AttrRun
	var sb strings.Builder
	current := f.attrAt(f.StartX, f.StartY)
	flush := func() {
		if sb.Len() == 0 {
			return
		}
		current.Text = sb.String()
		runs = append(runs, current)
		sb.Reset()
	}

	curX, curY := f.StartX, f.StartY
	for {
		if next := f.attrAt(curX, curY); next != current {
			flush()
			current = next
		}
		if curY < len(s.Buffer) && curX < len(s.Buffer[curY]) {
			sb.WriteRune(s.Buffer[curY][curX])
		}
		if curX == f.EndX && curY == f.EndY {
			break
		}
		curX++
		if curX >= s.Width {
			curX = 0
			curY++
			if curY >= s.Height {
				break
			}
			if curY <= f.EndY {
				sb.WriteRune('\n')
			}
		}
	}
	flush()
	return runs
}

// CharAt returns the character at a position or 0 if out of bounds.
func (s *Screen) CharAt(x, y int) rune {
	if y < 0 || y >= len(s.Buffer) {
		return 0
	}
	row := s.Buffer[y]
	if x < 0 || x >= len(row) {
		return 0
	}
	return row[x]
}

// Text returns the full screen text with newline separators.
func (s *Screen) Text() string {
	if s.Height == 0 {
		return ""
	}
	width := s.Width
	if width == 0 {
		for _, row := range s.Buffer {
			if len(row) > width {
				width = len(row)
			}
		}
	}
	var sb strings.Builder
	for y := 0; y < s.Height; y++ {
		for x := 0; x < width; x++ {
			ch := s.CharAt(x, y)
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		if y < s.Height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// UpdateFromText replaces the buffer using plain text (unformatted screens).
func (s *Screen) UpdateFromText(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	s.Height = len(lines)
	if s.Height == 0 {
		s.Buffer = nil
		s.Width = 0
		return
	}
	width := 0
	for _, line := range lines {
		if len(line) > width {
			width = len(line)
		}
	}
	s.Width = width
	s.Buffer = make([][]rune, s.Height)
	for y, line := range lines {
		row := make([]rune, width)
		for x, r := range line {
			if x >= width {
				break
			}
			row[x] = r
		}
		s.Buffer[y] = row
	}
}

// GetInputFieldAt returns the input field at the given coordinates, or nil.
func (s *Screen) GetInputFieldAt(x, y int) *Field {
	for _, f := range s.Fields {
		if f.IsProtected() {
			continue
		}
		if s.contains(f, x, y) {
			return f
		}
	}
	return nil
}

func (s *Screen) contains(f *Field, x, y int) bool {
	// Simple case: single line
	if f.StartY == f.EndY {
		return y == f.StartY && x >= f.StartX && x <= f.EndX
	}
	// Multi-line
	if y > f.StartY && y < f.EndY {
		return true
	}
	if y == f.StartY {
		return x >= f.StartX
	}
	if y == f.EndY {
		return x <= f.EndX
	}
	return false
}
