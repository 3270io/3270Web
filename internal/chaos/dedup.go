// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"math"
	"sort"
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
)

// Area dedup.
//
// Every observed screen is reduced to a layout signature and mapped to an
// area. Three things keep that mapping stable and cheap:
//
//   - An area is matched against the signature of the screen that founded it,
//     which never changes afterwards (see MindMap.observeScreen), so merging
//     one near-duplicate does not move the area towards the next.
//   - A signature that has been resolved once is remembered in an alias map,
//     so revisiting a merged screen is a map lookup, not a comparison against
//     every area.
//   - The comparison itself is a cut-off edit distance: it gives up as soon
//     as the distance exceeds what the similarity threshold allows, and it is
//     run without the engine lock held.

// maxSignatureAliases bounds the alias map. When it fills it is emptied and
// rebuilt on demand; every area is still found under its own key.
const maxSignatureAliases = 4096

// crossSizeRowContainment is the share of the smaller screen's rows that
// must reappear, in order, in the larger one for the two to be treated as
// the same screen at different terminal sizes.
const crossSizeRowContainment = 0.85

// dedupRef is the part of an area the matcher needs, copied out so the
// comparison can run without the engine lock.
type dedupRef struct {
	hash       string
	sig        string
	titleSig   string
	width      int
	height     int
	fieldCount int
}

// dedupRefsLocked snapshots the areas for matching, sorted by hash so a tie
// between equally good matches is always broken the same way. Caller must
// hold e.mu.
func (e *Engine) dedupRefsLocked() []dedupRef {
	if e.mindMap == nil || len(e.mindMap.Areas) == 0 {
		return nil
	}
	refs := make([]dedupRef, 0, len(e.mindMap.Areas))
	for hash, area := range e.mindMap.Areas {
		if area == nil || strings.TrimSpace(hash) == "" || area.DedupSignature == "" {
			continue
		}
		refs = append(refs, dedupRef{
			hash:       hash,
			sig:        area.DedupSignature,
			titleSig:   normalizeAreaTitleDedupSignature(area.Label),
			width:      area.ScreenWidth,
			height:     area.ScreenHeight,
			fieldCount: area.FieldCount,
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].hash < refs[j].hash })
	return refs
}

func (e *Engine) dedupThreshold() float64 {
	threshold := e.cfg.ScreenDedupSimilarity
	if threshold <= 0 || threshold > 1 {
		threshold = 0.95
	}
	return threshold
}

// matchDedupArea returns the hash of the area a candidate screen should be
// merged into, or "" when none is similar enough. refs must be sorted by
// hash; among equal scores the first (smallest hash) wins.
func matchDedupArea(candidateSig, candidateTitleSig string, cw, ch, candidateFields int, refs []dedupRef, threshold float64) string {
	if candidateSig == "" || len(refs) == 0 {
		return ""
	}
	bestHash := ""
	bestScore := 0.0
	candidateRunes := []rune(candidateSig)
	var candidateRows []string
	var prevRow, currRow []int
	for _, ref := range refs {
		if ref.width > 0 && ref.height > 0 && (ref.width != cw || ref.height != ch) {
			// A different terminal size: the cell-for-cell signature cannot
			// match, so compare what the two screens say row by row.
			if candidateRows == nil {
				candidateRows = contentRowsFromSignature(candidateSig)
			}
			refRows := contentRowsFromSignature(ref.sig)
			score := crossSizeRowScore(candidateRows, refRows)
			if score < crossSizeRowContainment && candidateTitleSig != "" && candidateTitleSig == ref.titleSig {
				// The rows did not line up, but the two screens carry the
				// same title in words. A screen that lays itself out again
				// for the size — two columns folded into one, a line that
				// states the size — says the same things in different rows,
				// so compare what is said rather than where.
				score = crossSizeTokenScore(candidateRows, refRows)
			}
			if score >= crossSizeRowContainment && score > bestScore {
				bestScore = score
				bestHash = ref.hash
			}
			continue
		}
		// Coarse structural gate before the string compare.
		if ref.fieldCount > 0 || candidateFields > 0 {
			diff := ref.fieldCount - candidateFields
			if diff < 0 {
				diff = -diff
			}
			if diff > 2 {
				continue
			}
		}
		useTitle := candidateTitleSig != "" && ref.titleSig != ""
		// The lowest body score that can still reach the threshold once the
		// title's 10% is counted at its best.
		minBody := threshold
		if useTitle {
			minBody = (threshold - 0.1) / 0.9
		}
		bodyScore, ok := similarityRatioCapped(candidateRunes, []rune(ref.sig), minBody, &prevRow, &currRow)
		if !ok {
			continue
		}
		score := bodyScore
		if useTitle {
			// Use the captured title as a light discriminator so screens with
			// nearly identical layouts but different headings are less likely
			// to collapse into one "unique screen".
			titleScore := similarityRatioBuf(candidateTitleSig, ref.titleSig, &prevRow, &currRow)
			score = (bodyScore * 0.9) + (titleScore * 0.1)
		}
		if score >= threshold && score > bestScore {
			bestScore = score
			bestHash = ref.hash
		}
	}
	return bestHash
}

