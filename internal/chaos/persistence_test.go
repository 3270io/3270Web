package chaos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRunID(t *testing.T) {
	cases := []struct {
		name    string
		runID   string
		wantErr bool
	}{
		{"empty", "", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"parent traversal", "../../etc/passwd", true},
		{"backslash traversal", `..\..\windows`, true},
		{"absolute unix", "/etc/passwd", true},
		{"absolute windows", `C:\Windows`, true},
		{"contains slash", "foo/bar", true},
		{"contains dot", "foo.bar", true},
		{"null byte", "foo\x00bar", true},
		{"newline", "foo\nbar", true},
		{"space", "foo bar", true},
		{"valid generated", "20260517-192152-deadbeef", false},
		{"valid alnum", "run123", false},
		{"valid underscore", "run_1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRunID(tc.runID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateRunID(%q) error = %v, wantErr = %v", tc.runID, err, tc.wantErr)
			}
		})
	}
}

func TestNewRunIDPassesValidator(t *testing.T) {
	for i := 0; i < 50; i++ {
		id := NewRunID()
		if err := validateRunID(id); err != nil {
			t.Fatalf("NewRunID() returned %q which fails validateRunID: %v", id, err)
		}
	}
}

func TestLoadRunRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	// Create a sibling file outside the chaos runs dir to confirm traversal would
	// otherwise succeed.
	parent := filepath.Dir(dir)
	secretPath := filepath.Join(parent, "secret.json")
	if err := os.WriteFile(secretPath, []byte(`{"id":"x"}`), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(secretPath) })

	_, err := LoadRun(dir, "../secret")
	if err == nil {
		t.Fatal("expected LoadRun to reject traversal, got nil")
	}
	if !strings.Contains(err.Error(), "invalid run ID") {
		t.Fatalf("expected invalid run ID error, got %v", err)
	}
}

func TestDeleteRunRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	err := DeleteRun(dir, "../something")
	if err == nil {
		t.Fatal("expected DeleteRun to reject traversal, got nil")
	}
	if !strings.Contains(err.Error(), "invalid run ID") {
		t.Fatalf("expected invalid run ID error, got %v", err)
	}
}

func TestSaveRunRejectsInvalidID(t *testing.T) {
	dir := t.TempDir()
	err := SaveRun(dir, &SavedRun{SavedRunMeta: SavedRunMeta{ID: "../escape"}})
	if err == nil {
		t.Fatal("expected SaveRun to reject invalid ID, got nil")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	run := &SavedRun{
		SavedRunMeta: SavedRunMeta{
			ID:       NewRunID(),
			StepsRun: 3,
		},
	}
	if err := SaveRun(dir, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	got, err := LoadRun(dir, run.ID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if got.ID != run.ID || got.StepsRun != run.StepsRun {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got.SavedRunMeta, run.SavedRunMeta)
	}
	if err := DeleteRun(dir, run.ID); err != nil {
		t.Fatalf("DeleteRun: %v", err)
	}
}
