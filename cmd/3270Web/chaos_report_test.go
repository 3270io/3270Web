// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/session"
)

// reportFixture is a three-screen application: a menu that Enter takes to a
// detail screen and back again many times, and the same menu seen once more
// at a larger terminal size.
func reportFixture() (*chaos.SavedRun, chaosReportContext) {
	menuFields := map[string]chaos.MindMapFieldMetadata{
		"R20C10L4": {Row: 20, Column: 10, Length: 4},
	}
	mm := &chaos.MindMap{
		Areas: map[string]*chaos.MindMapArea{
			"menu24": {
				Hash:            "menu24",
				Label:           `MAIN "MENU" | A&B <1>`,
				Visits:          30,
				InputFieldCount: 1,
				ScreenWidth:     80,
				ScreenHeight:    24,
				FieldMetadata:   menuFields,
				FieldSemantics: map[string]chaos.BusinessFieldSemantic{
					"R20C10L4": {Name: "Menu option"},
				},
				KnownWorkingValues: map[string][]string{"R20C10L4": {"INQ"}},
				KnownTriedValues:   map[string][]string{"R20C10L4": {"INQ", "A|B"}},
				BusinessPurpose:    "Entry point: choose what to do",
				BusinessNotes:      "Option INQ opens the account inquiry.\nOthers are not explored.",
				AutoBlockedKeys:    []string{"PF(12)"},
				KeyPresses: map[string]*chaos.MindMapKeyPress{
					"Enter": {Presses: 28, Progressions: 25, Destinations: map[string]int{"detail": 25}},
					"PF(2)": {Presses: 4, Progressions: 0},
				},
			},
			"detail": {
				Hash:            "detail",
				Label:           "ACCOUNT DETAIL",
				Visits:          25,
				InputFieldCount: 0,
				ScreenWidth:     80,
				ScreenHeight:    24,
				KeyPresses: map[string]*chaos.MindMapKeyPress{
					"Enter": {Presses: 25, Progressions: 25, Destinations: map[string]int{"menu24": 25}},
				},
			},
			"menu43": {
				Hash:            "menu43",
				Label:           `MAIN "MENU" | A&B <1>`,
				Visits:          1,
				InputFieldCount: 1,
				ScreenWidth:     80,
				ScreenHeight:    43,
				FieldMetadata:   menuFields,
			},
		},
		BusinessFunctions: map[string]*chaos.BusinessFunction{
			"account inquiry": {
				Name:            "Account inquiry",
				Description:     "Look up an account from the menu",
				EntryScreenHash: "menu24",
				Parameters:      []chaos.BusinessParameter{{Name: "account_number", Required: true}},
				Steps: []chaos.BusinessFunctionStep{
					{ScreenHash: "menu24", AIDKey: "Enter", ExpectHash: "detail", Inputs: []chaos.BusinessFunctionInput{{FieldKey: "R20C10L4", Value: "INQ"}}},
				},
			},
		},
	}
	run := &chaos.SavedRun{
		SavedRunMeta: chaos.SavedRunMeta{
			ID:            "report-run",
			StartedAt:     time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
			StoppedAt:     time.Date(2026, 3, 1, 9, 2, 0, 0, time.UTC),
			StepsRun:      60,
			Transitions:   2, // the engine's count: distinct transitions
			UniqueScreens: 3,
			UniqueInputs:  2,
			Label:         "Before the release",
		},
		AIDKeyCounts: map[string]int{"Enter": 53, "PF(2)": 4},
		MindMap:      mm,
	}
	ctx := chaosReportContext{
		Reason: chaos.TerminationReasonMaxSteps,
		Config: &session.ChaosRunConfig{
			MaxSteps:        60,
			TimeBudget:      5 * time.Minute,
			StepDelay:       250 * time.Millisecond,
			SaturationSteps: 15,
			CandidateKeys:   []string{"Enter", "PF(2)", "PF(5)"},
			BlockedKeys:     []string{"PF(3)"},
			HintCount:       2,
			ScreenHintCount: 1,
		},
		RunKeys: []string{"Enter", "PF(2)", "PF(5)"},
		ScreenHints: map[string]chaos.ScreenHint{
			"detail": {BlockedKeys: []string{"PF5"}},
		},
	}
	return run, ctx
}

func buildReportFixture(t *testing.T) string {
	t.Helper()
	run, ctx := reportFixture()
	out, _ := buildChaosDiscoveryReportMarkdown("mainframe.example", 992, run, ctx, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC))
	return string(out)
}

