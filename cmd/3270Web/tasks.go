package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/task"
)

// Guided Business Tasks — the run surface.
//
// A task is a recorded flow with named inputs and named outputs, so someone
// who knows the business question but not the application can get an answer
// without reading a green screen. See internal/task for the model and the
// runner; this file is the HTTP layer and the per-session run state that
// lets the browser show progress and cancel.

// maxConcurrentTaskRuns is one per session, not a number. A task drives the
// one terminal that session owns; two at once would interleave keystrokes
// into the same screen buffer and produce a result that is nonsense from
// both.
const maxTaskRunDuration = 5 * time.Minute

// taskRun is the live state of one in-flight run, kept so /tasks/status can
// report progress and /tasks/cancel can stop it.
type taskRun struct {
	Task      string
	StartedAt time.Time
	Total     int
	Step      int
	StepLabel string
	cancel    context.CancelFunc
	done      bool
	result    *task.Result
	err       string
}

// taskRunStore holds the in-flight run per session ID.
//
// Keyed by session rather than global: sessions are independent terminals,
// and one operator's balance enquiry must not block another's.
type taskRunStore struct {
	mu   sync.Mutex
	runs map[string]*taskRun
}

func newTaskRunStore() *taskRunStore {
	return &taskRunStore{runs: make(map[string]*taskRun)}
}

func (s *taskRunStore) begin(sessionID string, run *taskRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.runs[sessionID]; existing != nil && !existing.done {
		return fmt.Errorf("a task is already running in this session")
	}
	s.runs[sessionID] = run
	return nil
}

// snapshot returns a copy of the run's observable state, taken under the
// lock. Handlers must not read a *taskRun directly: the goroutine driving the
// run mutates done/result/err, so reading those fields outside the lock is a
// data race, and one that would only ever show up under load.
func (s *taskRunStore) snapshot(sessionID string) (taskRun, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[sessionID]
	if run == nil {
		return taskRun{}, false
	}
	return *run, true
}

// cancel stops an in-flight run, reporting whether there was one to stop.
// The done check and the cancel happen under the same lock, so a run that
// finishes concurrently cannot be reported as cancelled.
func (s *taskRunStore) cancel(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[sessionID]
	if run == nil || run.done || run.cancel == nil {
		return false
	}
	run.cancel()
	return true
}

func (s *taskRunStore) update(sessionID string, fn func(*taskRun)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run := s.runs[sessionID]; run != nil {
		fn(run)
	}
}

func (s *taskRunStore) forget(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, sessionID)
}

// taskStore lazily opens the catalogue, beside the connection profiles.
func (app *App) taskStore() *task.Store {
	app.taskStoreOnce.Do(func() {
		base := strings.TrimSpace(app.baseDir)
		if base == "" {
			base = resolveBaseDir()
		}
		app.tasks = task.NewStore(base)
	})
	return app.tasks
}

func (app *App) taskRuns() *taskRunStore {
	app.taskRunsOnce.Do(func() {
		app.taskRunStore = newTaskRunStore()
	})
	return app.taskRunStore
}

/* ---------------------------------------------------------------- */
/* Catalogue                                                         */
/* ---------------------------------------------------------------- */

// TasksListHandler returns the catalogue. Sensitive parameter defaults never
// exist (Validate rejects them), so the whole task is safe to send.
func (app *App) TasksListHandler(c *gin.Context) {
	tasks, err := app.taskStore().List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not read the task catalogue: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

func (app *App) TasksSaveHandler(c *gin.Context) {
	var t task.Task
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	tasks, err := app.taskStore().Upsert(t)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "tasks": tasks})
}

func (app *App) TasksDeleteHandler(c *gin.Context) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a task name is required"})
		return
	}
	tasks, err := app.taskStore().Delete(name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not update the task catalogue: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "tasks": tasks})
}

