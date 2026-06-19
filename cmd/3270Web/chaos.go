package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
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
}

func newChaosEngineStore() *chaosEngineStore {
	return &chaosEngineStore{
		engines:     make(map[string]*chaos.Engine),
		loadedRuns:  make(map[string]*chaos.SavedRun),
		screenHints: make(map[string]map[string]chaos.ScreenHint),
		removed:     make(map[string]bool),
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
	if existing, ok := s.engines[sessionID]; ok && existing.Status().Active {
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
// the session's loaded run, and removes the engine from the store. Holding
// the store mutex across all three steps means writes routed through
// withEngine/withLoadedRun land either in the engine before the snapshot or
// in the loaded run after it — never in the gap, where they would be lost.
// Returns nil if no engine is registered for the session.
func (s *chaosEngineStore) completeEngine(sessionID, runID string) *chaos.SavedRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	eng, ok := s.engines[sessionID]
	if !ok || eng == nil {
		return nil
	}
	snapshot := eng.Snapshot(runID)
	s.loadedRuns[sessionID] = snapshot
	delete(s.engines, sessionID)
	return snapshot
}

func (s *chaosEngineStore) getScreenHints(sessionID string) map[string]chaos.ScreenHint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneChaosScreenHintsMap(s.screenHints[sessionID])
}

func (s *chaosEngineStore) setScreenHints(sessionID string, hints map[string]chaos.ScreenHint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(hints) == 0 {
		delete(s.screenHints, sessionID)
		return
	}
	s.screenHints[sessionID] = cloneChaosScreenHintsMap(hints)
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

func (s *chaosEngineStore) getLoadedRun(sessionID string) (*chaos.SavedRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.loadedRuns[sessionID]
	return r, ok
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
	LearnedInputReuseBias       *float64          `json:"learned_input_reuse_bias"`
	LearnedKeyReuseBias         *float64          `json:"learned_key_reuse_bias"`
	LearnedReuseBias            *float64          `json:"learned_reuse_bias"` // legacy alias: applies to both if specific values not provided
	ForceOverrideExistingInputs *bool             `json:"force_override_existing_inputs"`
	Hints                       []chaos.Hint      `json:"hints"`
	FirstScreenHint             *chaos.ScreenHint `json:"first_screen_hint,omitempty"`
	ExcludeNoProgressEvents     *bool             `json:"exclude_no_progress_events"`
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
		LearnedInputReuseBias       *float64          `json:"learned_input_reuse_bias"`
		LearnedKeyReuseBias         *float64          `json:"learned_key_reuse_bias"`
		LearnedReuseBias            *float64          `json:"learned_reuse_bias"`
		ForceOverrideExistingInputs *bool             `json:"force_override_existing_inputs"`
		Hints                       []chaos.Hint      `json:"hints"`
		FirstScreenHint             *chaos.ScreenHint `json:"first_screen_hint,omitempty"`
		ExcludeNoProgressEvents     *bool             `json:"exclude_no_progress_events"`
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
		LearnedInputReuseBias       *float64          `json:"learnedInputReuseBias"`
		LearnedKeyReuseBias         *float64          `json:"learnedKeyReuseBias"`
		LearnedReuseBias            *float64          `json:"learnedReuseBias"`
		ForceOverrideExistingInputs *bool             `json:"forceOverrideExistingInputs"`
		FirstScreenHint             *chaos.ScreenHint `json:"firstScreenHint,omitempty"`
		ExcludeNoProgressEvents     *bool             `json:"excludeNoProgressEvents"`
		ExtendLimits                bool              `json:"extendLimits"`
	}
	_ = json.Unmarshal(data, &camel)

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
}

// ChaosStartHandler handles POST /chaos/start.
func (app *App) ChaosStartHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	app.chaosEngines.clearRemoved(s.ID)

	// Parse optional body; fall back to defaults if empty.
	cfg := chaos.DefaultConfig()
	app.applyChaosEnvSettings(&cfg)
	var req chaosStartRequest
	if err := c.ShouldBindJSON(&req); err == nil {
		applyChaosStartRequestToConfig(&cfg, req)
	}
	var savedHints chaosHintsPayload
	if saved, err := app.loadChaosHintsPayload(); err == nil {
		savedHints = saved
		if len(cfg.Hints) == 0 && len(saved.Hints) > 0 {
			cfg.Hints = saved.Hints
		}
		if len(cfg.KeyBlacklist) == 0 && len(saved.KeyBlacklist) > 0 {
			cfg.KeyBlacklist = append([]string(nil), saved.KeyBlacklist...)
		}
	}
	cfg.ScreenHints = app.resolveChaosScreenHints(s.ID, req, savedHints)
	cfg.OutputFile = safeChaosOutputFilePath(cfg.OutputFile, loadedWorkflowName(s))
	withSessionLock(s, func() {
		cfg.ExportHost = s.TargetHost
		cfg.ExportPort = s.TargetPort
	})

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
	go app.syncChaosStatus(s, eng)

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
	if !ok || !eng.Status().Active {
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

	if eng, ok := app.chaosEngines.get(s.ID); ok && eng.Status().Active {
		c.JSON(http.StatusConflict, gin.H{"error": "chaos exploration is running; stop it before removing"})
		return
	}

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
		c.JSON(http.StatusOK, gin.H{
			"active":        false,
			"stepsRun":      0,
			"transitions":   0,
			"uniqueScreens": 0,
			"uniqueInputs":  0,
			"screenHints":   app.chaosEngines.getScreenHints(s.ID),
		})
		return
	}

	verbose := parseBoolFormValue(c.DefaultQuery("verbose", ""))

	eng, ok := app.chaosEngines.get(s.ID)
	if !ok {
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
		// Include loaded run info if present.
		if loaded, ok2 := app.chaosEngines.getLoadedRun(s.ID); ok2 {
			resp["loadedRunID"] = loaded.ID
			if loaded.StepsRun > 0 {
				if cur, ok := resp["stepsRun"].(int); !ok || loaded.StepsRun > cur {
					resp["stepsRun"] = loaded.StepsRun
				}
			}
			if loaded.Transitions > 0 {
				if cur, ok := resp["transitions"].(int); !ok || loaded.Transitions > cur {
					resp["transitions"] = loaded.Transitions
				}
			}
			if loaded.UniqueScreens > 0 {
				if cur, ok := resp["uniqueScreens"].(int); !ok || loaded.UniqueScreens > cur {
					resp["uniqueScreens"] = loaded.UniqueScreens
				}
			}
			if loaded.UniqueInputs > 0 {
				if cur, ok := resp["uniqueInputs"].(int); !ok || loaded.UniqueInputs > cur {
					resp["uniqueInputs"] = loaded.UniqueInputs
				}
			}
			// Prefer the finalized saved-run mind map over any stale session snapshot.
			if mindMapJSON := chaosMindMapToJSON(loaded.MindMap); mindMapJSON != nil {
				if verbose {
					resp["mindMap"] = mindMapJSON
				} else {
					resp["mindMap"] = slimMindMapForStatus(loaded.MindMap)
				}
			}
			if _, hasFirst := resp["firstScreenHash"]; !hasFirst {
				if firstHash := chaosFirstScreenHashFromAttempts(loaded.Attempts); firstHash != "" {
					resp["firstScreenHash"] = firstHash
				}
			}
		}
		if hints := app.chaosEngines.getScreenHints(s.ID); len(hints) > 0 {
			resp["screenHints"] = hints
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	st := eng.Status()
	resp := chaosStatusToJSON(st)
	if !verbose {
		// Replace the full mind map with a slim summary so /chaos/status
		// stays cheap when polled. Pass ?verbose=true to get the full graph.
		if st.MindMap != nil {
			resp["mindMap"] = slimMindMapForStatus(st.MindMap)
		}
		// Also drop RecentAttempts (only keep LastAttempt) when not verbose.
		delete(resp, "recentAttempts")
	}
	if hints := app.chaosEngines.getScreenHints(s.ID); len(hints) > 0 {
		resp["screenHints"] = hints
	}
	c.JSON(http.StatusOK, resp)
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
// engine's mind map as JSON so it can be imported into a future session.
func (app *App) ChaosMindMapExportHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	eng, ok := app.chaosEngines.get(s.ID)
	if !ok || eng == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos engine for this session"})
		return
	}
	data, err := eng.ExportMindMap()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// ChaosMindMapImportHandler handles POST /chaos/mindmap/import – merges a
// previously exported mind map into the current engine. Rejected while a run
// is active.
func (app *App) ChaosMindMapImportHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var mm chaos.MindMap
	if err := c.ShouldBindJSON(&mm); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: " + err.Error()})
		return
	}
	eng, ok := app.chaosEngines.get(s.ID)
	if !ok || eng == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos engine for this session; start or load a run first"})
		return
	}
	if !eng.ImportMindMap(&mm) {
		c.JSON(http.StatusConflict, gin.H{"error": "cannot import while chaos exploration is running"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "areasImported": len(mm.Areas)})
}

