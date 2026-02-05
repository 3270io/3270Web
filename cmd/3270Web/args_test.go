package main

import (
	"testing"

	"github.com/jnnngs/3270Web/internal/config"
)

func TestBuildS3270Args_QuoteHandling(t *testing.T) {
	// Scenario: User wants to pass a complex option via <additional> in XML.
	// Example: -set "toggle allowRemote" or -scriptport "127.0.0.1:4000"
	opts := config.S3270Options{
		Additional: `-set "toggle allowRemote"`,
	}

	args := buildS3270Args(opts, "")

	// We expect the arguments to be preserved as ["-set", "toggle allowRemote"]
	// But strings.Fields will split it into ["-set", "\"toggle", "allowRemote\""]

	found := false
	for _, arg := range args {
		if arg == "toggle allowRemote" {
			found = true
			break
		}
	}

	// Current behavior check (this test expects the bug to exist)
	// If the bug exists, 'found' will be false.
	// I will assert that we WANT to find it.

	if !found {
		t.Logf("Args generated: %q", args)
		t.Error("Expected quoted argument to be preserved as a single token, but it was split.")
	}
}
