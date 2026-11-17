// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"errors"
	"fmt"
	"strings"
)

// Writing into one field, as a caller that cannot see the screen needs it done.
//
// WriteStringAt moves the cursor and types. That is the whole of it, and for a
// person at a keyboard it is enough: they can see that the cursor is in a
// label, that the old value is longer than the new one, that the keyboard has
// locked. A program driving the terminal by coordinates sees none of that, and
// each of those turned into a quiet wrong answer:
//
//   - typing LEE over SMITH left LEETH, because nothing erased the field;
//   - an empty string did nothing at all and reported success, so "clear this
//     field" could not be said;
//   - a write aimed at a protected cell came back as "Keyboard locked;
//     Operator error" and left the keyboard locked for whatever came next;
//   - and the caller was told "ok" either way, with no account of what the
//     field now held.
//
// WriteField looks first. It reads the terminal's own buffer, finds the field
// the position belongs to, and refuses — before a single keystroke — a write
// that the terminal would refuse or that would run past the end of the field.
// It does the looking and the typing under one hold of the session, so the
// screen it checked is the screen it types into.

// FieldWriter is implemented by hosts that can write into a field with those
// checks. It is separate from Host because Host has implementations outside
// this package that have no buffer to look at.
type FieldWriter interface {
	WriteField(row, col int, text string, erase bool) (*FieldWrite, error)
}

// FieldWrite describes the field after a write.
type FieldWrite struct {
	// Row and Col are the 0-based position of the field's first content cell,
	// which is not necessarily where the write started.
	Row int
	Col int
	// Length is the number of content cells in the field.
	Length int
	// Value is the field's content after the write, with trailing blanks
	// removed. It is the real content: masking a hidden field is the business
	// of whoever serialises this, alongside every other place a hidden value
	// is kept from leaving the server.
	Value   string
	Hidden  bool
	Numeric bool
	// Formatted is false when the screen has no fields and the write went
	// into the bare buffer.
	Formatted bool
}

// FieldWriteError is a refusal: the write was not attempted, or was attempted
// and the terminal rejected it. Either way the caller asked for something the
// screen does not permit, which is a different thing from the session failing.
type FieldWriteError struct {
	Reason string
	// KeyboardReset is true when the terminal rejected the write with an
	// operator error and Reset was sent to release the keyboard.
	KeyboardReset bool
}

func (e *FieldWriteError) Error() string { return e.Reason }

// IsFieldWriteError reports whether err is a refusal rather than a failure.
func IsFieldWriteError(err error) bool {
	var target *FieldWriteError
	return errors.As(err, &target)
}

// fieldTarget is what a position turned out to be.
type fieldTarget struct {
	field     *BufferField
	offset    int // cells between the field's first content cell and the position
	remaining int // cells from the position to the end of the field
}

// locateFieldTarget finds the field a position belongs to and decides whether
// text may be typed there. It is a pure function of the parsed buffer so the
// rules can be tested without a terminal.
func locateFieldTarget(cells [][]bufferCell, row, col int, textLen int) (*fieldTarget, error) {
	if row < 0 || col < 0 {
		return nil, &FieldWriteError{Reason: "row and col must not be negative"}
	}
	flat, cols := flattenCells(cells)
	if len(flat) == 0 {
		return nil, fmt.Errorf("the terminal reported an empty screen buffer")
	}
	if row >= len(cells) || col >= cols {
		return nil, &FieldWriteError{Reason: fmt.Sprintf(
			"row %d, col %d is outside the %d-row, %d-column screen (rows and columns are 0-indexed)",
			row, col, len(cells), cols)}
	}
	pos := row*cols + col

	field, err := fieldAt(cells, nil, row, col)
	if err != nil {
		return nil, err
	}
	if !field.Formatted {
		return &fieldTarget{field: field, offset: pos, remaining: len(flat) - pos}, nil
	}

	if flat[pos].startField {
		return nil, &FieldWriteError{Reason: fmt.Sprintf(
			"row %d, col %d is a field attribute position, not a place text can go; "+
				"the field it opens starts at row %d, col %d",
			row, col, field.Row, field.Col)}
	}
	if field.Protected {
		return nil, &FieldWriteError{Reason: fmt.Sprintf(
			"row %d, col %d is in a protected field (%s), not an input field; "+
				"use the row and col of an unprotected field from get_screen",
			row, col, describeProtected(field))}
	}

	start := field.Row*cols + field.Col
	offset := ((pos-start)%len(flat) + len(flat)) % len(flat)
	remaining := field.Length - offset
	if textLen > remaining {
		return nil, &FieldWriteError{Reason: fmt.Sprintf(
			"the text is %d characters but only %d fit from row %d, col %d to the end of the field "+
				"(the field starts at row %d, col %d and is %d long)",
			textLen, remaining, row, col, field.Row, field.Col, field.Length)}
	}
	return &fieldTarget{field: field, offset: offset, remaining: remaining}, nil
}

