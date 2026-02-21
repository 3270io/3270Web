package chaos

import (
	"math/rand"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// buildMockScreen returns a simple formatted screen with one unprotected
// field and one protected label field.
func buildMockScreen() *host.Screen {
	s := &host.Screen{
		Width:       80,
		Height:      24,
		IsFormatted: true,
		Buffer:      make([][]rune, 24),
	}
	for i := range s.Buffer {
		s.Buffer[i] = make([]rune, 80)
	}
	// Protected label field at row 0, col 0-9.
	s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, 0, 9, 0, 0, 0))
	// Unprotected input field at row 2, col 10-19.
	s.Fields = append(s.Fields, host.NewField(s, 0x00, 10, 2, 19, 2, 0, 0))
	return s
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxSteps <= 0 {
		t.Error("DefaultConfig.MaxSteps must be positive")
	}
	if cfg.TimeBudget <= 0 {
		t.Error("DefaultConfig.TimeBudget must be positive")
	}
	if len(cfg.AIDKeyWeights) == 0 {
		t.Error("DefaultConfig.AIDKeyWeights must not be empty")
	}
	if cfg.MaxFieldLength <= 0 {
		t.Error("DefaultConfig.MaxFieldLength must be positive")
	}
}

func TestHashScreen(t *testing.T) {
	s := buildMockScreen()
	h1 := hashScreen(s)
	if len(h1) == 0 {
		t.Fatal("hashScreen returned empty string")
	}

	// Same screen must produce the same hash.
	h2 := hashScreen(s)
	if h1 != h2 {
		t.Error("hashScreen is not deterministic for the same screen")
	}

	// Changing the cursor position must change the hash.
	s.CursorX = 5
	h3 := hashScreen(s)
	if h1 == h3 {
		t.Error("hashScreen did not change when cursor moved")
	}

	// nil screen must return empty string without panicking.
	if hashScreen(nil) != "" {
		t.Error("hashScreen(nil) should return empty string")
	}
}

func TestUnprotectedFields(t *testing.T) {
	s := buildMockScreen()
	fields := unprotectedFields(s)
	if len(fields) != 1 {
		t.Fatalf("expected 1 unprotected field, got %d", len(fields))
	}
	if fields[0].IsProtected() {
		t.Error("field must not be protected")
	}
}

func TestFieldLength(t *testing.T) {
	s := buildMockScreen()
	// The unprotected field spans col 10-19 on a single row → length 10.
	f := s.Fields[1]
	if got := fieldLength(f); got != 10 {
		t.Errorf("fieldLength = %d, want 10", got)
	}
}

func TestAidKeyToStepType(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"Enter", "PressEnter"},
		{"Clear", "PressClear"},
		{"Tab", "PressTab"},
		{"PF(1)", "PressPF1"},
		{"PF(12)", "PressPF12"},
		{"PA(1)", "PressPA1"},
		{"unknown", "PressEnter"},
	}
	for _, c := range cases {
		if got := aidKeyToStepType(c.key); got != c.want {
			t.Errorf("aidKeyToStepType(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestEngineStartStop(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 3
	cfg.StepDelay = 0
	cfg.Seed = 42

	e := New(h, cfg)

	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for the engine to finish (MaxSteps = 3 with no delay).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Status().Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	status := e.Status()
	if status.Active {
		t.Error("engine should have stopped after MaxSteps")
		e.Stop()
	}
	if status.StepsRun == 0 {
		t.Error("engine ran 0 steps")
	}
}

func TestEngineNotConnected(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	// Connected = false by default.

	e := New(h, DefaultConfig())
	if err := e.Start(); err == nil {
		t.Error("Start() should fail when not connected")
	}
}

func TestEngineDoubleStart(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Connected = true
	h.Screen = buildMockScreen()

	cfg := DefaultConfig()
	cfg.MaxSteps = 0 // unlimited – we will stop manually
	cfg.StepDelay = 50 * time.Millisecond
	cfg.Seed = 1

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer e.Stop()

	if err := e.Start(); err == nil {
		t.Error("second Start() should return an error")
	}
}

func TestExportWorkflow(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Connected = true
	h.Screen = buildMockScreen()

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 0
	cfg.Seed = 7

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Status().Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	data, err := e.ExportWorkflow("testhost", 3270)
	if err != nil {
		t.Fatalf("ExportWorkflow error: %v", err)
	}
	if len(data) == 0 {
		t.Error("ExportWorkflow returned empty JSON")
	}
}

func TestChooseAIDKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Seed = 99
	e := New(nil, cfg)

	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		k := e.chooseAIDKey()
		counts[k]++
	}
	// Enter has the highest weight (70%) – it should be chosen most often.
	enterCount := counts["Enter"]
	if enterCount < 500 {
		t.Errorf("Enter chosen only %d/1000 times, expected majority", enterCount)
	}
}

func TestGenerateValue_Numeric(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	// Numeric field (AttrNumeric bit set).
	f := host.NewField(s, host.AttrNumeric, 0, 0, 9, 0, 0, 0)
	e := New(nil, DefaultConfig())
	e.rng = rand.New(rand.NewSource(42)) //nolint:gosec

	v := e.generateValue(f)
	if len(v) == 0 {
		t.Fatal("generateValue returned empty string for numeric field")
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			t.Errorf("generateValue for numeric field contains non-digit %q", c)
		}
	}
}

