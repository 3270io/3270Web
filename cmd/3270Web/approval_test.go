// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/copilot"
)

// TestDangerousKeysAgreeWithChaosDefaults ties the two policies together.
//
// Auto Mode pressing keys unattended and chaos exploration pressing keys
// unattended are the same risk, and they had two lists. If someone decides
// PF3 is fine on their target and adds it to the exploration defaults, that
// is a decision about this deployment — but making it in one place and not
// the other leaves the product saying two things at once.
func TestDangerousKeysAgreeWithChaosDefaults(t *testing.T) {
	weights := chaos.DefaultConfig().AIDKeyWeights

	for _, key := range copilot.DangerousSendKeys() {
		for weighted := range weights {
			if copilot.CanonicalKeyName(weighted) == copilot.CanonicalKeyName(key) {
				t.Errorf("%s needs human approval in the chat panel but is in chaos's default key weights; "+
					"the two policies have diverged", key)
			}
		}
	}

	// And the list is not empty, because an empty one silently turns the
	// whole rule off.
	if len(copilot.DangerousSendKeys()) == 0 {
		t.Error("the dangerous-key list is empty, which disables approval entirely")
	}
}

// TestBrowserReadsTheServerDangerousKeys: the panel used to carry its own
// regexes. It must now take the list from the server, or the rule again
// exists only where the browser runs the tool loop.
func TestBrowserReadsTheServerDangerousKeys(t *testing.T) {
	src, err := os.ReadFile("../../web/static/copilot-panel.js")
	if err != nil {
		t.Fatalf("read the chat panel: %v", err)
	}
	panel := string(src)

	if !strings.Contains(panel, "data.dangerous_send_keys") {
		t.Error("the panel does not read dangerous_send_keys from /api/ai/tools")
	}

	// A literal key list in the panel is the thing that drifted. Anything
	// that looks like one fails here rather than in a conversation.
	literal := regexp.MustCompile(`(?i)/\^?pf\\?\(`)
	if literal.MatchString(panel) {
		t.Error("the panel has a hard-coded PF-key pattern again; the list belongs to the server")
	}

	// Missing list must mean "ask", not "run".
	if !strings.Contains(panel, "fail towards asking") {
		t.Error("the panel should require approval when the server sent no list, not skip the check")
	}
}

// TestSendKeyDescriptionNamesTheApprovalKeys — a model that finds out a key
// needs approval by having its call stall has already written a plan around
// pressing it.
func TestSendKeyDescriptionNamesTheApprovalKeys(t *testing.T) {
	var description string
	for _, tool := range copilot.DefaultTools() {
		if tool.Function.Name == "send_key" {
			description = tool.Function.Description
		}
	}
	if description == "" {
		t.Fatal("send_key is not in the catalogue")
	}
	for _, key := range copilot.DangerousSendKeys() {
		spelled := strings.NewReplacer("(", "", ")", "").Replace(key)
		if !strings.Contains(description, spelled) {
			t.Errorf("send_key's description does not mention %s, which needs approval", spelled)
		}
	}
}
