package chaos

import (
	"sort"
	"strings"
	"time"
)

// CompareSchemaVersion is the semver of the MindMapDiff JSON shape. Bump the
// minor when adding fields; bump the major for breaking changes.
const CompareSchemaVersion = "1.0.0"

// MindMapDiff is the result of comparing two chaos mind maps captured against
// different hosts (typically an IBM z/OS mainframe and a Rocket Enterprise
// Server, or two versions of the same host).
//
// Areas are keyed by Hash (the engine's screen hash) and matched on the
// stable DedupSignature so cosmetic differences between hosts do not cause
// false-positive divergence.
type MindMapDiff struct {
	SchemaVersion   string      `json:"schema_version"`
	GeneratedAt     time.Time   `json:"generated_at"`
	OnlyInBaseline  []AreaRef   `json:"only_in_baseline"`
	OnlyInCandidate []AreaRef   `json:"only_in_candidate"`
	Common          []AreaDiff  `json:"common"`
	Summary         DiffSummary `json:"summary"`
}

// DiffSummary contains rolled-up counters useful for at-a-glance comparison.
type DiffSummary struct {
	BaselineAreas       int `json:"baseline_areas"`
	CandidateAreas      int `json:"candidate_areas"`
	CommonAreas         int `json:"common_areas"`
	AreasOnlyInBaseline int `json:"areas_only_in_baseline"`
	AreasOnlyInCand     int `json:"areas_only_in_candidate"`
	FieldChanges        int `json:"field_changes"`
	TransitionChanges   int `json:"transition_changes"`
}

// AreaRef is a lightweight pointer to an area in the source mind map.
type AreaRef struct {
	Hash      string `json:"hash"`
	Signature string `json:"signature,omitempty"`
	Label     string `json:"label,omitempty"`
	Preview   string `json:"preview,omitempty"`
}

// AreaDiff captures the per-area deltas between baseline and candidate.
type AreaDiff struct {
	BaselineHash     string            `json:"baseline_hash"`
	CandidateHash    string            `json:"candidate_hash"`
	Signature        string            `json:"signature"`
	Label            string            `json:"label,omitempty"`
	FieldDeltas      []FieldDelta      `json:"field_deltas,omitempty"`
	TransitionDeltas []TransitionDelta `json:"transition_deltas,omitempty"`
}

// FieldDelta describes a single field that differs between baseline and
// candidate within an area.
type FieldDelta struct {
	Row    int                   `json:"row"`
	Col    int                   `json:"col"`
	Length int                   `json:"length"`
	Change string                `json:"change"` // "added" | "removed" | "modified"
	Before *MindMapFieldMetadata `json:"before,omitempty"`
	After  *MindMapFieldMetadata `json:"after,omitempty"`
}

// TransitionDelta describes a key-press destination change between baseline
// and candidate.
type TransitionDelta struct {
	Key             string `json:"key"`
	BaselineTarget  string `json:"baseline_target,omitempty"`  // signature of dest, or hash if not in common map
	CandidateTarget string `json:"candidate_target,omitempty"`
	Change          string `json:"change"` // "added" | "removed" | "redirected"
}

// CompareMindMaps returns the diff between baseline and candidate. A nil
// argument is treated as an empty mind map. The result is deterministic: all
// slice fields are sorted lexicographically by their natural keys, so two
// callers that pass equal inputs always get byte-equal JSON.
func CompareMindMaps(baseline, candidate *MindMap) *MindMapDiff {
	bAreas := areaMapBySignature(baseline)
	cAreas := areaMapBySignature(candidate)

	diff := &MindMapDiff{
		SchemaVersion: CompareSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
	}

	// Set difference: signatures only in baseline.
	for sig, ba := range bAreas {
		if _, ok := cAreas[sig]; ok {
			continue
		}
		diff.OnlyInBaseline = append(diff.OnlyInBaseline, areaRef(ba, sig))
	}
	// Set difference: signatures only in candidate.
	for sig, ca := range cAreas {
		if _, ok := bAreas[sig]; ok {
			continue
		}
		diff.OnlyInCandidate = append(diff.OnlyInCandidate, areaRef(ca, sig))
	}

	// Common signatures: compare field metadata and transition destinations.
	for sig, ba := range bAreas {
		ca, ok := cAreas[sig]
		if !ok {
			continue
		}
		fd := diffFields(ba.FieldMetadata, ca.FieldMetadata)
		td := diffTransitions(ba.KeyPresses, ca.KeyPresses, bAreas, cAreas)
		if len(fd) == 0 && len(td) == 0 {
			continue
		}
		ad := AreaDiff{
			BaselineHash:     ba.Hash,
			CandidateHash:    ca.Hash,
			Signature:        sig,
			Label:            firstNonEmpty(ba.Label, ca.Label),
			FieldDeltas:      fd,
			TransitionDeltas: td,
		}
		diff.Common = append(diff.Common, ad)
	}

	sortDiff(diff)

	diff.Summary = DiffSummary{
		BaselineAreas:       len(bAreas),
		CandidateAreas:      len(cAreas),
		CommonAreas:         len(bAreas) - len(diff.OnlyInBaseline),
		AreasOnlyInBaseline: len(diff.OnlyInBaseline),
		AreasOnlyInCand:     len(diff.OnlyInCandidate),
	}
	for _, ad := range diff.Common {
		diff.Summary.FieldChanges += len(ad.FieldDeltas)
		diff.Summary.TransitionChanges += len(ad.TransitionDeltas)
	}
	return diff
}

