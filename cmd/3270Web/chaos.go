// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/safepath"
	"github.com/jnnngs/3270Web/internal/session"
)

// maxSavedChaosRuns caps how many auto-saved chaos run files are retained on
// disk. The newest runs are kept and older ones are pruned so the runs
// directory (and the cost of listing it) does not grow without bound.
const maxSavedChaosRuns = 100

// chaosEngineStore maps session IDs to their running chaos engines. It lives
// outside App so that it can be initialised once and does not need a pointer
// receiver change on App.
type chaosEngineStore struct {
	mu          sync.Mutex
	engines     map[string]*chaos.Engine
	loadedRuns  map[string]*chaos.SavedRun
	screenHints map[string]map[string]chaos.ScreenHint
	removed     map[string]bool
	// syncStops holds one channel per running status goroutine, closed to
	// tell it to stop. See startChaosSync.
	syncStops map[string]chan struct{}
}

func newChaosEngineStore() *chaosEngineStore {
	return &chaosEngineStore{
		engines:     make(map[string]*chaos.Engine),
		loadedRuns:  make(map[string]*chaos.SavedRun),
		screenHints: make(map[string]map[string]chaos.ScreenHint),
		removed:     make(map[string]bool),
		syncStops:   make(map[string]chan struct{}),
	}
}

// beginSync registers a stop channel for a session's status goroutine and
// returns it. A goroutine already running for the session is told to stop
// first: the store holds one engine per session, so a second run means the
// first goroutine has nothing left to watch.
func (s *chaosEngineStore) beginSync(sessionID string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.syncStops[sessionID]; ok {
		close(existing)
	}
	stop := make(chan struct{})
	s.syncStops[sessionID] = stop
	return stop
}

// endSync forgets a stop channel once its goroutine has returned. It must not
// close the channel: the goroutine may be ending because a *newer* run
// replaced it, and the entry under this key is then the new run's.
func (s *chaosEngineStore) endSync(sessionID string, stop <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.syncStops[sessionID]; ok && current == stop {
		delete(s.syncStops, sessionID)
	}
}

// stopSync tells a session's status goroutine to stop, if one is running.
// Safe to call for a session that never ran chaos.
func (s *chaosEngineStore) stopSync(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stop, ok := s.syncStops[sessionID]; ok {
		close(stop)
		delete(s.syncStops, sessionID)
	}
}

// stopAllSync tells every status goroutine to stop. Used on shutdown, and by
// tests that want the background work finished before their fixtures go away.
func (s *chaosEngineStore) stopAllSync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, stop := range s.syncStops {
		close(stop)
		delete(s.syncStops, id)
	}
}

func (s *chaosEngineStore) get(sessionID string) (*chaos.Engine, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.engines[sessionID]
	return e, ok
}

func (s *chaosEngineStore) set(sessionID string, e *chaos.Engine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.removed, sessionID)
	s.engines[sessionID] = e
}

// startIfAbsent ensures only one chaos engine exists per session at a time.
// It holds the store mutex across the active-engine check, the build callback,
// and the resulting Start() call so two concurrent /chaos/start requests for
// the same session cannot both pass the "already running" guard.
//
// Returns (engine, nil, true) on a fresh start, (existing, nil, false) when
// an active engine is already present, or (nil, err, false) if build or
// Start fails. Callers must not invoke Start on the returned engine when
// started is true; it has already been started under the lock.
func (s *chaosEngineStore) startIfAbsent(sessionID string, build func() (*chaos.Engine, error)) (eng *chaos.Engine, err error, started bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.engines[sessionID]; ok && existing.Active() {
		return existing, nil, false
	}
	built, buildErr := build()
	if buildErr != nil {
		return nil, buildErr, false
	}
	if built == nil {
		return nil, fmt.Errorf("chaos engine build returned nil"), false
	}
	if startErr := built.Start(); startErr != nil {
		return nil, startErr, false
	}
	delete(s.removed, sessionID)
	s.engines[sessionID] = built
	return built, nil, true
}

// resumeIfAbsent is startIfAbsent's counterpart for /chaos/resume: it holds
// the store mutex across the active-engine check, the build callback, and
// the resume callback so a concurrent /chaos/start or /chaos/resume for the
// same session cannot race past the "already running" guard and overwrite
// each other's engine in the map (orphaning the loser as an unstoppable
// goroutine still driving the shared host connection).
//
// Returns (engine, nil, true) once resumed and installed, (existing, nil,
// false) when an active engine is already present, or (nil, err, false) if
// build or resume fails.
func (s *chaosEngineStore) resumeIfAbsent(sessionID string, build func() (*chaos.Engine, error), resume func(*chaos.Engine) error) (eng *chaos.Engine, err error, started bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.engines[sessionID]; ok && existing.Active() {
		return existing, nil, false
	}
	built, buildErr := build()
	if buildErr != nil {
		return nil, buildErr, false
	}
	if built == nil {
		return nil, fmt.Errorf("chaos engine build returned nil"), false
	}
	if resumeErr := resume(built); resumeErr != nil {
		return nil, resumeErr, false
	}
	delete(s.removed, sessionID)
	s.engines[sessionID] = built
	return built, nil, true
}

// loadRecordingIfInactive installs run as the session's loaded run, but only
// if no engine is currently active for that session. It holds the store
// mutex across the active-engine check and the map mutation so a concurrent
// /chaos/start cannot have its just-started engine's active status silently
// wiped out by this handler between the check and the write.
func (s *chaosEngineStore) loadRecordingIfInactive(sessionID string, run *chaos.SavedRun) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.engines[sessionID]; ok && existing.Active() {
		return false
	}
	delete(s.removed, sessionID)
	s.loadedRuns[sessionID] = run
	return true
}

func (s *chaosEngineStore) delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.engines, sessionID)
}

// withEngine runs fn against the session's engine while holding the store
// mutex, so the engine cannot be completed (snapshotted and removed) by
// completeEngine while fn is writing to it.
func (s *chaosEngineStore) withEngine(sessionID string, fn func(*chaos.Engine) error) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	eng, ok := s.engines[sessionID]
	if !ok || eng == nil {
		return false, nil
	}
	return true, fn(eng)
}

// completeEngine atomically snapshots the engine, installs the snapshot as
// the session's loaded run, removes the engine from the store, and encodes
// the snapshot to JSON — all while holding the store mutex. Holding the
// mutex across all four steps means writes routed through
// withEngine/withLoadedRun land either in the engine before the snapshot or
// in the loaded run after it — never in the gap, where they would be lost —
// and, just as importantly, the encode itself can never race a concurrent
// business-annotation write to the same run's maps (which is exactly what
// happened when the caller used to encode after this function returned,
// with the store mutex already released: a concurrent withLoadedRun write
// could be mutating MindMap.Areas/BusinessFunctions at the same moment
// json.Marshal was ranging over them, risking Go's fatal "concurrent map
// read and map write"). The returned bytes are ready for WriteRunFile,
// keeping the actual file I/O out of the critical section.
// Returns (nil, nil, nil) if no engine is registered for the session.
//
// finish, when not nil, is given the snapshot before it is installed and
// encoded, so the caller can stamp it with what only the handler layer knows
// (the target host, the reason the run ended) and have that land in both the
// loaded run and the saved file.
func (s *chaosEngineStore) completeEngine(sessionID, runID string) (*chaos.SavedRun, []byte, error) {
	return s.completeEngineWith(sessionID, runID, nil)
}

// completeEngineWith is completeEngine with the finish hook described above.
func (s *chaosEngineStore) completeEngineWith(sessionID, runID string, finish func(*chaos.SavedRun)) (*chaos.SavedRun, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	eng, ok := s.engines[sessionID]
	if !ok || eng == nil {
		return nil, nil, nil
	}
	snapshot := eng.Snapshot(runID)
	if finish != nil {
		finish(snapshot)
	}
	s.loadedRuns[sessionID] = snapshot
	delete(s.engines, sessionID)
	encoded, err := chaos.EncodeRun(snapshot)
	return snapshot, encoded, err
}

func (s *chaosEngineStore) getScreenHints(sessionID string) map[string]chaos.ScreenHint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneChaosScreenHintsMap(s.screenHints[sessionID])
}

func (s *chaosEngineStore) upsertScreenHint(sessionID, screenHash string, hint chaos.ScreenHint) map[string]chaos.ScreenHint {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.TrimSpace(screenHash)
	if key == "" {
		return cloneChaosScreenHintsMap(s.screenHints[sessionID])
	}
	if s.screenHints[sessionID] == nil {
		s.screenHints[sessionID] = make(map[string]chaos.ScreenHint)
	}
	clean := sanitizeChaosScreenHint(hint)
	if len(clean.KnownData) == 0 && len(clean.KnownKeys) == 0 && len(clean.BlockedKeys) == 0 && len(clean.KeyAssignments) == 0 {
		delete(s.screenHints[sessionID], key)
	} else {
		s.screenHints[sessionID][key] = clean
	}
	if len(s.screenHints[sessionID]) == 0 {
		delete(s.screenHints, sessionID)
		return nil
	}
	return cloneChaosScreenHintsMap(s.screenHints[sessionID])
}

// getLoadedRun returns a clone of the session's loaded run, made under the
// store lock. Handing back the shared pointer instead (as this used to)
// let callers range over/marshal MindMap.Areas/BusinessFunctions outside
// any lock, racing a concurrent business-annotation write to the same
// maps — CloneRun's cost (one MindMap deep-copy) is proven cheap enough
// elsewhere in this package (Engine.Snapshot/MindMapSnapshot do the same
// clone on every call).
func (s *chaosEngineStore) getLoadedRun(sessionID string) (*chaos.SavedRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.loadedRuns[sessionID]
	if !ok {
		return nil, false
	}
	return r.CloneRun(), true
}

func (s *chaosEngineStore) setLoadedRun(sessionID string, run *chaos.SavedRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadedRuns[sessionID] = run
}

func (s *chaosEngineStore) deleteLoadedRun(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.loadedRuns, sessionID)
}

func (s *chaosEngineStore) markRemoved(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removed[sessionID] = true
}

func (s *chaosEngineStore) isRemoved(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removed[sessionID]
}

func (s *chaosEngineStore) clearRemoved(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.removed, sessionID)
}

// chaosStartRequest is the JSON body accepted by POST /chaos/start.
// Both snake_case (the public MCP/HTTP tool descriptor form) and camelCase
// (the existing in-browser UI form) are accepted via a custom UnmarshalJSON.
type chaosStartRequest struct {
	MaxSteps                    int               `json:"max_steps"`
	TimeBudgetSec               float64           `json:"time_budget_sec"`
	StepDelaySec                float64           `json:"step_delay_sec"`
	Seed                        int64             `json:"seed"`
	AIDKeyWeights               map[string]int    `json:"aid_key_weights"`
	KeyBlacklist                []string          `json:"key_blacklist"`
	OutputFile                  string            `json:"output_file"`
	MaxFieldLength              int               `json:"max_field_length"`
	ScreenDedupSimilarity       float64           `json:"screen_dedup_similarity"`
	DedupMode                   string            `json:"dedup_mode"` // "structural" (default) or "exact"
	SaturationSteps             int               `json:"saturation_steps"`
	AutoBlockExitKeys           *bool             `json:"auto_block_exit_keys"`
	AutoPreferNavigationKeys    *bool             `json:"auto_prefer_navigation_keys"`
	LearnedInputReuseBias       *float64          `json:"learned_input_reuse_bias"`
	LearnedKeyReuseBias         *float64          `json:"learned_key_reuse_bias"`
	LearnedReuseBias            *float64          `json:"learned_reuse_bias"` // legacy alias: applies to both if specific values not provided
	ForceOverrideExistingInputs *bool             `json:"force_override_existing_inputs"`
	Hints                       []chaos.Hint      `json:"hints"`
	FirstScreenHint             *chaos.ScreenHint `json:"first_screen_hint,omitempty"`
	ExcludeNoProgressEvents     *bool             `json:"exclude_no_progress_events"`
	TransitionLogPath           string            `json:"transition_log_path"`
	ExtendLimits                bool              `json:"extend_limits"`
}

