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
const (
	AttrEhDefault    = 0x00
	AttrEhBlink      = 0x80
	AttrEhRevVideo   = 0xF2
	AttrEhUnderscore = 0xF4
)

// Color Attributes (42 mask)
const (
	AttrColDefault   = 0x00
	AttrColBlue      = 0xF1
	AttrColRed       = 0xF2
	AttrColPink      = 0xF3
	AttrColGreen     = 0xF4
	AttrColTurquoise = 0xF5
	AttrColYellow    = 0xF6
	AttrColWhite     = 0xF7
)

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
