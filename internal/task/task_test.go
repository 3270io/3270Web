// SPDX-License-Identifier: AGPL-3.0-or-later

package task

import (
	"strings"
	"testing"
)

// sampleTask is a two-screen balance enquiry: type an account number on a
// menu, press Enter, read the balance.
func sampleTask() Task {
	return Task{
		Name:        "Account balance enquiry",
		Description: "Retrieves the current cleared balance for a customer account.",
		Parameters: []Parameter{{
			Name:      "account_number",
			Label:     "Account number",
			Required:  true,
			MaxLength: 8,
			Pattern:   `\d{8}`,
		}},
		Steps: []Step{{
			Description: "Enter the account number",
			Expect:      []Expect{{Row: 1, Column: 1, Text: "ACCOUNT ENQUIRY"}},
			Inputs:      []Input{{Row: 5, Column: 20, Length: 8, Parameter: "account_number"}},
			AIDKey:      "Enter",
		}},
		Outputs: []Output{{
			Name:   "cleared_balance",
			Label:  "Cleared balance",
			Row:    8,
			Column: 20,
			Length: 12,
		}},
	}
}

func TestValidateAcceptsAWellFormedTask(t *testing.T) {
	task := sampleTask()
	if err := task.Validate(); err != nil {
		t.Fatalf("expected the sample task to validate, got %v", err)
	}
}

func TestValidateRejectsMalformedTasks(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Task)
		wantSub string
	}{
		{"no name", func(x *Task) { x.Name = "  " }, "name is required"},
		{"no steps", func(x *Task) { x.Steps = nil }, "at least one step"},
		{
			"parameter nothing uses",
			func(x *Task) {
				x.Parameters = append(x.Parameters, Parameter{Name: "unused"})
			},
			"never used",
		},
		{
			"input naming an undefined parameter",
			func(x *Task) { x.Steps[0].Inputs[0].Parameter = "nope" },
			"undefined parameter",
		},
		{
			"input with both a value and a parameter",
			func(x *Task) { x.Steps[0].Inputs[0].Value = "literal" },
			"exactly one of value or parameter",
		},
		{
			"input with neither",
			func(x *Task) { x.Steps[0].Inputs[0].Parameter = "" },
			"exactly one of value or parameter",
		},
		{
			"zero-based coordinates",
			func(x *Task) { x.Steps[0].Inputs[0].Row = 0 },
			"1-based",
		},
		{
			"a key the terminal cannot press",
			func(x *Task) { x.Steps[0].AIDKey = "Reboot" },
			"not a 3270 key",
		},
		{
			"a literal carrying a control character",
			func(x *Task) {
				x.Steps[0].Inputs[0].Parameter = ""
				x.Steps[0].Inputs[0].Value = "one\ttwo"
			},
			"CR, LF or TAB",
		},
		{
			"an uncompilable validation pattern",
			func(x *Task) { x.Parameters[0].Pattern = "([" },
			"not a valid regular expression",
		},
		{
			"a default on a sensitive parameter",
			func(x *Task) {
				x.Parameters[0].Sensitive = true
				x.Parameters[0].Default = "hunter2"
			},
			"cannot have a default",
		},
		{
			"two outputs with the same name",
			func(x *Task) { x.Outputs = append(x.Outputs, x.Outputs[0]) },
			"defined twice",
		},
		{
			"an output with no length",
			func(x *Task) { x.Outputs[0].Length = 0 },
			"length must be between",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := sampleTask()
			tc.mutate(&task)
			err := task.Validate()
			if err == nil {
				t.Fatalf("expected an error mentioning %q, got none", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantSub, err)
			}
		})
	}
}

