package render

import (
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

	sb.WriteString(`<form id="`)
	sb.WriteString(formName)
	sb.WriteString(`" name="`)
	sb.WriteString(formName)
	sb.WriteString(`" action="`)
	sb.WriteString(actionURL)
	sb.WriteString(`" method="post" class="renderer-form">`)
	sb.WriteString("\n")

	if s.IsFormatted {
		r.renderFormatted(s, id, &sb)
	} else {
		r.renderUnformatted(s, &sb)
	}

	sb.WriteString(`<div><input type="hidden" name="key" /></div>`)
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
			r.renderInputField(sb, f, id)
		} else {
			needSpan := r.needSpan(f)
			if needSpan {
				sb.WriteString(`<span class="`)
				r.writeProtectedFieldClass(sb, f)
				sb.WriteString(`">`)
			}

			r.writeEscaped(sb, f.GetValue())

			if needSpan {
				sb.WriteString("</span>")
			}
		}

		if f.EndX == s.Width-1 && f.EndY >= f.StartY {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("</pre>")
}

func (r *HtmlRenderer) renderUnformatted(s *host.Screen, sb *strings.Builder) {
	rows := s.Height
	cols := s.Width
	if rows <= 0 {
		rows = 24
	}
	if cols <= 0 {
		cols = 80
	}

	text := s.Text()
	sb.WriteString(`<textarea name="field" class="unformatted" rows="`)
	r.writeInt(sb, rows)
	sb.WriteString(`" cols="`)
	r.writeInt(sb, cols)
	sb.WriteString(`">`)
	r.writeEscaped(sb, text)
	sb.WriteString("</textarea>")
}

func (r *HtmlRenderer) renderInputField(sb *strings.Builder, f *host.Field, id string) {
	if !f.IsMultiline() {
		// Optimization: Avoid GetValueLines() allocation for single line fields
		val, _, _ := strings.Cut(f.GetValue(), "\n")
		width := f.EndX - f.StartX
		r.createHtmlInput(sb, f, id, val, -1, width)
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
					w = f.Screen.Width - f.StartX - 1
				} else {
					w = f.Screen.Width
				}
			} else {
				w = f.EndX + 1
			}

			r.createHtmlInput(sb, f, id, val, i, w)
			if i < f.Height()-1 {
				sb.WriteString("\n")
			}
		}
	}
}

func (r *HtmlRenderer) createHtmlInput(sb *strings.Builder, f *host.Field, id, val string, lineNum, width int) {
	inputType := "text"
	if f.IsHidden() {
		inputType = "password"
	}

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
	sb.WriteString(`" value="`)
	r.writeEscaped(sb, val)
	sb.WriteString(`" maxlength="`)
	r.writeInt(sb, width)
	sb.WriteString(`" size="`)
	r.writeInt(sb, width)
	sb.WriteString(`" data-x="`)
	r.writeInt(sb, dataX)
	sb.WriteString(`" data-y="`)
	r.writeInt(sb, dataY)
	sb.WriteString(`" data-w="`)
	r.writeInt(sb, width)
	sb.WriteString(`" autocomplete="off" autocorrect="off" autocapitalize="off" spellcheck="false" inputmode="text" />`)
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
	return f.IsIntensified() || f.IsHidden() || f.Color != host.AttrColDefault || f.ExtendedHighlight != host.AttrEhDefault
}

func (r *HtmlRenderer) writeProtectedFieldClass(sb *strings.Builder, f *host.Field) {
	first := true
	if f.IsIntensified() {
		sb.WriteString("color-intensified")
		first = false
	} else if f.IsHidden() {
		sb.WriteString("color-hidden")
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

func (r *HtmlRenderer) getFormName(id string) string {
	if id == "" {
		return "screen"
	}
	return "screen-" + id
}

func (r *HtmlRenderer) appendFocus(s *host.Screen, id string, sb *strings.Builder) {
	sb.WriteString(`<script type="text/javascript">` + "\n")
	sb.WriteString("  window.addEventListener(\"DOMContentLoaded\", function () {\n")
	fn := r.getFormName(id)
	sb.WriteString(`    installKeyHandler('`)
	sb.WriteString(fn)
	sb.WriteString(`');` + "\n")
	if !s.IsFormatted {
		sb.WriteString(`    document.forms["`)
		sb.WriteString(fn)
		sb.WriteString(`"].field.focus()` + "\n")
	} else {
		var focused *host.Field
		for _, f := range s.Fields {
			if f.Focused {
				focused = f
				break
			}
		}

		if focused != nil {
			suffix := ""
			if focused.IsMultiline() {
				suffix = "_0"
			}
			sb.WriteString(`    document.forms["`)
			sb.WriteString(fn)
			sb.WriteString(`"].field_`)
			r.writeInt(sb, focused.StartX)
			sb.WriteString(`_`)
			r.writeInt(sb, focused.StartY)
			sb.WriteString(suffix)
			sb.WriteString(`.focus()` + "\n")
		}
	}
	sb.WriteString("  });\n")
	sb.WriteString("</script>\n")
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
