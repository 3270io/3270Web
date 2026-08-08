package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore(t.TempDir())
	s.now = func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC) }
	return s
}

func TestStoreRoundTrip(t *testing.T) {
	store := newTestStore(t)

	if tasks, err := store.List(); err != nil || len(tasks) != 0 {
		t.Fatalf("a fresh catalogue should be empty, got %v, %v", tasks, err)
	}

	if _, err := store.Upsert(sampleTask()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	tasks, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Name != "Account balance enquiry" {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[0].UpdatedAt.IsZero() {
		t.Error("upsert should stamp UpdatedAt")
	}

	found, ok := store.Find("ACCOUNT   Balance   Enquiry")
	if !ok {
		t.Fatal("lookup should ignore case and extra whitespace")
	}
	if found.Name != "Account balance enquiry" {
		t.Errorf("found = %q", found.Name)
	}

	if _, ok := store.Find("nothing like it"); ok {
		t.Error("an unknown name must not resolve")
	}
}

func TestStoreUpsertReplacesRatherThanDuplicating(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Upsert(sampleTask()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	updated := sampleTask()
	updated.Name = "account balance ENQUIRY"
	updated.Description = "Now with a different description."
	tasks, err := store.Upsert(updated)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected the same task to be replaced, got %d entries", len(tasks))
	}
	if tasks[0].Description != "Now with a different description." {
		t.Errorf("description = %q", tasks[0].Description)
	}
}

func TestStoreRejectsAnInvalidTaskBeforeWriting(t *testing.T) {
	store := newTestStore(t)
	bad := sampleTask()
	bad.Steps[0].Inputs[0].Parameter = "ghost"
	if _, err := store.Upsert(bad); err == nil {
		t.Fatal("expected validation to reject the task")
	}
	if _, err := os.Stat(store.path); !os.IsNotExist(err) {
		t.Error("a rejected task must not create the catalogue file")
	}
}

func TestStoreDelete(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Upsert(sampleTask()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	tasks, err := store.Delete("Account Balance Enquiry")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected an empty catalogue, got %+v", tasks)
	}
	// Deleting what is already gone is what the caller wanted.
	if _, err := store.Delete("Account balance enquiry"); err != nil {
		t.Errorf("deleting a missing task should not error, got %v", err)
	}
}

func TestStoreReportsCorruptCatalogueRatherThanSilentlyEmptyingIt(t *testing.T) {
	store := newTestStore(t)
	if err := os.WriteFile(store.path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := store.List()
	if err == nil {
		t.Fatal("expected an error for a corrupt catalogue")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %v; want it to name the problem", err)
	}
}

// A partly-written catalogue would be worse than none, so the write goes via
// a temp file and a rename.
func TestStoreLeavesNoTempFileBehind(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Upsert(sampleTask()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(store.path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file %q was left behind", e.Name())
		}
	}
}

func TestStoreFindReturnsACopy(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Upsert(sampleTask()); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	found, _ := store.Find("Account balance enquiry")
	found.Steps[0].Inputs[0].Value = "mutated"
	again, _ := store.Find("Account balance enquiry")
	if again.Steps[0].Inputs[0].Value == "mutated" {
		t.Error("Find must hand back a copy, not the stored task")
	}
}
