// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"testing"
)

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		input      string
		expected   string
		expectedOK bool
	}{
		// Basic keys
		{"", "", false},
		{"   ", "", false},
		{"Enter", "Enter", true},
		{"enter", "Enter", true},
		{"Tab", "Tab", true},
		{"tab", "Tab", true},

		// Command injection prevention: rejected outright, not silently
		// reinterpreted as Enter.
		{"key;rm -rf /", "", false},
		{"key\n", "", false},
		{"key\r", "", false},
		{"key\t", "", false},

		// PF keys
		{"PF1", "PF(1)", true},
		{"pf1", "PF(1)", true},
		{"PF(1)", "PF(1)", true},
		{"PF12", "PF(12)", true},
		{"PF24", "PF(24)", true},
		{"F1", "PF(1)", true},
		{"f1", "PF(1)", true},

		// PA keys
		{"PA1", "PA(1)", true},
		{"pa1", "PA(1)", true},
		{"PA(1)", "PA(1)", true},
		{"PA3", "PA(3)", true},

		// Named keys
		{"BackTab", "BackTab", true},
		{"Clear", "Clear", true},
		{"Reset", "Reset", true},
		{"EraseEOF", "EraseEOF", true},
		{"erase_eof", "EraseEOF", true},
		{"EraseInput", "EraseInput", true},
		{"Dup", "Dup", true},
		{"FieldMark", "FieldMark", true},
		{"SysReq", "SysReq", true},
		{"Attn", "Attn", true},
		{"Newline", "Newline", true},
		{"BackSpace", "BackSpace", true},
		{"Delete", "Delete", true},
		{"Insert", "Insert", true},
		{"Home", "Home", true},
		{"Up", "Up", true},
		{"Down", "Down", true},
		{"Left", "Left", true},
		{"Right", "Right", true},

		// Invalid/Unknown keys: rejected, not silently defaulted to Enter —
		// a caller (Copilot's send_key tool, the REST API, the manual
		// submit form) must see this as a failure, since silently pressing
		// Enter for an unrecognized key can submit the current screen when
		// that was never the caller's intent.
		{"UnknownKey", "", false},
		{"PF0", "", false},
		{"PF25", "", false},
		{"PA4", "", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input=%q", tt.input), func(t *testing.T) {
			got, ok := normalizeKey(tt.input)
			if got != tt.expected || ok != tt.expectedOK {
				t.Errorf("normalizeKey(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.expected, tt.expectedOK)
			}
		})
	}
}

func TestWorkflowStepForKey(t *testing.T) {
	tests := []struct {
		input    string
		wantType string
		wantNil  bool
	}{
		{"Enter", "PressEnter", false},
		{"enter", "PressEnter", false},
		{"Tab", "PressTab", false},
		{"PF1", "PressPF1", false},
		{"pf1", "PressPF1", false},
		{"PF(1)", "PressPF1", false},
		{"PF24", "PressPF24", false},

		// Invalid inputs
		{"", "", true},
		{"   ", "", true},
		{"Unknown", "", true},
		{"PF0", "", true},
		{"PF25", "", true},
		{"PA1", "", true}, // Currently workflowStepForKey only handles Enter, Tab, PF
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input=%q", tt.input), func(t *testing.T) {
			step := workflowStepForKey(tt.input)
			if tt.wantNil {
				if step != nil {
					t.Errorf("workflowStepForKey(%q) = %v, want nil", tt.input, step)
				}
				return
			}
			if step == nil {
				t.Fatalf("workflowStepForKey(%q) returned nil, want type %q", tt.input, tt.wantType)
			}
			if step.Type != tt.wantType {
				t.Errorf("workflowStepForKey(%q).Type = %q, want %q", tt.input, step.Type, tt.wantType)
			}
		})
	}
}

func TestNormalizeKey_SecurityLogging(t *testing.T) {
	// This test just ensures no panic when logging, and that a suspicious
	// key is rejected outright rather than silently reinterpreted.
	input := "key;injection"
	got, ok := normalizeKey(input)
	if ok || got != "" {
		t.Errorf("normalizeKey(%q) = (%q, %v), want (\"\", false)", input, got, ok)
	}
}
