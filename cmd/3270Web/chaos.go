package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/session"
)

// chaosEngineStore maps session IDs to their running chaos engines. It lives
// outside App so that it can be initialised once and does not need a pointer
// receiver change on App.
type chaosEngineStore struct {
	mu         sync.Mutex
	engines    map[string]*chaos.Engine
	loadedRuns map[string]*chaos.SavedRun
	removed    map[string]bool
}

func newChaosEngineStore() *chaosEngineStore {
	return &chaosEngineStore{
		engines:    make(map[string]*chaos.Engine),
		loadedRuns: make(map[string]*chaos.SavedRun),
		removed:    make(map[string]bool),
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

func (s *chaosEngineStore) delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.engines, sessionID)
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
type chaosStartRequest struct {
	MaxSteps                int            `json:"maxSteps"`
	TimeBudgetSec           float64        `json:"timeBudgetSec"`
	StepDelaySec            float64        `json:"stepDelaySec"`
	Seed                    int64          `json:"seed"`
	AIDKeyWeights           map[string]int `json:"aidKeyWeights"`
	OutputFile              string         `json:"outputFile"`
	MaxFieldLength          int            `json:"maxFieldLength"`
	Hints                   []chaos.Hint   `json:"hints"`
	ExcludeNoProgressEvents *bool          `json:"excludeNoProgressEvents"`
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
	var req chaosStartRequest
	if err := c.ShouldBindJSON(&req); err == nil {
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
		if req.OutputFile != "" {
			cfg.OutputFile = req.OutputFile
		}
		if req.MaxFieldLength > 0 {
			cfg.MaxFieldLength = req.MaxFieldLength
		}
		if len(req.Hints) > 0 {
			cfg.Hints = sanitizeChaosHints(req.Hints)
		}
		if req.ExcludeNoProgressEvents != nil {
			cfg.ExcludeNoProgressEvents = *req.ExcludeNoProgressEvents
		}
	}
	if len(cfg.Hints) == 0 {
		if savedHints, err := app.loadChaosHints(); err == nil && len(savedHints) > 0 {
			cfg.Hints = savedHints
		}
	}
	cfg.OutputFile = safeChaosOutputFilePath(cfg.OutputFile, loadedWorkflowName(s))

	// Reject if an engine is already running for this session.
	if existing, ok := app.chaosEngines.get(s.ID); ok {
		if existing.Status().Active {
			c.JSON(http.StatusConflict, gin.H{"error": "chaos exploration is already running"})
			return
		}
	}

	var h interface{ IsConnected() bool }
	withSessionLock(s, func() { h = s.Host })
	if h == nil || !h.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not connected to host"})
		return
	}

	// Build engine with session host.
	var eng *chaos.Engine
	withSessionLock(s, func() {
		eng = chaos.New(s.Host, cfg)
	})

	if err := eng.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start: %v", err)})
		return
	}

	app.chaosEngines.set(s.ID, eng)

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
		})
		return
	}

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
			if resp["stepsRun"] == 0 && loaded.StepsRun > 0 {
				resp["stepsRun"] = loaded.StepsRun
			}
			if resp["transitions"] == 0 && loaded.Transitions > 0 {
				resp["transitions"] = loaded.Transitions
			}
			if resp["uniqueScreens"] == 0 && loaded.UniqueScreens > 0 {
				resp["uniqueScreens"] = loaded.UniqueScreens
			}
			if resp["uniqueInputs"] == 0 && loaded.UniqueInputs > 0 {
				resp["uniqueInputs"] = loaded.UniqueInputs
			}
			if _, hasMindMap := resp["mindMap"]; !hasMindMap {
				if mindMapJSON := chaosMindMapToJSON(loaded.MindMap); mindMapJSON != nil {
					resp["mindMap"] = mindMapJSON
				}
			}
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	st := eng.Status()
	resp := chaosStatusToJSON(st)
	c.JSON(http.StatusOK, resp)
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
	if eng, ok := app.chaosEngines.get(s.ID); ok {
		var err error
		data, err = eng.ExportWorkflow(targetHost, targetPort)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else if run, ok := app.chaosEngines.getLoadedRun(s.ID); ok {
		var err error
		data, err = marshalWorkflowExport(targetHost, targetPort, run.Steps)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else if run := app.loadSessionChaosRunFromDisk(s); run != nil {
		app.chaosEngines.setLoadedRun(s.ID, run)
		var err error
		data, err = marshalWorkflowExport(targetHost, targetPort, run.Steps)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos run data for this session"})
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
			snapshot := eng.Snapshot(runID)
			app.chaosEngines.setLoadedRun(s.ID, snapshot)
			withSessionLock(s, func() {
				if s.Chaos != nil {
					s.Chaos.LoadedRunID = runID
				}
			})
			// Auto-save the completed run if a runs directory is configured.
			if app.chaosRunsDir != "" {
				if saveErr := chaos.SaveRun(app.chaosRunsDir, snapshot); saveErr != nil {
					// Non-fatal: log but do not interrupt teardown.
					_ = saveErr
				}
			}
			app.chaosEngines.delete(s.ID)
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
	c.JSON(http.StatusOK, gin.H{
		"runID":         run.ID,
		"stepsRun":      run.StepsRun,
		"transitions":   run.Transitions,
		"uniqueScreens": run.UniqueScreens,
		"uniqueInputs":  run.UniqueInputs,
		"mindMap":       chaosMindMapToJSON(run.MindMap),
	})
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

	c.JSON(http.StatusOK, gin.H{
		"status":        "loaded",
		"source":        "recording",
		"runID":         run.ID,
		"stepsSeeded":   len(run.Steps),
		"stepsRun":      run.StepsRun,
		"transitions":   run.Transitions,
		"uniqueScreens": run.UniqueScreens,
		"uniqueInputs":  run.UniqueInputs,
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
		if req.OutputFile != "" {
			cfg.OutputFile = req.OutputFile
		}
		if req.MaxFieldLength > 0 {
			cfg.MaxFieldLength = req.MaxFieldLength
		}
		if len(req.Hints) > 0 {
			cfg.Hints = sanitizeChaosHints(req.Hints)
		}
		if req.ExcludeNoProgressEvents != nil {
			cfg.ExcludeNoProgressEvents = *req.ExcludeNoProgressEvents
		}
	}
	if len(cfg.Hints) == 0 {
		if savedHints, err := app.loadChaosHints(); err == nil && len(savedHints) > 0 {
			cfg.Hints = savedHints
		}
	}
	cfg.OutputFile = safeChaosOutputFilePath(cfg.OutputFile, loadedWorkflowName(s))

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
	Hints []chaos.Hint `json:"hints"`
}

type chaosHintsExtractResponse struct {
	Source string       `json:"source"`
	Hints  []chaos.Hint `json:"hints"`
}

// ChaosHintsGetHandler handles GET /chaos/hints – returns saved chaos hints.
func (app *App) ChaosHintsGetHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	hints, err := app.loadChaosHints()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"hints": hints})
}

