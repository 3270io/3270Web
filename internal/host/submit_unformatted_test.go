// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"strings"
	"testing"
)

func blankScreen(rows, cols int) *Screen {
	s := &Screen{Width: cols, Height: rows}
	s.Buffer = make([][]rune, rows)
	for y := range s.Buffer {
		s.Buffer[y] = make([]rune, cols)
		for x := range s.Buffer[y] {
			s.Buffer[y][x] = ' '
		}
	}
	return s
}

// Every action here is a round trip down the control pipe and a wait on a
// subprocess. Writing an unformatted screen a character at a time made a line
// of text cost two commands per character; the same line written as one run
// costs two in total.
func TestSubmitUnformattedWritesRunsRatherThanCharacters(t *testing.T) {
	h, logPath := startFakeS3270(t, "polite")
	defer h.Stop()
	h.screen = blankScreen(2, 10)

	if err := h.SubmitUnformatted("HELLO"); err != nil {
		t.Fatalf("SubmitUnformatted: %v", err)
	}

	cmds := recordedCommands(t, logPath)
	var writes []string
	for _, cmd := range cmds {
		if strings.HasPrefix(strings.ToLower(cmd), "movecursor") || strings.HasPrefix(cmd, "String(") {
			writes = append(writes, cmd)
		}
	}
	want := []string{"movecursor(0, 0)", `String("HELLO")`}
	if len(writes) != len(want) {
		t.Fatalf("wrote %d commands %v, want %d %v", len(writes), writes, len(want), want)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Errorf("command %d is %q, want %q", i, writes[i], want[i])
		}
	}
}

// A row separator ends the row wherever it falls. Treating it as a character
// on any line shorter than the screen is wide types a newline into the display
// and pushes the rest of the text one cell along — and then every row after it
// is off by one more.
func TestSubmitUnformattedEndsARowAtItsSeparator(t *testing.T) {
	h, logPath := startFakeS3270(t, "polite")
	defer h.Stop()
	h.screen = blankScreen(3, 10)

	if err := h.SubmitUnformatted("AB\nCD\n"); err != nil {
		t.Fatalf("SubmitUnformatted: %v", err)
	}

	cmds := recordedCommands(t, logPath)
	var writes []string
	for _, cmd := range cmds {
		if strings.HasPrefix(strings.ToLower(cmd), "movecursor") || strings.HasPrefix(cmd, "String(") {
			writes = append(writes, cmd)
		}
	}
	want := []string{"movecursor(0, 0)", `String("AB")`, "movecursor(1, 0)", `String("CD")`}
	if strings.Join(writes, " ") != strings.Join(want, " ") {
		t.Errorf("wrote %v, want %v", writes, want)
	}
}

// Only what changed is written. A run breaks where the screen already agrees
// with the text, so the cursor lands where the next difference starts.
func TestSubmitUnformattedSkipsWhatAlreadyMatches(t *testing.T) {
	h, logPath := startFakeS3270(t, "polite")
	defer h.Stop()
	h.screen = blankScreen(1, 10)
	copy(h.screen.Buffer[0], []rune("AB    GH  "))

	if err := h.SubmitUnformatted("ABCD  GHIJ"); err != nil {
		t.Fatalf("SubmitUnformatted: %v", err)
	}

	cmds := recordedCommands(t, logPath)
	var writes []string
	for _, cmd := range cmds {
		if strings.HasPrefix(strings.ToLower(cmd), "movecursor") || strings.HasPrefix(cmd, "String(") {
			writes = append(writes, cmd)
		}
	}
	want := []string{"movecursor(0, 2)", `String("CD")`, "movecursor(0, 8)", `String("IJ")`}
	if strings.Join(writes, " ") != strings.Join(want, " ") {
		t.Errorf("wrote %v, want %v", writes, want)
	}
}
