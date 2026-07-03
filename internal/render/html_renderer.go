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
	sb.WriteString(`" autocomplete="off" data-form-type="other">`)
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

func (r *HtmlRenderer) renderFormatted(s *host.Screen, id string, sb *strings.Builder) {
	sb.WriteString("<pre>")

	// rowLabels tracks the most recently rendered protected field's text per
	// row (keyed by StartY), so an unprotected field can be given an
	// aria-label derived from the label immediately to its left — without
	// this, screen readers announce every field as a bare "edit text" with
	// no indication of what it's for (fields are anonymous <input>s with no
	// association to the surrounding protected label text).
	rowLabels := make(map[int]string)

	for _, f := range s.Fields {
		// Append attribute spacer
		if f.StartX == 0 {
			if f.StartY > 0 {
				sb.WriteString(" \n")
			}
		} else {
			sb.WriteString(" ")
		}

		if !f.IsProtected() {
			r.renderInputField(sb, f, id, rowLabels[f.StartY])
		} else {
			needSpan := r.needSpan(f)
			if needSpan {
				sb.WriteString(`<span class="`)
				r.writeProtectedFieldClass(sb, f)
				sb.WriteString(`"`)
				r.writeFieldDebugDataAttrs(sb, f)
				sb.WriteString(`>`)
			}

			r.writeEscaped(sb, f.GetValue())

			if needSpan {
				sb.WriteString("</span>")
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

func (r *HtmlRenderer) renderInputField(sb *strings.Builder, f *host.Field, id, ariaLabel string) {
	if !f.IsMultiline() {
		// Optimization: Avoid GetValueLines() allocation for single line fields
		val, _, _ := strings.Cut(f.GetValue(), "\n")
		width := f.EndX - f.StartX + 1
		r.createHtmlInput(sb, f, id, val, -1, width, ariaLabel)
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
			r.createHtmlInput(sb, f, id, val, i, w, lineLabel)
			if i < f.Height()-1 {
				sb.WriteString("\n")
			}
		}
	}
}

func (r *HtmlRenderer) createHtmlInput(sb *strings.Builder, f *host.Field, id, val string, lineNum, width int, ariaLabel string) {
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
	sb.WriteString(` autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" inputmode="text" />`)
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

func (r *HtmlRenderer) needSpan(f *host.Field) bool {
	// Hidden styling is only applied to input fields; protected text remains visible even if
	// a host screen advertises a hidden FA, which avoids hiding labels when FA decoding is off.
	return f.IsIntensified() || f.Color != host.AttrColDefault || f.ExtendedHighlight != host.AttrEhDefault
}

func (r *HtmlRenderer) writeProtectedFieldClass(sb *strings.Builder, f *host.Field) {
	first := true
	if f.IsIntensified() {
		sb.WriteString("color-intensified")
		first = false
	}

	if f.Color != host.AttrColDefault {
		c := ""
		switch f.Color {
		case host.AttrColBlue:
			c = "color-blue"
		case host.AttrColRed:
			c = "color-red"
		case host.AttrColPink:
			c = "color-pink"
		case host.AttrColGreen:
			c = "color-green"
		case host.AttrColTurquoise:
			c = "color-turquoise"
		case host.AttrColYellow:
			c = "color-yellow"
		case host.AttrColWhite:
			c = "color-white"
		}
		if c != "" {
			if !first {
				sb.WriteString(" ")
			}
			sb.WriteString(c)
			first = false
		}
	}

	if f.ExtendedHighlight != host.AttrEhDefault {
		h := ""
		switch f.ExtendedHighlight {
		case host.AttrEhBlink:
			h = "highlight-blink"
		case host.AttrEhRevVideo:
			h = "highlight-rev-video"
		case host.AttrEhUnderscore:
			h = "highlight-underscore"
		}
		if h != "" {
			if !first {
				sb.WriteString(" ")
			}
			sb.WriteString(h)
		}
	}
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

func (r *HtmlRenderer) writeFieldColorAndHighlightClasses(sb *strings.Builder, f *host.Field) {
	if f == nil {
		return
	}
	if f.Color != host.AttrColDefault {
		c := ""
		switch f.Color {
		case host.AttrColBlue:
			c = "color-blue"
		case host.AttrColRed:
			c = "color-red"
		case host.AttrColPink:
			c = "color-pink"
		case host.AttrColGreen:
			c = "color-green"
		case host.AttrColTurquoise:
			c = "color-turquoise"
		case host.AttrColYellow:
			c = "color-yellow"
		case host.AttrColWhite:
			c = "color-white"
		}
		if c != "" {
			sb.WriteString(" ")
			sb.WriteString(c)
		}
	}
	if f.ExtendedHighlight != host.AttrEhDefault {
		h := ""
		switch f.ExtendedHighlight {
		case host.AttrEhBlink:
			h = "highlight-blink"
		case host.AttrEhRevVideo:
			h = "highlight-rev-video"
		case host.AttrEhUnderscore:
			h = "highlight-underscore"
		}
		if h != "" {
			sb.WriteString(" ")
			sb.WriteString(h)
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
