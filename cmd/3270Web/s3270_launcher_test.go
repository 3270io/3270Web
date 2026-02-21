package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jnnngs/3270Web/internal/assets"
)

func TestResolveS3270Path_FallsBackToEmbeddedWhenConfiguredDirMissingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH fallback semantics differ on windows")
	}

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "s3270")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write temp s3270: %v", err)
	}

	t.Setenv("PATH", tmpDir)

	embedded, err := assets.ExtractS3270()
	if err != nil {
		t.Fatalf("extract embedded s3270: %v", err)
	}

	got := resolveS3270Path("/usr/bin")
	if got != embedded {
		t.Fatalf("resolveS3270Path returned %q, want %q", got, embedded)
	}
}

func TestResolveS3270Path_UsesConfiguredBinaryPath(t *testing.T) {
	tmpDir := t.TempDir()
	binName := s3270BinaryName()
	binPath := filepath.Join(tmpDir, binName)
	if err := os.WriteFile(binPath, []byte("binary"), 0o644); err != nil {
		t.Fatalf("write temp s3270: %v", err)
	}

	got := resolveS3270Path(binPath)
	if got != binPath {
		t.Fatalf("resolveS3270Path returned %q, want %q", got, binPath)
	}
}
