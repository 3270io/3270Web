// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
)

// sampleMindMap builds a small two-screen application: a menu that advances via
// Enter to a detail screen, plus a dead-end detail screen with an input field
// that never advances. The menu also has a PF3 key pressed repeatedly without
// progress (a dead key).
func sampleMindMap() *chaos.MindMap {
	return &chaos.MindMap{
		Areas: map[string]*chaos.MindMapArea{
			"menuhash0001": {
				Hash:            "menuhash0001",
				Label:           "MAIN MENU",
				Visits:          5,
				InputFieldCount: 1,
				BusinessPurpose: "Main menu — choose an option",
				FieldMetadata: map[string]chaos.MindMapFieldMetadata{
					"R3C20L4": {Row: 3, Column: 20, Length: 4},
				},
				FieldSemantics: map[string]chaos.BusinessFieldSemantic{
					"R3C20L4": {Name: "option", Example: "INQ"},
				},
				KnownWorkingValues: map[string][]string{
					"R3C20L4": {"INQ"},
				},
				KeyPresses: map[string]*chaos.MindMapKeyPress{
					"Enter": {Presses: 4, Progressions: 3, Destinations: map[string]int{"detlhash0002": 3}},
					"PF3":   {Presses: 4, Progressions: 0},
				},
			},
			"detlhash0002": {
				Hash:            "detlhash0002",
				Label:           "ACCOUNT DETAIL",
				Visits:          3,
				InputFieldCount: 1,
				// No business purpose -> unannotated gap.
				FieldMetadata: map[string]chaos.MindMapFieldMetadata{
					"R5C10L8": {Row: 5, Column: 10, Length: 8},
				},
				FieldDiscovery: map[string]chaos.MindMapFieldDiscovery{
					"R5C10L8": {Writes: 6, WriteSuccesses: 6, Progressions: 0},
				},
				// No KnownWorkingValues and no progressing key -> dead end + no-values gap.
			},
		},
		BusinessFunctions: map[string]*chaos.BusinessFunction{
			"account inquiry": {
				Name:        "Account inquiry",
				Description: "Look up an account",
				Parameters: []chaos.BusinessParameter{
					{Name: "account_number", Required: true}, // missing example -> gap
				},
			},
		},
	}
}

// sliceLen returns the length of any slice value, or -1 if it is not a slice.
func sliceLen(v interface{}) int {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return -1
	}
	return rv.Len()
}

func subMap(t *testing.T, h gin.H, key string) gin.H {
	t.Helper()
	m, ok := h[key].(gin.H)
	if !ok {
		t.Fatalf("%s is not a gin.H: %T", key, h[key])
	}
	return m
}

func TestBuildBusinessAppOverview(t *testing.T) {
	out := buildBusinessAppOverview(sampleMindMap())

	cov := subMap(t, out, "coverage")
	if cov["screensDiscovered"] != 2 {
		t.Errorf("screensDiscovered = %v, want 2", cov["screensDiscovered"])
	}
	if cov["screensAnnotated"] != 1 {
		t.Errorf("screensAnnotated = %v, want 1", cov["screensAnnotated"])
	}
	if cov["screensWithTransitions"] != 1 {
		t.Errorf("screensWithTransitions = %v, want 1", cov["screensWithTransitions"])
	}
	if cov["businessFunctions"] != 1 {
		t.Errorf("businessFunctions = %v, want 1", cov["businessFunctions"])
	}

	gaps := subMap(t, out, "gaps")
	if got := sliceLen(gaps["unannotatedScreens"]); got != 1 {
		t.Errorf("unannotatedScreens len = %d, want 1", got)
	}
	if got := sliceLen(gaps["deadEndScreens"]); got != 1 {
		t.Errorf("deadEndScreens len = %d, want 1", got)
	}
	if got := sliceLen(gaps["screensWithoutValues"]); got != 1 {
		t.Errorf("screensWithoutValues len = %d, want 1", got)
	}
	missing, _ := gaps["functionsMissingExamples"].([]string)
	if len(missing) != 1 || missing[0] != "Account inquiry" {
		t.Errorf("functionsMissingExamples = %v, want [Account inquiry]", missing)
	}

	// The most-visited screen (menu) should be first and carry navigation.
	screens, ok := out["screens"].([]gin.H)
	if !ok {
		t.Fatalf("screens is not []gin.H: %T", out["screens"])
	}
	if len(screens) != 2 {
		t.Fatalf("screens len = %d, want 2", len(screens))
	}
	if screens[0]["label"] != "MAIN MENU" {
		t.Errorf("first screen label = %v, want MAIN MENU", screens[0]["label"])
	}
	if got := sliceLen(screens[0]["navigation"]); got != 1 {
		t.Errorf("menu navigation len = %d, want 1", got)
	}
}

func TestBuildChaosInsights(t *testing.T) {
	out := buildChaosInsights(sampleMindMap())

	if out["screenCount"] != 2 {
		t.Errorf("screenCount = %v, want 2", out["screenCount"])
	}

	suggested, ok := out["suggestedExperiments"].([]string)
	if !ok {
		t.Fatalf("suggestedExperiments not []string: %T", out["suggestedExperiments"])
	}
	if len(suggested) == 0 {
		t.Fatal("expected at least one suggested experiment")
	}
	// The dead-end detail screen (input field, no progression) is the
	// highest-priority experiment.
	if !strings.Contains(suggested[0], "ACCOUNT DETAIL") {
		t.Errorf("top experiment = %q, want it to mention ACCOUNT DETAIL", suggested[0])
	}

	perScreen, ok := out["perScreen"].([]gin.H)
	if !ok {
		t.Fatalf("perScreen not []gin.H: %T", out["perScreen"])
	}
	var menu gin.H
	for _, sc := range perScreen {
		if sc["label"] == "MAIN MENU" {
			menu = sc
		}
	}
	if menu == nil {
		t.Fatal("MAIN MENU not present in perScreen")
	}
	if got := sliceLen(menu["deadKeys"]); got != 1 {
		t.Errorf("menu deadKeys len = %d, want 1", got)
	}
	if got := sliceLen(menu["productiveKeys"]); got != 1 {
		t.Errorf("menu productiveKeys len = %d, want 1", got)
	}
}
