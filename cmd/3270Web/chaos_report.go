// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/session"
)

// ChaosReportHandler handles POST /chaos/report and returns a markdown report
// summarising the discovered application structure and learned inputs/keys.
func (app *App) ChaosReportHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	var targetHost string
	var targetPort int
	withSessionLock(s, func() {
		targetHost = s.TargetHost
		targetPort = s.TargetPort
	})

	screenHints := app.chaosEngines.getScreenHints(s.ID)
	var run *chaos.SavedRun
	ctx := chaosReportContext{ScreenHints: screenHints}
	if !app.chaosEngines.isRemoved(s.ID) {
		if eng, ok := app.chaosEngines.get(s.ID); ok {
			st := eng.Status()
			run = eng.Snapshot(strings.TrimSpace(st.LoadedRunID))
			ctx.Reason = st.TerminationReason
			ctx.SaturatedNoProgress = st.SaturatedNoProgress
			ctx.InProgress = st.Active
		} else if loaded, ok := app.chaosEngines.getLoadedRun(s.ID); ok {
			run = loaded
		} else if loaded := app.loadSessionChaosRunFromDisk(s); loaded != nil {
			app.chaosEngines.setLoadedRun(s.ID, loaded)
			run = loaded.CloneRun()
		}
	}
	if run != nil {
		// Every branch above hands over a copy that belongs to this request,
		// so what was learned about hidden fields can be masked in place
		// before a single line of the report is written from it.
		scrubMindMapSecrets(run.MindMap, chaosHiddenValueMask)
		ctx.Config = chaosRunConfigOf(s)
		ctx.RunKeys = app.sessionChaosRunKeys(s, run.MindMap)
		if state := chaosStateSnapshot(s); state != nil {
			if ctx.Reason == "" {
				ctx.Reason = state.TerminationReason
				ctx.SaturatedNoProgress = state.SaturatedNoProgress
			}
		}
		if ctx.Reason == "" {
			ctx.Reason = run.TerminationReason
		}
		if runTarget := strings.TrimSpace(run.TargetHost); runTarget != "" {
			// The run knows where it ran; the session may since have been
			// pointed somewhere else, or the run loaded from another day.
			targetHost, targetPort = runTarget, 0
		}
	}

	data, filename := buildChaosDiscoveryReportMarkdown(targetHost, targetPort, run, ctx, time.Now())
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", data)
}

// chaosReportContext is what the report needs to know that the saved run
// does not carry: how the run was configured, which keys it could press, why
// it ended, and the hints set for individual screens. Every field is
// optional; the report says so where something is not known rather than
// filling the gap with a default.
type chaosReportContext struct {
	// ScreenHints are the per-screen hints, keyed by screen hash.
	ScreenHints map[string]chaos.ScreenHint
	// Config is the summary of the run's configuration, nil when unknown.
	Config *session.ChaosRunConfig
	// RunKeys are the AID keys the run could press (blocked keys removed).
	// "Untried" is measured against these.
	RunKeys []string
	// Reason is the engine's termination reason code, "" when unknown.
	Reason              string
	SaturatedNoProgress bool
	// InProgress is true when the report is written while the run is going.
	InProgress bool
}

// untried returns the run's keys never pressed from the screen.
func (ctx chaosReportContext) untried(hash string, area *chaos.MindMapArea) []string {
	return chaosUntriedKeys(area, ctx.RunKeys, ctx.ScreenHints[strings.TrimSpace(hash)])
}

type chaosReportFieldRow struct {
	Key           string
	Row           int
	Column        int
	Length        int
	Metadata      *chaos.MindMapFieldMetadata
	Discovery     *chaos.MindMapFieldDiscovery
	KnownTried    []string
	KnownWorking  []string
	AllowedType   string
	ObservedTypes string
	// Name is the business name given to the field, when it has one.
	Name string
	// Secret is true for a hidden field or one annotated as sensitive: its
	// values are never printed.
	Secret bool
}

type chaosReportScreenRow struct {
	Area             *chaos.MindMapArea
	RefID            string
	Hash             string
	Label            string
	WorkingKeyCount  int
	ProgressFieldCnt int
	// VariantOf is the ref of the first screen in this one's size-variant
	// group, empty when the screen is that first one or has no variants.
	VariantOf string
}

// chaosReportVariantGroup is one screen that the run saw at more than one
// terminal size: same title, same fields in the same places, different
// dimensions. The engine keys screens by layout, so each size is a screen of
// its own in the map; a reader counting "screens" wants them counted once.
type chaosReportVariantGroup struct {
	Label string
	Refs  []string
	Sizes []string
}

// chaosReportVariantKey identifies the screen an area is a size-variant of:
// its label and its field template (which fields, where, how long). Areas
// with no label or no recorded size are never grouped — there is nothing to
// say they are the same screen.
func chaosReportVariantKey(row chaosReportScreenRow) string {
	area := row.Area
	if area == nil || strings.TrimSpace(area.Label) == "" || area.ScreenWidth <= 0 || area.ScreenHeight <= 0 {
		return ""
	}
	keys := make([]string, 0, len(area.FieldMetadata))
	for k := range area.FieldMetadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.ToLower(strings.Join(strings.Fields(area.Label), " ")) + "\x00" + strings.Join(keys, ",")
}

// groupChaosReportSizeVariants marks rows that are size-variants of an
// earlier row and returns the groups, in the order their first member
// appears. rows must already carry their RefIDs.
func groupChaosReportSizeVariants(rows []chaosReportScreenRow) []chaosReportVariantGroup {
	type group struct {
		indexes []int
		sizes   map[string]struct{}
	}
	byKey := make(map[string]*group)
	var order []string
	for i, row := range rows {
		key := chaosReportVariantKey(row)
		if key == "" {
			continue
		}
		g := byKey[key]
		if g == nil {
			g = &group{sizes: make(map[string]struct{})}
			byKey[key] = g
			order = append(order, key)
		}
		g.indexes = append(g.indexes, i)
		g.sizes[fmt.Sprintf("%dx%d", row.Area.ScreenHeight, row.Area.ScreenWidth)] = struct{}{}
	}
	var out []chaosReportVariantGroup
	for _, key := range order {
		g := byKey[key]
		// Same title and template at the same size is two different screens
		// that happen to look alike, not one screen at two sizes.
		if len(g.indexes) < 2 || len(g.sizes) != len(g.indexes) {
			continue
		}
		vg := chaosReportVariantGroup{Label: rows[g.indexes[0]].Label}
		for n, idx := range g.indexes {
			vg.Refs = append(vg.Refs, rows[idx].RefID)
			vg.Sizes = append(vg.Sizes, fmt.Sprintf("%dx%d", rows[idx].Area.ScreenHeight, rows[idx].Area.ScreenWidth))
			if n > 0 {
				rows[idx].VariantOf = rows[g.indexes[0]].RefID
			}
		}
		out = append(out, vg)
	}
	return out
}

