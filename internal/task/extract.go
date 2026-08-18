// SPDX-License-Identifier: AGPL-3.0-or-later

package task

import (
	"regexp"
	"strings"
)

// Reading the answer off a screen.
//
// Two callers need this. The runner reads the answer from the live terminal
// at the end of a run; the authoring wizard reads it from a screen that has
// already been captured, so the person marking a region can see what that
// region actually captures — and what a pattern extracts from it — before the
// task is saved rather than after the first run against a customer's account.
//
// They share one implementation deliberately. A preview computed by its own
// rules is worth nothing, because the only claim it makes is "this is what
// the run will do".

// regionReader returns the text of a screen region: 1-based row and column,
// length characters, clamped to the row it starts on.
type regionReader func(row, col, length int) string

// textRegionReader reads regions out of a screen that is already text — a
// recorded screen, or one the browser sent back.
func textRegionReader(screen string) regionReader {
	lines := strings.Split(screen, "\n")
	return func(row, col, length int) string {
		if length <= 0 || row <= 0 || col <= 0 || row > len(lines) {
			return ""
		}
		// Runes, not bytes: the column a browser reports is a character
		// offset, and slicing bytes would put the region somewhere else the
		// moment a screen carries anything outside ASCII.
		runes := []rune(lines[row-1])
		start := col - 1
		if start >= len(runes) {
			return ""
		}
		end := start + length
		if end > len(runes) {
			end = len(runes)
		}
		// NUL is what a position the host never wrote comes back as, and it
		// is invisible in a result: normalise it the same way the live
		// screen reader does.
		return strings.ReplaceAll(string(runes[start:end]), "\x00", " ")
	}
}

// captureOutputs reads the answer through a region reader. It returns the
// values it found and the labels of any non-optional output it could not.
func captureOutputs(read regionReader, outputs []Output) ([]OutputValue, []string) {
	if len(outputs) == 0 {
		return nil, nil
	}
	values := make([]OutputValue, 0, len(outputs))
	var missing []string
	for _, o := range outputs {
		raw := strings.TrimSpace(read(o.Row, o.Column, o.Length))
		value := raw
		found := raw != ""
		if o.Pattern != "" {
			// Validate() compiled this already; a failure here means the
			// task was stored by something that skipped validation, so treat
			// it as "cannot read" rather than panicking on a run.
			re, err := regexp.Compile(o.Pattern)
			if err != nil {
				found = false
				value = ""
			} else if match := re.FindStringSubmatch(raw); match != nil {
				if len(match) > 1 {
					value = match[1]
				} else {
					value = match[0]
				}
				found = true
			} else {
				found = false
				value = ""
			}
		}
		values = append(values, OutputValue{
			Name:  o.Name,
			Label: o.DisplayLabel(),
			Value: value,
			Found: found,
		})
		if !found && !o.Optional {
			missing = append(missing, o.DisplayLabel())
		}
	}
	return values, missing
}

// PreviewOutputs reports what a task's outputs would read from a screen,
// given the screen as text.
//
// This is what stops the wizard from being a guess. Someone drags a region
// over a value, adds a pattern to strip the currency suffix off it, and the
// preview answers with the string the runner would put on the result card —
// including "could not read this", which is the answer worth knowing before
// the task is in the catalogue rather than after.
func PreviewOutputs(screen string, outputs []Output) ([]OutputValue, []string) {
	return captureOutputs(textRegionReader(screen), outputs)
}
