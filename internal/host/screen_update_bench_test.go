// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"strings"
	"testing"
)

// benchScreenLines builds a one-line 80x24 ReadBuffer reply with a field
// attribute at the start of every row, like a typical formatted screen.
func benchScreenLines() (string, []string) {
	var sb strings.Builder
	sb.WriteString("data:")
	for r := 0; r < 24; r++ {
		sb.WriteString(" SF(c0=e0)")
		for c := 1; c < 80; c++ {
			sb.WriteString(" 40")
		}
	}
	status := "U F U C(localhost) I 2 24 80 0 0 0x0 -"
	return status, []string{sb.String()}
}

func BenchmarkScreenUpdate(b *testing.B) {
	status, lines := benchScreenLines()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := &Screen{}
		if err := s.Update(status, lines); err != nil {
			b.Fatal(err)
		}
	}
}
