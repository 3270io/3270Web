// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"strings"
	"testing"
)

// The path travels double-quoted, so the characters that would otherwise end
// an argument or an action are the quoting layer's problem, not a reason to
// refuse a perfectly ordinary Windows path.
func TestValidScreenTracePathAcceptsOrdinaryPaths(t *testing.T) {
	for _, path := range []string{
		"/var/lib/3270web/screen-traces/abc.txt",
		`C:\Program Files\3270Web\screen-traces\abc.txt`,
		"/tmp/trace (1).txt",
	} {
		if err := validScreenTracePath(path); err != nil {
			t.Errorf("validScreenTracePath(%q) = %v, want nil", path, err)
		}
	}
}

// A line break ends the command on the control pipe before the closing quote
// is ever written, so no amount of escaping makes it safe to pass on.
func TestValidScreenTracePathRefusesLineBreaksAndEmpty(t *testing.T) {
	for _, path := range []string{
		"",
		"   ",
		"/tmp/a.txt\nQuit()",
		"/tmp/a.txt\rQuit()",
	} {
		if err := validScreenTracePath(path); err == nil {
			t.Errorf("validScreenTracePath(%q) = nil, want a refusal", path)
		}
	}
}

// What the terminal is actually sent for a path with a space and a backslash:
// one quoted argument, with the backslash doubled so it is not read as an
// escape.
func TestScreenTracePathIsQuotedForTheTerminal(t *testing.T) {
	got := `"` + escapeForS3270String(`C:\Traces\a b.txt`) + `"`
	want := `"C:\\Traces\\a b.txt"`
	if got != want {
		t.Errorf("quoted path = %s, want %s", got, want)
	}
	if strings.Count(got, `"`) != 2 {
		t.Errorf("quoted path has stray quotes: %s", got)
	}
}
