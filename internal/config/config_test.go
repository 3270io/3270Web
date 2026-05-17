package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	content := `<?xml version="1.0" encoding="UTF-8"?>
<config>
	<exec-path>/opt/s3270</exec-path>
	<style>dark</style>
	<s3270-options>
		<charset>cp037</charset>
		<model>2</model>
		<additional>-trace</additional>
	</s3270-options>
	<target-host autoconnect="true">localhost:3270</target-host>
	<fonts default="Monospace">
		<font name="Monospace" description="Standard Monospace" />
	</fonts>
	<colorschemes default="Green">
		<scheme name="Green" pnbg="#000000" pnfg="#00FF00" />
	</colorschemes>
</config>`

	path := createConfigFile(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ExecPath != "/opt/s3270" {
		t.Errorf("ExecPath: got %q, want %q", cfg.ExecPath, "/opt/s3270")
	}
	if cfg.Style != "dark" {
		t.Errorf("Style: got %q, want %q", cfg.Style, "dark")
	}
	if cfg.S3270Options.Charset != "cp037" {
		t.Errorf("Charset: got %q, want %q", cfg.S3270Options.Charset, "cp037")
	}
	if cfg.S3270Options.Model != "2" {
		t.Errorf("Model: got %q, want %q", cfg.S3270Options.Model, "2")
	}
	if cfg.S3270Options.Additional != "-trace" {
		t.Errorf("Additional: got %q, want %q", cfg.S3270Options.Additional, "-trace")
	}
	if !cfg.TargetHost.AutoConnect {
		t.Error("TargetHost.AutoConnect: got false, want true")
	}
	if cfg.TargetHost.Value != "localhost:3270" {
		t.Errorf("TargetHost.Value: got %q, want %q", cfg.TargetHost.Value, "localhost:3270")
	}
	if cfg.Fonts.Default != "Monospace" {
		t.Errorf("Fonts.Default: got %q, want %q", cfg.Fonts.Default, "Monospace")
	}
	if len(cfg.Fonts.Fonts) != 1 || cfg.Fonts.Fonts[0].Name != "Monospace" {
		t.Errorf("Fonts: unexpected list %v", cfg.Fonts.Fonts)
	}
	if cfg.ColorSchemes.Default != "Green" {
		t.Errorf("ColorSchemes.Default: got %q, want %q", cfg.ColorSchemes.Default, "Green")
	}
	if len(cfg.ColorSchemes.Schemes) != 1 || cfg.ColorSchemes.Schemes[0].Name != "Green" {
		t.Errorf("ColorSchemes: unexpected list %v", cfg.ColorSchemes.Schemes)
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Empty config tags to trigger defaults
	content := `<config></config>`

	path := createConfigFile(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ExecPath != "/usr/local/bin" {
		t.Errorf("Default ExecPath: got %q, want %q", cfg.ExecPath, "/usr/local/bin")
	}
	if cfg.S3270Options.Charset != "bracket" {
		t.Errorf("Default Charset: got %q, want %q", cfg.S3270Options.Charset, "bracket")
	}
	if cfg.S3270Options.Model != "3" {
		t.Errorf("Default Model: got %q, want %q", cfg.S3270Options.Model, "3")
	}
	// When no fonts are configured, built-in defaults should be populated
	if len(cfg.Fonts.Fonts) == 0 {
		t.Error("Expected default fonts to be populated when none configured")
	}
	if cfg.Fonts.Default != "IBM 3270" {
		t.Errorf("Default font: got %q, want %q", cfg.Fonts.Default, "IBM 3270")
	}
}

func TestDefaultFonts(t *testing.T) {
	fonts := DefaultFonts()
	if len(fonts) == 0 {
		t.Fatal("DefaultFonts returned empty list")
	}
	// Verify first font is IBM 3270
	if fonts[0].Name != "IBM 3270" {
		t.Errorf("First default font: got %q, want %q", fonts[0].Name, "IBM 3270")
	}
	// All entries should have a name
	for i, f := range fonts {
		if f.Name == "" {
			t.Errorf("DefaultFonts[%d] has empty name", i)
		}
	}
}

func TestLoad_DefaultFontsNotOverriddenByXML(t *testing.T) {
	content := `<config>
	<fonts default="CustomFont">
		<font name="CustomFont" description="My Font" />
	</fonts>
</config>`

	path := createConfigFile(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// When fonts are explicitly configured, defaults should NOT be injected
	if len(cfg.Fonts.Fonts) != 1 {
		t.Errorf("Expected 1 font from XML, got %d", len(cfg.Fonts.Fonts))
	}
	if cfg.Fonts.Fonts[0].Name != "CustomFont" {
		t.Errorf("Expected CustomFont, got %q", cfg.Fonts.Fonts[0].Name)
	}
	if cfg.Fonts.Default != "CustomFont" {
		t.Errorf("Default font: got %q, want %q", cfg.Fonts.Default, "CustomFont")
	}
}

func TestLoad_ImplicitDefaultsFromList(t *testing.T) {
	content := `<config>
	<fonts>
		<font name="FontA" />
		<font name="FontB" />
	</fonts>
	<colorschemes>
		<scheme name="SchemeA" />
		<scheme name="SchemeB" />
	</colorschemes>
</config>`

	path := createConfigFile(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should default to first item if default attr is missing
	if cfg.Fonts.Default != "FontA" {
		t.Errorf("Implicit Default Font: got %q, want %q", cfg.Fonts.Default, "FontA")
	}
	if cfg.ColorSchemes.Default != "SchemeA" {
		t.Errorf("Implicit Default ColorScheme: got %q, want %q", cfg.ColorSchemes.Default, "SchemeA")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/path/to/non/existent/file.xml")
	if err == nil {
		t.Error("Load expected error for missing file, got nil")
	}
}

func TestLoad_MalformedXML(t *testing.T) {
	content := `<config><unclosed>`
	path := createConfigFile(t, content)
	_, err := Load(path)
	if err == nil {
		t.Error("Load expected error for malformed XML, got nil")
	}
}

func TestLoad_EncodingPassThrough(t *testing.T) {
	// The loader uses a pass-through CharsetReader, so it shouldn't fail on "ISO-8859-1"
	// even if Go's XML decoder doesn't support it natively without a helper.
	content := `<?xml version="1.0" encoding="ISO-8859-1"?>
<config>
	<style>tést</style>
</config>`

	path := createConfigFile(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed with encoding: %v", err)
	}
	// Since it's pass-through, the bytes are read as-is.
	// If the file was written as UTF-8 (which write_file does), "tést" is multibyte.
	// If the decoder thinks it's ISO-8859-1 but we feed it UTF-8 bytes and it passes them through,
	// the string in memory will be the UTF-8 bytes.
	if cfg.Style != "tést" {
		t.Errorf("Style: got %q, want %q", cfg.Style, "tést")
	}
}

func TestLoad_TrimTargetHost(t *testing.T) {
	content := `<config>
	<target-host>

		localhost:3270

	</target-host>
</config>`

	path := createConfigFile(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.TargetHost.Value != "localhost:3270" {
		t.Errorf("TargetHost.Value not trimmed: got %q", cfg.TargetHost.Value)
	}
}

func createConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.xml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}
	return path
}
