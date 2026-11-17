// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/session"
)

// deadKeyMinPresses is how many times an AID key must have been pressed from a
// screen without ever advancing before chaos_insights flags it as "dead".
const deadKeyMinPresses = 3

// insightsMaxScreens / insightsMaxExperiments bound the payload so a large
// discovered application cannot overflow the model context window.
const (
	insightsMaxScreens     = 50
	insightsMaxExperiments = 25
)

// ChaosInsightsHandler handles GET /chaos/insights – turns the raw chaos
// discovery data into actionable guidance: per-screen productive vs dead keys,
// unproductive input fields, conditional transitions, and a ranked list of
// suggested next experiments, plus the run's saturation/termination
// diagnostics. Works from the live engine, a loaded run, or the auto-saved
// run — including the diagnostics, which outlive the engine.
func (app *App) ChaosInsightsHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if app.chaosEngines.isRemoved(s.ID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos run data for this session"})
		return
	}
	mm := app.sessionChaosMindMap(s)
	if mm == nil || len(mm.Areas) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no screens discovered yet; run chaos exploration first"})
		return
	}

	// Measure "untried" against the keys this run could actually press, not
	// against the built-in defaults: a run started with its own key set, or
	// with keys blocked, has a different frontier from the default one.
	candidates := app.sessionChaosRunKeys(s, mm)
	resp := buildChaosInsightsWith(mm, candidates, app.chaosEngines.getScreenHints(s.ID))

	// Run diagnostics, in every state. The engine answers while it exists;
	// afterwards the session's kept snapshot and the finished/loaded run do.
	// This used to be engine-only, so the reason a run ended was reported
	// for the half second between the run ending and the engine being
	// discarded and never again.
	if diag := app.sessionChaosRunDiagnostics(s); diag != nil {
		resp["run"] = diag
	}

	c.JSON(http.StatusOK, resp)
}

// sessionChaosRunKeys returns the AID keys the session's run could press:
// its candidate keys minus the keys it was told never to press.
//
// The live engine knows; after the run, the configuration summary kept on the
// session does. For a run loaded from disk neither exists, and the keys are
// taken to be the ones the run is recorded as having pressed somewhere — a
// run demonstrably could press those, and guessing the built-in defaults
// instead reports keys as "untried" that the run was never allowed to try.
func (app *App) sessionChaosRunKeys(s *session.Session, mm *chaos.MindMap) []string {
	cfg := chaosRunConfigOf(s)
	if eng, ok := app.chaosEngines.get(s.ID); ok && eng != nil {
		keys := eng.CandidateKeys()
		if cfg != nil {
			keys = chaosKeysWithout(keys, cfg.BlockedKeys)
		}
		return keys
	}
	if cfg != nil && len(cfg.CandidateKeys) > 0 {
		return append([]string(nil), cfg.CandidateKeys...)
	}
	return chaosKeysPressedInMindMap(mm)
}

// chaosKeysPressedInMindMap lists every AID key recorded as pressed from any
// screen of the map, in the engine's spelling, sorted.
func chaosKeysPressedInMindMap(mm *chaos.MindMap) []string {
	if mm == nil {
		return nil
	}
	var pressed []string
	for _, area := range mm.Areas {
		if area == nil {
			continue
		}
		for key, kp := range area.KeyPresses {
			if kp != nil && kp.Presses > 0 {
				pressed = append(pressed, key)
			}
		}
	}
	return normalizedChaosKeys(pressed)
}

// chaosUntriedKeys lists the run's keys never pressed from the area, leaving
// out keys blocked there — by the run itself (an exit key it recognised) or
// by a hint for that screen.
func chaosUntriedKeys(area *chaos.MindMapArea, runKeys []string, hint chaos.ScreenHint) []string {
	untried := chaos.UntriedCandidateKeys(area, runKeys)
	if len(untried) == 0 {
		return nil
	}
	return chaosKeysWithout(untried, normalizedChaosKeys(hint.BlockedKeys))
}

