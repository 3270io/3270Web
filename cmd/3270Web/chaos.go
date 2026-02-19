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
	mu      sync.Mutex
	engines map[string]*chaos.Engine
}

func newChaosEngineStore() *chaosEngineStore {
	return &chaosEngineStore{engines: make(map[string]*chaos.Engine)}
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

// chaosStartRequest is the JSON body accepted by POST /chaos/start.
type chaosStartRequest struct {
	MaxSteps      int            `json:"maxSteps"`
	TimeBudgetSec float64        `json:"timeBudgetSec"`
	StepDelaySec  float64        `json:"stepDelaySec"`
	Seed          int64          `json:"seed"`
	AIDKeyWeights map[string]int `json:"aidKeyWeights"`
	OutputFile    string         `json:"outputFile"`
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
		c.JSON(http.StatusOK, gin.H{
			"active":      false,
			"stepsRun":    0,
			"transitions": 0,
		})
		return
	}

	st := eng.Status()
	resp := gin.H{
		"active":      st.Active,
		"stepsRun":    st.StepsRun,
		"transitions": st.Transitions,
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
// reflects the latest values.
func (app *App) syncChaosStatus(s *session.Session, eng *chaos.Engine) {
	for {
		st := eng.Status()
		withSessionLock(s, func() {
			s.Chaos = &session.ChaosState{
				Active:      st.Active,
				StepsRun:    st.StepsRun,
				StartedAt:   st.StartedAt,
				StoppedAt:   st.StoppedAt,
				Transitions: st.Transitions,
				Error:       st.Error,
			}
		})
		if !st.Active {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
