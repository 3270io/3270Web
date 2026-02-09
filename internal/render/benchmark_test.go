package render

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

func BenchmarkRenderInputFields(b *testing.B) {
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

	// Create many INPUT (unprotected) fields
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x += 10 {
			// Unprotected field (no AttrProtected bit)
			f := host.NewField(screen, 0, x, y, x+8, y, host.AttrColDefault, host.AttrEhDefault)
			f.SetValue("Testing")
			screen.Fields = append(screen.Fields, f)
		}
	}

	r := NewHtmlRenderer()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.Render(screen, "/submit", "session1")
	}
}
