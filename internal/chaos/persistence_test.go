package chaos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestWriteFileAtomicReplacesContentCorrectly is a basic correctness check:
// the final file must contain exactly the new data.
func TestWriteFileAtomicReplacesContentCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.json")

	if err := WriteFileAtomic(path, []byte("first"), 0600); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second"), 0600); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "second" {
		t.Fatalf("content = %q, want %q", got, "second")
	}
}

// TestWriteFileAtomicLeavesNoTempFileOnSuccess guards against ListRuns
// (which lists *.json files) picking up a leftover temp file: the ".tmp-*"
// staging file must be gone once WriteFileAtomic returns successfully.
func TestWriteFileAtomicLeavesNoTempFileOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.json")

	if err := WriteFileAtomic(path, []byte("data"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "run.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("directory contains unexpected entries after a successful write: %v", names)
	}
}

// TestWriteFileAtomicPreservesExistingStateOnFailure guards against the
// exact corruption os.WriteFile risks: it truncates the destination in
// place, so a failure partway through a write (crash, full disk, a
// concurrent change) can destroy whatever was previously there.
// WriteFileAtomic must leave the destination path completely untouched
// when the write itself fails, because it never opens/truncates the real
// path — only a temp file that only replaces it via a final rename.
//
// The failure is induced by making `path` an existing, non-empty directory:
// renaming a file onto that is rejected by the OS (EISDIR/ENOTEMPTY)
// regardless of user privilege, unlike a chmod-based simulation (this test
// container runs as root, which bypasses ordinary permission checks).
func TestWriteFileAtomicPreservesExistingStateOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.json")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("seed existing directory at path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "marker"), []byte("x"), 0600); err != nil {
		t.Fatalf("seed marker file inside directory: %v", err)
	}

	err := WriteFileAtomic(path, []byte(`{"id":"should-not-land"}`), 0600)
	if err == nil {
		t.Fatal("expected WriteFileAtomic to fail when path is an existing non-empty directory")
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("existing directory missing after failed write: %v", statErr)
	}
	if !info.IsDir() {
		t.Fatal("existing directory was replaced by a file despite the write failing")
	}
	if _, err := os.Stat(filepath.Join(path, "marker")); err != nil {
		t.Fatalf("marker file inside the existing directory is gone after failed write: %v", err)
	}

	// No leftover temp file should remain in dir after the failed rename.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "run.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("unexpected leftover entries in dir after failed write: %v", names)
	}
}

func TestPruneRuns(t *testing.T) {
	dir := t.TempDir()

	// keep <= 0 disables pruning.
	if removed, err := PruneRuns(dir, 0); err != nil || removed != 0 {
		t.Fatalf("PruneRuns(keep=0) = (%d, %v), want (0, nil)", removed, err)
	}

	// Create 5 run files with increasing modification times so the newest are
	// deterministic regardless of filesystem timestamp resolution.
	ids := make([]string, 5)
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		id := NewRunID()
		ids[i] = id
		run := &SavedRun{SavedRunMeta: SavedRunMeta{ID: id, StepsRun: i}}
		if err := SaveRun(dir, run); err != nil {
			t.Fatalf("SaveRun %d: %v", i, err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(filepath.Join(dir, runFileName(id)), mt, mt); err != nil {
			t.Fatalf("Chtimes %d: %v", i, err)
		}
	}

	removed, err := PruneRuns(dir, 2)
	if err != nil {
		t.Fatalf("PruneRuns: %v", err)
	}
	if removed != 3 {
		t.Fatalf("PruneRuns removed = %d, want 3", removed)
	}

	// The two newest (ids[4], ids[3]) must survive; the three oldest are gone.
	for _, id := range []string{ids[3], ids[4]} {
		if _, err := LoadRun(dir, id); err != nil {
			t.Fatalf("expected run %q to survive pruning: %v", id, err)
		}
	}
	for _, id := range []string{ids[0], ids[1], ids[2]} {
		if _, err := LoadRun(dir, id); err == nil {
			t.Fatalf("expected run %q to be pruned", id)
		}
	}

	// Pruning again is a no-op when the count is already within the limit.
	if removed, err := PruneRuns(dir, 2); err != nil || removed != 0 {
		t.Fatalf("PruneRuns(second) = (%d, %v), want (0, nil)", removed, err)
	}
}