func (r *chaosStartRequest) UnmarshalJSON(data []byte) error {
	var snake struct {
		MaxSteps                    int               `json:"max_steps"`
		TimeBudgetSec               float64           `json:"time_budget_sec"`
		StepDelaySec                float64           `json:"step_delay_sec"`
		Seed                        int64             `json:"seed"`
		AIDKeyWeights               map[string]int    `json:"aid_key_weights"`
		KeyBlacklist                []string          `json:"key_blacklist"`
		OutputFile                  string            `json:"output_file"`
		MaxFieldLength              int               `json:"max_field_length"`
		ScreenDedupSimilarity       float64           `json:"screen_dedup_similarity"`
		DedupMode                   string            `json:"dedup_mode"`
		SaturationSteps             int               `json:"saturation_steps"`
		AutoBlockExitKeys           *bool             `json:"auto_block_exit_keys"`
		AutoPreferNavigationKeys    *bool             `json:"auto_prefer_navigation_keys"`
		LearnedInputReuseBias       *float64          `json:"learned_input_reuse_bias"`
		LearnedKeyReuseBias         *float64          `json:"learned_key_reuse_bias"`
		LearnedReuseBias            *float64          `json:"learned_reuse_bias"`
		ForceOverrideExistingInputs *bool             `json:"force_override_existing_inputs"`
		Hints                       []chaos.Hint      `json:"hints"`
		FirstScreenHint             *chaos.ScreenHint `json:"first_screen_hint,omitempty"`
		ExcludeNoProgressEvents     *bool             `json:"exclude_no_progress_events"`
		TransitionLogPath           string            `json:"transition_log_path"`
		ExtendLimits                bool              `json:"extend_limits"`
	}
	if err := json.Unmarshal(data, &snake); err != nil {
		return err
	}
	var camel struct {
		MaxSteps                    int               `json:"maxSteps"`
		TimeBudgetSec               float64           `json:"timeBudgetSec"`
		StepDelaySec                float64           `json:"stepDelaySec"`
		AIDKeyWeights               map[string]int    `json:"aidKeyWeights"`
		KeyBlacklist                []string          `json:"keyBlacklist"`
		OutputFile                  string            `json:"outputFile"`
		MaxFieldLength              int               `json:"maxFieldLength"`
		ScreenDedupSimilarity       float64           `json:"screenDedupSimilarity"`
		DedupMode                   string            `json:"dedupMode"`
		SaturationSteps             int               `json:"saturationSteps"`
		AutoBlockExitKeys           *bool             `json:"autoBlockExitKeys"`
		AutoPreferNavigationKeys    *bool             `json:"autoPreferNavigationKeys"`
		LearnedInputReuseBias       *float64          `json:"learnedInputReuseBias"`
		LearnedKeyReuseBias         *float64          `json:"learnedKeyReuseBias"`
		LearnedReuseBias            *float64          `json:"learnedReuseBias"`
		ForceOverrideExistingInputs *bool             `json:"forceOverrideExistingInputs"`
		FirstScreenHint             *chaos.ScreenHint `json:"firstScreenHint,omitempty"`
		ExcludeNoProgressEvents     *bool             `json:"excludeNoProgressEvents"`
		TransitionLogPath           string            `json:"transitionLogPath"`
		ExtendLimits                bool              `json:"extendLimits"`
	}
	// The same document is read twice, once per spelling. A wrongly typed
	// camelCase option is as much an error as a wrongly typed snake_case
	// one; ignoring it here is how {"maxSteps":"50"} used to start a run
	// with the default limit.
	if err := json.Unmarshal(data, &camel); err != nil {
		return err
	}

	r.MaxSteps = firstNonZeroInt(snake.MaxSteps, camel.MaxSteps)
	r.TimeBudgetSec = firstNonZeroFloat(snake.TimeBudgetSec, camel.TimeBudgetSec)
	r.StepDelaySec = firstNonZeroFloat(snake.StepDelaySec, camel.StepDelaySec)
	r.Seed = snake.Seed
	if len(snake.AIDKeyWeights) > 0 {
		r.AIDKeyWeights = snake.AIDKeyWeights
	} else {
		r.AIDKeyWeights = camel.AIDKeyWeights
	}
	r.KeyBlacklist = firstNonNilStrings(snake.KeyBlacklist, camel.KeyBlacklist)
	r.OutputFile = firstNonEmptyString(snake.OutputFile, camel.OutputFile)
	r.MaxFieldLength = firstNonZeroInt(snake.MaxFieldLength, camel.MaxFieldLength)
	r.ScreenDedupSimilarity = firstNonZeroFloat(snake.ScreenDedupSimilarity, camel.ScreenDedupSimilarity)
	r.DedupMode = firstNonEmptyString(snake.DedupMode, camel.DedupMode)
	r.SaturationSteps = firstNonZeroInt(snake.SaturationSteps, camel.SaturationSteps)
	r.AutoBlockExitKeys = firstNonNilBool(snake.AutoBlockExitKeys, camel.AutoBlockExitKeys)
	r.AutoPreferNavigationKeys = firstNonNilBool(snake.AutoPreferNavigationKeys, camel.AutoPreferNavigationKeys)
	r.LearnedInputReuseBias = firstNonNilFloat(snake.LearnedInputReuseBias, camel.LearnedInputReuseBias)
	r.LearnedKeyReuseBias = firstNonNilFloat(snake.LearnedKeyReuseBias, camel.LearnedKeyReuseBias)
	r.LearnedReuseBias = firstNonNilFloat(snake.LearnedReuseBias, camel.LearnedReuseBias)
	r.ForceOverrideExistingInputs = firstNonNilBool(snake.ForceOverrideExistingInputs, camel.ForceOverrideExistingInputs)
	r.Hints = snake.Hints
	if snake.FirstScreenHint != nil {
		r.FirstScreenHint = snake.FirstScreenHint
	} else {
		r.FirstScreenHint = camel.FirstScreenHint
	}
	r.ExcludeNoProgressEvents = firstNonNilBool(snake.ExcludeNoProgressEvents, camel.ExcludeNoProgressEvents)
	r.TransitionLogPath = firstNonEmptyString(snake.TransitionLogPath, camel.TransitionLogPath)
	r.ExtendLimits = snake.ExtendLimits || camel.ExtendLimits
	return nil
}

func firstNonZeroInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func firstNonZeroFloat(a, b float64) float64 {
	if a != 0 {
		return a
	}
	return b
}

func firstNonNilFloat(a, b *float64) *float64 {
	if a != nil {
		return a
	}
	return b
}

func firstNonNilBool(a, b *bool) *bool {
	if a != nil {
		return a
	}
	return b
}

// applyChaosStartRequestToConfig merges JSON-bound request overrides into cfg.
// Used by both ChaosStartHandler and ChaosResumeHandler.
func applyChaosStartRequestToConfig(cfg *chaos.Config, req chaosStartRequest) {
	if req.MaxSteps > 0 {
		cfg.MaxSteps = req.MaxSteps
	}
	if req.TimeBudgetSec > 0 {
		cfg.TimeBudget = time.Duration(req.TimeBudgetSec * float64(time.Second))
	}
	if req.StepDelaySec > 0 {
		cfg.StepDelay = time.Duration(req.StepDelaySec * float64(time.Second))
	}
	if req.Seed != 0 {
		cfg.Seed = req.Seed
	}
	if len(req.AIDKeyWeights) > 0 {
		cfg.AIDKeyWeights = req.AIDKeyWeights
	}
	if len(req.KeyBlacklist) > 0 {
		cfg.KeyBlacklist = append([]string(nil), req.KeyBlacklist...)
	}
	if req.OutputFile != "" {
		cfg.OutputFile = req.OutputFile
	}
	if req.MaxFieldLength > 0 {
		cfg.MaxFieldLength = req.MaxFieldLength
	}
	if req.ScreenDedupSimilarity > 0 && req.ScreenDedupSimilarity <= 1 {
		cfg.ScreenDedupSimilarity = req.ScreenDedupSimilarity
	}
	switch strings.ToLower(strings.TrimSpace(req.DedupMode)) {
	case chaos.DedupModeStructural:
		cfg.DedupMode = chaos.DedupModeStructural
	case chaos.DedupModeExact:
		cfg.DedupMode = chaos.DedupModeExact
	}
	if req.SaturationSteps > 0 {
		cfg.SaturationSteps = req.SaturationSteps
	}
	if req.AutoBlockExitKeys != nil {
		cfg.AutoBlockExitKeys = *req.AutoBlockExitKeys
	}
	if req.AutoPreferNavigationKeys != nil {
		cfg.AutoPreferNavigationKeys = *req.AutoPreferNavigationKeys
	}
	if req.LearnedInputReuseBias != nil && *req.LearnedInputReuseBias >= 0 && *req.LearnedInputReuseBias <= 1 {
		cfg.LearnedInputReuseBias = *req.LearnedInputReuseBias
	} else if req.LearnedReuseBias != nil && *req.LearnedReuseBias >= 0 && *req.LearnedReuseBias <= 1 {
		cfg.LearnedInputReuseBias = *req.LearnedReuseBias
	}
	if req.LearnedKeyReuseBias != nil && *req.LearnedKeyReuseBias >= 0 && *req.LearnedKeyReuseBias <= 1 {
		cfg.LearnedKeyReuseBias = *req.LearnedKeyReuseBias
	} else if req.LearnedReuseBias != nil && *req.LearnedReuseBias >= 0 && *req.LearnedReuseBias <= 1 {
		cfg.LearnedKeyReuseBias = *req.LearnedReuseBias
	}
	if req.ForceOverrideExistingInputs != nil {
		cfg.ForceOverrideExistingInputs = *req.ForceOverrideExistingInputs
	}
	if len(req.Hints) > 0 {
		cfg.Hints = sanitizeChaosHints(req.Hints)
	}
	if req.ExcludeNoProgressEvents != nil {
		cfg.ExcludeNoProgressEvents = *req.ExcludeNoProgressEvents
	}
	if req.TransitionLogPath != "" {
		// Confined to a file name inside the runs directory by
		// confineChaosPaths, exactly like OutputFile.
		cfg.TransitionLogPath = req.TransitionLogPath
	}
}

// bindChaosStartRequest reads the optional JSON body of /chaos/start and
// /chaos/resume. An empty body means "use the defaults" and is not an error;
// a body that is present but does not parse is.
//
// It used to be `if err := c.ShouldBindJSON(&req); err == nil { apply }`,
// which treated the two the same. A caller that sent
// {"max_steps":"50","key_blacklist":["PF3"]} — a number as a string, the kind
// of thing a hand-written script or a model produces — got a 200 and a run
// with the default limits and nothing blocked, typing into a live host with
// the one key they had asked it never to press. A request that cannot be
// understood must not be answered by doing something else.
func bindChaosStartRequest(c *gin.Context, req *chaosStartRequest) error {
	if c.Request == nil || c.Request.Body == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxChaosRequestBodyBytes+1))
	if err != nil {
		return fmt.Errorf("could not read the request body: %w", err)
	}
	if len(body) > maxChaosRequestBodyBytes {
		return fmt.Errorf("request body is larger than %d bytes", maxChaosRequestBodyBytes)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, req); err != nil {
		return describeChaosBodyError(err)
	}
	return nil
}

// maxChaosRequestBodyBytes bounds a start/resume body. Hints are the only
// part of it that can be long, and a megabyte of hints is already absurd.
const maxChaosRequestBodyBytes = 1 << 20

// describeChaosBodyError turns a JSON decoding error into something a person
// can act on: which option was wrong and what it should have been.
func describeChaosBodyError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return fmt.Errorf("option %q must be a %s, not a %s", typeErr.Field, chaosJSONKindName(typeErr.Type), typeErr.Value)
	}
	return fmt.Errorf("body is not valid JSON: %v", err)
}

func chaosJSONKindName(t reflect.Type) string {
	if t == nil {
		return "different type"
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean (true or false)"
	case reflect.String:
		return "string"
	case reflect.Slice, reflect.Array:
		return "list"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return t.String()
	}
}

// rejectChaosBody answers a start/resume request whose body could not be
// understood. Nothing has been started when this is called.
func rejectChaosBody(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error": "invalid chaos options, nothing was started: " + err.Error(),
		"code":  "chaos_invalid_options",
	})
}

// ChaosStartHandler handles POST /chaos/start.
func (app *App) ChaosStartHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	// Parse the optional body before touching any state: an empty body means
	// the defaults, a body that does not parse starts nothing.
	cfg := chaos.DefaultConfig()
	app.applyChaosEnvSettings(&cfg)
	var req chaosStartRequest
	if err := bindChaosStartRequest(c, &req); err != nil {
		rejectChaosBody(c, err)
		return
	}
	applyChaosStartRequestToConfig(&cfg, req)
	app.chaosEngines.clearRemoved(s.ID)
	var savedHints chaosHintsPayload
	if saved, err := app.loadChaosHintsPayload(s.OwnerID); err == nil {
		savedHints = saved
		if len(cfg.Hints) == 0 && len(saved.Hints) > 0 {
			cfg.Hints = saved.Hints
		}
		if len(cfg.KeyBlacklist) == 0 && len(saved.KeyBlacklist) > 0 {
			cfg.KeyBlacklist = append([]string(nil), saved.KeyBlacklist...)
		}
	}
	cfg.ScreenHints = app.resolveChaosScreenHints(s.ID, req, savedHints)
	app.confineChaosPaths(&cfg, loadedWorkflowName(s), s.OwnerID)
	withSessionLock(s, func() {
		cfg.ExportHost = s.TargetHost
		cfg.ExportPort = s.TargetPort
	})
	runConfig := chaosRunConfigSummary(cfg, false)

	var h interface{ IsConnected() bool }
	withSessionLock(s, func() { h = s.Host })
	if h == nil || !h.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not connected to host"})
		return
	}

	eng, err, started := app.chaosEngines.startIfAbsent(s.ID, func() (*chaos.Engine, error) {
		var built *chaos.Engine
		withSessionLock(s, func() {
			built = chaos.New(s.Host, cfg)
		})
		return built, nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start: %v", err)})
		return
	}
	if !started {
		c.JSON(http.StatusConflict, gin.H{"error": "chaos exploration is already running"})
		return
	}

	// Kick off a background goroutine that syncs Status back to the session.
	app.startChaosSync(s, eng, runConfig)

	c.JSON(http.StatusOK, gin.H{"status": "started"})
}

// ChaosStopHandler handles POST /chaos/stop.
func (app *App) ChaosStopHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	eng, ok := app.chaosEngines.get(s.ID)
	if !ok || !eng.Active() {
		c.JSON(http.StatusOK, gin.H{"status": "not running"})
		return
	}

	eng.Stop()
	c.JSON(http.StatusOK, gin.H{"status": "stopping"})
}

// ChaosRemoveHandler handles POST /chaos/remove – clears completed/loaded chaos
// run state from the current session.
func (app *App) ChaosRemoveHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	if eng, ok := app.chaosEngines.get(s.ID); ok && eng.Active() {
		c.JSON(http.StatusConflict, gin.H{"error": "chaos exploration is running; stop it before removing"})
		return
	}

	// markRemoved alone would end the status goroutine only on its next wake,
	// up to a poll interval later — and it is the write it does on the way
	// out that a caller removing a run does not want landing afterwards.
	app.chaosEngines.stopSync(s.ID)
	app.chaosEngines.markRemoved(s.ID)
	app.chaosEngines.delete(s.ID)
	app.chaosEngines.deleteLoadedRun(s.ID)
	withSessionLock(s, func() {
		s.Chaos = nil
	})

	c.JSON(http.StatusOK, gin.H{"status": "removed"})
}

