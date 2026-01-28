package main

import (
	"fmt"
	"testing"
)

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Basic keys
		{"", "Enter"},
		{"   ", "Enter"},
		{"Enter", "Enter"},
		{"enter", "Enter"},
		{"Tab", "Tab"},
		{"tab", "Tab"},

		// Command injection prevention
		{"key;rm -rf /", "Enter"},
		{"key\n", "Enter"},
		{"key\r", "Enter"},
		{"key\t", "Enter"},

		// PF keys
		{"PF1", "PF(1)"},
		{"pf1", "PF(1)"},
		{"PF(1)", "PF(1)"},
		{"PF12", "PF(12)"},
		{"PF24", "PF(24)"},
		{"F1", "PF(1)"},
		{"f1", "PF(1)"},

		// PA keys
		{"PA1", "PA(1)"},
		{"pa1", "PA(1)"},
		{"PA(1)", "PA(1)"},
		{"PA3", "PA(3)"},

		// Named keys
		{"BackTab", "BackTab"},
		{"Clear", "Clear"},
		{"Reset", "Reset"},
		{"EraseEOF", "EraseEOF"},
		{"erase_eof", "EraseEOF"},
		{"EraseInput", "EraseInput"},
		{"Dup", "Dup"},
		{"FieldMark", "FieldMark"},
		{"SysReq", "SysReq"},
		{"Attn", "Attn"},
		{"Newline", "Newline"},
		{"BackSpace", "BackSpace"},
		{"Delete", "Delete"},
		{"Insert", "Insert"},
		{"Home", "Home"},
		{"Up", "Up"},
		{"Down", "Down"},
		{"Left", "Left"},
		{"Right", "Right"},

		// Invalid/Unknown keys
		{"UnknownKey", "Enter"},
		{"PF0", "Enter"},
		{"PF25", "Enter"},
		{"PA4", "Enter"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input=%q", tt.input), func(t *testing.T) {
			got := normalizeKey(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeKey(%q) = %q, want %q", tt.input, got, tt.expected)
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
	// This test just ensures no panic when logging
	input := "key;injection"
	got := normalizeKey(input)
	if got != "Enter" {
		t.Errorf("normalizeKey(%q) = %q, want 'Enter'", input, got)
	}
}