// describeProtected says what is at a protected position in a way that helps
// the caller find where it meant to write.
func describeProtected(f *BufferField) string {
	text := strings.Join(strings.Fields(f.Text), " ")
	if f.Hidden || text == "" {
		return fmt.Sprintf("starting at row %d, col %d", f.Row, f.Col)
	}
	const maxShown = 40
	if r := []rune(text); len(r) > maxShown {
		text = string(r[:maxShown]) + "…"
	}
	return fmt.Sprintf("%q, starting at row %d, col %d", text, f.Row, f.Col)
}

// WriteField writes text into the unprotected field containing (row, col),
// both 0-based, starting at that position.
//
// With erase set, everything from the position to the end of the field is
// cleared first, so the field ends up holding exactly what was written rather
// than the new text followed by the tail of the old. Empty text with erase set
// clears the field from that position. With erase unset the text overtypes
// what is there and the rest is left alone.
func (h *S3270) WriteField(row, col int, text string, erase bool) (*FieldWrite, error) {
	if ContainsForbiddenFieldText(text) {
		return nil, &FieldWriteError{Reason: "text must not contain CR/LF/TAB"}
	}
	if err := h.lockForInteraction(); err != nil {
		return nil, err
	}
	defer h.mu.Unlock()

	lines, err := h.readBufferLocked("Ascii")
	if err != nil {
		if refusalMentions(err, "keyboard locked") {
			return nil, &FieldWriteError{Reason: "the keyboard is locked while the host is processing; wait for it to unlock before writing"}
		}
		return nil, err
	}
	cells := parseReadBuffer(lines)
	target, err := locateFieldTarget(cells, row, col, len([]rune(text)))
	if err != nil {
		return nil, err
	}
	hidden := target.field.Hidden

	steps := []struct {
		cmd, logCmd string
	}{{cmd: fmt.Sprintf("movecursor(%d, %d)", row, col)}}
	// EraseEOF on a screen with no fields clears to the end of the buffer,
	// which is the rest of the screen rather than the rest of a field.
	if erase && target.field.Formatted {
		steps = append(steps, struct{ cmd, logCmd string }{cmd: "eraseeof"})
	}
	if text != "" {
		step := struct{ cmd, logCmd string }{cmd: fmt.Sprintf(`String("%s")`, escapeForS3270String(text))}
		if hidden {
			// What goes into a non-display field is a password or something
			// treated like one. It is typed, and it is not written down.
			step.logCmd = `String("***")`
		}
		steps = append(steps, step)
	}

	for _, step := range steps {
		logCmd := step.logCmd
		if logCmd == "" {
			logCmd = step.cmd
		}
		data, status, cmdErr := h.executeCommandLocked(step.cmd, logCmd)
		h.logAction(logCmd, status)
		if cmdErr == nil && !isS3270Error(status, data) {
			continue
		}
		if cmdErr != nil && !IsActionError(cmdErr) {
			// The pipe, not the screen.
			return nil, cmdErr
		}
		// The terminal refused the keystroke. A refused keystroke leaves the
		// keyboard inhibited ("X -f" on a real terminal) until Reset, and a
		// caller that cannot see the indicator would otherwise find every
		// later action refused for a reason unrelated to it.
		reset := h.resetKeyboardLocked()
		reason := "the terminal refused the write"
		if actionErr, ok := AsActionError(cmdErr); ok && strings.TrimSpace(actionErr.Detail) != "" {
			reason += ": " + strings.TrimSpace(actionErr.Detail)
		}
		if target.field.Numeric {
			reason += " (this is a numeric field)"
		}
		if reset {
			reason += "; the keyboard was reset and nothing further was typed"
		}
		return nil, &FieldWriteError{Reason: reason, KeyboardReset: reset}
	}

	out := &FieldWrite{
		Row:       target.field.Row,
		Col:       target.field.Col,
		Length:    target.field.Length,
		Hidden:    hidden,
		Numeric:   target.field.Numeric,
		Formatted: target.field.Formatted,
	}
	// Read the field back rather than reporting what was asked for: the
	// terminal is what decides what the field holds.
	if after, readErr := h.readBufferLocked("Ascii"); readErr == nil {
		if f, atErr := fieldAt(parseReadBuffer(after), nil, row, col); atErr == nil {
			out.Value = strings.TrimRight(f.Text, " \x00")
			if !f.Formatted {
				// The whole buffer is not a useful echo; report what was typed.
				out.Value = text
			}
			return out, nil
		}
	}
	out.Value = text
	return out, nil
}

