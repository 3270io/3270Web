// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/session"
	"github.com/jnnngs/3270Web/internal/task"
)

func newTaskTestApp(t *testing.T) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	app := &App{SessionManager: session.NewManager(), baseDir: t.TempDir()}
	return app
}

func taskRouter(app *App) *gin.Engine {
	r := gin.New()
	r.GET("/tasks", app.TasksListHandler)
	r.POST("/tasks/save", app.TasksSaveHandler)
	r.POST("/tasks/delete", app.TasksDeleteHandler)
	r.POST("/tasks/run", app.TasksRunHandler)
	r.GET("/tasks/status", app.TasksStatusHandler)
	r.POST("/tasks/cancel", app.TasksCancelHandler)
	r.GET("/tasks/draft", app.TasksDraftHandler)
	return r
}

func postJSON(r *gin.Engine, path string, payload any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func validTaskPayload() map[string]any {
	return map[string]any{
		"name":        "Account balance enquiry",
		"description": "Retrieves the current cleared balance.",
		"parameters": []map[string]any{{
			"name": "account_number", "label": "Account number", "required": true,
		}},
		"steps": []map[string]any{{
			"description": "Enter the account number",
			"expect":      []map[string]any{{"row": 1, "column": 1, "text": "ACCOUNT ENQUIRY"}},
			"inputs":      []map[string]any{{"row": 5, "column": 20, "parameter": "account_number"}},
			"aidKey":      "Enter",
		}},
		"outputs": []map[string]any{{
			"name": "cleared_balance", "label": "Cleared balance",
			"row": 8, "column": 20, "length": 12,
		}},
	}
}

func TestTasksCatalogueRoundTrip(t *testing.T) {
	app := newTaskTestApp(t)
	r := taskRouter(app)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", w.Code, w.Body.String())
	}
	var listed struct{ Tasks []task.Task }
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tasks) != 0 {
		t.Fatalf("a fresh catalogue should be empty, got %d", len(listed.Tasks))
	}

	if w := postJSON(r, "/tasks/save", validTaskPayload()); w.Code != http.StatusOK {
		t.Fatalf("save status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tasks) != 1 || listed.Tasks[0].Name != "Account balance enquiry" {
		t.Fatalf("tasks = %+v", listed.Tasks)
	}

	if w := postJSON(r, "/tasks/delete", map[string]string{"name": "Account balance enquiry"}); w.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tasks) != 0 {
		t.Fatalf("expected an empty catalogue after delete, got %+v", listed.Tasks)
	}
}

// A malformed task must be refused at the boundary — the store and the runner
// both assume Validate has already run.
func TestTasksSaveRejectsAMalformedTask(t *testing.T) {
	app := newTaskTestApp(t)
	r := taskRouter(app)

	payload := validTaskPayload()
	payload["steps"].([]map[string]any)[0]["inputs"].([]map[string]any)[0]["parameter"] = "does_not_exist"
	w := postJSON(r, "/tasks/save", payload)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("undefined parameter")) {
		t.Errorf("body = %s; want it to name the problem", w.Body.String())
	}
}