// section returns the text of one "## " section of the report.
func reportSection(t *testing.T, report, heading string) string {
	t.Helper()
	start := strings.Index(report, "## "+heading+"\n")
	if start < 0 {
		t.Fatalf("the report has no %q section:\n%s", heading, report)
	}
	rest := report[start+len(heading)+4:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestChaosReportCountsTransitionsAndPathsSeparately(t *testing.T) {
	report := buildReportFixture(t)
	tldr := reportSection(t, report, "TL;DR")
	// 25 there and 25 back: fifty changes of screen, along two paths. The
	// report used to say "observed 2 screen transitions".
	if !strings.Contains(tldr, "The screen changed 50 times in total, along 2 distinct transitions.") {
		t.Errorf("TL;DR does not give both counts with the right words:\n%s", tldr)
	}
	if strings.Contains(report, "observed 2 screen transition") {
		t.Error("the report still describes the distinct count as the number of transitions observed")
	}
	summary := reportSection(t, report, "Run Summary")
	if !strings.Contains(summary, "**Screen Changes (total)**: 50") || !strings.Contains(summary, "**Distinct Transitions**: 2") {
		t.Errorf("Run Summary:\n%s", summary)
	}
}

func TestChaosReportSaysWhyTheRunEndedAndHowItWasConfigured(t *testing.T) {
	report := buildReportFixture(t)
	if !strings.Contains(reportSection(t, report, "TL;DR"), "Why it ended: It reached its step limit.") {
		t.Errorf("TL;DR does not say why the run ended:\n%s", reportSection(t, report, "TL;DR"))
	}
	cfg := reportSection(t, report, "Configuration Used")
	for _, want := range []string{
		"**Step Limit**: 60 attempts",
		"**Time Limit**: 5m0s",
		"**Delay Between Steps**: 250ms",
		"**Keys It Could Press**: Enter, PF(2), PF(5)",
		"**Blocked Keys**: PF(3)",
		"**Hints**: 2 general, 1 for specific screens",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("Configuration Used is missing %q:\n%s", want, cfg)
		}
	}
	if !strings.Contains(reportSection(t, report, "Run Summary"), "**Run Name**: Before the release") {
		t.Error("the run's label is not in the summary")
	}
}

func TestChaosReportMeasuresUntriedKeysAgainstTheRunsOwnKeys(t *testing.T) {
	report := buildReportFixture(t)
	detailed := reportSection(t, report, "Detailed Screens")

	// The run could press Enter, PF(2) and PF(5). On the menu it pressed the
	// first two, so PF(5) is the frontier there — not PF(1), PF(4) or
	// PF(12), which are in the built-in defaults but which this run could
	// never press.
	menu := detailed[strings.Index(detailed, "### S01."):strings.Index(detailed, "### S02.")]
	if !strings.Contains(menu, "never did from this screen: PF(5).") {
		t.Errorf("menu: want PF(5) alone as untried:\n%s", menu)
	}
	for _, defaultOnly := range []string{"PF(1)", "PF(4)"} {
		if strings.Contains(menu, defaultOnly) {
			t.Errorf("menu: %s is reported, but it is a built-in default this run did not have", defaultOnly)
		}
	}
	if !strings.Contains(menu, "blocked): PF(12), PF(3).") {
		t.Errorf("menu: want the keys blocked there listed (the run's and the screen's own):\n%s", menu)
	}

	// On the detail screen PF(5) is blocked by a hint, so only PF(2) is left.
	detail := detailed[strings.Index(detailed, "### S02."):strings.Index(detailed, "### S03.")]
	if !strings.Contains(detail, "never did from this screen: PF(2).") {
		t.Errorf("detail: want PF(2) alone as untried (PF(5) is blocked by a hint):\n%s", detail)
	}
}

func TestChaosReportWithoutARecordedConfigurationSaysSo(t *testing.T) {
	run, ctx := reportFixture()
	ctx.Config = nil
	ctx.RunKeys = chaosKeysPressedInMindMap(run.MindMap)
	out, _ := buildChaosDiscoveryReportMarkdown("h", 23, run, ctx, time.Now())
	cfg := reportSection(t, string(out), "Configuration Used")
	if !strings.Contains(cfg, "not recorded with this run") {
		t.Errorf("want the report to say the configuration is unknown rather than print defaults:\n%s", cfg)
	}
	if !strings.Contains(cfg, "Enter, PF(2)") {
		t.Errorf("want the keys the run is known to have pressed:\n%s", cfg)
	}
}

func TestChaosReportEscapesMermaidLabels(t *testing.T) {
	report := buildReportFixture(t)
	flow := reportSection(t, report, "Discovery Flow Map")
	start := strings.Index(flow, "```mermaid\n")
	end := strings.Index(flow[start+11:], "```")
	if start < 0 || end < 0 {
		t.Fatalf("no mermaid block:\n%s", flow)
	}
	diagram := flow[start+11 : start+11+end]

	if strings.Contains(diagram, `\"`) {
		t.Errorf("a backslash-escaped quote in a Mermaid label; Mermaid reads it as the end of the label:\n%s", diagram)
	}
	want := `S01["S01: MAIN #quot;MENU#quot; #124; A#amp;B #lt;1#gt;"]`
	if !strings.Contains(diagram, want) {
		t.Errorf("want the node label escaped as %s, got:\n%s", want, diagram)
	}
	for _, line := range strings.Split(strings.TrimSpace(diagram), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "S") || !strings.Contains(line, `["`) {
			continue
		}
		// Exactly the two quotes that delimit the label.
		if n := strings.Count(line, `"`); n != 2 {
			t.Errorf("node line has %d double quotes, want 2: %s", n, line)
		}
	}
	if !strings.Contains(diagram, `S01 -->|"Enter (25)"| S02`) {
		t.Errorf("want quoted edge labels:\n%s", diagram)
	}
}

