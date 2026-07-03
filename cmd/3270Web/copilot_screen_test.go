package main

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/host"
)

// TestScreenToJSONRedactsHiddenFields guards against sending typed
// password/hidden-field values to the Copilot API (and from there into the
// browser's tool-card transcript and localStorage). get_screen must report
// hidden fields with an empty value and must not leak their characters via
// the raw screen "text" dump either, since Screen.Text() renders the actual
// buffer contents regardless of the field's hidden attribute.
func TestScreenToJSONRedactsHiddenFields(t *testing.T) {
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
	hidden := host.NewField(s, host.AttrDisp1|host.AttrDisp2, 10, 0, 19, 0, 0, 0)
	s.Fields = append(s.Fields, hidden)
	copy(s.Buffer[0][10:], []rune("hunter2   "))
	// Ordinary unprotected input at row 1, cols 0-9, should pass through.
	visible := host.NewField(s, 0x00, 0, 1, 9, 1, 0, 0)
	s.Fields = append(s.Fields, visible)
	copy(s.Buffer[1][0:], []rune("alice     "))

	out := screenToJSON(s)

	text, _ := out["text"].(string)
	if strings.Contains(text, "hunter2") {
		t.Fatalf("screen text leaked hidden field value: %q", text)
	}
	if !strings.Contains(text, "alice") {
		t.Fatalf("screen text should still contain visible field value: %q", text)
	}

	fields, ok := out["fields"].([]gin.H)
	if !ok {
		t.Fatalf("fields has unexpected type %T", out["fields"])
	}

	var sawHidden, sawVisible bool
	for _, f := range fields {
		row, _ := f["row"].(int)
		if row == 0 && f["col"] == 10 {
			sawHidden = true
			if hidden, _ := f["hidden"].(bool); !hidden {
				t.Error("expected hidden=true on the password field")
			}
			if val, _ := f["value"].(string); val != "" {
				t.Errorf("hidden field value should be redacted, got %q", val)
			}
		}
		if row == 1 && f["col"] == 0 {
			sawVisible = true
			if val, _ := f["value"].(string); !strings.HasPrefix(val, "alice") {
				t.Errorf("visible field value should pass through unmodified, got %q", val)
			}
		}
	}
	if !sawHidden {
		t.Fatal("hidden field not found in fields output")
	}
	if !sawVisible {
		t.Fatal("visible field not found in fields output")
	}
}