func TestGenerateValue_RespectsMaxFieldLength(t *testing.T) {
	s := &host.Screen{Width: 80, Height: 24}
	// Wide unprotected field: col 0-49 = length 50
	f := host.NewField(s, 0x00, 0, 0, 49, 0, 0, 0)

	cfg := DefaultConfig()
	cfg.MaxFieldLength = 5
	e := New(nil, cfg)
	e.rng = rand.New(rand.NewSource(1)) //nolint:gosec

	v := e.generateValue(f)
	if len(v) > 5 {
		t.Errorf("generateValue produced %d chars, want at most 5", len(v))
	}
}

func TestEngineMetadata(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 3
	cfg.StepDelay = 0
	cfg.Seed = 77

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Status().Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if st.UniqueScreens == 0 {
		t.Error("expected at least one unique screen hash after exploration")
	}
	if st.AIDKeyCounts == nil || len(st.AIDKeyCounts) == 0 {
		t.Error("expected AIDKeyCounts to be populated after exploration")
	}
	total := 0
	for _, v := range st.AIDKeyCounts {
		total += v
	}
	if total != st.StepsRun {
		t.Errorf("sum of AIDKeyCounts (%d) != StepsRun (%d)", total, st.StepsRun)
	}
}

func TestEngineStatusIncludesAttemptDetails(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 0
	cfg.Seed = 123

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Status().Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := e.Status()
	if len(st.RecentAttempts) == 0 {
		t.Fatal("expected RecentAttempts to be populated")
	}
	if st.LastAttempt == nil {
		t.Fatal("expected LastAttempt to be populated")
	}
	if st.LastAttempt.Attempt <= 0 {
		t.Errorf("LastAttempt.Attempt = %d, want > 0", st.LastAttempt.Attempt)
	}
	if st.LastAttempt.AIDKey == "" {
		t.Error("LastAttempt.AIDKey should not be empty")
	}
	if st.LastAttempt.FieldsTargeted <= 0 {
		t.Errorf("LastAttempt.FieldsTargeted = %d, want > 0", st.LastAttempt.FieldsTargeted)
	}
	if len(st.LastAttempt.FieldWrites) == 0 {
		t.Error("expected at least one field write record in LastAttempt")
	}
}

func TestSnapshotAndResume(t *testing.T) {
	h, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	h.Screen = buildMockScreen()
	h.Connected = true

	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	cfg.StepDelay = 0
	cfg.Seed = 13

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Status().Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	snap := e.Snapshot("test-run-1")
	if snap.ID != "test-run-1" {
		t.Errorf("snapshot ID = %q, want %q", snap.ID, "test-run-1")
	}
	if snap.StepsRun == 0 {
		t.Error("snapshot StepsRun should be > 0")
	}
	if len(snap.ScreenHashes) == 0 {
		t.Error("snapshot ScreenHashes should be populated")
	}

	// Resume from snapshot on a fresh engine with a higher MaxSteps so that
	// at least 2 new steps are run beyond the original count.
	cfg2 := DefaultConfig()
	cfg2.MaxSteps = snap.StepsRun + 2
	cfg2.StepDelay = 0
	cfg2.Seed = 99
	e2 := New(h, cfg2)

	if err := e2.Resume(snap); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !e2.Status().Active {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st2 := e2.Status()
	if st2.LoadedRunID != "test-run-1" {
		t.Errorf("resumed engine LoadedRunID = %q, want %q", st2.LoadedRunID, "test-run-1")
	}
	// Total steps should include those from the original run.
	if st2.StepsRun <= snap.StepsRun {
		t.Errorf("resumed engine StepsRun (%d) should exceed original (%d)", st2.StepsRun, snap.StepsRun)
	}
	// Screen hashes should include those from the original run.
	if st2.UniqueScreens < snap.UniqueScreens {
		t.Errorf("resumed engine UniqueScreens (%d) less than original (%d)", st2.UniqueScreens, snap.UniqueScreens)
	}
}

func TestPersistenceRoundtrip(t *testing.T) {
	dir := t.TempDir()

	run := &SavedRun{
		SavedRunMeta: SavedRunMeta{
			ID:            "20240101-000000-ab",
			StartedAt:     time.Now().Add(-time.Minute),
			StoppedAt:     time.Now(),
			StepsRun:      5,
			Transitions:   2,
			UniqueScreens: 3,
			UniqueInputs:  4,
		},
		ScreenHashes: map[string]bool{"abc": true, "def": true},
		AIDKeyCounts: map[string]int{"Enter": 4, "PF(1)": 1},
	}

	if err := SaveRun(dir, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	metas, err := ListRuns(dir)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("ListRuns: want 1 entry, got %d", len(metas))
	}
	if metas[0].ID != run.ID {
		t.Errorf("ListRuns ID = %q, want %q", metas[0].ID, run.ID)
	}

	loaded, err := LoadRun(dir, run.ID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if loaded.StepsRun != run.StepsRun {
		t.Errorf("loaded StepsRun = %d, want %d", loaded.StepsRun, run.StepsRun)
	}
	if len(loaded.ScreenHashes) != len(run.ScreenHashes) {
		t.Errorf("loaded ScreenHashes len = %d, want %d", len(loaded.ScreenHashes), len(run.ScreenHashes))
	}
}

func TestListRuns_NonExistentDir(t *testing.T) {
	metas, err := ListRuns("/tmp/nonexistent-chaos-dir-xyz-999")
	if err != nil {
		t.Errorf("ListRuns non-existent dir should not error, got: %v", err)
	}
	if metas != nil {
		t.Errorf("expected nil slice for non-existent dir, got %v", metas)
	}
}
