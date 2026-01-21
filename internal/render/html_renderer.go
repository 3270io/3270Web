package render

import (
	"fmt"
	"html"
	"strings"

	"github.com/jnnngs/h3270/internal/host"
)

type HtmlRenderer struct{}

func NewHtmlRenderer() *HtmlRenderer {
	return &HtmlRenderer{}
}

func (r *HtmlRenderer) Render(s *host.Screen, actionURL, id string) string {
	var sb strings.Builder
	formName := r.getFormName(id)

	sb.WriteString(fmt.Sprintf(`<form id="%s" name="%s" action="%s" method="post" class="h3270-form">`, formName, formName, actionURL))
	sb.WriteString("\n")

	if s.IsFormatted {
		r.renderFormatted(s, id, &sb)
	} else {
		r.renderUnformatted(s, &sb)
	}

	sb.WriteString(`<div><input type="hidden" name="key" /></div>`)
	sb.WriteString("\n")
	if id != "" {
		sb.WriteString(fmt.Sprintf(`<div><input type="hidden" name="TERMINAL" value="%s"></div>`, id))
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
				sb.WriteString(fmt.Sprintf(`<span class="%s">`, r.protectedFieldClass(f)))
			}

			val := f.GetValue()
			escaped := html.EscapeString(val)
			escaped = strings.ReplaceAll(escaped, "\u0000", " ")
			sb.WriteString(escaped)

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
	sb.WriteString(fmt.Sprintf(`<textarea name="field" class="h3270-unformatted" rows="%d" cols="%d">`, rows, cols))
	sb.WriteString(html.EscapeString(text))
	sb.WriteString("</textarea>")
}

func (r *HtmlRenderer) renderInputField(sb *strings.Builder, f *host.Field, id string) {
	lines := f.GetValueLines()

	if !f.IsMultiline() {
		val := ""
		if len(lines) > 0 {
			val = lines[0]
		}
		r.createHtmlInput(sb, f, id, val, -1, f.EndX-f.StartX+1)
	} else {
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

	name := fmt.Sprintf("field_%d_%d", f.StartX, f.StartY)
	if lineNum != -1 {
		name += fmt.Sprintf("_%d", lineNum)
	}

	class := "h3270-input"
	if f.IsIntensified() {
		class = "h3270-input-intensified"
	} else if f.IsHidden() {
		class = "h3270-input-hidden"
	}

	val = strings.Trim(val, "\x00 _")

	dataX := f.StartX
	dataY := f.StartY
	if lineNum > 0 {
		dataY += lineNum
	}

	sb.WriteString(fmt.Sprintf(`<input type="%s" name="%s" class="%s" value="%s" maxlength="%d" size="%d" data-x="%d" data-y="%d" data-w="%d" />`,
		inputType, name, class, html.EscapeString(val), width, width, dataX, dataY, width))
}

func (r *HtmlRenderer) needSpan(f *host.Field) bool {
	return f.IsIntensified() || f.IsHidden() || f.Color != host.AttrColDefault || f.ExtendedHighlight != host.AttrEhDefault
}

func (r *HtmlRenderer) protectedFieldClass(f *host.Field) string {
	var classes []string
	if f.IsIntensified() {
		classes = append(classes, "h3270-intensified")
	} else if f.IsHidden() {
		classes = append(classes, "h3270-hidden")
	}

	if f.Color != host.AttrColDefault {
		c := ""
		switch f.Color {
		case host.AttrColBlue:
			c = "h3270-color-blue"
		case host.AttrColRed:
			c = "h3270-color-red"
		case host.AttrColPink:
			c = "h3270-color-pink"
		case host.AttrColGreen:
			c = "h3270-color-green"
		case host.AttrColTurquoise:
			c = "h3270-color-turquoise"
		case host.AttrColYellow:
			c = "h3270-color-yellow"
		case host.AttrColWhite:
			c = "h3270-color-white"
		}
		if c != "" {
			classes = append(classes, c)
		}
	}

	if f.ExtendedHighlight != host.AttrEhDefault {
		h := ""
		switch f.ExtendedHighlight {
		case host.AttrEhBlink:
			h = "h3270-highlight-blink"
		case host.AttrEhRevVideo:
			h = "h3270-highlight-rev-video"
		case host.AttrEhUnderscore:
			h = "h3270-highlight-underscore"
		}
		if h != "" {
			classes = append(classes, h)
		}
	}
	return strings.Join(classes, " ")
}

func (r *HtmlRenderer) getFormName(id string) string {
	if id == "" {
		return "screen"
	}
	return "screen-" + id
}

func (r *HtmlRenderer) appendFocus(s *host.Screen, id string, sb *strings.Builder) {
	sb.WriteString(`<script type="text/javascript">` + "\n")
	fn := r.getFormName(id)
	sb.WriteString(fmt.Sprintf(`  installKeyHandler('%s');`+"\n", fn))
	if !s.IsFormatted {
		sb.WriteString(fmt.Sprintf(`  document.forms["%s"].field.focus()`+"\n", fn))
		sb.WriteString("</script>\n")
		return
	}

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
		sb.WriteString(fmt.Sprintf(`  document.forms["%s"].field_%d_%d%s.focus()`+"\n",
			fn, focused.StartX, focused.StartY, suffix))
	}
	sb.WriteString("</script>\n")
}
