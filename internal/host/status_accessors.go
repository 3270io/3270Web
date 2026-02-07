package host

import (
	"strconv"
	"strings"
)

func statusParts(status string) []string {
	if status == "" {
		return nil
	}
	parts := strings.Fields(status)
	if len(parts) < statusMinFields {
		return nil
	}
	return parts
}

// StatusKeyboardState returns the raw keyboard state ("U", "L", "E") from the status line.
func (s *Screen) StatusKeyboardState() (string, bool) {
	parts := statusParts(s.Status)
	if parts == nil {
		return "", false
	}
	return parts[statusIdxKeyboard], true
}

// StatusKeyboardLocked reports whether the keyboard is locked.
func (s *Screen) StatusKeyboardLocked() (bool, bool) {
	state, ok := s.StatusKeyboardState()
	if !ok {
		return false, false
	}
	return state != "U", true
}

// StatusModel returns the model number from the status line.
func (s *Screen) StatusModel() (string, bool) {
	parts := statusParts(s.Status)
	if parts == nil {
		return "", false
	}
	return parts[statusIdxModel], true
}

// StatusDimensions returns the row/column dimensions reported in the status line.
func (s *Screen) StatusDimensions() (int, int, bool) {
	parts := statusParts(s.Status)
	if parts == nil {
		return 0, 0, false
	}
	rows, err := strconv.Atoi(parts[statusIdxRows])
	if err != nil {
		return 0, 0, false
	}
	cols, err := strconv.Atoi(parts[statusIdxCols])
	if err != nil {
		return 0, 0, false
	}
	return rows, cols, true
}

// StatusCursor returns the cursor row/column reported in the status line (0-based).
func (s *Screen) StatusCursor() (int, int, bool) {
	parts := statusParts(s.Status)
	if parts == nil {
		return 0, 0, false
	}
	row, err := strconv.Atoi(parts[statusIdxCursorRow])
	if err != nil {
		return 0, 0, false
	}
	col, err := strconv.Atoi(parts[statusIdxCursorCol])
	if err != nil {
		return 0, 0, false
	}
	return row, col, true
}