func TestTasksSaveRejectsAKeyTheTerminalCannotPress(t *testing.T) {
	app := newTaskTestApp(t)
	r := taskRouter(app)
	payload := validTaskPayload()
	payload["steps"].([]map[string]any)[0]["aidKey"] = "Quit()"
	w := postJSON(r, "/tasks/save", payload)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

// Running without a session must not be mistaken for running without a host.
func TestTasksRunRequiresASession(t *testing.T) {
	app := newTaskTestApp(t)
	r := taskRouter(app)
	w := postJSON(r, "/tasks/run", map[string]any{"name": "Account balance enquiry"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body=%s", w.Code, w.Body.String())
	}
}

func TestTasksStatusRequiresASession(t *testing.T) {
	app := newTaskTestApp(t)
	r := taskRouter(app)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tasks/status", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

/* ---------------------------------------------------------------- */
/* The run store                                                    */
/* ---------------------------------------------------------------- */

func TestTaskRunStoreRefusesASecondConcurrentRun(t *testing.T) {
	store := newTaskRunStore()
	if err := store.begin("s1", &taskRun{Task: "first", cancel: func() {}}); err != nil {
		t.Fatalf("first begin: %v", err)
	}
	// One task drives the one terminal a session owns; two at once would
	// interleave keystrokes into the same screen buffer.
	if err := store.begin("s1", &taskRun{Task: "second", cancel: func() {}}); err == nil {
		t.Fatal("expected a second concurrent run in the same session to be refused")
	}
	// A different session is independent.
	if err := store.begin("s2", &taskRun{Task: "other", cancel: func() {}}); err != nil {
		t.Errorf("a run in another session must be allowed, got %v", err)
	}
	// Once the first finishes, its session is free again.
	store.update("s1", func(r *taskRun) { r.done = true })
	if err := store.begin("s1", &taskRun{Task: "third", cancel: func() {}}); err != nil {
		t.Errorf("expected a new run after the previous finished, got %v", err)
	}
}

func TestTaskRunStoreCancelOnlyReportsALiveRun(t *testing.T) {
	store := newTaskRunStore()
	cancelled := false
	run := &taskRun{Task: "t", cancel: func() { cancelled = true }}
	if err := store.begin("s1", run); err != nil {
		t.Fatal(err)
	}
	if !store.cancel("s1") {
		t.Fatal("cancelling a live run should report true")
	}
	if !cancelled {
		t.Error("the run's cancel func was not called")
	}
	store.update("s1", func(r *taskRun) { r.done = true })
	if store.cancel("s1") {
		t.Error("cancelling a finished run must report false")
	}
	if store.cancel("nobody") {
		t.Error("cancelling an unknown session must report false")
	}
}

func TestTaskRunStoreSnapshotIsACopy(t *testing.T) {
	store := newTaskRunStore()
	if err := store.begin("s1", &taskRun{Task: "t", Total: 3, cancel: func() {}}); err != nil {
		t.Fatal(err)
	}
	snap, ok := store.snapshot("s1")
	if !ok {
		t.Fatal("expected a snapshot")
	}
	snap.Task = "mutated"
	again, _ := store.snapshot("s1")
	if again.Task == "mutated" {
		t.Error("snapshot must hand back a copy, not the live run")
	}
	if _, ok := store.snapshot("nobody"); ok {
		t.Error("an unknown session must not yield a snapshot")
	}
}

func TestTaskRunStoreForgetReleasesTheEntry(t *testing.T) {
	store := newTaskRunStore()
	if err := store.begin("s1", &taskRun{Task: "t", cancel: func() {}}); err != nil {
		t.Fatal(err)
	}
	store.forget("s1")
	if _, ok := store.snapshot("s1"); ok {
		t.Error("forget should remove the run so the map does not grow per session")
	}
}

// The store is read by request handlers and written by the goroutine driving
// the run, so it has to be safe under -race with both happening at once.
func TestTaskRunStoreIsConcurrencySafe(t *testing.T) {
	store := newTaskRunStore()
	if err := store.begin("s1", &taskRun{Task: "t", Total: 2, cancel: func() {}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); store.update("s1", func(r *taskRun) { r.Step++ }) }()
		go func() { defer wg.Done(); store.snapshot("s1") }()
	}
	wg.Wait()
	snap, _ := store.snapshot("s1")
	if snap.Step != 20 {
		t.Errorf("Step = %d, want 20", snap.Step)
	}
}

func TestTasksDraftRequiresASession(t *testing.T) {
	app := newTaskTestApp(t)
	r := taskRouter(app)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tasks/draft", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

/* ---------------------------------------------------------------- */
/* The token-authenticated API                                      */
/* ---------------------------------------------------------------- */

func apiRouter(app *App) *gin.Engine {
	r := gin.New()
	app.registerAPIv1(r)
	return r
}

func apiRequest(r *gin.Engine, method, path, token string, payload any) *httptest.ResponseRecorder {
	var body *bytes.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The whole /api/v1 surface is off unless API_TOKEN is set, and the task
// routes must not be an exception — they can drive a mainframe transaction.
func TestAPITaskRoutesAreOffWithoutAToken(t *testing.T) {
	t.Setenv("API_TOKEN", "")
	app := newTaskTestApp(t)
	r := apiRouter(app)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tasks"},
		{http.MethodPost, "/api/v1/tasks"},
		{http.MethodPost, "/api/v1/sessions/abc/tasks/run"},
	} {
		w := apiRequest(r, tc.method, tc.path, "", map[string]any{})
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d, want 503 when API_TOKEN is unset", tc.method, tc.path, w.Code)
		}
	}
}

func TestAPITaskRoutesRejectABadToken(t *testing.T) {
	t.Setenv("API_TOKEN", "correct-horse")
	app := newTaskTestApp(t)
	r := apiRouter(app)
	if w := apiRequest(r, http.MethodGet, "/api/v1/tasks", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", w.Code)
	}
	if w := apiRequest(r, http.MethodGet, "/api/v1/tasks", "wrong", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", w.Code)
	}
}

// The catalogue endpoint doubles as export/import: what GET returns is what
// POST accepts, so a catalogue can be version-controlled and moved between
// deployments without a bespoke format.
func TestAPITaskCatalogueRoundTripsThroughItself(t *testing.T) {
	t.Setenv("API_TOKEN", "tok")
	app := newTaskTestApp(t)
	r := apiRouter(app)

	if w := apiRequest(r, http.MethodPost, "/api/v1/tasks", "tok", validTaskPayload()); w.Code != http.StatusOK {
		t.Fatalf("save = %d, body=%s", w.Code, w.Body.String())
	}
	w := apiRequest(r, http.MethodGet, "/api/v1/tasks", "tok", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d", w.Code)
	}
	var exported struct{ Tasks []task.Task }
	if err := json.Unmarshal(w.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.Tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(exported.Tasks))
	}

	// Feed an exported task straight back in — into a fresh catalogue, so this
	// proves the shape survives the round trip rather than that nothing moved.
	fresh := newTaskTestApp(t)
	fr := apiRouter(fresh)
	if w := apiRequest(fr, http.MethodPost, "/api/v1/tasks", "tok", exported.Tasks[0]); w.Code != http.StatusOK {
		t.Fatalf("re-import = %d, body=%s", w.Code, w.Body.String())
	}
	again, _ := fresh.taskStore().Find("Account balance enquiry")
	if len(again.Steps) != 1 || len(again.Outputs) != 1 || len(again.Parameters) != 1 {
		t.Errorf("the round trip lost part of the task: %+v", again)
	}
}

func TestAPISaveTaskValidatesAtTheBoundary(t *testing.T) {
	t.Setenv("API_TOKEN", "tok")
	app := newTaskTestApp(t)
	r := apiRouter(app)
	bad := validTaskPayload()
	bad["steps"].([]map[string]any)[0]["inputs"].([]map[string]any)[0]["parameter"] = "ghost"
	w := apiRequest(r, http.MethodPost, "/api/v1/tasks", "tok", bad)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
}

func TestAPIRunTaskRejectsAnUnknownSession(t *testing.T) {
	t.Setenv("API_TOKEN", "tok")
	app := newTaskTestApp(t)
	r := apiRouter(app)
	w := apiRequest(r, http.MethodPost, "/api/v1/sessions/nope/tasks/run", "tok",
		map[string]any{"name": "Account balance enquiry"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", w.Code, w.Body.String())
	}
}

// A session with no host is a different failure from a missing session, and
// conflating them would send a caller looking in the wrong place.
func TestAPIRunTaskRejectsADisconnectedSession(t *testing.T) {
	t.Setenv("API_TOKEN", "tok")
	app := newTaskTestApp(t)
	s := app.SessionManager.CreateSession(nil)
	r := apiRouter(app)
	w := apiRequest(r, http.MethodPost, "/api/v1/sessions/"+s.ID+"/tasks/run", "tok",
		map[string]any{"name": "Account balance enquiry"})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", w.Code, w.Body.String())
	}
}
