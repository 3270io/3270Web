package chaos

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// Transition records a state change observed during exploration.
type Transition struct {
	FromHash string                  `json:"fromHash"`
	ToHash   string                  `json:"toHash"`
	Steps    []session.WorkflowStep  `json:"steps"`
}

// Status is a snapshot of the engine's current state.
type Status struct {
	Active      bool      `json:"active"`
	StepsRun    int       `json:"stepsRun"`
	StartedAt   time.Time `json:"startedAt,omitempty"`
	StoppedAt   time.Time `json:"stoppedAt,omitempty"`
	Transitions int       `json:"transitions"`
	Error       string    `json:"error,omitempty"`
}

// Engine is the chaos exploration engine. It runs a loop that reads the
// current 3270 screen, fills unprotected fields with random values, and
// submits a randomly chosen AID key. Observed state transitions and
// individual workflow steps are accumulated and can be exported as a
// workflow JSON compatible with the existing playback system.
type Engine struct {
	cfg    Config
	h      host.Host
	rng    *rand.Rand

	mu          sync.Mutex
	active      bool
	stopCh      chan struct{}
	stepsRun    int
	startedAt   time.Time
	stoppedAt   time.Time
	lastErr     string
	transitions []Transition
	steps       []session.WorkflowStep
}

// New creates a new Engine with the given host and configuration.
func New(h host.Host, cfg Config) *Engine {
	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return &Engine{
		cfg:    cfg,
		h:      h,
		rng:    rand.New(rand.NewSource(seed)), //nolint:gosec
		stopCh: make(chan struct{}),
	}
}

// Start begins chaos exploration in a background goroutine.
// It returns an error if exploration is already running or the host is not
// connected.
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.active {
		return fmt.Errorf("chaos exploration is already running")
	}
	if !e.h.IsConnected() {
		return fmt.Errorf("not connected to host")
	}

	e.active = true
	e.startedAt = time.Now()
	e.stoppedAt = time.Time{}
	e.stepsRun = 0
	e.transitions = nil
	e.steps = nil
	e.lastErr = ""
	e.stopCh = make(chan struct{})

	go e.run()
	return nil
}

// Stop signals the engine to halt after the current step completes.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.active {
		return
	}
	close(e.stopCh)
}

// Status returns a snapshot of the current engine state.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()

	return Status{
		Active:      e.active,
		StepsRun:    e.stepsRun,
		StartedAt:   e.startedAt,
		StoppedAt:   e.stoppedAt,
		Transitions: len(e.transitions),
		Error:       e.lastErr,
	}
}

// exportedWorkflow is the JSON shape expected by the existing workflow loader.
type exportedWorkflow struct {
	Host  string                 `json:"Host"`
	Port  int                    `json:"Port"`
	Steps []session.WorkflowStep `json:"Steps"`
}

// ExportWorkflow returns the learned workflow as indented JSON that is
// compatible with the existing WorkflowConfig format.
func (e *Engine) ExportWorkflow(hostName string, port int) ([]byte, error) {
	e.mu.Lock()
	steps := make([]session.WorkflowStep, len(e.steps))
	copy(steps, e.steps)
	e.mu.Unlock()

	return json.MarshalIndent(exportedWorkflow{
		Host:  hostName,
		Port:  port,
		Steps: steps,
	}, "", "  ")
}