// overriding only fields whose corresponding setting is non-empty. Request body
// overrides applied later in ChaosStartHandler will always take final precedence.
func (app *App) applyChaosEnvSettings(cfg *chaos.Config) {
	if app == nil || cfg == nil {
		return
	}
	settings, _, err := app.settingsSnapshot(true)
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
	settings, _, err := app.settingsSnapshot(true)
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
	if app.chaosRunsDir == "" || s == nil || app.chaosEngines.isRemoved(s.ID) {
		return nil
	}
	snapshot := chaosStateSnapshot(s)
	if snapshot == nil || strings.TrimSpace(snapshot.LoadedRunID) == "" {
		return nil
	}
	runID := strings.TrimSpace(snapshot.LoadedRunID)
	run, err := chaos.LoadRun(app.chaosRunsDir, runID)
	if err != nil {
		return nil
	}
	return run
}

// syncChaosStatus runs in a background goroutine and copies engine status
// snapshots into the session's ChaosState so that the session store always
// reflects the latest values. It removes the engine from the store once the
// run completes to avoid memory growth.
func (app *App) syncChaosStatus(s *session.Session, eng *chaos.Engine) {
	for {
		if app.chaosEngines.isRemoved(s.ID) {
			return
		}
		st := eng.Status()
		if app.chaosEngines.isRemoved(s.ID) {
			return
		}
		withSessionLock(s, func() {
			if app.chaosEngines.isRemoved(s.ID) {
				return
			}
			s.Chaos = &session.ChaosState{
				Active:         st.Active,
				StepsRun:       st.StepsRun,
				StartedAt:      st.StartedAt,
				StoppedAt:      st.StoppedAt,
				MaxSteps:       st.MaxSteps,
				TimeBudget:     st.TimeBudget,
				Transitions:    st.Transitions,
				UniqueScreens:  st.UniqueScreens,
				UniqueInputs:   st.UniqueInputs,
				AIDKeyCounts:   st.AIDKeyCounts,
				LoadedRunID:    st.LoadedRunID,
				LastAttempt:    toSessionChaosAttempt(st.LastAttempt),
				RecentAttempts: toSessionChaosAttempts(st.RecentAttempts),
				MindMap:        marshalChaosMindMap(st.MindMap),
				Error:          st.Error,
			}
		})
		if !st.Active {
			if app.chaosEngines.isRemoved(s.ID) {
				return
			}
			runID := chaos.NewRunID()
			// Snapshot, install as loaded run, and unregister the engine in
			// one atomic store operation so concurrent business writes are
			// never lost between the snapshot and the engine removal.
			snapshot := app.chaosEngines.completeEngine(s.ID, runID)
			if snapshot == nil {
				return
			}
			withSessionLock(s, func() {
				if s.Chaos != nil {
					s.Chaos.LoadedRunID = runID
				}
			})
			// Auto-save the completed run if a runs directory is configured.
			if app.chaosRunsDir != "" {
				if saveErr := chaos.SaveRun(app.chaosRunsDir, snapshot); saveErr != nil {
					// Non-fatal: log but do not interrupt teardown.
					log.Printf("chaos: auto-save run %s failed: %v", runID, saveErr)
				} else if pruned, pruneErr := chaos.PruneRuns(app.chaosRunsDir, maxSavedChaosRuns); pruneErr != nil {
					log.Printf("chaos: prune saved runs failed: %v", pruneErr)
				} else if pruned > 0 {
					log.Printf("chaos: pruned %d old saved run(s), keeping newest %d", pruned, maxSavedChaosRuns)
				}
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// ChaosListRunsHandler handles GET /chaos/runs – returns saved run metadata.
func (app *App) ChaosListRunsHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	metas, err := chaos.ListRuns(app.chaosRunsDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if metas == nil {
		metas = []chaos.SavedRunMeta{}
	}
	c.JSON(http.StatusOK, metas)
}

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

	run, err := chaos.LoadRun(app.chaosRunsDir, req.RunID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	app.chaosEngines.clearRemoved(s.ID)
	app.chaosEngines.setLoadedRun(s.ID, run)
	withSessionLock(s, func() {
		// Loading a chaos run should clear stale active-run status metadata.
		s.Chaos = nil
	})
	c.JSON(http.StatusOK, gin.H{
		"runID":         run.ID,
		"stepsRun":      run.StepsRun,
		"transitions":   run.Transitions,
		"uniqueScreens": run.UniqueScreens,
		"uniqueInputs":  run.UniqueInputs,
		"uniqueKeys":    len(run.AIDKeyCounts),
		"mindMap":       chaosMindMapToJSON(run.MindMap),
	})
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
	if err := chaos.DeleteRun(app.chaosRunsDir, runID); err != nil {
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
	if existing, ok := app.chaosEngines.get(s.ID); ok && existing.Status().Active {
		c.JSON(http.StatusConflict, gin.H{"error": "chaos exploration is already running"})
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
	app.chaosEngines.clearRemoved(s.ID)
	app.chaosEngines.setLoadedRun(s.ID, run)
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
		"mindMap":       chaosMindMapToJSON(run.MindMap),
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

	// Reject if an engine is already running for this session.
	if existing, ok2 := app.chaosEngines.get(s.ID); ok2 && existing.Status().Active {
		c.JSON(http.StatusConflict, gin.H{"error": "chaos exploration is already running"})
		return
	}

	var h interface{ IsConnected() bool }
	withSessionLock(s, func() { h = s.Host })
	if h == nil || !h.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not connected to host"})
		return
	}

	// Parse optional config overrides (same fields as /chaos/start).
	cfg := chaos.DefaultConfig()
	var req chaosStartRequest
	if err := c.ShouldBindJSON(&req); err == nil {
		applyChaosStartRequestToConfig(&cfg, req)
	}
	var savedHints chaosHintsPayload
	if saved, err := app.loadChaosHintsPayload(); err == nil {
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
	cfg.OutputFile = safeChaosOutputFilePath(cfg.OutputFile, loadedWorkflowName(s))
	withSessionLock(s, func() {
		cfg.ExportHost = s.TargetHost
		cfg.ExportPort = s.TargetPort
	})

	var eng *chaos.Engine
	withSessionLock(s, func() {
		eng = chaos.New(s.Host, cfg)
	})

	if err := eng.Resume(loaded); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to resume: %v", err)})
		return
	}

	app.chaosEngines.set(s.ID, eng)
	app.chaosEngines.clearRemoved(s.ID)
	go app.syncChaosStatus(s, eng)

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
	payload, err := app.loadChaosHintsPayload()
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
	if err := app.saveChaosHintsPayload(payload); err != nil {
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
	normalized := normalizeKey(trimmed)
	if normalized != "Enter" || strings.EqualFold(trimmed, "Enter") {
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

func (app *App) loadChaosHints() ([]chaos.Hint, error) {
	payload, err := app.loadChaosHintsPayload()
	if err != nil {
		return nil, err
	}
	return payload.Hints, nil
}

func (app *App) loadChaosHintsPayload() (chaosHintsPayload, error) {
	if app == nil || strings.TrimSpace(app.chaosHintsPath) == "" {
		return chaosHintsPayload{Hints: []chaos.Hint{}, KeyBlacklist: []string{}}, nil
	}
	app.chaosHintsMu.Lock()
	defer app.chaosHintsMu.Unlock()
	data, err := os.ReadFile(app.chaosHintsPath)
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

func (app *App) saveChaosHints(hints []chaos.Hint) error {
	return app.saveChaosHintsPayload(chaosHintsPayload{Hints: hints})
}

func (app *App) saveChaosHintsPayload(payload chaosHintsPayload) error {
	if app == nil || strings.TrimSpace(app.chaosHintsPath) == "" {
		return fmt.Errorf("chaos hints path not configured")
	}
	app.chaosHintsMu.Lock()
	defer app.chaosHintsMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(app.chaosHintsPath), 0750); err != nil {
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
	if err := os.WriteFile(app.chaosHintsPath, data, 0600); err != nil {
		return fmt.Errorf("write chaos hints: %w", err)
	}
	return nil
}

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
	if st.LastAttempt != nil {
		resp["lastAttempt"] = chaosAttemptToJSON(*st.LastAttempt)
	}
	if len(st.RecentAttempts) > 0 {
		attempts := make([]gin.H, 0, len(st.RecentAttempts))
		for _, attempt := range st.RecentAttempts {
			attempts = append(attempts, chaosAttemptToJSON(attempt))
		}
		resp["recentAttempts"] = attempts
	}
	if mindMapJSON := chaosMindMapToJSON(st.MindMap); mindMapJSON != nil {
		resp["mindMap"] = mindMapJSON
	}
	// Surface why the run stopped so the UI and Copilot agent can react
	// (e.g. "blocked" -> relax the key blacklist; "saturated" with
	// saturatedNoProgress -> stop resuming and update hints instead).
	if st.TerminationReason != "" {
		resp["terminationReason"] = st.TerminationReason
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
		}
	}
	if st.Error != "" {
		resp["error"] = st.Error
	}
	return resp
}

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
	if firstHash := sessionChaosFirstScreenHashFromAttempts(state.RecentAttempts); firstHash != "" {
		resp["firstScreenHash"] = firstHash
	}
	if !state.StartedAt.IsZero() {
		resp["startedAt"] = state.StartedAt.Format(time.RFC3339)
	}
	if !state.StoppedAt.IsZero() {
		resp["stoppedAt"] = state.StoppedAt.Format(time.RFC3339)
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
	if decoded := rawJSONToInterface(state.MindMap); decoded != nil {
		resp["mindMap"] = decoded
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
			MindMap:       append(json.RawMessage(nil), s.Chaos.MindMap...),
			Error:         s.Chaos.Error,
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

func marshalWorkflowExport(hostName string, port int, steps []session.WorkflowStep, header *chaos.WorkflowHeader, discovery *chaos.WorkflowDiscoveryMetadata) ([]byte, error) {
	type workflowExportWithChaosMetadata struct {
		WorkflowConfig
		ChaosDiscovery *chaos.WorkflowDiscoveryMetadata `json:"ChaosDiscovery,omitempty"`
	}
	export := workflowExportWithChaosMetadata{
		WorkflowConfig: WorkflowConfig{
			Host:  hostName,
			Port:  port,
			Steps: steps,
		},
		ChaosDiscovery: discovery,
	}
	if header == nil {
		header = chaosSeedWorkflowHeader(nil)
	}
	if header != nil {
		if header.EveryStepDelay != nil {
			export.WorkflowConfig.EveryStepDelay = &session.WorkflowDelayRange{
				Min: header.EveryStepDelay.Min,
				Max: header.EveryStepDelay.Max,
			}
		}
		if header.EndOfTaskDelay != nil {
			export.WorkflowConfig.EndOfTaskDelay = &session.WorkflowDelayRange{
				Min: header.EndOfTaskDelay.Min,
				Max: header.EndOfTaskDelay.Max,
			}
		}
		export.WorkflowConfig.OutputFilePath = header.OutputFilePath
		export.WorkflowConfig.RampUpBatchSize = header.RampUpBatchSize
		export.WorkflowConfig.RampUpDelay = header.RampUpDelay
	}
	return json.MarshalIndent(export, "", "  ")
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
	mindMap := buildChaosSeedMindMap(steps)

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

func buildChaosSeedMindMap(steps []session.WorkflowStep) *chaos.MindMap {
	mindMap := &chaos.MindMap{Areas: map[string]*chaos.MindMapArea{}}
	if len(steps) == 0 {
		return mindMap
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
			area.FieldMetadata[fieldKey] = chaos.MindMapFieldMetadata{
				Row:    row,
				Column: col,
				Length: length,
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

func safeChaosOutputFilePath(outputPath, loadedWorkflowFileName string) string {
	outputPath = strings.TrimSpace(outputPath)
	loadedWorkflowFileName = strings.TrimSpace(loadedWorkflowFileName)
	if outputPath == "" || loadedWorkflowFileName == "" {
		return outputPath
	}

	outputBase := filepath.Base(filepath.Clean(outputPath))
	workflowBase := filepath.Base(filepath.Clean(loadedWorkflowFileName))
	if outputBase == "" || workflowBase == "" {
		return outputPath
	}
	if !strings.EqualFold(outputBase, workflowBase) {
		return outputPath
	}

	ext := filepath.Ext(outputBase)
	stem := strings.TrimSuffix(outputBase, ext)
	if strings.HasSuffix(strings.ToLower(stem), "-chaos") {
		return outputPath
	}
	if stem == "" {
		stem = "chaos-workflow"
	}
	if ext == "" {
		ext = ".json"
	}
	return filepath.Join(filepath.Dir(outputPath), stem+"-chaos"+ext)
}
