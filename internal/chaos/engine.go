package chaos

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// minPressesForPenalty is the number of times a key must be pressed from a
// screen without causing any transition before it receives a negative boost.
// Below this threshold the engine gives a key the benefit of the doubt; above
// it the key is progressively de-prioritised in favour of untried keys.
const minPressesForPenalty = 5

// maxProgressionBoostFactor is the maximum number of progressions used when
// computing the per-key boost in chooseAIDKeyBoosted.  Capping this value
// prevents a single successful AID key from monopolising selection after many
// transitions from the same screen, preserving exploration breadth so that
// other configured keys remain tried at a meaningful rate.
const maxProgressionBoostFactor = 20

// Engine is the chaos exploration engine. It runs a loop that reads the
// current 3270 screen, fills unprotected fields with random values, and
// submits a randomly chosen AID key. Observed state transitions and
// individual workflow steps are accumulated and can be exported as a
// workflow JSON compatible with the existing playback system.
type Engine struct {
	cfg Config
	h   host.Host
	rng *rand.Rand

	mu             sync.Mutex
	active         bool
	stopCh         chan struct{}
	stepsRun       int
	startedAt      time.Time
	stoppedAt      time.Time
	lastErr        string
	transitions    []Transition
	steps          []session.WorkflowStep
	screenHashes   map[string]bool
	uniqueInputs   map[string]bool
	aidKeyCounts   map[string]int
	loadedRunID    string
	attempts       []Attempt
	mindMap        *MindMap
	workflowHeader *WorkflowHeader

	hintTransactions []string
	hintKnownData    []string
	defaultHintTx    []string
	defaultHintData  []string
	hintKeyMappings  map[string]string
	screenHints      map[string]ScreenHint
	blacklistedKeys  map[string]struct{}
}

// New creates a new Engine with the given host and configuration.
func New(h host.Host, cfg Config) *Engine {
	seed := cfg.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	defaultTx, defaultData, defaultKeyMappings := normalizeHints(defaultChaosHints())
	hintTransactions, hintKnownData, userKeyMappings := normalizeHints(cfg.Hints)
	hintKeyMappings := make(map[string]string)
	for k, v := range defaultKeyMappings {
		hintKeyMappings[k] = v
	}
	for k, v := range userKeyMappings {
		hintKeyMappings[k] = v
	}
	if len(hintKeyMappings) == 0 {
		hintKeyMappings = nil
	}
	return &Engine{
		cfg:              cfg,
		h:                h,
		rng:              rand.New(rand.NewSource(seed)), //nolint:gosec
		stopCh:           make(chan struct{}),
		workflowHeader:   workflowHeaderFromConfig(cfg),
		hintTransactions: hintTransactions,
		hintKnownData:    hintKnownData,
		defaultHintTx:    defaultTx,
		defaultHintData:  defaultData,
		hintKeyMappings:  hintKeyMappings,
		screenHints:      cloneScreenHints(cfg.ScreenHints),
		blacklistedKeys:  normalizeChaosKeySet(cfg.KeyBlacklist),
	}
}

// SetScreenHints replaces all screen-scoped hints. Safe to call while the
// engine is running.
func (e *Engine) SetScreenHints(hints map[string]ScreenHint) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.screenHints = cloneScreenHints(hints)
}

// SetScreenHint upserts (or removes) a single screen-scoped hint. Safe to call
// while the engine is running.
func (e *Engine) SetScreenHint(hash string, hint ScreenHint) {
	if e == nil {
		return
	}
	key := strings.TrimSpace(hash)
	if key == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.screenHints == nil {
		e.screenHints = make(map[string]ScreenHint)
	}
	clean := sanitizeScreenHint(hint)
	if len(clean.KnownData) == 0 && len(clean.KnownKeys) == 0 && len(clean.KeyAssignments) == 0 {
		delete(e.screenHints, key)
		return
	}
	e.screenHints[key] = clean
}

// ScreenHints returns a defensive copy of the current screen hints.
func (e *Engine) ScreenHints() map[string]ScreenHint {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return cloneScreenHints(e.screenHints)
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
	e.workflowHeader = workflowHeaderFromConfig(e.cfg)
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
	select {
	case <-e.stopCh:
		// already closed
	default:
		close(e.stopCh)
	}
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
	Host            string                      `json:"Host"`
	Port            int                         `json:"Port"`
	EveryStepDelay  *session.WorkflowDelayRange `json:"EveryStepDelay,omitempty"`
	OutputFilePath  string                      `json:"OutputFilePath,omitempty"`
	RampUpBatchSize int                         `json:"RampUpBatchSize,omitempty"`
	RampUpDelay     float64                     `json:"RampUpDelay,omitempty"`
	EndOfTaskDelay  *session.WorkflowDelayRange `json:"EndOfTaskDelay,omitempty"`
	Steps           []session.WorkflowStep      `json:"Steps"`
	ChaosDiscovery  *WorkflowDiscoveryMetadata  `json:"ChaosDiscovery,omitempty"`
}

