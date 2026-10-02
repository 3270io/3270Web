// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

// referenceHashScreen is the original Fprintf-per-cell implementation, kept so
// the buffered hashScreen is proven byte-for-byte equivalent.
func referenceHashScreen(s *host.Screen) string {
	width, height := screenDimensions(s)
	masked := make([]bool, width*height)
	for _, f := range s.Fields {
		if f == nil || f.IsProtected() {
			continue
		}
		curX, curY := f.StartX, f.StartY
		for {
			if curX >= 0 && curX < width && curY >= 0 && curY < height {
				masked[(curY*width)+curX] = true
			}
			if curX == f.EndX && curY == f.EndY {
				break
			}
			curX++
			if curX >= width {
				curX = 0
				curY++
				if curY >= height {
					break
				}
			}
		}
	}
	h := sha256.New()
	fmt.Fprintf(h, "%dx%d|", width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			ch := s.CharAt(x, y)
			if ch == 0 || masked[(y*width)+x] {
				ch = ' '
			}
			fmt.Fprintf(h, "%c", ch)
		}
		fmt.Fprint(h, "\n")
	}
	fmt.Fprintf(h, "|%d", len(s.Fields))
	for _, f := range s.Fields {
		if f == nil {
			continue
		}
		fmt.Fprintf(h, "|%d,%d,%d,%d,%d", f.StartY, f.StartX, f.EndY, f.EndX, f.FieldCode)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func benchHashScreen() *host.Screen {
	s := &host.Screen{Width: 80, Height: 24, IsFormatted: true, Buffer: make([][]rune, 24)}
	for y := range s.Buffer {
		s.Buffer[y] = make([]rune, 80)
		for x := range s.Buffer[y] {
			s.Buffer[y][x] = 'A' + rune((x+y)%26)
		}
	}
	s.Buffer[3][5] = 'é'
	s.Buffer[4][6] = 0
	s.Buffer[5][7] = -1
	for y := 0; y < 24; y += 2 {
		f := host.NewField(s, 0, 10, y, 18, y, host.AttrColDefault, host.AttrEhDefault)
		s.Fields = append(s.Fields, f)
	}
	return s
}

func TestHashScreenMatchesReferenceImplementation(t *testing.T) {
	s := benchHashScreen()
	if got, want := hashScreen(s), referenceHashScreen(s); got != want {
		t.Fatalf("hashScreen = %s, reference = %s", got, want)
	}
}

func BenchmarkHashScreen(b *testing.B) {
	s := benchHashScreen()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		hashScreen(s)
	}
}

func BenchmarkHashScreenReference(b *testing.B) {
	s := benchHashScreen()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		referenceHashScreen(s)
	}
}
