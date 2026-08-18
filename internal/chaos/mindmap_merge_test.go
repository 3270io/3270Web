// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/session"
)

func TestImportMindMapMergesExistingAreas(t *testing.T) {
	e := newFrontierTestEngine(t)
	e.mindMap = newMindMap()
	local := e.mindMap.ensureArea("shared")
	local.Label = "LOCAL LABEL"
	local.Visits = 2
	local.KeyPresses["Enter"] = &MindMapKeyPress{
		Presses:      3,
		Progressions: 1,
		Destinations: map[string]int{"other": 1},
	}
	local.KnownWorkingValues["R1C1L4"] = []string{"LOCA"}

	imported := &MindMap{Areas: map[string]*MindMapArea{
		"shared": {
			Hash:            "shared",
			Label:           "IMPORTED LABEL",
			Visits:          5,
			BusinessPurpose: "Imported purpose",
			KeyPresses: map[string]*MindMapKeyPress{
				"Enter": {Presses: 4, Progressions: 2, Destinations: map[string]int{"other": 2, "third": 1}},
				"PF(8)": {Presses: 1, Progressions: 1, Destinations: map[string]int{"third": 1}},
			},
			KnownWorkingValues: map[string][]string{"R1C1L4": {"IMPO"}},
		},
		"newarea": {Hash: "newarea", Label: "BRAND NEW", Visits: 1},
	}}

	if ok := e.ImportMindMap(imported); !ok {
		t.Fatal("ImportMindMap returned false on an inactive engine")
	}

	area := e.mindMap.Areas["shared"]
	if area.Label != "LOCAL LABEL" {
		t.Errorf("local label should win: %q", area.Label)
	}
	if area.Visits != 7 {
		t.Errorf("Visits = %d, want 7 (2 local + 5 imported)", area.Visits)
	}
	if area.BusinessPurpose != "Imported purpose" {
		t.Errorf("empty local purpose should be filled from import: %q", area.BusinessPurpose)
	}
	enter := area.KeyPresses["Enter"]
	if enter.Presses != 7 || enter.Progressions != 3 {
		t.Errorf("Enter presses/progressions = %d/%d, want 7/3", enter.Presses, enter.Progressions)
	}
	if enter.Destinations["other"] != 3 || enter.Destinations["third"] != 1 {
		t.Errorf("Enter destinations not merged additively: %v", enter.Destinations)
	}
	if area.KeyPresses["PF(8)"] == nil || area.KeyPresses["PF(8)"].Presses != 1 {
		t.Errorf("imported-only key PF(8) missing: %v", area.KeyPresses["PF(8)"])
	}
	working := area.KnownWorkingValues["R1C1L4"]
	if len(working) != 2 || working[0] != "LOCA" || working[1] != "IMPO" {
		t.Errorf("KnownWorkingValues = %v, want [LOCA IMPO]", working)
	}
	if e.mindMap.Areas["newarea"] == nil || e.mindMap.Areas["newarea"].Label != "BRAND NEW" {
		t.Error("area only present in the import should be added as-is")
	}
}

func TestMergeAreaIntoTimesAndGaps(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dst := &MindMapArea{FirstSeen: late, LastSeen: late}
	src := &MindMapArea{
		FirstSeen:   early,
		LastSeen:    early,
		PreviewText: "IMPORTED PREVIEW",
		FieldDiscovery: map[string]MindMapFieldDiscovery{
			"R1C1L2": {Writes: 2, WriteSuccesses: 1, Progressions: 1, LastWorkedValue: "OK"},
		},
	}
	mergeAreaInto(dst, src)
	if !dst.FirstSeen.Equal(early) {
		t.Errorf("FirstSeen = %v, want the earlier %v", dst.FirstSeen, early)
	}
	if !dst.LastSeen.Equal(late) {
		t.Errorf("LastSeen = %v, want the later %v", dst.LastSeen, late)
	}
	if dst.PreviewText != "IMPORTED PREVIEW" {
		t.Errorf("empty preview should fill from src: %q", dst.PreviewText)
	}
	fd := dst.FieldDiscovery["R1C1L2"]
	if fd.Writes != 2 || fd.Progressions != 1 || fd.LastWorkedValue != "OK" {
		t.Errorf("FieldDiscovery not merged: %+v", fd)
	}
}

// TestBuildUnsuccessfulCheckStepsHonoursBalance covers the export path for a
// run with zero successful transitions: the success balance now bounds how
// many unsuccessful attempts become check steps instead of exporting all of
// them regardless of the requested balance.
func TestBuildUnsuccessfulCheckStepsHonoursBalance(t *testing.T) {
	mm := newMindMap()
	area := mm.ensureArea("stuckhash")
	area.PreviewText = "STUCK SCREEN\n"

	attempts := make([]Attempt, 10)
	for i := range attempts {
		attempts[i] = Attempt{Attempt: i + 1, FromHash: "stuckhash", Transitioned: false}
	}

	countChecks := func(steps []session.WorkflowStep) int {
		n := 0
		for _, s := range steps {
			if s.Type == "CheckValue" {
				n++
			}
			if !strings.EqualFold(s.Type, "CheckValue") && s.Type != "" {
				t.Fatalf("unexpected step type %q", s.Type)
			}
		}
		return n
	}

	if got := countChecks(buildUnsuccessfulCheckSteps(attempts, mm, 0.8)); got != 2 {
		t.Errorf("balance 0.8: %d check steps, want 2 (20%% of 10 attempts)", got)
	}
	if got := countChecks(buildUnsuccessfulCheckSteps(attempts, mm, 0.99)); got != 1 {
		t.Errorf("balance 0.99: %d check steps, want the minimum of 1", got)
	}
	if got := countChecks(buildUnsuccessfulCheckSteps(attempts, mm, 0.5)); got != 5 {
		t.Errorf("balance 0.5: %d check steps, want 5", got)
	}
}