// WorkflowDiscoveryMetadata captures chaos learning/discovery metadata that is
// embedded in exported workflow JSON for later inspection. Workflow loaders
// ignore this extra field, but the data is useful for understanding what chaos
// learned (screen/key/field behavior) while generating the workflow.
type WorkflowDiscoveryMetadata struct {
	ExportedAt     time.Time      `json:"exportedAt,omitempty"`
	LoadedRunID    string         `json:"loadedRunID,omitempty"`
	StepsRun       int            `json:"stepsRun"`
	Transitions    int            `json:"transitions"`
	UniqueScreens  int            `json:"uniqueScreens"`
	UniqueInputs   int            `json:"uniqueInputs"`
	AIDKeyCounts   map[string]int `json:"aidKeyCounts,omitempty"`
	RecentAttempts []Attempt      `json:"recentAttempts,omitempty"`
	MindMap        *MindMap       `json:"mindMap,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// ExportWorkflow returns the learned workflow as indented JSON that is
// compatible with the existing WorkflowConfig format.
func (e *Engine) ExportWorkflow(hostName string, port int) ([]byte, error) {
	e.mu.Lock()
	steps := make([]session.WorkflowStep, len(e.steps))
	copy(steps, e.steps)
	header := e.workflowHeader.clone()
	discovery := e.workflowDiscoveryMetadataLocked()
	e.mu.Unlock()

	if hostName == "" {
		hostName = e.cfg.ExportHost
	}
	if port == 0 {
		port = e.cfg.ExportPort
	}

	export := exportedWorkflow{
		Host:           hostName,
		Port:           port,
		Steps:          steps,
		ChaosDiscovery: discovery,
	}
	if header != nil {
		export.EveryStepDelay = cloneWorkflowDelayRange(header.EveryStepDelay)
		export.OutputFilePath = header.OutputFilePath
		export.RampUpBatchSize = header.RampUpBatchSize
		export.RampUpDelay = header.RampUpDelay
		export.EndOfTaskDelay = cloneWorkflowDelayRange(header.EndOfTaskDelay)
	}

	return json.MarshalIndent(export, "", "  ")
}

func (e *Engine) workflowDiscoveryMetadataLocked() *WorkflowDiscoveryMetadata {
	if e == nil {
		return nil
	}
	aid := make(map[string]int, len(e.aidKeyCounts))
	for k, v := range e.aidKeyCounts {
		aid[k] = v
	}
	attempts := make([]Attempt, len(e.attempts))
	copy(attempts, e.attempts)
	mindMap := e.mindMap.clone()
	return &WorkflowDiscoveryMetadata{
		ExportedAt:     time.Now().UTC(),
		LoadedRunID:    e.loadedRunID,
		StepsRun:       e.stepsRun,
		Transitions:    len(e.transitions),
		UniqueScreens:  len(e.screenHashes),
		UniqueInputs:   len(e.uniqueInputs),
		AIDKeyCounts:   aid,
		RecentAttempts: attempts,
		MindMap:        mindMap,
		Error:          e.lastErr,
	}
}

// WorkflowDiscoveryMetadataFromSavedRun builds export metadata from a saved or
// loaded run so /chaos/export includes discovery data even when no engine is active.
func WorkflowDiscoveryMetadataFromSavedRun(run *SavedRun) *WorkflowDiscoveryMetadata {
	if run == nil {
		return nil
	}
	aid := make(map[string]int, len(run.AIDKeyCounts))
	for k, v := range run.AIDKeyCounts {
		aid[k] = v
	}
	attempts := make([]Attempt, len(run.Attempts))
	copy(attempts, run.Attempts)
	return &WorkflowDiscoveryMetadata{
		ExportedAt:     time.Now().UTC(),
		LoadedRunID:    run.ID,
		StepsRun:       run.StepsRun,
		Transitions:    run.Transitions,
		UniqueScreens:  run.UniqueScreens,
		UniqueInputs:   run.UniqueInputs,
		AIDKeyCounts:   aid,
		RecentAttempts: attempts,
		MindMap:        run.MindMap.clone(),
		Error:          run.Error,
	}
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
		WorkflowHeader:    e.workflowHeader.clone(),
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
	if saved == nil {
		return fmt.Errorf("saved run is required")
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
	e.workflowHeader = saved.WorkflowHeader.clone()
	if e.workflowHeader == nil {
		e.workflowHeader = workflowHeaderFromConfig(e.cfg)
	}
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
		fields := e.selectTargetFields(unprotectedFields(screen))
		attempt.FieldsTargeted = len(fields)

		// Snapshot learned data for this screen area under a brief lock so that
		// field writes (which may block on I/O) don't race with the recording
		// code that also holds the lock.
		e.mu.Lock()
		knownValues := e.snapshotAreaValuesLocked(currentHash)
		triedValues := e.snapshotAreaTriedValuesLocked(currentHash)
		keyBoosts := e.snapshotKeyBoostsLocked(currentHash)
		screenHint := e.snapshotScreenHintLocked(currentHash)
		e.mu.Unlock()
		keyBoosts = mergeKeyBoostMaps(keyBoosts, e.hintKeyBoostsForScreen(screen))
		keyBoosts = mergeKeyBoostMaps(keyBoosts, e.screenHintKeyBoostsForScreen(screen, screenHint))
		keyBoosts = mergeKeyBoostMaps(keyBoosts, inferScreenHelpKeyBoosts(screen))

		for idx, f := range fields {
			value := e.generateValueForFieldWith(f, idx == 0, knownValues, triedValues, screenHint)
			if value == "" {
				continue
			}
			if e.cfg.ForceOverrideExistingInputs {
				value = e.prepareFieldWriteValue(f, value)
				if value == "" {
					continue
				}
			}
			fieldAttempt := AttemptFieldWrite{
				Row:    f.StartY + 1,
				Column: f.StartX + 1,
				Length: len(value),
				Value:  value,
			}
			if e.cfg.ForceOverrideExistingInputs {
				if err := e.clearFieldBeforeWrite(f); err != nil {
					fieldAttempt.Error = fmt.Sprintf("clear field: %v", err)
					attempt.FieldWrites = append(attempt.FieldWrites, fieldAttempt)
					continue
				}
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

		// Choose and send an AID key (adaptive: prefer keys that previously
		// caused screen transitions from the current area).
		aidKey := e.chooseAIDKeyBoosted(keyBoosts)
		if isBlacklistedKeyInSet(e.blacklistedKeys, aidKey) {
			aidKey = fallbackChaosKey(e.blacklistedKeys)
		}
		attempt.AIDKey = aidKey
		if strings.TrimSpace(aidKey) == "" {
			attempt.Error = "no non-blacklisted AID key available"
			e.mu.Lock()
			e.lastErr = attempt.Error
			e.observeMindMapAreaLocked(currentHash, screen, attempt.Time)
			e.recordMindMapAttemptLocked(attempt)
			e.appendAttemptLocked(attempt)
			e.mu.Unlock()
			return
		}
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

// snapshotAreaValuesLocked returns a copy of the KnownWorkingValues for the
// given screen hash.  Must be called with e.mu held.
func (e *Engine) snapshotAreaValuesLocked(hash string) map[string][]string {
	if e.mindMap == nil || hash == "" {
		return nil
	}
	area, ok := e.mindMap.Areas[hash]
	if !ok || area == nil || len(area.KnownWorkingValues) == 0 {
		return nil
	}
	out := make(map[string][]string, len(area.KnownWorkingValues))
	for k, vs := range area.KnownWorkingValues {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// snapshotAreaTriedValuesLocked returns a copy of previously written values
// (successful field writes, regardless of transition) for the given screen hash.
// Must be called with e.mu held.
func (e *Engine) snapshotAreaTriedValuesLocked(hash string) map[string][]string {
	if e.mindMap == nil || hash == "" {
		return nil
	}
	area, ok := e.mindMap.Areas[hash]
	if !ok || area == nil || len(area.KnownTriedValues) == 0 {
		return nil
	}
	out := make(map[string][]string, len(area.KnownTriedValues))
	for k, vs := range area.KnownTriedValues {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// snapshotScreenHintLocked returns a copy of the configured screen-scoped hint
// for the given hash. Must be called with e.mu held.
func (e *Engine) snapshotScreenHintLocked(hash string) *ScreenHint {
	if e == nil || len(e.screenHints) == 0 || strings.TrimSpace(hash) == "" {
		return nil
	}
	h, ok := e.screenHints[hash]
	if !ok {
		return nil
	}
	clone := sanitizeScreenHint(h)
	return &clone
}

// snapshotKeyBoostsLocked returns a map of AID key → boost amount derived
// from the MindMap statistics for the given area.  Keys that previously caused
// screen transitions receive a positive boost proportional to their
// progression count.  Keys that have been pressed at least minPressesForPenalty
// times from this screen without ever causing a transition receive a negative
// boost (penalty) to steer the engine toward less-explored alternatives.
// Must be called with e.mu held.
func (e *Engine) snapshotKeyBoostsLocked(hash string) map[string]int {
	if e.mindMap == nil || hash == "" {
		return nil
	}
	area, ok := e.mindMap.Areas[hash]
	if !ok || area == nil || len(area.KeyPresses) == 0 {
		return nil
	}
	boosts := make(map[string]int, len(area.KeyPresses))
	for key, kp := range area.KeyPresses {
		if kp == nil {
			continue
		}
		if kp.Progressions > 0 {
			progressions := kp.Progressions
			if progressions > maxProgressionBoostFactor {
				progressions = maxProgressionBoostFactor
			}
			boosts[key] += progressions * 10
		} else if kp.Presses >= minPressesForPenalty {
			// Penalise keys pressed many times without causing any transition.
			boosts[key] -= kp.Presses
		}
	}
	if len(boosts) == 0 {
		return nil
	}
	return boosts
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

func defaultChaosHints() []Hint {
	return []Hint{
		{
			KnownData: []string{
				"Y", "N", "1", "0", "01", "99", "0001", "1234",
				"TEST", "DEMO", "USER", "ADMIN", "PASS", "HELP",
				"MENU", "MAIN", "LIST", "DETAIL", "SEARCH", "ALL",
				"20240101", "20250101",
			},
			KeyAssignments: map[string]string{
				"HELP":         "PF1",
				"RETURN":       "PF3",
				"BACK":         "PF3",
				"NEXT":         "PF8",
				"PAGE FORWARD": "PF8",
				"PREVIOUS":     "PF7",
				"PREV":         "PF7",
				"PAGE BACK":    "PF7",
				"REFRESH":      "PF5",
				"SELECT":       "Enter",
				"DETAIL":       "Enter",
				"CONFIRM":      "Enter",
				"SUBMIT":       "Enter",
				"ACCEPT":       "Enter",
				"UP":           "Up",
				"DOWN":         "Down",
				"LEFT":         "Left",
				"RIGHT":        "Right",
			},
		},
		{
			Transaction: "MENU",
		},
		{
			Transaction: "HELP",
		},
	}
}

func normalizeHints(hints []Hint) ([]string, []string, map[string]string) {
	if len(hints) == 0 {
		return nil, nil, nil
	}
	transactions := make([]string, 0, len(hints))
	knownData := make([]string, 0, len(hints))
	keyMappings := make(map[string]string)
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
		for rawLabel, rawKey := range hint.KeyAssignments {
			label := normalizeHintAssignmentLabel(rawLabel)
			key := normalizeChaosKeyName(rawKey)
			if label == "" || key == "" {
				continue
			}
			// Last-write-wins lets later hint rows override earlier rows.
			keyMappings[label] = key
		}
	}
	if len(keyMappings) == 0 {
		keyMappings = nil
	}
	return transactions, knownData, keyMappings
}

func normalizeChaosKeySet(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(keys))
	for _, raw := range keys {
		key := normalizeChaosKeyName(raw)
		if key == "" {
			continue
		}
		out[key] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isBlacklistedKeyInSet(blocked map[string]struct{}, key string) bool {
	if len(blocked) == 0 {
		return false
	}
	nKey := normalizeChaosKeyName(key)
	if nKey == "" {
		nKey = strings.TrimSpace(key)
	}
	_, ok := blocked[nKey]
	return ok
}

func fallbackChaosKey(blocked map[string]struct{}) string {
	candidates := []string{"Enter", "PF(1)", "PF(2)", "PF(4)", "PF(7)", "PF(8)", "PF(12)", "Tab"}
	for _, key := range candidates {
		if !isBlacklistedKeyInSet(blocked, key) {
			return key
		}
	}
	return ""
}

func pickHintValueForFieldPool(rng *rand.Rand, pool []string, length int, numeric bool) string {
	if len(pool) == 0 || length <= 0 {
		return ""
	}
	type weightedCandidate struct {
		value  string
		weight int
	}
	candidates := make([]weightedCandidate, 0, len(pool))
	totalWeight := 0
	for _, raw := range pool {
		fitted := fitHintValueForField(raw, length, numeric)
		if fitted == "" {
			continue
		}
		weight := hintCandidateWeight(raw, fitted, length, numeric)
		if weight < 1 {
			weight = 1
		}
		candidates = append(candidates, weightedCandidate{value: fitted, weight: weight})
		totalWeight += weight
	}
	if len(candidates) == 0 || totalWeight <= 0 {
		return ""
	}
	if rng == nil || len(candidates) == 1 {
		return candidates[0].value
	}
	pick := rng.Intn(totalWeight)
	sum := 0
	for _, c := range candidates {
		sum += c.weight
		if pick < sum {
			return c.value
		}
	}
	return candidates[len(candidates)-1].value
}

func hintCandidateWeight(raw, fitted string, length int, numeric bool) int {
	if length <= 0 || fitted == "" {
		return 1
	}
	weight := 1
	fittedLen := len([]rune(fitted))
	diff := length - fittedLen
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff == 0:
		weight += 12
	case diff == 1:
		weight += 8
	case diff == 2:
		weight += 5
	case diff <= 4:
		weight += 2
	}
	if fittedLen > 0 && fittedLen < length {
		weight += 1
	}

	rawTrim := strings.TrimSpace(raw)
	if !numeric && isDigitsOnlyString(rawTrim) {
		// Avoid over-biasing text fields toward all-digit defaults (e.g. dates)
		// purely because they happen to match field length exactly.
		weight -= 4
	}
	if wasHintValueTruncated(rawTrim, fitted, length, numeric) {
		weight -= 2
	}
	if strings.EqualFold(rawTrim, fitted) {
		weight += 1
	}
	if weight < 1 {
		return 1
	}
	return weight
}

func wasHintValueTruncated(raw, fitted string, length int, numeric bool) bool {
	if length <= 0 {
		return false
	}
	if numeric {
		digitCount := 0
		for _, c := range raw {
			if c >= '0' && c <= '9' {
				digitCount++
			}
		}
		return digitCount > len([]rune(fitted))
	}
	return len([]rune(strings.TrimSpace(raw))) > len([]rune(fitted))
}

func isDigitsOnlyString(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func inferScreenHelpKeyBoosts(screen *host.Screen) map[string]int {
	assignments := inferScreenHelpKeyAssignments(screen)
	if len(assignments) == 0 {
		return nil
	}
	boosts := make(map[string]int)
	for label, key := range assignments {
		if key == "" {
			continue
		}
		boosts[key] += chaosHelpLabelBoost(label)
	}
	if len(boosts) == 0 {
		return nil
	}
	return boosts
}

func inferScreenHelpKeyAssignments(screen *host.Screen) map[string]string {
	if screen == nil || screen.Height <= 0 {
		return nil
	}
	startRow := screen.Height - 4
	if startRow < 0 {
		startRow = 0
	}
	assignments := make(map[string]string)
	for y := startRow; y < screen.Height; y++ {
		line := strings.TrimSpace(screenRowText(screen, y))
		if line == "" {
			continue
		}
		for label, key := range extractKeyAssignmentsFromHelpLine(line) {
			if label == "" || key == "" {
				continue
			}
			if _, exists := assignments[label]; exists {
				continue
			}
			assignments[label] = key
		}
	}
	if len(assignments) == 0 {
		return nil
	}
	return assignments
}

func screenRowText(screen *host.Screen, y int) string {
	if screen == nil || y < 0 || y >= screen.Height {
		return ""
	}
	width := screen.Width
	if width <= 0 && y >= 0 && y < len(screen.Buffer) {
		width = len(screen.Buffer[y])
	}
	if width <= 0 {
		return ""
	}
	row := make([]rune, width)
	for x := 0; x < width; x++ {
		ch := screen.CharAt(x, y)
		if ch == 0 {
			ch = ' '
		}
		row[x] = ch
	}
	return string(row)
}

func extractKeyAssignmentsFromHelpLine(line string) map[string]string {
	tokens := strings.Fields(line)
	if len(tokens) == 0 {
		return nil
	}
	assignments := make(map[string]string)
	var currentKey string
	labelParts := make([]string, 0, 4)
	flush := func() {
		if currentKey == "" {
			labelParts = labelParts[:0]
			return
		}
		label := normalizeHintAssignmentLabel(strings.Join(labelParts, " "))
		if label != "" {
			assignments[label] = currentKey
		}
		currentKey = ""
		labelParts = labelParts[:0]
	}
	foundKey := false
	for _, tok := range tokens {
		if key, inlineLabel, ok := parseHelpKeyToken(tok); ok {
			flush()
			currentKey = key
			foundKey = true
			if inlineLabel != "" {
				labelParts = append(labelParts, inlineLabel)
			}
			continue
		}
		if currentKey == "" {
			continue
		}
		part := strings.TrimSpace(strings.Trim(tok, "-=:|[]()"))
		if part == "" {
			continue
		}
		labelParts = append(labelParts, part)
	}
	flush()
	if !foundKey || len(assignments) == 0 {
		return nil
	}
	return assignments
}

func parseHelpKeyToken(token string) (key, inlineLabel string, ok bool) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return "", "", false
	}
	trimmed = strings.Trim(trimmed, "|")

	separators := []string{"=", ":", "-"}
	for _, sep := range separators {
		if idx := strings.Index(trimmed, sep); idx > 0 {
			left := strings.TrimSpace(trimmed[:idx])
			right := strings.TrimSpace(trimmed[idx+1:])
			if k := normalizeChaosKeyName(left); isCommonHelpKey(k) {
				return k, strings.TrimSpace(strings.Trim(right, "-=:|[]()")), true
			}
		}
	}

	if k := normalizeChaosKeyName(trimmed); isCommonHelpKey(k) {
		return k, "", true
	}
	return "", "", false
}

func isCommonHelpKey(key string) bool {
	switch {
	case key == "":
		return false
	case strings.HasPrefix(key, "PF("):
		return true
	case strings.HasPrefix(key, "PA("):
		return true
	}
	switch key {
	case "Enter", "Clear", "Tab", "BackTab", "Up", "Down", "Left", "Right":
		return true
	default:
		return false
	}
}

func chaosHelpLabelBoost(label string) int {
	n := normalizeHintAssignmentLabel(label)
	if n == "" {
		return 0
	}
	if strings.Contains(n, "LOGOFF") || strings.Contains(n, "LOGOUT") ||
		strings.Contains(n, "SIGNOFF") || strings.Contains(n, "SIGN OUT") ||
		strings.Contains(n, "DISCONNECT") {
		return -200
	}
	if strings.Contains(n, "EXIT") || strings.Contains(n, "QUIT") ||
		strings.Contains(n, "CANCEL") || strings.Contains(n, "TERMINATE") {
		return -40
	}
	if strings.Contains(n, "HELP") {
		return 45
	}
	if strings.Contains(n, "NEXT") || strings.Contains(n, "FORWARD") ||
		strings.Contains(n, "MORE") || strings.Contains(n, "CONTINUE") ||
		strings.Contains(n, "SELECT") || strings.Contains(n, "DETAIL") ||
		strings.Contains(n, "OPEN") || strings.Contains(n, "SUBMIT") ||
		strings.Contains(n, "CONFIRM") || strings.Contains(n, "ENTER") {
		return 70
	}
	if strings.Contains(n, "PAGE") || strings.Contains(n, "SCROLL") ||
		strings.Contains(n, "UP") || strings.Contains(n, "DOWN") ||
		strings.Contains(n, "LEFT") || strings.Contains(n, "RIGHT") {
		return 50
	}
	return 20
}

func mergeKeyBoostMaps(base, extra map[string]int) map[string]int {
	if len(extra) == 0 {
		return base
	}
	if len(base) == 0 {
		out := make(map[string]int, len(extra))
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	out := make(map[string]int, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] += v
	}
	return out
}

func (e *Engine) hintKeyBoostsForScreen(screen *host.Screen) map[string]int {
	if e == nil || screen == nil || len(e.hintKeyMappings) == 0 {
		return nil
	}
	text := normalizeHintAssignmentLabel(screen.Text())
	if text == "" {
		return nil
	}
	boosts := make(map[string]int)
	for label, key := range e.hintKeyMappings {
		if label == "" || key == "" {
			continue
		}
		if strings.Contains(text, label) {
			// Strongly prefer keys explicitly mapped to labels present on screen,
			// while still allowing exploration through weighted randomness.
			boosts[key] += 100
		}
	}
	if len(boosts) == 0 {
		return nil
	}
	return boosts
}

func (e *Engine) screenHintKeyBoostsForScreen(screen *host.Screen, screenHint *ScreenHint) map[string]int {
	if e == nil || screenHint == nil {
		return nil
	}
	boosts := make(map[string]int)
	for _, rawKey := range screenHint.KnownKeys {
		key := normalizeChaosKeyName(rawKey)
		if key == "" {
			continue
		}
		boosts[key] += 120
	}
	if len(screenHint.KeyAssignments) == 0 || screen == nil {
		if len(boosts) == 0 {
			return nil
		}
		return boosts
	}
	text := normalizeHintAssignmentLabel(screen.Text())
	if text == "" {
		if len(boosts) == 0 {
			return nil
		}
		return boosts
	}
	for label, key := range screenHint.KeyAssignments {
		nLabel := normalizeHintAssignmentLabel(label)
		nKey := normalizeChaosKeyName(key)
		if nLabel == "" || nKey == "" {
			continue
		}
		if strings.Contains(text, nLabel) {
			boosts[nKey] += 140
		}
	}
	if len(boosts) == 0 {
		return nil
	}
	return boosts
}

func normalizeHintAssignmentLabel(value string) string {
	fields := strings.Fields(strings.ToUpper(strings.TrimSpace(value)))
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func normalizeChaosKeyName(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	if strings.ContainsAny(trimmed, "\n\r\t;") {
		return ""
	}

	upper := strings.ToUpper(trimmed)
	lower := strings.ToLower(trimmed)

	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 24 {
			return fmt.Sprintf("PF(%d)", n)
		}
		return ""
	}
	if strings.HasPrefix(upper, "PA(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PA("), ")")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 3 {
			return fmt.Sprintf("PA(%d)", n)
		}
		return ""
	}
	if strings.HasPrefix(upper, "PF") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "PF")); err == nil && n >= 1 && n <= 24 {
			return fmt.Sprintf("PF(%d)", n)
		}
	}
	if strings.HasPrefix(upper, "PA") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "PA")); err == nil && n >= 1 && n <= 3 {
			return fmt.Sprintf("PA(%d)", n)
		}
	}
	if strings.HasPrefix(upper, "F") {
		if n, err := strconv.Atoi(strings.TrimPrefix(upper, "F")); err == nil && n >= 1 && n <= 24 {
			return fmt.Sprintf("PF(%d)", n)
		}
	}

	switch lower {
	case "enter":
		return "Enter"
	case "tab":
		return "Tab"
	case "backtab":
		return "BackTab"
	case "clear":
		return "Clear"
	case "reset":
		return "Reset"
	case "eraseeof", "erase_eof":
		return "EraseEOF"
	case "eraseinput", "erase_input":
		return "EraseInput"
	case "dup":
		return "Dup"
	case "fieldmark", "field_mark":
		return "FieldMark"
	case "sysreq", "sys_req":
		return "SysReq"
	case "attn":
		return "Attn"
	case "newline", "new_line":
		return "Newline"
	case "backspace":
		return "BackSpace"
	case "delete":
		return "Delete"
	case "insert":
		return "Insert"
	case "home":
		return "Home"
	case "up":
		return "Up"
	case "down":
		return "Down"
	case "left":
		return "Left"
	case "right":
		return "Right"
	}

	// Allow unknown but sanitized key names to pass through for host-specific
	// keys. These may not round-trip to workflow steps if unsupported.
	return trimmed
}

func cloneScreenHints(in map[string]ScreenHint) map[string]ScreenHint {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ScreenHint, len(in))
	for hash, hint := range in {
		key := strings.TrimSpace(hash)
		if key == "" {
			continue
		}
		clean := sanitizeScreenHint(hint)
		if len(clean.KnownData) == 0 && len(clean.KnownKeys) == 0 && len(clean.KeyAssignments) == 0 {
			continue
		}
		out[key] = clean
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeScreenHint(h ScreenHint) ScreenHint {
	knownData := make([]string, 0, len(h.KnownData))
	seenData := make(map[string]bool)
	for _, raw := range h.KnownData {
		v := strings.TrimSpace(raw)
		if v == "" || seenData[v] {
			continue
		}
		seenData[v] = true
		knownData = append(knownData, v)
	}
	knownKeys := make([]string, 0, len(h.KnownKeys))
	seenKeys := make(map[string]bool)
	for _, raw := range h.KnownKeys {
		k := normalizeChaosKeyName(raw)
		if k == "" || seenKeys[k] {
			continue
		}
		seenKeys[k] = true
		knownKeys = append(knownKeys, k)
	}
	assignments := make(map[string]string)
	for rawLabel, rawKey := range h.KeyAssignments {
		label := strings.TrimSpace(rawLabel)
		key := normalizeChaosKeyName(rawKey)
		if label == "" || key == "" {
			continue
		}
		assignments[label] = key
	}
	if len(assignments) == 0 {
		assignments = nil
	}
	return ScreenHint{
		KnownData:      knownData,
		KnownKeys:      knownKeys,
		KeyAssignments: assignments,
	}
}

func (e *Engine) generateValueForField(f *host.Field, preferTransaction bool) string {
	return e.generateValueForFieldWith(f, preferTransaction, nil, nil, nil)
}

// generateValueForFieldWith extends generateValueForField with optional
// per-screen known-working values learned from previous transitions. Callers
// that hold the engine lock must snapshot area values before calling; this
// function must not touch e.mindMap directly.
func (e *Engine) generateValueForFieldWith(f *host.Field, preferTransaction bool, knownValues map[string][]string, triedValues map[string][]string, screenHint *ScreenHint) string {
	// 1. Prefer values already known to work on this screen / field position.
	if len(knownValues) > 0 {
		row := f.StartY + 1
		col := f.StartX + 1
		length := fieldLength(f)
		if length > 0 {
			key := mindMapFieldKey(row, col, length)
			if values, ok := knownValues[key]; ok && len(values) > 0 {
				// Use a known working value 80 % of the time so that the engine
				// still occasionally explores new inputs rather than repeating.
				if e.rng.Intn(100) < 80 {
					if v := pickHintValueForFieldPool(e.rng, values, length, f.IsNumeric()); v != "" {
						return v
					}
				}
			}
		}
	}
	// 2. Reuse previously tried values for this screen/field some of the time.
	// This helps chaos continue functional discovery without requiring a prior
	// successful transition for every field.
	if len(triedValues) > 0 {
		row := f.StartY + 1
		col := f.StartX + 1
		length := fieldLength(f)
		if length > 0 {
			key := mindMapFieldKey(row, col, length)
			if values, ok := triedValues[key]; ok && len(values) > 0 {
				// Lower probability than known-working values to preserve breadth.
				if e.rng.Intn(100) < 35 {
					if v := pickHintValueForFieldPool(e.rng, values, length, f.IsNumeric()); v != "" {
						return v
					}
				}
			}
		}
	}
	// 3. Fall back to user-supplied hints.
	if hinted := e.hintValueForField(f, preferTransaction, screenHint); hinted != "" {
		return hinted
	}
	// 4. Generate a random value appropriate for the field type.
	return e.generateValue(f)
}

func (e *Engine) hintValueForField(f *host.Field, preferTransaction bool, screenHint *ScreenHint) string {
	if len(e.hintTransactions) == 0 && len(e.hintKnownData) == 0 &&
		len(e.defaultHintTx) == 0 && len(e.defaultHintData) == 0 &&
		(screenHint == nil || len(screenHint.KnownData) == 0) {
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
	numeric := f.IsNumeric()

	if screenHint != nil && len(screenHint.KnownData) > 0 && e.rng.Intn(100) < 70 {
		if v := pickHintValueForFieldPool(e.rng, screenHint.KnownData, length, numeric); v != "" {
			return v
		}
	}
	if preferTransaction && len(e.hintTransactions) > 0 && e.rng.Intn(100) < 65 {
		if v := pickHintValueForFieldPool(e.rng, e.hintTransactions, length, numeric); v != "" {
			return v
		}
	}
	pool := e.hintKnownData
	if len(pool) == 0 {
		pool = e.hintTransactions
	}
	if len(pool) > 0 && e.rng.Intn(100) < 55 {
		if v := pickHintValueForFieldPool(e.rng, pool, length, numeric); v != "" {
			return v
		}
	}
	pool = e.defaultHintData
	if len(pool) == 0 {
		pool = e.defaultHintTx
	}
	if len(pool) > 0 && e.rng.Intn(100) < 35 {
		if v := pickHintValueForFieldPool(e.rng, pool, length, numeric); v != "" {
			return v
		}
	}
	return ""
}

func (e *Engine) prepareFieldWriteValue(f *host.Field, candidate string) string {
	if e == nil || f == nil {
		return ""
	}
	width := e.effectiveFieldWriteLength(f)
	if width <= 0 {
		return ""
	}
	proposed := e.expandWriteValueForField(candidate, width, f.IsNumeric())
	if proposed == "" {
		return ""
	}
	current := fieldCurrentValueForWriteWidth(f, width)
	if sameNonBlankFieldValue(current, proposed) {
		alt := e.generateValue(f)
		if alt != "" {
			if altExpanded := e.expandWriteValueForField(alt, width, f.IsNumeric()); altExpanded != "" {
				proposed = altExpanded
			}
		}
	}
	return proposed
}

func (e *Engine) effectiveFieldWriteLength(f *host.Field) int {
	length := fieldLength(f)
	if length <= 0 {
		return 0
	}
	maxLen := e.cfg.MaxFieldLength
	if maxLen <= 0 {
		maxLen = 40
	}
	if length > maxLen {
		length = maxLen
	}
	return length
}

func (e *Engine) clearFieldBeforeWrite(f *host.Field) error {
	if e == nil || e.h == nil || f == nil {
		return nil
	}
	clearLen := fieldLength(f)
	if clearLen <= 0 {
		return nil
	}
	clearText := strings.Repeat(" ", clearLen)
	if err := e.h.WriteStringAt(f.StartY, f.StartX, clearText); err != nil {
		// Some hosts/screens may reject blanks in numeric fields. Fallback to
		// zero-fill so we still replace existing contents before writing the new value.
		if f.IsNumeric() {
			return e.h.WriteStringAt(f.StartY, f.StartX, strings.Repeat("0", clearLen))
		}
		return err
	}
	return nil
}

func (e *Engine) expandWriteValueForField(value string, width int, numeric bool) string {
	if width <= 0 {
		return ""
	}
	fitted := fitHintValueForField(value, width, numeric)
	if fitted == "" {
		return ""
	}
	runes := []rune(fitted)
	if len(runes) >= width {
		return string(runes[:width])
	}
	if !numeric {
		return string(runes) + strings.Repeat(" ", width-len(runes))
	}
	const digits = "0123456789"
	var sb strings.Builder
	sb.Grow(width)
	sb.WriteString(string(runes))
	for i := len(runes); i < width; i++ {
		sb.WriteByte(digits[e.rng.Intn(len(digits))])
	}
	return sb.String()
}

func fieldCurrentValueForWriteWidth(f *host.Field, width int) string {
	if f == nil || width <= 0 {
		return ""
	}
	current := strings.ReplaceAll(strings.ReplaceAll(f.GetValue(), "\r", " "), "\n", " ")
	if current == "" {
		return ""
	}
	runes := []rune(current)
	if len(runes) > width {
		runes = runes[:width]
	}
	return string(runes)
}

func sameNonBlankFieldValue(current, proposed string) bool {
	cleanCurrent := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(current, "\r", " "), "\n", " "))
	if cleanCurrent == "" {
		return false
	}
	return normalizeFieldValueForCompare(current) == normalizeFieldValueForCompare(proposed)
}

func normalizeFieldValueForCompare(value string) string {
	clean := strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")
	return strings.Join(strings.Fields(clean), " ")
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

	// 3270 mainframe applications predominantly use uppercase input for
	// commands, transaction codes and data.  Generating only uppercase
	// characters and digits (plus a single space for subsequent positions)
	// makes random values far more likely to match valid application inputs,
	// improving the chance that each submission causes a meaningful screen
	// transition.  The first character never uses a space because 3270
	// command and transaction-code fields reject leading whitespace; avoiding
	// it eliminates wasted exploration steps on those fields.
	const charsFirst = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	b := make([]byte, length)
	b[0] = charsFirst[e.rng.Intn(len(charsFirst))]
	for i := 1; i < length; i++ {
		b[i] = chars[e.rng.Intn(len(chars))]
	}
	return string(b)
}

// chooseAIDKey selects an AID key using the configured weights.
func (e *Engine) chooseAIDKey() string {
	return e.chooseAIDKeyBoosted(nil)
}

// chooseAIDKeyBoosted selects an AID key using the configured weights plus
// any extra boosts supplied by the caller (e.g. derived from MindMap
// transition statistics for the current screen area).  Keys are sorted before
// sampling so that results are deterministic for a given RNG seed.
// Each effective weight is clamped to a minimum of 1 so that all configured
// keys remain selectable for exploration breadth even when penalties apply.
func (e *Engine) chooseAIDKeyBoosted(boosts map[string]int) string {
	weights := e.cfg.AIDKeyWeights
	blocked := e.blacklistedKeys
	if len(weights) == 0 && len(boosts) == 0 {
		return fallbackChaosKey(blocked)
	}

	// Merge configured weights with any caller-supplied boosts, canonicalising
	// key names so aliases like "PF3" and "PF(3)" collapse to one entry.
	effective := make(map[string]int, len(weights)+len(boosts))
	for rawKey, w := range weights {
		key := normalizeChaosKeyName(rawKey)
		if key == "" {
			key = strings.TrimSpace(rawKey)
		}
		if key == "" || isBlacklistedKeyInSet(blocked, key) {
			continue
		}
		effective[key] += w
	}
	for rawKey, b := range boosts {
		key := normalizeChaosKeyName(rawKey)
		if key == "" {
			key = strings.TrimSpace(rawKey)
		}
		if key == "" || isBlacklistedKeyInSet(blocked, key) {
			continue
		}
		effective[key] += b
	}
	// Clamp to minimum weight of 1 so that penalised keys remain selectable
	// (preserving exploration breadth) rather than being silently excluded.
	for k := range effective {
		if effective[k] < 1 {
			effective[k] = 1
		}
	}
	if len(effective) == 0 {
		return fallbackChaosKey(blocked)
	}

	// Sort keys so that the weighted pick is deterministic for a given seed.
	keys := make([]string, 0, len(effective))
	for k := range effective {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	total := 0
	for _, k := range keys {
		total += effective[k]
	}
	if total <= 0 {
		return fallbackChaosKey(blocked)
	}

	pick := e.rng.Intn(total)
	cum := 0
	for _, k := range keys {
		cum += effective[k]
		if pick < cum {
			return k
		}
	}
	return fallbackChaosKey(blocked)
}

// hashScreen produces a short stable fingerprint of the screen state based on
// its text content and field structure. Cursor position is intentionally
// excluded: on 3270 terminals the cursor moves freely between input fields
// (e.g. via Tab) without changing the logical screen, so including it would
// cause the same screen to appear as many different "areas" in the MindMap and
// make Tab key presses register as false screen transitions.
//
// Field positions and attribute codes are included so that two screens with
// identical text but different field layouts (e.g. one has a numeric field
// where the other has an alphanumeric field, or fields at different row/column
// offsets) are correctly identified as distinct screens.
func hashScreen(s *host.Screen) string {
	if s == nil {
		return ""
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d", s.Text(), len(s.Fields))
	for _, f := range s.Fields {
		if f == nil {
			continue
		}
		fmt.Fprintf(h, "|%d,%d,%d,%d,%d", f.StartY, f.StartX, f.EndY, f.EndX, f.FieldCode)
	}
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

// selectTargetFields chooses which unprotected fields to populate in this
// attempt. It can select one, several, or all fields. On screens where every
// unprotected field is single-cell, it deterministically selects one field to
// avoid overfilling option-style input grids.
func (e *Engine) selectTargetFields(fields []*host.Field) []*host.Field {
	if len(fields) <= 1 {
		return fields
	}

	allSingleCell := true
	for _, f := range fields {
		if fieldLength(f) != 1 {
			allSingleCell = false
			break
		}
	}
	if allSingleCell {
		return []*host.Field{fields[e.rng.Intn(len(fields))]}
	}

	n := len(fields)
	oneWeight := 3
	severalWeight := 3
	allWeight := 2

	pick := e.rng.Intn(oneWeight + severalWeight + allWeight)
	targetCount := n
	switch {
	case pick < oneWeight:
		targetCount = 1
	case pick < oneWeight+severalWeight:
		if n == 2 {
			targetCount = 1
		} else {
			targetCount = 2 + e.rng.Intn(n-2)
		}
	default:
		targetCount = n
	}

	if targetCount >= n {
		return fields
	}
	perm := e.rng.Perm(n)
	selected := make([]int, targetCount)
	copy(selected, perm[:targetCount])
	sort.Ints(selected)
	out := make([]*host.Field, 0, targetCount)
	for _, idx := range selected {
		out = append(out, fields[idx])
	}
	return out
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

// aidKeyToStepType converts a key name to the workflow step type used by the
// existing playback system. The function supports the common virtual-keyboard
// keys (not only AID keys) so chaos can record accurate workflows when key
// hints introduce non-PF/non-PA actions.
func aidKeyToStepType(key string) string {
	upper := strings.ToUpper(strings.TrimSpace(key))
	switch upper {
	case "ENTER":
		return "PressEnter"
	case "TAB":
		return "PressTab"
	case "BACKTAB":
		return "PressBackTab"
	case "CLEAR":
		return "PressClear"
	case "RESET":
		return "PressReset"
	case "ERASEEOF", "ERASE_EOF":
		return "PressEraseEOF"
	case "ERASEINPUT", "ERASE_INPUT":
		return "PressEraseInput"
	case "DUP":
		return "PressDup"
	case "FIELDMARK", "FIELD_MARK":
		return "PressFieldMark"
	case "SYSREQ", "SYS_REQ":
		return "PressSysReq"
	case "ATTN":
		return "PressAttn"
	case "NEWLINE", "NEW_LINE":
		return "PressNewline"
	case "BACKSPACE":
		return "PressBackspace"
	case "DELETE":
		return "PressDelete"
	case "INSERT":
		return "PressInsert"
	case "HOME":
		return "PressHome"
	case "UP":
		return "PressUp"
	case "DOWN":
		return "PressDown"
	case "LEFT":
		return "PressLeft"
	case "RIGHT":
		return "PressRight"
	}
	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		return "PressPF" + inner
	}
	if strings.HasPrefix(upper, "PF") {
		inner := strings.TrimPrefix(upper, "PF")
		if _, err := strconv.Atoi(inner); err == nil {
			return "PressPF" + inner
		}
	}
	if strings.HasPrefix(upper, "F") {
		inner := strings.TrimPrefix(upper, "F")
		if n, err := strconv.Atoi(inner); err == nil && n >= 1 && n <= 24 {
			return fmt.Sprintf("PressPF%d", n)
		}
	}
	if strings.HasPrefix(upper, "PA(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PA("), ")")
		return "PressPA" + inner
	}
	if strings.HasPrefix(upper, "PA") {
		inner := strings.TrimPrefix(upper, "PA")
		if _, err := strconv.Atoi(inner); err == nil {
			return "PressPA" + inner
		}
	}
	return "PressEnter"
}
