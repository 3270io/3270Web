// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
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

		// Every other key normalizeKey recognises must also produce a step —
		// recordActionKey treats a nil return as "nothing happened", so a key
		// missing here is a keypress silently dropped from a recording.
		{"PA1", "PressPA1", false},
		{"pa1", "PressPA1", false},
		{"PA(2)", "PressPA2", false},
		{"PA3", "PressPA3", false},
		{"BackTab", "PressBackTab", false},
		{"Clear", "PressClear", false},
		{"Reset", "PressReset", false},
		{"EraseEOF", "PressEraseEOF", false},
		{"EraseInput", "PressEraseInput", false},
		{"Dup", "PressDup", false},
		{"FieldMark", "PressFieldMark", false},
		{"SysReq", "PressSysReq", false},
		{"Attn", "PressAttn", false},
		{"Newline", "PressNewline", false},
		{"BackSpace", "PressBackspace", false},
		{"Delete", "PressDelete", false},
		{"Insert", "PressInsert", false},
		{"Home", "PressHome", false},
		{"Up", "PressUp", false},
		{"Down", "PressDown", false},
		{"Left", "PressLeft", false},
		{"Right", "PressRight", false},

		// Invalid inputs
		{"", "", true},
		{"   ", "", true},
		{"Unknown", "", true},
		{"PF0", "", true},
		{"PF25", "", true},
		{"PA4", "", true},
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

// TestRecordActionKey_NonPFKeysAreRecorded is a regression test for a
// recording that silently lost a step: recordActionKey used to treat any
// key workflowStepForKey did not recognise as nothing having happened,
// which for PA1, Clear, and the rest of the keypad was true of every key
// except Enter, Tab and PF(n). The keypress still reached the host — only
// the recording lost track of it, with no error anywhere.
func TestRecordActionKey_NonPFKeysAreRecorded(t *testing.T) {
	mockHost, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("mock host: %v", err)
	}
	mgr := session.NewManager()
	sess := mgr.CreateSession(mockHost)
	withSessionLock(sess, func() {
		sess.Recording = &session.WorkflowRecording{Active: true}
	})

	for _, key := range []string{"PA(1)", "Clear", "SysReq", "Attn", "BackTab", "EraseEOF"} {
		recordActionKey(sess, key)
	}

	var steps []session.WorkflowStep
	withSessionLock(sess, func() {
		steps = sess.Recording.Steps
	})
	want := []string{"PressPA1", "PressClear", "PressSysReq", "PressAttn", "PressBackTab", "PressEraseEOF"}
	if len(steps) != len(want) {
		t.Fatalf("recorded %d steps, want %d: %+v", len(steps), len(want), steps)
	}
	for i, w := range want {
		if steps[i].Type != w {
			t.Errorf("step %d = %q, want %q", i, steps[i].Type, w)
		}
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
