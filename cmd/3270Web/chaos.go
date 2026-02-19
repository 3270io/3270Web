package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	mu          sync.Mutex
	engines     map[string]*chaos.Engine
	loadedRuns  map[string]*chaos.SavedRun
}

func newChaosEngineStore() *chaosEngineStore {
	return &chaosEngineStore{
		engines:    make(map[string]*chaos.Engine),
		loadedRuns: make(map[string]*chaos.SavedRun),
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

// chaosStartRequest is the JSON body accepted by POST /chaos/start.
type chaosStartRequest struct {
	MaxSteps       int            `json:"maxSteps"`
	TimeBudgetSec  float64        `json:"timeBudgetSec"`
	StepDelaySec   float64        `json:"stepDelaySec"`
	Seed           int64          `json:"seed"`
	AIDKeyWeights  map[string]int `json:"aidKeyWeights"`
	OutputFile     string         `json:"outputFile"`
	MaxFieldLength int            `json:"maxFieldLength"`
}

// ChaosStartHandler handles POST /chaos/start.
func (app *App) ChaosStartHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

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
	}

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

// ChaosStatusHandler handles GET /chaos/status.
func (app *App) ChaosStatusHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	eng, ok := app.chaosEngines.get(s.ID)
	if !ok {
		resp := gin.H{
			"active":        false,
			"stepsRun":      0,
			"transitions":   0,
			"uniqueScreens": 0,
			"uniqueInputs":  0,
		}
		// Include loaded run info if present.
		if loaded, ok2 := app.chaosEngines.getLoadedRun(s.ID); ok2 {
			resp["loadedRunID"] = loaded.ID
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	st := eng.Status()
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
	if st.Error != "" {
		resp["error"] = st.Error
	}
	c.JSON(http.StatusOK, resp)
}

// ChaosExportHandler handles POST /chaos/export – returns the learned workflow.
func (app *App) ChaosExportHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	eng, ok := app.chaosEngines.get(s.ID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "no chaos engine for this session"})
		return
	}

	var targetHost string
	var targetPort int
	withSessionLock(s, func() {
		targetHost = s.TargetHost
		targetPort = s.TargetPort
	})

	data, err := eng.ExportWorkflow(targetHost, targetPort)
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

// syncChaosStatus runs in a background goroutine and copies engine status
// snapshots into the session's ChaosState so that the session store always
// reflects the latest values. It removes the engine from the store once the
// run completes to avoid memory growth.
func (app *App) syncChaosStatus(s *session.Session, eng *chaos.Engine) {
	for {
		st := eng.Status()
		withSessionLock(s, func() {
			s.Chaos = &session.ChaosState{
				Active:        st.Active,
				StepsRun:      st.StepsRun,
				StartedAt:     st.StartedAt,
				StoppedAt:     st.StoppedAt,
				Transitions:   st.Transitions,
				UniqueScreens: st.UniqueScreens,
				UniqueInputs:  st.UniqueInputs,
				AIDKeyCounts:  st.AIDKeyCounts,
				LoadedRunID:   st.LoadedRunID,
				Error:         st.Error,
			}
		})
		if !st.Active {
			// Auto-save the completed run if a runs directory is configured.
			if app.chaosRunsDir != "" {
				runID := chaos.NewRunID()
				snapshot := eng.Snapshot(runID)
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

	app.chaosEngines.setLoadedRun(s.ID, run)
	c.JSON(http.StatusOK, gin.H{
		"runID":         run.ID,
		"stepsRun":      run.StepsRun,
		"transitions":   run.Transitions,
		"uniqueScreens": run.UniqueScreens,
		"uniqueInputs":  run.UniqueInputs,
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
	}

	var eng *chaos.Engine
	withSessionLock(s, func() {
		eng = chaos.New(s.Host, cfg)
	})

	if err := eng.Resume(loaded); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to resume: %v", err)})
		return
	}

	app.chaosEngines.set(s.ID, eng)
	go app.syncChaosStatus(s, eng)

	c.JSON(http.StatusOK, gin.H{
		"status":      "resumed",
		"loadedRunID": loaded.ID,
	})
}