func TestMermaidLabel(t *testing.T) {
	cases := map[string]string{
		`plain`:            `plain`,
		`say "hi"`:         `say #quot;hi#quot;`,
		`#1 choice`:        `#35;1 choice`,
		"two\nlines":       `two lines`,
		"back\\slash":      `back#92;slash`,
		"tick`s":           `tick#96;s`,
		"a|b":              `a#124;b`,
		"ctrl\x07char":     `ctrlchar`,
		`<script>&"x"</s>`: `#lt;script#gt;#amp;#quot;x#quot;#lt;/s#gt;`,
	}
	for in, want := range cases {
		if got := mermaidLabel(in); got != want {
			t.Errorf("mermaidLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChaosReportTableCellsUseNothingTheViewerPrintsLiterally(t *testing.T) {
	if got := mdTableCell("first\r\nsecond\n\nthird | fourth"); got != "first / second / third ¦ fourth" {
		t.Errorf("mdTableCell = %q", got)
	}
	report := buildReportFixture(t)
	if strings.Contains(report, "<br>") {
		t.Error("the report contains <br>, which the in-app viewer shows as text")
	}
	if strings.Contains(report, `\|`) {
		t.Error(`the report contains \|, which the in-app viewer splits a row on`)
	}
	// Every row of every table has the same number of cells as its header.
	lines := strings.Split(report, "\n")
	inFence := false
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(lines[i], "|") || i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "|---") {
			continue
		}
		want := strings.Count(lines[i], "|")
		for j := i + 2; j < len(lines) && strings.HasPrefix(lines[j], "|"); j++ {
			if got := strings.Count(lines[j], "|"); got != want {
				t.Errorf("table row has %d pipes, its header has %d:\n%s\n%s", got, want, lines[i], lines[j])
			}
		}
	}
}

func TestChaosReportShowsBusinessKnowledge(t *testing.T) {
	report := buildReportFixture(t)
	detailed := reportSection(t, report, "Detailed Screens")
	for _, want := range []string{
		"**Business Purpose**: Entry point: choose what to do",
		"**Business Notes**: Option INQ opens the account inquiry. Others are not explored.",
		"| R20 C10 L4 | Menu option |",
	} {
		if !strings.Contains(detailed, want) {
			t.Errorf("Detailed Screens is missing %q", want)
		}
	}
	functions := reportSection(t, report, "Business Functions")
	for _, want := range []string{
		"### Account inquiry",
		"Look up an account from the menu",
		"**Inputs**: account_number (required)",
		"press Enter, expect S02 (ACCOUNT DETAIL)",
	} {
		if !strings.Contains(functions, want) {
			t.Errorf("Business Functions is missing %q:\n%s", want, functions)
		}
	}

	run, ctx := reportFixture()
	run.MindMap.BusinessFunctions = nil
	out, _ := buildChaosDiscoveryReportMarkdown("h", 23, run, ctx, time.Now())
	if strings.Contains(string(out), "## Business Functions") {
		t.Error("a Business Functions section was written for a run with none")
	}
}

func TestChaosReportEndsWithWhatToDoNext(t *testing.T) {
	report := buildReportFixture(t)
	next := reportSection(t, report, "What To Do Next")
	if !strings.Contains(next, "continue exploring to give it more steps") {
		t.Errorf("the first suggestion does not follow from why the run ended:\n%s", next)
	}
	// From the insights: the menu's untried key.
	if !strings.Contains(next, "PF(5)") {
		t.Errorf("the insights' suggested experiments are not in the section:\n%s", next)
	}
	if n := strings.Count(next, "\n- "); n > chaosReportMaxNextSteps+2 {
		t.Errorf("%d suggestions; the section is meant to be short", n)
	}
	if strings.Index(report, "## What To Do Next") > strings.Index(report, "## Notes") {
		t.Error("What To Do Next should come before the closing notes")
	}
}

func TestChaosReportGroupsSizeVariantsOfOneScreen(t *testing.T) {
	report := buildReportFixture(t)
	inventory := reportSection(t, report, "Screen Inventory")
	if !strings.Contains(inventory, "### Size Variants") {
		t.Fatalf("no size-variant group in the inventory:\n%s", inventory)
	}
	if !strings.Contains(inventory, "`S01` (24x80), `S03` (43x80)") {
		t.Errorf("want the two menu sizes grouped:\n%s", inventory)
	}
	if !strings.Contains(reportSection(t, report, "TL;DR"), "counting those once leaves 2") {
		t.Errorf("TL;DR does not give the count after grouping:\n%s", reportSection(t, report, "TL;DR"))
	}
	detailed := reportSection(t, report, "Detailed Screens")
	variant := detailed[strings.Index(detailed, "### S03."):]
	if !strings.Contains(variant, "**Size Variant Of**: `S01`") {
		t.Errorf("the variant's own section does not point at the screen it is a variant of:\n%s", variant)
	}

	// Same title and fields at the same size is two screens, not a variant.
	run, ctx := reportFixture()
	run.MindMap.Areas["menu43"].ScreenHeight = 24
	out, _ := buildChaosDiscoveryReportMarkdown("h", 23, run, ctx, time.Now())
	if strings.Contains(string(out), "### Size Variants") {
		t.Error("two screens of the same size were grouped as size variants")
	}
}

func TestChaosTerminationSummaryCoversEveryReason(t *testing.T) {
	for _, reason := range []string{
		chaos.TerminationReasonMaxSteps,
		chaos.TerminationReasonTimeBudget,
		chaos.TerminationReasonSaturated,
		chaos.TerminationReasonStopped,
		chaos.TerminationReasonError,
		chaos.TerminationReasonBlocked,
	} {
		summary := chaosTerminationSummary(reason, false)
		if summary == "" || strings.Contains(summary, "_") || strings.HasPrefix(summary, "It ended with reason") {
			t.Errorf("%s: want a sentence in plain English, not the code: %q", reason, summary)
		}
	}
	if got := chaosTerminationSummary(chaos.TerminationReasonSaturated, true); !strings.Contains(got, "first screen") {
		t.Errorf("saturated without progress: %q", got)
	}
	if chaosTerminationSummary("", false) != "" {
		t.Error("an unknown reason should produce no sentence")
	}
	if got := chaosTerminationSummary("something_new", false); !strings.Contains(got, "something_new") {
		t.Errorf("an unrecognised reason should be passed through, got %q", got)
	}
}

func TestChaosInsightsMeasureUntriedAgainstTheGivenKeysAndHints(t *testing.T) {
	run, ctx := reportFixture()
	out := buildChaosInsightsWith(run.MindMap, ctx.RunKeys, ctx.ScreenHints)
	perScreen, _ := out["perScreen"].([]gin.H)
	untried := map[string][]string{}
	for _, entry := range perScreen {
		hash, _ := entry["hash"].(string)
		keys, _ := entry["untriedKeys"].([]string)
		untried[hash] = keys
	}
	if got := strings.Join(untried["menu24"], ","); got != "PF(5)" {
		t.Errorf("menu untried = %q, want PF(5)", got)
	}
	if got := strings.Join(untried["detail"], ","); got != "PF(2)" {
		t.Errorf("detail untried = %q, want PF(2) — PF(5) is blocked there by a hint", got)
	}
}
