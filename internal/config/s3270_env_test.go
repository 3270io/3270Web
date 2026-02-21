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
	if !strings.Contains(string(content), "CHAOS_MAX_STEPS=100") {
		t.Errorf("Created .env file does not contain CHAOS_MAX_STEPS default")
	}
	if !strings.Contains(string(content), "CHAOS_EXCLUDE_NO_PROGRESS_EVENTS=true") {
		t.Errorf("Created .env file does not contain CHAOS_EXCLUDE_NO_PROGRESS_EVENTS default")
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

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  []string
		wantError bool
	}{
		{
			name:     "Empty input",
			input:    "",
			expected: nil,
		},
		{
			name:     "Simple args",
			input:    "one two three",
			expected: []string{"one", "two", "three"},
		},
		{
			name:     "Multiple spaces",
			input:    "one   two",
			expected: []string{"one", "two"},
		},
		{
			name:     "Leading and trailing spaces",
			input:    "  one two  ",
			expected: []string{"one", "two"},
		},
		{
			name:     "Double quotes",
			input:    `"one two" three`,
			expected: []string{"one two", "three"},
		},
		{
			name:     "Single quotes",
			input:    `'one two' three`,
			expected: []string{"one two", "three"},
		},
		{
			name:     "Empty double quotes",
			input:    `one "" two`,
			expected: []string{"one", "", "two"},
		},
		{
			name:     "Empty single quotes",
			input:    `one '' two`,
			expected: []string{"one", "", "two"},
		},
		{
			name:     "Empty quotes at start",
			input:    `"" one`,
			expected: []string{"", "one"},
		},
		{
			name:     "Empty quotes at end",
			input:    `one ""`,
			expected: []string{"one", ""},
		},
		{
			name:     "Nested quotes (single in double)",
			input:    `"one 'two' three"`,
			expected: []string{"one 'two' three"},
		},
		{
			name:     "Nested quotes (double in single)",
			input:    `'one "two" three'`,
			expected: []string{"one \"two\" three"},
		},
		{
			name:     "Escaped double quote",
			input:    `"one \"two\" three"`,
			expected: []string{`one "two" three`},
		},
		{
			name:     "Escaped backslash",
			input:    `one\\two`,
			expected: []string{`one\two`},
		},
		{
			name:      "Unterminated double quote",
			input:     `"one`,
			wantError: true,
		},
		{
			name:      "Unterminated single quote",
			input:     `'one`,
			wantError: true,
		},
		{
			name:      "Unterminated escape",
			input:     `one\`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitArgs(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("SplitArgs(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitArgs(%q) unexpected error: %v", tt.input, err)
			}
			if len(got) != len(tt.expected) {
				t.Errorf("SplitArgs(%q) length mismatch: got %d, want %d (got %q)", tt.input, len(got), len(tt.expected), got)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("SplitArgs(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestUpsertDotEnvValueUpdatesExistingKey(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	initial := "A=1\nAPP_USE_KEYPAD=false\nB=2\n"
	if err := os.WriteFile(envPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	if err := UpsertDotEnvValue(envPath, "APP_USE_KEYPAD", "true"); err != nil {
		t.Fatalf("UpsertDotEnvValue failed: %v", err)
	}

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "APP_USE_KEYPAD=true") {
		t.Fatalf("expected updated key, got: %q", text)
	}
	if strings.Contains(text, "APP_USE_KEYPAD=false") {
		t.Fatalf("old key value still present: %q", text)
	}
}

func TestUpsertDotEnvValueAppendsMissingKey(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	initial := "A=1\nB=2\n"
	if err := os.WriteFile(envPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write initial: %v", err)
	}

	if err := UpsertDotEnvValue(envPath, "APP_USE_KEYPAD", "true"); err != nil {
		t.Fatalf("UpsertDotEnvValue failed: %v", err)
	}

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "APP_USE_KEYPAD=true") {
		t.Fatalf("expected appended key, got: %q", text)
	}
}
