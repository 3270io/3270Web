// SPDX-License-Identifier: AGPL-3.0-or-later

package host

// Conformance: the shape of the display, and what happens at its edges.

import (
	"strings"
	"testing"
)

// A screen with no fields on it at all. An unformatted screen is what a host
// writes before it has drawn a panel — a login banner, a line-mode
// application, the output of a command that just prints — and the whole field
// model does not apply to it. Deciding it is formatted anyway produces a
// screen of one enormous field, and deciding a formatted one is unformatted
// throws every entry box away.
func TestConformanceRecognisesAnUnformattedScreen(t *testing.T) {
	screen := newScreen(80).at(0, 0).text("PLAIN TEXT ONLY").bytes()

	host := startConformanceHost(t, screen)
	// connectTerminal waits for a field, which this screen has none of.
	term := connectTerminalUnformatted(t, host, "-model", "2")
	if err := term.UpdateScreen(); err != nil {
		t.Fatalf("UpdateScreen: %v", err)
	}
	s := term.GetScreenSnapshot()
	if s.IsFormatted {
		t.Error("a screen with no field attributes on it should decode as unformatted")
	}
	if len(s.Fields) != 0 {
		t.Errorf("an unformatted screen should have no fields, got %d", len(s.Fields))
	}
	if got := screenRow(s, 0); got != "PLAIN TEXT ONLY" {
		t.Errorf("row 0: got %q want %q", got, "PLAIN TEXT ONLY")
	}
}

// connectTerminalUnformatted is connectTerminal for a screen that has no
// fields to wait for.
func connectTerminalUnformatted(t *testing.T, h *conformanceHost, args ...string) *S3270 {
	t.Helper()
	exe := requireTerminal(t)
	term := NewS3270(exe, append(append([]string{}, args...), h.addr())...)
	if err := term.Start(); err != nil {
		skipOrFail(t, "could not start the terminal at %s: %v", exe, err)
	}
	t.Cleanup(func() { _ = term.Stop() })
	return term
}

// Every model's standard size, read off the wire rather than assumed. The
// numbers decide where a recording's coordinates land, so a model whose size
// is wrong here replays a flow into the wrong columns.
func TestConformanceReadsEveryModelSize(t *testing.T) {
	cases := []struct {
		model      string
		rows, cols int
	}{
		{"2", 24, 80},
		{"3", 24, 80},
		{"4", 24, 80},
		{"5", 24, 80},
	}
	for _, c := range cases {
		t.Run("model"+c.model, func(t *testing.T) {
			host := startConformanceHost(t, entryScreen(80))
			term := connectTerminal(t, host, "-model", c.model)
			s := term.GetScreenSnapshot()
			// Every model starts on its *default* size, which is 24x80 for all
			// of them. The larger size a model is named for is its alternate,
			// and an application asks for it explicitly.
			if s.Height != c.rows || s.Width != c.cols {
				t.Errorf("model %s default screen: got %dx%d, want %dx%d",
					c.model, s.Height, s.Width, c.rows, c.cols)
			}
		})
	}
}

// The alternate size, which is the one a model is named for. A host asks for
// it with an Erase/Write Alternate, and from that point the display is the
// larger shape until something switches it back.
func TestConformanceSwitchesToTheAlternateSize(t *testing.T) {
	cases := []struct {
		model      string
		rows, cols int
	}{
		{"2", 24, 80},
		{"3", 32, 80},
		{"4", 43, 80},
		{"5", 27, 132},
	}
	for _, c := range cases {
		t.Run("model"+c.model, func(t *testing.T) {
			screen := newAlternateScreen(c.cols).
				at(0, 0).field(faProtected).text("ALT").
				at(c.rows-1, 0).field(0).cursor().
				bytes()

			host := startConformanceHost(t, screen)
			term := connectTerminal(t, host, "-model", c.model)
			s := term.GetScreenSnapshot()

			if s.Height != c.rows || s.Width != c.cols {
				t.Errorf("model %s alternate screen: got %dx%d, want %dx%d",
					c.model, s.Height, s.Width, c.rows, c.cols)
			}
			if got := screenRow(s, 0); got != " ALT" {
				t.Errorf("model %s: row 0 got %q", c.model, got)
			}
			if rows, cols, ok := s.StatusDimensions(); !ok || rows != c.rows || cols != c.cols {
				t.Errorf("model %s: the status line says %dx%d (ok=%v)", c.model, rows, cols, ok)
			}
		})
	}
}

