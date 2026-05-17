package chaos

import (
	"encoding/json"
	"testing"
)

func makeArea(hash, sig, label string, fields map[string]MindMapFieldMetadata, keyPresses map[string]*MindMapKeyPress) *MindMapArea {
	return &MindMapArea{
		Hash:           hash,
		Label:          label,
		DedupSignature: sig,
		FieldMetadata:  fields,
		KeyPresses:     keyPresses,
	}
}

func TestCompareMindMaps_IdenticalMapsHaveNoDeltas(t *testing.T) {
	a := makeArea("h1", "sig-login", "LOGON", map[string]MindMapFieldMetadata{
		"R1C1L8": {Row: 1, Column: 1, Length: 8},
	}, map[string]*MindMapKeyPress{
		"Enter": {Presses: 3, Progressions: 1, Destinations: map[string]int{"h2": 1}},
	})
	b := makeArea("h2", "sig-menu", "MENU", nil, nil)
	mm := &MindMap{Areas: map[string]*MindMapArea{"h1": a, "h2": b}}

	diff := CompareMindMaps(mm, mm)
	if diff.Summary.AreasOnlyInBaseline != 0 || diff.Summary.AreasOnlyInCand != 0 {
		t.Errorf("expected zero set differences, got summary %+v", diff.Summary)
	}
	if diff.Summary.FieldChanges != 0 || diff.Summary.TransitionChanges != 0 {
		t.Errorf("expected zero field/transition deltas, got summary %+v", diff.Summary)
	}
	if len(diff.Common) != 0 {
		t.Errorf("expected no Common entries (areas were equal), got %d", len(diff.Common))
	}
}

func TestCompareMindMaps_AreaOnlyInBaseline(t *testing.T) {
	a := makeArea("h1", "sig-login", "LOGON", nil, nil)
	baseline := &MindMap{Areas: map[string]*MindMapArea{"h1": a}}
	candidate := &MindMap{Areas: map[string]*MindMapArea{}}

	diff := CompareMindMaps(baseline, candidate)
	if len(diff.OnlyInBaseline) != 1 {
		t.Fatalf("expected 1 area only in baseline, got %d", len(diff.OnlyInBaseline))
	}
	if diff.OnlyInBaseline[0].Signature != "sig-login" {
		t.Errorf("OnlyInBaseline signature: %q", diff.OnlyInBaseline[0].Signature)
	}
	if diff.OnlyInBaseline[0].Hash != "h1" {
		t.Errorf("OnlyInBaseline hash: %q", diff.OnlyInBaseline[0].Hash)
	}
	if diff.Summary.AreasOnlyInBaseline != 1 || diff.Summary.AreasOnlyInCand != 0 {
		t.Errorf("summary: %+v", diff.Summary)
	}
}

func TestCompareMindMaps_FieldDeltas(t *testing.T) {
	baseFields := map[string]MindMapFieldMetadata{
		"R1C1L8":  {Row: 1, Column: 1, Length: 8},  // unchanged
		"R2C1L20": {Row: 2, Column: 1, Length: 20}, // will be removed in candidate
		"R3C1L10": {Row: 3, Column: 1, Length: 10, Hidden: false}, // will be modified
	}
	candFields := map[string]MindMapFieldMetadata{
		"R1C1L8":  {Row: 1, Column: 1, Length: 8},
		"R3C1L10": {Row: 3, Column: 1, Length: 10, Hidden: true}, // changed
		"R5C1L40": {Row: 5, Column: 1, Length: 40},               // added
	}
	a := makeArea("h1", "sig-form", "FORM", baseFields, nil)
	c := makeArea("h1", "sig-form", "FORM", candFields, nil)

	baseline := &MindMap{Areas: map[string]*MindMapArea{"h1": a}}
	candidate := &MindMap{Areas: map[string]*MindMapArea{"h1": c}}

	diff := CompareMindMaps(baseline, candidate)
	if len(diff.Common) != 1 {
		t.Fatalf("expected 1 common area with deltas, got %d", len(diff.Common))
	}
	got := diff.Common[0].FieldDeltas
	if len(got) != 3 {
		t.Fatalf("expected 3 field deltas (1 added, 1 removed, 1 modified), got %d: %+v", len(got), got)
	}
	// Sorted by (Row, Col): R2 removed, R3 modified, R5 added.
	if got[0].Change != "removed" || got[0].Row != 2 {
		t.Errorf("delta[0]: %+v", got[0])
	}
	if got[1].Change != "modified" || got[1].Row != 3 {
		t.Errorf("delta[1]: %+v", got[1])
	}
	if got[2].Change != "added" || got[2].Row != 5 {
		t.Errorf("delta[2]: %+v", got[2])
	}
	if diff.Summary.FieldChanges != 3 {
		t.Errorf("summary.FieldChanges: %d", diff.Summary.FieldChanges)
	}
}

