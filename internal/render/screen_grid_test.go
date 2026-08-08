package render

import (
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

// newTextScreen builds a formatted screen whose buffer holds the given rows,
// space-padded to width.
func newTextScreen(width, height int, rows []string) *host.Screen {
	s := &host.Screen{Width: width, Height: height, IsFormatted: true}
	s.Buffer = make([][]rune, height)
	for y := 0; y < height; y++ {
		line := ""
		if y < len(rows) {
			line = rows[y]
		}
		row := make([]rune, width)
		for x := 0; x < width; x++ {
			if x < len([]rune(line)) {
				row[x] = []rune(line)[x]
			} else {
				row[x] = ' '
			}
		}
		s.Buffer[y] = row
	}
	return s
}

func TestScreenGridTextIsRectangular(t *testing.T) {
	// Deliberately ragged input: the host left row 1 short and row 2 empty.
	s := newTextScreen(10, 3, []string{"ACCOUNT", "42", ""})
	got := screenGridText(s, 3, 10)

	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for i, line := range lines {
		if len([]rune(line)) != 10 {
			t.Errorf("line %d has width %d, want 10 — block copy column arithmetic depends on a rectangular grid", i, len([]rune(line)))
		}
	}
	if lines[0] != "ACCOUNT   " {
		t.Errorf("line 0 = %q, want %q", lines[0], "ACCOUNT   ")
	}
	if lines[2] != strings.Repeat(" ", 10) {
		t.Errorf("line 2 = %q, want ten spaces", lines[2])
	}
}

// NUL is what an untouched buffer cell holds; rendering it literally would
// put a control character on the clipboard.
func TestScreenGridTextSubstitutesNulWithSpace(t *testing.T) {
	s := &host.Screen{Width: 3, Height: 1, IsFormatted: true}
	s.Buffer = [][]rune{{'A', 0, 'C'}}
	got := screenGridText(s, 1, 3)
	if got != "A C" {
		t.Errorf("screenGridText() = %q, want %q", got, "A C")
	}
}

func TestScreenGridTextHandlesNilScreen(t *testing.T) {
	if got := screenGridText(nil, 24, 80); got != "" {
		t.Errorf("screenGridText(nil) = %q, want empty", got)
	}
}

// The grid rides in an HTML attribute, so its row boundaries must survive as
// character references rather than raw newlines.
func TestRenderEmitsScreenTextAttribute(t *testing.T) {
	s := newTextScreen(5, 2, []string{"HELLO", "WORLD"})
	out := NewHtmlRenderer().Render(s, "/submit", "sess")

	if !strings.Contains(out, `data-screen-text="HELLO&#10;WORLD"`) {
		t.Errorf("rendered form is missing the screen grid attribute.\nGot: %s", out)
	}
}

func TestRenderEscapesScreenTextAttribute(t *testing.T) {
	s := newTextScreen(6, 1, []string{`A<"&>B`})
	out := NewHtmlRenderer().Render(s, "/submit", "sess")

	if !strings.Contains(out, `data-screen-text="A&lt;&#34;&amp;&gt;B"`) {
		t.Errorf("screen grid attribute is not escaped.\nGot: %s", out)
	}
}

// The 3270 numeric attribute was parsed but never reached the browser, so
// numeric-only fields silently accepted letters.
func TestRenderMarksNumericFields(t *testing.T) {
	s := &host.Screen{Width: 20, Height: 1, IsFormatted: true}
	s.Buffer = [][]rune{make([]rune, 20)}
	for i := range s.Buffer[0] {
		s.Buffer[0][i] = ' '
	}
	numeric := host.NewField(s, host.AttrNumeric, 0, 0, 5, 0, host.AttrColDefault, host.AttrEhDefault)
	text := host.NewField(s, 0x00, 7, 0, 12, 0, host.AttrColDefault, host.AttrEhDefault)
	s.Fields = []*host.Field{numeric, text}

	out := NewHtmlRenderer().Render(s, "/submit", "sess")

	if !strings.Contains(out, `name="field_0_0"`) || !strings.Contains(out, `data-numeric="1" inputmode="numeric"`) {
		t.Errorf("numeric field was not marked for client-side enforcement.\nGot: %s", out)
	}
	// The non-numeric field must not pick the numeric attributes up.
	if strings.Count(out, `data-numeric="1"`) != 1 {
		t.Errorf("expected exactly one numeric field, got %d.\nOut: %s", strings.Count(out, `data-numeric="1"`), out)
	}
	if !strings.Contains(out, `inputmode="text"`) {
		t.Errorf("non-numeric field lost its text inputmode.\nGot: %s", out)
	}
}