// chaosReportTransitionTotals returns how many times the screen changed in
// total, and along how many distinct paths. They are different numbers and
// the report used to print the second while describing the first: a run that
// went menu -> detail -> menu fifty times made a hundred transitions along
// two paths.
func chaosReportTransitionTotals(run *chaos.SavedRun, m *chaos.MindMap) (total, distinct int) {
	if m != nil {
		for _, area := range m.Areas {
			if area == nil {
				continue
			}
			for _, kp := range area.KeyPresses {
				if kp == nil {
					continue
				}
				total += kp.Progressions
				for _, count := range kp.Destinations {
					if count > 0 {
						distinct++
					}
				}
			}
		}
	}
	if run != nil {
		// The run's own count of distinct (from, key, to) transitions is
		// authoritative when it is the larger: the map can have been trimmed.
		if run.Transitions > distinct {
			distinct = run.Transitions
		}
	}
	if total < distinct {
		total = distinct
	}
	return total, distinct
}

var chaosMindMapFieldKeyRe = regexp.MustCompile(`^R(\d+)C(\d+)L(\d+)$`)

func buildChaosDiscoveryReportMarkdown(targetHost string, targetPort int, run *chaos.SavedRun, ctx chaosReportContext, generatedAt time.Time) ([]byte, string) {
	screenHints := ctx.ScreenHints
	if run == nil {
		name := "chaos-discovery-report.md"
		return []byte("# Chaos Discovery Report\n\nNo chaos run data available.\n"), name
	}

	var buf bytes.Buffer
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(&buf, format, args...)
	}

	titleRunID := strings.TrimSpace(run.ID)
	reportTitle := "Chaos Discovery Report"
	if titleRunID != "" {
		reportTitle = fmt.Sprintf("%s (%s)", reportTitle, titleRunID)
	}
	write("# %s\n\n", mdText(reportTitle))
	write("Generated: `%s`\n\n", generatedAt.UTC().Format(time.RFC3339))

	mindMap := run.MindMap
	if mindMap == nil || len(mindMap.Areas) == 0 {
		write("## TL;DR\n\n")
		write("- No discovery screens were recorded in the chaos run metadata.\n")
		write("- Run attempts: **%d**, distinct transitions: **%d**.\n", run.StepsRun, run.Transitions)
		if summary := chaosReportEndedLine(ctx); summary != "" {
			write("- %s\n", mdText(summary))
		}
		write("\n")
		write("## Run Summary\n\n")
		write("- **Target**: %s\n", mdText(formatTargetHostPort(targetHost, targetPort)))
		write("- **Run ID**: %s\n", mdText(orNA(titleRunID)))
		write("- **Run Window**: %s\n", mdText(formatRunWindow(run.StartedAt, run.StoppedAt)))
		write("- **Duration**: %s\n", mdText(formatRunDuration(run.StartedAt, run.StoppedAt)))
		write("- **Attempts**: %d\n", run.StepsRun)
		write("- **Distinct Transitions**: %d\n", run.Transitions)
		writeChaosReportConfiguration(&buf, ctx)
		write("## Discovery Overview\n\n")
		write("No discovery screens were recorded in the run metadata.\n")
		return buf.Bytes(), chaosReportFileName(run.ID, generatedAt)
	}

	screenRows := buildChaosReportScreenRows(mindMap)
	screenRefByHash := make(map[string]string, len(screenRows))
	for i := range screenRows {
		screenRows[i].RefID = chaosReportScreenRef(i + 1)
		screenRefByHash[screenRows[i].Hash] = screenRows[i].RefID
	}
	variantGroups := groupChaosReportSizeVariants(screenRows)
	totalTransitions, distinctTransitions := chaosReportTransitionTotals(run, mindMap)

	write("## TL;DR\n\n")
	for _, line := range buildChaosDiscoveryTLDRLines(targetHost, targetPort, run, mindMap, screenRows, variantGroups, ctx) {
		write("- %s\n", mdText(line))
	}
	write("\n")

	write("## Run Summary\n\n")
	write("- **Target**: %s\n", mdText(formatTargetHostPort(targetHost, targetPort)))
	write("- **Run ID**: %s\n", mdText(orNA(titleRunID)))
	write("- **Run Window**: %s\n", mdText(formatRunWindow(run.StartedAt, run.StoppedAt)))
	write("- **Duration**: %s\n", mdText(formatRunDuration(run.StartedAt, run.StoppedAt)))
	if label := strings.TrimSpace(run.Label); label != "" {
		write("- **Run Name**: %s\n", mdText(label))
	}
	write("- **Attempts**: %d\n", run.StepsRun)
	write("- **Screen Changes (total)**: %d\n", totalTransitions)
	write("- **Distinct Transitions**: %d (each a from-screen, key and to-screen seen at least once)\n", distinctTransitions)
	write("- **Unique Screens**: %d\n", run.UniqueScreens)
	if len(variantGroups) > 0 {
		write("- **Screens After Grouping Size Variants**: %d\n", chaosReportGroupedScreenCount(len(screenRows), variantGroups))
	}
	write("- **Unique Input Values**: %d\n", run.UniqueInputs)
	if ended := chaosReportEndedLine(ctx); ended != "" {
		write("- **Why It Ended**: %s\n", mdText(strings.TrimPrefix(ended, "Why it ended: ")))
	}
	if strings.TrimSpace(run.Error) != "" {
		write("- **Run Error**: %s\n", mdText(run.Error))
	}
	if run.WorkflowHeader != nil {
		write("- **Workflow Export Header**: present\n")
		if run.WorkflowHeader.OutputFilePath != "" {
			write("  - Output file path: `%s`\n", mdCode(run.WorkflowHeader.OutputFilePath))
		}
	}
	write("\n")

	writeChaosReportConfiguration(&buf, ctx)

	write("## Discovery Highlights\n\n")
	write("%s\n\n", mdText(buildChaosDiscoveryExecutiveSummary(mindMap)))

	write("## Discovery Flow Map\n\n")
	write("The map below references screen captures listed in the **Screen Inventory** and **Detailed Screens** sections.\n\n")
	write("%s\n\n", buildChaosDiscoveryFlowMapMarkdown(mindMap, screenRows, screenRefByHash))

	write("## Screen Inventory\n\n")
	write("| Ref | Screen | Hash | Visits | Inputs | Numeric | Working Keys | Progressing Fields | Captured Example | Variants Data |\n")
	write("|---|---|---|---:|---:|---:|---:|---:|---|---|\n")
	for _, sr := range screenRows {
		area := sr.Area
		knownWorkingVals, knownTriedVals := countAreaValues(area)
		captured := "No preview"
		if strings.TrimSpace(area.PreviewText) != "" {
			pw, ph := area.PreviewWidth, area.PreviewHeight
			if pw > 0 && ph > 0 {
				captured = fmt.Sprintf("%dx%d example", ph, pw)
			} else {
				captured = "Example captured"
			}
		}
		write(
			"| `%s` | %s | `%s` | %d | %d | %d | %d | %d | %s | %d working / %d tried |\n",
			mdCode(sr.RefID),
			mdTableCell(sr.Label),
			mdCode(sr.Hash),
			area.Visits,
			area.InputFieldCount,
			area.NumericFieldCount,
			sr.WorkingKeyCount,
			sr.ProgressFieldCnt,
			mdTableCell(captured),
			knownWorkingVals,
			knownTriedVals,
		)
	}
	write("\n")
	if len(variantGroups) > 0 {
		write("### Size Variants\n\n")
		write("These entries are one screen seen at more than one terminal size: the title and the fields are the same, only the dimensions differ. They are listed separately above because the run met them separately.\n\n")
		for _, g := range variantGroups {
			parts := make([]string, 0, len(g.Refs))
			for i, ref := range g.Refs {
				parts = append(parts, fmt.Sprintf("`%s` (%s)", mdCode(ref), g.Sizes[i]))
			}
			write("- %s: %s\n", mdText(g.Label), strings.Join(parts, ", "))
		}
		write("\n")
	}

	write("## Global Function Key Activity\n\n")
	write("%s\n\n", buildChaosGlobalKeyTable(run.AIDKeyCounts))

	write("## Detailed Screens\n\n")
	write("Each screen section includes one captured example. The same unique screen may appear with different data values during other visits.\n\n")
	for i, sr := range screenRows {
		writeChaosScreenSection(&buf, i+1, sr, mindMap, screenHints[strings.TrimSpace(sr.Hash)], screenRefByHash, ctx)
	}

	writeChaosReportBusinessFunctions(&buf, mindMap, screenRefByHash)
	writeChaosReportNextSteps(&buf, mindMap, ctx)

	write("## Notes\n\n")
	write("- This report is based on observed chaos exploration attempts and is **not** an exhaustive validation of all host behavior.\n")
	write("- The Discovery Flow map references screen IDs (for example `S01`) used in the Screen Inventory and Detailed Screens sections.\n")
	write("- Captured screen previews are examples from one observed visit and may differ across other visits based on data entered.\n")
	write("- \"Confirmed working data\" means a successful field write observed during a progression from the screen (screen transition occurred).\n")
	write("- \"Allowed data type\" combines host field attributes (for example numeric-only) with observed working/tried value patterns.\n")
	write("- Values typed into hidden (non-display) fields, and into fields annotated as sensitive, are never printed: the report says that a value is known, not what it is.\n")
	write("- \"Screen changes (total)\" counts every time a key press led to a different screen; \"distinct transitions\" counts each from-screen, key and to-screen combination once.\n")

	return buf.Bytes(), chaosReportFileName(run.ID, generatedAt)
}