// resetKeyboardLocked sends Reset and reports whether the terminal took it.
// The caller must hold h.mu.
func (h *S3270) resetKeyboardLocked() bool {
	data, status, err := h.doCommandLocked("Reset")
	h.logAction("Reset", status)
	return err == nil && !isS3270Error(status, data)
}

// WriteField on the sample-app host is the terminal's own.
func (h *GoSampleAppHost) WriteField(row, col int, text string, erase bool) (*FieldWrite, error) {
	if h.client == nil {
		return nil, errors.New(sampleAppClientNotStarted)
	}
	return h.client.WriteField(row, col, text, erase)
}

// WriteField applies the same rules to the mock's own screen, so a handler
// test exercises the refusal paths without a terminal.
func (m *MockHost) WriteField(row, col int, text string, erase bool) (*FieldWrite, error) {
	if ContainsForbiddenFieldText(text) {
		return nil, &FieldWriteError{Reason: "text must not contain CR/LF/TAB"}
	}
	if row < 0 || col < 0 {
		return nil, &FieldWriteError{Reason: "row and col must not be negative"}
	}
	if m.Screen == nil || len(m.Screen.Fields) == 0 {
		// No field structure: the bare buffer, as on an unformatted screen.
		if err := m.WriteStringAt(row, col, text); err != nil {
			return nil, err
		}
		return &FieldWrite{Row: row, Col: col, Length: len([]rune(text)), Value: text}, nil
	}
	for _, f := range m.Screen.Fields {
		if f == nil || f.IsZeroLength() || !m.Screen.contains(f, col, row) {
			continue
		}
		if f.IsProtected() {
			return nil, &FieldWriteError{Reason: fmt.Sprintf(
				"row %d, col %d is in a protected field (%q, starting at row %d, col %d), not an input field; "+
					"use the row and col of an unprotected field from get_screen",
				row, col, strings.TrimSpace(f.GetValue()), f.StartY, f.StartX)}
		}
		length := f.EndX - f.StartX + 1
		remaining := f.EndX - col + 1
		runes := []rune(text)
		if len(runes) > remaining {
			return nil, &FieldWriteError{Reason: fmt.Sprintf(
				"the text is %d characters but only %d fit from row %d, col %d to the end of the field "+
					"(the field starts at row %d, col %d and is %d long)",
				len(runes), remaining, row, col, f.StartY, f.StartX, length)}
		}
		m.Commands = append(m.Commands, "write")
		if erase {
			for x := col; x <= f.EndX; x++ {
				m.Screen.Buffer[row][x] = ' '
			}
		}
		for i, r := range runes {
			m.Screen.Buffer[row][col+i] = r
		}
		f.Value = ""
		value := strings.TrimRight(strings.ReplaceAll(string(m.Screen.Buffer[f.StartY][f.StartX:f.EndX+1]), "\x00", " "), " ")
		f.Value = value
		return &FieldWrite{
			Row: f.StartY, Col: f.StartX, Length: length, Value: value,
			Hidden: f.IsHidden(), Numeric: f.IsNumeric(), Formatted: true,
		}, nil
	}
	return nil, &FieldWriteError{Reason: fmt.Sprintf(
		"row %d, col %d is not inside any field on this screen", row, col)}
}
