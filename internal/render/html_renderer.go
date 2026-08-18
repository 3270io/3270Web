// SPDX-License-Identifier: AGPL-3.0-or-later

package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
)

type HtmlRenderer struct{}

func NewHtmlRenderer() *HtmlRenderer {
	return &HtmlRenderer{}
}

func (r *HtmlRenderer) Render(s *host.Screen, actionURL, id string) string {
	var sb strings.Builder
	formName := r.getFormName(id)
	rows, cols := r.screenDimensions(s)

	sb.WriteString(`<form id="`)
	sb.WriteString(formName)
	sb.WriteString(`" name="`)
	sb.WriteString(formName)
	sb.WriteString(`" action="`)
	sb.WriteString(actionURL)
	sb.WriteString(`" method="post" class="renderer-form" data-rows="`)
	r.writeInt(&sb, rows)
	sb.WriteString(`" data-cols="`)
	r.writeInt(&sb, cols)
	sb.WriteString(`" autocomplete="off" data-form-type="other" data-screen-text="`)
	// The plain character grid backing this screen. Browser selection across
	// the rendered form is unusable for copying — the screen is a <pre> with
	// <input> elements spliced into it, so a selection silently drops every
	// input's value. Carrying the grid lets the client reconstruct exactly
	// what is on screen (overlaying live, not-yet-submitted input values on
	// top) for whole-screen and rectangular block copy.
	r.writeEscapedAttrLines(&sb, screenGridText(s, rows, cols))
	sb.WriteString(`">`)
	sb.WriteString("\n")

	if s.IsFormatted {
		r.renderFormatted(s, id, &sb)
	} else {
		r.renderUnformatted(s, &sb)
	}

	sb.WriteString(`<div><input type="hidden" name="key" /></div>`)
	sb.WriteString(`<div><input type="hidden" name="cursor_row" /><input type="hidden" name="cursor_col" /></div>`)
	sb.WriteString("\n")
	if id != "" {
		sb.WriteString(`<div><input type="hidden" name="TERMINAL" value="`)
		sb.WriteString(id)
		sb.WriteString(`"></div>`)
		sb.WriteString("\n")
	}
	sb.WriteString("</form>\n")

	r.appendFocus(s, id, &sb)

	return sb.String()
}

