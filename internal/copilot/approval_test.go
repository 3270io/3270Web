// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import "testing"

func TestCanonicalKeyNameFoldsTheSpellings(t *testing.T) {
	cases := map[string]string{
		"pf3": "PF(3)", "PF3": "PF(3)", "PF(3)": "PF(3)", "pf( 3 )": "PF(3)",
		"PF 3": "PF(3)", "PF03": "PF(3)",
		"clear": "Clear", "CLEAR": "Clear", " Clear ": "Clear",
		"enter": "Enter", "pa1": "PA(1)", "pf13": "PF(13)",
		"": "",
	}
	for in, want := range cases {
		if got := CanonicalKeyName(in); got != want {
			t.Errorf("CanonicalKeyName(%q) = %q, want %q", in, got, want)
		}
	}

	// Not the key parser: a nonsense key folds to something rather than being
	// rejected here, because the server's own check is what rejects it and
	// two gates disagreeing is worse than one.
	if got := CanonicalKeyName("PFX"); got == "" {
		t.Error("an unrecognised key should still fold to a comparable string")
	}
}

// TestRequiresApprovalCatchesEverySpelling is the reason folding exists. The
// regex it replaced matched "pf3" and "pf(3)" and would have missed "PF 3" —
// which is exactly how a model that has read a screen legend writes it.
func TestRequiresApprovalCatchesEverySpelling(t *testing.T) {
	for _, spelling := range []string{"PF3", "pf3", "PF(3)", "pf( 3 )", "PF 3", "PF03", "clear", "CLEAR"} {
		if got := RequiresApproval("send_key", map[string]any{"key": spelling}); got == "" {
			t.Errorf("send_key %q should require approval", spelling)
		} else if got != spelling {
			t.Errorf("the reason should quote the caller's spelling %q, got %q", spelling, got)
		}
	}

	for _, safe := range []string{"Enter", "PF1", "pf12", "Tab", "PA1", "", "  "} {
		if got := RequiresApproval("send_key", map[string]any{"key": safe}); got != "" {
			t.Errorf("send_key %q should not require approval, got %q", safe, got)
		}
	}

	// Only send_key is classified. Everything else is either read-only or
	// already gated by a tier.
	if got := RequiresApproval("write_field", map[string]any{"key": "PF3"}); got != "" {
		t.Errorf("write_field should not be classified, got %q", got)
	}
	// A shape it cannot read is not dangerous — the server rejects it anyway.
	if got := RequiresApproval("send_key", map[string]any{"key": 3}); got != "" {
		t.Errorf("a non-string key should not be classified, got %q", got)
	}
}

func TestDangerousSendKeysIsACopy(t *testing.T) {
	keys := DangerousSendKeys()
	if len(keys) == 0 {
		t.Fatal("the list is empty")
	}
	keys[0] = "Enter"
	if DangerousSendKeys()[0] == "Enter" {
		t.Error("a caller mutated the policy through the returned slice")
	}
}
