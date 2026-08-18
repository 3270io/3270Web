// SPDX-License-Identifier: AGPL-3.0-or-later

package task

import "testing"

func previewScreen() string {
	return "ACCOUNT ENQUIRY\n" +
		"\n" +
		"Name . . . . . :  GRACE HOPPER\n" +
		"Cleared balance:      1,240.55 CR\n"
}

func TestPreviewOutputsReadsTheSameRegionsARunWould(t *testing.T) {
	values, missing := PreviewOutputs(previewScreen(), []Output{
		{Name: "name", Label: "Name", Row: 3, Column: 19, Length: 12},
		{Name: "balance", Label: "Cleared balance", Row: 4, Column: 19, Length: 15,
			Pattern: `([\d,]+\.\d{2})`},
	})
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	if values[0].Value != "GRACE HOPPER" {
		t.Errorf("name = %q, want GRACE HOPPER", values[0].Value)
	}
	// The pattern is what turns the whole slot into the figure on it.
	if values[1].Value != "1,240.55" {
		t.Errorf("balance = %q, want 1,240.55", values[1].Value)
	}
}

func TestPreviewOutputsReportsWhatItCannotRead(t *testing.T) {
	values, missing := PreviewOutputs(previewScreen(), []Output{
		{Name: "blank", Label: "Overdraft limit", Row: 2, Column: 10, Length: 10},
		{Name: "nomatch", Label: "Reference", Row: 3, Column: 19, Length: 12, Pattern: `\d{6}`},
		{Name: "spare", Label: "Spare", Row: 2, Column: 40, Length: 5, Optional: true},
	})
	// An empty region and a pattern that matches nothing are both "could not
	// read this", which is the answer worth having before the task is saved.
	if len(missing) != 2 {
		t.Fatalf("missing = %v, want the two non-optional ones", missing)
	}
	for _, v := range values[:2] {
		if v.Found || v.Value != "" {
			t.Errorf("%s: found=%v value=%q, want not found and empty", v.Name, v.Found, v.Value)
		}
	}
	if values[2].Found {
		t.Errorf("an optional blank region reported found")
	}
}

func TestPreviewOutputsClampsToTheRowAndTrims(t *testing.T) {
	// A region wider than the row is the normal case, not an edge one: a
	// marked region runs to the end of the line so a longer value than the
	// recorded one is not cut short.
	values, _ := PreviewOutputs(previewScreen(), []Output{
		{Name: "name", Label: "Name", Row: 3, Column: 19, Length: 120},
	})
	if values[0].Value != "GRACE HOPPER" {
		t.Errorf("value = %q, want the trimmed row remainder", values[0].Value)
	}

	off, _ := PreviewOutputs(previewScreen(), []Output{
		{Name: "gone", Label: "Gone", Row: 99, Column: 1, Length: 4},
	})
	if off[0].Found {
		t.Errorf("a region off the bottom of the screen reported found")
	}
}
