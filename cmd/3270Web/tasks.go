// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/session"
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
			base = resolveStateDir()
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

// extensionTasks parses and validates the task documents extensions
// contributed, caching the result alongside the catalogue that produced it.
//
// Validated by task.Validate, the same gate the browser and the importer go
// through: an extension gets no shortcut past the check that a task's steps
// reference parameters that exist and press keys that are real. A document
// that fails is reported on the catalogue's problem list and skipped, in
// keeping with the rest of the loader — one bad pack does not stop the others.
func (app *App) extensionTasks() []task.Task {
	app.extensionTasksOnce.Do(func() {
		for _, doc := range app.skillCatalogue().Tasks() {
			var t task.Task
			if err := json.Unmarshal(doc.Data, &t); err != nil {
				log.Printf("task from %s (%s): not valid JSON: %v", doc.Source, filepath.Base(doc.File), err)
				continue
			}
			if err := t.Validate(); err != nil {
				log.Printf("task from %s (%s): %v", doc.Source, filepath.Base(doc.File), err)
				continue
			}
			// Stamped so a listing can say whose task this is. An extension's
			// contribution reads no differently from a recorded one otherwise,
			// and "where did this come from" is the first question anyone asks
			// about a task that did something surprising.
			t.Source = doc.Source.String()
			app.extensionTaskList = append(app.extensionTaskList, t)
		}
	})
	return app.extensionTaskList
}

