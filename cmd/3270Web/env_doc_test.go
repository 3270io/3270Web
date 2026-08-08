package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/config"
)

func TestEnvDocumentationDrift(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")

	if err := config.EnsureDotEnv(envPath); err != nil {
		t.Fatalf("EnsureDotEnv failed: %v", err)
	}

	contentBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	content := string(contentBytes)

	// Every setting the template offers must arrive with a comment saying
	// what it does. A value alone leaves the operator to guess, and the
	// ones that gate a network surface are the worst to guess about.
	cases := []struct{ comment, value string }{
		{"# -noverifycert: Do not verify the TLS host certificate (default: false)", "S3270_NO_VERIFY_CERT=false"},
		{"# Bearer token for /api/v1 and the MCP HTTP transport.", "API_TOKEN="},
		{"# Allow the headless API to open sessions against the bundled sample apps", "ALLOW_SAMPLE_APPS=false"},
		{"# Tool tier for the MCP server", "MCP_TOOLS=interactive"},
		{"# Comma-separated glob list of hosts an AI client may connect to.", "MCP_ALLOWED_HOSTS="},
	}

	for _, tc := range cases {
		if !strings.Contains(content, tc.comment) {
			t.Errorf("Drift check failed: Expected comment %q not found in content:\n%s", tc.comment, content)
		}
		if !strings.Contains(content, tc.value) {
			t.Errorf("Drift check failed: Expected value %q not found in content:\n%s", tc.value, content)
		}
	}
}
