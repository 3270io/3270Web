// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

func stepTypes(steps []session.WorkflowStep) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Type)
	}
	return out
}

// TestEngineStopsOnSaturation verifies that a run looping between two screens
// without producing new transitions/values stops with TerminationReasonSaturated
// once the saturation streak hits the configured threshold.
func TestEngineStopsOnSaturation(t *testing.T) {
	a := buildScriptedChaosScreen("FIRST SCREEN A", false)
	b := buildScriptedChaosScreen("SECOND SCREEN B", false)
	// Loop the host between A and B forever so the (from, to, aid) tuple
	// (A, B, Enter) and (B, A, Enter) are seen on every other step. After the
	// first two transitions, no new tuple is added.
	h := &loopingChaosHost{
		screens:   []*host.Screen{a, b},
		connected: true,
	}

	cfg := DefaultConfig()
	cfg.MaxSteps = 100
	cfg.TimeBudget = 5 * time.Second
	cfg.StepDelay = 0
	cfg.Seed = 1
	cfg.SaturationSteps = 5
	cfg.AIDKeyWeights = map[string]int{"Enter": 1}
	cfg.AutoBlockExitKeys = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	st := e.Status()
	if st.Active {
		t.Fatalf("engine still active; saturation should have stopped it")
	}
	if st.TerminationReason != TerminationReasonSaturated {
		t.Fatalf("TerminationReason = %q, want %q", st.TerminationReason, TerminationReasonSaturated)
	}
	if st.StepsRun >= cfg.MaxSteps {
		t.Fatalf("StepsRun = %d reached MaxSteps; saturation should have triggered first", st.StepsRun)
	}
	if st.CoverageStats == nil {
		t.Fatalf("CoverageStats nil; want populated")
	}
	if st.CoverageStats.SaturationStreak < cfg.SaturationSteps {
		t.Fatalf("SaturationStreak = %d, want >= %d", st.CoverageStats.SaturationStreak, cfg.SaturationSteps)
	}
}

// TestSaturationStopStillWritesWorkflowOutput protects the 3270Connect
// volume-testing pipeline: chaos must still write a valid workflow JSON to
// cfg.OutputFile when a run terminates via saturation (not just MaxSteps).
// The file must parse as exportedWorkflow with non-empty Host/Port and a
// Steps array — 3270Connect's playback loader rejects anything else.
func TestSaturationStopStillWritesWorkflowOutput(t *testing.T) {
	a := buildScriptedChaosScreen("FIRST SCREEN A", false)
	b := buildScriptedChaosScreen("SECOND SCREEN B", false)
	h := &loopingChaosHost{
		screens:   []*host.Screen{a, b},
		connected: true,
	}

	out := filepath.Join(t.TempDir(), "chaos-output.json")
	cfg := DefaultConfig()
	cfg.MaxSteps = 100
	cfg.TimeBudget = 5 * time.Second
	cfg.StepDelay = 0
	cfg.Seed = 1
	cfg.SaturationSteps = 5
	cfg.OutputFile = out
	cfg.ExportHost = "volumetest.example.com"
	cfg.ExportPort = 3270
	cfg.AIDKeyWeights = map[string]int{"Enter": 1}
	cfg.AutoBlockExitKeys = false

	e := New(h, cfg)
	if err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !e.Active() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e.Active() {
		t.Fatalf("engine still active; saturation should have stopped it")
	}
	if got := e.Status().TerminationReason; got != TerminationReasonSaturated {
		t.Fatalf("TerminationReason = %q, want %q", got, TerminationReasonSaturated)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("expected OutputFile %q to exist after saturation stop: %v", out, err)
	}
	var exported exportedWorkflow
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("OutputFile is not valid workflow JSON: %v\nbody: %s", err, string(data))
	}
	if exported.Host != "volumetest.example.com" {
		t.Errorf("workflow Host = %q, want %q", exported.Host, "volumetest.example.com")
	}
	if exported.Port != 3270 {
		t.Errorf("workflow Port = %d, want 3270", exported.Port)
	}
	if len(exported.Steps) == 0 {
		t.Fatalf("workflow Steps is empty; 3270Connect playback needs at least one step")
	}
	// 3270Connect expects step types from a known vocabulary (Connect /
	// FillString / PressEnter / PressPF<n> / Disconnect …). We don't need
	// to enumerate them all — just confirm we see at least one PressEnter
	// since the test loop sends Enter.
	sawEnter := false
	for _, s := range exported.Steps {
		if s.Type == "PressEnter" {
			sawEnter = true
			break
		}
	}
	if !sawEnter {
		t.Errorf("workflow Steps missing PressEnter; got types: %v", stepTypes(exported.Steps))
	}
}

