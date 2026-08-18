// SPDX-License-Identifier: AGPL-3.0-or-later

package render

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

// The renderer names a style and the stylesheet implements it, and nothing in
// between checks that the two agree. A class the renderer emits and the
// stylesheet has never heard of is not an error anywhere: the screen renders,
// the element is there, and the attribute the host sent simply does not show —
// which is the one kind of terminal bug an operator cannot report usefully,
// because there is nothing on the screen to point at.
//
// So the classes are collected from the renderer's own output over every
// attribute value the decoder can produce, and each one is looked for in the
// stylesheet.
func TestEveryAttributeClassTheRendererEmitsIsStyled(t *testing.T) {
	css := readStylesheet(t)

	colors := []int{
		host.AttrColDefault, host.AttrColNeutral, host.AttrColBlue, host.AttrColRed,
		host.AttrColPink, host.AttrColGreen, host.AttrColTurquoise, host.AttrColYellow,
		host.AttrColWhite,
	}
	highlights := []int{
		host.AttrEhDefault, host.AttrEhBlink, host.AttrEhRevVideo, host.AttrEhUnderscore,
	}

	classPattern := regexp.MustCompile(`class="([^"]*)"`)
	seen := map[string]bool{}

	for _, color := range colors {
		for _, background := range colors {
			for _, highlight := range highlights {
				for _, code := range []byte{host.AttrProtected, 0} {
					s := &host.Screen{Width: 7, Height: 1, IsFormatted: true}
					s.Buffer = [][]rune{[]rune(" ABCDEF")}
					f := host.NewField(s, code, 1, 0, 6, 0, color, highlight)
					f.Background = background
					s.Fields = []*host.Field{f}

					out := NewHtmlRenderer().Render(s, "/submit", "")
					for _, match := range classPattern.FindAllStringSubmatch(out, -1) {
						for _, class := range strings.Fields(match[1]) {
							seen[class] = true
						}
					}
				}
			}
		}
	}

	if len(seen) == 0 {
		t.Fatal("collected no classes from the renderer")
	}
	for class := range seen {
		if !strings.Contains(class, "color-") && !strings.Contains(class, "highlight-") && !strings.HasPrefix(class, "bg-") {
			continue
		}
		if !strings.Contains(css, "."+class) {
			t.Errorf("the renderer emits %q and the stylesheet does not define it", class)
		}
	}
}

// Reverse video swaps the ink and the ground. Writing that as
// "background-color: currentColor" alongside a colour on the same rule makes
// the two cancel: currentColor is the element's own colour, which that rule
// has just set to the background, so the text ends up painted in the colour it
// is sitting on and the field renders as a blank hole. It looks right in the
// stylesheet and is invisible on the screen, which is why it is worth a test
// rather than a comment.
func TestReverseVideoDoesNotPaintTextOnItsOwnColour(t *testing.T) {
	css := readStylesheet(t)

	rule := ruleBody(css, ".highlight-rev-video")
	if rule == "" {
		t.Fatal("no .highlight-rev-video rule in the stylesheet")
	}
	if strings.Contains(rule, "currentColor") {
		t.Errorf("reverse video resolves its background from the colour it also sets:\n%s", rule)
	}
	if !strings.Contains(rule, "background-color") || !strings.Contains(rule, "color:") {
		t.Errorf("reverse video should set both a background and a foreground:\n%s", rule)
	}
}

// A blinking field is the host asking for attention, and an operator who has
// asked the system for less motion still needs to be told. The attribute keeps
// a still form rather than being switched off.
func TestBlinkKeepsAStillFormUnderReducedMotion(t *testing.T) {
	css := readStylesheet(t)
	idx := strings.Index(css, "prefers-reduced-motion")
	for idx != -1 {
		block := css[idx:]
		if end := strings.Index(block, "\n}\n"); end != -1 {
			block = block[:end]
		}
		if strings.Contains(block, "highlight-blink") {
			replaced := false
			for _, marker := range []string{"outline", "text-decoration", "background", "box-shadow", "border"} {
				if strings.Contains(block, marker) {
					replaced = true
					break
				}
			}
			if !replaced {
				t.Errorf("reduced motion drops the blink without replacing it:\n%s", block)
			}
			return
		}
		next := strings.Index(css[idx+1:], "prefers-reduced-motion")
		if next == -1 {
			break
		}
		idx += 1 + next
	}
	t.Error("no reduced-motion handling for the blink attribute")
}

func readStylesheet(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../web/static/style.css")
	if err != nil {
		t.Fatalf("reading the stylesheet: %v", err)
	}
	return string(raw)
}

// ruleBody returns the declarations of the first rule whose selector list ends
// with selector, or "" when there is none.
func ruleBody(css, selector string) string {
	for i := 0; i < len(css); {
		idx := strings.Index(css[i:], selector+" {")
		if idx == -1 {
			return ""
		}
		start := i + idx
		// Guard against matching a longer class name that ends the same way.
		if start > 0 {
			prev := css[start-1]
			if prev != '\n' && prev != ' ' && prev != ',' {
				i = start + len(selector)
				continue
			}
		}
		open := strings.Index(css[start:], "{")
		close := strings.Index(css[start:], "}")
		if open == -1 || close == -1 {
			return ""
		}
		return css[start+open+1 : start+close]
	}
	return ""
}
