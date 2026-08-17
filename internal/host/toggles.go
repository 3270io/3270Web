// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"fmt"
	"sort"
	"strings"
)

// The terminal keeps a set of runtime toggles — monocase, cursor blink, the
// crosshair, whether control characters are drawn — that an operator would
// otherwise have to restart the session to change. Set() reads and writes
// them.
//
// Two rules make that safe to expose. First, the names are an allowlist:
// Set() also reaches trace files, printer sessions and the proxy
// configuration, none of which belong on an HTTP endpoint, so only the
// display toggles below can be named at all. Second, every name that goes to
// the terminal is one the terminal itself just reported — the bare Set()
// response is the source of truth for what exists in this build — so a name
// invented by a caller never reaches the control pipe, and the values are
// booleans, which is the whole of what the allowlist accepts. Between the two
// there is nothing left for a caller to inject.

// safeToggles are the display and interaction toggles that may be changed
// over HTTP, with what each one does. Deliberately narrow: everything here
// affects only how this session draws and accepts input, and nothing here
// touches the filesystem, the network, or another process.
var safeToggles = map[string]string{
	"altCursor":      "draw the cursor as an underscore rather than a block",
	"blankFill":      "treat trailing blanks in a field as if they were nulls",
	"crossHair":      "draw a crosshair through the cursor position",
	"cursorBlink":    "blink the cursor",
	"marginedPaste":  "keep pasted text inside the rectangle it started in",
	"monoCase":       "display all letters in upper case",
	"numericLock":    "lock the keyboard on a letter typed into a numeric field",
	"overlayPaste":   "let pasted text overlay protected fields instead of stopping",
	"showTiming":     "report how long the last host interaction took",
	"typeahead":      "accept keystrokes while the keyboard is locked",
	"underscore":     "draw input fields with an underscore",
	"visibleControl": "draw control characters instead of hiding them",
}

// SafeToggleNames returns the names that may be set, in order. Handlers quote
// it back when a caller names something outside the allowlist, so the refusal
// carries the answer rather than only the complaint.
func SafeToggleNames() []string {
	out := make([]string, 0, len(safeToggles))
	for name := range safeToggles {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Toggle is one runtime setting as it currently stands.
type Toggle struct {
	Name        string `json:"name"`
	Value       bool   `json:"value"`
	Description string `json:"description"`
}

// parseSettings splits the bare Set() response into name/value pairs. The
// response is one "name: value" per line; the same shape the Query response
// uses.
func parseSettings(raw string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if name == "" || !found {
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	return out
}

// parseToggleValue reads the terminal's spelling of a boolean. Builds differ
// on whether they answer "true", "set", "on" or "1", so all four are
// accepted, and anything else is reported as unrecognised rather than guessed
// at.
func parseToggleValue(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "set", "on", "1", "yes":
		return true, true
	case "false", "clear", "off", "0", "no":
		return false, true
	}
	return false, false
}

// settingsSnapshotLocked issues the bare Set() and returns what the terminal
// reports. The caller must hold h.mu.
func (h *S3270) settingsSnapshotLocked() (map[string]string, error) {
	data, status, err := h.doCommandLocked("Set()")
	if err != nil {
		return nil, err
	}
	if isS3270Error(status, data) {
		return nil, fmt.Errorf("s3270 Set() error: %s", status)
	}
	return parseSettings(strings.Join(dataLines(data), "\n")), nil
}

// Toggles returns the display toggles this build supports, in name order.
// A toggle in the allowlist that the terminal does not report is left out
// rather than reported as false: an older or newer build not having it is not
// the same as it being off.
func (h *S3270) Toggles() ([]Toggle, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	settings, err := h.settingsSnapshotLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Toggle, 0, len(safeToggles))
	for name, raw := range settings {
		canonical, ok := canonicalToggleName(name)
		if !ok {
			continue
		}
		value, known := parseToggleValue(raw)
		if !known {
			continue
		}
		out = append(out, Toggle{Name: canonical, Value: value, Description: safeToggles[canonical]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// canonicalToggleName matches a name the terminal reported against the
// allowlist, case-insensitively, and returns the allowlist's spelling.
func canonicalToggleName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	for allowed := range safeToggles {
		if strings.EqualFold(allowed, name) {
			return allowed, true
		}
	}
	return "", false
}

// SetToggle turns one allowlisted display toggle on or off and returns its
// value as the terminal reports it afterwards, rather than as the caller
// asked for it — a build that silently declines a change should be visible in
// the answer.
//
// The name is resolved against what the terminal just reported, so the string
// that reaches the control pipe is the terminal's own, never the caller's.
func (h *S3270) SetToggle(name string, value bool) (*Toggle, error) {
	if _, ok := canonicalToggleName(name); !ok {
		return nil, fmt.Errorf("%q is not a settable display toggle", name)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	settings, err := h.settingsSnapshotLocked()
	if err != nil {
		return nil, err
	}
	reported := ""
	for known := range settings {
		if strings.EqualFold(known, name) {
			reported = known
			break
		}
	}
	if reported == "" {
		return nil, fmt.Errorf("this terminal build does not have a %q toggle", name)
	}

	word := "clear"
	if value {
		word = "set"
	}
	cmd := fmt.Sprintf("Toggle(%s,%s)", reported, word)
	data, status, err := h.doCommandLocked(cmd)
	if err != nil {
		return nil, err
	}
	if isS3270Error(status, data) {
		return nil, fmt.Errorf("s3270 %s error: %s", cmd, status)
	}

	after, err := h.settingsSnapshotLocked()
	if err != nil {
		return nil, err
	}
	canonical, _ := canonicalToggleName(reported)
	result := &Toggle{Name: canonical, Value: value, Description: safeToggles[canonical]}
	if got, known := parseToggleValue(after[reported]); known {
		result.Value = got
	}
	return result, nil
}