// TasksDraftHandler turns the session's recording into a proposed task.
//
// It proposes and explains; it does not decide. Which recorded values are
// inputs rather than fixed choices, and which part of the final screen is the
// answer, are judgements about the business — so the draft comes back with
// every assumption listed as a note and with no outputs at all, for a human to
// complete before saving.
func (app *App) TasksDraftHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}

	var steps []task.RecordedStep
	var screens []task.RecordedScreen
	var recordingActive bool
	withSessionLock(s, func() {
		if s.Recording == nil {
			return
		}
		recordingActive = s.Recording.Active
		for _, step := range s.Recording.Steps {
			rec := task.RecordedStep{Type: step.Type, Text: step.Text}
			if step.Coordinates != nil {
				rec.Row = step.Coordinates.Row
				rec.Column = step.Coordinates.Column
				rec.Length = step.Coordinates.Length
			}
			steps = append(steps, rec)
		}
		for _, screen := range s.Recording.Screens {
			screens = append(screens, task.RecordedScreen{StepIndex: screen.StepIndex, Text: screen.Text})
		}
	})

	if len(steps) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "there is no recording to build a task from — record a flow first",
		})
		return
	}

	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		name = "New task"
	}
	// The screen the flow ended on comes from the live session, not the
	// recording: the recorder captures each screen before its submit, so the
	// screen the last key produced — the one holding the answer — is never in
	// it. This is the screen the wizard offers for picking outputs.
	finalScreen := ""
	if h := app.sessionHost(s); h != nil {
		if screen := h.GetScreen(); screen != nil {
			finalScreen = screen.Text()
		}
	}
	draft, err := task.DraftFromRecording(name, steps, screens, finalScreen)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if recordingActive {
		// A draft from a running recording is a snapshot of a flow that is
		// still being performed, which is rarely what someone means.
		draft.Notes = append(draft.Notes,
			"Recording is still running, so this draft covers only what has been done so far. Stop the recording for the complete flow.")
	}
	c.JSON(http.StatusOK, draft)
}

/* ---------------------------------------------------------------- */
/* Running                                                           */
/* ---------------------------------------------------------------- */

// TasksRunHandler starts a task in the caller's session and returns
// immediately. The run is asynchronous because a 3270 transaction takes
// seconds and the browser has to be able to show progress and offer Cancel
// while it happens — the audit is explicit that hiding the terminal during a
// run would be a mistake, and a blocking request would do exactly that.
func (app *App) TasksRunHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	h := app.sessionHost(s)
	if h == nil || !h.IsConnected() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "connect to a host before running a task"})
		return
	}

	var payload struct {
		Name       string            `json:"name"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	t, ok := app.taskStore().Find(payload.Name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("there is no task called %q", payload.Name)})
		return
	}
	// Resolve up front so a bad form comes back as a 400 the form can show
	// against its fields, rather than as an asynchronous failure the
	// operator has to go and look for.
	if _, err := t.ResolveParameters(payload.Parameters); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), maxTaskRunDuration)
	run := &taskRun{
		Task:      t.Name,
		StartedAt: time.Now(),
		Total:     len(t.Steps),
		cancel:    cancel,
	}
	if err := app.taskRuns().begin(s.ID, run); err != nil {
		cancel()
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	sessionID := s.ID
	runner := &task.Runner{Terminal: h}
	go func() {
		defer cancel()
		result, err := runner.Run(ctx, t, payload.Parameters)
		app.taskRuns().update(sessionID, func(r *taskRun) {
			r.done = true
			r.result = result
			if err != nil {
				r.err = err.Error()
			}
		})
	}()

	c.JSON(http.StatusAccepted, gin.H{"ok": true, "task": t.Name, "steps": len(t.Steps)})
}

// TasksStatusHandler reports the in-flight or just-finished run.
func (app *App) TasksStatusHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	run, ok := app.taskRuns().snapshot(s.ID)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"running": false})
		return
	}
	body := gin.H{
		"running": !run.done,
		"task":    run.Task,
		"steps":   run.Total,
	}
	if run.done {
		if run.err != "" {
			body["error"] = run.err
		}
		if run.result != nil {
			body["result"] = run.result
		}
	}
	c.JSON(http.StatusOK, body)
}

// TasksCancelHandler stops an in-flight run. The terminal is left exactly
// where the run stopped — the operator asked for the automation to stop, not
// for it to be undone, and a 3270 transaction cannot be rolled back anyway.
func (app *App) TasksCancelHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	if !app.taskRuns().cancel(s.ID) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "running": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