func chaosReportFileName(runID string, generatedAt time.Time) string {
	id := strings.TrimSpace(runID)
	if id == "" {
		return "chaos-discovery-report.md"
	}
	return fmt.Sprintf("chaos-discovery-report-%s.md", sanitizeFilePart(id))
}

func sanitizeFilePart(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "report"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func buildChaosDiscoveryExecutiveSummary(m *chaos.MindMap) string {
	if m == nil || len(m.Areas) == 0 {
		return "No screens discovered."
	}
	totalAreas := 0
	totalVisits := 0
	totalInputFields := 0
	totalNumericFields := 0
	totalHiddenFields := 0
	totalFieldProgressions := 0
	totalWorkingKeys := 0
	workingKeySet := make(map[string]struct{})
	for _, area := range m.Areas {
		if area == nil {
			continue
		}
		totalAreas++
		totalVisits += area.Visits
		totalInputFields += area.InputFieldCount
		totalNumericFields += area.NumericFieldCount
		totalHiddenFields += area.HiddenFieldCount
		for _, fd := range area.FieldDiscovery {
			totalFieldProgressions += fd.Progressions
		}
		for key, kp := range area.KeyPresses {
			if kp != nil && kp.Progressions > 0 {
				if _, seen := workingKeySet[key]; !seen {
					workingKeySet[key] = struct{}{}
					totalWorkingKeys++
				}
			}
		}
	}
	return fmt.Sprintf(
		"Chaos discovered **%d screen(s)** across **%d total visit(s)**. It identified **%d input field(s)** (%d numeric, %d hidden), observed **%d field progression event(s)**, and confirmed **%d unique working function/AID key(s)**.",
		totalAreas, totalVisits, totalInputFields, totalNumericFields, totalHiddenFields, totalFieldProgressions, totalWorkingKeys,
	)
}

func buildChaosGlobalKeyTable(aidCounts map[string]int) string {
	if len(aidCounts) == 0 {
		return "No AID/function key activity recorded."
	}
	type row struct {
		Key   string
		Count int
	}
	rows := make([]row, 0, len(aidCounts))
	for key, count := range aidCounts {
		rows = append(rows, row{Key: strings.TrimSpace(key), Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	var b strings.Builder
	b.WriteString("| Key | Presses |\n")
	b.WriteString("|---|---:|\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("| %s | %d |\n", mdTableCell(r.Key), r.Count))
	}
	return b.String()
}

func buildChaosReportScreenRows(m *chaos.MindMap) []chaosReportScreenRow {
	rows := make([]chaosReportScreenRow, 0, len(m.Areas))
	for hash, area := range m.Areas {
		if area == nil {
			continue
		}
		workingKeyCount := 0
		for _, kp := range area.KeyPresses {
			if kp != nil && kp.Progressions > 0 {
				workingKeyCount++
			}
		}
		progressFieldCnt := 0
		for _, fd := range area.FieldDiscovery {
			if fd.Progressions > 0 {
				progressFieldCnt++
			}
		}
		label := strings.TrimSpace(area.Label)
		if label == "" {
			label = "Unlabeled screen"
		}
		rows = append(rows, chaosReportScreenRow{
			Area:             area,
			Hash:             strings.TrimSpace(hash),
			Label:            label,
			WorkingKeyCount:  workingKeyCount,
			ProgressFieldCnt: progressFieldCnt,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		a := rows[i].Area
		b := rows[j].Area
		if a == nil || b == nil {
			return rows[i].Hash < rows[j].Hash
		}
		if a.Visits != b.Visits {
			return a.Visits > b.Visits
		}
		if rows[i].Label != rows[j].Label {
			return rows[i].Label < rows[j].Label
		}
		return rows[i].Hash < rows[j].Hash
	})
	return rows
}

func chaosReportScreenRef(n int) string {
	if n < 1 {
		n = 1
	}
	if n < 10 {
		return fmt.Sprintf("S0%d", n)
	}
	return fmt.Sprintf("S%d", n)
}

func buildChaosDiscoveryTLDRLines(targetHost string, targetPort int, run *chaos.SavedRun, m *chaos.MindMap, screenRows []chaosReportScreenRow, variantGroups []chaosReportVariantGroup, ctx chaosReportContext) []string {
	lines := make([]string, 0, 8)
	total, distinct := chaosReportTransitionTotals(run, m)
	lines = append(lines, fmt.Sprintf(
		"Chaos explored %s for %d attempt%s. The screen changed %d time%s in total, along %d distinct transition%s.",
		formatTargetHostPort(targetHost, targetPort),
		run.StepsRun,
		pluralS(run.StepsRun),
		total,
		pluralS(total),
		distinct,
		pluralS(distinct),
	))
	if ended := chaosReportEndedLine(ctx); ended != "" {
		lines = append(lines, ended)
	}
	screensLine := fmt.Sprintf(
		"%d unique screen%s and %d unique input value%s were captured.",
		run.UniqueScreens,
		pluralS(run.UniqueScreens),
		run.UniqueInputs,
		pluralS(run.UniqueInputs),
	)
	if len(variantGroups) > 0 {
		grouped := chaosReportGroupedScreenCount(len(screenRows), variantGroups)
		screensLine += fmt.Sprintf(" Some are the same screen at different terminal sizes: counting those once leaves %d.", grouped)
	}
	lines = append(lines, screensLine)
	if len(screenRows) > 0 {
		top := screenRows[0]
		if top.Area != nil {
			lines = append(lines, fmt.Sprintf(
				"Most visited screen: %s (%s) with %d visit%s and %d input field%s.",
				top.RefID,
				top.Label,
				top.Area.Visits,
				pluralS(top.Area.Visits),
				top.Area.InputFieldCount,
				pluralS(top.Area.InputFieldCount),
			))
		}
	}
	if m != nil {
		totalProgressFields := 0
		totalWorkingKeys := 0
		workingKeys := map[string]struct{}{}
		for _, area := range m.Areas {
			if area == nil {
				continue
			}
			for _, fd := range area.FieldDiscovery {
				if fd.Progressions > 0 {
					totalProgressFields++
				}
			}
			for key, kp := range area.KeyPresses {
				if kp != nil && kp.Progressions > 0 {
					workingKeys[key] = struct{}{}
				}
			}
		}
		totalWorkingKeys = len(workingKeys)
		lines = append(lines, fmt.Sprintf(
			"%d progressing field discovery signal%s and %d confirmed working function/AID key%s were observed.",
			totalProgressFields,
			pluralS(totalProgressFields),
			totalWorkingKeys,
			pluralS(totalWorkingKeys),
		))
		// Measured against the keys this run could press, not the built-in
		// defaults: see chaosReportContext.RunKeys.
		frontierScreens := 0
		for hash, area := range m.Areas {
			if area == nil {
				continue
			}
			if len(ctx.untried(hash, area)) > 0 {
				frontierScreens++
			}
		}
		if frontierScreens > 0 {
			lines = append(lines, fmt.Sprintf(
				"%d screen%s still %s keys the run could press but never did there — the exploration frontier is not exhausted.",
				frontierScreens,
				pluralS(frontierScreens),
				pluralHave(frontierScreens),
			))
		}
	}
	if strings.TrimSpace(run.Error) != "" {
		lines = append(lines, fmt.Sprintf("Run ended with error: %s.", strings.TrimSpace(run.Error)))
	}
	return lines
}

func buildChaosDiscoveryFlowMapMarkdown(m *chaos.MindMap, screenRows []chaosReportScreenRow, screenRefByHash map[string]string) string {
	if m == nil || len(screenRows) == 0 {
		return "No discovery flow data recorded."
	}
	type edgeRow struct {
		FromHash string
		ToHash   string
		Count    int
		Keys     map[string]int
	}
	edgeMap := make(map[string]*edgeRow)
	for _, sr := range screenRows {
		area := sr.Area
		if area == nil {
			continue
		}
		for key, kp := range area.KeyPresses {
			if kp == nil || len(kp.Destinations) == 0 {
				continue
			}
			for toHash, count := range kp.Destinations {
				toHash = strings.TrimSpace(toHash)
				if toHash == "" || count <= 0 {
					continue
				}
				mapKey := sr.Hash + "->" + toHash
				row := edgeMap[mapKey]
				if row == nil {
					row = &edgeRow{
						FromHash: sr.Hash,
						ToHash:   toHash,
						Keys:     make(map[string]int),
					}
					edgeMap[mapKey] = row
				}
				row.Count += count
				row.Keys[strings.TrimSpace(key)] += count
			}
		}
	}

	edges := make([]edgeRow, 0, len(edgeMap))
	for _, row := range edgeMap {
		if row == nil || row.Count <= 0 {
			continue
		}
		edges = append(edges, *row)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Count != edges[j].Count {
			return edges[i].Count > edges[j].Count
		}
		if edges[i].FromHash != edges[j].FromHash {
			return edges[i].FromHash < edges[j].FromHash
		}
		return edges[i].ToHash < edges[j].ToHash
	})

	var b strings.Builder
	b.WriteString("```mermaid\n")
	b.WriteString("flowchart LR\n")
	for _, sr := range screenRows {
		label := fmt.Sprintf("%s: %s", sr.RefID, truncateForReportMermaid(sr.Label, 40))
		b.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", mermaidNodeID(sr.RefID), mermaidLabel(label)))
	}
	if len(edges) == 0 {
		b.WriteString("  %% No transitions observed between captured screens\n")
	}
	for _, e := range edges {
		fromRef := screenRefByHash[strings.TrimSpace(e.FromHash)]
		toRef := screenRefByHash[strings.TrimSpace(e.ToHash)]
		if fromRef == "" || toRef == "" {
			continue
		}
		b.WriteString(fmt.Sprintf(
			"  %s -->|\"%s\"| %s\n",
			mermaidNodeID(fromRef),
			mermaidLabel(truncateForReportMermaid(chaosMapEdgeKeySummary(e.Keys, e.Count), 36)),
			mermaidNodeID(toRef),
		))
	}
	b.WriteString("```\n\n")

	b.WriteString("| From | To | Transitions | Keys Observed |\n")
	b.WriteString("|---|---|---:|---|\n")
	if len(edges) == 0 {
		b.WriteString("| _None_ | _None_ | 0 | No transitions recorded |\n")
		return b.String()
	}
	for _, e := range edges {
		fromRef := screenRefByHash[strings.TrimSpace(e.FromHash)]
		toRef := screenRefByHash[strings.TrimSpace(e.ToHash)]
		if fromRef == "" || toRef == "" {
			continue
		}
		b.WriteString(fmt.Sprintf(
			"| `%s` | `%s` | %d | %s |\n",
			mdCode(fromRef),
			mdCode(toRef),
			e.Count,
			mdTableCell(chaosMapEdgeKeySummary(e.Keys, e.Count)),
		))
	}
	return b.String()
}

// mermaidLabel escapes text for use inside a double-quoted Mermaid label.
//
// Mermaid does not read backslash escapes, so Go's %q — which this used to
// be — produced \" for a quote in a screen title, and Mermaid took the quote
// as the end of the label and failed to parse the diagram. Mermaid's own
// escape is an entity code: #quot; for a quote, #35; for the # that starts
// one. Angle brackets are escaped too, because labels are rendered as HTML.
func mermaidLabel(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '#':
			b.WriteString("#35;")
		case '"':
			b.WriteString("#quot;")
		case '<':
			b.WriteString("#lt;")
		case '>':
			b.WriteString("#gt;")
		case '&':
			b.WriteString("#amp;")
		case '\\':
			b.WriteString("#92;")
		case '`':
			// A backtick opens Mermaid's "markdown string" syntax.
			b.WriteString("#96;")
		case '|':
			b.WriteString("#124;")
		default:
			if r < 0x20 || r == 0x7f {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mermaidNodeID(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, ref)
	if ref == "" {
		return "S_unknown"
	}
	if ref[0] >= '0' && ref[0] <= '9' {
		return "S_" + ref
	}
	return ref
}

func truncateForReportMermaid(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	if max == 1 {
		return string(r[:1])
	}
	return string(r[:max-1]) + "…"
}

func chaosMapEdgeKeySummary(keys map[string]int, total int) string {
	if len(keys) == 0 {
		if total > 0 {
			return fmt.Sprintf("%d transition%s", total, pluralS(total))
		}
		return "No key data"
	}
	type row struct {
		Key   string
		Count int
	}
	rows := make([]row, 0, len(keys))
	for key, count := range keys {
		key = strings.TrimSpace(key)
		if key == "" || count <= 0 {
			continue
		}
		rows = append(rows, row{Key: key, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	parts := make([]string, 0, len(rows))
	for i, r := range rows {
		if i >= 3 {
			break
		}
		if r.Count > 1 {
			parts = append(parts, fmt.Sprintf("%s (%d)", r.Key, r.Count))
		} else {
			parts = append(parts, r.Key)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d transition%s", total, pluralS(total))
	}
	return strings.Join(parts, ", ")
}

func writeChaosScreenSection(buf *bytes.Buffer, _ int, sr chaosReportScreenRow, m *chaos.MindMap, hint chaos.ScreenHint, screenRefByHash map[string]string, ctx chaosReportContext) {
	area := sr.Area
	if area == nil {
		return
	}
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(buf, format, args...)
	}

	write("### %s. %s\n\n", mdCode(sr.RefID), mdText(sr.Label))
	write("- **Screen Ref**: `%s`\n", mdCode(sr.RefID))
	write("- **Screen Hash**: `%s`\n", mdCode(sr.Hash))
	write("- **Visits**: %d\n", area.Visits)
	write("- **First Seen**: %s\n", mdText(formatTime(area.FirstSeen)))
	write("- **Last Seen**: %s\n", mdText(formatTime(area.LastSeen)))
	write("- **Field Counts**: %d total, %d input, %d numeric, %d hidden\n", area.FieldCount, area.InputFieldCount, area.NumericFieldCount, area.HiddenFieldCount)
	if area.ScreenWidth > 0 && area.ScreenHeight > 0 {
		write("- **Screen Size**: %dx%d\n", area.ScreenHeight, area.ScreenWidth)
	}
	if sr.VariantOf != "" {
		write("- **Size Variant Of**: `%s` (the same screen at a different terminal size)\n", mdCode(sr.VariantOf))
	}
	if purpose := strings.TrimSpace(area.BusinessPurpose); purpose != "" {
		write("- **Business Purpose**: %s\n", mdText(purpose))
	}
	if notes := strings.TrimSpace(area.BusinessNotes); notes != "" {
		write("- **Business Notes**: %s\n", mdText(notes))
	}
	write("\n")

	write("#### Captured Screen Example\n\n")
	if strings.TrimSpace(area.PreviewText) == "" {
		write("No captured screen preview is available for this screen in the current run.\n\n")
	} else {
		write("Example captured during one observed visit (values may differ on other visits based on data entered):\n\n")
		write("```text\n%s\n```\n\n", sanitizeMarkdownCodeFence(area.PreviewText))
	}

	write("#### Known Working Function Keys\n\n")
	write("%s\n\n", buildChaosWorkingKeyTable(area, m, screenRefByHash))

	if untried := ctx.untried(sr.Hash, area); len(untried) > 0 {
		write("#### Untried Keys (Exploration Frontier)\n\n")
		write("The run could press these keys but never did from this screen: %s. Continuing the exploration steers toward them; they are also worth trying by hand.\n\n", mdText(strings.Join(untried, ", ")))
	}
	if blocked := chaosReportBlockedKeysForScreen(area, hint, ctx); len(blocked) > 0 {
		write("Keys not pressed here on purpose (blocked): %s.\n\n", mdText(strings.Join(blocked, ", ")))
	}

	write("#### Input Field Discovery\n\n")
	fields := buildChaosFieldRows(area)
	if len(fields) == 0 {
		write("No input field metadata or field discovery values were recorded for this screen.\n\n")
	} else {
		write("| Field | Name | Allowed Data Type | Observed Working Types | Writes | Successes | Progressions | Confirmed Working Data | Tried Data (sample) |\n")
		write("|---|---|---|---|---:|---:|---:|---|---|\n")
		for _, f := range fields {
			writes, successes, progressions := 0, 0, 0
			if f.Discovery != nil {
				writes = f.Discovery.Writes
				successes = f.Discovery.WriteSuccesses
				progressions = f.Discovery.Progressions
			}
			working := joinSampleValues(f.KnownWorking, 5)
			tried := joinSampleValues(f.KnownTried, 5)
			observed := orNA(strings.TrimSpace(f.ObservedTypes))
			if f.Secret {
				// The values were masked before the report was built; say
				// so in words rather than printing the mask as if it were
				// data, and say nothing about what kind of value it was.
				working = chaosReportWithheld(len(f.KnownWorking))
				tried = chaosReportWithheld(len(f.KnownTried))
				observed = "withheld"
			}
			write(
				"| %s | %s | %s | %s | %d | %d | %d | %s | %s |\n",
				mdTableCell(formatFieldLabel(f)),
				mdTableCell(orNA(f.Name)),
				mdTableCell(f.AllowedType),
				mdTableCell(observed),
				writes,
				successes,
				progressions,
				mdTableCell(working),
				mdTableCell(tried),
			)
		}
		write("\n")
	}

	write("#### Navigation Destinations (Observed)\n\n")
	write("%s\n\n", buildChaosDestinationSummary(area, m, screenRefByHash))

	if len(hint.KnownData) > 0 || len(hint.KnownKeys) > 0 || len(hint.KeyAssignments) > 0 {
		write("#### Analyst Hints (UI-Configured)\n\n")
		if len(hint.KnownData) > 0 {
			write("- **Known Data Hints**: %s\n", mdText(strings.Join(cleanAndSortStrings(hint.KnownData), ", ")))
		}
		if len(hint.KnownKeys) > 0 {
			write("- **Known Key Hints**: %s\n", mdText(strings.Join(cleanAndSortStrings(hint.KnownKeys), ", ")))
		}
		if len(hint.KeyAssignments) > 0 {
			keys := make([]string, 0, len(hint.KeyAssignments))
			for label := range hint.KeyAssignments {
				keys = append(keys, label)
			}
			sort.Strings(keys)
			write("- **Key Assignments**:\n")
			for _, label := range keys {
				write("  - `%s` -> `%s`\n", mdCode(label), mdCode(strings.TrimSpace(hint.KeyAssignments[label])))
			}
		}
		write("\n")
	}
}

func buildChaosWorkingKeyTable(area *chaos.MindMapArea, m *chaos.MindMap, screenRefByHash map[string]string) string {
	if area == nil || len(area.KeyPresses) == 0 {
		return "No function/AID key presses recorded."
	}
	type row struct {
		Key          string
		Presses      int
		Progressions int
		Destinations string
	}
	var working []row
	var tried []row
	for key, kp := range area.KeyPresses {
		if kp == nil {
			continue
		}
		r := row{
			Key:          strings.TrimSpace(key),
			Presses:      kp.Presses,
			Progressions: kp.Progressions,
			Destinations: formatDestinationList(kp.Destinations, m, screenRefByHash, 3),
		}
		if kp.Progressions > 0 {
			working = append(working, r)
		} else {
			tried = append(tried, r)
		}
	}
	sortRows := func(rows []row) {
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Progressions != rows[j].Progressions {
				return rows[i].Progressions > rows[j].Progressions
			}
			if rows[i].Presses != rows[j].Presses {
				return rows[i].Presses > rows[j].Presses
			}
			return rows[i].Key < rows[j].Key
		})
	}
	sortRows(working)
	sortRows(tried)

	var b strings.Builder
	if len(working) > 0 {
		b.WriteString("| Key | Presses | Progressions | Known Destinations |\n")
		b.WriteString("|---|---:|---:|---|\n")
		for _, r := range working {
			b.WriteString(fmt.Sprintf("| %s | %d | %d | %s |\n",
				mdTableCell(r.Key), r.Presses, r.Progressions, mdTableCell(orNA(r.Destinations)),
			))
		}
	}
	if len(tried) > 0 {
		if len(working) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("**Other keys tried (no confirmed progression):** ")
		names := make([]string, 0, len(tried))
		for _, r := range tried {
			names = append(names, fmt.Sprintf("%s (%d press%s)", strings.TrimSpace(r.Key), r.Presses, pluralSuffix(r.Presses)))
		}
		b.WriteString(mdText(strings.Join(names, ", ")))
	}
	if b.Len() == 0 {
		return "No function/AID key presses recorded."
	}
	return b.String()
}

func buildChaosDestinationSummary(area *chaos.MindMapArea, m *chaos.MindMap, screenRefByHash map[string]string) string {
	if area == nil || len(area.KeyPresses) == 0 {
		return "No transitions recorded from this screen."
	}
	type destRow struct {
		Key         string
		Destination string
		Count       int
	}
	var rows []destRow
	for key, kp := range area.KeyPresses {
		if kp == nil || len(kp.Destinations) == 0 {
			continue
		}
		for toHash, count := range kp.Destinations {
			if count <= 0 {
				continue
			}
			rows = append(rows, destRow{
				Key:         strings.TrimSpace(key),
				Destination: destinationLabelForHashWithRef(toHash, m, screenRefByHash),
				Count:       count,
			})
		}
	}
	if len(rows) == 0 {
		return "No transitions recorded from this screen."
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		if rows[i].Key != rows[j].Key {
			return rows[i].Key < rows[j].Key
		}
		return rows[i].Destination < rows[j].Destination
	})
	var b strings.Builder
	b.WriteString("| Key | Destination | Count |\n")
	b.WriteString("|---|---|---:|\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("| %s | %s | %d |\n", mdTableCell(r.Key), mdTableCell(r.Destination), r.Count))
	}
	return b.String()
}

func buildChaosFieldRows(area *chaos.MindMapArea) []chaosReportFieldRow {
	if area == nil {
		return nil
	}
	keySet := make(map[string]struct{})
	for k := range area.FieldMetadata {
		keySet[strings.TrimSpace(k)] = struct{}{}
	}
	for k := range area.FieldDiscovery {
		keySet[strings.TrimSpace(k)] = struct{}{}
	}
	for k := range area.KnownTriedValues {
		keySet[strings.TrimSpace(k)] = struct{}{}
	}
	for k := range area.KnownWorkingValues {
		keySet[strings.TrimSpace(k)] = struct{}{}
	}
	for k := range area.FieldSemantics {
		keySet[strings.TrimSpace(k)] = struct{}{}
	}
	rows := make([]chaosReportFieldRow, 0, len(keySet))
	for key := range keySet {
		if key == "" {
			continue
		}
		row := chaosReportFieldRow{Key: key}
		if meta, ok := area.FieldMetadata[key]; ok {
			metaCopy := meta
			row.Metadata = &metaCopy
			row.Row = meta.Row
			row.Column = meta.Column
			row.Length = meta.Length
		}
		if fd, ok := area.FieldDiscovery[key]; ok {
			fdCopy := fd
			row.Discovery = &fdCopy
		}
		row.KnownTried = append([]string(nil), area.KnownTriedValues[key]...)
		row.KnownWorking = append([]string(nil), area.KnownWorkingValues[key]...)
		if row.Row == 0 || row.Column == 0 || row.Length == 0 {
			if r, c, l, ok := parseMindMapFieldKey(key); ok {
				row.Row, row.Column, row.Length = r, c, l
			}
		}
		row.Secret = chaosFieldIsSecret(area, key)
		if sem, ok := area.FieldSemantics[key]; ok {
			row.Name = strings.TrimSpace(sem.Name)
		}
		if row.Secret {
			row.AllowedType = inferAllowedFieldType(row.Metadata, nil, nil)
		} else {
			row.AllowedType = inferAllowedFieldType(row.Metadata, row.KnownWorking, row.KnownTried)
			row.ObservedTypes = summarizeObservedValueTypes(row.KnownWorking, row.KnownTried)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Row != b.Row {
			return a.Row < b.Row
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Length != b.Length {
			return a.Length < b.Length
		}
		return a.Key < b.Key
	})
	return rows
}

func parseMindMapFieldKey(key string) (row, col, length int, ok bool) {
	m := chaosMindMapFieldKeyRe.FindStringSubmatch(strings.TrimSpace(key))
	if len(m) != 4 {
		return 0, 0, 0, false
	}
	r, err1 := strconv.Atoi(m[1])
	c, err2 := strconv.Atoi(m[2])
	l, err3 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return r, c, l, true
}

func formatFieldLabel(f chaosReportFieldRow) string {
	flags := make([]string, 0, 3)
	if f.Metadata != nil {
		if f.Metadata.Numeric {
			flags = append(flags, "numeric")
		}
		if f.Metadata.Hidden {
			flags = append(flags, "hidden")
		}
		if f.Metadata.MultiLine {
			flags = append(flags, "multi")
		}
	}
	base := f.Key
	if f.Row > 0 && f.Column > 0 && f.Length > 0 {
		base = fmt.Sprintf("R%d C%d L%d", f.Row, f.Column, f.Length)
	}
	if len(flags) == 0 {
		return base
	}
	return base + " (" + strings.Join(flags, ", ") + ")"
}

func inferAllowedFieldType(meta *chaos.MindMapFieldMetadata, working, tried []string) string {
	base := "text/alphanumeric"
	if meta != nil {
		switch {
		case meta.Numeric:
			base = "numeric-only"
		case meta.Hidden:
			base = "hidden text"
		}
		if meta.Hidden && meta.Numeric {
			base = "hidden numeric-only"
		}
		if meta.MultiLine {
			base += ", multi-line"
		}
	}
	observedKinds := valueKindsUnion(append(append([]string(nil), working...), tried...))
	if len(observedKinds) == 0 {
		return base
	}
	return base + " (observed: " + strings.Join(observedKinds, ", ") + ")"
}

func summarizeObservedValueTypes(working, tried []string) string {
	workingKinds := valueKindsUnion(working)
	triedKinds := valueKindsUnion(tried)
	if len(workingKinds) > 0 {
		return "working: " + strings.Join(workingKinds, ", ")
	}
	if len(triedKinds) > 0 {
		return "tried-only: " + strings.Join(triedKinds, ", ")
	}
	return ""
}

func valueKindsUnion(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, v := range values {
		kind := classifyValueKind(v)
		if kind == "" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

func classifyValueKind(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	isDigits := true
	isLetters := true
	isAlphaNum := true
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			isLetters = false
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			isDigits = false
		default:
			isDigits = false
			isLetters = false
			if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
				isAlphaNum = false
			}
		}
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			isAlphaNum = false
		}
	}
	switch {
	case isDigits:
		return "numeric"
	case isLetters:
		return "alphabetic"
	case isAlphaNum:
		return "alphanumeric"
	default:
		return "mixed/symbols"
	}
}

func formatDestinationList(dests map[string]int, m *chaos.MindMap, screenRefByHash map[string]string, limit int) string {
	if len(dests) == 0 {
		return ""
	}
	type row struct {
		Hash  string
		Count int
	}
	rows := make([]row, 0, len(dests))
	for hash, count := range dests {
		if count <= 0 {
			continue
		}
		rows = append(rows, row{Hash: hash, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Hash < rows[j].Hash
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("%s (%d)", destinationLabelForHashWithRef(r.Hash, m, screenRefByHash), r.Count))
	}
	return strings.Join(parts, ", ")
}

func destinationLabelForHashWithRef(hash string, m *chaos.MindMap, screenRefByHash map[string]string) string {
	base := destinationLabelForHash(hash, m)
	ref := ""
	if screenRefByHash != nil {
		ref = strings.TrimSpace(screenRefByHash[strings.TrimSpace(hash)])
	}
	if ref == "" {
		return base
	}
	return fmt.Sprintf("%s (%s)", ref, base)
}

func destinationLabelForHash(hash string, m *chaos.MindMap) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return "Unknown"
	}
	if m != nil && m.Areas != nil {
		if area := m.Areas[hash]; area != nil {
			label := strings.TrimSpace(area.Label)
			if label != "" {
				return label
			}
		}
	}
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func countAreaValues(area *chaos.MindMapArea) (working, tried int) {
	if area == nil {
		return 0, 0
	}
	for _, values := range area.KnownWorkingValues {
		working += len(values)
	}
	for _, values := range area.KnownTriedValues {
		tried += len(values)
	}
	return working, tried
}

func cleanAndSortStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func joinSampleValues(values []string, max int) string {
	values = cleanAndSortStrings(values)
	if len(values) == 0 {
		return "None"
	}
	if max > 0 && len(values) > max {
		rest := len(values) - max
		values = append(values[:max], fmt.Sprintf("+%d more", rest))
	}
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		if strings.HasPrefix(v, "+") && strings.HasSuffix(v, "more") {
			quoted = append(quoted, v)
			continue
		}
		quoted = append(quoted, `"`+v+`"`)
	}
	return strings.Join(quoted, ", ")
}

func formatTargetHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "Unknown target"
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", host, port)
	}
	return host
}

func formatRunWindow(startedAt, stoppedAt time.Time) string {
	if startedAt.IsZero() && stoppedAt.IsZero() {
		return "Unknown"
	}
	if stoppedAt.IsZero() {
		return fmt.Sprintf("%s to in progress", formatTime(startedAt))
	}
	return fmt.Sprintf("%s to %s", formatTime(startedAt), formatTime(stoppedAt))
}

func formatRunDuration(startedAt, stoppedAt time.Time) string {
	if startedAt.IsZero() {
		return "Unknown"
	}
	end := stoppedAt
	if end.IsZero() {
		end = time.Now()
	}
	if end.Before(startedAt) {
		return "Unknown"
	}
	d := end.Sub(startedAt).Round(time.Second)
	if d < 0 {
		return "Unknown"
	}
	return d.String()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "Unknown"
	}
	return t.UTC().Format(time.RFC3339)
}

