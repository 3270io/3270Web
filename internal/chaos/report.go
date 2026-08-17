// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"fmt"
	"sort"
	"strings"
)

// Report renders a Markdown summary of the engine's current discovery state.
// Intended for the chaos_report tool and the in-browser report modal. The
// report is a snapshot — calling it during an active run is fine, the result
// reflects the moment the snapshot was taken.
func (e *Engine) Report() string {
	st := e.Status()
	var b strings.Builder
	b.WriteString("# Chaos Exploration Report\n\n")
	writeReportSummary(&b, &st)
	writeReportScreenGraph(&b, st.MindMap)
	writeReportPerScreen(&b, st.MindMap)
	writeReportSuggestions(&b, st.MindMap)
	return b.String()
}

// ReportFromSavedRun renders the Markdown report for a checkpointed run that
// is loaded into the engine but not (yet) actively running. The shape of the
// output matches Engine.Report().
func ReportFromSavedRun(run *SavedRun) string {
	if run == nil {
		return "# Chaos Exploration Report\n\n_No saved run available._\n"
	}
	st := Status{
		StepsRun:      run.StepsRun,
		Transitions:   run.Transitions,
		UniqueScreens: run.UniqueScreens,
		UniqueInputs:  run.UniqueInputs,
		MindMap:       run.MindMap,
		Error:         run.Error,
		LoadedRunID:   run.ID,
		StartedAt:     run.StartedAt,
		StoppedAt:     run.StoppedAt,
	}
	var b strings.Builder
	b.WriteString("# Chaos Exploration Report\n\n")
	if run.ID != "" {
		fmt.Fprintf(&b, "_Loaded saved run: `%s`_\n\n", run.ID)
	}
	writeReportSummary(&b, &st)
	writeReportScreenGraph(&b, st.MindMap)
	writeReportPerScreen(&b, st.MindMap)
	writeReportSuggestions(&b, st.MindMap)
	return b.String()
}

func writeReportSummary(b *strings.Builder, st *Status) {
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(b, "- Steps run: %d\n", st.StepsRun)
	fmt.Fprintf(b, "- Transitions: %d\n", st.Transitions)
	fmt.Fprintf(b, "- Unique screens: %d\n", st.UniqueScreens)
	fmt.Fprintf(b, "- Unique inputs: %d\n", st.UniqueInputs)
	if st.TerminationReason != "" {
		fmt.Fprintf(b, "- Termination reason: `%s`\n", st.TerminationReason)
	}
	if st.CoverageStats != nil {
		fmt.Fprintf(b, "- Saturation streak: %d / %d\n", st.CoverageStats.SaturationStreak, st.CoverageStats.SaturationThresholdSteps)
		fmt.Fprintf(b, "- New screens in last %d steps: %d\n", st.CoverageStats.WindowSteps, st.CoverageStats.NewScreensInWindow)
		fmt.Fprintf(b, "- New transitions in last %d steps: %d\n", st.CoverageStats.WindowSteps, st.CoverageStats.NewTransitionsInWindow)
	}
	if st.Error != "" {
		fmt.Fprintf(b, "- Last error: %s\n", st.Error)
	}
	b.WriteString("\n")
}

func writeReportScreenGraph(b *strings.Builder, m *MindMap) {
	b.WriteString("## Screen Graph\n\n")
	if m == nil || len(m.Areas) == 0 {
		b.WriteString("_No screens observed yet._\n\n")
		return
	}
	b.WriteString("```\n")
	hashes := sortedAreaHashes(m)
	for _, h := range hashes {
		area := m.Areas[h]
		label := strings.TrimSpace(area.Label)
		if label == "" {
			label = "(no label)"
		}
		fmt.Fprintf(b, "[%s] %s  (visits=%d)\n", shortHash(h), label, area.Visits)
		// Sort key presses by progressions, descending; show only those that
		// actually moved chaos to a different screen.
		type kpEntry struct {
			key string
			kp  *MindMapKeyPress
		}
		entries := make([]kpEntry, 0, len(area.KeyPresses))
		for k, kp := range area.KeyPresses {
			if kp == nil || kp.Progressions == 0 {
				continue
			}
			entries = append(entries, kpEntry{k, kp})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].kp.Progressions > entries[j].kp.Progressions
		})
		for _, e := range entries {
			dests := make([]string, 0, len(e.kp.Destinations))
			for d, c := range e.kp.Destinations {
				dests = append(dests, fmt.Sprintf("%s x%d", shortHash(d), c))
			}
			sort.Strings(dests)
			fmt.Fprintf(b, "    --%s--> %s\n", e.key, strings.Join(dests, ", "))
		}
	}
	b.WriteString("```\n\n")
}

