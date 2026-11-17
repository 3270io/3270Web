// SPDX-License-Identifier: AGPL-3.0-or-later

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
	"unicode/utf8"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// Transition records a state change observed during exploration.
type Transition struct {
	FromHash string                 `json:"fromHash"`
	ToHash   string                 `json:"toHash"`
	Steps    []session.WorkflowStep `json:"steps"`
	// AIDKey is the AID key that caused this transition. Combined with
	// FromHash/ToHash it forms the same dedup tuple key used to decide
	// whether a transition is newly-discovered (see run() and Resume()) —
	// stored explicitly rather than re-derived from Steps so Resume can
	// rebuild that dedup state without reversing aidKeyToStepType.
	AIDKey string `json:"aidKey,omitempty"`
	// HiddenSteps lists the indexes into Steps of fill steps that went into
	// non-display (password-style) fields. In memory those steps carry the
	// real text, so a live export can replay the sign-on; everything that
	// serialises a run masks them (see maskedTransitions).
	HiddenSteps []int `json:"hiddenSteps,omitempty"`
}

// Status is a snapshot of the engine's current state.
type Status struct {
	Active            bool           `json:"active"`
	StepsRun          int            `json:"stepsRun"`
	StartedAt         time.Time      `json:"startedAt,omitempty"`
	StoppedAt         time.Time      `json:"stoppedAt,omitempty"`
	MaxSteps          int            `json:"maxSteps,omitempty"`
	TimeBudget        time.Duration  `json:"timeBudget,omitempty"`
	Transitions       int            `json:"transitions"`
	UniqueScreens     int            `json:"uniqueScreens"`
	UniqueInputs      int            `json:"uniqueInputs"`
	AIDKeyCounts      map[string]int `json:"aidKeyCounts,omitempty"`
	LoadedRunID       string         `json:"loadedRunID,omitempty"`
	FirstScreenHash   string         `json:"firstScreenHash,omitempty"`
	LastAttempt       *Attempt       `json:"lastAttempt,omitempty"`
	RecentAttempts    []Attempt      `json:"recentAttempts,omitempty"`
	MindMap           *MindMap       `json:"mindMap,omitempty"`
	CoverageStats     *CoverageStats `json:"coverageStats,omitempty"`
	TerminationReason string         `json:"terminationReason,omitempty"`
	// SaturatedNoProgress is true when TerminationReason is "saturated" and the
	// run discovered no transitions at all. Callers use it to avoid resuming a
	// run that will only re-saturate on the same screen.
	SaturatedNoProgress bool   `json:"saturatedNoProgress,omitempty"`
	Error               string `json:"error,omitempty"`
	// MindMapVersion increases every time the mind map changes. A caller
	// that already holds the mind map for a version need not fetch or diff
	// it again until the number moves.
	MindMapVersion uint64 `json:"mindMapVersion"`
	// OutputFileError is set when the run finished but its workflow could
	// not be written to Config.OutputFile.
	OutputFileError string `json:"outputFileError,omitempty"`
	// Seed is the random seed the run is using (the configured one, or the
	// one derived from the clock when none was configured).
	Seed int64 `json:"seed,omitempty"`
}

// CoverageStats summarises how productive the recent window of chaos steps
// has been. Used by saturation detection and surfaced via chaos_status so
// callers (UI, Copilot) can see progress trends without parsing the mind map.
type CoverageStats struct {
	WindowSteps              int `json:"windowSteps"`
	NewScreensInWindow       int `json:"newScreensLast10Steps"`
	NewTransitionsInWindow   int `json:"newTransitionsLast10Steps"`
	SaturationStreak         int `json:"saturationStreak"`
	SaturationThresholdSteps int `json:"saturationThresholdSteps,omitempty"`
	// FrontierAreas counts discovered areas that still have at least one
	// candidate AID key never pressed from them; UntriedKeysTotal is the sum
	// of those untried keys. Together they say how much known-but-unexplored
	// territory remains — 0/0 with an active run means the configured key set
	// is exhausted everywhere the run has been.
	FrontierAreas    int `json:"frontierAreas"`
	UntriedKeysTotal int `json:"untriedKeysTotal"`
	// UntriedInputsTotal counts the values read off discovered screens (menu
	// options, listed commands) that have not been submitted yet.
	UntriedInputsTotal int `json:"untriedInputsTotal"`
}

// Termination reasons reported via Status.TerminationReason.
const (
	TerminationReasonMaxSteps   = "max_steps"
	TerminationReasonTimeBudget = "time_budget"
	TerminationReasonSaturated  = "saturated"
	TerminationReasonStopped    = "stopped"
	TerminationReasonError      = "error"
	// TerminationReasonBlocked means the run halted because no usable
	// (non-blacklisted) AID key was available for the current screen. Unlike
	// TerminationReasonError this is a recoverable, expected stop: the caller
	// (Copilot agent / UI) should relax the key blacklist or add hints and
	// resume, rather than treating it as a host failure.
	TerminationReasonBlocked = "blocked"
)

// hostOpMaxAttempts and hostOpBaseBackoff control the bounded retry applied to
// transient host operations (screen reads, key sends) inside the chaos loop.
// A single s3270 timeout or socket blip should not kill an entire exploration
// run, so each op is retried with exponential backoff before the run gives up.
const (
	hostOpMaxAttempts = 3
	hostOpBaseBackoff = 200 * time.Millisecond
)

const coverageWindowSize = 10

