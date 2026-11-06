// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"math/rand"
	"strings"
	"testing"
)

// referenceLevenshtein is the plain two-row DP with no prefix/suffix trimming.
func referenceLevenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			m := prev[j] + 1
			if curr[j-1]+1 < m {
				m = curr[j-1] + 1
			}
			if prev[j-1]+cost < m {
				m = prev[j-1] + cost
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func TestLevenshteinTrimMatchesPlainDP(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	alphabet := []rune("ab9 ")
	gen := func(n int) []rune {
		out := make([]rune, n)
		for i := range out {
			out[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return out
	}
	for i := 0; i < 2000; i++ {
		a := gen(rng.Intn(30))
		b := append([]rune(nil), a...)
		for k := rng.Intn(4); k > 0 && len(b) > 0; k-- {
			switch rng.Intn(3) {
			case 0:
				b[rng.Intn(len(b))] = alphabet[rng.Intn(len(alphabet))]
			case 1:
				j := rng.Intn(len(b))
				b = append(b[:j], b[j+1:]...)
			default:
				j := rng.Intn(len(b) + 1)
				b = append(b[:j], append([]rune{'a'}, b[j:]...)...)
			}
		}
		if i%5 == 0 {
			b = gen(rng.Intn(30))
		}
		if got, want := levenshteinDistance(a, b), referenceLevenshtein(a, b); got != want {
			t.Fatalf("distance(%q,%q) = %d, want %d", string(a), string(b), got, want)
		}
	}
}

func BenchmarkSimilarityRatioNearDuplicateScreen(b *testing.B) {
	base := strings.Repeat("AAAA aaaa 9999 :.\n", 107)
	other := []byte(base)
	other[500] = '9'
	other[1500] = '.'
	a, c := base, string(other)
	var p, q []int
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		similarityRatioBuf(a, c, &p, &q)
	}
}
