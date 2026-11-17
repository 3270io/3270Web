// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import "testing"

func TestCrossSizeTokenScore(t *testing.T) {
	twoColumns := []string{
		"AAA999 AAAA AAAAAAA",
		"AAAA Aaaa aaaa AAAAA Aaaa aaa",
		"AAAA Aaaaaaaa AAAA Aaaaaaaaaaaa",
		"AAA Aaaaa AAAAA Aaaaa aaaaaaaaa",
		"AAAAAAA ===>",
	}
	oneColumn := []string{
		"AAA999 AAAA AAAAAAA",
		"AAAA Aaaa aaaa",
		"AAAA Aaaaaaaa",
		"AAA Aaaaa",
		"AAAAA Aaaa aaa",
		"AAAA Aaaaaaaaaaaa",
		"AAAAA Aaaaa aaaaaaaaa",
		"AAAAAAA ===>",
	}
	if got := crossSizeRowScore(twoColumns, oneColumn); got >= crossSizeRowContainment {
		t.Fatalf("the rows line up (%.2f); this case no longer needs the word comparison", got)
	}
	if got := crossSizeTokenScore(twoColumns, oneColumn); got < crossSizeRowContainment {
		t.Errorf("one screen in two layouts scored %.2f, below %.2f", got, crossSizeRowContainment)
	}

	// A screen that merely contains another is not that screen rearranged.
	longer := append(append([]string(nil), oneColumn...),
		"Aaaa aaaaaa aaaa aaaaa", "Aaaaaaa aaaa aaaaaaaa aaa", "Aaa aaaaa aaaaaa aaaa aa", "Aaaaaa aaa aaaaaaa aaaa")
	if got := crossSizeTokenScore(twoColumns, longer); got >= crossSizeRowContainment {
		t.Errorf("a screen with much more on it scored %.2f", got)
	}

	// A different title is a different screen, whatever else agrees.
	other := append([]string{"AAA999 AAAAA AAAAA"}, oneColumn[1:]...)
	if got := crossSizeTokenScore(twoColumns, other); got != 0 {
		t.Errorf("screens with different titles scored %.2f", got)
	}
}

func TestCollapseDigitRuns(t *testing.T) {
	for in, want := range map[string]string{
		"Aaa 99.99":            "Aaa 9.9",
		"Aaa 9.99":             "Aaa 9.9",
		"99":                   "9",
		"9":                    "9",
		"AAA-9999-9-A AA9 aaa": "AAA-9-9-A AA9 aaa",
		"Aaaa aaa":             "Aaaa aaa",
		"":                     "",
	} {
		if got := collapseDigitRuns(in); got != want {
			t.Errorf("collapseDigitRuns(%q) = %q, want %q", in, got, want)
		}
	}
}
