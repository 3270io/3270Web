package main

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
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
// suggested next experiments, plus the live saturation/termination
// diagnostics. Works from the live engine, a loaded run, or the auto-saved run.
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

	resp := buildChaosInsights(mm)

	// Layer in live run diagnostics when an engine exists for this session.
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
			diag["saturatedNoProgress"] = st.SaturatedNoProgress
		}
		if st.CoverageStats != nil {
			diag["coverage"] = st.CoverageStats
		}
		resp["run"] = diag
	}

	c.JSON(http.StatusOK, resp)
}

// buildChaosInsights is the pure analysis over a mind map. Separated from the
// handler so it is straightforward to unit test.
func buildChaosInsights(mm *chaos.MindMap) gin.H {
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

		entry := gin.H{
			"hash":   ae.hash,
			"label":  label,
			"visits": area.Visits,
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