// ChaosHintsSaveHandler handles POST /chaos/hints – persists chaos hint data.
func (app *App) ChaosHintsSaveHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var req chaosHintsPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	hints := sanitizeChaosHints(req.Hints)
	if err := app.saveChaosHints(hints); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "saved",
		"hints":  hints,
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
		if tx == "" && len(known) == 0 {
			continue
		}
		out = append(out, chaos.Hint{
			Transaction: tx,
			KnownData:   known,
		})
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
	if app == nil || strings.TrimSpace(app.chaosHintsPath) == "" {
		return []chaos.Hint{}, nil
	}
	app.chaosHintsMu.Lock()
	defer app.chaosHintsMu.Unlock()
	data, err := os.ReadFile(app.chaosHintsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []chaos.Hint{}, nil
		}
		return nil, fmt.Errorf("read chaos hints: %w", err)
	}
	var payload chaosHintsPayload
	if err := json.Unmarshal(data, &payload); err == nil {
		return sanitizeChaosHints(payload.Hints), nil
	}
	var hints []chaos.Hint
	if err := json.Unmarshal(data, &hints); err != nil {
		return nil, fmt.Errorf("parse chaos hints: %w", err)
	}
	return sanitizeChaosHints(hints), nil
}

func (app *App) saveChaosHints(hints []chaos.Hint) error {
	if app == nil || strings.TrimSpace(app.chaosHintsPath) == "" {
		return fmt.Errorf("chaos hints path not configured")
	}
	app.chaosHintsMu.Lock()
	defer app.chaosHintsMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(app.chaosHintsPath), 0750); err != nil {
		return fmt.Errorf("create chaos hints directory: %w", err)
	}
	payload := chaosHintsPayload{Hints: sanitizeChaosHints(hints)}
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

func marshalWorkflowExport(hostName string, port int, steps []session.WorkflowStep) ([]byte, error) {
	export := struct {
		Host  string                 `json:"Host"`
		Port  int                    `json:"Port"`
		Steps []session.WorkflowStep `json:"Steps"`
	}{
		Host:  hostName,
		Port:  port,
		Steps: steps,
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
		AIDKeyCounts:      map[string]int{},
		UniqueInputValues: uniqueInputs,
		Attempts:          []chaos.Attempt{},
		MindMap:           mindMap,
	}
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