func TestResolveParametersAppliesTheDeclaredRules(t *testing.T) {
	task := sampleTask()
	task.Parameters = append(task.Parameters, Parameter{
		Name:    "as_at_date",
		Label:   "As-at date",
		Default: "TODAY",
	})
	task.Steps[0].Inputs = append(task.Steps[0].Inputs, Input{Row: 6, Column: 20, Parameter: "as_at_date"})
	if err := task.Validate(); err != nil {
		t.Fatalf("fixture does not validate: %v", err)
	}

	resolved, err := task.ResolveParameters(map[string]string{"account_number": "40218855"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["account_number"] != "40218855" {
		t.Errorf("account_number = %q", resolved["account_number"])
	}
	if resolved["as_at_date"] != "TODAY" {
		t.Errorf("an omitted optional parameter should fall back to its default, got %q", resolved["as_at_date"])
	}
}

func TestResolveParametersRejectsBadInput(t *testing.T) {
	task := sampleTask()
	cases := []struct {
		name     string
		supplied map[string]string
		wantSub  string
	}{
		{"missing required", map[string]string{}, "is required"},
		{"blank required", map[string]string{"account_number": "   "}, "is required"},
		{"fails the pattern", map[string]string{"account_number": "ABC"}, "not in the expected format"},
		{"too long", map[string]string{"account_number": "123456789"}, "8 characters or fewer"},
		{"control character", map[string]string{"account_number": "1234\n567"}, "CR, LF or TAB"},
		{
			"a value for a parameter that does not exist",
			map[string]string{"account_number": "40218855", "ghost": "x"},
			"not a parameter of this task",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := task.ResolveParameters(tc.supplied)
			if err == nil {
				t.Fatalf("expected an error mentioning %q, got none", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.wantSub, err)
			}
		})
	}
}

// The pattern must constrain the whole value, not merely appear in it —
// otherwise "\d{8}" would accept "DROP TABLE 40218855".
func TestResolveParametersAnchorsThePattern(t *testing.T) {
	task := sampleTask()
	if _, err := task.ResolveParameters(map[string]string{"account_number": "x40218855"}); err == nil {
		t.Fatal("expected an unanchored match to be rejected")
	}
}

func TestHostAIDKeySpeaksS3270(t *testing.T) {
	// The catalogue says "PF3"; s3270's action is "PF(3)". Sending the
	// catalogue spelling makes s3270 fall back to Key(PF3) and refuse it,
	// which is how business tasks with PF keys used to stop mid-run.
	cases := map[string]string{
		"pf3":    "PF(3)",
		"PF24":   "PF(24)",
		"pa2":    "PA(2)",
		"enter":  "Enter",
		"clear":  "Clear",
		"attn":   "Attn",
		"sysreq": "SysReq",
	}
	for in, want := range cases {
		got, ok := HostAIDKey(in)
		if !ok || got != want {
			t.Errorf("HostAIDKey(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	if _, ok := HostAIDKey("PF25"); ok {
		t.Error("PF25 does not exist and must not be accepted")
	}
}

func TestCanonicalAIDKeyNormalisesSpelling(t *testing.T) {
	for _, spelling := range []string{"pf3", "PF3", " Pf3 "} {
		got, ok := CanonicalAIDKey(spelling)
		if !ok || got != "PF3" {
			t.Errorf("CanonicalAIDKey(%q) = %q, %v; want PF3, true", spelling, got, ok)
		}
	}
	if _, ok := CanonicalAIDKey("PF25"); ok {
		t.Error("PF25 does not exist and must not be accepted")
	}
	if _, ok := CanonicalAIDKey("Quit()"); ok {
		t.Error("an s3270 action must not be accepted as a key name")
	}
}

func TestCloneDoesNotShareSlices(t *testing.T) {
	original := sampleTask()
	clone := original.Clone()
	clone.Steps[0].Inputs[0].Value = "mutated"
	clone.Parameters[0].Name = "mutated"
	if original.Steps[0].Inputs[0].Value == "mutated" {
		t.Error("mutating a clone's step inputs changed the original")
	}
	if original.Parameters[0].Name == "mutated" {
		t.Error("mutating a clone's parameters changed the original")
	}
}
