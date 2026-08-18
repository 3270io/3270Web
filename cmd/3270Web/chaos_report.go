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
	if !app.chaosEngines.isRemoved(s.ID) {
		if eng, ok := app.chaosEngines.get(s.ID); ok {
			st := eng.Status()
			run = eng.Snapshot(strings.TrimSpace(st.LoadedRunID))
		} else if loaded, ok := app.chaosEngines.getLoadedRun(s.ID); ok {
			run = loaded
		} else if loaded := app.loadSessionChaosRunFromDisk(s); loaded != nil {
			app.chaosEngines.setLoadedRun(s.ID, loaded)
			run = loaded
		}
	}

	data, filename := buildChaosDiscoveryReportMarkdown(targetHost, targetPort, run, screenHints, time.Now())
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", data)
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
}

type chaosReportScreenRow struct {
	Area             *chaos.MindMapArea
	RefID            string
	Hash             string
	Label            string
	WorkingKeyCount  int
	ProgressFieldCnt int
}

var chaosMindMapFieldKeyRe = regexp.MustCompile(`^R(\d+)C(\d+)L(\d+)$`)

func buildChaosDiscoveryReportMarkdown(targetHost string, targetPort int, run *chaos.SavedRun, screenHints map[string]chaos.ScreenHint, generatedAt time.Time) ([]byte, string) {
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
		write("- Run attempts: **%d**, transitions: **%d**.\n", run.StepsRun, run.Transitions)
		write("\n")
		write("## Run Summary\n\n")
		write("- **Target**: %s\n", mdText(formatTargetHostPort(targetHost, targetPort)))
		write("- **Run ID**: %s\n", mdText(orNA(titleRunID)))
		write("- **Run Window**: %s\n", mdText(formatRunWindow(run.StartedAt, run.StoppedAt)))
		write("- **Duration**: %s\n", mdText(formatRunDuration(run.StartedAt, run.StoppedAt)))
		write("- **Attempts**: %d\n", run.StepsRun)
		write("- **Transitions**: %d\n", run.Transitions)
		write("\n")
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

	write("## TL;DR\n\n")
	for _, line := range buildChaosDiscoveryTLDRLines(targetHost, targetPort, run, mindMap, screenRows) {
		write("- %s\n", mdText(line))
	}
	write("\n")

	write("## Run Summary\n\n")
	write("- **Target**: %s\n", mdText(formatTargetHostPort(targetHost, targetPort)))
	write("- **Run ID**: %s\n", mdText(orNA(titleRunID)))
	write("- **Run Window**: %s\n", mdText(formatRunWindow(run.StartedAt, run.StoppedAt)))
	write("- **Duration**: %s\n", mdText(formatRunDuration(run.StartedAt, run.StoppedAt)))
	write("- **Attempts**: %d\n", run.StepsRun)
	write("- **Transitions**: %d\n", run.Transitions)
	write("- **Unique Screens**: %d\n", run.UniqueScreens)
	write("- **Unique Input Values**: %d\n", run.UniqueInputs)
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

	write("## Global Function Key Activity\n\n")
	write("%s\n\n", buildChaosGlobalKeyTable(run.AIDKeyCounts))

	write("## Detailed Screens\n\n")
	write("Each screen section includes one captured example. The same unique screen may appear with different data values during other visits.\n\n")
	for i, sr := range screenRows {
		writeChaosScreenSection(&buf, i+1, sr, mindMap, screenHints[strings.TrimSpace(sr.Hash)], screenRefByHash)
	}

	write("## Notes\n\n")
	write("- This report is based on observed chaos exploration attempts and is **not** an exhaustive validation of all host behavior.\n")
	write("- The Discovery Flow map references screen IDs (for example `S01`) used in the Screen Inventory and Detailed Screens sections.\n")
	write("- Captured screen previews are examples from one observed visit and may differ across other visits based on data entered.\n")
	write("- \"Confirmed working data\" means a successful field write observed during a progression from the screen (screen transition occurred).\n")
	write("- \"Allowed data type\" combines host field attributes (for example numeric-only) with observed working/tried value patterns.\n")

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

func buildChaosDiscoveryTLDRLines(targetHost string, targetPort int, run *chaos.SavedRun, m *chaos.MindMap, screenRows []chaosReportScreenRow) []string {
	lines := make([]string, 0, 6)
	lines = append(lines, fmt.Sprintf(
		"Chaos explored %s for %d attempt%s and observed %d screen transition%s.",
		formatTargetHostPort(targetHost, targetPort),
		run.StepsRun,
		pluralS(run.StepsRun),
		run.Transitions,
		pluralS(run.Transitions),
	))
	lines = append(lines, fmt.Sprintf(
		"%d unique screen%s and %d unique input value%s were captured.",
		run.UniqueScreens,
		pluralS(run.UniqueScreens),
		run.UniqueInputs,
		pluralS(run.UniqueInputs),
	))
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
		candidates := chaos.CandidateKeys(chaos.DefaultConfig().AIDKeyWeights)
		frontierScreens := 0
		for _, area := range m.Areas {
			if area == nil {
				continue
			}
			if len(chaos.UntriedCandidateKeys(area, candidates)) > 0 {
				frontierScreens++
			}
		}
		if frontierScreens > 0 {
			lines = append(lines, fmt.Sprintf(
				"%d screen%s still %s untried candidate keys — the exploration frontier is not exhausted.",
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
		b.WriteString(fmt.Sprintf("  %s[%q]\n", mermaidNodeID(sr.RefID), label))
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
			"  %s -->|%s| %s\n",
			mermaidNodeID(fromRef),
			truncateForReportMermaid(chaosMapEdgeKeySummary(e.Keys, e.Count), 36),
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

func writeChaosScreenSection(buf *bytes.Buffer, _ int, sr chaosReportScreenRow, m *chaos.MindMap, hint chaos.ScreenHint, screenRefByHash map[string]string) {
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

	if untried := chaos.UntriedCandidateKeys(area, chaos.CandidateKeys(chaos.DefaultConfig().AIDKeyWeights)); len(untried) > 0 {
		write("#### Untried Keys (Exploration Frontier)\n\n")
		write("These candidate keys have never been pressed from this screen: %s. A resumed run steers toward them; they are also worth trying manually.\n\n", mdText(strings.Join(untried, ", ")))
	}

	write("#### Input Field Discovery\n\n")
	fields := buildChaosFieldRows(area)
	if len(fields) == 0 {
		write("No input field metadata or field discovery values were recorded for this screen.\n\n")
	} else {
		write("| Field | Allowed Data Type | Observed Working Types | Writes | Successes | Progressions | Confirmed Working Data | Tried Data (sample) |\n")
		write("|---|---|---|---:|---:|---:|---|---|\n")
		for _, f := range fields {
			writes, successes, progressions := 0, 0, 0
			if f.Discovery != nil {
				writes = f.Discovery.Writes
				successes = f.Discovery.WriteSuccesses
				progressions = f.Discovery.Progressions
			}
			write(
				"| %s | %s | %s | %d | %d | %d | %s | %s |\n",
				mdTableCell(formatFieldLabel(f)),
				mdTableCell(f.AllowedType),
				mdTableCell(orNA(strings.TrimSpace(f.ObservedTypes))),
				writes,
				successes,
				progressions,
				mdTableCell(joinSampleValues(f.KnownWorking, 5)),
				mdTableCell(joinSampleValues(f.KnownTried, 5)),
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
		row.AllowedType = inferAllowedFieldType(row.Metadata, row.KnownWorking, row.KnownTried)
		row.ObservedTypes = summarizeObservedValueTypes(row.KnownWorking, row.KnownTried)
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
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "<br>")
	s = strings.ReplaceAll(s, "|", "\\|")
	return s
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
