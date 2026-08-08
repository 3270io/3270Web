package main

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/host"
)

// passwordScreen builds a screen with a hidden password field holding
// "hunter2" and an ordinary input holding "alice".
func passwordScreen() *host.Screen {
	s := &host.Screen{
		Width:       80,
		Height:      3,
		IsFormatted: true,
		Buffer:      make([][]rune, 3),
	}
	for i := range s.Buffer {
		s.Buffer[i] = make([]rune, 80)
		for x := range s.Buffer[i] {
			s.Buffer[i][x] = ' '
		}
	}
	// Protected label "Password:" at row 0, cols 0-8.
	s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, 0, 8, 0, 0, 0))
	// Hidden (password) input at row 0, cols 10-19.
	s.Fields = append(s.Fields, host.NewField(s, host.AttrDisp1|host.AttrDisp2, 10, 0, 19, 0, 0, 0))
	copy(s.Buffer[0][10:], []rune("hunter2   "))
	// Ordinary unprotected input at row 1, cols 0-9.
	s.Fields = append(s.Fields, host.NewField(s, 0x00, 0, 1, 9, 1, 0, 0))
	copy(s.Buffer[1][0:], []rune("alice     "))
	return s
}

// TestEveryScreenSerializerRedactsHiddenFields runs the hidden-field rule
// over every function that turns a *host.Screen into JSON.
//
// screenToPublicJSON shipped without the redaction screenToJSON has, so
// /api/v1 returned typed passwords in both `value` and `text` to any bearer
// token holder. Enumerating the serializers here — rather than testing one
// of them — is what makes the next serializer fail loudly instead of
// quietly repeating it.
func TestEveryScreenSerializerRedactsHiddenFields(t *testing.T) {
	// Each serializer names its own keys; the rule is identical.
	serializers := []struct {
		name      string
		render    func(*host.Screen) gin.H
		rowKey    string
		colKey    string
		hiddenAt  [2]int // row, col of the hidden field
		visibleAt [2]int
	}{
		{"screenToJSON", screenToJSON, "row", "col", [2]int{0, 10}, [2]int{1, 0}},
		{"screenToPublicJSON", screenToPublicJSON, "start_row", "start_col", [2]int{0, 10}, [2]int{1, 0}},
	}

	for _, sz := range serializers {
		t.Run(sz.name, func(t *testing.T) {
			out := sz.render(passwordScreen())

			text, _ := out["text"].(string)
			if strings.Contains(text, "hunter2") {
				t.Errorf("screen text leaked hidden field value: %q", text)
			}
			if !strings.Contains(text, "alice") {
				t.Errorf("screen text should still contain the visible field value: %q", text)
			}

			fields, ok := out["fields"].([]gin.H)
			if !ok {
				t.Fatalf("fields has unexpected type %T", out["fields"])
			}

			var sawHidden, sawVisible bool
			for _, f := range fields {
				row, _ := f[sz.rowKey].(int)
				col, _ := f[sz.colKey].(int)
				val, _ := f["value"].(string)
				switch {
				case row == sz.hiddenAt[0] && col == sz.hiddenAt[1]:
					sawHidden = true
					if h, _ := f["hidden"].(bool); !h {
						t.Error("expected hidden=true on the password field")
					}
					if val != "" {
						t.Errorf("hidden field value should be redacted, got %q", val)
					}
				case row == sz.visibleAt[0] && col == sz.visibleAt[1]:
					sawVisible = true
					if !strings.HasPrefix(val, "alice") {
						t.Errorf("visible field value should pass through unmodified, got %q", val)
					}
				}
			}
			if !sawHidden {
				t.Error("hidden field not found in fields output")
			}
			if !sawVisible {
				t.Error("visible field not found in fields output")
			}
		})
	}
}