func mdText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func sanitizeMarkdownCodeFence(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "```", "'''")
	return strings.TrimRight(s, "\n")
}

func mdCode(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "`", "'")
	return s
}

func mdTableCell(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// A cell is one line of plain text. A line break becomes " / " and a
	// pipe becomes a broken bar, rather than the <br> and \| a full Markdown
	// renderer would take: the report is read first in the app's own viewer,
	// which escapes HTML (so <br> showed up as five literal characters) and
	// splits rows on every pipe, escaped or not (so \| moved the rest of the
	// row one column to the right).
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	kept := parts[:0]
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	s = strings.Join(kept, " / ")
	s = strings.ReplaceAll(s, "|", "¦")
	return s
}

// chaosReportWithheld is what a values column says for a hidden field.
func chaosReportWithheld(n int) string {
	if n == 0 {
		return "None"
	}
	return "Known, withheld (hidden field)"
}

// chaosReportGroupedScreenCount is the number of screens once each group of
// size variants is counted as one.
func chaosReportGroupedScreenCount(screens int, groups []chaosReportVariantGroup) int {
	for _, g := range groups {
		screens -= len(g.Refs) - 1
	}
	if screens < 0 {
		return 0
	}
	return screens
}

// chaosReportEndedLine is the one-line "why it ended" for the summary, empty
// when the reason is not known.
func chaosReportEndedLine(ctx chaosReportContext) string {
	if ctx.InProgress {
		return "Why it ended: it has not — the run was still going when this report was written."
	}
	summary := chaosTerminationSummary(ctx.Reason, ctx.SaturatedNoProgress)
	if summary == "" {
		return ""
	}
	return "Why it ended: " + summary
}

