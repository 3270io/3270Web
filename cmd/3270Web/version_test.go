// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"regexp"
	"testing"
)

// The version is decided in one place — appVersion in main.go — and repeated
// in two documentation files that no compiler ever reads. They had already
// drifted: the docs site advertised 1.8.6 while every binary and container
// reported 0.4.0, so an operator comparing what they were running against what
// the documentation described had no way to tell which was wrong.
//
// This is the check that stops it happening again. Change appVersion, run the
// tests, and the failures list what else to update.
func TestVersionStringsAgree(t *testing.T) {
	// Paths are relative to this package directory, which is where `go test`
	// runs; the repository root is two levels up.
	for _, tc := range []struct {
		name    string
		path    string
		pattern *regexp.Regexp
	}{
		{
			name:    "mkdocs.yml extra.version",
			path:    "../../mkdocs.yml",
			pattern: regexp.MustCompile(`(?m)^\s+version:\s*(\S+)\s*$`),
		},
		{
			name: "the version chip on the docs home page",
			path: "../../docs/index.md",
			// <span class="chip accent">… Open source · v0.4.0</span>
			pattern: regexp.MustCompile(`Open source · v(\S+?)<`),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}

			m := tc.pattern.FindSubmatch(b)
			if m == nil {
				t.Fatalf("no version found in %s — did its format change? "+
					"Keep it in step with appVersion (%s) either way.", tc.path, appVersion)
			}

			if got := string(m[1]); got != appVersion {
				t.Errorf("%s says %s, appVersion says %s — update %s",
					tc.name, got, appVersion, tc.path)
			}
		})
	}
}
