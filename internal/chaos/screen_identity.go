// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
)

// Tying what exploration learned to the screen in front of the operator.
//
// The mind map is keyed by a screen fingerprint, and everything discovery
// knows — what a screen is for, which keys led somewhere, what each field
// means — hangs off that key. Nothing outside the engine could compute it, so
// a caller holding a live screen had no way to ask "have you seen this one?".
// These are the engine's own fingerprints, exported; they call the functions
// the engine calls and decide nothing themselves.

// ScreenHash returns the raw fingerprint of a screen: its protected text and
// field structure, with input-field contents and the cursor left out so the
// same logical screen hashes the same whatever has been typed into it.
func ScreenHash(s *host.Screen) string {
	return hashScreen(s)
}

// StructuralScreenHash returns the key the engine files a screen under in its
// default (structural) dedup mode, where screens with the same layout collapse
// into one area regardless of the values echoed in them. Empty when the screen
// has no usable dimensions.
func StructuralScreenHash(s *host.Screen) string {
	return signatureCanonicalKey(buildAreaDedupSignature(s))
}

// DedupSignature returns the layout signature the engine stores on an area and
// compares near-identical screens by.
func DedupSignature(s *host.Screen) string {
	return buildAreaDedupSignature(s)
}

// areaMatchSimilarity is the engine's default threshold for folding a
// near-identical screen into an existing area.
const areaMatchSimilarity = 0.95

// AreaForScreen finds the discovered area a live screen belongs to.
//
// The engine may have filed the screen under its raw hash (exact mode), under
// its structural key (the default), or folded it into a similar area, so the
// lookup tries each in that order — exact matches first, then the same
// similarity test the engine applies. It returns the key the area is stored
// under, which is the hash every other chaos tool takes.
func AreaForScreen(mm *MindMap, s *host.Screen) (string, *MindMapArea, bool) {
	if mm == nil || s == nil || len(mm.Areas) == 0 {
		return "", nil, false
	}
	for _, key := range []string{hashScreen(s), StructuralScreenHash(s)} {
		if key == "" {
			continue
		}
		if area, ok := mm.Areas[key]; ok && area != nil {
			return key, area, true
		}
	}

	sig := buildAreaDedupSignature(s)
	if sig == "" {
		return "", nil, false
	}
	titleSig := areaTitleDedupSignatureFromScreen(s)
	width, height := screenDimensions(s)
	fields := len(s.Fields)

	bestKey, bestScore := "", 0.0
	var prevRow, currRow []int
	for key, area := range mm.Areas {
		if area == nil || strings.TrimSpace(key) == "" || area.DedupSignature == "" {
			continue
		}
		if area.ScreenWidth > 0 && area.ScreenHeight > 0 && (area.ScreenWidth != width || area.ScreenHeight != height) {
			continue
		}
		if diff := area.FieldCount - fields; diff > 2 || diff < -2 {
			continue
		}
		score := similarityRatioBuf(sig, normalizeDedupSignature(area.DedupSignature), &prevRow, &currRow)
		if titleSig != "" {
			if areaTitle := normalizeAreaTitleDedupSignature(area.Label); areaTitle != "" {
				score = score*0.9 + similarityRatioBuf(titleSig, areaTitle, &prevRow, &currRow)*0.1
			}
		}
		if score > bestScore || (score == bestScore && key < bestKey) {
			bestKey, bestScore = key, score
		}
	}
	if bestKey != "" && bestScore >= areaMatchSimilarity {
		return bestKey, mm.Areas[bestKey], true
	}
	return "", nil, false
}

// CurrentScreenHash returns the hash to report for a live screen: the key of
// the discovered area it matches when there is one, otherwise the key a
// default-mode run would file it under, otherwise the raw fingerprint.
func CurrentScreenHash(mm *MindMap, s *host.Screen) string {
	if key, _, ok := AreaForScreen(mm, s); ok {
		return key
	}
	if key := StructuralScreenHash(s); key != "" {
		return key
	}
	return hashScreen(s)
}

// ProductiveKeys lists the keys that have led somewhere from an area, each
// with the areas it led to, most-used destination first.
func ProductiveKeys(area *MindMapArea) map[string][]string {
	if area == nil {
		return nil
	}
	out := map[string][]string{}
	for key, press := range area.KeyPresses {
		if press == nil || press.Progressions <= 0 {
			continue
		}
		dests := make([]string, 0, len(press.Destinations))
		for dest := range press.Destinations {
			dests = append(dests, dest)
		}
		sortByCountThenName(dests, press.Destinations)
		out[key] = dests
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortByCountThenName(names []string, counts map[string]int) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0; j-- {
			a, b := names[j-1], names[j]
			if counts[a] > counts[b] || (counts[a] == counts[b] && a <= b) {
				break
			}
			names[j-1], names[j] = b, a
		}
	}
}
