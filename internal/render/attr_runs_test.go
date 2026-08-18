// SPDX-License-Identifier: AGPL-3.0-or-later

package render

import (
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

// buildScreen assembles a one-row screen holding text, with an optional
// character-attribute overlay, the way a decoded read would leave it.
func buildScreen(text string, attrs []host.CellAttr) *host.Screen {
	runes := []rune(text)
	s := &host.Screen{
		Width:       len(runes) + 1,
		Height:      1,
		IsFormatted: true,
	}
	row := make([]rune, s.Width)
	row[0] = ' '
	copy(row[1:], runes)
	s.Buffer = [][]rune{row}
	if attrs != nil {
		grid := make([]host.CellAttr, s.Width)
		copy(grid[1:], attrs)
		s.CellAttrs = [][]host.CellAttr{grid}
	}
	return s
}

// A field with an SA-coloured run inside it renders as one span per run, so the
// four words the host coloured are the four words that come out coloured —
// rather than the whole field taking one attribute or none of it taking any.
func TestProtectedFieldSplitsAtCharacterAttributes(t *testing.T) {
	s := buildScreen("ABCDEF", []host.CellAttr{
		{}, {},
		{Color: host.AttrColGreen}, {Color: host.AttrColGreen},
		{Color: host.AttrColGreen, Highlight: host.AttrEhBlink},
		{Color: host.AttrColGreen, Highlight: host.AttrEhBlink},
	})
	f := host.NewField(s, host.AttrProtected, 1, 0, 6, 0, host.AttrColDefault, host.AttrEhDefault)
	s.Fields = []*host.Field{f}

	out := NewHtmlRenderer().Render(s, "/submit", "")

	for _, want := range []string{
		"<pre> AB<span",
		`class="color-green"`,
		`>CD</span>`,
		`class="color-green highlight-blink"`,
		`>EF</span>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

// The run text put together again is the field's text, so a screen with no
// character attributes renders exactly as it did before there were any.
func TestProtectedFieldWithoutCharacterAttributesIsOneSpan(t *testing.T) {
	s := buildScreen("HELLO", nil)
	f := host.NewField(s, host.AttrProtected, 1, 0, 5, 0, host.AttrColRed, host.AttrEhDefault)
	s.Fields = []*host.Field{f}

	out := NewHtmlRenderer().Render(s, "/submit", "")
	if strings.Count(out, "<span") != 1 {
		t.Errorf("expected a single span, got:\n%s", out)
	}
	if !strings.Contains(out, `>HELLO</span>`) {
		t.Errorf("expected the whole field in one span, got:\n%s", out)
	}
}

// A background colour is a separate attribute from the foreground, and both
// reach the element together when the host sets both.
func TestBackgroundColourReachesTheSpan(t *testing.T) {
	s := buildScreen("PANEL", nil)
	f := host.NewField(s, host.AttrProtected, 1, 0, 5, 0, host.AttrColWhite, host.AttrEhDefault)
	f.Background = host.AttrColBlue
	s.Fields = []*host.Field{f}

	out := NewHtmlRenderer().Render(s, "/submit", "")
	if !strings.Contains(out, `class="color-white bg-blue"`) {
		t.Errorf("expected foreground and background classes together, got:\n%s", out)
	}
}

// An input box is one element whatever the host did inside it, so it takes the
// attributes in force where it starts.
func TestInputFieldTakesTheAttributesAtItsFirstPosition(t *testing.T) {
	s := buildScreen("ABCDE", []host.CellAttr{
		{Color: host.AttrColYellow}, {Color: host.AttrColYellow},
		{Color: host.AttrColYellow}, {Color: host.AttrColYellow},
		{Color: host.AttrColYellow},
	})
	f := host.NewField(s, 0, 1, 0, 5, 0, host.AttrColDefault, host.AttrEhDefault)
	s.Fields = []*host.Field{f}

	out := NewHtmlRenderer().Render(s, "/submit", "")
	if !strings.Contains(out, "color-input color-yellow") {
		t.Errorf("expected the input to take the character attribute, got:\n%s", out)
	}
}