// ChaosStatusHandler handles GET /chaos/status.
func (app *App) ChaosStatusHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if app.chaosEngines.isRemoved(s.ID) {
		resp := gin.H{
			"active":        false,
			"stepsRun":      0,
			"transitions":   0,
			"uniqueScreens": 0,
			"uniqueInputs":  0,
			"screenHints":   app.chaosEngines.getScreenHints(s.ID),
		}
		finishChaosStatusTarget(resp, sessionChaosTargetHost(s))
		c.JSON(http.StatusOK, resp)
		return
	}

	verbose := parseBoolFormValue(c.DefaultQuery("verbose", ""))
	sessionTarget := sessionChaosTargetHost(s)

	eng, ok := app.chaosEngines.get(s.ID)
	if !ok {
		// No engine: the run is over (or was loaded, or never happened).
		// Everything the engine used to answer is answered from what was
		// kept — the session's last snapshot and the finished/loaded run.
		snapshot := chaosStateSnapshot(s)
		resp := chaosStateToJSON(snapshot)
		if snapshot == nil {
			resp = gin.H{
				"active":        false,
				"stepsRun":      0,
				"transitions":   0,
				"uniqueScreens": 0,
				"uniqueInputs":  0,
			}
		}
		mindMapSet := false
		// Read the loaded run under the store lock rather than cloning it:
		// this is a polled endpoint, and the default (non-verbose) answer
		// needs four fields per screen, not a deep copy of the whole map.
		_, _ = app.chaosEngines.withLoadedRun(s.ID, func(loaded *chaos.SavedRun) error {
			resp["loadedRunID"] = loaded.ID
			raiseStatusCounter(resp, "stepsRun", loaded.StepsRun)
			raiseStatusCounter(resp, "transitions", loaded.Transitions)
			raiseStatusCounter(resp, "uniqueScreens", loaded.UniqueScreens)
			raiseStatusCounter(resp, "uniqueInputs", loaded.UniqueInputs)
			// Prefer the finalized saved-run mind map over any stale session snapshot.
			if loaded.MindMap != nil {
				if verbose {
					resp["mindMap"] = redactedMindMap(loaded.MindMap)
				} else {
					resp["mindMap"] = slimMindMapForStatus(loaded.MindMap)
				}
				mindMapSet = true
			}
			if _, has := resp["firstScreenHash"]; !has {
				if firstHash := strings.TrimSpace(loaded.FirstScreenHash); firstHash != "" {
					resp["firstScreenHash"] = firstHash
				} else if firstHash := chaosFirstScreenHashFromAttempts(loaded.Attempts); firstHash != "" {
					resp["firstScreenHash"] = firstHash
				}
			}
			if _, has := resp["terminationReason"]; !has && loaded.TerminationReason != "" {
				resp["terminationReason"] = loaded.TerminationReason
			}
			if _, has := resp["targetHost"]; !has && loaded.TargetHost != "" {
				resp["targetHost"] = loaded.TargetHost
			}
			if loaded.Label != "" {
				resp["label"] = loaded.Label
			}
			if _, has := resp["error"]; !has && loaded.Error != "" {
				resp["error"] = loaded.Error
			}
			return nil
		})
		if !mindMapSet && snapshot != nil && len(snapshot.MindMap) > 0 {
			// Only reached when the finished run is no longer held (it was
			// deleted, say): fall back to the copy made when the run ended.
			if verbose {
				resp["mindMap"] = snapshot.MindMap
			} else {
				var kept chaos.MindMap
				if err := json.Unmarshal(snapshot.MindMap, &kept); err == nil {
					resp["mindMap"] = slimMindMapForStatus(&kept)
				}
			}
		}
		if reason, _ := resp["terminationReason"].(string); reason != "" {
			resp["terminationSummary"] = chaosTerminationSummary(reason, resp["saturatedNoProgress"] == true)
		}
		finishChaosStatusTarget(resp, sessionTarget)
		if hints := app.chaosEngines.getScreenHints(s.ID); len(hints) > 0 {
			resp["screenHints"] = hints
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	st := eng.Status()
	slim := gin.H(nil)
	if !verbose && st.MindMap != nil {
		// Replace the full mind map with a slim summary so /chaos/status
		// stays cheap when polled. Pass ?verbose=true to get the full graph.
		slim = slimMindMapForStatus(st.MindMap)
	}
	resp := chaosStatusToJSON(st)
	if !verbose {
		if slim != nil {
			resp["mindMap"] = slim
		}
		// Also drop RecentAttempts (only keep LastAttempt) when not verbose.
		delete(resp, "recentAttempts")
	}
	if cfg := chaosRunConfigOf(s); cfg != nil {
		resp["config"] = chaosRunConfigToJSON(cfg)
	}
	finishChaosStatusTarget(resp, sessionTarget)
	if hints := app.chaosEngines.getScreenHints(s.ID); len(hints) > 0 {
		resp["screenHints"] = hints
	}
	c.JSON(http.StatusOK, resp)
}

// raiseStatusCounter lifts a counter in a status response to the saved run's
// value when that is higher (the session snapshot can lag the final save).
func raiseStatusCounter(resp gin.H, key string, value int) {
	if value <= 0 {
		return
	}
	if cur, ok := resp[key].(int); !ok || value > cur {
		resp[key] = value
	}
}

// finishChaosStatusTarget makes sure a status response says which host it is
// about. targetHost is the host the run explored when a run is known, and
// otherwise the host the session is connected to (what a run started now
// would explore). sessionTargetHost is always the latter, so a client showing
// a loaded run can tell when it came from somewhere else.
func finishChaosStatusTarget(resp gin.H, sessionTarget string) {
	if sessionTarget != "" {
		resp["sessionTargetHost"] = sessionTarget
	}
	if got, _ := resp["targetHost"].(string); got == "" && sessionTarget != "" {
		resp["targetHost"] = sessionTarget
	}
}

// sessionChaosTargetHost names the host a session is connected to, as
// "host:port" (or just the host when the port is unknown).
func sessionChaosTargetHost(s *session.Session) string {
	if s == nil {
		return ""
	}
	var hostName string
	var port int
	withSessionLock(s, func() {
		hostName = strings.TrimSpace(s.TargetHost)
		port = s.TargetPort
	})
	if hostName == "" {
		return ""
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", hostName, port)
	}
	return hostName
}

// chaosRunConfigOf returns a copy of the configuration summary of the
// session's current or last run, or nil when none is known.
func chaosRunConfigOf(s *session.Session) *session.ChaosRunConfig {
	if s == nil {
		return nil
	}
	var cfg *session.ChaosRunConfig
	withSessionLock(s, func() {
		if s.Chaos != nil {
			cfg = s.Chaos.Config.Clone()
		}
	})
	return cfg
}

// chaosRunConfigSummary reduces an engine configuration to what is worth
// keeping after the engine has gone: the limits, and which keys the run could
// and could not press. Hint values are not kept, only how many there were.
func chaosRunConfigSummary(cfg chaos.Config, continued bool) *session.ChaosRunConfig {
	blocked := normalizedChaosKeys(cfg.KeyBlacklist)
	return &session.ChaosRunConfig{
		MaxSteps:          cfg.MaxSteps,
		TimeBudget:        cfg.TimeBudget,
		StepDelay:         cfg.StepDelay,
		Seed:              cfg.Seed,
		SaturationSteps:   cfg.SaturationSteps,
		DedupMode:         cfg.DedupMode,
		CandidateKeys:     chaosKeysWithout(chaos.CandidateKeys(cfg.AIDKeyWeights), blocked),
		BlockedKeys:       blocked,
		AutoBlockExitKeys: cfg.AutoBlockExitKeys,
		HintCount:         len(cfg.Hints),
		ScreenHintCount:   len(cfg.ScreenHints),
		Continued:         continued,
	}
}

// normalizedChaosKeys returns keys in the engine's own spelling ("PF3"
// becomes "PF(3)"), sorted and without duplicates.
func normalizedChaosKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	weights := make(map[string]int, len(keys))
	for _, k := range keys {
		if strings.TrimSpace(k) != "" {
			weights[k] = 1
		}
	}
	if len(weights) == 0 {
		return nil
	}
	return chaos.CandidateKeys(weights)
}

// chaosKeysWithout returns keys minus blocked, preserving order.
func chaosKeysWithout(keys, blocked []string) []string {
	if len(blocked) == 0 {
		return keys
	}
	drop := make(map[string]struct{}, len(blocked))
	for _, k := range blocked {
		drop[k] = struct{}{}
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, isBlocked := drop[k]; !isBlocked {
			out = append(out, k)
		}
	}
	return out
}

func chaosRunConfigToJSON(cfg *session.ChaosRunConfig) gin.H {
	if cfg == nil {
		return nil
	}
	return gin.H{
		"maxSteps":          cfg.MaxSteps,
		"timeBudgetSec":     cfg.TimeBudget.Seconds(),
		"stepDelaySec":      cfg.StepDelay.Seconds(),
		"seed":              cfg.Seed,
		"saturationSteps":   cfg.SaturationSteps,
		"dedupMode":         cfg.DedupMode,
		"candidateKeys":     append([]string{}, cfg.CandidateKeys...),
		"blockedKeys":       append([]string{}, cfg.BlockedKeys...),
		"autoBlockExitKeys": cfg.AutoBlockExitKeys,
		"hintCount":         cfg.HintCount,
		"screenHintCount":   cfg.ScreenHintCount,
		"continued":         cfg.Continued,
	}
}

// chaosTerminationSummary says in plain English why a run ended. The reason
// codes are the engine's; an unknown one is passed through rather than
// guessed at.
func chaosTerminationSummary(reason string, saturatedNoProgress bool) string {
	switch strings.TrimSpace(reason) {
	case "":
		return ""
	case chaos.TerminationReasonMaxSteps:
		return "It reached its step limit. There may be more to find: continue exploring to give it more steps."
	case chaos.TerminationReasonTimeBudget:
		return "It ran out of time. There may be more to find: continue exploring to give it more time."
	case chaos.TerminationReasonSaturated:
		if saturatedNoProgress {
			return "It stopped because nothing it tried moved the application off the first screen. Continuing as it is will end the same way: give it a value or key that works on that screen (a hint, or a recording) first."
		}
		return "It stopped because it had stopped finding anything new: many steps in a row led only to screens and transitions it had already seen."
	case chaos.TerminationReasonStopped:
		return "It was stopped by hand before it finished."
	case chaos.TerminationReasonBlocked:
		return "It stopped because every key it could press on the current screen is blocked. Unblock a key for that screen, or start from a different one."
	case chaos.TerminationReasonError:
		return "It stopped because of an error talking to the host."
	default:
		return fmt.Sprintf("It ended with reason %q.", reason)
	}
}

// slimMindMapForStatus returns a compact mind-map summary suitable for the
// default chaos_status payload: hash + label + visit counts only, no
// preview text or per-field discovery details. Verbose callers
// (?verbose=true) bypass this and get the full structure.
func slimMindMapForStatus(m *chaos.MindMap) gin.H {
	if m == nil {
		return nil
	}
	areas := make([]gin.H, 0, len(m.Areas))
	for hash, area := range m.Areas {
		if area == nil {
			continue
		}
		entry := gin.H{
			"hash":            hash,
			"label":           area.Label,
			"visits":          area.Visits,
			"inputFieldCount": area.InputFieldCount,
		}
		if area.BusinessPurpose != "" {
			entry["businessPurpose"] = area.BusinessPurpose
		}
		areas = append(areas, entry)
	}
	return gin.H{
		"areaCount": len(areas),
		"areas":     areas,
	}
}