// allTasks merges the operator's saved catalogue with extension
// contributions.
//
// The store wins a name collision. The saved catalogue is the one someone on
// this deployment authored and can edit; an extension is content that arrived
// with a folder, and letting it silently replace a task an operator recorded
// would be the wrong way round. The shadowed entry is dropped rather than
// renamed, so the name means one thing everywhere.
func (app *App) allTasks(c *gin.Context) ([]task.Task, error) {
	saved, err := app.taskStoreForRequest(c).List()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(saved))
	for _, t := range saved {
		seen[task.NormalizeName(t.Name)] = true
	}

	out := append([]task.Task(nil), saved...)
	for _, t := range app.extensionTasks() {
		if seen[task.NormalizeName(t.Name)] {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// findTask resolves a task by name across both sources.
func (app *App) findTask(c *gin.Context, name string) (task.Task, bool) {
	if t, ok := app.taskStoreForRequest(c).Find(name); ok {
		return t, true
	}
	want := task.NormalizeName(name)
	for _, t := range app.extensionTasks() {
		if task.NormalizeName(t.Name) == want {
			return t, true
		}
	}
	return task.Task{}, false
}

// isExtensionTask reports whether a name belongs to an extension and not to
// the editable catalogue.
func (app *App) isExtensionTask(c *gin.Context, name string) bool {
	if _, ok := app.taskStoreForRequest(c).Find(name); ok {
		return false
	}
	want := task.NormalizeName(name)
	for _, t := range app.extensionTasks() {
		if task.NormalizeName(t.Name) == want {
			return true
		}
	}
	return false
}

/* ---------------------------------------------------------------- */
/* Catalogue                                                         */
/* ---------------------------------------------------------------- */

// TasksListHandler returns the catalogue. Sensitive parameter defaults never
// exist (Validate rejects them), so the whole task is safe to send.
func (app *App) TasksListHandler(c *gin.Context) {
	tasks, err := app.allTasks(c)
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
	if _, err := app.taskStoreForRequest(c).Upsert(t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// The merged list, not the store's: the browser replaces its menu with
	// what comes back, and returning only the saved half would make every
	// extension task disappear until the next page load.
	tasks, err := app.allTasks(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not read the task catalogue: %v", err)})
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
	// An extension's task is not in the file this writes, so deleting it
	// would report success and change nothing — and the task would still be
	// on the menu after a refresh.
	if app.isExtensionTask(c, name) {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf(
			"%q comes from an installed extension and is not in this catalogue. "+
				"Disable the extension to remove it.", name)})
		return
	}
	if _, err := app.taskStoreForRequest(c).Delete(name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not update the task catalogue: %v", err)})
		return
	}
	tasks, err := app.allTasks(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not read the task catalogue: %v", err)})
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

	// Reopening a saved task goes through the same wizard as building a new
	// one. Anything else means every correction — a mislabelled input, a
	// region a character too short, a guard that turned out to be the date —
	// costs a re-recording of the whole flow, which is how a catalogue ends
	// up full of tasks nobody dares touch.
	if from := strings.TrimSpace(c.Query("from")); from != "" {
		app.draftFromSavedTask(c, s, from)
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
		if screen := h.GetScreenSnapshot(); screen != nil {
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

// draftFromSavedTask reopens a task from the catalogue as a draft.
//
// The screen offered for marking outputs is whatever the terminal is showing
// now, and the draft says so: a saved task carries its output positions but
// not the screen they were read from, and presenting the current screen as
// "the screen this task ends on" would be a lie whenever it is not. The
// positions are editable as numbers regardless, so a correction never depends
// on the terminal being in the right place.
func (app *App) draftFromSavedTask(c *gin.Context, s *session.Session, name string) {
	t, ok := app.findTask(c, name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("there is no task called %q", name)})
		return
	}
	draft := &task.Draft{Task: t.Clone(), Origin: "task", OriginalName: t.Name}
	if app.isExtensionTask(c, t.Name) {
		draft.Notes = append(draft.Notes, fmt.Sprintf(
			"%q came from an installed extension. Saving stores your version in this deployment's catalogue, which then takes precedence over the extension's.", t.Name))
	}

	if h := app.sessionHost(s); h != nil {
		if screen := h.GetScreenSnapshot(); screen != nil {
			draft.FinalScreen = screen.Text()
		}
	}
	if draft.FinalScreen == "" {
		draft.Notes = append(draft.Notes,
			"The terminal has no screen to show, so the answer regions can only be edited as row, column and length.")
	} else {
		draft.Notes = append(draft.Notes,
			"The screen shown for the answer is what this terminal is displaying now — not necessarily the screen this task ends on. Run the task first if you want to re-mark a region by eye.")
	}

	// A guard is editable against the screen it runs on where there is one;
	// a saved task has never carried its screens, so the wizard falls back to
	// editing the guard text itself. Reporting an unguarded step is still
	// worth doing — it is the one defect that makes a task dangerous rather
	// than merely wrong.
	for i, step := range draft.Task.Steps {
		if len(step.Expect) == 0 {
			draft.Notes = append(draft.Notes, fmt.Sprintf(
				"Step %d has no guard, so it will act on whatever screen it finds. Give it one.", i+1))
		}
	}
	c.JSON(http.StatusOK, draft)
}

// TasksPreviewHandler answers "what would this actually do?" for a task that
// has not been saved yet.
//
// It is the check the authoring wizard runs before the Save button, and it
// exists because everything that makes a task wrong is invisible on the form:
// a region a character too short reads "GRA" instead of "GRACE", a pattern
// that matches nothing reports the answer as missing, and both look perfectly
// fine until somebody runs the task for a customer. The preview runs the
// task's own validation and its own extraction code — not a second
// implementation of either — so what it shows is what the runner will do.
func (app *App) TasksPreviewHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session not found"})
		return
	}
	var payload struct {
		Task task.Task `json:"task"`
		// Screen is the screen to read the answer from. The wizard sends the
		// one it is showing, so the preview matches what the person is
		// looking at; omitting it falls back to the live terminal.
		Screen string `json:"screen"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}

	t := payload.Task
	if err := t.Validate(); err != nil {
		// 200, not 400: an incomplete task is the expected state of something
		// being authored, and the wizard wants the complaint to show beside
		// the field rather than as a failed request.
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	screen := payload.Screen
	if strings.TrimSpace(screen) == "" {
		if h := app.sessionHost(s); h != nil {
			if snapshot := h.GetScreenSnapshot(); snapshot != nil {
				screen = snapshot.Text()
			}
		}
	}

	outputs, missing := task.PreviewOutputs(screen, t.Outputs)
	body := gin.H{"ok": true, "outputs": outputs}
	if len(missing) > 0 {
		body["missing"] = missing
	}
	if warnings := previewWarnings(t); len(warnings) > 0 {
		body["warnings"] = warnings
	}
	// Saving over an existing task is a replacement, and the only moment it
	// can be mentioned usefully is before it happens.
	if existing, ok := app.findTask(c, t.Name); ok {
		body["replaces"] = existing.Name
	}

	// What this task is called everywhere that is not the Tasks menu. A task
	// is also an MCP tool and an API operation the moment it is saved, and
	// the person who just authored it is the one who needs to know the name
	// to hand to a colleague, a script or an assistant — so it is answered
	// here rather than left to be discovered.
	if tool := task.ToolName(t.Name); tool != task.ToolNamePrefix {
		body["toolName"] = tool
		if clash, ok := app.toolNameClash(c, t.Name, tool); ok {
			body["toolNameClash"] = clash
		}
	}
	c.JSON(http.StatusOK, body)
}

// toolNameClash reports another task that would generate the same tool name.
//
// Tool names are lossy — "Check balance", "check-balance" and "Check Balance"
// all land on task_check_balance — and the catalogue resolves a collision by
// dropping one of them. A task that is in the menu and missing from a model's
// tool list is the kind of gap nobody notices until somebody asks the model to
// run it and is told it does not exist.
func (app *App) toolNameClash(c *gin.Context, name, tool string) (string, bool) {
	tasks, err := app.allTasks(c)
	if err != nil {
		return "", false
	}
	want := task.NormalizeName(name)
	for _, other := range tasks {
		if task.NormalizeName(other.Name) == want {
			continue
		}
		if task.ToolName(other.Name) == tool {
			return other.Name, true
		}
	}
	return "", false
}

// previewWarnings lists what is legal but probably not intended. These are
// warnings rather than errors on purpose: an unguarded step is a real task
// somebody may have meant to write, and refusing to save it would be the
// wizard overruling the person who knows the application.
func previewWarnings(t task.Task) []string {
	var warnings []string
	for i, step := range t.Steps {
		if len(step.Expect) == 0 {
			warnings = append(warnings, fmt.Sprintf(
				"Step %d has no guard: it will type into whatever screen the terminal happens to be showing.", i+1))
		}
	}
	if len(t.Outputs) == 0 {
		warnings = append(warnings,
			"This task reports nothing back, so running it can only be judged by looking at the terminal.")
	}
	for _, p := range t.Parameters {
		if p.Sensitive && strings.TrimSpace(p.Example) != "" {
			warnings = append(warnings, fmt.Sprintf(
				"%s is marked as a secret but carries an example value, which is stored in the catalogue in clear text.", p.DisplayLabel()))
		}
	}
	return warnings
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
	t, ok := app.findTask(c, payload.Name)
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
