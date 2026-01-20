package config

import (
	"testing"
)

func TestLoadConfig(t *testing.T) {
	cfg, err := Load("../../webapp/WEB-INF/h3270-config.xml")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.ExecPath != "/usr/local/bin" {
		t.Errorf("Expected exec-path /usr/local/bin, got %s", cfg.ExecPath)
	}
}