// screenGridText renders the screen buffer as exactly rows lines of cols
// characters, padding short rows and substituting spaces for NULs. Unlike
// host.Screen.Text() the result is guaranteed rectangular, which is what a
// client doing rectangular block copy needs — a ragged grid would make
// column arithmetic wrong on any row the host left short.
func screenGridText(s *host.Screen, rows, cols int) string {
	if s == nil || rows <= 0 || cols <= 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow(rows * (cols + 1))
	for y := 0; y < rows; y++ {
		if y > 0 {
			sb.WriteByte('\n')
		}
		for x := 0; x < cols; x++ {
			ch := s.CharAt(x, y)
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

func (r *HtmlRenderer) renderFormatted(s *host.Screen, id string, sb *strings.Builder) {
	sb.WriteString("<pre>")

	// rowLabels tracks the most recently rendered protected field's text per
	// row (keyed by StartY), so an unprotected field can be given an
	// aria-label derived from the label immediately to its left — without
	// this, screen readers announce every field as a bare "edit text" with
	// no indication of what it's for (fields are anonymous <input>s with no
	// association to the surrounding protected label text).
	rowLabels := make(map[int]string)

	for i, f := range s.Fields {
		// Append attribute spacer
		if f.StartX == 0 {
			if f.StartY > 0 {
				sb.WriteString(" \n")
			}
		} else {
			sb.WriteString(" ")
		}

		if f.IsZeroLength() {
			// The attribute byte written above is the whole of this field.
			// An unprotected one still gets no <input>: a box no character
			// wide is a tab stop the operator cannot type into and cannot
			// see, and the host that wrote the field meant it as a stop.
		} else if !f.IsProtected() {
			r.renderInputField(sb, f, id, rowLabels[f.StartY], nextIsAutoSkip(s, i))
		} else {
			// A protected field is written as one span per stretch of
			// like-attributed text rather than one span for the field. Where
			// the host set no character attributes — which is most fields —
			// that is a single run and the output is what it always was; where
			// it did, the run it coloured is the run that gets the colour,
			// instead of the whole field taking the attribute of its first
			// character or none of it taking any.
			for _, run := range f.AttrRuns() {
				needSpan := r.needSpanRun(f, run)
				if needSpan {
					sb.WriteString(`<span class="`)
					r.writeProtectedFieldClass(sb, f, run)
					sb.WriteString(`"`)
					r.writeFieldDebugDataAttrs(sb, f)
					sb.WriteString(`>`)
				}

				r.writeEscaped(sb, run.Text)

				if needSpan {
					sb.WriteString("</span>")
				}
			}

			if label := strings.TrimSpace(f.GetValue()); label != "" {
				rowLabels[f.StartY] = label
			}
		}

		if f.EndX == s.Width-1 && f.EndY >= f.StartY {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("</pre>")
}

func (r *HtmlRenderer) renderUnformatted(s *host.Screen, sb *strings.Builder) {
	rows, cols := r.screenDimensions(s)

	text := s.Text()
	sb.WriteString(`<textarea name="field" class="unformatted" rows="`)
	r.writeInt(sb, rows)
	sb.WriteString(`" cols="`)
	r.writeInt(sb, cols)
	sb.WriteString(`">`)
	r.writeEscaped(sb, text)
	sb.WriteString("</textarea>")
}

func (r *HtmlRenderer) screenDimensions(s *host.Screen) (int, int) {
	rows := 24
	cols := 80
	if s == nil {
		return rows, cols
	}
	if s.Height > 0 {
		rows = s.Height
	}
	if s.Width > 0 {
		cols = s.Width
	}
	return rows, cols
}

// nextIsAutoSkip reports whether the field following index i is an auto-skip
// field.
//
// On a 3270 "auto-skip" is not a bit of its own: it is the protected+numeric
// combination. Its effect is on cursor advance — filling the last position of
// an input field moves the cursor on only when the field that follows is
// auto-skip. That is the attribute-driven rule, and it is what lets an
// application say "this field runs straight into the next one" (a date split
// across three boxes) versus "stop here" (a password the operator may want to
// correct).
//
// The browser cannot work this out for itself: protected fields render as text
// with no attributes attached, so the answer has to be computed here, where
// the field list and its attribute bytes are.
func nextIsAutoSkip(s *host.Screen, i int) bool {
	if s == nil || i+1 >= len(s.Fields) {
		return false
	}
	next := s.Fields[i+1]
	if next == nil {
		return false
	}
	return next.IsProtected() && next.IsNumeric()
}

func (r *HtmlRenderer) renderInputField(sb *strings.Builder, f *host.Field, id, ariaLabel string, autoSkipNext bool) {
	if !f.IsMultiline() {
		// Optimization: Avoid GetValueLines() allocation for single line fields
		val, _, _ := strings.Cut(f.GetValue(), "\n")
		width := f.EndX - f.StartX + 1
		r.createHtmlInput(sb, f, id, val, -1, width, ariaLabel, autoSkipNext)
	} else {
		lines := f.GetValueLines()
		for i := 0; i < f.Height(); i++ {
			val := ""
			if i < len(lines) {
				val = lines[i]
			}

			w := 0
			if i < f.Height()-1 {
				if i == 0 {
					w = f.Screen.Width - f.StartX
				} else {
					w = f.Screen.Width
				}
			} else {
				w = f.EndX + 1
			}
			lineLabel := ariaLabel
			if lineLabel != "" && i > 0 {
				lineLabel = fmt.Sprintf("%s (line %d)", lineLabel, i+1)
			}
			r.createHtmlInput(sb, f, id, val, i, w, lineLabel, autoSkipNext)
			if i < f.Height()-1 {
				sb.WriteString("\n")
			}
		}
	}
}

func (r *HtmlRenderer) createHtmlInput(sb *strings.Builder, f *host.Field, id, val string, lineNum, width int, ariaLabel string, autoSkipNext bool) {
	inputType := "text"

	class := "color-input"
	if f.IsIntensified() {
		class = "color-input-intensified"
	} else if f.IsHidden() {
		class = "color-input-hidden"
	}

	val = r.trimFieldVal(val)

	dataX := f.StartX
	dataY := f.StartY
	if lineNum > 0 {
		dataY += lineNum
	}

	sb.WriteString(`<input type="`)
	sb.WriteString(inputType)
	sb.WriteString(`" name="field_`)
	r.writeInt(sb, f.StartX)
	sb.WriteString("_")
	r.writeInt(sb, f.StartY)
	if lineNum != -1 {
		sb.WriteString("_")
		r.writeInt(sb, lineNum)
	}
	sb.WriteString(`" class="`)
	sb.WriteString(class)
	r.writeFieldColorAndHighlightClasses(sb, f)
	sb.WriteString(`" value="`)
	r.writeEscaped(sb, val)
	sb.WriteString(`" maxlength="`)
	r.writeInt(sb, width)
	sb.WriteString(`" size="`)
	r.writeInt(sb, width)
	sb.WriteString(`" style="width: `)
	r.writeInt(sb, width)
	sb.WriteString(`ch; max-width: `)
	r.writeInt(sb, width)
	sb.WriteString(`ch;" data-x="`)
	r.writeInt(sb, dataX)
	sb.WriteString(`" data-y="`)
	r.writeInt(sb, dataY)
	sb.WriteString(`" data-w="`)
	r.writeInt(sb, width)
	sb.WriteString(`"`)
	if ariaLabel != "" {
		sb.WriteString(` aria-label="`)
		r.writeEscaped(sb, ariaLabel)
		sb.WriteString(`"`)
	}
	r.writeFieldDebugDataAttrs(sb, f)
	// The 3270 numeric attribute was parsed but never reached the browser, so
	// numeric-only fields accepted letters and the host had to reject them a
	// round-trip later. data-numeric lets the client enforce it the way a real
	// emulator does (inhibit input, raise an operator error); inputmode gets
	// touch keyboards to open on digits.
	if f.IsNumeric() {
		sb.WriteString(` data-numeric="1" inputmode="numeric"`)
	} else {
		sb.WriteString(` inputmode="text"`)
	}
	// data-autoskip says the field after this one is auto-skip, which is what
	// decides whether filling this field's last position advances the cursor.
	// The client cannot derive it: protected fields render as plain text with
	// no attributes to inspect.
	if autoSkipNext {
		sb.WriteString(` data-autoskip="1"`)
	}
	sb.WriteString(` autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" />`)
}

func (r *HtmlRenderer) writeEscaped(sb *strings.Builder, s string) {
	if strings.IndexAny(s, "\x00\"&'<>") == -1 {
		sb.WriteString(s)
		return
	}

	start := 0
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b == 0 || b == '"' || b == '&' || b == '\'' || b == '<' || b == '>' {
			if i > start {
				sb.WriteString(s[start:i])
			}
			switch b {
			case 0:
				sb.WriteByte(' ')
			case '"':
				sb.WriteString("&#34;")
			case '&':
				sb.WriteString("&amp;")
			case '\'':
				sb.WriteString("&#39;")
			case '<':
				sb.WriteString("&lt;")
			case '>':
				sb.WriteString("&gt;")
			}
			start = i + 1
		}
	}
	if start < len(s) {
		sb.WriteString(s[start:])
	}
}

// writeEscapedAttrLines escapes s for an attribute value, additionally
// encoding newlines as &#10;. A raw newline inside an attribute survives
// most parsers, but the HTML spec permits normalizing whitespace there, and
// a grid whose row boundaries can silently vanish is not worth the risk.
func (r *HtmlRenderer) writeEscapedAttrLines(sb *strings.Builder, s string) {
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			sb.WriteString("&#10;")
		}
		r.writeEscaped(sb, line)
	}
}

// needSpanRun reports whether a run of protected text carries anything worth
// wrapping in an element. Hidden styling is only applied to input fields;
// protected text remains visible even if a host screen advertises a hidden FA,
// which avoids hiding labels when FA decoding is off.
func (r *HtmlRenderer) needSpanRun(f *host.Field, run host.AttrRun) bool {
	return f.IsIntensified() ||
		foregroundClass(run.Color) != "" ||
		backgroundClass(run.Background) != "" ||
		highlightClass(run.Highlight) != ""
}

// foregroundClass names the style for a colour attribute, or "" for one that
// asks for nothing. AttrColNeutral is deliberately absent: as a foreground it
// is the screen's own ground colour, and painting text in it would erase it.
func foregroundClass(color int) string {
	switch color {
	case host.AttrColBlue:
		return "color-blue"
	case host.AttrColRed:
		return "color-red"
	case host.AttrColPink:
		return "color-pink"
	case host.AttrColGreen:
		return "color-green"
	case host.AttrColTurquoise:
		return "color-turquoise"
	case host.AttrColYellow:
		return "color-yellow"
	case host.AttrColWhite:
		return "color-white"
	}
	return ""
}

// backgroundClass names the style for a background colour attribute. Unlike the
// foreground, the neutral value is meaningful here — it is how a host paints a
// panel back to the screen's own ground behind text that is otherwise coloured.
func backgroundClass(color int) string {
	switch color {
	case host.AttrColNeutral:
		return "bg-neutral"
	case host.AttrColBlue:
		return "bg-blue"
	case host.AttrColRed:
		return "bg-red"
	case host.AttrColPink:
		return "bg-pink"
	case host.AttrColGreen:
		return "bg-green"
	case host.AttrColTurquoise:
		return "bg-turquoise"
	case host.AttrColYellow:
		return "bg-yellow"
	case host.AttrColWhite:
		return "bg-white"
	}
	return ""
}

// highlightClass names the style for an extended highlighting attribute.
func highlightClass(highlight int) string {
	switch highlight {
	case host.AttrEhBlink:
		return "highlight-blink"
	case host.AttrEhRevVideo:
		return "highlight-rev-video"
	case host.AttrEhUnderscore:
		return "highlight-underscore"
	}
	return ""
}

func (r *HtmlRenderer) writeProtectedFieldClass(sb *strings.Builder, f *host.Field, run host.AttrRun) {
	first := true
	write := func(class string) {
		if class == "" {
			return
		}
		if !first {
			sb.WriteString(" ")
		}
		sb.WriteString(class)
		first = false
	}

	if f.IsIntensified() {
		write("color-intensified")
	}
	write(foregroundClass(run.Color))
	write(backgroundClass(run.Background))
	write(highlightClass(run.Highlight))
}

func (r *HtmlRenderer) writeFieldDebugDataAttrs(sb *strings.Builder, f *host.Field) {
	if f == nil {
		return
	}
	sb.WriteString(` data-fa="0x`)
	r.writeHexByte(sb, f.FieldCode)
	sb.WriteString(`" data-display="`)
	sb.WriteString(r.displayModeName(f))
	sb.WriteString(`" data-hidden="`)
	if f.IsHidden() {
		sb.WriteString("1")
	} else {
		sb.WriteString("0")
	}
	sb.WriteString(`" data-protected="`)
	if f.IsProtected() {
		sb.WriteString("1")
	} else {
		sb.WriteString("0")
	}
	sb.WriteString(`"`)
}

func (r *HtmlRenderer) writeHexByte(sb *strings.Builder, b byte) {
	const hex = "0123456789abcdef"
	sb.WriteByte(hex[(b>>4)&0x0f])
	sb.WriteByte(hex[b&0x0f])
}

func (r *HtmlRenderer) displayModeName(f *host.Field) string {
	if f == nil {
		return "unknown"
	}
	switch f.DisplayMode() {
	case host.DisplayNormal:
		return "normal"
	case host.DisplayIntensified:
		return "intensified"
	case host.DisplayHidden:
		return "hidden"
	default:
		return "unknown"
	}
}

// writeFieldColorAndHighlightClasses styles an input box. An input is one
// element whatever the host did inside it, so it takes the attributes in force
// at its first position — the field's own, unless a character attribute
// overrides them there.
func (r *HtmlRenderer) writeFieldColorAndHighlightClasses(sb *strings.Builder, f *host.Field) {
	if f == nil {
		return
	}
	runs := f.AttrRuns()
	run := host.AttrRun{Color: f.Color, Highlight: f.ExtendedHighlight, Background: f.Background}
	if len(runs) > 0 {
		run = runs[0]
	}
	for _, class := range []string{
		foregroundClass(run.Color),
		backgroundClass(run.Background),
		highlightClass(run.Highlight),
	} {
		if class != "" {
			sb.WriteString(" ")
			sb.WriteString(class)
		}
	}
}

func (r *HtmlRenderer) getFormName(id string) string {
	if id == "" {
		return "screen"
	}
	return "screen-" + id
}

// appendFocus emits a hidden marker element carrying the data the browser
// needs to install the key handler and restore focus/caret on load, instead
// of an inline <script> — this output lands verbatim inside a page the
// browser parses normally, so an inline script here would need CSP's
// script-src to allow 'unsafe-inline'. web/static/initial-focus.js reads
// this marker on DOMContentLoaded.
func (r *HtmlRenderer) appendFocus(s *host.Screen, id string, sb *strings.Builder) {
	fn := r.getFormName(id)
	sb.WriteString(`<div hidden data-initial-focus data-form-name="`)
	sb.WriteString(fn)
	sb.WriteString(`"`)
	if !s.IsFormatted {
		sb.WriteString(` data-unformatted="1"`)
	} else {
		focused := s.GetInputFieldAt(s.CursorX, s.CursorY)
		if focused != nil {
			lineOffset := 0
			if focused.IsMultiline() {
				lineOffset = s.CursorY - focused.StartY
				if lineOffset < 0 {
					lineOffset = 0
				} else if lineOffset >= focused.Height() {
					lineOffset = focused.Height() - 1
				}
			}
			// focused.StartX is already the field's first character column,
			// 0-based and directly comparable to s.CursorX (also 0-based) —
			// not an attribute-byte position needing a +1 to reach the first
			// character cell. The +1 this used to have shifted every restored
			// caret one column left of the true host cursor (see the matching
			// fix in setCursorFromTarget, web/static/keyboard.js).
			inputStartX := focused.StartX
			if focused.IsMultiline() && lineOffset > 0 {
				inputStartX = 0
			}
			caret := s.CursorX - inputStartX
			if caret < 0 {
				caret = 0
			}
			sb.WriteString(` data-field-name="field_`)
			r.writeInt(sb, focused.StartX)
			sb.WriteString("_")
			r.writeInt(sb, focused.StartY)
			if focused.IsMultiline() {
				sb.WriteString("_")
				r.writeInt(sb, lineOffset)
			}
			sb.WriteString(`" data-caret="`)
			r.writeInt(sb, caret)
			sb.WriteString(`"`)
		}
	}
	sb.WriteString("></div>\n")
}

func (r *HtmlRenderer) writeInt(sb *strings.Builder, n int) {
	if n >= 0 && n < 1000 {
		if n < 10 {
			sb.WriteByte(byte(n) + '0')
			return
		}
		if n < 100 {
			sb.WriteByte(byte(n/10) + '0')
			sb.WriteByte(byte(n%10) + '0')
			return
		}
		// n < 1000
		sb.WriteByte(byte(n/100) + '0')
		sb.WriteByte(byte((n/10)%10) + '0')
		sb.WriteByte(byte(n%10) + '0')
		return
	}
	var buf [20]byte
	sb.Write(strconv.AppendInt(buf[:0], int64(n), 10))
}

func (r *HtmlRenderer) trimFieldVal(s string) string {
	start := 0
	for start < len(s) {
		c := s[start]
		if c != 0 && c != ' ' && c != '_' {
			break
		}
		start++
	}
	end := len(s)
	for end > start {
		c := s[end-1]
		if c != 0 && c != ' ' && c != '_' {
			break
		}
		end--
	}
	return s[start:end]
}
