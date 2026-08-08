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
	// 7 chars width (10 is the attribute cell; input is 11 to 20, minus 3)
	// 11 chars width (input is 10 to 20 inclusive)
	f := host.NewField(screen, 0, 10, 5, 20, 5, host.AttrColDefault, host.AttrEhDefault)
	f.SetValue("Hello")
	screen.Fields = append(screen.Fields, f)

	r := NewHtmlRenderer()
	output := r.Render(screen, "/submit", "test_id")

	expectedSubstrings := []string{
		`<form id="screen-test_id" name="screen-test_id" action="/submit" method="post" class="renderer-form" data-rows="24" data-cols="80" autocomplete="off" data-form-type="other" data-screen-text="`,
		`<input type="text" name="field_10_5" class="color-input" value="Hello" maxlength="11" size="11" style="width: 11ch; max-width: 11ch;" data-x="10" data-y="5" data-w="11" data-fa="0x00" data-display="normal" data-hidden="0" data-protected="0" inputmode="text" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" />`,
		`<div hidden data-initial-focus data-form-name="screen-test_id"`,
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Output missing expected substring: %s\nGot:\n%s", expected, output)
		}
	}
}

// TestAppendFocusCaretMatchesHostCursorColumn guards a caret-restore
// off-by-one: inputStartX used to be focused.StartX+1, treating StartX as
// an attribute-byte position one column before the field's actual first
// character. StartX is already that first character column (see the width
// calculation elsewhere: EndX-StartX+1), so the +1 shifted every restored
// caret one column left of the true host cursor — most visibly on a
// field's last character, where the true caret (width-1) came out as
// width-2.
func TestAppendFocusCaretMatchesHostCursorColumn(t *testing.T) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
		for j := range screen.Buffer[i] {
			screen.Buffer[i][j] = ' '
		}
	}

	// Field spans host columns 10-20 inclusive (width 11): StartX=10 is the
	// first character cell, EndX=20 is the last.
	f := host.NewField(screen, 0, 10, 5, 20, 5, host.AttrColDefault, host.AttrEhDefault)
	f.SetValue("Hello")
	screen.Fields = append(screen.Fields, f)

	cases := []struct {
		name      string
		cursorX   int
		wantCaret string
	}{
		{"cursor at field start", 10, `data-caret="0"`},
		{"cursor mid-field", 13, `data-caret="3"`},
		{"cursor at field's last character", 20, `data-caret="10"`},
	}

	r := NewHtmlRenderer()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			screen.CursorX = tc.cursorX
			screen.CursorY = 5
			output := r.Render(screen, "/submit", "test_id")
			if !strings.Contains(output, tc.wantCaret) {
				t.Errorf("cursorX=%d: output missing %q\nGot:\n%s", tc.cursorX, tc.wantCaret, output)
			}
		})
	}
}

// TestRenderInputFieldGetsAriaLabelFromPrecedingLabel guards against screen
// readers announcing every 3270 input as a bare, context-free "edit text":
// an unprotected field must be given an aria-label derived from the
// protected label text immediately preceding it on the same row.
func TestRenderInputFieldGetsAriaLabelFromPrecedingLabel(t *testing.T) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
		for j := range screen.Buffer[i] {
			screen.Buffer[i][j] = ' '
		}
	}

	label := host.NewField(screen, host.AttrProtected, 0, 3, 10, 3, host.AttrColDefault, host.AttrEhDefault)
	label.SetValue("First Name:")
	screen.Fields = append(screen.Fields, label)

	input := host.NewField(screen, 0, 12, 3, 30, 3, host.AttrColDefault, host.AttrEhDefault)
	input.SetValue("")
	screen.Fields = append(screen.Fields, input)

	r := NewHtmlRenderer()
	output := r.Render(screen, "/submit", "test_id")

	if !strings.Contains(output, `aria-label="First Name:"`) {
		t.Errorf("expected input to be labeled from preceding protected field, got:\n%s", output)
	}
}

// TestRenderInputFieldWithoutPrecedingLabelHasNoAriaLabel confirms the
// renderer doesn't invent a label when there's nothing sensible to derive
// it from — an input with no preceding protected text on its row should
// not get an aria-label attribute at all.
func TestRenderInputFieldWithoutPrecedingLabelHasNoAriaLabel(t *testing.T) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
		for j := range screen.Buffer[i] {
			screen.Buffer[i][j] = ' '
		}
	}

	input := host.NewField(screen, 0, 10, 5, 20, 5, host.AttrColDefault, host.AttrEhDefault)
	input.SetValue("Hello")
	screen.Fields = append(screen.Fields, input)

	r := NewHtmlRenderer()
	output := r.Render(screen, "/submit", "test_id")

	if strings.Contains(output, "aria-label") {
		t.Errorf("expected no aria-label without a preceding protected label, got:\n%s", output)
	}
}

