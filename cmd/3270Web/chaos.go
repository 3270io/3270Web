package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	app.chaosEngines.clearRemoved(s.ID)
	go app.syncChaosStatus(s, eng)

	c.JSON(http.StatusOK, gin.H{
		"status":      "resumed",
		"loadedRunID": loaded.ID,
	})
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
	if state.Error != "" {
		resp["error"] = state.Error
	}
	return resp
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