// A display configured larger than its model. The setting is offered, so the
// screen it produces has to arrive whole — this is the end-to-end form of the
// bug where the reported size was cut back to the model's and the rest of the
// screen was dropped on the floor.
func TestConformanceKeepsAnOversizeDisplayWhole(t *testing.T) {
	const cols, rows = 100, 30
	screen := newAlternateScreen(cols).
		at(0, 0).field(faProtected).text("TOPLEFT").
		at(rows-1, cols-9).field(faProtected).text("BOTRIGHT").
		at(1, 0).field(0).cursor().
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2", "-oversize", "100x30")
	s := term.GetScreenSnapshot()

	if s.Width != cols || s.Height != rows {
		t.Fatalf("an oversize display decoded as %dx%d, want %dx%d", s.Height, s.Width, rows, cols)
	}
	if got := screenRow(s, 0); got != " TOPLEFT" {
		t.Errorf("row 0: got %q", got)
	}
	if got := strings.TrimSpace(screenRow(s, rows-1)); got != "BOTRIGHT" {
		t.Errorf("the last row of an oversize display: got %q want %q", got, "BOTRIGHT")
	}
}

// The characters a code page is chosen for. A screen that loses them is not
// subtly wrong — a UK screen loses its pound signs, a German one its umlauts,
// and no amount of reading further down recovers them.
func TestConformanceDrawsCodePageCharacters(t *testing.T) {
	cases := []struct {
		codePage string
		byteVal  byte
		want     rune
		what     string
	}{
		{"cp285", 0x4A, '$', "the UK code page's currency position"},
		{"cp273", 0x4A, 'Ä', "a German umlaut"},
		{"cp297", 0x4A, '°', "a French degree sign"},
		{"cp037", 0x4A, '¢', "the US cent sign"},
	}
	for _, c := range cases {
		t.Run(c.codePage, func(t *testing.T) {
			screen := newScreen(80).
				at(0, 0).field(faProtected).
				bytesWith(c.byteVal).
				at(1, 0).field(0).cursor().
				bytes()

			host := startConformanceHost(t, screen)
			term := connectTerminal(t, host, "-model", "2", "-charset", c.codePage)
			s := term.GetScreenSnapshot()

			got := s.CharAt(1, 0)
			if got != c.want {
				t.Errorf("%s (%s, byte %#02x): got %q want %q", c.what, c.codePage, c.byteVal, got, c.want)
			}
		})
	}
}

// A first screen with nothing to type into. A report, a broadcast notice, a
// "system unavailable" message: display-only panels are ordinary, and one
// arriving first used to stop the session coming up at all — the connection
// succeeded, the screen arrived, and the operator was told the host could not
// be reached.
func TestConformanceConnectsToADisplayOnlyFirstScreen(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("SYSTEM UNAVAILABLE").
		at(2, 0).field(faProtected).text("PRESS PF3 TO EXIT").
		bytes()

	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")
	s := term.GetScreenSnapshot()

	if got := screenRow(s, 0); got != " SYSTEM UNAVAILABLE" {
		t.Errorf("row 0: got %q", got)
	}
	if !s.IsFormatted {
		t.Error("a screen of protected fields is still a formatted screen")
	}
	for _, f := range s.Fields {
		if !f.IsProtected() {
			t.Errorf("this screen has no unprotected field, but one decoded at (%d,%d)", f.StartX, f.StartY)
		}
	}
}

// The same screen, and the function key that gets out of it. A display-only
// panel is not a dead end: the operator reads it and presses a key, and that
// key has to reach the host.
func TestConformanceSendsAKeyFromADisplayOnlyScreen(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("NOTICE").
		bytes()

	host := startConformanceHost(t, screen, screen)
	term := connectTerminal(t, host, "-model", "2")

	if err := term.SendKey("PF3"); err != nil {
		t.Fatalf("PF3 from a display-only screen: %v", err)
	}
	if rec := host.awaitRecord(1); rec.AID != aidPF3 {
		t.Errorf("the host received AID %#02x, want %#02x", rec.AID, aidPF3)
	}
}