// chaosReportBlockedKeysForScreen lists the keys deliberately not pressed
// from a screen: blocked for the whole run, blocked by a hint for this
// screen, or recognised by the run as an exit key here.
func chaosReportBlockedKeysForScreen(area *chaos.MindMapArea, hint chaos.ScreenHint, ctx chaosReportContext) []string {
	var keys []string
	if ctx.Config != nil {
		keys = append(keys, ctx.Config.BlockedKeys...)
	}
	keys = append(keys, hint.BlockedKeys...)
	if area != nil {
		keys = append(keys, area.AutoBlockedKeys...)
	}
	return normalizedChaosKeys(keys)
}

// writeChaosReportConfiguration writes the "Configuration Used" section: the
// limits the run had and the keys it was and was not allowed to press. A
// report that says a run "stopped at its step limit" without saying what the
// limit was leaves the reader to guess whether to raise it.
func writeChaosReportConfiguration(buf *bytes.Buffer, ctx chaosReportContext) {
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(buf, format, args...)
	}
	write("## Configuration Used\n\n")
	cfg := ctx.Config
	if cfg == nil {
		write("The configuration was not recorded with this run (it was loaded from a saved file).")
		if len(ctx.RunKeys) > 0 {
			write(" The keys it could press are taken to be the ones it is recorded as having pressed: %s.", mdText(strings.Join(ctx.RunKeys, ", ")))
		}
		write("\n\n")
		return
	}
	limit := func(n int, unit string) string {
		if n <= 0 {
			return "no limit"
		}
		return fmt.Sprintf("%d %s", n, unit)
	}
	write("- **Step Limit**: %s\n", limit(cfg.MaxSteps, "attempts"))
	if cfg.TimeBudget > 0 {
		write("- **Time Limit**: %s\n", cfg.TimeBudget.Round(time.Second))
	} else {
		write("- **Time Limit**: no limit\n")
	}
	write("- **Delay Between Steps**: %s\n", cfg.StepDelay.Round(time.Millisecond))
	if cfg.SaturationSteps > 0 {
		write("- **Stops When Nothing New For**: %d steps in a row\n", cfg.SaturationSteps)
	} else {
		write("- **Stops When Nothing New For**: never (saturation check off)\n")
	}
	write("- **Keys It Could Press**: %s\n", mdText(orNone(strings.Join(cfg.CandidateKeys, ", "))))
	write("- **Blocked Keys**: %s\n", mdText(orNone(strings.Join(cfg.BlockedKeys, ", "))))
	if cfg.AutoBlockExitKeys {
		write("- **Exit Keys**: blocked automatically on screens whose key legend names them\n")
	} else {
		write("- **Exit Keys**: not blocked automatically\n")
	}
	write("- **Hints**: %d general, %d for specific screens\n", cfg.HintCount, cfg.ScreenHintCount)
	if cfg.DedupMode != "" {
		write("- **Screen Matching**: %s\n", mdText(cfg.DedupMode))
	}
	if cfg.Seed != 0 {
		write("- **Seed**: %d\n", cfg.Seed)
	}
	if cfg.Continued {
		write("- **Continued From An Earlier Run**: yes — the counts in this report include what that run found\n")
	}
	write("\n")
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "none"
	}
	return s
}

