// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"strings"
	"testing"
)

// newTestApp builds the application the way newApp does and puts the process
// environment back when the test ends.
//
// Starting the application loads its .env into the process environment, which
// is what a running server wants and what a test binary cannot afford: the
// file a fresh data directory is given sets every documented default,
// S3270_MODEL among them, and an environment setting overrides what a caller
// asks for. A test that built an app therefore changed the terminal model for
// every test that ran after it in the same process. Which tests those were
// depended on the selection, so the failure came and went with -run.
func newTestApp(t testing.TB, baseDir string) *App {
	t.Helper()
	restoreEnvironmentAfter(t)
	return newApp(baseDir)
}

// newTestAppAt is newTestApp for a program directory and a data directory
// that differ.
func newTestAppAt(t testing.TB, installDir, dataDir string) *App {
	t.Helper()
	restoreEnvironmentAfter(t)
	return newAppAt(installDir, dataDir)
}

// restoreEnvironmentAfter records the environment now and restores exactly
// that when the test finishes: variables added since are removed, and ones
// changed or removed are put back.
func restoreEnvironmentAfter(t testing.TB) {
	t.Helper()
	before := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			before[k] = v
		}
	}
	t.Cleanup(func() {
		for _, kv := range os.Environ() {
			k, _, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			if _, had := before[k]; !had {
				_ = os.Unsetenv(k)
			}
		}
		for k, v := range before {
			if cur, ok := os.LookupEnv(k); !ok || cur != v {
				_ = os.Setenv(k, v)
			}
		}
	})
}

// The helper exists so that one test cannot change what the next one sees.
// This pins the particular setting that gave the failure away.
func TestBuildingAnAppDoesNotLeakItsEnvironment(t *testing.T) {
	const key = "S3270_MODEL"
	before, had := os.LookupEnv(key)

	t.Run("build", func(t *testing.T) {
		newTestApp(t, t.TempDir())
		if _, set := os.LookupEnv(key); !set {
			t.Skip("building an app no longer sets " + key + "; nothing to restore")
		}
	})

	after, has := os.LookupEnv(key)
	if has != had || after != before {
		t.Errorf("%s was (%q, set=%v) before a test built an app and is (%q, set=%v) after it", key, before, had, after, has)
	}
}