func TestRenderHiddenInputDoesNotUsePasswordType(t *testing.T) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
	}

	f := host.NewField(screen, 0x0C, 10, 5, 20, 5, host.AttrColDefault, host.AttrEhDefault)
	f.SetValue("secret")
	screen.Fields = append(screen.Fields, f)

	r := NewHtmlRenderer()
	output := r.Render(screen, "/submit", "test_hidden")

	if strings.Contains(output, `type="password"`) {
		t.Fatalf("hidden field should not render as password input: %s", output)
	}
	if !strings.Contains(output, `class="color-input-hidden"`) {
		t.Fatalf("hidden field should use hidden input class: %s", output)
	}
}

func TestRenderMultilineInputWidths(t *testing.T) {
	screen := &host.Screen{
		Width:       10,
		Height:      3,
		IsFormatted: true,
		Buffer:      make([][]rune, 3),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 10)
		for j := range screen.Buffer[i] {
			screen.Buffer[i][j] = ' '
		}
	}

	f := host.NewField(screen, 0, 3, 0, 4, 2, host.AttrColDefault, host.AttrEhDefault)
	f.SetValue("ABC\nDEF\nG")
	screen.Fields = append(screen.Fields, f)

	r := NewHtmlRenderer()
	output := r.Render(screen, "/submit", "")

	expectedSubstrings := []string{
		`<input type="text" name="field_3_0_0" class="color-input" value="ABC" maxlength="7" size="7" style="width: 7ch; max-width: 7ch;" data-x="3" data-y="0" data-w="7" data-fa="0x00" data-display="normal" data-hidden="0" data-protected="0" inputmode="text" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" />`,
		`<input type="text" name="field_3_0_1" class="color-input" value="DEF" maxlength="10" size="10" style="width: 10ch; max-width: 10ch;" data-x="3" data-y="1" data-w="10" data-fa="0x00" data-display="normal" data-hidden="0" data-protected="0" inputmode="text" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" />`,
		`<input type="text" name="field_3_0_2" class="color-input" value="G" maxlength="5" size="5" style="width: 5ch; max-width: 5ch;" data-x="3" data-y="2" data-w="5" data-fa="0x00" data-display="normal" data-hidden="0" data-protected="0" inputmode="text" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" />`,
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
		`<span class="color-red" data-fa="0x20" data-display="normal" data-hidden="0" data-protected="1">AAAAA</span>`,
		// Case 2
		`<span class="color-blue highlight-underscore" data-fa="0x20" data-display="normal" data-hidden="0" data-protected="1">BBBBB</span>`,
		// Case 3
		`<span class="color-intensified color-green" data-fa="0x28" data-display="intensified" data-hidden="0" data-protected="1">CCCCC</span>`,
		// Case 4
		`<span class="color-pink highlight-blink" data-fa="0x2c" data-display="hidden" data-hidden="1" data-protected="1">DDDDD</span>`,
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Output missing expected substring: %s", expected)
		}
	}
}

func TestWriteEscaped(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Empty", "", ""},
		{"Normal", "abc123", "abc123"},
		{"Space", "   ", "   "},
		{"Null", "\x00", " "},
		{"Nulls", "\x00\x00\x00", "   "},
		{"NullMixed", "a\x00b", "a b"},
		{"Quote", "\"", "&#34;"},
		{"Ampersand", "&", "&amp;"},
		{"Apostrophe", "'", "&#39;"},
		{"LessThan", "<", "&lt;"},
		{"GreaterThan", ">", "&gt;"},
		{"MixedHTML", `<script>alert("xss")</script>`, "&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt;"},
		{"MixedNullHTML", "<\x00>", "&lt; &gt;"},
		{"ControlChars", "\n\t\r", "\n\t\r"},
		{"Unicode", "你好", "你好"},
		{"Emoji", "👍", "👍"},
		{"OptimizationPath", "no special chars", "no special chars"},
		{"OptimizationPathFail", "special < char", "special &lt; char"},
	}

	r := NewHtmlRenderer()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sb strings.Builder
			r.writeEscaped(&sb, tt.input)
			got := sb.String()
			if got != tt.expected {
				t.Errorf("writeEscaped(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRenderInputFieldAddsColorAndHighlightClasses(t *testing.T) {
	screen := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range screen.Buffer {
		screen.Buffer[i] = make([]rune, 80)
	}

	f := host.NewField(screen, 0x00, 10, 5, 20, 5, host.AttrColBlue, host.AttrEhUnderscore)
	f.SetValue("Hello")
	screen.Fields = []*host.Field{f}

	r := NewHtmlRenderer()
	out := r.Render(screen, "/submit", "")

	if !strings.Contains(out, `class="color-input color-blue highlight-underscore"`) {
		t.Fatalf("expected input field classes with color/highlight, got output: %s", out)
	}
}