// TestInferAutoBlockedKeys checks the PF-label regex picks up Exit-style
// labels and ignores navigation-style labels.
func TestInferAutoBlockedKeys(t *testing.T) {
	s := buildScriptedChaosScreen("MENU", false)
	// Write a help legend at the bottom of the screen.
	writeRow := func(y int, text string) {
		for i, r := range []rune(text) {
			if i >= s.Width {
				break
			}
			s.Buffer[y][i] = r
		}
	}
	writeRow(s.Height-2, "PF1 Help    PF3 Exit    PF8 Forward")
	// Bottom-row text must be inside a protected field for the help-line
	// scanner to read it cleanly; otherwise unprotected attribute bytes
	// would override the read.
	s.Fields = append(s.Fields, host.NewField(s, host.AttrProtected, 0, s.Height-2, s.Width-1, s.Height-2, host.AttrColDefault, host.AttrEhDefault))

	blocked := inferAutoBlockedKeys(s)
	if len(blocked) != 1 {
		t.Fatalf("inferAutoBlockedKeys = %v, want [PF(3)]", blocked)
	}
	if !strings.Contains(blocked[0], "3") {
		t.Errorf("blocked key = %q, expected to match PF3", blocked[0])
	}

	known := inferAutoNavigationKeys(s)
	if len(known) < 2 {
		t.Fatalf("inferAutoNavigationKeys = %v, want at least PF1 (Help) and PF8 (Forward)", known)
	}
}

// loopingChaosHost cycles between supplied screens on each SendKey so the
// chaos engine sees a stable ping-pong pattern useful for saturation tests.
type loopingChaosHost struct {
	screens   []*host.Screen
	index     int
	connected bool
}

func (h *loopingChaosHost) Start() error                        { h.connected = true; return nil }
func (h *loopingChaosHost) Stop() error                         { h.connected = false; return nil }
func (h *loopingChaosHost) IsConnected() bool                   { return h.connected }
func (h *loopingChaosHost) UpdateScreen() error                 { return nil }
func (h *loopingChaosHost) MoveCursor(row, col int) error       { return nil }
func (h *loopingChaosHost) SubmitScreen() error                 { return nil }
func (h *loopingChaosHost) SubmitUnformatted(data string) error { return nil }
func (h *loopingChaosHost) SubmitOperatorInput(edit func(*host.Screen) string) error {
	if edit != nil {
		edit(h.GetScreenSnapshot())
	}
	return nil
}
func (h *loopingChaosHost) WriteStringAt(int, int, string) error {
	return nil
}
func (h *loopingChaosHost) PrintText(string) (string, error) {
	if s := h.GetScreenSnapshot(); s != nil {
		return s.Text(), nil
	}
	return "", nil
}
func (h *loopingChaosHost) Query(string) (string, error) { return "", nil }
func (h *loopingChaosHost) GetScreenSnapshot() *host.Screen {
	if len(h.screens) == 0 {
		return nil
	}
	return h.screens[h.index%len(h.screens)]
}
func (h *loopingChaosHost) SendKey(string) error {
	if len(h.screens) > 0 {
		h.index = (h.index + 1) % len(h.screens)
	}
	return nil
}
