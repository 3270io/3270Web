package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/agent"
	"github.com/jnnngs/3270Web/internal/session"
	"github.com/jnnngs/3270Web/internal/task"
)

// installTaskExtension writes an extension beside the binary that contributes
// one task document, and returns the app that will pick it up.
func installTaskExtension(t *testing.T, docs map[string]string) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	base := t.TempDir()

	dir := filepath.Join(base, "extensions", "acme-pack")
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}

	files := make([]string, 0, len(docs))
	for name, body := range docs {
		if err := os.WriteFile(filepath.Join(dir, "tasks", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, "tasks/"+name)
	}

	manifest := map[string]any{
		"schemaVersion": 1,
		"name":          "acme-pack",
		"version":       "0.1.0",
		"contributes":   map[string]any{"tasks": taskDecls(files)},
	}
	encoded, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, agent.ExtensionManifestName), encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	return &App{SessionManager: session.NewManager(), baseDir: base}
}

func taskDecls(files []string) []map[string]string {
	out := make([]map[string]string, 0, len(files))
	for _, f := range files {
		out = append(out, map[string]string{"file": f})
	}
	return out
}

func extensionTaskDoc(name string) string {
	return `{
      "name": "` + name + `",
      "description": "Contributed by a site pack.",
      "parameters": [{"name":"account_number","required":true}],
      "steps": [{"expect":[{"row":1,"column":1,"text":"ACCOUNT ENQUIRY"}],
                 "inputs":[{"row":5,"column":20,"parameter":"account_number"}],
                 "aidKey":"Enter"}],
      "outputs": [{"name":"balance","label":"Balance","row":8,"column":20,"length":12}]
    }`
}

// TestExtensionTasksReachTheCatalogue is the gap this closes: the manifest has
// always declared contributes.tasks and nothing ever read it, so a pack could
// ship tasks that silently never appeared.
func TestExtensionTasksReachTheCatalogue(t *testing.T) {
	app := installTaskExtension(t, map[string]string{
		"check-balance.json": extensionTaskDoc("ACME balance enquiry"),
	})

	tasks, err := app.allTasks()
	if err != nil {
		t.Fatalf("allTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected the contributed task, got %d: %+v", len(tasks), tasks)
	}
	if got := tasks[0].Source; got != "extension:acme-pack" {
		t.Errorf("source = %q, want extension:acme-pack — provenance is the first question anyone asks", got)
	}

	// And it is runnable by name, which is what the run handlers resolve
	// through.
	if _, ok := app.findTask("acme balance enquiry"); !ok {
		t.Error("a contributed task must resolve by name, case-insensitively like any other")
	}
	if !app.isExtensionTask("ACME balance enquiry") {
		t.Error("the task should be recognised as coming from an extension")
	}
}

// TestExtensionTasksGoThroughTheSameGate: an extension gets no shortcut past
// task.Validate. The malformed document is skipped and the sound one loads —
// one bad file must not take the pack with it.
func TestExtensionTasksGoThroughTheSameGate(t *testing.T) {
	app := installTaskExtension(t, map[string]string{
		"good.json": extensionTaskDoc("Good task"),
		// References a parameter no step fills, which Validate rejects.
		"bad.json": `{"name":"Bad task","parameters":[{"name":"unused"}],
		              "steps":[{"aidKey":"Enter"}]}`,
		"garbage.json": `not json at all`,
	})

	tasks, err := app.allTasks()
	if err != nil {
		t.Fatalf("allTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Name != "Good task" {
		t.Fatalf("expected only the valid task, got %+v", tasks)
	}
}

// TestSavedTaskWinsOverAnExtension: the catalogue an operator can edit takes
// precedence over content that arrived with a folder. The other way round, an
// extension could silently replace a task someone recorded.
func TestSavedTaskWinsOverAnExtension(t *testing.T) {
	app := installTaskExtension(t, map[string]string{
		"check-balance.json": extensionTaskDoc("Account balance enquiry"),
	})

	saved := validTaskPayload()
	var t2 task.Task
	encoded, _ := json.Marshal(saved)
	if err := json.Unmarshal(encoded, &t2); err != nil {
		t.Fatal(err)
	}
	if _, err := app.taskStore().Upsert(t2); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	tasks, err := app.allTasks()
	if err != nil {
		t.Fatalf("allTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("the shadowed entry should be dropped, not listed twice: %+v", tasks)
	}
	if tasks[0].Source == "extension:acme-pack" {
		t.Error("the extension's version won; the operator's saved task must")
	}
	if app.isExtensionTask("Account balance enquiry") {
		t.Error("a name the store holds is not an extension task")
	}

	// And the run path resolves to the same one the listing shows.
	resolved, ok := app.findTask("Account balance enquiry")
	if !ok {
		t.Fatal("the task should resolve")
	}
	if resolved.Source == "extension:acme-pack" {
		t.Error("findTask resolved to the extension's version while the listing shows the operator's")
	}
}

// TestDeletingAnExtensionTaskIsRefused. Delete rewrites the store's file, and
// an extension's task is not in it — so a delete would report success, change
// nothing, and put the task back on the menu at the next refresh.
func TestDeletingAnExtensionTaskIsRefused(t *testing.T) {
	app := installTaskExtension(t, map[string]string{
		"check-balance.json": extensionTaskDoc("ACME balance enquiry"),
	})
	r := taskRouter(app)

	w := postJSON(r, "/tasks/delete", map[string]any{"name": "ACME balance enquiry"})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "extension") {
		t.Errorf("the refusal should say where the task comes from, got %s", body)
	}

	if tasks, _ := app.allTasks(); len(tasks) != 1 {
		t.Errorf("the task should still be there, got %+v", tasks)
	}
}

// TestSaveReturnsTheMergedCatalogue: the browser replaces its menu with what
// comes back from a save, so returning only the store's half would make every
// extension task vanish until the next page load.
func TestSaveReturnsTheMergedCatalogue(t *testing.T) {
	app := installTaskExtension(t, map[string]string{
		"check-balance.json": extensionTaskDoc("ACME balance enquiry"),
	})
	r := taskRouter(app)

	w := postJSON(r, "/tasks/save", validTaskPayload())
	if w.Code != http.StatusOK {
		t.Fatalf("save: status = %d (body %s)", w.Code, w.Body.String())
	}
	names := taskNamesFrom(t, w)
	if len(names) != 2 {
		t.Errorf("save returned %v, want both the saved and the contributed task", names)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tasks", nil))
	if got := taskNamesFrom(t, w); len(got) != 2 {
		t.Errorf("the listing returned %v, want both", got)
	}
}

func taskNamesFrom(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	var payload struct {
		Tasks []task.Task `json:"tasks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response is not a task list: %v (%s)", err, w.Body.String())
	}
	out := make([]string, 0, len(payload.Tasks))
	for _, task := range payload.Tasks {
		out = append(out, task.Name)
	}
	return out
}
