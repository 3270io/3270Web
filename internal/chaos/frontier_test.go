// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"reflect"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

func TestCandidateKeysNormalizesWeights(t *testing.T) {
	got := CandidateKeys(map[string]int{"PF3": 5, "enter": 70, "PF(8)": 5})
	want := []string{"Enter", "PF(3)", "PF(8)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CandidateKeys = %v, want %v", got, want)
	}
}

func TestCandidateKeysFallsBackWhenEmpty(t *testing.T) {
	got := CandidateKeys(nil)
	if len(got) != len(fallbackChaosKeyCandidates) {
		t.Fatalf("CandidateKeys(nil) = %v, want the %d fallback keys", got, len(fallbackChaosKeyCandidates))
	}
}

func TestUntriedCandidateKeys(t *testing.T) {
	area := &MindMapArea{
		KeyPresses: map[string]*MindMapKeyPress{
			"Enter": {Presses: 3},
			"PF(2)": {Presses: 0}, // recorded but never actually pressed
		},
		AutoBlockedKeys: []string{"PF(4)"},
	}
	candidates := []string{"Enter", "PF(1)", "PF(2)", "PF(4)"}
	got := UntriedCandidateKeys(area, candidates)
	want := []string{"PF(1)", "PF(2)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UntriedCandidateKeys = %v, want %v", got, want)
	}
}

func TestUntriedCandidateKeysNilArea(t *testing.T) {
	candidates := []string{"Enter", "PF(1)"}
	if got := UntriedCandidateKeys(nil, candidates); !reflect.DeepEqual(got, candidates) {
		t.Fatalf("UntriedCandidateKeys(nil) = %v, want all candidates", got)
	}
}

func newFrontierTestEngine(t *testing.T) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Seed = 42
	return newFrontierTestEngineWithConfig(t, cfg)
}

func newFrontierTestEngineWithConfig(t *testing.T, cfg Config) *Engine {
	t.Helper()
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	mockHost.Connected = true
	return New(mockHost, cfg)
}

func TestNoveltyKeyBoostsSkipsBrandNewArea(t *testing.T) {
	e := newFrontierTestEngine(t)
	e.mindMap = newMindMap()
	e.mindMap.ensureArea("fresh")

	e.mu.Lock()
	got := e.noveltyKeyBoostsLocked("fresh")
	e.mu.Unlock()
	if got != nil {
		t.Fatalf("noveltyKeyBoostsLocked on a press-free area = %v, want nil", got)
	}
}

func TestNoveltyKeyBoostsBoostUntriedKeys(t *testing.T) {
	e := newFrontierTestEngine(t)
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("visited")
	area.KeyPresses["Enter"] = &MindMapKeyPress{Presses: 5, Progressions: 1}

	e.mu.Lock()
	got := e.noveltyKeyBoostsLocked("visited")
	e.mu.Unlock()
	if got == nil {
		t.Fatal("noveltyKeyBoostsLocked = nil, want boosts for untried keys")
	}
	if _, ok := got["Enter"]; ok {
		t.Errorf("Enter has been pressed and must not get a novelty boost: %v", got)
	}
	for _, key := range []string{"PF(1)", "PF(2)", "PF(4)", "PF(12)"} {
		if got[key] != noveltyKeyBoost {
			t.Errorf("boost[%s] = %d, want %d", key, got[key], noveltyKeyBoost)
		}
	}
}

// TestFrontierKeyBoostsSteerTowardUntriedAreas builds the graph
// A --Enter--> B --PF(2)--> C where A and B are fully tried and C still has
// untried keys, and verifies that from A the key leading toward C (Enter,
// distance 2 via B) is boosted more than the key looping back to A (PF(1),
// distance 3).
func TestFrontierKeyBoostsSteerTowardUntriedAreas(t *testing.T) {
	e := newFrontierTestEngine(t)
	e.mindMap = newMindMap()
	pressedAll := func(area *MindMapArea, dests map[string]map[string]int) {
		for _, k := range e.candidateKeys {
			kp := &MindMapKeyPress{Presses: 1}
			if d, ok := dests[k]; ok {
				kp.Destinations = d
				kp.Progressions = 1
			}
			area.KeyPresses[k] = kp
		}
	}
	a := e.mindMap.ensureArea("areaA")
	pressedAll(a, map[string]map[string]int{
		"Enter": {"areaB": 2},
		"PF(1)": {"areaA": 1},
	})
	b := e.mindMap.ensureArea("areaB")
	pressedAll(b, map[string]map[string]int{
		"PF(2)": {"areaC": 1},
	})
	c := e.mindMap.ensureArea("areaC")
	c.KeyPresses["Enter"] = &MindMapKeyPress{Presses: 1}

	e.mu.Lock()
	got := e.frontierKeyBoostsLocked("areaA")
	e.mu.Unlock()
	if got == nil {
		t.Fatal("frontierKeyBoostsLocked = nil, want boosts toward the frontier")
	}
	// dist(C)=0, dist(B)=1, dist(A)=2: Enter -> B scores 60/2, PF(1) -> A scores 60/3.
	if got["Enter"] != frontierProximityBoost/2 {
		t.Errorf("boost[Enter] = %d, want %d", got["Enter"], frontierProximityBoost/2)
	}
	if got["PF(1)"] != frontierProximityBoost/3 {
		t.Errorf("boost[PF(1)] = %d, want %d", got["PF(1)"], frontierProximityBoost/3)
	}
	if got["Enter"] <= got["PF(1)"] {
		t.Errorf("key toward frontier should outrank the loop key: %v", got)
	}
}