// AttemptFieldWrite captures one field write operation attempted by chaos
// during a single step.
//
// Length is the length of the field, not of what was typed into it: together
// with Row and Column it forms the same key the mind map files the field's
// metadata under. TypedLength is the length of the text written. Value is
// masked, and Masked set, when the field is a non-display one.
type AttemptFieldWrite struct {
	Row         int    `json:"row"`
	Column      int    `json:"column"`
	Length      int    `json:"length"`
	TypedLength int    `json:"typedLength,omitempty"`
	Value       string `json:"value,omitempty"`
	Masked      bool   `json:"masked,omitempty"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
}

// Attempt captures granular details for a single chaos submission cycle:
// field writes, selected AID key, transition result, and any terminal error.
type Attempt struct {
	Attempt                int       `json:"attempt"`
	Time                   time.Time `json:"time"`
	FromHash               string    `json:"fromHash,omitempty"`
	ToHash                 string    `json:"toHash,omitempty"`
	AIDKey                 string    `json:"aidKey,omitempty"`
	FirstScreenHintApplied bool      `json:"firstScreenHintApplied,omitempty"`
	FirstScreenHintReason  string    `json:"firstScreenHintReason,omitempty"`
	FieldsTargeted         int       `json:"fieldsTargeted"`
	FieldsWritten          int       `json:"fieldsWritten"`
	Transitioned           bool      `json:"transitioned"`
	Error                  string    `json:"error,omitempty"`
	// Retries counts how many extra host-operation attempts (transient screen
	// read / key send failures that were retried) this step absorbed before
	// succeeding. Zero in the common case; non-zero indicates a flaky host.
	Retries     int                 `json:"retries,omitempty"`
	FieldWrites []AttemptFieldWrite `json:"fieldWrites,omitempty"`
	// Outcome classifies what the key press did (one of the Outcome*
	// constants). Transitioned says only that the area changed; Outcome also
	// tells a rejection from a silent no-op and a sign-off from progress.
	Outcome string `json:"outcome,omitempty"`
	// ContentChanged: the press stayed on the same area but changed its body.
	ContentChanged bool `json:"contentChanged,omitempty"`
	// Rejected: the application answered with an error message.
	Rejected bool `json:"rejected,omitempty"`
	// SessionEnded: the press threw the run back onto its first screen.
	SessionEnded bool `json:"sessionEnded,omitempty"`
	// Message is the text of the message row(s) that changed, if any.
	Message string `json:"message,omitempty"`
	// Plan says why the step was taken when it was not the default weighted
	// choice: "input" (a value read off the screen), "key" (an untried key)
	// or "navigate" (a hop back towards unexplored territory).
	Plan string `json:"plan,omitempty"`
}

const maxRecentAttempts = 40

// maxRecentRawSteps bounds e.steps, the flat, unconditional log of every
// step's WorkflowSteps regardless of whether it made progress. It exists
// only as a last-resort export fallback for a run that never produced a
// single transition or recorded attempt (see ExportWorkflowWithSuccessBalance
// and buildExportWorkflowSteps), so it needs no more history than a handful
// of attempts' worth — without a cap it grew with the total steps ever
// taken across a whole (possibly many-times-resumed) exploration campaign.
const maxRecentRawSteps = 200

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

	// candidateKeys is the normalized AID-key candidate set implied by
	// cfg.AIDKeyWeights, fixed at construction. It is the baseline that
	// novelty and frontier steering measure "untried" against.
	candidateKeys []string

	// seed is the seed the random source was created with.
	seed int64
	// builtinHintLabels marks the hintKeyMappings entries that come from the
	// engine's own defaults rather than from the user.
	builtinHintLabels map[string]bool
	// attemptHook, when set, receives every attempt. Tests use it to see the
	// whole run rather than the capped recent-attempts window.
	attemptHook func(Attempt)

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
	aidKeyCounts map[string]int
	// stepsHidden parallels steps: true where the step's text is the content
	// of a non-display field.
	stepsHidden []bool

	// Unique-input accounting: a count, a bounded set of hashes behind it,
	// and a bounded sample of (displayable) values for saved runs.
	uniqueInputCount  int
	uniqueInputSeen   map[uint64]struct{}
	uniqueInputSample map[string]bool
	productiveSeen    map[uint64]struct{}

	mindMapVersion   uint64
	outputFileErr    string
	sigAliases       map[string]string
	contentSeen      map[string]map[string]struct{}
	commandPool      []string
	identifierPool   []string
	navPlan          []navStep
	navTarget        string
	navFailures      map[string]int
	lastBounce       *bounceState
	eraseUnsupported bool

	// firstScreenIsGate is true when the first screen is a sign-on screen
	// (it has a non-display input field): landing back on it from anywhere
	// deeper means the session was lost.
	firstScreenIsGate bool
	// sessionEndingValues are the values (upper-cased) that ended the
	// session when submitted; never typed again away from the first screen.
	sessionEndingValues map[string]struct{}
	// hiddenValues holds the real content typed into non-display fields
	// that worked, per area and field key. It exists only in memory, for
	// reuse within the run; everything serialised carries the mask.
	hiddenValues map[string]map[string][]string
	// edgeInputs holds the real inputs behind each observed edge, for
	// replaying it within the run (see rememberEdgeInputsLocked).
	edgeInputs     map[string]map[string]string
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
	firstScreenHash  string

	// Saturation tracking
	transitionTuples  map[string]bool // key = fromHash|toHash|aidKey
	saturationStreak  int
	newScreensWindow  []bool // sliding window: did step add a new screen?
	newTransWindow    []bool // sliding window: did step add a new transition tuple?
	terminationReason string
	// saturatedNoProgress is set when the run saturates without ever recording
	// a single state transition. It is the signal the caller uses to stop
	// resuming a run that keeps re-saturating on the same screen with nothing
	// new to discover (the saturate -> resume -> saturate loop).
	saturatedNoProgress bool
	autoAppliedHashes   map[string]bool // areas where we've already auto-applied PF label scan

	// Optional JSONL transition log (one JSON object per attempt).
	transitionLog *os.File
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
	builtinHintLabels := make(map[string]bool)
	for k, v := range defaultKeyMappings {
		hintKeyMappings[k] = v
		builtinHintLabels[k] = true
	}
	for k, v := range userKeyMappings {
		hintKeyMappings[k] = v
		delete(builtinHintLabels, k)
	}
	if len(hintKeyMappings) == 0 {
		hintKeyMappings = nil
	}
	return &Engine{
		cfg:               cfg,
		h:                 h,
		rng:               rand.New(rand.NewSource(seed)), //nolint:gosec
		seed:              seed,
		builtinHintLabels: builtinHintLabels,
		candidateKeys:     CandidateKeys(cfg.AIDKeyWeights),
		stopCh:            make(chan struct{}),
		workflowHeader:    workflowHeaderFromConfig(cfg),
		hintTransactions:  hintTransactions,
		hintKnownData:     hintKnownData,
		defaultHintTx:     defaultTx,
		defaultHintData:   defaultData,
		hintKeyMappings:   hintKeyMappings,
		screenHints:       cloneScreenHints(cfg.ScreenHints),
		blacklistedKeys:   normalizeChaosKeySet(cfg.KeyBlacklist),
	}
}

// MindMapSnapshot returns a deep copy of the engine's current mind map
// without the rest of the Status payload (attempts, counters), making it the
// cheap accessor for read-only mind-map consumers. Safe to call while the
// engine is running.
func (e *Engine) MindMapSnapshot() *MindMap {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mindMap.clone()
}

// ExportMindMap returns the engine's current mind map as a JSON byte slice.
// Safe to call while the engine is running.
func (e *Engine) ExportMindMap() ([]byte, error) {
	e.mu.Lock()
	mm := e.mindMap.clone()
	e.mu.Unlock()
	if mm == nil {
		return []byte("{}"), nil
	}
	return json.MarshalIndent(mm, "", "  ")
}

// ImportMindMap merges the supplied mind map into the engine's current one.
// Areas with the same hash are merged: learning counters and known values
// accumulate, while descriptive fields and business annotations keep the
// local value when both sides have one (see mergeAreaInto). Returns false if
// the engine is currently active (imports must happen between runs to avoid
// races with the run loop's own mind-map writes).
func (e *Engine) ImportMindMap(imported *MindMap) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active {
		return false
	}
	if imported == nil {
		return true
	}
	normalizeMindMapSignatures(imported)
	normalizeMindMapFieldKeys(imported)
	e.touchMindMapLocked()
	if e.mindMap == nil {
		e.mindMap = newMindMap()
	}
	if e.mindMap.Areas == nil {
		e.mindMap.Areas = make(map[string]*MindMapArea)
	}
	for hash, area := range imported.Areas {
		if area == nil || strings.TrimSpace(hash) == "" {
			continue
		}
		if existing, ok := e.mindMap.Areas[hash]; ok && existing != nil {
			mergeAreaInto(existing, area)
			continue
		}
		e.mindMap.Areas[hash] = area
	}
	for key, fn := range imported.BusinessFunctions {
		if fn == nil || strings.TrimSpace(key) == "" {
			continue
		}
		if e.mindMap.BusinessFunctions == nil {
			e.mindMap.BusinessFunctions = make(map[string]*BusinessFunction)
		}
		e.mindMap.BusinessFunctions[key] = fn
	}
	return true
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

// SetKeyBlacklist replaces the configured AID-key blacklist. Safe to call
// while the engine is running.
func (e *Engine) SetKeyBlacklist(keys []string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg.KeyBlacklist = append([]string(nil), keys...)
	e.blacklistedKeys = normalizeChaosKeySet(keys)
}

func (e *Engine) shouldApplyFirstScreenHintForHashLocked(currentHash string, isFirstAttempt bool) (bool, string) {
	if e == nil {
		return false, ""
	}
	if strings.TrimSpace(currentHash) == "" {
		if isFirstAttempt {
			return true, "initial"
		}
		return false, ""
	}
	if isFirstAttempt && strings.TrimSpace(e.firstScreenHash) == "" {
		e.firstScreenHash = currentHash
		return true, "initial"
	}
	if strings.TrimSpace(e.firstScreenHash) != "" && e.firstScreenHash == currentHash {
		if isFirstAttempt {
			return true, "initial"
		}
		return true, "hash_match"
	}
	return false, ""
}

func (e *Engine) blacklistedKeysSnapshot() map[string]struct{} {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.blacklistedKeys
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
	if len(clean.KnownData) == 0 && len(clean.KnownKeys) == 0 && len(clean.BlockedKeys) == 0 && len(clean.KeyAssignments) == 0 {
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
	// Fail fast if the key blacklist leaves no usable AID key at all: the run
	// would otherwise start and immediately halt on the first screen. Surfacing
	// this from Start() gives the caller a clear, actionable message instead of
	// a "blocked" run with zero steps.
	if fallbackChaosKey(e.blacklistedKeys) == "" {
		return fmt.Errorf("all usable AID keys are blacklisted; relax the key blacklist before starting chaos")
	}

	e.active = true
	e.startedAt = time.Now()
	e.stoppedAt = time.Time{}
	e.stepsRun = 0
	e.transitions = nil
	e.steps = nil
	e.lastErr = ""
	e.stepsHidden = nil
	e.screenHashes = make(map[string]bool)
	e.uniqueInputCount = 0
	e.uniqueInputSeen = make(map[uint64]struct{})
	e.uniqueInputSample = make(map[string]bool)
	e.aidKeyCounts = make(map[string]int)
	e.loadedRunID = ""
	e.attempts = nil
	e.mindMap = newMindMap()
	e.firstScreenHash = ""
	e.firstScreenIsGate = false
	e.sessionEndingValues = nil
	e.hiddenValues = nil
	e.edgeInputs = nil
	e.touchMindMapLocked()
	e.workflowHeader = workflowHeaderFromConfig(e.cfg)
	e.stopCh = make(chan struct{})
	e.resetSaturationStateLocked()

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
// Active reports whether exploration is still running.
//
// Separate from Status because Status clones the entire mind map — right when
// serving a status page, absurd when the caller wants one boolean. Several
// callers poll exactly that: the store checks it before starting a second run
// on a session, shutdown checks it per session, and a test helper checks it
// every few milliseconds. On a run that has explored a few hundred screens,
// each of those was copying the whole graph to read a flag.
func (e *Engine) Active() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.active
}

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
		Active:              e.active,
		StepsRun:            e.stepsRun,
		StartedAt:           e.startedAt,
		StoppedAt:           e.stoppedAt,
		MaxSteps:            e.cfg.MaxSteps,
		TimeBudget:          e.cfg.TimeBudget,
		Transitions:         len(e.transitions),
		UniqueScreens:       len(e.screenHashes),
		UniqueInputs:        e.uniqueInputCount,
		AIDKeyCounts:        aidCopy,
		LoadedRunID:         e.loadedRunID,
		FirstScreenHash:     e.firstScreenHash,
		LastAttempt:         lastAttempt,
		RecentAttempts:      attempts,
		MindMap:             mindMap,
		CoverageStats:       e.coverageStatsSnapshotLocked(),
		TerminationReason:   e.terminationReason,
		SaturatedNoProgress: e.saturatedNoProgress,
		Error:               e.lastErr,
		MindMapVersion:      e.mindMapVersion,
		OutputFileError:     e.outputFileErr,
		Seed:                e.seed,
	}
}

// coverageStatsSnapshotLocked builds the CoverageStats view of recent steps.
// Caller must hold e.mu.
func (e *Engine) coverageStatsSnapshotLocked() *CoverageStats {
	if len(e.newScreensWindow) == 0 && e.saturationStreak == 0 && len(e.transitionTuples) == 0 {
		return nil
	}
	newScreens := 0
	for _, v := range e.newScreensWindow {
		if v {
			newScreens++
		}
	}
	newTrans := 0
	for _, v := range e.newTransWindow {
		if v {
			newTrans++
		}
	}
	frontierAreas, untriedKeys := e.frontierStatsLocked()
	untriedInputs := 0
	if e.mindMap != nil {
		for _, area := range e.mindMap.Areas {
			untriedInputs += len(e.untriedInputsLocked(area))
		}
	}
	return &CoverageStats{
		WindowSteps:              len(e.newScreensWindow),
		NewScreensInWindow:       newScreens,
		NewTransitionsInWindow:   newTrans,
		SaturationStreak:         e.saturationStreak,
		SaturationThresholdSteps: e.cfg.SaturationSteps,
		FrontierAreas:            frontierAreas,
		UntriedKeysTotal:         untriedKeys,
		UntriedInputsTotal:       untriedInputs,
	}
}

// exportedWorkflow is the JSON shape expected by the existing workflow loader.
type exportedWorkflow struct {
	Host             string                      `json:"Host"`
	Port             int                         `json:"Port"`
	Name             string                      `json:"Name,omitempty"`
	Description      string                      `json:"Description,omitempty"`
	BusinessFunction string                      `json:"BusinessFunction,omitempty"`
	Parameters       []session.WorkflowParameter `json:"Parameters,omitempty"`
	EveryStepDelay   *session.WorkflowDelayRange `json:"EveryStepDelay,omitempty"`
	OutputFilePath   string                      `json:"OutputFilePath,omitempty"`
	RampUpBatchSize  int                         `json:"RampUpBatchSize,omitempty"`
	RampUpDelay      float64                     `json:"RampUpDelay,omitempty"`
	EndOfTaskDelay   *session.WorkflowDelayRange `json:"EndOfTaskDelay,omitempty"`
	Steps            []session.WorkflowStep      `json:"Steps"`
	ChaosDiscovery   *WorkflowDiscoveryMetadata  `json:"ChaosDiscovery,omitempty"`
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

const defaultChaosExportSuccessBalance = 1.0

// ExportWorkflow returns the learned workflow as indented JSON that is
// compatible with the existing WorkflowConfig format.
func (e *Engine) ExportWorkflow(hostName string, port int) ([]byte, error) {
	return e.ExportWorkflowWithSuccessBalance(hostName, port, defaultChaosExportSuccessBalance)
}

// ExportWorkflowWithSuccessBalance returns the learned workflow as indented JSON
// while controlling how many unsuccessful attempts are represented in the
// exported steps. A balance of 1 exports only successful transition steps.
func (e *Engine) ExportWorkflowWithSuccessBalance(hostName string, port int, successBalance float64) ([]byte, error) {
	e.mu.Lock()
	rawSteps := make([]session.WorkflowStep, len(e.steps))
	copy(rawSteps, e.steps)
	transitions := make([]Transition, len(e.transitions))
	copy(transitions, e.transitions)
	attempts := make([]Attempt, len(e.attempts))
	copy(attempts, e.attempts)
	mindMap := e.mindMap.clone()
	header := e.workflowHeader.clone()
	discovery := e.workflowDiscoveryMetadataLocked()
	firstHash := e.firstScreenHash
	e.mu.Unlock()

	if hostName == "" {
		hostName = e.cfg.ExportHost
	}
	if port == 0 {
		port = e.cfg.ExportPort
	}
	steps := buildExportWorkflowStepsFrom(firstHash, transitions, attempts, mindMap, successBalance)
	if len(steps) == 0 && len(rawSteps) > 0 {
		steps = rawSteps
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

// ExportWorkflowFromSavedRun returns a workflow export JSON payload for a saved
// run using the same success/unsuccessful balance rules as active-engine export.
func ExportWorkflowFromSavedRun(run *SavedRun, hostName string, port int, successBalance float64) ([]byte, error) {
	if run == nil {
		return nil, fmt.Errorf("saved run is nil")
	}
	transitions := make([]Transition, len(run.TransitionList))
	copy(transitions, run.TransitionList)
	attempts := make([]Attempt, len(run.Attempts))
	copy(attempts, run.Attempts)
	steps := buildExportWorkflowStepsFrom(run.FirstScreenHash, transitions, attempts, run.MindMap, successBalance)
	if len(steps) == 0 && len(run.Steps) > 0 {
		steps = append([]session.WorkflowStep(nil), run.Steps...)
	}
	return marshalChaosWorkflowExport(hostName, port, steps, run.WorkflowHeader, WorkflowDiscoveryMetadataFromSavedRun(run))
}

func marshalChaosWorkflowExport(hostName string, port int, steps []session.WorkflowStep, header *WorkflowHeader, discovery *WorkflowDiscoveryMetadata) ([]byte, error) {
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

func buildExportWorkflowSteps(transitions []Transition, attempts []Attempt, mindMap *MindMap, successBalance float64) []session.WorkflowStep {
	return buildExportWorkflowStepsFrom("", transitions, attempts, mindMap, successBalance)
}

// buildExportWorkflowStepsFrom builds the exported step list as one connected
// walk over the discovered edges, starting on the run's first screen.
//
// Transitions are stored once per edge, in the order they were discovered.
// Concatenating them in that order is only replayable while the run never
// backtracks: A->B, B->A, A->C plays fine, but as soon as a second edge out
// of B is found later (A->B, B->A, A->C, B->D) the fourth press lands on C,
// not B. The walk instead follows unvisited edges out of wherever it is and,
// when there are none, replays known edges to the nearest screen that still
// has one. Every step it emits is pressed on the screen it was recorded on.
func buildExportWorkflowStepsFrom(firstHash string, transitions []Transition, attempts []Attempt, mindMap *MindMap, successBalance float64) []session.WorkflowStep {
	balance := clampExportSuccessBalance(successBalance)
	walk := orderTransitionsAsWalk(firstHash, transitions)
	if len(attempts) == 0 {
		return flattenTransitionSteps(walk)
	}
	if len(transitions) == 0 {
		if balance >= 0.999999 {
			return nil
		}
		return buildUnsuccessfulCheckSteps(attempts, mindMap, balance)
	}

	successCount := 0
	for _, attempt := range attempts {
		if isSuccessfulExportAttempt(attempt) {
			successCount++
		}
	}
	maxUnsuccessful := exportUnsuccessfulBudget(successCount, balance)
	if maxUnsuccessful <= 0 {
		return flattenTransitionSteps(walk)
	}

	// Unsuccessful attempts become check steps on the screen they were made
	// from, emitted when the walk is standing on that screen. Screens the
	// walk never visits get none: a check for a screen playback is not on
	// can only fail.
	pendingChecks := make(map[string]int)
	for _, attempt := range attempts {
		if isSuccessfulExportAttempt(attempt) {
			continue
		}
		if from := strings.TrimSpace(attempt.FromHash); from != "" {
			pendingChecks[from]++
		}
	}

	steps := make([]session.WorkflowStep, 0, len(walk)*2)
	includedUnsuccessful := 0
	for _, tr := range walk {
		from := strings.TrimSpace(tr.FromHash)
		for pendingChecks[from] > 0 && includedUnsuccessful < maxUnsuccessful {
			pendingChecks[from]--
			if mindMap == nil {
				break
			}
			check, ok := checkStepForArea(mindMap.Areas[from])
			if !ok {
				pendingChecks[from] = 0
				break
			}
			steps = append(steps, check)
			includedUnsuccessful++
		}
		steps = append(steps, tr.Steps...)
	}
	return steps
}

// orderTransitionsAsWalk arranges transitions into a connected walk from the
// first screen. An edge may appear more than once (it is replayed to get
// back to a screen with something left); an edge that cannot be reached from
// the first screen over known edges is left out, since there is no way to
// stand on its starting screen. Transitions without screen hashes (hand-made
// or from very old runs) cannot be ordered and are returned as they are.
func orderTransitionsAsWalk(firstHash string, transitions []Transition) []Transition {
	if len(transitions) == 0 {
		return nil
	}
	outgoing := make(map[string][]int)
	for i, tr := range transitions {
		if strings.TrimSpace(tr.FromHash) == "" || strings.TrimSpace(tr.ToHash) == "" {
			return transitions
		}
		outgoing[tr.FromHash] = append(outgoing[tr.FromHash], i)
	}
	start := strings.TrimSpace(firstHash)
	if len(outgoing[start]) == 0 {
		start = transitions[0].FromHash
	}

	visited := make([]bool, len(transitions))
	walk := make([]Transition, 0, len(transitions))
	current := start
	for {
		next := -1
		for _, idx := range outgoing[current] {
			if !visited[idx] {
				next = idx
				break
			}
		}
		if next >= 0 {
			visited[next] = true
			walk = append(walk, transitions[next])
			current = transitions[next].ToHash
			continue
		}
		// Nothing new from here: replay known edges to the nearest screen
		// that still has an unvisited edge.
		bridge := bridgeToUnvisited(current, transitions, outgoing, visited)
		if len(bridge) == 0 {
			return walk
		}
		for _, idx := range bridge {
			walk = append(walk, transitions[idx])
			current = transitions[idx].ToHash
		}
	}
}

// bridgeToUnvisited returns the indexes of the edges to replay, in order, to
// get from a screen to the nearest one with an unvisited outgoing edge.
func bridgeToUnvisited(from string, transitions []Transition, outgoing map[string][]int, visited []bool) []int {
	type visit struct {
		hash string
		via  int
		prev int
	}
	queue := []visit{{hash: from, via: -1, prev: -1}}
	seen := map[string]bool{from: true}
	for i := 0; i < len(queue); i++ {
		for _, idx := range outgoing[queue[i].hash] {
			to := transitions[idx].ToHash
			if seen[to] {
				continue
			}
			seen[to] = true
			queue = append(queue, visit{hash: to, via: idx, prev: i})
			hasUnvisited := false
			for _, out := range outgoing[to] {
				if !visited[out] {
					hasUnvisited = true
					break
				}
			}
			if !hasUnvisited {
				continue
			}
			var path []int
			for at := len(queue) - 1; at > 0; at = queue[at].prev {
				path = append(path, queue[at].via)
			}
			for l, r := 0, len(path)-1; l < r; l, r = l+1, r-1 {
				path[l], path[r] = path[r], path[l]
			}
			return path
		}
	}
	return nil
}

func flattenTransitionSteps(transitions []Transition) []session.WorkflowStep {
	if len(transitions) == 0 {
		return nil
	}
	steps := make([]session.WorkflowStep, 0)
	for _, tr := range transitions {
		if len(tr.Steps) == 0 {
			continue
		}
		steps = append(steps, tr.Steps...)
	}
	return steps
}

// buildUnsuccessfulCheckSteps builds check steps for a run that never
// produced a successful transition. With no successes to anchor the usual
// success:unsuccessful ratio, the balance is applied as the share of attempts
// left for exploration noise: balance 0.8 keeps at most 20% of the attempts
// as check steps (minimum one, since the caller only gets here when the
// balance asked for some unsuccessful representation).
func buildUnsuccessfulCheckSteps(attempts []Attempt, mindMap *MindMap, successBalance float64) []session.WorkflowStep {
	balance := clampExportSuccessBalance(successBalance)
	budget := int(float64(len(attempts))*(1-balance) + 0.5)
	if budget < 1 {
		budget = 1
	}
	steps := make([]session.WorkflowStep, 0, budget)
	for _, attempt := range attempts {
		if len(steps) >= budget {
			break
		}
		if isSuccessfulExportAttempt(attempt) {
			continue
		}
		check, ok := exportCheckStepForAttempt(attempt, mindMap)
		if ok {
			steps = append(steps, check)
		}
	}
	return steps
}

func isSuccessfulExportAttempt(attempt Attempt) bool {
	return attempt.Transitioned && !attempt.SessionEnded && strings.TrimSpace(attempt.Error) == ""
}

func clampExportSuccessBalance(v float64) float64 {
	if v <= 0 {
		return defaultChaosExportSuccessBalance
	}
	if v > 1 {
		return 1
	}
	return v
}

func exportUnsuccessfulBudget(successCount int, balance float64) int {
	if successCount <= 0 || balance >= 1 {
		return 0
	}
	unsuccessRatio := (1 - balance) / balance
	if unsuccessRatio <= 0 {
		return 0
	}
	return int(float64(successCount) * unsuccessRatio)
}

func exportCheckStepForAttempt(attempt Attempt, mindMap *MindMap) (session.WorkflowStep, bool) {
	hash := strings.TrimSpace(attempt.FromHash)
	if hash == "" || mindMap == nil || len(mindMap.Areas) == 0 {
		return session.WorkflowStep{}, false
	}
	return checkStepForArea(mindMap.Areas[hash])
}

// checkStepForArea builds a CheckValue step asserting the first non-blank
// text run in the area's preview, so playback can verify it reached the
// expected screen.
func checkStepForArea(area *MindMapArea) (session.WorkflowStep, bool) {
	if area == nil || strings.TrimSpace(area.PreviewText) == "" {
		return session.WorkflowStep{}, false
	}
	lines := strings.Split(area.PreviewText, "\n")
	for rowIdx, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		runes := []rune(line)
		start := -1
		end := -1
		for i, r := range runes {
			if r == ' ' || r == 0 {
				if start >= 0 {
					break
				}
				continue
			}
			if start < 0 {
				start = i
			}
			end = i
		}
		if start < 0 || end < start {
			continue
		}
		text := string(runes[start : end+1])
		if strings.TrimSpace(text) == "" {
			continue
		}
		return session.WorkflowStep{
			Type: "CheckValue",
			Coordinates: &session.WorkflowCoordinates{
				Row:    rowIdx + 1,
				Column: start + 1,
				Length: end - start + 1,
			},
			Text: text,
		}, true
	}
	return session.WorkflowStep{}, false
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
		UniqueInputs:   e.uniqueInputCount,
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
//
// Nothing typed into a non-display field leaves through here: attempts and
// the mind map never held it, and the fill steps that did are masked on the
// way out. The price is that a workflow exported from a saved run types the
// mask where the live engine's export types the real value.
func (e *Engine) Snapshot(runID string) *SavedRun {
	e.mu.Lock()
	defer e.mu.Unlock()

	hashes := make(map[string]bool, len(e.screenHashes))
	for k, v := range e.screenHashes {
		hashes[k] = v
	}
	inputs := make(map[string]bool, len(e.uniqueInputSample))
	for k, v := range e.uniqueInputSample {
		inputs[k] = v
	}
	aid := make(map[string]int, len(e.aidKeyCounts))
	for k, v := range e.aidKeyCounts {
		aid[k] = v
	}
	transitions := maskedTransitions(e.transitions)
	steps := make([]session.WorkflowStep, len(e.steps))
	copy(steps, e.steps)
	for i := range steps {
		if i < len(e.stepsHidden) && e.stepsHidden[i] {
			steps[i].Text = maskedFieldValue
		}
	}
	attempts := make([]Attempt, len(e.attempts))
	copy(attempts, e.attempts)
	mindMap := e.mindMap.clone()
	cfg := e.persistableConfigLocked()

	return &SavedRun{
		SavedRunMeta: SavedRunMeta{
			ID:                runID,
			StartedAt:         e.startedAt,
			StoppedAt:         e.stoppedAt,
			StepsRun:          e.stepsRun,
			Transitions:       len(transitions),
			UniqueScreens:     len(hashes),
			UniqueInputs:      e.uniqueInputCount,
			Error:             e.lastErr,
			TerminationReason: e.terminationReason,
			FirstScreenHash:   e.firstScreenHash,
			TargetHost:        e.targetHostLocked(),
		},
		ScreenHashes:      hashes,
		TransitionList:    transitions,
		Steps:             steps,
		WorkflowHeader:    e.workflowHeader.clone(),
		AIDKeyCounts:      aid,
		UniqueInputValues: inputs,
		Attempts:          attempts,
		MindMap:           mindMap,
		Seed:              e.seed,
		Config:            &cfg,
	}
}

// targetHostLocked names the host the run explored, from the export
// metadata the caller configured. Caller must hold e.mu.
func (e *Engine) targetHostLocked() string {
	hostName := strings.TrimSpace(e.cfg.ExportHost)
	if hostName == "" {
		return ""
	}
	if e.cfg.ExportPort > 0 {
		return hostName + ":" + strconv.Itoa(e.cfg.ExportPort)
	}
	return hostName
}

// persistableConfigLocked returns the configuration the run is using, in the
// form written into a saved run. The effective seed replaces a zero one, so
// a resume can continue the same random sequence. Hints are left out on
// purpose: they are where a user puts the credentials for the sign-on
// screen, and a run file is not the place for those. Whoever resumes the run
// supplies its hints again. Caller must hold e.mu.
func (e *Engine) persistableConfigLocked() Config {
	cfg := e.cfg
	cfg.Seed = e.seed
	cfg.Hints = nil
	cfg.ScreenHints = nil
	if len(e.cfg.AIDKeyWeights) > 0 {
		cfg.AIDKeyWeights = make(map[string]int, len(e.cfg.AIDKeyWeights))
		for k, v := range e.cfg.AIDKeyWeights {
			cfg.AIDKeyWeights[k] = v
		}
	}
	cfg.KeyBlacklist = append([]string(nil), e.cfg.KeyBlacklist...)
	return cfg
}

// resetSaturationStateLocked (re)initializes the per-run saturation-tracking
// fields and reopens the optional transition log. Both Start and Resume must
// call this: leaving transitionTuples/productiveValues/autoAppliedHashes nil
// causes a nil-map write panic (crashing the run() goroutine, which has no
// recover) the first time run() records a screen transition or productive
// field write. Callers must hold e.mu.
func (e *Engine) resetSaturationStateLocked() {
	e.transitionTuples = make(map[string]bool)
	e.productiveSeen = make(map[uint64]struct{})
	e.sigAliases = make(map[string]string)
	e.contentSeen = make(map[string]map[string]struct{})
	e.navPlan = nil
	e.navTarget = ""
	e.navFailures = make(map[string]int)
	e.lastBounce = nil
	e.outputFileErr = ""
	e.saturationStreak = 0
	e.newScreensWindow = nil
	e.newTransWindow = nil
	e.terminationReason = ""
	e.saturatedNoProgress = false
	e.autoAppliedHashes = make(map[string]bool)
	if e.transitionLog != nil {
		_ = e.transitionLog.Close()
		e.transitionLog = nil
	}
	if path := strings.TrimSpace(e.cfg.TransitionLogPath); path != "" {
		if dir := filepath.Dir(path); dir != "" {
			_ = os.MkdirAll(dir, 0750)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err == nil {
			e.transitionLog = f
		}
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
	e.uniqueInputSeen = make(map[uint64]struct{}, len(saved.UniqueInputValues))
	e.uniqueInputSample = make(map[string]bool, len(saved.UniqueInputValues))
	for k, v := range saved.UniqueInputValues {
		if !v {
			continue
		}
		e.uniqueInputSeen[hashValue(k)] = struct{}{}
		if len(e.uniqueInputSample) < maxSavedInputSample {
			e.uniqueInputSample[k] = true
		}
	}
	// The saved count is authoritative: the sample is bounded and never
	// holds non-display values, so it can be smaller than the count.
	e.uniqueInputCount = len(e.uniqueInputSeen)
	if saved.UniqueInputs > e.uniqueInputCount {
		e.uniqueInputCount = saved.UniqueInputs
	}
	e.aidKeyCounts = make(map[string]int, len(saved.AIDKeyCounts))
	for k, v := range saved.AIDKeyCounts {
		e.aidKeyCounts[k] = v
	}
	e.transitions = make([]Transition, len(saved.TransitionList))
	copy(e.transitions, saved.TransitionList)
	e.steps = make([]session.WorkflowStep, len(saved.Steps))
	copy(e.steps, saved.Steps)
	e.stepsHidden = make([]bool, len(e.steps))
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
	// Runs saved before field writes were keyed by field length carry keys
	// nothing can look up; move what they learned under the real keys.
	normalizeMindMapSignatures(e.mindMap)
	normalizeMindMapFieldKeys(e.mindMap)
	e.touchMindMapLocked()
	// The first screen is the one the saved run says it started on. Runs
	// saved before that was recorded fall back to the oldest attempt still
	// in the (capped) history, which is only right for a short run.
	e.firstScreenHash = strings.TrimSpace(saved.FirstScreenHash)
	if e.firstScreenHash == "" && len(saved.Attempts) > 0 {
		e.firstScreenHash = strings.TrimSpace(saved.Attempts[0].FromHash)
	}
	e.firstScreenIsGate = false
	if area := e.mindMap.Areas[e.firstScreenHash]; area != nil && area.HiddenFieldCount > 0 {
		e.firstScreenIsGate = true
	}
	// Carry over what the saved run learned ends the session.
	e.sessionEndingValues = nil
	for _, area := range e.mindMap.Areas {
		if area == nil {
			continue
		}
		for _, values := range area.SessionEndingValues {
			for _, v := range values {
				if e.sessionEndingValues == nil {
					e.sessionEndingValues = make(map[string]struct{})
				}
				e.sessionEndingValues[strings.ToUpper(strings.TrimSpace(v))] = struct{}{}
			}
		}
	}
	e.hiddenValues = nil
	e.edgeInputs = nil
	// A run started without a seed draws one from the clock. Resuming it
	// with the seed it saved (advanced by the steps already taken, so the
	// resumed run does not replay the first one's choices) makes the
	// continuation reproducible.
	if e.cfg.Seed == 0 && saved.Seed != 0 {
		e.seed = saved.Seed
		e.rng = rand.New(rand.NewSource(saved.Seed + int64(saved.StepsRun))) //nolint:gosec
	}

	e.active = true
	e.startedAt = time.Now()
	e.stoppedAt = time.Time{}
	e.lastErr = ""
	e.stopCh = make(chan struct{})
	e.resetSaturationStateLocked()
	// resetSaturationStateLocked clears transitionTuples, so without this the
	// resumed run wouldn't recognize any edge discovered before the resume as
	// already-seen — re-discovering it once more would append one more
	// Transition entry for it on every resume, rather than staying capped at
	// one entry per edge for the run's entire life (see run()'s newTuple
	// gating on e.transitions).
	for _, tr := range e.transitions {
		e.transitionTuples[tr.FromHash+"|"+tr.ToHash+"|"+tr.AIDKey] = true
	}

	go e.run()
	return nil
}

// run is the main exploration loop executed in a goroutine.
// screenSnapshot returns an immutable copy of the host's current screen. The
// terminal rebuilds its screen in place on every read from the host, so this
// background goroutine reading the live one while a request refreshes the same
// session could observe a half-rebuilt field list. The copy is taken under the
// terminal's own lock, which is the whole of the fix and now the only thing on
// offer — there is no accessor for the live screen to fall back to.
func (e *Engine) screenSnapshot() *host.Screen {
	return e.h.GetScreenSnapshot()
}

// retryHostOp runs a transient host operation (screen read, key send) with a
// bounded number of retries and exponential backoff. A single timeout or
// socket blip should not terminate a whole chaos run, so the op is retried
// hostOpMaxAttempts times. The backoff wait is cancellable via stopCh so a
// user-requested Stop is honoured promptly even while retrying. It returns the
// number of *extra* attempts consumed (0 on first-try success) and the final
// error (nil on success).
func (e *Engine) retryHostOp(fn func() error) (retries int, err error) {
	backoff := hostOpBaseBackoff
	for attempt := 1; attempt <= hostOpMaxAttempts; attempt++ {
		if err = fn(); err == nil {
			return attempt - 1, nil
		}
		if attempt == hostOpMaxAttempts {
			break
		}
		select {
		case <-e.stopCh:
			return attempt - 1, err
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return hostOpMaxAttempts - 1, err
}

// appendSlidingBool appends v to *win and keeps len(*win) <= maxLen by
// dropping the oldest entry when it grows past the cap.
func appendSlidingBool(win *[]bool, v bool, maxLen int) {
	if maxLen <= 0 {
		*win = append(*win, v)
		return
	}
	*win = append(*win, v)
	if len(*win) > maxLen {
		*win = (*win)[len(*win)-maxLen:]
	}
}

// signatureCanonicalKey derives a stable canonical area key from the layout
// dedup signature. It returns the first 16 hex chars of SHA-256(sig), which
// is enough to avoid practical collisions for any single chaos run while
// staying short enough to read in logs and JSON.
func signatureCanonicalKey(sig string) string {
	if sig == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sig))
	return hex.EncodeToString(sum[:8])
}

// similarityRatio scores how alike two dedup signatures are, from 0 (nothing
// alike) to 1 (identical). It's edit-distance based rather than a positional
// character compare: a single character inserted or removed anywhere in a
// signature shifts every character after it out of positional alignment,
// which made a true near-duplicate (e.g. one extra digit in a counter field)
// score as almost entirely different instead of one edit away.
func similarityRatio(a, b string) float64 {
	var prevRow, currRow []int
	return similarityRatioBuf(a, b, &prevRow, &currRow)
}

// similarityRatioBuf is similarityRatio with caller-supplied DP row buffers
// (see levenshteinDistanceBuf). The mind-map dedup loop that canonicalizes
// every observed screen compares one candidate against every known area, so
// it calls this once or twice per area; passing the same two buffers down
// the whole loop lets them grow once and be reused, instead of the two
// fresh slice allocations levenshteinDistance made per comparison.
func similarityRatioBuf(a, b string, prevRow, currRow *[]int) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	ar := []rune(a)
	br := []rune(b)
	maxLen := len(ar)
	if len(br) > maxLen {
		maxLen = len(br)
	}
	if maxLen == 0 {
		return 1
	}
	dist := levenshteinDistanceBuf(ar, br, prevRow, currRow)
	ratio := 1 - float64(dist)/float64(maxLen)
	if ratio < 0 {
		ratio = 0
	}
	return ratio
}

// levenshteinDistance computes the classic edit distance (insertions,
// deletions, substitutions each cost 1) between two rune slices, using a
// two-row DP to keep memory at O(min(len(a), len(b))) instead of O(len(a) *
// len(b)).
func levenshteinDistance(a, b []rune) int {
	var prevRow, currRow []int
	return levenshteinDistanceBuf(a, b, &prevRow, &currRow)
}

// levenshteinDistanceBuf is levenshteinDistance with the two DP row slices
// passed in by the caller instead of allocated fresh each call. A slice
// that already has enough capacity is reused (just re-sliced to length);
// only the first call at a given size class grows it. levenshteinDistance
// keeps the old single-call signature for its other callers and tests.
func levenshteinDistanceBuf(a, b []rune, prevRow, currRow *[]int) int {
	// A common prefix and suffix never change the edit distance, so strip
	// them before the O(len(a)*len(b)) DP. Dedup signatures of the same
	// layout differ in a few cells out of ~1,900, which turns a ~4M-cell
	// table into a near-empty one. The result is identical.
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
	}
	for len(a) > 0 && len(b) > 0 && a[len(a)-1] == b[len(b)-1] {
		a, b = a[:len(a)-1], b[:len(b)-1]
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	// Iterate over the shorter slice's length as the row width.
	if len(a) < len(b) {
		a, b = b, a
	}
	need := len(b) + 1
	if cap(*prevRow) < need {
		*prevRow = make([]int, need)
	} else {
		*prevRow = (*prevRow)[:need]
	}
	if cap(*currRow) < need {
		*currRow = make([]int, need)
	} else {
		*currRow = (*currRow)[:need]
	}
	pr, cr := *prevRow, *currRow
	for j := range pr {
		pr[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := pr[j] + 1
			ins := cr[j-1] + 1
			sub := pr[j-1] + cost
			min := del
			if ins < min {
				min = ins
			}
			if sub < min {
				min = sub
			}
			cr[j] = min
		}
		pr, cr = cr, pr
	}
	*prevRow, *currRow = pr, cr
	return pr[len(b)]
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

func mergeScreenHints(base, override *ScreenHint) *ScreenHint {
	if base == nil && override == nil {
		return nil
	}
	merged := ScreenHint{}
	if base != nil {
		merged.KnownData = append(merged.KnownData, base.KnownData...)
		merged.KnownKeys = append(merged.KnownKeys, base.KnownKeys...)
		merged.BlockedKeys = append(merged.BlockedKeys, base.BlockedKeys...)
		if len(base.KeyAssignments) > 0 {
			merged.KeyAssignments = make(map[string]string, len(base.KeyAssignments))
			for k, v := range base.KeyAssignments {
				merged.KeyAssignments[k] = v
			}
		}
	}
	if override != nil {
		merged.KnownData = append(merged.KnownData, override.KnownData...)
		merged.KnownKeys = append(merged.KnownKeys, override.KnownKeys...)
		merged.BlockedKeys = append(merged.BlockedKeys, override.BlockedKeys...)
		if len(override.KeyAssignments) > 0 {
			if merged.KeyAssignments == nil {
				merged.KeyAssignments = make(map[string]string, len(override.KeyAssignments))
			}
			for k, v := range override.KeyAssignments {
				merged.KeyAssignments[k] = v
			}
		}
	}
	clean := sanitizeScreenHint(merged)
	if len(clean.KnownData) == 0 && len(clean.KnownKeys) == 0 && len(clean.BlockedKeys) == 0 && len(clean.KeyAssignments) == 0 {
		return nil
	}
	return &clean
}

// snapshotKeyBoostsLocked returns a map of AID key → boost amount derived
// from the MindMap statistics for the given area.  Keys that previously caused
// screen transitions receive a positive boost proportional to their
// progression count.  Keys that have been pressed at least minPressesForPenalty
// times from this screen without ever causing a transition receive a negative
// boost (penalty) to steer the engine toward less-explored alternatives.
// Must be called with e.mu held.
func (e *Engine) snapshotKeyBoostsLocked(hash string, targetFieldCount int) map[string]int {
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
			if targetFieldCount == 1 {
				boosts[key] += kp.SingleFieldProgressions * 4
				if kp.SingleFieldProgressions > 0 && kp.Presses > 0 {
					// Reward keys that convert single-field attempts reliably.
					boosts[key] += (kp.SingleFieldProgressions * 20) / kp.Presses
				}
			} else if targetFieldCount > 1 {
				boosts[key] += kp.MultiFieldProgressions * 4
				if kp.MultiFieldProgressions > 0 && kp.Presses > 0 {
					// Reward keys that convert multi-field attempts reliably.
					boosts[key] += (kp.MultiFieldProgressions * 20) / kp.Presses
				}
			}
		} else if kp.Presses >= minPressesForPenalty && kp.ContentChanges == 0 {
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

// defaultChaosHints are the engine's built-in fallback values and key
// labels. Nothing in here may end a session or change data: no LOGOFF or
// LOGON among the values (typed into a command line they sign the user off,
// or sign on as a user called LOGON), and no label that maps to PF3, to a
// commit action, or to a cursor key. A built-in mapping only ever re-weights
// a key the configuration already allows (see hintKeyBoostsForScreen).
func defaultChaosHints() []Hint {
	return []Hint{
		{
			KnownData: []string{
				"Y", "N", "1", "0", "01", "99", "0001", "1234",
				"TEST", "DEMO", "USER", "ADMIN", "PASS", "HELP",
				"MENU", "MAIN", "LIST", "DETAIL", "SEARCH", "ALL",
				"ALICE", "SMITH", "BOB", "CAROL", "JOHN",
				"IPL", "TSO", "CICS", "ISPF",
				"IKJEFT01", "BPXBATCH",
				"20240101", "20250101",
			},
			KeyAssignments: map[string]string{
				"HELP":         "PF1",
				"NEXT":         "PF8",
				"PAGE FORWARD": "PF8",
				"PREVIOUS":     "PF7",
				"PREV":         "PF7",
				"PAGE BACK":    "PF7",
				"REFRESH":      "PF5",
				"SELECT":       "Enter",
				"DETAIL":       "Enter",
				"ACCEPT":       "Enter",
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

func mergeChaosKeySets(base, extra map[string]struct{}) map[string]struct{} {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(base)+len(extra))
	for key := range base {
		out[key] = struct{}{}
	}
	for key := range extra {
		out[key] = struct{}{}
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
	for _, key := range fallbackChaosKeyCandidates {
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
	startRow := screen.Height - legendRowCount
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

// mergeSortedUniqueStrings returns a sorted, deduplicated union of base + add.
func mergeSortedUniqueStrings(base, add []string) []string {
	seen := make(map[string]struct{}, len(base)+len(add))
	for _, s := range base {
		seen[s] = struct{}{}
	}
	for _, s := range add {
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// negativeKeyBoostsOnly strips the positive entries from a legend-inferred
// boost map, keeping only the penalties (exit/logoff-style labels). Used when
// AutoPreferNavigationKeys is off, so declining the navigation steering does
// not also drop the soft protection against session-ending keys.
func negativeKeyBoostsOnly(boosts map[string]int) map[string]int {
	if len(boosts) == 0 {
		return nil
	}
	out := make(map[string]int)
	for k, v := range boosts {
		if v < 0 {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

// hintKeyBoostsForScreen boosts the keys that hint key assignments map to
// labels shown in the screen's key legend.
//
// Labels are matched as whole words and only in the legend rows. Matching
// them as substrings of the whole screen found "UP" in "GROUP UPDATE" and
// "RETURN" in any sentence that used the word, and boosted keys on the
// strength of body text that had nothing to do with keys.
//
// A built-in mapping can only re-weight a key the configuration already
// allows, and only an attention key: the defaults are a guess about what a
// label means, and a guess must not be what puts PF3, or a cursor key that
// never reaches the host, into play. A mapping the user supplied is an
// instruction and is applied as given.
func (e *Engine) hintKeyBoostsForScreen(screen *host.Screen) map[string]int {
	if e == nil || screen == nil || len(e.hintKeyMappings) == 0 {
		return nil
	}
	text := legendText(screen)
	if text == "" {
		return nil
	}
	boosts := make(map[string]int)
	for label, key := range e.hintKeyMappings {
		if label == "" || key == "" {
			continue
		}
		if !containsLabelWord(text, label) {
			continue
		}
		if e.builtinHintLabels[label] {
			if !isAIDKeyName(key) || !containsString(e.candidateKeys, key) || labelIsDenied(label) {
				continue
			}
		}
		// Strongly prefer keys explicitly mapped to labels present on screen,
		// while still allowing exploration through weighted randomness.
		boosts[key] += 100
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
		// A hint scoped to one screen may name a label anywhere on it, but
		// still as a whole word.
		if containsLabelWord(text, nLabel) {
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

	compacted := strings.NewReplacer(" ", "", "_", "", "-", "", "(", "", ")", "").Replace(trimmed)
	compactedUpper := strings.ToUpper(compacted)
	compactedLower := strings.ToLower(compacted)

	// Accept workflow-style aliases (e.g. PressPF3, Press Enter, Press_Back_Tab).
	if strings.HasPrefix(compactedUpper, "PRESS") && len(compactedUpper) > len("PRESS") {
		compactedUpper = strings.TrimPrefix(compactedUpper, "PRESS")
		compactedLower = strings.TrimPrefix(compactedLower, "press")
	}

	// Atoi accepts a leading sign, so "PF+3" must be screened as digits first.
	if strings.HasPrefix(compactedUpper, "PF") {
		if digits := strings.TrimPrefix(compactedUpper, "PF"); isDigitsOnlyString(digits) {
			n, err := strconv.Atoi(digits)
			if err == nil && n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n)
			}
		}
	}
	if strings.HasPrefix(compactedUpper, "PA") {
		if digits := strings.TrimPrefix(compactedUpper, "PA"); isDigitsOnlyString(digits) {
			n, err := strconv.Atoi(digits)
			if err == nil && n >= 1 && n <= 3 {
				return fmt.Sprintf("PA(%d)", n)
			}
		}
	}
	if strings.HasPrefix(compactedUpper, "F") {
		if digits := strings.TrimPrefix(compactedUpper, "F"); isDigitsOnlyString(digits) {
			n, err := strconv.Atoi(digits)
			if err == nil && n >= 1 && n <= 24 {
				return fmt.Sprintf("PF(%d)", n)
			}
		}
	}
	switch compactedLower {
	case "enter", "return":
		return "Enter"
	case "tab":
		return "Tab"
	case "backtab":
		return "BackTab"
	case "clear":
		return "Clear"
	case "reset":
		return "Reset"
	case "eraseeof":
		return "EraseEOF"
	case "eraseinput":
		return "EraseInput"
	case "dup":
		return "Dup"
	case "fieldmark":
		return "FieldMark"
	case "sysreq":
		return "SysReq"
	case "attn":
		return "Attn"
	case "newline":
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
		if len(clean.KnownData) == 0 && len(clean.KnownKeys) == 0 && len(clean.BlockedKeys) == 0 && len(clean.KeyAssignments) == 0 {
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
	blockedKeys := make([]string, 0, len(h.BlockedKeys))
	seenBlockedKeys := make(map[string]bool)
	for _, raw := range h.BlockedKeys {
		k := normalizeChaosKeyName(raw)
		if k == "" || seenBlockedKeys[k] {
			continue
		}
		seenBlockedKeys[k] = true
		blockedKeys = append(blockedKeys, k)
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
		BlockedKeys:    blockedKeys,
		KeyAssignments: assignments,
	}
}

func (e *Engine) generateValueForField(f *host.Field, preferTransaction bool) string {
	return e.generateValueForFieldWith(f, preferTransaction, nil, nil, nil)
}

// genContext carries what the generator knows about the current screen. It
// is assembled under the engine lock and then used without it.
type genContext struct {
	known          map[string][]string // values that moved the run on
	tried          map[string][]string // values already written
	rejected       map[string][]string // values the application refused
	candidates     map[string][]string // values read off the screen
	candidateTried map[string][]string
	commandField   string
	commandPool    []string
	screenHint     *ScreenHint
	forceHints     bool
	// sessionEnding holds the values (upper-cased) that ended the session
	// earlier in the run. It is nil on the first screen, where signing on
	// again with a value typed elsewhere is not the same act.
	sessionEnding map[string]struct{}
}

func (e *Engine) generateValueForFieldWithPolicy(
	f *host.Field,
	preferTransaction bool,
	knownValues map[string][]string,
	triedValues map[string][]string,
	screenHint *ScreenHint,
	forceHints bool,
) string {
	return e.generateFieldValue(f, preferTransaction, &genContext{
		known:      knownValues,
		tried:      triedValues,
		screenHint: screenHint,
		forceHints: forceHints,
	})
}

// generateFieldValue chooses a value for a field. In order of preference:
// a value known to work here, a value the screen itself offers, a value
// tried before, a hint, a length probe, and last a plausible filler. A
// value that was rejected in this field, or that ended the session, is
// never chosen.
func (e *Engine) generateFieldValue(f *host.Field, preferTransaction bool, ctx *genContext) string {
	if ctx == nil {
		ctx = &genContext{}
	}
	length := fieldLength(f)
	key := ""
	if length > 0 {
		key = mindMapFieldKey(f.StartY+1, f.StartX+1, length)
	}
	banned := func(v string) bool {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return false
		}
		if _, ended := ctx.sessionEnding[strings.ToUpper(trimmed)]; ended {
			return true
		}
		return key != "" && containsString(ctx.rejected[key], trimmed)
	}

	// 1. Prefer values already known to work on this screen / field position.
	if key != "" {
		if values, ok := ctx.known[key]; ok && len(values) > 0 {
			// Use a known working value 80 % of the time so that the engine
			// still occasionally explores new inputs rather than repeating.
			chance := 80
			if extra := len(values) - 1; extra > 0 {
				chance += extra * 3
				if chance > 95 {
					chance = 95
				}
			}
			chance = e.scaleLearnedInputReuseChance(chance)
			if e.rng.Intn(100) < chance {
				if v := pickHintValueForFieldPool(e.rng, values, length, f.IsNumeric()); v != "" && !banned(v) {
					return v
				}
			}
		}
	}
	// 2. Values the screen offers: its own menu options and listed commands,
	// and on a command line the commands seen elsewhere in the run.
	if key != "" && !ctx.forceHints {
		offered := ctx.candidates[key]
		if key == ctx.commandField {
			for _, cmd := range ctx.commandPool {
				if len([]rune(cmd)) <= length {
					offered = appendCandidate(offered, cmd)
				}
			}
		}
		if len(offered) > 0 {
			var fresh, usable []string
			for _, v := range offered {
				if banned(v) {
					continue
				}
				usable = append(usable, v)
				if !containsString(ctx.candidateTried[key], v) && !containsString(ctx.tried[key], v) {
					fresh = append(fresh, v)
				}
			}
			roll := e.rng.Intn(100)
			if len(fresh) > 0 && roll < 75 {
				return fresh[e.rng.Intn(len(fresh))]
			}
			if len(usable) > 0 && roll < 30 {
				return usable[e.rng.Intn(len(usable))]
			}
		}
	}
	// 3. Reuse previously tried values for this screen/field some of the time.
	// This helps chaos continue functional discovery without requiring a prior
	// successful transition for every field.
	if key != "" {
		if values, ok := ctx.tried[key]; ok && len(values) > 0 {
			// Lower probability than known-working values to preserve breadth.
			chance := 35
			if extra := len(values) - 1; extra > 0 {
				chance += extra * 2
				if chance > 60 {
					chance = 60
				}
			}
			chance = e.scaleLearnedInputReuseChance(chance)
			if e.rng.Intn(100) < chance {
				if v := pickHintValueForFieldPool(e.rng, values, length, f.IsNumeric()); v != "" && !banned(v) {
					return v
				}
			}
		}
	}
	// 4. Fall back to user-supplied hints.
	if hinted := e.hintValueForFieldWithPolicy(f, preferTransaction, ctx.screenHint, ctx.forceHints); hinted != "" {
		if ctx.forceHints || !banned(hinted) {
			return hinted
		}
	}
	// 5. Periodically emit a length-probe value to ensure each field exercises
	// each value class that can actually be written (short, half, exact) over
	// the course of a run, even when neither hints nor learned values fire.
	if !ctx.forceHints && e.rng.Intn(100) < 35 {
		if v := e.lengthProbeValueForField(f, ctx.tried); v != "" && !banned(v) {
			return v
		}
	}
	// 6. A filler value appropriate for the field type.
	for try := 0; try < 3; try++ {
		if v := e.fillerValue(f); v != "" && !banned(v) {
			return v
		}
	}
	return ""
}

// randomProbeChance is how often, out of 100, the filler is a full-width
// random string. Forty random characters almost never mean anything to an
// application, so this is a probe of how it handles noise, not the default.
const randomProbeChance = 15

// fillerValue produces a value for a field nothing more specific is known
// about: usually one of the short built-in values that fits, occasionally a
// full-width random string.
func (e *Engine) fillerValue(f *host.Field) string {
	if e.rng.Intn(100) < randomProbeChance {
		return e.generateValue(f)
	}
	length := e.effectiveFieldWriteLength(f)
	if length <= 0 {
		return ""
	}
	if v := pickHintValueForFieldPool(e.rng, e.defaultHintData, length, f.IsNumeric()); v != "" {
		return v
	}
	return e.generateShortValue(f, length)
}

// generateShortValue produces a short random value: one to four characters,
// which is what most command, option and code fields take.
func (e *Engine) generateShortValue(f *host.Field, length int) string {
	n := 1 + e.rng.Intn(4)
	if n > length {
		n = length
	}
	const digits = "0123456789"
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		if f.IsNumeric() {
			b[i] = digits[e.rng.Intn(len(digits))]
		} else {
			b[i] = letters[e.rng.Intn(len(letters))]
		}
	}
	return string(b)
}

// fieldValueClass labels each generated value by length relative to the
// field's declared capacity so the engine can balance coverage across
// classes and the chaos_report can show which classes have been exercised.
type fieldValueClass string

const (
	fieldValueClassEmpty    fieldValueClass = "empty"
	fieldValueClassShort    fieldValueClass = "short"
	fieldValueClassHalf     fieldValueClass = "half"
	fieldValueClassExact    fieldValueClass = "exact"
	fieldValueClassOverflow fieldValueClass = "overflow"
)

// classifyValueForField returns the length class for value against a field of
// the given declared length. Used by the chaos_report to summarize coverage
// and by lengthProbeValueForField to choose an untried class.
func classifyValueForField(value string, fieldLen int) fieldValueClass {
	n := len([]rune(value))
	switch {
	case n == 0:
		return fieldValueClassEmpty
	case n == 1:
		return fieldValueClassShort
	case fieldLen > 0 && n > fieldLen:
		return fieldValueClassOverflow
	case fieldLen > 0 && n == fieldLen:
		return fieldValueClassExact
	case fieldLen > 0 && n >= fieldLen/2:
		return fieldValueClassHalf
	default:
		return fieldValueClassShort
	}
}

// probeClassesForField returns the length classes that can actually be
// written into a field under the current configuration, in exploration
// order.
//
// "empty" and "overflow" used to be on this list unconditionally and could
// never come off it. An empty value is skipped by the write path (there is
// nothing to type), and with ForceOverrideExistingInputs on every value is
// cut to the field's width before it is written, so an overflow probe
// arrived as an exact-length one. Both stayed "untried" for the whole run
// and kept being drawn. A class is now only offered when a write can
// produce it: overflow when values are written as generated and one more
// character still fits under MaxFieldLength, empty never. A field left
// blank is still exercised, by every attempt that targets a subset of the
// screen's fields.
func (e *Engine) probeClassesForField(length int) []fieldValueClass {
	classes := []fieldValueClass{fieldValueClassExact, fieldValueClassHalf}
	if !e.cfg.ForceOverrideExistingInputs {
		maxLen := e.cfg.MaxFieldLength
		if maxLen <= 0 {
			maxLen = 40
		}
		if length+1 <= maxLen {
			classes = append(classes, fieldValueClassOverflow)
		}
	}
	return append(classes, fieldValueClassShort)
}

// lengthProbeValueForField returns a value that exercises a writable length
// class (short, half, exact, and overflow where possible) that has not yet
// been tried against the given field. Returns "" when every such class has
// already been seen.
func (e *Engine) lengthProbeValueForField(f *host.Field, triedValues map[string][]string) string {
	length := fieldLength(f)
	if length <= 0 {
		return ""
	}
	// Classes are judged against what can be written: a field wider than
	// MaxFieldLength is only ever filled up to that cap, and judged against
	// its full width would never count as "exact".
	width := e.effectiveFieldWriteLength(f)
	row := f.StartY + 1
	col := f.StartX + 1
	tried := triedValues[mindMapFieldKey(row, col, length)]
	triedClasses := make(map[fieldValueClass]bool, len(tried))
	for _, v := range tried {
		triedClasses[classifyValueForField(v, width)] = true
	}
	var untried []fieldValueClass
	for _, c := range e.probeClassesForField(width) {
		if !triedClasses[c] {
			untried = append(untried, c)
		}
	}
	if len(untried) == 0 {
		return ""
	}
	pick := untried[e.rng.Intn(len(untried))]
	return e.makeProbeValue(f, width, pick)
}

func (e *Engine) makeProbeValue(f *host.Field, length int, class fieldValueClass) string {
	switch class {
	case fieldValueClassShort:
		if f.IsNumeric() {
			return "9"
		}
		return "A"
	case fieldValueClassHalf:
		return e.repeatProbeRune(f, length/2)
	case fieldValueClassExact:
		return e.repeatProbeRune(f, length)
	case fieldValueClassOverflow:
		// length+1 stays within MaxFieldLength to avoid pathological writes.
		maxLen := e.cfg.MaxFieldLength
		if maxLen <= 0 {
			maxLen = 40
		}
		target := length + 1
		if target > maxLen {
			target = maxLen
		}
		return e.repeatProbeRune(f, target)
	}
	return ""
}

func (e *Engine) repeatProbeRune(f *host.Field, n int) string {
	if n <= 0 {
		return ""
	}
	if f.IsNumeric() {
		const digits = "0123456789"
		b := make([]byte, n)
		for i := range b {
			b[i] = digits[e.rng.Intn(len(digits))]
		}
		return string(b)
	}
	// Single capital letter, repeated. Easy to spot in logs and obeys the
	// "no leading space" rule the random generator enforces.
	b := make([]byte, n)
	letter := byte('A') + byte(e.rng.Intn(26))
	for i := range b {
		b[i] = letter
	}
	return string(b)
}

// generateValueForFieldWith extends generateValueForField with optional
// per-screen known-working values learned from previous transitions. Callers
// that hold the engine lock must snapshot area values before calling; this
// function must not touch e.mindMap directly.
func (e *Engine) generateValueForFieldWith(f *host.Field, preferTransaction bool, knownValues map[string][]string, triedValues map[string][]string, screenHint *ScreenHint) string {
	return e.generateValueForFieldWithPolicy(f, preferTransaction, knownValues, triedValues, screenHint, false)
}

func (e *Engine) hintValueForFieldWithPolicy(f *host.Field, preferTransaction bool, screenHint *ScreenHint, force bool) string {
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

	if screenHint != nil && len(screenHint.KnownData) > 0 && (force || e.rng.Intn(100) < 70) {
		if v := pickHintValueForFieldPool(e.rng, screenHint.KnownData, length, numeric); v != "" {
			return v
		}
	}
	if preferTransaction && len(e.hintTransactions) > 0 && (force || e.rng.Intn(100) < 65) {
		if v := pickHintValueForFieldPool(e.rng, e.hintTransactions, length, numeric); v != "" {
			return v
		}
	}
	pool := e.hintKnownData
	if len(pool) == 0 {
		pool = e.hintTransactions
	}
	if len(pool) > 0 && (force || e.rng.Intn(100) < 55) {
		if v := pickHintValueForFieldPool(e.rng, pool, length, numeric); v != "" {
			return v
		}
	}
	pool = e.defaultHintData
	if len(pool) == 0 {
		pool = e.defaultHintTx
	}
	if len(pool) > 0 && (force || e.rng.Intn(100) < 35) {
		if v := pickHintValueForFieldPool(e.rng, pool, length, numeric); v != "" {
			return v
		}
	}
	return ""
}

func (e *Engine) prepareFieldWriteValue(f *host.Field, candidate string) string {
	return e.prepareFieldWriteValueWithPolicy(f, candidate, false)
}

func (e *Engine) prepareFieldWriteValueWithPolicy(f *host.Field, candidate string, allowSamePrefilled bool) string {
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
	if !allowSamePrefilled && sameNonBlankFieldValue(current, proposed) {
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
	// A numeric value is written exactly as it is. Padding it out with
	// random digits turned the hint "1" into "146873" in a six-digit field:
	// a different number, typed with the confidence of a known-good one.
	// The rest of the field is erased by the write instead (see writeField).
	return string(runes)
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
	return e.chooseAIDKeyWithExploration(boosts, nil)
}

// chooseAIDKeyWithExploration extends chooseAIDKeyBoosted with a second boost
// map that bypasses LearnedKeyReuseBias scaling. Learned/hint boosts encode
// reuse of what already worked and are scaled by the bias; exploration boosts
// (novelty, frontier proximity) encode the opposite pull and are applied
// verbatim, so turning the reuse bias down genuinely shifts selection toward
// exploration instead of muting both.
func (e *Engine) chooseAIDKeyWithExploration(boosts, explorationBoosts map[string]int) string {
	return e.chooseAIDKeyBlocked(boosts, explorationBoosts, e.blacklistedKeysSnapshot())
}

// chooseAIDKeyBlocked is chooseAIDKeyWithExploration given the complete set
// of keys that must not be pressed on the current screen: the blacklist,
// the screen hint's blocked keys and the run's own auto-blocks. Blocked keys
// are left out of the draw altogether. Drawing first and swapping a blocked
// result for the fallback key afterwards turned every blocked draw into
// Enter, which both skewed the distribution and hid the other keys.
func (e *Engine) chooseAIDKeyBlocked(boosts, explorationBoosts map[string]int, blocked map[string]struct{}) string {
	weights := e.cfg.AIDKeyWeights
	if len(weights) == 0 && len(boosts) == 0 && len(explorationBoosts) == 0 {
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
		effective[key] += e.scaleLearnedKeyReuseBoost(b)
	}
	for rawKey, b := range explorationBoosts {
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

func clampReuseBias(bias float64) float64 {
	if bias < 0 {
		return 0
	}
	if bias > 1 {
		return 1
	}
	return bias
}

func (e *Engine) learnedInputReuseBias() float64 {
	if e == nil {
		return 1.0
	}
	return clampReuseBias(e.cfg.LearnedInputReuseBias)
}

func (e *Engine) learnedKeyReuseBias() float64 {
	if e == nil {
		return 1.0
	}
	return clampReuseBias(e.cfg.LearnedKeyReuseBias)
}

func (e *Engine) scaleLearnedInputReuseChance(chance int) int {
	if chance <= 0 {
		return 0
	}
	scaled := int(float64(chance) * e.learnedInputReuseBias())
	if scaled < 0 {
		return 0
	}
	if scaled > 100 {
		return 100
	}
	return scaled
}

func (e *Engine) scaleLearnedKeyReuseBoost(boost int) int {
	if boost == 0 {
		return 0
	}
	return int(float64(boost) * e.learnedKeyReuseBias())
}

// hashScreen produces a short stable fingerprint of the screen state based on
// normalized screen text and field structure. Cursor position is intentionally
// excluded: on 3270 terminals the cursor moves freely between input fields
// (e.g. via Tab) without changing the logical screen, so including it would
// cause the same screen to appear as many different "areas" in the MindMap and
// make Tab key presses register as false screen transitions.
//
// Unprotected field contents are normalized to spaces so the same logical
// screen is not split into many hashes just because chaos typed different data
// into input fields (or the host redisplays previously entered values).
// Protected text and field positions/attribute codes are still included so two
// screens with identical input layouts but different labels/help text remain
// distinguishable.
func hashScreen(s *host.Screen) string {
	if s == nil {
		return ""
	}
	width, height := screenDimensions(s)
	if width <= 0 || height <= 0 {
		return ""
	}

	maskedInputCells := make([]bool, width*height)
	for _, f := range s.Fields {
		if f == nil || f.IsProtected() {
			continue
		}
		curX, curY := f.StartX, f.StartY
		endX, endY := f.EndX, f.EndY
		for {
			if curX >= 0 && curX < width && curY >= 0 && curY < height {
				maskedInputCells[(curY*width)+curX] = true
			}
			if curX == endX && curY == endY {
				break
			}
			curX++
			if curX >= width {
				curX = 0
				curY++
				if curY >= height {
					break
				}
			}
		}
	}

	// Build the hashed text in one buffer and hash it once. This runs on every
	// chaos step, and a Fprintf per cell (width*height calls through the
	// io.Writer interface) dominated its cost. utf8.AppendRune writes the same
	// bytes %c does, including U+FFFD for an invalid rune, so hashes are
	// unchanged.
	buf := make([]byte, 0, (width+1)*height+32+len(s.Fields)*24)
	buf = strconv.AppendInt(buf, int64(width), 10)
	buf = append(buf, 'x')
	buf = strconv.AppendInt(buf, int64(height), 10)
	buf = append(buf, '|')
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			ch := s.CharAt(x, y)
			if ch == 0 || maskedInputCells[(y*width)+x] {
				ch = ' '
			}
			if ch >= 0 && ch < utf8.RuneSelf {
				buf = append(buf, byte(ch))
			} else {
				buf = utf8.AppendRune(buf, ch)
			}
		}
		buf = append(buf, '\n')
	}
	buf = append(buf, '|')
	buf = strconv.AppendInt(buf, int64(len(s.Fields)), 10)
	for _, f := range s.Fields {
		if f == nil {
			continue
		}
		buf = append(buf, '|')
		buf = strconv.AppendInt(buf, int64(f.StartY), 10)
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, int64(f.StartX), 10)
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, int64(f.EndY), 10)
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, int64(f.EndX), 10)
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, int64(f.FieldCode), 10)
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])[:16]
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
func (e *Engine) selectTargetFieldsForScreen(hash string, fields []*host.Field) []*host.Field {
	if preferred := e.preferredFieldTargetCountForScreen(hash, len(fields)); preferred > 0 {
		return e.selectTargetFieldsWithCount(fields, preferred)
	}
	return e.selectTargetFields(fields)
}

func (e *Engine) preferredFieldTargetCountForScreen(hash string, total int) int {
	if total <= 1 {
		return total
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.mindMap == nil || strings.TrimSpace(hash) == "" {
		return 0
	}
	area, ok := e.mindMap.Areas[hash]
	if !ok || area == nil || len(area.FieldCountProgressions) == 0 {
		return 0
	}
	bestCount := 0
	bestScore := 0
	for count, score := range area.FieldCountProgressions {
		if count <= 0 || count > total || score <= 0 {
			continue
		}
		if score > bestScore {
			bestScore = score
			bestCount = count
		}
	}
	if bestCount == 0 {
		return 0
	}
	if bestCount == 1 && area.SingleFieldProgressions <= area.MultiFieldProgressions {
		return 0
	}
	if bestCount > 1 && area.MultiFieldProgressions < area.SingleFieldProgressions {
		return 0
	}
	// Keep exploration breadth while still strongly preferring previously
	// successful field-count strategies on this screen.
	if e.rng.Intn(100) < 75 {
		return bestCount
	}
	return 0
}

func (e *Engine) selectTargetFieldsWithCount(fields []*host.Field, targetCount int) []*host.Field {
	n := len(fields)
	if n <= 1 || targetCount <= 0 || targetCount >= n {
		return fields
	}
	selected := make([]int, targetCount)
	copy(selected, e.rng.Perm(n)[:targetCount])
	sort.Ints(selected)
	out := make([]*host.Field, 0, targetCount)
	for _, idx := range selected {
		out = append(out, fields[idx])
	}
	return out
}

func (e *Engine) selectTargetFields(fields []*host.Field) []*host.Field {
	if len(fields) <= 1 {
		return fields
	}

	if allFieldsSingleCell(fields) {
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

func allFieldsSingleCell(fields []*host.Field) bool {
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		if fieldLength(f) != 1 {
			return false
		}
	}
	return true
}

func (e *Engine) selectFirstScreenTargetFields(fields []*host.Field) []*host.Field {
	if len(fields) <= 1 {
		return fields
	}
	// Preserve the single-cell menu heuristic; otherwise initial login/selection
	// screens with multi-character fields should be filled more completely when
	// user hints are present.
	if allFieldsSingleCell(fields) {
		return e.selectTargetFields(fields)
	}
	return fields
}

func (e *Engine) hasUserFieldHints(screenHint *ScreenHint) bool {
	if e == nil {
		return screenHint != nil && len(screenHint.KnownData) > 0
	}
	if len(e.hintTransactions) > 0 || len(e.hintKnownData) > 0 {
		return true
	}
	return screenHint != nil && len(screenHint.KnownData) > 0
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
