package main

import (
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
)

// This file holds the single definition of what a hidden 3270 field is
// allowed to disclose. Every serializer that turns a *host.Screen into JSON
// goes through these helpers.
//
// The rule lives in one place deliberately. 3270 "hidden" only suppresses
// local echo on a real terminal — the typed characters are still present in
// the buffer s3270 hands us, so anything that renders the buffer verbatim
// publishes passwords. There are two screen serializers today
// (screenToJSON for the browser panel, screenToPublicJSON for /api/v1) and
// the second one shipped without the redaction the first one has. Keeping
// the rule beside its callers is what stops a third from repeating it.

// visibleFieldValue returns what a field may disclose: its value, or the
// empty string when the field is hidden.
func visibleFieldValue(f *host.Field) string {
	if f == nil || f.IsHidden() {
		return ""
	}
	return f.GetValue()
}

// redactHiddenFieldText returns the screen's text with any hidden (e.g.
// password) field's characters replaced by '*'.
func redactHiddenFieldText(s *host.Screen) string {
	if s == nil {
		return ""
	}
	text := s.Text()
	if s.Width <= 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	for _, f := range s.Fields {
		if f == nil || !f.IsHidden() {
			continue
		}
		curX, curY := f.StartX, f.StartY
		endX, endY := f.EndX, f.EndY
		for {
			if curY >= 0 && curY < len(lines) {
				line := []rune(lines[curY])
				if curX >= 0 && curX < len(line) {
					line[curX] = '*'
					lines[curY] = string(line)
				}
			}
			if curX == endX && curY == endY {
				break
			}
			curX++
			if curX >= s.Width {
				curX = 0
				curY++
				if curY >= s.Height {
					break
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// fieldLength reports how many character positions a field covers.
func fieldLength(f *host.Field) int {
	if f == nil {
		return 0
	}
	if f.EndY == f.StartY {
		return f.EndX - f.StartX + 1
	}
	// Multi-line field: approximate length as the rune count of the value.
	return len([]rune(f.GetValue()))
}
