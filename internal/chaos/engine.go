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
	FromHash string                 `json:"fromHash"`
	ToHash   string                 `json:"toHash"`
	Steps    []session.WorkflowStep `json:"steps"`
}

// Status is a snapshot of the engine's current state.
type Status struct {
	Active         bool           `json:"active"`
	StepsRun       int            `json:"stepsRun"`
	StartedAt      time.Time      `json:"startedAt,omitempty"`
	StoppedAt      time.Time      `json:"stoppedAt,omitempty"`
	Transitions    int            `json:"transitions"`
	UniqueScreens  int            `json:"uniqueScreens"`
	UniqueInputs   int            `json:"uniqueInputs"`
	AIDKeyCounts   map[string]int `json:"aidKeyCounts,omitempty"`
	LoadedRunID    string         `json:"loadedRunID,omitempty"`
	LastAttempt    *Attempt       `json:"lastAttempt,omitempty"`
	RecentAttempts []Attempt      `json:"recentAttempts,omitempty"`
	MindMap        *MindMap       `json:"mindMap,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// AttemptFieldWrite captures one field write operation attempted by chaos
// during a single step.
type AttemptFieldWrite struct {
	Row     int    `json:"row"`
	Column  int    `json:"column"`
	Length  int    `json:"length"`
	Value   string `json:"value,omitempty"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Attempt captures granular details for a single chaos submission cycle:
// field writes, selected AID key, transition result, and any terminal error.
type Attempt struct {
	Attempt        int                 `json:"attempt"`
	Time           time.Time           `json:"time"`
	FromHash       string              `json:"fromHash,omitempty"`
	ToHash         string              `json:"toHash,omitempty"`
	AIDKey         string              `json:"aidKey,omitempty"`
	FieldsTargeted int                 `json:"fieldsTargeted"`
	FieldsWritten  int                 `json:"fieldsWritten"`
	Transitioned   bool                `json:"transitioned"`
	Error          string              `json:"error,omitempty"`
	FieldWrites    []AttemptFieldWrite `json:"fieldWrites,omitempty"`
}

const maxRecentAttempts = 40

// Engine is the chaos exploration engine. It runs a loop that reads the
// current 3270 screen, fills unprotected fields with random values, and
// submits a randomly chosen AID key. Observed state transitions and
// individual workflow steps are accumulated and can be exported as a
// workflow JSON compatible with the existing playback system.
type Engine struct {
	cfg Config
	h   host.Host
	rng *rand.Rand

	mu           sync.Mutex
	active       bool
	stopCh       chan struct{}
	stepsRun     int
	startedAt    time.Time
	stoppedAt    time.Time
	lastErr      string
	transitions  []Transition
	steps        []session.WorkflowStep
	screenHashes map[string]bool
	uniqueInputs map[string]bool
	aidKeyCounts map[string]int
	loadedRunID  string
	attempts     []Attempt
	mindMap      *MindMap

	hintTransactions []string
	hintKnownData    []string
}

// New creates a new Engine with the given host and configuration.
func New(h host.Host, cfg Config) *Engine {
	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	hintTransactions, hintKnownData := normalizeHints(cfg.Hints)
	return &Engine{
		cfg:              cfg,
		h:                h,
		rng:              rand.New(rand.NewSource(seed)), //nolint:gosec
		stopCh:           make(chan struct{}),
		hintTransactions: hintTransactions,
		hintKnownData:    hintKnownData,
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
	e.screenHashes = make(map[string]bool)
	e.uniqueInputs = make(map[string]bool)
	e.aidKeyCounts = make(map[string]int)
	e.loadedRunID = ""
	e.attempts = nil
	e.mindMap = newMindMap()
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

	aidCopy := make(map[string]int, len(e.aidKeyCounts))
	for k, v := range e.aidKeyCounts {
		aidCopy[k] = v
	}
	attempts := make([]Attempt, len(e.attempts))
	copy(attempts, e.attempts)
	var lastAttempt *Attempt
	if n := len(attempts); n > 0 {
		latest := attempts[n-1]
		lastAttempt = &latest
	}
	mindMap := e.mindMap.clone()
	return Status{
		Active:         e.active,
		StepsRun:       e.stepsRun,
		StartedAt:      e.startedAt,
		StoppedAt:      e.stoppedAt,
		Transitions:    len(e.transitions),
		UniqueScreens:  len(e.screenHashes),
		UniqueInputs:   len(e.uniqueInputs),
		AIDKeyCounts:   aidCopy,
		LoadedRunID:    e.loadedRunID,
		LastAttempt:    lastAttempt,
		RecentAttempts: attempts,
		MindMap:        mindMap,
		Error:          e.lastErr,
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

// Snapshot returns a SavedRun capturing the engine's current accumulated
// state. It is safe to call while the engine is running.
func (e *Engine) Snapshot(runID string) *SavedRun {
	e.mu.Lock()
	defer e.mu.Unlock()

	hashes := make(map[string]bool, len(e.screenHashes))
	for k, v := range e.screenHashes {
		hashes[k] = v
	}
	inputs := make(map[string]bool, len(e.uniqueInputs))
	for k, v := range e.uniqueInputs {
		inputs[k] = v
	}
	aid := make(map[string]int, len(e.aidKeyCounts))
	for k, v := range e.aidKeyCounts {
		aid[k] = v
	}
	transitions := make([]Transition, len(e.transitions))
	copy(transitions, e.transitions)
	steps := make([]session.WorkflowStep, len(e.steps))
	copy(steps, e.steps)
	attempts := make([]Attempt, len(e.attempts))
	copy(attempts, e.attempts)
	mindMap := e.mindMap.clone()

	return &SavedRun{
		SavedRunMeta: SavedRunMeta{
			ID:            runID,
			StartedAt:     e.startedAt,
			StoppedAt:     e.stoppedAt,
			StepsRun:      e.stepsRun,
			Transitions:   len(transitions),
			UniqueScreens: len(hashes),
			UniqueInputs:  len(inputs),
			Error:         e.lastErr,
		},
		ScreenHashes:      hashes,
		TransitionList:    transitions,
		Steps:             steps,
		AIDKeyCounts:      aid,
		UniqueInputValues: inputs,
		Attempts:          attempts,
		MindMap:           mindMap,
	}
}

// Resume starts the engine from a previously saved run, merging the existing
// state (screen hashes, transitions, steps) into the new exploration.
// It returns an error if exploration is already running or the host is not
// connected.
func (e *Engine) Resume(saved *SavedRun) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.active {
		return fmt.Errorf("chaos exploration is already running")
	}
	if !e.h.IsConnected() {
		return fmt.Errorf("not connected to host")
	}

	// Seed state from the saved run.
	e.screenHashes = make(map[string]bool, len(saved.ScreenHashes))
	for k, v := range saved.ScreenHashes {
		e.screenHashes[k] = v
	}
	e.uniqueInputs = make(map[string]bool, len(saved.UniqueInputValues))
	for k, v := range saved.UniqueInputValues {
		e.uniqueInputs[k] = v
	}
	e.aidKeyCounts = make(map[string]int, len(saved.AIDKeyCounts))
	for k, v := range saved.AIDKeyCounts {
		e.aidKeyCounts[k] = v
	}
	e.transitions = make([]Transition, len(saved.TransitionList))
	copy(e.transitions, saved.TransitionList)
	e.steps = make([]session.WorkflowStep, len(saved.Steps))
	copy(e.steps, saved.Steps)
	e.attempts = make([]Attempt, len(saved.Attempts))
	copy(e.attempts, saved.Attempts)
	if e.cfg.ExcludeNoProgressEvents {
		e.attempts = filterProgressAttempts(e.attempts)
	}
	e.stepsRun = saved.StepsRun
	e.loadedRunID = saved.ID
	e.mindMap = saved.MindMap.clone()
	if e.mindMap == nil {
		e.mindMap = newMindMap()
	}

	e.active = true
	e.startedAt = time.Now()
	e.stoppedAt = time.Time{}
	e.lastErr = ""
	e.stopCh = make(chan struct{})

	go e.run()
	return nil
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
		attempt := Attempt{
			Attempt:  steps + 1,
			Time:     time.Now(),
			FromHash: currentHash,
		}

		// Fill unprotected fields with random values.
		var batchSteps []session.WorkflowStep
		fields := unprotectedFields(screen)
		attempt.FieldsTargeted = len(fields)

		for idx, f := range fields {
			value := e.generateValueForField(f, idx == 0)
			if value == "" {
				continue
			}
			fieldAttempt := AttemptFieldWrite{
				Row:    f.StartY + 1,
				Column: f.StartX + 1,
				Length: len(value),
				Value:  value,
			}
			if err := e.h.WriteStringAt(f.StartY, f.StartX, value); err != nil {
				// Non-fatal: skip this field.
				fieldAttempt.Error = err.Error()
				attempt.FieldWrites = append(attempt.FieldWrites, fieldAttempt)
				continue
			}
			fieldAttempt.Success = true
			attempt.FieldWrites = append(attempt.FieldWrites, fieldAttempt)
			attempt.FieldsWritten++
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
		attempt.AIDKey = aidKey
		if err := e.h.SendKey(aidKey); err != nil {
			attempt.Error = err.Error()
			e.mu.Lock()
			e.lastErr = err.Error()
			e.observeMindMapAreaLocked(currentHash, screen, attempt.Time)
			e.recordMindMapAttemptLocked(attempt)
			e.appendAttemptLocked(attempt)
			e.mu.Unlock()
			return
		}
		batchSteps = append(batchSteps, session.WorkflowStep{Type: aidKeyToStepType(aidKey)})

		// Refresh the screen after the key press.
		if err := e.h.UpdateScreen(); err != nil {
			attempt.Error = err.Error()
			e.mu.Lock()
			e.lastErr = err.Error()
			e.observeMindMapAreaLocked(currentHash, screen, attempt.Time)
			e.recordMindMapAttemptLocked(attempt)
			e.appendAttemptLocked(attempt)
			e.mu.Unlock()
			return
		}
		newScreen := e.h.GetScreen()
		newHash := ""
		if newScreen != nil {
			newHash = hashScreen(newScreen)
		}
		attempt.ToHash = newHash
		attempt.Transitioned = newHash != "" && newHash != currentHash
		recordAttempt := !e.cfg.ExcludeNoProgressEvents || attempt.Transitioned || attempt.Error != ""

		// Record the step and any state transition.
		e.mu.Lock()
		e.observeMindMapAreaLocked(currentHash, screen, attempt.Time)
		if newHash != "" {
			e.observeMindMapAreaLocked(newHash, newScreen, attempt.Time)
		}
		e.recordMindMapAttemptLocked(attempt)
		e.stepsRun++
		e.steps = append(e.steps, batchSteps...)
		e.screenHashes[currentHash] = true
		if newHash != "" {
			e.screenHashes[newHash] = true
		}
		e.aidKeyCounts[aidKey]++
		for _, bs := range batchSteps {
			if bs.Type == "FillString" && bs.Text != "" {
				e.uniqueInputs[bs.Text] = true
			}
		}
		if newHash != "" && newHash != currentHash {
			e.transitions = append(e.transitions, Transition{
				FromHash: currentHash,
				ToHash:   newHash,
				Steps:    batchSteps,
			})
		}
		if recordAttempt {
			e.appendAttemptLocked(attempt)
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

func (e *Engine) observeMindMapAreaLocked(hash string, screen *host.Screen, seenAt time.Time) {
	if e.mindMap == nil {
		e.mindMap = newMindMap()
	}
	e.mindMap.observeScreen(hash, screen, seenAt)
}

func (e *Engine) recordMindMapAttemptLocked(attempt Attempt) {
	if e.mindMap == nil {
		e.mindMap = newMindMap()
	}
	e.mindMap.recordAttempt(attempt)
}

func (e *Engine) appendAttemptLocked(attempt Attempt) {
	e.attempts = append(e.attempts, attempt)
	if len(e.attempts) > maxRecentAttempts {
		e.attempts = e.attempts[len(e.attempts)-maxRecentAttempts:]
	}
}

func filterProgressAttempts(attempts []Attempt) []Attempt {
	if len(attempts) == 0 {
		return attempts
	}
	filtered := make([]Attempt, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.Transitioned || attempt.Error != "" {
			filtered = append(filtered, attempt)
		}
	}
	return filtered
}

func normalizeHints(hints []Hint) ([]string, []string) {
	if len(hints) == 0 {
		return nil, nil
	}
	transactions := make([]string, 0, len(hints))
	knownData := make([]string, len(hints))
	knownData = knownData[:0]
	seenTx := make(map[string]bool)
	seenData := make(map[string]bool)
	for _, hint := range hints {
		tx := strings.TrimSpace(hint.Transaction)
		if tx != "" && !seenTx[tx] {
			transactions = append(transactions, tx)
			seenTx[tx] = true
		}
		for _, raw := range hint.KnownData {
			value := strings.TrimSpace(raw)
			if value == "" || seenData[value] {
				continue
			}
			knownData = append(knownData, value)
			seenData[value] = true
		}
	}
	return transactions, knownData
}

func (e *Engine) generateValueForField(f *host.Field, preferTransaction bool) string {
	if hinted := e.hintValueForField(f, preferTransaction); hinted != "" {
		return hinted
	}
	return e.generateValue(f)
}

func (e *Engine) hintValueForField(f *host.Field, preferTransaction bool) string {
	if len(e.hintTransactions) == 0 && len(e.hintKnownData) == 0 {
		return ""
	}
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

	var candidate string
	if preferTransaction && len(e.hintTransactions) > 0 && e.rng.Intn(100) < 75 {
		candidate = e.hintTransactions[e.rng.Intn(len(e.hintTransactions))]
	}
	if candidate == "" {
		pool := e.hintKnownData
		if len(pool) == 0 {
			pool = e.hintTransactions
		}
		if len(pool) > 0 {
			candidate = pool[e.rng.Intn(len(pool))]
		}
	}
	return fitHintValueForField(candidate, length, f.IsNumeric())
}

func fitHintValueForField(candidate string, maxLen int, numeric bool) string {
	if maxLen <= 0 {
		return ""
	}
	value := strings.TrimSpace(candidate)
	if value == "" {
		return ""
	}
	if numeric {
		digits := make([]rune, 0, len(value))
		for _, c := range value {
			if c >= '0' && c <= '9' {
				digits = append(digits, c)
			}
		}
		if len(digits) == 0 {
			return ""
		}
		value = string(digits)
	}
	runes := []rune(value)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return value
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
