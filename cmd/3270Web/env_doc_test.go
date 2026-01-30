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

	// Check for the drift
	expectedComment := "# -noverifycert: Do not verify the TLS host certificate (default: false)"
	expectedValue := "S3270_NO_VERIFY_CERT=false"

	if !strings.Contains(content, expectedComment) {
		t.Errorf("Drift check failed: Expected comment %q not found in content:\n%s", expectedComment, content)
	}
	if !strings.Contains(content, expectedValue) {
		t.Errorf("Drift check failed: Expected value %q not found in content:\n%s", expectedValue, content)
	}
}