// writeChaosReportBusinessFunctions writes the catalogued business functions,
// when the map has any: what each does, what it needs, and the path it takes
// through the screens.
func writeChaosReportBusinessFunctions(buf *bytes.Buffer, m *chaos.MindMap, screenRefByHash map[string]string) {
	functions := chaos.BusinessFunctionsOf(m)
	if len(functions) == 0 {
		return
	}
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(buf, format, args...)
	}
	screen := func(hash string) string {
		hash = strings.TrimSpace(hash)
		if hash == "" {
			return ""
		}
		return destinationLabelForHashWithRef(hash, m, screenRefByHash)
	}
	write("## Business Functions\n\n")
	write("Named operations catalogued for this application, each a path through the screens above.\n\n")
	for _, fn := range functions {
		write("### %s\n\n", mdText(fn.Name))
		if desc := strings.TrimSpace(fn.Description); desc != "" {
			write("%s\n\n", mdText(desc))
		}
		if entry := screen(fn.EntryScreenHash); entry != "" {
			write("- **Starts On**: %s\n", mdText(entry))
		}
		if len(fn.Parameters) > 0 {
			names := make([]string, 0, len(fn.Parameters))
			for _, p := range fn.Parameters {
				name := strings.TrimSpace(p.Name)
				if name == "" {
					continue
				}
				if p.Required {
					name += " (required)"
				}
				names = append(names, name)
			}
			write("- **Inputs**: %s\n", mdText(orNone(strings.Join(names, ", "))))
		}
		if len(fn.Steps) > 0 {
			write("- **Steps**:\n")
			for i, step := range fn.Steps {
				line := fmt.Sprintf("on %s", orNA(screen(step.ScreenHash)))
				if n := len(step.Inputs); n > 0 {
					line += fmt.Sprintf(", fill %d field%s", n, pluralS(n))
				}
				line += fmt.Sprintf(", press %s", orNA(strings.TrimSpace(step.AIDKey)))
				if to := screen(step.ExpectHash); to != "" {
					line += ", expect " + to
				}
				write("  - %d. %s\n", i+1, mdText(line))
			}
		}
		write("\n")
	}
}

// chaosReportMaxNextSteps bounds the "What To Do Next" list: it is a short
// list of the best next moves, not the full analysis (GET /chaos/insights).
const chaosReportMaxNextSteps = 6

// writeChaosReportNextSteps writes the closing section: what the reason the
// run ended suggests, then the highest-value experiments the insights
// analysis found.
func writeChaosReportNextSteps(buf *bytes.Buffer, m *chaos.MindMap, ctx chaosReportContext) {
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(buf, format, args...)
	}
	write("## What To Do Next\n\n")
	if ended := chaosTerminationSummary(ctx.Reason, ctx.SaturatedNoProgress); ended != "" && !ctx.InProgress {
		write("- %s\n", mdText(ended))
	}
	insights := buildChaosInsightsWith(m, ctx.RunKeys, ctx.ScreenHints)
	suggested, _ := insights["suggestedExperiments"].([]string)
	for i, text := range suggested {
		if i >= chaosReportMaxNextSteps {
			write("- %d more suggestion%s: see the insights for this run.\n", len(suggested)-i, pluralS(len(suggested)-i))
			break
		}
		write("- %s\n", mdText(text))
	}
	write("\n")
}

func orNA(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "N/A"
	}
	return s
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func pluralHave(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}