// similarityRatioCapped is similarityRatio that stops early: it reports
// ok=false as soon as the two cannot be at least minRatio alike.
func similarityRatioCapped(a, b []rune, minRatio float64, prevRow, currRow *[]int) (ratio float64, ok bool) {
	if len(a) == 0 || len(b) == 0 {
		return 0, false
	}
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if minRatio < 0 {
		minRatio = 0
	}
	limit := int(math.Floor((1-minRatio)*float64(maxLen) + 1e-9))
	dist := levenshteinDistanceCapped(a, b, limit, prevRow, currRow)
	if dist > limit {
		return 0, false
	}
	return 1 - float64(dist)/float64(maxLen), true
}

// levenshteinDistanceCapped computes the edit distance between a and b, or
// returns limit+1 as soon as it is known to exceed limit. Only the diagonal
// band of width 2*limit+1 is filled, so two long signatures that differ
// substantially cost O(limit * len) rather than O(len * len), and two that
// differ in length by more than limit cost nothing at all.
func levenshteinDistanceCapped(a, b []rune, limit int, prevRow, currRow *[]int) int {
	if limit < 0 {
		limit = 0
	}
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	la, lb := len(a), len(b)
	if la-lb > limit {
		return limit + 1
	}
	if lb == 0 {
		return la
	}
	const inf = math.MaxInt32 / 2
	need := lb + 2
	if cap(*prevRow) < need {
		*prevRow = make([]int, need)
	} else {
		*prevRow = (*prevRow)[:need]
	}
	if cap(*currRow) < need {
		*currRow = make([]int, need)
	} else {
		*currRow = (*currRow)[:need]
	}
	pr, cr := *prevRow, *currRow
	for j := 0; j <= lb+1; j++ {
		if j <= limit && j <= lb {
			pr[j] = j
		} else {
			pr[j] = inf
		}
	}
	for i := 1; i <= la; i++ {
		lo := i - limit
		if lo < 1 {
			lo = 1
		}
		hi := i + limit
		if hi > lb {
			hi = lb
		}
		if lo == 1 && i <= limit {
			cr[0] = i
		} else {
			cr[lo-1] = inf
		}
		rowMin := inf
		for j := lo; j <= hi; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			best := pr[j-1] + cost
			if v := pr[j] + 1; v < best {
				best = v
			}
			if v := cr[j-1] + 1; v < best {
				best = v
			}
			cr[j] = best
			if best < rowMin {
				rowMin = best
			}
		}
		// The cell just past the band must read as unreachable from the next
		// row, which looks one column further right.
		cr[hi+1] = inf
		if rowMin > limit {
			*prevRow, *currRow = pr, cr
			return limit + 1
		}
		pr, cr = cr, pr
	}
	*prevRow, *currRow = pr, cr
	if pr[lb] > limit {
		return limit + 1
	}
	return pr[lb]
}

// contentRowsFromSignature returns the non-blank rows of a dedup signature
// with runs of spaces collapsed: what the screen says, independent of how
// many rows and columns the terminal has.
func contentRowsFromSignature(sig string) []string {
	start := strings.IndexByte(sig, '|')
	if start < 0 {
		return nil
	}
	body := sig[start+1:]
	if end := strings.LastIndex(body, "|fields:"); end >= 0 {
		body = body[:end]
	}
	var rows []string
	for _, line := range strings.Split(body, "\n") {
		collapsed := strings.Join(strings.Fields(line), " ")
		if collapsed != "" {
			rows = append(rows, collapsed)
		}
	}
	return rows
}

// crossSizeRowScore scores two screens of different sizes by the share of
// the shorter one's rows found, in order, in the longer one. The title row
// has to agree outright: screens that share only their frame are not the
// same screen.
func crossSizeRowScore(a, b []string) float64 {
	if len(a) < 4 || len(b) < 4 || a[0] != b[0] {
		return 0
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	// Longest common subsequence of rows.
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				curr[j] = prev[j-1] + 1
			case prev[j] >= curr[j-1]:
				curr[j] = prev[j]
			default:
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
	}
	return float64(prev[len(b)]) / float64(len(a))
}

