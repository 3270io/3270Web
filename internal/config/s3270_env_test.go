package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDotEnv(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")

	// Case 1: File does not exist
	err := EnsureDotEnv(envPath)
	if err != nil {
		t.Fatalf("EnsureDotEnv failed: %v", err)
	}

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("Could not read created .env file: %v", err)
	}
	if !strings.Contains(string(content), "S3270_MODEL") {
		t.Errorf("Created .env file does not contain expected content")
	}

	// Case 2: File exists
	err = os.WriteFile(envPath, []byte("EXISTING=true"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = EnsureDotEnv(envPath)
	if err != nil {
		t.Fatalf("EnsureDotEnv failed on existing file: %v", err)
	}

	content, err = os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "EXISTING=true" {
		t.Errorf("EnsureDotEnv overwrote existing file")
	}
}

func TestLoadDotEnv(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")

	content := `
S3270_TEST_VAR=123
# Comment
S3270_TEST_VAR2="quoted value"
export S3270_TEST_VAR3=exported
`
	err := os.WriteFile(envPath, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure existing var is not overwritten
	os.Setenv("S3270_TEST_VAR", "original")
	defer os.Unsetenv("S3270_TEST_VAR")
	defer os.Unsetenv("S3270_TEST_VAR2")
	defer os.Unsetenv("S3270_TEST_VAR3")

	err = LoadDotEnv(envPath)
	if err != nil {
		t.Fatalf("LoadDotEnv failed: %v", err)
	}

	if val := os.Getenv("S3270_TEST_VAR"); val != "original" {
		t.Errorf("Expected S3270_TEST_VAR to be 'original', got %q", val)
	}
	if val := os.Getenv("S3270_TEST_VAR2"); val != "quoted value" {
		t.Errorf("Expected S3270_TEST_VAR2 to be 'quoted value', got %q", val)
	}
	if val := os.Getenv("S3270_TEST_VAR3"); val != "exported" {
		t.Errorf("Expected S3270_TEST_VAR3 to be 'exported', got %q", val)
	}
}

func TestS3270EnvOverridesFromEnv(t *testing.T) {
	// Set up environment
	envVars := map[string]string{
		"S3270_MODEL":     "2",
		"S3270_CODE_PAGE": "cp037",
		"S3270_TRACE":     "true",
		"S3270_PORT":      "2323",
	}

	for k, v := range envVars {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	overrides, err := S3270EnvOverridesFromEnv()
	if err != nil {
		t.Fatalf("S3270EnvOverridesFromEnv failed: %v", err)
	}

	if overrides.Model != "2" {
		t.Errorf("Expected Model to be '2', got %q", overrides.Model)
	}
	if !overrides.HasModel {
		t.Error("Expected HasModel to be true")
	}
	if overrides.CodePage != "cp037" {
		t.Errorf("Expected CodePage to be 'cp037', got %q", overrides.CodePage)
	}
	if !overrides.HasCodePage {
		t.Error("Expected HasCodePage to be true")
	}

	hasTrace := false
	hasPort := false

	for i := 0; i < len(overrides.Args); i++ {
		arg := overrides.Args[i]
		if arg == "-trace" {
			hasTrace = true
		}
		if arg == "-port" {
			if i+1 < len(overrides.Args) && overrides.Args[i+1] == "2323" {
				hasPort = true
			}
		}
	}

	if !hasTrace {
		t.Error("Expected -trace flag")
	}
	if !hasPort {
		t.Error("Expected -port 2323 flag")
	}
}

func TestS3270EnvOverridesArgParsing(t *testing.T) {
	os.Setenv("S3270_SET", "foo bar \"baz qux\"")
	defer os.Unsetenv("S3270_SET")

	overrides, err := S3270EnvOverridesFromEnv()
	if err != nil {
		t.Fatalf("S3270EnvOverridesFromEnv failed: %v", err)
	}

	found := false
	for i, arg := range overrides.Args {
		if arg == "-set" {
			// Next args should be the values
			if i+3 < len(overrides.Args) {
				if overrides.Args[i+1] == "foo" &&
					overrides.Args[i+2] == "bar" &&
					overrides.Args[i+3] == "baz qux" {
					found = true
				}
			}
		}
	}

	if !found {
		t.Errorf("Did not find expected -set arguments. Args: %v", overrides.Args)
	}
}