// sessionChaosRunDiagnostics describes the session's current or last run:
// whether it is active, how far it got, and why it ended. Nil when the
// session has no run at all.
func (app *App) sessionChaosRunDiagnostics(s *session.Session) gin.H {
	if eng, ok := app.chaosEngines.get(s.ID); ok && eng != nil {
		st := eng.Status()
		diag := gin.H{
			"active":        st.Active,
			"stepsRun":      st.StepsRun,
			"uniqueScreens": st.UniqueScreens,
			"transitions":   st.Transitions,
		}
		if st.TerminationReason != "" {
			diag["terminationReason"] = st.TerminationReason
			diag["terminationSummary"] = chaosTerminationSummary(st.TerminationReason, st.SaturatedNoProgress)
			diag["saturatedNoProgress"] = st.SaturatedNoProgress
		}
		if st.CoverageStats != nil {
			diag["coverage"] = st.CoverageStats
		}
		return diag
	}

	state := chaosStateSnapshot(s)
	diag := gin.H{"active": false}
	known := false
	reason := ""
	saturatedNoProgress := false
	if state != nil {
		known = true
		diag["stepsRun"] = state.StepsRun
		diag["uniqueScreens"] = state.UniqueScreens
		diag["transitions"] = state.Transitions
		reason = state.TerminationReason
		saturatedNoProgress = state.SaturatedNoProgress
		if state.TargetHost != "" {
			diag["targetHost"] = state.TargetHost
		}
	}
	_, _ = app.chaosEngines.withLoadedRun(s.ID, func(run *chaos.SavedRun) error {
		known = true
		raiseStatusCounter(diag, "stepsRun", run.StepsRun)
		raiseStatusCounter(diag, "uniqueScreens", run.UniqueScreens)
		raiseStatusCounter(diag, "transitions", run.Transitions)
		diag["runID"] = run.ID
		if reason == "" {
			reason = run.TerminationReason
		}
		if _, has := diag["targetHost"]; !has && run.TargetHost != "" {
			diag["targetHost"] = run.TargetHost
		}
		if run.Label != "" {
			diag["label"] = run.Label
		}
		return nil
	})
	if !known {
		return nil
	}
	for _, key := range []string{"stepsRun", "uniqueScreens", "transitions"} {
		if _, has := diag[key]; !has {
			diag[key] = 0
		}
	}
	if reason != "" {
		diag["terminationReason"] = reason
		diag["terminationSummary"] = chaosTerminationSummary(reason, saturatedNoProgress)
		diag["saturatedNoProgress"] = saturatedNoProgress
	}
	return diag
}

// buildChaosInsights is the pure analysis over a mind map. Separated from the
// handler so it is straightforward to unit test. candidates is the AID-key
// set to measure per-screen "untried keys" against (the exploration frontier).
func buildChaosInsights(mm *chaos.MindMap, candidates []string) gin.H {
	return buildChaosInsightsWith(mm, candidates, nil)
}