// areaMapBySignature builds a signature→area index for the given mind map.
// If two areas share a signature (e.g. multiple recordings of the same
// screen), the lexicographically smallest hash wins so the result is stable.
func areaMapBySignature(m *MindMap) map[string]*MindMapArea {
	out := make(map[string]*MindMapArea)
	if m == nil {
		return out
	}
	for _, area := range m.Areas {
		if area == nil {
			continue
		}
		sig := strings.TrimSpace(area.DedupSignature)
		if sig == "" {
			// Fall back to the hash itself so areas without a signature
			// still participate in comparison (and only collide with
			// themselves across maps).
			sig = "hash:" + area.Hash
		}
		if existing, ok := out[sig]; !ok || area.Hash < existing.Hash {
			out[sig] = area
		}
	}
	return out
}

func areaRef(a *MindMapArea, sig string) AreaRef {
	if a == nil {
		return AreaRef{Signature: sig}
	}
	return AreaRef{
		Hash:      a.Hash,
		Signature: sig,
		Label:     a.Label,
		Preview:   truncatePreview(a.PreviewText, 240),
	}
}

func truncatePreview(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// diffFields returns the set of fields that were added, removed, or modified
// between baseline and candidate, keyed by the engine's MindMapFieldKey shape
// (R{row}C{col}L{length}).
func diffFields(before, after map[string]MindMapFieldMetadata) []FieldDelta {
	if len(before) == 0 && len(after) == 0 {
		return nil
	}
	keys := make(map[string]struct{}, len(before)+len(after))
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	var out []FieldDelta
	for k := range keys {
		b, hasB := before[k]
		a, hasA := after[k]
		switch {
		case hasB && !hasA:
			b := b
			out = append(out, FieldDelta{
				Row:    b.Row,
				Col:    b.Column,
				Length: b.Length,
				Change: "removed",
				Before: &b,
			})
		case !hasB && hasA:
			a := a
			out = append(out, FieldDelta{
				Row:    a.Row,
				Col:    a.Column,
				Length: a.Length,
				Change: "added",
				After:  &a,
			})
		case hasB && hasA && b != a:
			b := b
			a := a
			out = append(out, FieldDelta{
				Row:    a.Row,
				Col:    a.Column,
				Length: a.Length,
				Change: "modified",
				Before: &b,
				After:  &a,
			})
		}
	}
	return out
}

// diffTransitions returns the set of AID-key transitions whose destination
// signature differs between baseline and candidate.
func diffTransitions(
	before, after map[string]*MindMapKeyPress,
	bAreas, cAreas map[string]*MindMapArea,
) []TransitionDelta {
	if len(before) == 0 && len(after) == 0 {
		return nil
	}
	keys := make(map[string]struct{}, len(before)+len(after))
	for k := range before {
		keys[k] = struct{}{}
	}
	for k := range after {
		keys[k] = struct{}{}
	}
	bHashToSig := hashToSignatureIndex(bAreas)
	cHashToSig := hashToSignatureIndex(cAreas)

	var out []TransitionDelta
	for k := range keys {
		bDest := primaryDestination(before[k], bHashToSig)
		cDest := primaryDestination(after[k], cHashToSig)
		if bDest == cDest {
			continue
		}
		change := "redirected"
		switch {
		case bDest == "" && cDest != "":
			change = "added"
		case bDest != "" && cDest == "":
			change = "removed"
		}
		out = append(out, TransitionDelta{
			Key:             k,
			BaselineTarget:  bDest,
			CandidateTarget: cDest,
			Change:          change,
		})
	}
	return out
}

// hashToSignatureIndex maps area hashes back to their dedup signatures so
// transition destinations (which are stored as hashes by the engine) can be
// expressed in the cross-host-stable signature space.
func hashToSignatureIndex(byKey map[string]*MindMapArea) map[string]string {
	out := make(map[string]string, len(byKey))
	for sig, area := range byKey {
		if area == nil {
			continue
		}
		out[area.Hash] = sig
	}
	return out
}

// primaryDestination returns the most-frequent destination signature for a
// key press, or "" if no transition was recorded. When ties occur, the
// lexicographically smallest signature wins so the result is stable.
func primaryDestination(kp *MindMapKeyPress, hashToSig map[string]string) string {
	if kp == nil || len(kp.Destinations) == 0 {
		return ""
	}
	var best string
	bestCount := -1
	for destHash, count := range kp.Destinations {
		sig := hashToSig[destHash]
		if sig == "" {
			sig = "hash:" + destHash
		}
		if count > bestCount || (count == bestCount && sig < best) {
			best = sig
			bestCount = count
		}
	}
	return best
}

// sortDiff places all slice fields in deterministic order.
func sortDiff(d *MindMapDiff) {
	sort.Slice(d.OnlyInBaseline, func(i, j int) bool {
		return d.OnlyInBaseline[i].Signature < d.OnlyInBaseline[j].Signature
	})
	sort.Slice(d.OnlyInCandidate, func(i, j int) bool {
		return d.OnlyInCandidate[i].Signature < d.OnlyInCandidate[j].Signature
	})
	sort.Slice(d.Common, func(i, j int) bool {
		return d.Common[i].Signature < d.Common[j].Signature
	})
	for i := range d.Common {
		sort.Slice(d.Common[i].FieldDeltas, func(a, b int) bool {
			if d.Common[i].FieldDeltas[a].Row != d.Common[i].FieldDeltas[b].Row {
				return d.Common[i].FieldDeltas[a].Row < d.Common[i].FieldDeltas[b].Row
			}
			return d.Common[i].FieldDeltas[a].Col < d.Common[i].FieldDeltas[b].Col
		})
		sort.Slice(d.Common[i].TransitionDeltas, func(a, b int) bool {
			return d.Common[i].TransitionDeltas[a].Key < d.Common[i].TransitionDeltas[b].Key
		})
	}
}