func writeReportPerScreen(b *strings.Builder, m *MindMap) {
	if m == nil || len(m.Areas) == 0 {
		return
	}
	b.WriteString("## Per-Screen Details\n\n")
	for _, h := range sortedAreaHashes(m) {
		area := m.Areas[h]
		label := strings.TrimSpace(area.Label)
		if label == "" {
			label = "(no label)"
		}
		fmt.Fprintf(b, "### %s `%s`\n\n", label, shortHash(h))
		fmt.Fprintf(b, "- Size: %dx%d, fields: %d (inputs: %d, numeric: %d, hidden: %d)\n",
			area.ScreenWidth, area.ScreenHeight, area.FieldCount, area.InputFieldCount, area.NumericFieldCount, area.HiddenFieldCount)
		if len(area.AutoBlockedKeys) > 0 {
			fmt.Fprintf(b, "- Auto-blocked keys (exit-style labels): %s\n", strings.Join(area.AutoBlockedKeys, ", "))
		}
		if len(area.AutoKnownKeys) > 0 {
			fmt.Fprintf(b, "- Auto-known keys (navigation labels): %s\n", strings.Join(area.AutoKnownKeys, ", "))
		}
		if len(area.FieldDiscovery) > 0 {
			b.WriteString("- Input fields:\n")
			fieldKeys := make([]string, 0, len(area.FieldDiscovery))
			for k := range area.FieldDiscovery {
				fieldKeys = append(fieldKeys, k)
			}
			sort.Strings(fieldKeys)
			for _, fk := range fieldKeys {
				d := area.FieldDiscovery[fk]
				fmt.Fprintf(b, "    - %s: writes=%d successes=%d progressions=%d", fk, d.Writes, d.WriteSuccesses, d.Progressions)
				if d.LastWorkedValue != "" {
					fmt.Fprintf(b, " (last-working=%q)", d.LastWorkedValue)
				}
				b.WriteString("\n")
			}
		}
		writeReportKeys(b, area)
		b.WriteString("\n")
	}
}

func writeReportKeys(b *strings.Builder, area *MindMapArea) {
	if len(area.KeyPresses) == 0 {
		return
	}
	type kpEntry struct {
		key string
		kp  *MindMapKeyPress
	}
	entries := make([]kpEntry, 0, len(area.KeyPresses))
	for k, kp := range area.KeyPresses {
		entries = append(entries, kpEntry{k, kp})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].kp.Progressions != entries[j].kp.Progressions {
			return entries[i].kp.Progressions > entries[j].kp.Progressions
		}
		return entries[i].key < entries[j].key
	})
	working := make([]string, 0)
	dead := make([]string, 0)
	for _, e := range entries {
		if e.kp.Progressions > 0 {
			working = append(working, fmt.Sprintf("%s (presses=%d, progressions=%d)", e.key, e.kp.Presses, e.kp.Progressions))
		} else if e.kp.Presses > 0 {
			dead = append(dead, fmt.Sprintf("%s (presses=%d)", e.key, e.kp.Presses))
		}
	}
	if len(working) > 0 {
		fmt.Fprintf(b, "- Working AID keys: %s\n", strings.Join(working, "; "))
	}
	if len(dead) > 0 {
		fmt.Fprintf(b, "- Tried but no progress: %s\n", strings.Join(dead, "; "))
	}
}

func writeReportSuggestions(b *strings.Builder, m *MindMap) {
	if m == nil || len(m.Areas) == 0 {
		return
	}
	b.WriteString("## Suggested Next Experiments\n\n")
	allAIDKeys := []string{"Enter", "PF(1)", "PF(2)", "PF(3)", "PF(4)", "PF(5)", "PF(6)", "PF(7)", "PF(8)", "PF(9)", "PF(10)", "PF(11)", "PF(12)"}
	any := false
	for _, h := range sortedAreaHashes(m) {
		area := m.Areas[h]
		untried := []string{}
		for _, k := range allAIDKeys {
			if _, ok := area.KeyPresses[k]; !ok {
				untried = append(untried, k)
			}
		}
		// Strip auto-blocked from suggestions.
		blockedSet := make(map[string]struct{}, len(area.AutoBlockedKeys))
		for _, k := range area.AutoBlockedKeys {
			blockedSet[k] = struct{}{}
		}
		filtered := untried[:0]
		for _, k := range untried {
			if _, blocked := blockedSet[k]; blocked {
				continue
			}
			filtered = append(filtered, k)
		}
		if len(filtered) == 0 {
			continue
		}
		any = true
		label := strings.TrimSpace(area.Label)
		if label == "" {
			label = "(no label)"
		}
		// Show the first 5 untried keys to keep the list scannable.
		max := 5
		if len(filtered) < max {
			max = len(filtered)
		}
		fmt.Fprintf(b, "- **%s** `%s`: untried AID keys: %s\n", label, shortHash(h), strings.Join(filtered[:max], ", "))
	}
	if !any {
		b.WriteString("_All common AID keys have been tried on every discovered screen._\n")
	}
}

func sortedAreaHashes(m *MindMap) []string {
	out := make([]string, 0, len(m.Areas))
	for h := range m.Areas {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}