func TestCompareMindMaps_TransitionRedirect(t *testing.T) {
	// Two areas (login, menu) shared by sig. In baseline, Enter on login
	// goes to menu. In candidate, Enter on login goes to a different
	// area (admin).
	loginBase := makeArea("h-login", "sig-login", "LOGON", nil, map[string]*MindMapKeyPress{
		"Enter": {Presses: 2, Destinations: map[string]int{"h-menu": 2}},
	})
	menuBase := makeArea("h-menu", "sig-menu", "MENU", nil, nil)

	loginCand := makeArea("h-login", "sig-login", "LOGON", nil, map[string]*MindMapKeyPress{
		"Enter": {Presses: 1, Destinations: map[string]int{"h-admin": 1}},
	})
	adminCand := makeArea("h-admin", "sig-admin", "ADMIN", nil, nil)

	baseline := &MindMap{Areas: map[string]*MindMapArea{
		"h-login": loginBase, "h-menu": menuBase,
	}}
	candidate := &MindMap{Areas: map[string]*MindMapArea{
		"h-login": loginCand, "h-admin": adminCand,
	}}

	diff := CompareMindMaps(baseline, candidate)
	// Areas: login is common; menu only in baseline; admin only in candidate.
	if diff.Summary.AreasOnlyInBaseline != 1 || diff.Summary.AreasOnlyInCand != 1 {
		t.Errorf("summary set diffs: %+v", diff.Summary)
	}
	if len(diff.Common) != 1 || diff.Common[0].Signature != "sig-login" {
		t.Fatalf("expected one common area sig-login, got %+v", diff.Common)
	}
	tds := diff.Common[0].TransitionDeltas
	if len(tds) != 1 {
		t.Fatalf("expected 1 transition delta, got %d: %+v", len(tds), tds)
	}
	if tds[0].Key != "Enter" || tds[0].Change != "redirected" {
		t.Errorf("transition delta: %+v", tds[0])
	}
	if tds[0].BaselineTarget != "sig-menu" || tds[0].CandidateTarget != "sig-admin" {
		t.Errorf("transition targets: baseline=%q candidate=%q", tds[0].BaselineTarget, tds[0].CandidateTarget)
	}
}

func TestCompareMindMaps_DeterministicJSON(t *testing.T) {
	// Build a small mind map with intentionally non-trivial field/transition
	// counts, then compare twice and assert byte-for-byte JSON equality
	// (modulo GeneratedAt which obviously changes).
	makeMap := func(suffix string) *MindMap {
		return &MindMap{Areas: map[string]*MindMapArea{
			"h-a": makeArea("h-a", "sig-a", "A"+suffix, map[string]MindMapFieldMetadata{
				"R1C1L5": {Row: 1, Column: 1, Length: 5},
				"R2C1L5": {Row: 2, Column: 1, Length: 5},
			}, map[string]*MindMapKeyPress{
				"Enter": {Destinations: map[string]int{"h-b": 1}},
			}),
			"h-b": makeArea("h-b", "sig-b", "B", nil, nil),
		}}
	}
	a := makeMap("")
	b := makeMap("-other") // labels differ, signatures match
	d1 := CompareMindMaps(a, b)
	d2 := CompareMindMaps(a, b)
	d1.GeneratedAt = d2.GeneratedAt // null out the only non-deterministic field
	j1, _ := json.Marshal(d1)
	j2, _ := json.Marshal(d2)
	if string(j1) != string(j2) {
		t.Errorf("two compare runs produced different JSON:\nA: %s\nB: %s", j1, j2)
	}
}

func TestCompareMindMaps_NilInputsAreEmpty(t *testing.T) {
	diff := CompareMindMaps(nil, nil)
	if diff == nil {
		t.Fatal("expected non-nil diff for nil inputs")
	}
	if diff.Summary.BaselineAreas != 0 || diff.Summary.CandidateAreas != 0 {
		t.Errorf("expected empty summary, got %+v", diff.Summary)
	}
}
