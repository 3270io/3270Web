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
	cfg.MaxSteps = 0   // unlimited – we will stop manually
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
