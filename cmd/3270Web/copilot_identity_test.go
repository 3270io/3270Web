// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/chaos"
)

// The engine may file a screen under its raw hash (exact mode), under its
// structural key (the default), or fold it into a near-identical area. A live
// screen has to be matched whichever happened.
func TestAreaForScreenMatchesHoweverTheEngineFiledIt(t *testing.T) {
	screen := formScreen()

	if _, _, ok := chaos.AreaForScreen(nil, screen); ok {
		t.Error("nothing discovered must match nothing")
	}
	if _, _, ok := chaos.AreaForScreen(&chaos.MindMap{Areas: map[string]*chaos.MindMapArea{}}, nil); ok {
		t.Error("no screen must match nothing")
	}

	raw := chaos.ScreenHash(screen)
	structural := chaos.StructuralScreenHash(screen)
	if raw == "" || structural == "" || raw == structural {
		t.Fatalf("want two distinct non-empty hashes, got raw %q structural %q", raw, structural)
	}

	for name, key := range map[string]string{"raw hash": raw, "structural key": structural} {
		mm := &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{
			key:     {Hash: key, Label: "CUSTOMER MAINTENANCE"},
			"other": {Hash: "other", Label: "SOMETHING ELSE"},
		}}
		got, area, ok := chaos.AreaForScreen(mm, screen)
		if !ok || got != key || area.Label != "CUSTOMER MAINTENANCE" {
			t.Errorf("filed under its %s: got %q ok=%v", name, got, ok)
		}
		if hash := chaos.CurrentScreenHash(mm, screen); hash != key {
			t.Errorf("filed under its %s: CurrentScreenHash = %q, want %q", name, hash, key)
		}
	}

	// What is typed into input fields does not change the screen's identity.
	typed := formScreen()
	copy(typed.Buffer[2][13:], []rune("SOMEONE ELSE"))
	if chaos.ScreenHash(typed) != raw || chaos.StructuralScreenHash(typed) != structural {
		t.Error("typing into an input field changed the screen's hash")
	}

	// A screen nobody has seen reports the key a default run would give it.
	if hash := chaos.CurrentScreenHash(nil, screen); hash != structural {
		t.Errorf("CurrentScreenHash for an undiscovered screen = %q, want the structural key %q", hash, structural)
	}
}

// A near-identical screen — one protected character different — belongs to the
// area the engine would have folded it into, and a different screen does not.
func TestAreaForScreenFallsBackToSimilarity(t *testing.T) {
	discovered := formScreen()
	mm := &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{
		"foldedkey": {
			Hash:           "foldedkey",
			Label:          "CUSTOMER MAINTENANCE",
			ScreenWidth:    80,
			ScreenHeight:   24,
			FieldCount:     len(discovered.Fields),
			DedupSignature: chaos.DedupSignature(discovered),
		},
	}}

	live := formScreen()
	// A counter in protected text ticks over: same screen, different bytes.
	copy(live.Buffer[0][40:], []rune("PAGE 2"))
	if chaos.StructuralScreenHash(live) == chaos.StructuralScreenHash(discovered) {
		t.Fatal("the variation did not change the structural key, so this test is not exercising the fallback")
	}
	key, _, ok := chaos.AreaForScreen(mm, live)
	if !ok || key != "foldedkey" {
		t.Errorf("a near-identical screen should match the area it was folded into, got %q ok=%v", key, ok)
	}

	different := formScreen()
	for y := 8; y < 20; y++ {
		copy(different.Buffer[y][2:], []rune("A COMPLETELY DIFFERENT BODY OF PROTECTED TEXT ON THIS ROW"))
	}
	if key, _, ok := chaos.AreaForScreen(mm, different); ok {
		t.Errorf("a different screen matched %q", key)
	}
}