// crossSizeTokenScore scores two screens of different sizes by the share of
// words they have in common, counted with repeats and measured against the
// wordier of the two. Measuring against the larger keeps a screen that merely
// contains another from passing: this is for one screen rearranged, and both
// arrangements say about the same amount. The title row has to agree, as in
// crossSizeRowScore.
func crossSizeTokenScore(a, b []string) float64 {
	if len(a) < 4 || len(b) < 4 || a[0] != b[0] {
		return 0
	}
	count := func(rows []string) (map[string]int, int) {
		m := make(map[string]int)
		n := 0
		for _, row := range rows {
			for _, tok := range strings.Fields(row) {
				m[tok]++
				n++
			}
		}
		return m, n
	}
	ta, na := count(a)
	tb, nb := count(b)
	if na == 0 || nb == 0 {
		return 0
	}
	shared := 0
	for tok, ca := range ta {
		if cb := tb[tok]; cb < ca {
			shared += cb
		} else {
			shared += ca
		}
	}
	larger := na
	if nb > larger {
		larger = nb
	}
	return float64(shared) / float64(larger)
}

// rememberSignatureAliasLocked records that a signature key resolves to an
// area. Caller must hold e.mu.
func (e *Engine) rememberSignatureAliasLocked(sigKey, areaHash string) {
	if sigKey == "" || areaHash == "" {
		return
	}
	if e.sigAliases == nil || len(e.sigAliases) >= maxSignatureAliases {
		e.sigAliases = make(map[string]string)
	}
	e.sigAliases[sigKey] = areaHash
}

// resolveKnownSignatureLocked returns the area a signature key is already
// known to resolve to. Caller must hold e.mu.
func (e *Engine) resolveKnownSignatureLocked(sigKey string) (string, bool) {
	if e.mindMap == nil {
		return "", false
	}
	if _, ok := e.mindMap.Areas[sigKey]; ok {
		return sigKey, true
	}
	if alias, ok := e.sigAliases[sigKey]; ok {
		if _, exists := e.mindMap.Areas[alias]; exists {
			return alias, true
		}
		delete(e.sigAliases, sigKey)
	}
	return "", false
}

func (e *Engine) exactDedupMode() bool {
	return strings.EqualFold(strings.TrimSpace(e.cfg.DedupMode), DedupModeExact)
}

// canonicalizeScreenHash maps an observed screen to the key of the area it
// belongs to. It takes the engine lock only to consult and update the alias
// map and to copy out what the matcher needs; the comparison against known
// areas runs unlocked. Only the run goroutine adds observed areas while a
// run is active, so nothing it would have matched can appear in between.
func (e *Engine) canonicalizeScreenHash(rawHash string, screen *host.Screen) string {
	rawHash = strings.TrimSpace(rawHash)
	if rawHash == "" || screen == nil {
		return rawHash
	}
	// Exact mode opts out of structural merging entirely: callers will see
	// every distinct rawHash as its own area.
	if e.exactDedupMode() {
		return rawHash
	}
	candidateSig := buildAreaDedupSignature(screen)
	if candidateSig == "" {
		return rawHash
	}
	sigKey := signatureCanonicalKey(candidateSig)

	e.mu.Lock()
	if known, ok := e.resolveKnownSignatureLocked(sigKey); ok {
		e.mu.Unlock()
		return known
	}
	refs := e.dedupRefsLocked()
	threshold := e.dedupThreshold()
	e.mu.Unlock()

	cw, ch := screenDimensions(screen)
	best := matchDedupArea(candidateSig, areaTitleDedupSignatureFromScreen(screen), cw, ch, len(screen.Fields), refs, threshold)

	e.mu.Lock()
	defer e.mu.Unlock()
	if best != "" {
		if _, exists := e.mindMap.Areas[best]; exists {
			e.rememberSignatureAliasLocked(sigKey, best)
			return best
		}
	}
	return sigKey
}

// canonicalizeObservedScreenHashLocked is canonicalizeScreenHash for callers
// that already hold e.mu. The run loop uses the unlocked form.
func (e *Engine) canonicalizeObservedScreenHashLocked(rawHash string, screen *host.Screen) string {
	rawHash = strings.TrimSpace(rawHash)
	if rawHash == "" || screen == nil {
		return rawHash
	}
	if e.exactDedupMode() {
		return rawHash
	}
	// Structural mode (default): derive a deterministic canonical key from
	// the dedup signature. Two screens with the same signature (i.e. the
	// same layout once unprotected fields are masked and alphanumerics are
	// abstracted) collapse into the same area regardless of echoed values
	// in protected text.
	candidateSig := buildAreaDedupSignature(screen)
	if candidateSig == "" {
		return rawHash
	}
	sigKey := signatureCanonicalKey(candidateSig)
	if known, ok := e.resolveKnownSignatureLocked(sigKey); ok {
		return known
	}
	cw, ch := screenDimensions(screen)
	best := matchDedupArea(candidateSig, areaTitleDedupSignatureFromScreen(screen), cw, ch, len(screen.Fields), e.dedupRefsLocked(), e.dedupThreshold())
	if best != "" {
		e.rememberSignatureAliasLocked(sigKey, best)
		return best
	}
	return sigKey
}
