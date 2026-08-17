// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"strconv"
	"strings"
)

// Operator Information Area input-inhibit indicators. These are the literal
// strings a 3270 operator is trained to read: a locked keyboard shows
// "X SYSTEM" while the host is thinking, and an operator error shows "X -f"
// until Reset is pressed.
const (
	OIAReady       = ""
	OIASystemWait  = "X SYSTEM"
	OIAOperatorErr = "X -f"
)

// OIAIndicator returns the input-inhibit indicator for the current keyboard
// state, plus a plain-language explanation for tooltips and screen readers.
func (s *Screen) OIAIndicator() (indicator, explanation string) {
	state, ok := s.StatusKeyboardState()
	if !ok {
		return OIAReady, ""
	}
	switch state {
	case "U":
		return OIAReady, "Ready for input"
	case "L":
		return OIASystemWait, "Host is processing — input inhibited"
	case "E":
		return OIAOperatorErr, "Operator error — press Reset"
	default:
		return OIASystemWait, "Input inhibited"
	}
}

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

// StatusConnection returns the raw connection field from the status line:
// "N" when not connected, otherwise "C(hostname)".
func (s *Screen) StatusConnection() (string, bool) {
	parts := statusParts(s.Status)
	if parts == nil {
		return "", false
	}
	return parts[statusIdxConnection], true
}

// StatusConnected reports whether s3270 holds a host connection, along with
// the peer name s3270 reports for it. The third return distinguishes "not
// connected" from "no usable status line to read".
func (s *Screen) StatusConnected() (connected bool, peer string, ok bool) {
	field, ok := s.StatusConnection()
	if !ok {
		return false, "", false
	}
	if field == "N" {
		return false, "", true
	}
	peer = strings.TrimSuffix(strings.TrimPrefix(field, "C("), ")")
	return true, peer, true
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