// ChaosExportHandler handles POST /chaos/export – returns the learned workflow.
func (app *App) ChaosExportHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if app.chaosEngines.isRemoved(s.ID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos run data for this session"})
		return
	}

	var targetHost string
	var targetPort int
	withSessionLock(s, func() {
		targetHost = s.TargetHost
		targetPort = s.TargetPort
	})

	var data []byte
	var err error
	exportSuccessBalance := app.chaosExportSuccessBalanceSetting()
	if eng, ok := app.chaosEngines.get(s.ID); ok {
		data, err = eng.ExportWorkflowWithSuccessBalance(targetHost, targetPort, exportSuccessBalance)
	} else if run, ok := app.sessionChaosRun(s); ok {
		data, err = chaos.ExportWorkflowFromSavedRun(run, targetHost, targetPort, exportSuccessBalance)
	} else {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos run data for this session"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Validate the exported JSON is well-formed.
	var v interface{}
	if jsonErr := json.Unmarshal(data, &v); jsonErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "export produced invalid JSON"})
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// ChaosMindMapExportHandler handles GET /chaos/mindmap/export – returns the
// session's mind map as JSON so it can be compared with another run or
// imported into a later session.
//
// The map comes from wherever the session's run currently lives: the engine
// while a run is active, and the finished or loaded run afterwards. It used
// to insist on the engine, which is discarded within half a second of the run
// ending — so the one moment anybody wants to export a map, after the run,
// was the moment it answered 404.
//
// Values learned for hidden fields are left out of the document; see
// chaos_redact.go.
func (app *App) ChaosMindMapExportHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var mm *chaos.MindMap
	if !app.chaosEngines.isRemoved(s.ID) {
		mm = app.sessionChaosMindMap(s)
	}
	if mm == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "there is no chaos run in this session to export a map from; run an exploration or load a saved run first",
			"code":  "chaos_no_run",
		})
		return
	}
	// sessionChaosMindMap hands back a private copy, so it can be scrubbed
	// in place.
	scrubMindMapSecrets(mm, "")
	data, err := json.MarshalIndent(mm, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="chaos-mindmap.json"`)
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// ChaosMindMapImportHandler handles POST /chaos/mindmap/import – merges a
// previously exported mind map into the session's run, so that the next
// "continue exploring" starts from what an earlier session already learned.
//
// Where the map goes depends on what the session has:
//
//   - an engine that is not running (the brief window after a run ends):
//     merged into the engine;
//   - a finished or loaded run: merged into that run;
//   - nothing at all: the imported map becomes a new loaded run.
//
// It is refused only while a run is active, because the run loop is writing
// the same map. Before this it required an engine and refused an active one,
// and since an engine only exists while a run is active (give or take half a
// second) there was no state in which it could succeed.
func (app *App) ChaosMindMapImportHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var mm chaos.MindMap
	if err := c.ShouldBindJSON(&mm); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the body is not a mind map exported by GET /chaos/mindmap/export: " + err.Error()})
		return
	}
	dropMaskedMindMapValues(&mm)
	imported := 0
	for hash, area := range mm.Areas {
		if area != nil && strings.TrimSpace(hash) != "" {
			imported++
		}
	}

	const activeMsg = "a chaos run is active; stop it (or let it finish) before importing a map"
	target := ""
	runID := ""
	total := 0
	active := false
	found, _ := app.chaosEngines.withEngine(s.ID, func(eng *chaos.Engine) error {
		if !eng.ImportMindMap(&mm) {
			active = true
			return nil
		}
		target = "engine"
		if merged := eng.MindMapSnapshot(); merged != nil {
			total = len(merged.Areas)
		}
		return nil
	})
	if active {
		c.JSON(http.StatusConflict, gin.H{"error": activeMsg, "code": "chaos_run_active"})
		return
	}
	if !found {
		// Make sure a run saved by this session but no longer held in memory
		// is the thing merged into, rather than replaced by a new one.
		_, _ = app.sessionChaosRun(s)
		runID, total, target = app.chaosEngines.importMindMapIntoLoadedRun(s.ID, &mm)
		if target == "" {
			c.JSON(http.StatusConflict, gin.H{"error": activeMsg, "code": "chaos_run_active"})
			return
		}
		app.chaosEngines.clearRemoved(s.ID)
	}

	resp := gin.H{
		"ok":            true,
		"areasImported": imported,
		"areasTotal":    total,
		"target":        target,
	}
	if runID != "" {
		resp["runID"] = runID
	}
	c.JSON(http.StatusOK, resp)
}

// importMindMapIntoLoadedRun merges imported into the session's loaded run,
// creating one to hold it when the session has none. It returns the run's ID,
// the number of areas the run's map now has, and which of the two happened
// ("loadedRun" or "newRun"); target is empty when an engine turned up in the
// meantime, which the caller reports as a conflict.
func (s *chaosEngineStore) importMindMapIntoLoadedRun(sessionID string, imported *chaos.MindMap) (runID string, total int, target string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if eng, ok := s.engines[sessionID]; ok && eng != nil {
		return "", 0, ""
	}
	run, ok := s.loadedRuns[sessionID]
	target = "loadedRun"
	if !ok || run == nil {
		run = &chaos.SavedRun{
			SavedRunMeta: chaos.SavedRunMeta{
				ID:        "imported-map-" + chaos.NewRunID(),
				StartedAt: time.Now(),
			},
			ScreenHashes:      map[string]bool{},
			TransitionList:    []chaos.Transition{},
			Steps:             []session.WorkflowStep{},
			AIDKeyCounts:      map[string]int{},
			UniqueInputValues: map[string]bool{},
			Attempts:          []chaos.Attempt{},
		}
		s.loadedRuns[sessionID] = run
		target = "newRun"
	}
	run.MindMap = mergeChaosMindMaps(run.MindMap, imported)
	if run.MindMap != nil {
		total = len(run.MindMap.Areas)
	}
	delete(s.removed, sessionID)
	return run.ID, total, target
}

// mergeChaosMindMaps returns base with imported merged into it, by the
// engine's own rules (counters and learned values add up; where both sides
// describe a screen, base's description wins). Neither argument is modified.
//
// The merge itself belongs to the engine and is only reachable through one,
// so this borrows a throwaway engine that is never started.
func mergeChaosMindMaps(base, imported *chaos.MindMap) *chaos.MindMap {
	scratch := chaos.New(nil, chaos.DefaultConfig())
	if base != nil {
		scratch.ImportMindMap(cloneChaosMindMap(base))
	}
	if imported != nil {
		scratch.ImportMindMap(cloneChaosMindMap(imported))
	}
	return scratch.MindMapSnapshot()
}

// overriding only fields whose corresponding setting is non-empty. Request body
// overrides applied later in ChaosStartHandler will always take final precedence.
func (app *App) applyChaosEnvSettings(cfg *chaos.Config) {
	if app == nil || cfg == nil {
		return
	}
	settings, _, _, err := app.settingsSnapshot()
	if err != nil {
		return
	}

	if v := strings.TrimSpace(settings["CHAOS_MAX_STEPS"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxSteps = n
		}
	}
	if v := strings.TrimSpace(settings["CHAOS_TIME_BUDGET_SEC"]); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			cfg.TimeBudget = time.Duration(f * float64(time.Second))
		}
	}
	if v := strings.TrimSpace(settings["CHAOS_STEP_DELAY_SEC"]); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			cfg.StepDelay = time.Duration(f * float64(time.Second))
		}
	}
	if v := strings.TrimSpace(settings["CHAOS_SEED"]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Seed = n
		}
	}
	if v := strings.TrimSpace(settings["CHAOS_MAX_FIELD_LENGTH"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxFieldLength = n
		}
	}
	if v := strings.TrimSpace(settings["CHAOS_SCREEN_DEDUP_SIMILARITY"]); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			cfg.ScreenDedupSimilarity = f
		}
	}
	// Apply learned reuse bias: specific keys take precedence over the legacy alias.
	learnedBias := strings.TrimSpace(settings["CHAOS_LEARNED_REUSE_BIAS"])
	if v := strings.TrimSpace(settings["CHAOS_LEARNED_INPUT_REUSE_BIAS"]); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			cfg.LearnedInputReuseBias = f
		}
	} else if learnedBias != "" {
		if f, err := strconv.ParseFloat(learnedBias, 64); err == nil && f >= 0 && f <= 1 {
			cfg.LearnedInputReuseBias = f
		}
	}
	if v := strings.TrimSpace(settings["CHAOS_LEARNED_KEY_REUSE_BIAS"]); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			cfg.LearnedKeyReuseBias = f
		}
	} else if learnedBias != "" {
		if f, err := strconv.ParseFloat(learnedBias, 64); err == nil && f >= 0 && f <= 1 {
			cfg.LearnedKeyReuseBias = f
		}
	}
	if v := strings.TrimSpace(settings["CHAOS_OUTPUT_FILE"]); v != "" {
		cfg.OutputFile = v
	}
	if v := strings.TrimSpace(settings["CHAOS_TRANSITION_LOG_PATH"]); v != "" {
		cfg.TransitionLogPath = v
	}
	if v := strings.TrimSpace(settings["CHAOS_FORCE_OVERRIDE_EXISTING_INPUTS"]); v != "" {
		cfg.ForceOverrideExistingInputs = strings.EqualFold(v, "true")
	}
	if v := strings.TrimSpace(settings["CHAOS_EXCLUDE_NO_PROGRESS_EVENTS"]); v != "" {
		cfg.ExcludeNoProgressEvents = strings.EqualFold(v, "true")
	}
}

func (app *App) chaosExportSuccessBalanceSetting() float64 {
	const defaultBalance = 1.0
	if app == nil {
		return defaultBalance
	}
	settings, _, _, err := app.settingsSnapshot()
	if err != nil {
		return defaultBalance
	}
	raw := strings.TrimSpace(settings["CHAOS_EXPORT_SUCCESS_BALANCE"])
	if raw == "" {
		return defaultBalance
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 || v > 1 {
		return defaultBalance
	}
	return v
}

func (app *App) loadSessionChaosRunFromDisk(s *session.Session) *chaos.SavedRun {
	if app.baseDir == "" || s == nil || app.chaosEngines.isRemoved(s.ID) {
		return nil
	}
	snapshot := chaosStateSnapshot(s)
	if snapshot == nil || strings.TrimSpace(snapshot.LoadedRunID) == "" {
		return nil
	}
	runID := strings.TrimSpace(snapshot.LoadedRunID)
	run, err := chaos.LoadRun(app.chaosRunsDirForSession(s.OwnerID), runID)
	if err != nil {
		return nil
	}
	return run
}

// chaosSyncInterval is how often the status goroutine copies engine state
// into the session.
const chaosSyncInterval = 500 * time.Millisecond

// startChaosSync launches syncChaosStatus and registers it, so that both a
// shutdown and a test can wait for it instead of leaving it to outlive
// whatever started it.
//
// Before this existed the goroutine was launched bare, with its only exit
// conditions being "the run finished" and "the session was marked removed" —
// and cleanupSession clears that mark on its way out. A session that ended
// mid-run therefore left a goroutine polling a detached engine forever, and
// when the run did eventually finish it wrote the saved-run file into a
// directory whose owner was long gone. In tests that owner is t.TempDir(),
// which is why this surfaced as "TempDir RemoveAll cleanup: directory not
// empty" on a loaded CI runner rather than as anything anyone could
// reproduce.
//
// runConfig is the summary of the configuration the run was started with; it
// is kept on the session so it outlives the engine. Nil is allowed.
func (app *App) startChaosSync(s *session.Session, eng *chaos.Engine, runConfig *session.ChaosRunConfig) {
	stop := app.chaosEngines.beginSync(s.ID)
	app.chaosSync.Add(1)
	go func() {
		defer app.chaosSync.Done()
		defer app.chaosEngines.endSync(s.ID, stop)
		app.syncChaosStatus(s, eng, stop, runConfig)
	}()
}

// waitForChaosSync blocks until every status goroutine has returned, or until
// timeout elapses. Reports whether they all finished.
//
// Bounded on purpose: this is called on the shutdown path and from test
// cleanup, and neither is a place to hang indefinitely because one goroutine
// is wedged. Callers that care tell the goroutines to stop first.
func (app *App) waitForChaosSync(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		app.chaosSync.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// syncChaosStatus runs in a background goroutine and copies engine status
// snapshots into the session's ChaosState so that the session store always
// reflects the latest values. It removes the engine from the store once the
// run completes to avoid memory growth.
//
// While the run is going it copies counters and the recent attempts only.
// The mind map is not copied: nothing reads the session's copy during a run
// (status asks the engine directly), and marshalling tens of kilobytes of map
// twice a second for the length of a run to keep an unread copy fresh was
// most of what this goroutine cost. The map is marshalled exactly once, when
// the run ends, which is the only moment the session's copy becomes the one
// that is read.
//
// What is left per tick is whatever Engine.Status itself does to produce a
// snapshot. That belongs to the engine; if Status grows a way to say whether
// the map has changed (a version counter), chaosStateFromStatus is the one
// place that needs to know.
//
// It returns when the run completes, when the session's engine is marked
// removed, or when stop is closed — whichever comes first. Launch it through
// startChaosSync rather than directly, so it is counted.
func (app *App) syncChaosStatus(s *session.Session, eng *chaos.Engine, stop <-chan struct{}, runConfig *session.ChaosRunConfig) {
	targetHost := sessionChaosTargetHost(s)
	for {
		select {
		case <-stop:
			return
		default:
		}
		if app.chaosEngines.isRemoved(s.ID) {
			return
		}
		st := eng.Status()
		if app.chaosEngines.isRemoved(s.ID) {
			return
		}
		state := chaosStateFromStatus(st, runConfig, targetHost)
		withSessionLock(s, func() {
			if app.chaosEngines.isRemoved(s.ID) {
				return
			}
			s.Chaos = state
		})
		if !st.Active {
			if app.chaosEngines.isRemoved(s.ID) {
				return
			}
			runID := chaos.NewRunID()
			// Snapshot, install as loaded run, unregister the engine, and
			// encode to JSON in one atomic store operation so concurrent
			// business writes are never lost between the snapshot and the
			// engine removal, and never race the encode itself (see
			// completeEngine's doc comment).
			//
			// The hook stamps the run with what the engine does not know or
			// may not have recorded — where it ran and why it ended — so the
			// saved file answers those questions long after this session has
			// gone. It also takes the one marshalled copy of the map, under
			// the same lock that guards the run it is read from.
			var finalMap json.RawMessage
			snapshot, encoded, encodeErr := app.chaosEngines.completeEngineWith(s.ID, runID, func(run *chaos.SavedRun) {
				stampChaosRun(run, st, targetHost)
				finalMap = marshalChaosMindMap(redactedMindMap(run.MindMap))
			})
			if snapshot == nil {
				return
			}
			// Auto-save the completed run (if configured) BEFORE exposing
			// LoadedRunID via session status. A caller that polls status
			// and acts on LoadedRunID immediately — including the
			// disk-fallback path used once a run has aged out of the
			// in-memory cache — must not be able to observe the ID before
			// the file it names actually exists on disk.
			if runsDir := app.chaosRunsDirForSession(s.OwnerID); runsDir != "" {
				if encodeErr != nil {
					log.Printf("chaos: auto-save run %s failed: %v", runID, encodeErr)
				} else if saveErr := chaos.WriteRunFile(runsDir, runID, encoded); saveErr != nil {
					// Non-fatal: log but do not interrupt teardown.
					log.Printf("chaos: auto-save run %s failed: %v", runID, saveErr)
				} else if pruned, pruneErr := chaos.PruneRuns(runsDir, maxSavedChaosRuns); pruneErr != nil {
					log.Printf("chaos: prune saved runs failed: %v", pruneErr)
				} else if pruned > 0 {
					log.Printf("chaos: pruned %d old saved run(s), keeping newest %d", pruned, maxSavedChaosRuns)
				}
			}
			withSessionLock(s, func() {
				if s.Chaos != nil {
					s.Chaos.LoadedRunID = runID
					s.Chaos.MindMap = finalMap
				}
			})
			return
		}
		// A select rather than a sleep, so a session ending is acted on at
		// once instead of up to half a second later — the window in which the
		// old code could still be writing a run file.
		select {
		case <-stop:
			return
		case <-time.After(chaosSyncInterval):
		}
	}
}

// chaosStateFromStatus builds the session's view of a run from one engine
// status snapshot: counters, the recent attempts (with anything typed into a
// hidden field masked on the way in, so the session never holds it), and why
// the run ended once it has. It deliberately leaves MindMap empty; see
// syncChaosStatus.
func chaosStateFromStatus(st chaos.Status, runConfig *session.ChaosRunConfig, targetHost string) *session.ChaosState {
	firstHash := strings.TrimSpace(st.FirstScreenHash)
	if firstHash == "" {
		firstHash = chaosFirstScreenHashFromAttempts(st.RecentAttempts)
	}
	var last *chaos.Attempt
	if st.LastAttempt != nil {
		masked := redactedChaosAttempt(st.MindMap, *st.LastAttempt)
		last = &masked
	}
	return &session.ChaosState{
		Active:              st.Active,
		StepsRun:            st.StepsRun,
		StartedAt:           st.StartedAt,
		StoppedAt:           st.StoppedAt,
		MaxSteps:            st.MaxSteps,
		TimeBudget:          st.TimeBudget,
		Transitions:         st.Transitions,
		UniqueScreens:       st.UniqueScreens,
		UniqueInputs:        st.UniqueInputs,
		AIDKeyCounts:        st.AIDKeyCounts,
		LoadedRunID:         st.LoadedRunID,
		LastAttempt:         toSessionChaosAttempt(last),
		RecentAttempts:      toSessionChaosAttempts(redactedChaosAttempts(st.MindMap, st.RecentAttempts)),
		Error:               st.Error,
		TerminationReason:   st.TerminationReason,
		SaturatedNoProgress: st.SaturatedNoProgress,
		TargetHost:          targetHost,
		FirstScreenHash:     firstHash,
		Config:              runConfig.Clone(),
	}
}

// stampChaosRun records on a finished run where it ran and why it ended.
// Anything the engine already filled in is left as the engine wrote it.
func stampChaosRun(run *chaos.SavedRun, st chaos.Status, targetHost string) {
	if run == nil {
		return
	}
	if run.TargetHost == "" {
		run.TargetHost = targetHost
	}
	if run.TerminationReason == "" {
		run.TerminationReason = st.TerminationReason
	}
	if run.FirstScreenHash == "" {
		run.FirstScreenHash = strings.TrimSpace(st.FirstScreenHash)
		if run.FirstScreenHash == "" {
			run.FirstScreenHash = chaosFirstScreenHashFromAttempts(run.Attempts)
		}
	}
}

// ChaosListRunsHandler handles GET /chaos/runs – returns saved run metadata.
func (app *App) ChaosListRunsHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	metas, err := chaos.ListRuns(app.chaosRunsDirForSession(s.OwnerID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if metas == nil {
		metas = []chaos.SavedRunMeta{}
	}
	c.JSON(http.StatusOK, metas)
}

// chaosLabelRunRequest is the JSON body for POST /chaos/runs/label.
type chaosLabelRunRequest struct {
	ID    string `json:"id"`
	RunID string `json:"runID"`
	Label string `json:"label"`
}

// maxChaosRunLabelRunes bounds a run's label: it is a name for a list, not a
// description.
const maxChaosRunLabelRunes = 80

// sanitizeChaosRunLabel reduces a label to one line of printable text.
func sanitizeChaosRunLabel(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, raw)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if runes := []rune(cleaned); len(runes) > maxChaosRunLabelRunes {
		cleaned = strings.TrimSpace(string(runes[:maxChaosRunLabelRunes]))
	}
	return cleaned
}

// ChaosLabelRunHandler handles POST /chaos/runs/label – gives a saved run a
// name, so a list of them is more than a column of timestamps. An empty label
// clears it. Body: {"id": "<run id>", "label": "<name>"}.
func (app *App) ChaosLabelRunHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var req chaosLabelRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "body must be JSON of the form {\"id\": \"<run id>\", \"label\": \"<name>\"}"})
		return
	}
	runID := strings.TrimSpace(firstNonEmptyString(req.ID, req.RunID))
	if runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required: the ID of the saved run to name"})
		return
	}
	label := sanitizeChaosRunLabel(req.Label)
	runsDir := app.chaosRunsDirForSession(s.OwnerID)

	// When the run is the one this session holds, label that copy and write
	// it out: it may carry annotations newer than the file's.
	var encoded []byte
	held, err := app.chaosEngines.withLoadedRun(s.ID, func(run *chaos.SavedRun) error {
		if run.ID != runID {
			return errChaosRunNotHeld
		}
		run.Label = label
		data, encErr := chaos.EncodeRun(run)
		if encErr != nil {
			return encErr
		}
		encoded = data
		return nil
	})
	switch {
	case held && err == nil:
		if _, loadErr := chaos.LoadRun(runsDir, runID); loadErr != nil {
			// Held in memory but never saved (a recording seed, say): the
			// label lives with the session's copy only.
			c.JSON(http.StatusOK, gin.H{"status": "labelled", "id": runID, "label": label, "saved": false})
			return
		}
		if writeErr := chaos.WriteRunFile(runsDir, runID, encoded); writeErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": writeErr.Error()})
			return
		}
	case held && !errors.Is(err, errChaosRunNotHeld):
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	default:
		run, loadErr := chaos.LoadRun(runsDir, runID)
		if loadErr != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": loadErr.Error()})
			return
		}
		run.Label = label
		if saveErr := chaos.SaveRun(runsDir, run); saveErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": saveErr.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "labelled", "id": runID, "label": label, "saved": true})
}

// errChaosRunNotHeld reports that the session's loaded run is not the one a
// request named.
var errChaosRunNotHeld = errors.New("run is not the one held by this session")

// chaosLoadRequest is the JSON body for POST /chaos/load.
type chaosLoadRequest struct {
	RunID string `json:"runID"`
}

type chaosDeleteRunRequest struct {
	RunID string `json:"runID"`
}

// ChaosLoadHandler handles POST /chaos/load – loads a saved run into the session.
func (app *App) ChaosLoadHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	var req chaosLoadRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RunID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "runID is required"})
		return
	}

	run, err := chaos.LoadRun(app.chaosRunsDirForSession(s.OwnerID), req.RunID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// Refused while a run is active, under the same lock the start path
	// takes. Loading used to go ahead regardless: it replaced the session's
	// loaded run and wiped its status while the engine carried on typing
	// into the host, and when that run finished it overwrote what had just
	// been loaded.
	display := redactedMindMap(run.MindMap)
	if !app.chaosEngines.loadRecordingIfInactive(s.ID, run) {
		c.JSON(http.StatusConflict, gin.H{
			"error": "a chaos run is active; stop it (or let it finish) before loading another run",
			"code":  "chaos_run_active",
		})
		return
	}
	withSessionLock(s, func() {
		// Loading a chaos run should clear stale active-run status metadata.
		s.Chaos = nil
	})
	resp := gin.H{
		"runID":         run.ID,
		"stepsRun":      run.StepsRun,
		"transitions":   run.Transitions,
		"uniqueScreens": run.UniqueScreens,
		"uniqueInputs":  run.UniqueInputs,
		"uniqueKeys":    len(run.AIDKeyCounts),
		"mindMap":       display,
	}
	if run.TerminationReason != "" {
		resp["terminationReason"] = run.TerminationReason
		resp["terminationSummary"] = chaosTerminationSummary(run.TerminationReason, false)
	}
	if run.TargetHost != "" {
		resp["targetHost"] = run.TargetHost
	}
	if run.Label != "" {
		resp["label"] = run.Label
	}
	c.JSON(http.StatusOK, resp)
}

// ChaosDeleteRunHandler handles POST /chaos/runs/delete – deletes a saved run from disk.
func (app *App) ChaosDeleteRunHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	var req chaosDeleteRunRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.RunID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "runID is required"})
		return
	}
	runID := strings.TrimSpace(req.RunID)
	if err := chaos.DeleteRun(app.chaosRunsDirForSession(s.OwnerID), runID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// If the deleted run is currently loaded in this session, clear the loaded reference.
	if loaded, ok := app.chaosEngines.getLoadedRun(s.ID); ok && loaded != nil && loaded.ID == runID {
		app.chaosEngines.deleteLoadedRun(s.ID)
		withSessionLock(s, func() {
			if s.Chaos != nil && s.Chaos.LoadedRunID == runID {
				s.Chaos.LoadedRunID = ""
			}
		})
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted", "runID": runID})
}

// ChaosLoadRecordingHandler handles POST /chaos/load-recording – seeds chaos
// mode with the currently loaded recording.
func (app *App) ChaosLoadRecordingHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var workflowPayload []byte
	withSessionLock(s, func() {
		if s.LoadedWorkflow != nil {
			workflowPayload = append([]byte(nil), s.LoadedWorkflow.Payload...)
		}
	})
	if len(workflowPayload) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no recording loaded; load a recording first"})
		return
	}

	workflow, err := parseWorkflowPayload(workflowPayload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("loaded recording is invalid: %v", err)})
		return
	}

	run := chaosSeedRunFromWorkflow(workflow)
	if !app.chaosEngines.loadRecordingIfInactive(s.ID, run) {
		c.JSON(http.StatusConflict, gin.H{"error": "chaos exploration is already running"})
		return
	}
	withSessionLock(s, func() {
		// Loading a recording into chaos should clear stale active-run metadata.
		s.Chaos = nil
	})

	c.JSON(http.StatusOK, gin.H{
		"status":        "loaded",
		"source":        "recording",
		"runID":         run.ID,
		"stepsSeeded":   len(run.Steps),
		"stepsRun":      run.StepsRun,
		"transitions":   run.Transitions,
		"uniqueScreens": run.UniqueScreens,
		"uniqueInputs":  run.UniqueInputs,
		"uniqueKeys":    len(run.AIDKeyCounts),
		"mindMap":       redactedMindMap(run.MindMap),
	})
}

// ChaosResumeHandler handles POST /chaos/resume – resumes from a loaded run.
func (app *App) ChaosResumeHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	loaded, ok := app.chaosEngines.getLoadedRun(s.ID)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no run loaded; call POST /chaos/load first"})
		return
	}
	if loaded == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "loaded run is invalid; load a run again"})
		return
	}

	var h interface{ IsConnected() bool }
	withSessionLock(s, func() { h = s.Host })
	if h == nil || !h.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not connected to host"})
		return
	}

	// Parse optional config overrides (same fields as /chaos/start), on top
	// of the same configured defaults a fresh start gets: continuing a run
	// must not quietly drop the operator's CHAOS_* settings (step delay
	// included) and fall back to the built-in ones.
	cfg := chaos.DefaultConfig()
	app.applyChaosEnvSettings(&cfg)
	var req chaosStartRequest
	if err := bindChaosStartRequest(c, &req); err != nil {
		rejectChaosBody(c, err)
		return
	}
	applyChaosStartRequestToConfig(&cfg, req)
	var savedHints chaosHintsPayload
	if saved, err := app.loadChaosHintsPayload(s.OwnerID); err == nil {
		savedHints = saved
		if len(cfg.Hints) == 0 && len(saved.Hints) > 0 {
			cfg.Hints = saved.Hints
		}
		if len(cfg.KeyBlacklist) == 0 && len(saved.KeyBlacklist) > 0 {
			cfg.KeyBlacklist = append([]string(nil), saved.KeyBlacklist...)
		}
	}
	if req.ExtendLimits && cfg.MaxSteps > 0 {
		// "Extend" treats maxSteps as an additional budget beyond the loaded run's
		// existing attempts so a completed run can continue instead of immediately
		// stopping at the same cap.
		cfg.MaxSteps = loaded.StepsRun + cfg.MaxSteps
	}
	cfg.ScreenHints = app.resolveChaosScreenHints(s.ID, req, savedHints)
	app.confineChaosPaths(&cfg, loadedWorkflowName(s), s.OwnerID)
	withSessionLock(s, func() {
		cfg.ExportHost = s.TargetHost
		cfg.ExportPort = s.TargetPort
	})

	runConfig := chaosRunConfigSummary(cfg, true)

	eng, err, started := app.chaosEngines.resumeIfAbsent(s.ID, func() (*chaos.Engine, error) {
		var built *chaos.Engine
		withSessionLock(s, func() {
			built = chaos.New(s.Host, cfg)
		})
		return built, nil
	}, func(built *chaos.Engine) error {
		return built.Resume(loaded)
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to resume: %v", err)})
		return
	}
	if !started {
		c.JSON(http.StatusConflict, gin.H{"error": "chaos exploration is already running"})
		return
	}
	app.startChaosSync(s, eng, runConfig)

	c.JSON(http.StatusOK, gin.H{
		"status":      "resumed",
		"loadedRunID": loaded.ID,
	})
}

type chaosHintsPayload struct {
	Hints           []chaos.Hint      `json:"hints"`
	KeyBlacklist    []string          `json:"keyBlacklist,omitempty"`
	FirstScreenHint *chaos.ScreenHint `json:"firstScreenHint,omitempty"`
}

type chaosHintsExtractResponse struct {
	Source string       `json:"source"`
	Hints  []chaos.Hint `json:"hints"`
}

type chaosScreenHintsResponse struct {
	ScreenHints map[string]chaos.ScreenHint `json:"screenHints"`
}

// chaosScreenHintUpsertRequest accepts both snake_case (the public MCP/HTTP
// tool descriptor) and camelCase (the existing in-browser UI) forms. The
// custom UnmarshalJSON decodes both shapes; snake_case wins when both keys
// are present on the same field.
type chaosScreenHintUpsertRequest struct {
	ScreenHash     string            `json:"screen_hash"`
	KnownData      []string          `json:"known_data"`
	KnownKeys      []string          `json:"known_keys"`
	BlockedKeys    []string          `json:"blocked_keys"`
	KeyAssignments map[string]string `json:"key_assignments"`
}

func (r *chaosScreenHintUpsertRequest) UnmarshalJSON(data []byte) error {
	var snake struct {
		ScreenHash     string            `json:"screen_hash"`
		KnownData      []string          `json:"known_data"`
		KnownKeys      []string          `json:"known_keys"`
		BlockedKeys    []string          `json:"blocked_keys"`
		KeyAssignments map[string]string `json:"key_assignments"`
	}
	if err := json.Unmarshal(data, &snake); err != nil {
		return err
	}
	var camel struct {
		ScreenHash     string            `json:"screenHash"`
		KnownData      []string          `json:"knownData"`
		KnownKeys      []string          `json:"knownKeys"`
		BlockedKeys    []string          `json:"blockedKeys"`
		KeyAssignments map[string]string `json:"keyAssignments"`
	}
	_ = json.Unmarshal(data, &camel)
	r.ScreenHash = firstNonEmptyString(snake.ScreenHash, camel.ScreenHash)
	r.KnownData = firstNonNilStrings(snake.KnownData, camel.KnownData)
	r.KnownKeys = firstNonNilStrings(snake.KnownKeys, camel.KnownKeys)
	r.BlockedKeys = firstNonNilStrings(snake.BlockedKeys, camel.BlockedKeys)
	if len(snake.KeyAssignments) > 0 {
		r.KeyAssignments = snake.KeyAssignments
	} else {
		r.KeyAssignments = camel.KeyAssignments
	}
	return nil
}

func firstNonEmptyString(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func firstNonNilStrings(a, b []string) []string {
	if a != nil {
		return a
	}
	return b
}

// ChaosHintsGetHandler handles GET /chaos/hints – returns saved chaos hints.
func (app *App) ChaosHintsGetHandler(c *gin.Context) {
	payload, err := app.loadChaosHintsPayload(principalFrom(c).UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"hints":           payload.Hints,
		"keyBlacklist":    payload.KeyBlacklist,
		"firstScreenHint": payload.FirstScreenHint,
	})
}

// ChaosHintsSaveHandler handles POST /chaos/hints – persists chaos hint data.
func (app *App) ChaosHintsSaveHandler(c *gin.Context) {
	var req chaosHintsPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	payload := chaosHintsPayload{
		Hints:           sanitizeChaosHints(req.Hints),
		KeyBlacklist:    sanitizeChaosKeyBlacklist(req.KeyBlacklist),
		FirstScreenHint: sanitizeChaosScreenHintPtr(req.FirstScreenHint),
	}
	if err := app.saveChaosHintsPayload(principalFrom(c).UserID, payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if s := app.getSession(c); s != nil {
		if eng, ok := app.chaosEngines.get(s.ID); ok && eng != nil {
			eng.SetKeyBlacklist(payload.KeyBlacklist)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"status":          "saved",
		"hints":           payload.Hints,
		"keyBlacklist":    payload.KeyBlacklist,
		"firstScreenHint": payload.FirstScreenHint,
	})
}

// ChaosHintsExtractHandler handles POST /chaos/hints/extract-recording.
// It extracts hint candidates from a workflow recording, either uploaded
// as multipart form file "workflow" or from the currently loaded recording
// in session if no file is provided.
func (app *App) ChaosHintsExtractHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	workflow, source, err := app.workflowForHintExtraction(c, s)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hints := extractChaosHintsFromWorkflow(workflow)
	c.JSON(http.StatusOK, chaosHintsExtractResponse{
		Source: source,
		Hints:  hints,
	})
}

// ChaosScreenHintsGetHandler returns session-scoped per-screen chaos hints.
func (app *App) ChaosScreenHintsGetHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, chaosScreenHintsResponse{
		ScreenHints: app.chaosEngines.getScreenHints(s.ID),
	})
}

// ChaosScreenHintsSaveHandler upserts per-screen chaos hints and applies them
// live to a running engine (if present).
func (app *App) ChaosScreenHintsSaveHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var req chaosScreenHintUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	screenHash := strings.TrimSpace(req.ScreenHash)
	if screenHash == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "screen_hash is required"})
		return
	}
	updated := app.chaosEngines.upsertScreenHint(s.ID, screenHash, chaos.ScreenHint{
		KnownData:      req.KnownData,
		KnownKeys:      req.KnownKeys,
		BlockedKeys:    req.BlockedKeys,
		KeyAssignments: req.KeyAssignments,
	})
	if eng, ok := app.chaosEngines.get(s.ID); ok && eng != nil {
		eng.SetScreenHints(updated)
	}
	c.JSON(http.StatusOK, chaosScreenHintsResponse{ScreenHints: updated})
}

func sanitizeChaosHints(hints []chaos.Hint) []chaos.Hint {
	if len(hints) == 0 {
		return []chaos.Hint{}
	}
	out := make([]chaos.Hint, 0, len(hints))
	for _, hint := range hints {
		tx := strings.TrimSpace(hint.Transaction)
		known := make([]string, 0, len(hint.KnownData))
		seenKnown := make(map[string]bool)
		for _, raw := range hint.KnownData {
			value := strings.TrimSpace(raw)
			if value == "" || seenKnown[value] {
				continue
			}
			known = append(known, value)
			seenKnown[value] = true
		}
		assignments := make(map[string]string)
		for rawLabel, rawKey := range hint.KeyAssignments {
			label := strings.TrimSpace(rawLabel)
			keyText := sanitizeChaosHintKeyName(rawKey)
			if label == "" || keyText == "" {
				continue
			}
			assignments[label] = keyText
		}
		if len(assignments) == 0 {
			assignments = nil
		}
		if tx == "" && len(known) == 0 && len(assignments) == 0 {
			continue
		}
		out = append(out, chaos.Hint{
			Transaction:    tx,
			KnownData:      known,
			KeyAssignments: assignments,
		})
	}
	return out
}

func sanitizeChaosKeyBlacklist(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(keys))
	seen := make(map[string]bool)
	for _, raw := range keys {
		key := sanitizeChaosHintKeyName(raw)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

func sanitizeChaosHintKeyName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.ContainsAny(trimmed, "\n\r\t;") {
		return ""
	}
	if normalized, ok := normalizeKey(trimmed); ok {
		return normalized
	}
	// Preserve unrecognised but sanitized key names so host-specific keys can
	// still be used by chaos hints.
	return trimmed
}

func sanitizeChaosScreenHint(h chaos.ScreenHint) chaos.ScreenHint {
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
		k := sanitizeChaosHintKeyName(raw)
		if k == "" || seenKeys[k] {
			continue
		}
		seenKeys[k] = true
		knownKeys = append(knownKeys, k)
	}
	blockedKeys := make([]string, 0, len(h.BlockedKeys))
	seenBlocked := make(map[string]bool)
	for _, raw := range h.BlockedKeys {
		k := sanitizeChaosHintKeyName(raw)
		if k == "" || seenBlocked[k] {
			continue
		}
		seenBlocked[k] = true
		blockedKeys = append(blockedKeys, k)
	}
	assignments := make(map[string]string)
	for rawLabel, rawKey := range h.KeyAssignments {
		label := strings.TrimSpace(rawLabel)
		key := sanitizeChaosHintKeyName(rawKey)
		if label == "" || key == "" {
			continue
		}
		assignments[label] = key
	}
	if len(assignments) == 0 {
		assignments = nil
	}
	return chaos.ScreenHint{
		KnownData:      knownData,
		KnownKeys:      knownKeys,
		BlockedKeys:    blockedKeys,
		KeyAssignments: assignments,
	}
}

func sanitizeChaosScreenHintPtr(h *chaos.ScreenHint) *chaos.ScreenHint {
	if h == nil {
		return nil
	}
	clean := sanitizeChaosScreenHint(*h)
	if len(clean.KnownData) == 0 && len(clean.KnownKeys) == 0 && len(clean.BlockedKeys) == 0 && len(clean.KeyAssignments) == 0 {
		return nil
	}
	return &clean
}

// resolveChaosScreenHints combines the session's screen-scoped hints with the
// first-screen hint sources in explicit precedence order: a first_screen_hint
// in the request body wins, then a session-scoped __FIRST_SCREEN__ hint saved
// via /chaos/screen-hints, and only when neither exists does the hint from the
// saved hints file apply. Previously the saved-file hint unconditionally
// overwrote a session-scoped first-screen hint.
func (app *App) resolveChaosScreenHints(sessionID string, req chaosStartRequest, savedHints chaosHintsPayload) map[string]chaos.ScreenHint {
	hints := app.chaosEngines.getScreenHints(sessionID)
	if req.FirstScreenHint != nil {
		return mergeFirstScreenHintIntoChaosScreenHints(hints, *req.FirstScreenHint)
	}
	if _, ok := hints[chaos.FirstScreenHintKey]; ok {
		return hints
	}
	if savedHints.FirstScreenHint != nil {
		return mergeFirstScreenHintIntoChaosScreenHints(hints, *savedHints.FirstScreenHint)
	}
	return hints
}

func mergeFirstScreenHintIntoChaosScreenHints(in map[string]chaos.ScreenHint, hint chaos.ScreenHint) map[string]chaos.ScreenHint {
	clean := sanitizeChaosScreenHint(hint)
	if len(clean.KnownData) == 0 && len(clean.KnownKeys) == 0 && len(clean.BlockedKeys) == 0 && len(clean.KeyAssignments) == 0 {
		return cloneChaosScreenHintsMap(in)
	}
	out := cloneChaosScreenHintsMap(in)
	if out == nil {
		out = make(map[string]chaos.ScreenHint)
	}
	out[chaos.FirstScreenHintKey] = clean
	return out
}

func cloneChaosScreenHintsMap(in map[string]chaos.ScreenHint) map[string]chaos.ScreenHint {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]chaos.ScreenHint, len(in))
	for hash, hint := range in {
		key := strings.TrimSpace(hash)
		if key == "" {
			continue
		}
		clean := sanitizeChaosScreenHint(hint)
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

func (app *App) workflowForHintExtraction(c *gin.Context, s *session.Session) (*WorkflowConfig, string, error) {
	if c != nil && strings.EqualFold(c.ContentType(), "multipart/form-data") {
		if _, err := c.FormFile("workflow"); err == nil {
			upload, uploadErr := loadWorkflowUpload(c)
			if uploadErr != nil {
				return nil, "", fmt.Errorf("load recording failed: %w", uploadErr)
			}
			return upload.Config, "upload", nil
		} else if !errors.Is(err, http.ErrMissingFile) {
			return nil, "", fmt.Errorf("read upload failed: %w", err)
		}
	}
	if s == nil {
		return nil, "", fmt.Errorf("no recording loaded; upload a workflow or load a recording first")
	}

	var payload []byte
	withSessionLock(s, func() {
		if s.LoadedWorkflow != nil {
			payload = append([]byte(nil), s.LoadedWorkflow.Payload...)
		}
	})
	if len(payload) == 0 {
		return nil, "", fmt.Errorf("no recording loaded; upload a workflow or load a recording first")
	}

	workflow, err := parseWorkflowPayload(payload)
	if err != nil {
		return nil, "", fmt.Errorf("loaded recording is invalid: %w", err)
	}
	return workflow, "loaded", nil
}

func extractChaosHintsFromWorkflow(workflow *WorkflowConfig) []chaos.Hint {
	if workflow == nil || len(workflow.Steps) == 0 {
		return []chaos.Hint{}
	}

	hints := make([]chaos.Hint, 0)
	batch := make([]string, 0, 8)
	flushBatch := func() {
		if len(batch) == 0 {
			return
		}
		hint := hintFromFillValues(batch)
		if hint.Transaction != "" || len(hint.KnownData) > 0 {
			hints = append(hints, hint)
		}
		batch = batch[:0]
	}

	for _, step := range workflow.Steps {
		if strings.EqualFold(step.Type, "FillString") {
			v := strings.TrimSpace(step.Text)
			if v != "" {
				batch = append(batch, v)
			}
			continue
		}
		flushBatch()
	}
	flushBatch()

	return sanitizeChaosHints(hints)
}

func hintFromFillValues(values []string) chaos.Hint {
	if len(values) == 0 {
		return chaos.Hint{}
	}
	unique := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		unique = append(unique, v)
		seen[v] = true
	}
	if len(unique) == 0 {
		return chaos.Hint{}
	}

	tx := ""
	known := make([]string, 0, len(unique))
	for idx, v := range unique {
		if tx == "" && idx < 2 && looksLikeTransactionCode(v) {
			tx = strings.ToUpper(v)
			continue
		}
		known = append(known, v)
	}
	if tx == "" && len(unique) == 1 && looksLikeTransactionCode(unique[0]) {
		tx = strings.ToUpper(unique[0])
		known = known[:0]
	}

	return chaos.Hint{
		Transaction: tx,
		KnownData:   known,
	}
}

func looksLikeTransactionCode(value string) bool {
	v := strings.TrimSpace(value)
	if len(v) < 2 || len(v) > 12 {
		return false
	}
	if strings.ContainsAny(v, " \t\r\n") {
		return false
	}

	hasLetter := false
	for _, r := range v {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= 'a' && r <= 'z':
			hasLetter = true
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '/':
		default:
			return false
		}
	}
	return hasLetter
}

func (app *App) loadChaosHintsPayload(ownerID string) (chaosHintsPayload, error) {
	if app == nil || strings.TrimSpace(app.baseDir) == "" {
		return chaosHintsPayload{Hints: []chaos.Hint{}, KeyBlacklist: []string{}}, nil
	}
	app.chaosHintsMu.Lock()
	defer app.chaosHintsMu.Unlock()
	hintsPath := app.dataScopeFor(ownerID).chaosHintsPath()
	data, err := os.ReadFile(hintsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return chaosHintsPayload{Hints: []chaos.Hint{}, KeyBlacklist: []string{}}, nil
		}
		return chaosHintsPayload{}, fmt.Errorf("read chaos hints: %w", err)
	}
	var payload chaosHintsPayload
	if err := json.Unmarshal(data, &payload); err == nil {
		return chaosHintsPayload{
			Hints:           sanitizeChaosHints(payload.Hints),
			KeyBlacklist:    sanitizeChaosKeyBlacklist(payload.KeyBlacklist),
			FirstScreenHint: sanitizeChaosScreenHintPtr(payload.FirstScreenHint),
		}, nil
	}
	var hints []chaos.Hint
	if err := json.Unmarshal(data, &hints); err != nil {
		return chaosHintsPayload{}, fmt.Errorf("parse chaos hints: %w", err)
	}
	return chaosHintsPayload{
		Hints:           sanitizeChaosHints(hints),
		KeyBlacklist:    []string{},
		FirstScreenHint: nil,
	}, nil
}

func (app *App) saveChaosHintsPayload(ownerID string, payload chaosHintsPayload) error {
	if app == nil || strings.TrimSpace(app.baseDir) == "" {
		return fmt.Errorf("chaos hints path not configured")
	}
	app.chaosHintsMu.Lock()
	defer app.chaosHintsMu.Unlock()
	hintsPath := app.dataScopeFor(ownerID).chaosHintsPath()
	if err := os.MkdirAll(filepath.Dir(hintsPath), 0750); err != nil {
		return fmt.Errorf("create chaos hints directory: %w", err)
	}
	payload = chaosHintsPayload{
		Hints:           sanitizeChaosHints(payload.Hints),
		KeyBlacklist:    sanitizeChaosKeyBlacklist(payload.KeyBlacklist),
		FirstScreenHint: sanitizeChaosScreenHintPtr(payload.FirstScreenHint),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal chaos hints: %w", err)
	}
	if err := chaos.WriteFileAtomic(hintsPath, data, 0600); err != nil {
		return fmt.Errorf("write chaos hints: %w", err)
	}
	return nil
}

// chaosStatusToJSON renders a live engine status. Values typed into hidden
// fields are masked in the attempts and in the mind map; st.MindMap is the
// caller's own copy (Engine.Status clones it), so it is scrubbed in place.
func chaosStatusToJSON(st chaos.Status) gin.H {
	resp := gin.H{
		"active":        st.Active,
		"stepsRun":      st.StepsRun,
		"transitions":   st.Transitions,
		"uniqueScreens": st.UniqueScreens,
		"uniqueInputs":  st.UniqueInputs,
	}
	if len(st.AIDKeyCounts) > 0 {
		resp["aidKeyCounts"] = st.AIDKeyCounts
	}
	if st.LoadedRunID != "" {
		resp["loadedRunID"] = st.LoadedRunID
	}
	if st.FirstScreenHash != "" {
		resp["firstScreenHash"] = st.FirstScreenHash
	} else if firstHash := chaosFirstScreenHashFromAttempts(st.RecentAttempts); firstHash != "" {
		resp["firstScreenHash"] = firstHash
	}
	if !st.StartedAt.IsZero() {
		resp["startedAt"] = st.StartedAt.Format(time.RFC3339)
	}
	if !st.StoppedAt.IsZero() {
		resp["stoppedAt"] = st.StoppedAt.Format(time.RFC3339)
	}
	if st.MaxSteps > 0 {
		resp["maxSteps"] = st.MaxSteps
	}
	if st.TimeBudget > 0 {
		resp["timeBudgetSec"] = st.TimeBudget.Seconds()
	}
	if st.LastAttempt != nil {
		resp["lastAttempt"] = chaosAttemptToJSON(redactedChaosAttempt(st.MindMap, *st.LastAttempt))
	}
	if len(st.RecentAttempts) > 0 {
		attempts := make([]gin.H, 0, len(st.RecentAttempts))
		for _, attempt := range st.RecentAttempts {
			attempts = append(attempts, chaosAttemptToJSON(redactedChaosAttempt(st.MindMap, attempt)))
		}
		resp["recentAttempts"] = attempts
	}
	if st.MindMap != nil {
		scrubMindMapSecrets(st.MindMap, chaosHiddenValueMask)
		resp["mindMap"] = st.MindMap
	}
	// Surface why the run stopped so the UI and Copilot agent can react
	// (e.g. "blocked" -> relax the key blacklist; "saturated" with
	// saturatedNoProgress -> stop resuming and update hints instead).
	if st.TerminationReason != "" {
		resp["terminationReason"] = st.TerminationReason
		resp["terminationSummary"] = chaosTerminationSummary(st.TerminationReason, st.SaturatedNoProgress)
	}
	if st.SaturatedNoProgress {
		resp["saturatedNoProgress"] = true
	}
	if cov := st.CoverageStats; cov != nil {
		resp["coverageStats"] = gin.H{
			"windowSteps":               cov.WindowSteps,
			"newScreensLast10Steps":     cov.NewScreensInWindow,
			"newTransitionsLast10Steps": cov.NewTransitionsInWindow,
			"saturationStreak":          cov.SaturationStreak,
			"saturationThresholdSteps":  cov.SaturationThresholdSteps,
			"frontierAreas":             cov.FrontierAreas,
			"untriedKeysTotal":          cov.UntriedKeysTotal,
		}
	}
	if st.Error != "" {
		resp["error"] = st.Error
	}
	return resp
}

// chaosStateToJSON renders the session's kept view of a run — what status
// answers from once the engine has gone. It carries the same end-of-run
// fields the live status does (terminationReason, saturatedNoProgress,
// targetHost), because "why did it stop?" is asked after the run, which is
// exactly when the engine is no longer there to say.
//
// The mind map is not included: the caller decides where the map comes from
// and how much of it to send.
func chaosStateToJSON(state *session.ChaosState) gin.H {
	if state == nil {
		return nil
	}
	resp := gin.H{
		"active":        state.Active,
		"stepsRun":      state.StepsRun,
		"transitions":   state.Transitions,
		"uniqueScreens": state.UniqueScreens,
		"uniqueInputs":  state.UniqueInputs,
	}
	if len(state.AIDKeyCounts) > 0 {
		resp["aidKeyCounts"] = state.AIDKeyCounts
	}
	if state.LoadedRunID != "" {
		resp["loadedRunID"] = state.LoadedRunID
	}
	if state.FirstScreenHash != "" {
		resp["firstScreenHash"] = state.FirstScreenHash
	} else if firstHash := sessionChaosFirstScreenHashFromAttempts(state.RecentAttempts); firstHash != "" {
		resp["firstScreenHash"] = firstHash
	}
	if !state.StartedAt.IsZero() {
		resp["startedAt"] = state.StartedAt.Format(time.RFC3339)
	}
	if !state.StoppedAt.IsZero() {
		resp["stoppedAt"] = state.StoppedAt.Format(time.RFC3339)
	}
	if state.MaxSteps > 0 {
		resp["maxSteps"] = state.MaxSteps
	}
	if state.TimeBudget > 0 {
		resp["timeBudgetSec"] = state.TimeBudget.Seconds()
	}
	if state.LastAttempt != nil {
		resp["lastAttempt"] = sessionChaosAttemptToJSON(*state.LastAttempt)
	}
	if len(state.RecentAttempts) > 0 {
		attempts := make([]gin.H, 0, len(state.RecentAttempts))
		for _, attempt := range state.RecentAttempts {
			attempts = append(attempts, sessionChaosAttemptToJSON(attempt))
		}
		resp["recentAttempts"] = attempts
	}
	if state.TerminationReason != "" {
		resp["terminationReason"] = state.TerminationReason
	}
	if state.SaturatedNoProgress {
		resp["saturatedNoProgress"] = true
	}
	if state.TargetHost != "" {
		resp["targetHost"] = state.TargetHost
	}
	if state.Config != nil {
		resp["config"] = chaosRunConfigToJSON(state.Config)
	}
	if state.Error != "" {
		resp["error"] = state.Error
	}
	return resp
}

func chaosFirstScreenHashFromAttempts(attempts []chaos.Attempt) string {
	for _, attempt := range attempts {
		if hash := strings.TrimSpace(attempt.FromHash); hash != "" {
			return hash
		}
	}
	return ""
}

func sessionChaosFirstScreenHashFromAttempts(attempts []session.ChaosAttempt) string {
	for _, attempt := range attempts {
		if hash := strings.TrimSpace(attempt.FromHash); hash != "" {
			return hash
		}
	}
	return ""
}

func marshalChaosMindMap(mindMap *chaos.MindMap) json.RawMessage {
	if mindMap == nil {
		return nil
	}
	data, err := json.Marshal(mindMap)
	if err != nil || len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.RawMessage(data)
}

func chaosMindMapToJSON(mindMap *chaos.MindMap) interface{} {
	if mindMap == nil {
		return nil
	}
	return rawJSONToInterface(marshalChaosMindMap(mindMap))
}

func rawJSONToInterface(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	return decoded
}

func chaosAttemptToJSON(attempt chaos.Attempt) gin.H {
	fieldWrites := make([]gin.H, 0, len(attempt.FieldWrites))
	for _, fw := range attempt.FieldWrites {
		fieldWrites = append(fieldWrites, gin.H{
			"row":     fw.Row,
			"column":  fw.Column,
			"length":  fw.Length,
			"value":   fw.Value,
			"success": fw.Success,
			"error":   fw.Error,
		})
	}
	return gin.H{
		"attempt":        attempt.Attempt,
		"time":           attempt.Time.Format(time.RFC3339),
		"fromHash":       attempt.FromHash,
		"toHash":         attempt.ToHash,
		"aidKey":         attempt.AIDKey,
		"fieldsTargeted": attempt.FieldsTargeted,
		"fieldsWritten":  attempt.FieldsWritten,
		"transitioned":   attempt.Transitioned,
		"error":          attempt.Error,
		"fieldWrites":    fieldWrites,
	}
}

func sessionChaosAttemptToJSON(attempt session.ChaosAttempt) gin.H {
	fieldWrites := make([]gin.H, 0, len(attempt.FieldWrites))
	for _, fw := range attempt.FieldWrites {
		fieldWrites = append(fieldWrites, gin.H{
			"row":     fw.Row,
			"column":  fw.Column,
			"length":  fw.Length,
			"value":   fw.Value,
			"success": fw.Success,
			"error":   fw.Error,
		})
	}
	return gin.H{
		"attempt":        attempt.Attempt,
		"time":           attempt.Time.Format(time.RFC3339),
		"fromHash":       attempt.FromHash,
		"toHash":         attempt.ToHash,
		"aidKey":         attempt.AIDKey,
		"fieldsTargeted": attempt.FieldsTargeted,
		"fieldsWritten":  attempt.FieldsWritten,
		"transitioned":   attempt.Transitioned,
		"error":          attempt.Error,
		"fieldWrites":    fieldWrites,
	}
}

func toSessionChaosAttempts(attempts []chaos.Attempt) []session.ChaosAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]session.ChaosAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, toSessionChaosAttemptValue(attempt))
	}
	return out
}

func toSessionChaosAttempt(attempt *chaos.Attempt) *session.ChaosAttempt {
	if attempt == nil {
		return nil
	}
	mapped := toSessionChaosAttemptValue(*attempt)
	return &mapped
}

func toSessionChaosAttemptValue(attempt chaos.Attempt) session.ChaosAttempt {
	fieldWrites := make([]session.ChaosFieldWrite, 0, len(attempt.FieldWrites))
	for _, fw := range attempt.FieldWrites {
		fieldWrites = append(fieldWrites, session.ChaosFieldWrite{
			Row:     fw.Row,
			Column:  fw.Column,
			Length:  fw.Length,
			Value:   fw.Value,
			Success: fw.Success,
			Error:   fw.Error,
		})
	}
	return session.ChaosAttempt{
		Attempt:        attempt.Attempt,
		Time:           attempt.Time,
		FromHash:       attempt.FromHash,
		ToHash:         attempt.ToHash,
		AIDKey:         attempt.AIDKey,
		FieldsTargeted: attempt.FieldsTargeted,
		FieldsWritten:  attempt.FieldsWritten,
		Transitioned:   attempt.Transitioned,
		Error:          attempt.Error,
		FieldWrites:    fieldWrites,
	}
}

func chaosStateSnapshot(s *session.Session) *session.ChaosState {
	if s == nil {
		return nil
	}
	var snapshot *session.ChaosState
	withSessionLock(s, func() {
		if s.Chaos == nil {
			return
		}
		snapshot = &session.ChaosState{
			Active:        s.Chaos.Active,
			StepsRun:      s.Chaos.StepsRun,
			StartedAt:     s.Chaos.StartedAt,
			StoppedAt:     s.Chaos.StoppedAt,
			MaxSteps:      s.Chaos.MaxSteps,
			TimeBudget:    s.Chaos.TimeBudget,
			Transitions:   s.Chaos.Transitions,
			UniqueScreens: s.Chaos.UniqueScreens,
			UniqueInputs:  s.Chaos.UniqueInputs,
			LoadedRunID:   s.Chaos.LoadedRunID,
			// Shared, not copied: the bytes are written once when a run
			// ends and replaced wholesale, never edited in place, and this
			// snapshot is taken on every status poll.
			MindMap:             s.Chaos.MindMap,
			Error:               s.Chaos.Error,
			TerminationReason:   s.Chaos.TerminationReason,
			SaturatedNoProgress: s.Chaos.SaturatedNoProgress,
			TargetHost:          s.Chaos.TargetHost,
			FirstScreenHash:     s.Chaos.FirstScreenHash,
			Config:              s.Chaos.Config.Clone(),
		}
		if len(s.Chaos.AIDKeyCounts) > 0 {
			snapshot.AIDKeyCounts = make(map[string]int, len(s.Chaos.AIDKeyCounts))
			for k, v := range s.Chaos.AIDKeyCounts {
				snapshot.AIDKeyCounts[k] = v
			}
		}
		if s.Chaos.LastAttempt != nil {
			last := cloneSessionChaosAttempt(*s.Chaos.LastAttempt)
			snapshot.LastAttempt = &last
		}
		if len(s.Chaos.RecentAttempts) > 0 {
			snapshot.RecentAttempts = make([]session.ChaosAttempt, 0, len(s.Chaos.RecentAttempts))
			for _, attempt := range s.Chaos.RecentAttempts {
				snapshot.RecentAttempts = append(snapshot.RecentAttempts, cloneSessionChaosAttempt(attempt))
			}
		}
	})
	return snapshot
}

func cloneSessionChaosAttempt(attempt session.ChaosAttempt) session.ChaosAttempt {
	out := attempt
	if len(attempt.FieldWrites) > 0 {
		out.FieldWrites = append([]session.ChaosFieldWrite(nil), attempt.FieldWrites...)
	}
	return out
}

func chaosSeedRunFromWorkflow(workflow *WorkflowConfig) *chaos.SavedRun {
	steps := make([]session.WorkflowStep, 0)
	if workflow != nil && len(workflow.Steps) > 0 {
		steps = append(steps, workflow.Steps...)
	}

	uniqueInputs := make(map[string]bool)
	for _, step := range steps {
		if step.Type != "FillString" {
			continue
		}
		text := strings.TrimSpace(step.Text)
		if text == "" {
			continue
		}
		uniqueInputs[text] = true
	}
	var parameters []session.WorkflowParameter
	if workflow != nil {
		parameters = workflow.Parameters
	}
	mindMap := buildChaosSeedMindMap(steps, parameters)

	return &chaos.SavedRun{
		SavedRunMeta: chaos.SavedRunMeta{
			ID:           "recording-seed-" + chaos.NewRunID(),
			StartedAt:    time.Now(),
			StepsRun:     0,
			Transitions:  0,
			UniqueInputs: len(uniqueInputs),
		},
		ScreenHashes:      map[string]bool{},
		TransitionList:    []chaos.Transition{},
		Steps:             steps,
		WorkflowHeader:    chaosSeedWorkflowHeader(workflow),
		AIDKeyCounts:      map[string]int{},
		UniqueInputValues: uniqueInputs,
		Attempts:          []chaos.Attempt{},
		MindMap:           mindMap,
	}
}

func chaosSeedWorkflowHeader(workflow *WorkflowConfig) *chaos.WorkflowHeader {
	if workflow == nil {
		defaultCfg := chaos.DefaultConfig()
		stepDelaySec := roundDelaySeconds(defaultCfg.StepDelay.Seconds())
		return &chaos.WorkflowHeader{
			EveryStepDelay:  &session.WorkflowDelayRange{Min: stepDelaySec, Max: stepDelaySec},
			RampUpBatchSize: 50,
			RampUpDelay:     1.5,
			EndOfTaskDelay:  &session.WorkflowDelayRange{Min: 60, Max: 120},
		}
	}

	header := &chaos.WorkflowHeader{
		OutputFilePath:  workflow.OutputFilePath,
		RampUpBatchSize: workflow.RampUpBatchSize,
		RampUpDelay:     workflow.RampUpDelay,
	}
	if workflow.EveryStepDelay != nil {
		header.EveryStepDelay = &session.WorkflowDelayRange{
			Min: workflow.EveryStepDelay.Min,
			Max: workflow.EveryStepDelay.Max,
		}
	}
	if workflow.EndOfTaskDelay != nil {
		header.EndOfTaskDelay = &session.WorkflowDelayRange{
			Min: workflow.EndOfTaskDelay.Min,
			Max: workflow.EndOfTaskDelay.Max,
		}
	}

	if header.EveryStepDelay == nil {
		defaultCfg := chaos.DefaultConfig()
		stepDelaySec := roundDelaySeconds(defaultCfg.StepDelay.Seconds())
		header.EveryStepDelay = &session.WorkflowDelayRange{Min: stepDelaySec, Max: stepDelaySec}
	}
	if header.RampUpBatchSize == 0 {
		header.RampUpBatchSize = 50
	}
	if header.RampUpDelay == 0 {
		header.RampUpDelay = 1.5
	}
	if header.EndOfTaskDelay == nil {
		header.EndOfTaskDelay = &session.WorkflowDelayRange{Min: 60, Max: 120}
	}

	return header
}

// buildChaosSeedMindMap turns a recording into a starting mind map: one area
// per screen the recording passed through, with the values it typed recorded
// as known to work.
//
// A recording does not say which of its fields were hidden on the screen, so
// a password typed during recording would arrive here looking like any other
// value and be shown as one. What a recording does carry is used to mark such
// fields hidden, which is all the serializers need to withhold them (see
// chaos_redact.go):
//
//   - a parameter marked Sensitive names the row and column it fills;
//   - a fill whose text is a ${variable} set by a Sensitive SetVariable step
//     is a fill from a field the recording itself said not to write down.
func buildChaosSeedMindMap(steps []session.WorkflowStep, parameters []session.WorkflowParameter) *chaos.MindMap {
	mindMap := &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{}}
	if len(steps) == 0 {
		return mindMap
	}
	type position struct{ row, column int }
	sensitiveAt := make(map[position]bool)
	for _, p := range parameters {
		if p.Sensitive && p.Row > 0 && p.Column > 0 {
			sensitiveAt[position{p.Row, p.Column}] = true
		}
	}
	sensitiveVars := make(map[string]bool)
	for _, step := range steps {
		if step.Sensitive && strings.TrimSpace(step.Variable) != "" {
			sensitiveVars[strings.TrimSpace(step.Variable)] = true
		}
	}
	fillIsSensitive := func(row, column int, text string) bool {
		if sensitiveAt[position{row, column}] {
			return true
		}
		for name := range sensitiveVars {
			if strings.Contains(text, "${"+name+"}") {
				return true
			}
		}
		return false
	}
	now := time.Now().UTC()
	areaSeq := 1
	currentAreaID := fmt.Sprintf("recording:area-%d", areaSeq)
	syntheticRow := 1
	syntheticCol := 1

	ensureArea := func(areaID string) *chaos.MindMapArea {
		if existing, ok := mindMap.Areas[areaID]; ok && existing != nil {
			if existing.Hash == "" {
				existing.Hash = areaID
			}
			return existing
		}
		area := &chaos.MindMapArea{
			Hash:               areaID,
			Label:              fmt.Sprintf("Recording Area %d", areaSeq),
			FirstSeen:          now,
			LastSeen:           now,
			FieldMetadata:      map[string]chaos.MindMapFieldMetadata{},
			KnownWorkingValues: map[string][]string{},
			KeyPresses:         map[string]*chaos.MindMapKeyPress{},
		}
		mindMap.Areas[areaID] = area
		return area
	}

	appendUniqueLimited := func(values []string, candidate string, max int) []string {
		for _, existing := range values {
			if existing == candidate {
				return values
			}
		}
		if max > 0 && len(values) >= max {
			return values
		}
		return append(values, candidate)
	}

	for _, step := range steps {
		stepType := strings.TrimSpace(step.Type)
		if stepType == "" || strings.EqualFold(stepType, "Connect") || strings.EqualFold(stepType, "Disconnect") {
			continue
		}
		area := ensureArea(currentAreaID)
		area.Visits++
		area.LastSeen = now

		if strings.EqualFold(stepType, "FillString") {
			text := strings.TrimSpace(step.Text)
			if text == "" {
				continue
			}
			row := syntheticRow
			col := syntheticCol
			length := len([]rune(text))
			if step.Coordinates != nil {
				if step.Coordinates.Row > 0 {
					row = step.Coordinates.Row
				}
				if step.Coordinates.Column > 0 {
					col = step.Coordinates.Column
				}
				if step.Coordinates.Length > 0 {
					length = step.Coordinates.Length
				}
			}
			if length <= 0 {
				length = 1
			}
			fieldKey := fmt.Sprintf("R%dC%dL%d", row, col, length)
			hidden := fillIsSensitive(row, col, text)
			if existing, ok := area.FieldMetadata[fieldKey]; ok && existing.Hidden {
				hidden = true
			}
			area.FieldMetadata[fieldKey] = chaos.MindMapFieldMetadata{
				Row:    row,
				Column: col,
				Length: length,
				Hidden: hidden,
			}
			if hidden {
				area.HiddenFieldCount = 0
				for _, meta := range area.FieldMetadata {
					if meta.Hidden {
						area.HiddenFieldCount++
					}
				}
			}
			area.KnownWorkingValues[fieldKey] = appendUniqueLimited(area.KnownWorkingValues[fieldKey], text, 12)
			area.InputFieldCount = len(area.FieldMetadata)
			syntheticRow++
			if syntheticRow > 24 {
				syntheticRow = 1
				syntheticCol++
			}
			continue
		}

		aidKey, ok := workflowKeyForStepType(stepType)
		if !ok {
			aidKey = stepType
		}
		if strings.TrimSpace(aidKey) == "" {
			continue
		}
		keyPress := area.KeyPresses[aidKey]
		if keyPress == nil {
			keyPress = &chaos.MindMapKeyPress{Destinations: map[string]int{}}
			area.KeyPresses[aidKey] = keyPress
		}
		keyPress.Presses++
		keyPress.Progressions++
		keyPress.LastUsedAt = now

		areaSeq++
		nextAreaID := fmt.Sprintf("recording:area-%d", areaSeq)
		keyPress.Destinations[nextAreaID]++
		currentAreaID = nextAreaID
		_ = ensureArea(currentAreaID)
	}

	return mindMap
}

// defaultChaosOutputName is used when a caller supplies a name with no usable
// characters left after sanitising. It must itself be a valid safepath name.
const defaultChaosOutputName = "chaos-workflow.json"

// confineChaosPaths rewrites the engine's two writable paths so they land
// inside the chaos runs directory, whatever the caller asked for.
//
// Both OutputFile and TransitionLogPath used to be taken as literal
// filesystem paths straight from the request body (and from CHAOS_OUTPUT_FILE
// / CHAOS_TRANSITION_LOG_PATH, which the settings API can write). The engine
// calls os.MkdirAll on the parent before writing, and the transition log is
// opened O_APPEND, so between them they were an arbitrary file create and an
// arbitrary append for anyone who could reach POST /chaos/start.
//
// These are now names, not paths: a caller chooses what the file is called and
// the server decides where it goes, which is the same rule the IND$FILE
// transfer code follows. Operators who need the artefacts elsewhere should
// mount the runs directory where they want it rather than pointing the app at
// an arbitrary location.
func (app *App) confineChaosPaths(cfg *chaos.Config, loadedWorkflowFileName, ownerID string) {
	runsDir := app.chaosRunsDirForSession(ownerID)
	cfg.OutputFile = confineChaosFilePath(runsDir, cfg.OutputFile, loadedWorkflowFileName)
	cfg.TransitionLogPath = confineChaosFilePath(runsDir, cfg.TransitionLogPath, "")
}

// confineChaosFilePath resolves requested to a file inside baseDir.
//
// An empty request stays empty — that is how the engine is told not to write
// the artefact at all, and turning "off" into a default path would create
// files nobody asked for.
func confineChaosFilePath(baseDir, requested, loadedWorkflowFileName string) string {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return ""
	}
	if strings.TrimSpace(baseDir) == "" {
		// No runs directory configured means there is nowhere safe to put the
		// file, so decline to write rather than falling back to the caller's path.
		return ""
	}

	name := safepath.SanitizeName(requested, defaultChaosOutputName)
	name = avoidWorkflowNameCollision(name, loadedWorkflowFileName)

	path, err := safepath.Join(baseDir, name)
	if err != nil {
		// SanitizeName guarantees a valid name, so this is unreachable short of
		// a bug in safepath. Fail closed.
		return ""
	}
	return path
}

// avoidWorkflowNameCollision keeps a chaos export from overwriting the
// workflow file the session already has loaded, by appending "-chaos" to the
// stem when the two names match.
func avoidWorkflowNameCollision(name, loadedWorkflowFileName string) string {
	loadedWorkflowFileName = strings.TrimSpace(loadedWorkflowFileName)
	if loadedWorkflowFileName == "" {
		return name
	}
	workflowBase := filepath.Base(filepath.Clean(loadedWorkflowFileName))
	if workflowBase == "" || !strings.EqualFold(name, workflowBase) {
		return name
	}

	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if strings.HasSuffix(strings.ToLower(stem), "-chaos") {
		return name
	}
	if stem == "" {
		stem = "chaos-workflow"
	}
	if ext == "" {
		ext = ".json"
	}
	return stem + "-chaos" + ext
}
