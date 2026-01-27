package render

import (
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

func BenchmarkRender(b *testing.B) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
		for j := range screen.Buffer[i] {
			screen.Buffer[i][j] = 'a'
		}
	}

	// Create many fields to stress the renderer
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x += 10 {
			f := host.NewField(screen, host.AttrProtected, x, y, x+8, y, host.AttrColGreen, host.AttrEhDefault)
			screen.Fields = append(screen.Fields, f)
		}
	}

	r := NewHtmlRenderer()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Render(screen, "/submit", "session1")
	}
}

func BenchmarkRenderWithSpecialChars(b *testing.B) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
		for j := range screen.Buffer[i] {
			if j%5 == 0 {
				screen.Buffer[i][j] = '<' // Special char
			} else {
				screen.Buffer[i][j] = 'a'
			}
		}
	}

	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x += 10 {
			f := host.NewField(screen, host.AttrProtected, x, y, x+8, y, host.AttrColGreen, host.AttrEhDefault)
			screen.Fields = append(screen.Fields, f)
		}
	}

	r := NewHtmlRenderer()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Render(screen, "/submit", "session1")
	}
}

func TestRenderCorrectness(t *testing.T) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	// Initialize buffer
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
		for j := range screen.Buffer[i] {
			screen.Buffer[i][j] = ' '
		}
	}

	// Add one input field
	// 11 chars width (10 to 20 inclusive is 11 chars)
	f := host.NewField(screen, 0, 10, 5, 20, 5, host.AttrColDefault, host.AttrEhDefault)
	f.SetValue("Hello")
	screen.Fields = append(screen.Fields, f)

	r := NewHtmlRenderer()
	output := r.Render(screen, "/submit", "test_id")

	expectedSubstrings := []string{
		`<form id="screen-test_id" name="screen-test_id" action="/submit" method="post" class="renderer-form">`,
		`<input type="text" name="field_10_5" class="color-input" value="Hello" maxlength="11" size="11" data-x="10" data-y="5" data-w="11" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" inputmode="text" />`,
		`installKeyHandler('screen-test_id');`,
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Output missing expected substring: %s\nGot:\n%s", expected, output)
		}
	}
}

func TestRenderProtectedFieldClasses(t *testing.T) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
	}

	// Case 1: Just color
	// 0x20 (Protected) | 0x00 (Normal) -> 0x20
	f1 := host.NewField(screen, host.AttrProtected, 0, 0, 5, 0, host.AttrColRed, host.AttrEhDefault)
	f1.SetValue("AAAAA")

	// Case 2: Color + Extended Highlight
	f2 := host.NewField(screen, host.AttrProtected, 10, 0, 15, 0, host.AttrColBlue, host.AttrEhUnderscore)
	f2.SetValue("BBBBB")

	// Case 3: Intensified + Color
	// 0x20 (Protected) | 0x08 (Intensified) -> 0x28
	f3 := host.NewField(screen, host.AttrProtected|0x08, 20, 0, 25, 0, host.AttrColGreen, host.AttrEhDefault)
	f3.SetValue("CCCCC")

	// Case 4: Hidden + Color + Highlight
	// 0x20 (Protected) | 0x0C (Hidden) -> 0x2C
	f4 := host.NewField(screen, host.AttrProtected|0x0C, 30, 0, 35, 0, host.AttrColPink, host.AttrEhBlink)
	f4.SetValue("DDDDD")

	screen.Fields = []*host.Field{f1, f2, f3, f4}

	r := NewHtmlRenderer()
	output := r.Render(screen, "/submit", "")

	expectedSubstrings := []string{
		// Case 1
		`<span class="color-red">AAAAA</span>`,
		// Case 2
		`<span class="color-blue highlight-underscore">BBBBB</span>`,
		// Case 3
		`<span class="color-intensified color-green">CCCCC</span>`,
		// Case 4
		`<span class="color-hidden color-pink highlight-blink">DDDDD</span>`,
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Output missing expected substring: %s", expected)
		}
	}
}