func TestFrontierKeyBoostsNilWhileLocalKeysUntried(t *testing.T) {
	e := newFrontierTestEngine(t)
	e.mindMap = newMindMap()
	area := e.mindMap.ensureArea("current")
	area.KeyPresses["Enter"] = &MindMapKeyPress{Presses: 1}
	other := e.mindMap.ensureArea("other")
	other.KeyPresses["Enter"] = &MindMapKeyPress{Presses: 1}

	e.mu.Lock()
	got := e.frontierKeyBoostsLocked("current")
	e.mu.Unlock()
	if got != nil {
		t.Fatalf("frontier boost with local untried keys = %v, want nil (novelty handles it)", got)
	}
}

func TestFrontierStatsCountUntriedKeys(t *testing.T) {
	e := newFrontierTestEngine(t)
	e.mindMap = newMindMap()
	full := e.mindMap.ensureArea("full")
	for _, k := range e.candidateKeys {
		full.KeyPresses[k] = &MindMapKeyPress{Presses: 1}
	}
	partial := e.mindMap.ensureArea("partial")
	partial.KeyPresses["Enter"] = &MindMapKeyPress{Presses: 1}

	e.mu.Lock()
	areas, untried := e.frontierStatsLocked()
	e.mu.Unlock()
	if areas != 1 {
		t.Errorf("frontierAreas = %d, want 1", areas)
	}
	wantUntried := len(e.candidateKeys) - 1
	if untried != wantUntried {
		t.Errorf("untriedKeys = %d, want %d", untried, wantUntried)
	}
}

func TestNegativeKeyBoostsOnly(t *testing.T) {
	in := map[string]int{"PF(8)": 50, "PF(3)": -40, "Enter": 70}
	got := negativeKeyBoostsOnly(in)
	if !reflect.DeepEqual(got, map[string]int{"PF(3)": -40}) {
		t.Fatalf("negativeKeyBoostsOnly = %v, want only the PF(3) penalty", got)
	}
	if negativeKeyBoostsOnly(map[string]int{"Enter": 10}) != nil {
		t.Error("all-positive input should collapse to nil")
	}
	if negativeKeyBoostsOnly(nil) != nil {
		t.Error("nil input should stay nil")
	}
}

// TestExplorationBoostsBypassLearnedKeyReuseBias verifies the split between
// the two boost maps: with LearnedKeyReuseBias 0, learned boosts are muted
// (scaled to 0) while exploration boosts still steer selection.
func TestExplorationBoostsBypassLearnedKeyReuseBias(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Seed = 7
	cfg.LearnedKeyReuseBias = 0
	cfg.AIDKeyWeights = map[string]int{"Enter": 1, "PF(8)": 1}
	e := newFrontierTestEngineWithConfig(t, cfg)

	countPicks := func(scaled, exploration map[string]int) map[string]int {
		out := make(map[string]int)
		for i := 0; i < 200; i++ {
			out[e.chooseAIDKeyWithExploration(scaled, exploration)]++
		}
		return out
	}

	// A huge learned boost is fully muted by bias 0: picks stay ~50/50.
	learnedOnly := countPicks(map[string]int{"PF(8)": 10_000}, nil)
	if learnedOnly["PF(8)"] > 150 {
		t.Errorf("learned boost should be muted at bias 0, picks = %v", learnedOnly)
	}
	// The same boost as an exploration boost dominates selection.
	explorationOnly := countPicks(nil, map[string]int{"PF(8)": 10_000})
	if explorationOnly["PF(8)"] < 190 {
		t.Errorf("exploration boost should dominate at bias 0, picks = %v", explorationOnly)
	}
}
