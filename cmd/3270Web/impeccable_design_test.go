// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Design-judgement fixes from IMPECCABLE_AUDIT.md. These pin decisions that
// are easy to undo silently.

// Finding 3 follow-up: promoting the dialog titles to h2 left the nested
// confirmation headings at h4, which skipped a level from the h2 above them.
func TestConnectConfirmHeadingsDoNotSkipALevel(t *testing.T) {
	src := templateSource(t, "connect.html")
	for _, id := range []string{"settings-restart-title", "settings-discard-title"} {
		if !strings.Contains(src, `<h3 id="`+id+`"`) {
			t.Errorf("connect.html: %s is not an h3 under the settings h2 (finding 3)", id)
		}
		if strings.Contains(src, `<h4 id="`+id+`"`) {
			t.Errorf("connect.html: %s is still an h4, skipping a heading level (finding 3)", id)
		}
	}
}

func cssRule(t *testing.T, src, marker string) string {
	t.Helper()
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("rule %q is gone", marker)
	}
	end := strings.Index(src[start:], "}")
	if end < 0 {
		t.Fatalf("unterminated rule %q", marker)
	}
	return src[start : start+end]
}

// Finding 4: 11px stays for labels and chips, but helper and status prose is
// read as a sentence, so it uses the 0.75rem step.
func TestCopilotProseIsNotElevenPixels(t *testing.T) {
	src := staticSource(t, "style.css")
	for _, selector := range []string{
		".copilot-panel-hint",
		".copilot-statusline",
		".copilot-statusline-action",
		".copilot-context",
		".copilot-chip-note",
	} {
		block := cssRule(t, src, selector+" {")
		if !strings.Contains(block, "font-size: 0.75rem") {
			t.Errorf("%s: prose is not 0.75rem (finding 4)", selector)
		}
		if strings.Contains(block, "font-size: 11px") {
			t.Errorf("%s: prose is still 11px (finding 4)", selector)
		}
	}
}

// Finding 6: the toast entrance uses the shared ease-out curve rather than an
// overshoot, matching the rest of the motion system.
func TestNotificationUsesSharedEaseOut(t *testing.T) {
	block := cssRule(t, staticSource(t, "style.css"), ".h3270-notification {")
	if !strings.Contains(block, "var(--ease-out") {
		t.Error(".h3270-notification: entrance does not use --ease-out (finding 6)")
	}
	if strings.Contains(block, "cubic-bezier(0.34, 1.42, 0.64, 1)") {
		t.Error(".h3270-notification: still has the overshoot curve (finding 6)")
	}
}

// Finding 7 open item: the radius scale now lives in the sidecar beside the
// type scale, and the tooling reads it as named radius roles.
func TestDesignSidecarRecordsRadiusScale(t *testing.T) {
	var doc struct {
		Extensions struct {
			RoundedMeta map[string]json.RawMessage `json:"roundedMeta"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal([]byte(rootFile(t, ".impeccable", "design.json")), &doc); err != nil {
		t.Fatalf(".impeccable/design.json is not valid JSON: %v", err)
	}
	for _, key := range []string{"xs", "sm", "md", "lg", "xl", "pill", "scale"} {
		if _, ok := doc.Extensions.RoundedMeta[key]; !ok {
			t.Errorf(".impeccable/design.json: roundedMeta has no %q (finding 7)", key)
		}
	}
	var scale []string
	if err := json.Unmarshal(doc.Extensions.RoundedMeta["scale"], &scale); err != nil {
		t.Fatalf("roundedMeta.scale is not a string list: %v", err)
	}
	for _, want := range []string{"2px", "8px", "10px", "14px", "999px"} {
		found := false
		for _, got := range scale {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("roundedMeta.scale is missing %q", want)
		}
	}
}
