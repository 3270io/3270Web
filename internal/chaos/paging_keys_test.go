// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

// The default key set has no paging keys, so a list was only ever seen one
// page deep. A key the screen's own legend labels as paging is admitted on
// that screen; one that leaves the screen, or one the legend does not
// mention, is not.
func TestLegendPagingKeysAreAdmittedOnTheScreenThatShowsThem(t *testing.T) {
	s := buildScriptedChaosScreen("LIST", false)
	text := "PF3 Back    PF7 Prev    PF8 Next    PF12 Menu"
	for i, r := range []rune(text) {
		s.Buffer[s.Height-2][i] = r
	}
	s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, s.Height-2, s.Width-1, s.Height-2, host.AttrColDefault, host.AttrEhDefault))

	paging := inferPagingKeys(s)
	if !containsString(paging, "PF(7)") || !containsString(paging, "PF(8)") {
		t.Fatalf("inferPagingKeys = %v, want PF(7) and PF(8)", paging)
	}
	for _, k := range []string{"PF(3)", "PF(12)"} {
		if containsString(paging, k) {
			t.Errorf("%s leaves the screen and must not be admitted as paging: %v", k, paging)
		}
	}

	configured := CandidateKeys(DefaultConfig().AIDKeyWeights)
	if containsString(configured, "PF(8)") {
		t.Fatal("the default key set now has PF(8); this test no longer shows anything")
	}
	boosts := inferScreenHelpKeyBoosts(s)
	if got := restrictBoostsToKeys(boosts, configured); got["PF(8)"] != 0 {
		t.Errorf("without admission PF(8) should be dropped, got %v", got)
	}
	allowed := mergeSortedUniqueStrings(append([]string(nil), configured...), paging)
	got := restrictBoostsToKeys(boosts, allowed)
	if got["PF(8)"] <= 0 || got["PF(7)"] <= 0 {
		t.Errorf("admitted paging keys carry no weight: %v", got)
	}
	if got["PF(3)"] > 0 {
		t.Errorf("PF(3) gained weight from the legend: %v", got)
	}
}