// run is the main exploration loop executed in a goroutine.
func (e *Engine) run() {
	defer func() {
		e.mu.Lock()
		e.active = false
		e.stoppedAt = time.Now()
		outputFile := e.cfg.OutputFile
		e.mu.Unlock()

		if outputFile != "" {
			if data, err := e.ExportWorkflow("", 0); err == nil {
				if dir := filepath.Dir(outputFile); dir != "" {
					_ = os.MkdirAll(dir, 0750)
				}
				_ = os.WriteFile(outputFile, data, 0600)
			}
		}
	}()

	var deadline time.Time
	if e.cfg.TimeBudget > 0 {
		deadline = time.Now().Add(e.cfg.TimeBudget)
	}

	for {
		// Check for stop signal.
		select {
		case <-e.stopCh:
			return
		default:
		}

		// Check step and time limits.
		e.mu.Lock()
		steps := e.stepsRun
		e.mu.Unlock()

		if e.cfg.MaxSteps > 0 && steps >= e.cfg.MaxSteps {
			return
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return
		}

		// Read the current screen state.
		if err := e.h.UpdateScreen(); err != nil {
			e.mu.Lock()
			e.lastErr = err.Error()
			e.mu.Unlock()
			return
		}
		screen := e.h.GetScreen()
		if screen == nil {
			return
		}

		currentHash := hashScreen(screen)

		// Fill unprotected fields with random values.
		var batchSteps []session.WorkflowStep
		fields := unprotectedFields(screen)

		for _, f := range fields {
			value := e.generateValue(f)
			if value == "" {
				continue
			}
			if err := e.h.WriteStringAt(f.StartY, f.StartX, value); err != nil {
				// Non-fatal: skip this field.
				continue
			}
			batchSteps = append(batchSteps, session.WorkflowStep{
				Type: "FillString",
				Coordinates: &session.WorkflowCoordinates{
					Row:    f.StartY + 1, // workflow uses 1-based coordinates
					Column: f.StartX + 1,
				},
				Text: value,
			})
		}

		// Choose and send an AID key.
		aidKey := e.chooseAIDKey()
		if err := e.h.SendKey(aidKey); err != nil {
			e.mu.Lock()
			e.lastErr = err.Error()
			e.mu.Unlock()
			return
		}
		batchSteps = append(batchSteps, session.WorkflowStep{Type: aidKeyToStepType(aidKey)})

		// Refresh the screen after the key press.
		if err := e.h.UpdateScreen(); err != nil {
			e.mu.Lock()
			e.lastErr = err.Error()
			e.mu.Unlock()
			return
		}
		newScreen := e.h.GetScreen()
		newHash := ""
		if newScreen != nil {
			newHash = hashScreen(newScreen)
		}

		// Record the step and any state transition.
		e.mu.Lock()
		e.stepsRun++
		e.steps = append(e.steps, batchSteps...)
		if newHash != "" && newHash != currentHash {
			e.transitions = append(e.transitions, Transition{
				FromHash: currentHash,
				ToHash:   newHash,
				Steps:    batchSteps,
			})
		}
		e.mu.Unlock()

		// Inter-step delay (cancellable).
		if e.cfg.StepDelay > 0 {
			select {
			case <-e.stopCh:
				return
			case <-time.After(e.cfg.StepDelay):
			}
		}
	}
}

// generateValue produces a random string appropriate for the field's
// type and length constraints.
func (e *Engine) generateValue(f *host.Field) string {
	length := fieldLength(f)
	if length <= 0 {
		return ""
	}
	maxLen := e.cfg.MaxFieldLength
	if maxLen <= 0 {
		maxLen = 40
	}
	if length > maxLen {
		length = maxLen
	}

	if f.IsNumeric() {
		const digits = "0123456789"
		b := make([]byte, length)
		for i := range b {
			b[i] = digits[e.rng.Intn(len(digits))]
		}
		return string(b)
	}

	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789 "
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[e.rng.Intn(len(chars))]
	}
	return string(b)
}

// chooseAIDKey selects an AID key using the configured weights.
func (e *Engine) chooseAIDKey() string {
	weights := e.cfg.AIDKeyWeights
	if len(weights) == 0 {
		return "Enter"
	}

	total := 0
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return "Enter"
	}

	pick := e.rng.Intn(total)
	cum := 0
	for key, w := range weights {
		cum += w
		if pick < cum {
			return key
		}
	}
	return "Enter"
}

// hashScreen produces a short stable fingerprint of the screen state.
func hashScreen(s *host.Screen) string {
	if s == nil {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d,%d|%d", s.Text(), s.CursorX, s.CursorY, len(s.Fields))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// unprotectedFields returns all input (non-protected) fields from the screen.
func unprotectedFields(s *host.Screen) []*host.Field {
	var result []*host.Field
	for _, f := range s.Fields {
		if !f.IsProtected() {
			result = append(result, f)
		}
	}
	return result
}

// fieldLength returns the maximum number of characters that fit in f.
func fieldLength(f *host.Field) int {
	if f.StartY == f.EndY {
		return f.EndX - f.StartX + 1
	}
	// Multi-line field: count cells from start to end.
	s := f.Screen
	if s == nil || s.Width <= 0 {
		return 0
	}
	total := (s.Width - f.StartX) + (f.EndX + 1)
	if f.EndY-f.StartY > 1 {
		total += (f.EndY - f.StartY - 1) * s.Width
	}
	return total
}

// aidKeyToStepType converts an AID key name to the workflow step type used by
// the existing playback system.
func aidKeyToStepType(key string) string {
	switch key {
	case "Enter":
		return "PressEnter"
	case "Clear":
		return "PressClear"
	case "Tab":
		return "PressTab"
	}
	upper := strings.ToUpper(key)
	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		return "PressPF" + inner
	}
	if strings.HasPrefix(upper, "PA(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PA("), ")")
		return "PressPA" + inner
	}
	return "PressEnter"
}