// buildChaosInsightsWith is buildChaosInsights with the per-screen hints, so
// that a key blocked on one screen is not reported as untried there.
func buildChaosInsightsWith(mm *chaos.MindMap, candidates []string, screenHints map[string]chaos.ScreenHint) gin.H {
	type areaEntry struct {
		hash string
		area *chaos.MindMapArea
	}
	areas := make([]areaEntry, 0, len(mm.Areas))
	for hash, area := range mm.Areas {
		if area == nil {
			continue
		}
		areas = append(areas, areaEntry{hash: hash, area: area})
	}
	// Most-visited first so the busiest screens lead both lists.
	sort.Slice(areas, func(i, j int) bool {
		if areas[i].area.Visits != areas[j].area.Visits {
			return areas[i].area.Visits > areas[j].area.Visits
		}
		return areas[i].hash < areas[j].hash
	})

	type experiment struct {
		text     string
		priority int // higher = more important
	}
	var experiments []experiment
	frontierScreens := 0

	perScreen := make([]gin.H, 0, len(areas))
	for _, ae := range areas {
		area := ae.area
		label := area.Label
		if label == "" {
			label = labelForHash(mm, ae.hash)
		}

		var productiveKeys, deadKeys []gin.H
		var conditional []gin.H
		for key, kp := range area.KeyPresses {
			if kp == nil {
				continue
			}
			if kp.Progressions > 0 {
				productiveKeys = append(productiveKeys, gin.H{
					"key": key, "presses": kp.Presses, "progressions": kp.Progressions,
				})
				for toHash, count := range kp.Destinations {
					conditional = append(conditional, gin.H{
						"key": key, "toLabel": labelForHash(mm, toHash), "toHash": toHash, "count": count,
					})
				}
			} else if kp.Presses >= deadKeyMinPresses {
				deadKeys = append(deadKeys, gin.H{"key": key, "presses": kp.Presses})
			}
		}
		sortKeyStat(productiveKeys, "progressions")
		sortKeyStat(deadKeys, "presses")

		var unproductiveFields []gin.H
		for fkey, fd := range area.FieldDiscovery {
			if fd.Writes > 0 && fd.Progressions == 0 {
				unproductiveFields = append(unproductiveFields, gin.H{
					"key": fkey, "writes": fd.Writes, "writeSuccesses": fd.WriteSuccesses,
				})
			}
		}
		sortKeyStat(unproductiveFields, "writes")

		untriedKeys := chaosUntriedKeys(area, candidates, screenHints[ae.hash])

		entry := gin.H{
			"hash":   ae.hash,
			"label":  label,
			"visits": area.Visits,
		}
		if len(untriedKeys) > 0 {
			entry["untriedKeys"] = untriedKeys
		}
		if len(productiveKeys) > 0 {
			entry["productiveKeys"] = productiveKeys
		}
		if len(deadKeys) > 0 {
			entry["deadKeys"] = deadKeys
		}
		if len(unproductiveFields) > 0 {
			entry["unproductiveFields"] = unproductiveFields
		}
		if len(conditional) > 0 {
			entry["conditionalTransitions"] = conditional
		}
		if len(perScreen) < insightsMaxScreens {
			perScreen = append(perScreen, entry)
		}

		// Derive suggested experiments. A screen with input fields that has
		// never advanced is the highest-value place to intervene.
		if area.InputFieldCount > 0 && len(productiveKeys) == 0 {
			experiments = append(experiments, experiment{
				priority: 100 + area.Visits,
				text: fmt.Sprintf("Screen %q (%s): %d input field(s) but nothing has advanced it. Seed a transaction code / known value via chaos hints, or drive it manually to learn a working value.",
					label, shortHash(ae.hash), area.InputFieldCount),
			})
		}
		if len(deadKeys) > 0 && len(productiveKeys) > 0 {
			keys := make([]string, 0, len(deadKeys))
			for _, dk := range deadKeys {
				keys = append(keys, fmt.Sprintf("%v", dk["key"]))
			}
			experiments = append(experiments, experiment{
				priority: 40 + area.Visits,
				text: fmt.Sprintf("Screen %q (%s): key(s) %v were pressed repeatedly without advancing. Consider blocking them (chaos_save_screen_hint blocked_keys) to focus the run.",
					label, shortHash(ae.hash), keys),
			})
		}
		if len(unproductiveFields) > 0 && len(productiveKeys) > 0 {
			experiments = append(experiments, experiment{
				priority: 30 + area.Visits,
				text: fmt.Sprintf("Screen %q (%s): %d field(s) accept writes but never advance the screen — they may need a specific format or a companion field. Try known values from a recording.",
					label, shortHash(ae.hash), len(unproductiveFields)),
			})
		}
		if len(untriedKeys) > 0 {
			frontierScreens++
			// Only worth surfacing as an experiment once the screen has some
			// history; a brand-new area's untried keys are just "not yet".
			if len(area.KeyPresses) > 0 {
				experiments = append(experiments, experiment{
					priority: 20 + area.Visits,
					text: fmt.Sprintf("Screen %q (%s): key(s) %v have never been pressed here — untried frontier. Continue exploring (the run steers toward untried keys) or press them manually to extend the map.",
						label, shortHash(ae.hash), untriedKeys),
				})
			}
		}
	}

	sort.SliceStable(experiments, func(i, j int) bool {
		return experiments[i].priority > experiments[j].priority
	})
	suggested := make([]string, 0, len(experiments))
	for _, e := range experiments {
		if len(suggested) >= insightsMaxExperiments {
			break
		}
		suggested = append(suggested, e.text)
	}
	if len(suggested) == 0 {
		suggested = append(suggested, "No obvious dead ends. Coverage looks healthy; consider exporting a workflow or cataloging business functions.")
	}

	return gin.H{
		"screenCount":          len(areas),
		"frontierScreens":      frontierScreens,
		"perScreen":            perScreen,
		"perScreenTruncated":   len(areas) > len(perScreen),
		"suggestedExperiments": suggested,
	}
}

// sortKeyStat sorts a slice of stat maps by the given integer field, descending.
func sortKeyStat(items []gin.H, field string) {
	sort.SliceStable(items, func(i, j int) bool {
		vi, _ := items[i][field].(int)
		vj, _ := items[j][field].(int)
		return vi > vj
	})
}

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}
