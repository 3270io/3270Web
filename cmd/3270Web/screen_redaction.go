// SPDX-License-Identifier: AGPL-3.0-or-later

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

// hiddenCells returns the linear buffer positions covered by hidden fields,
// counting row by row from zero. It is the one place that walks a hidden
// field's extent; both the screen serializers and the buffer reads in
// readbuffer.go ask it rather than repeating the walk, since a second copy of
// this loop is a second chance to get the wrap at the right-hand edge wrong and
// publish the last character of a password.
func hiddenCells(s *host.Screen) map[int]bool {
	out := make(map[int]bool)
	if s == nil || s.Width <= 0 {
		return out
	}
	for _, f := range s.Fields {
		if f == nil || !f.IsHidden() {
			continue
		}
		curX, curY := f.StartX, f.StartY
		endX, endY := f.EndX, f.EndY
		for {
			if curY >= 0 && curX >= 0 {
				out[curY*s.Width+curX] = true
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
	return out
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
	hidden := hiddenCells(s)
	if len(hidden) == 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	for pos := range hidden {
		y, x := pos/s.Width, pos%s.Width
		if y < 0 || y >= len(lines) {
			continue
		}
		line := []rune(lines[y])
		if x < len(line) {
			line[x] = '*'
			lines[y] = string(line)
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
