package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/macroimport"
	"github.com/jnnngs/3270Web/internal/session"
)

const sampleMacro = `' Account inquiry
Sub Main
    Session.Connect
    Session.Screen.PutString "USER01", 5, 20
    Session.Screen.SendKeys "<Enter>"
    Session.Screen.WaitHostQuiet 1500
    Session.Screen.MoveTo 10, 15
    Session.Screen.SendKeys "ACCT100<Enter>"
    If Session.Screen.GetString(1, 1, 5) = "READY" Then
        Session.Screen.SendKeys "<PF3>"
    End If
    MsgBox "done"
    Session.Disconnect
End Sub
`

func macroTestApp(t *testing.T) (*App, *gin.Engine, *session.Session) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	mock.Connected = true
	app := &App{SessionManager: session.NewManager(), baseDir: t.TempDir()}
	sess := app.SessionManager.CreateSession(mock)
	withSessionLock(sess, func() {
		sess.TargetHost = "mainframe.example.test"
		sess.TargetPort = 992
	})

	r := gin.New()
	r.POST("/workflow/import-macro", app.ImportMacroHandler)
	return app, r, sess
}

// postMacroFile sends the macro the way a browser does: as a file upload.
func postMacroFile(t *testing.T, r http.Handler, sessID, filename, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("macro", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/workflow/import-macro", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if sessID != "" {
		req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sessID})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestImportMacroLoadsTheTranslatedRecording(t *testing.T) {
	_, r, sess := macroTestApp(t)

	w := postMacroFile(t, r, sess.ID, "account-inquiry.mac", sampleMacro)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}
	var res struct {
		Name       string `json:"name"`
		StepTotal  int    `json:"stepTotal"`
		Statements int    `json:"statements"`
		Translated int    `json:"translated"`
		Notes      []struct {
			Line   int    `json:"line"`
			Source string `json:"source"`
			Reason string `json:"reason"`
		} `json:"notes"`
		Advisories []string `json:"advisories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.Name != "account-inquiry.json" {
		t.Errorf("name = %q, want the macro's own name with a json extension", res.Name)
	}
	if res.StepTotal != 9 {
		t.Errorf("stepTotal = %d, want 9", res.StepTotal)
	}
	// The branch came across as decision steps; the dialog did not, and says
	// so against its own line.
	if len(res.Notes) != 1 {
		t.Fatalf("notes = %+v, want one", res.Notes)
	}
	for _, note := range res.Notes {
		if note.Line == 0 || strings.TrimSpace(note.Source) == "" || strings.TrimSpace(note.Reason) == "" {
			t.Errorf("note is missing its line, source or reason: %+v", note)
		}
	}

	// The recording is loaded into the session, so Play and Debug reach it
	// without a second upload.
	var payload []byte
	var name string
	var stepTotal int
	withSessionLock(sess, func() {
		if sess.LoadedWorkflow == nil {
			return
		}
		payload = append([]byte(nil), sess.LoadedWorkflow.Payload...)
		name = sess.LoadedWorkflow.Name
		stepTotal = sess.LoadedWorkflow.StepTotal
	})
	if len(payload) == 0 {
		t.Fatal("no recording was loaded into the session")
	}
	if name != "account-inquiry.json" || stepTotal != 9 {
		t.Errorf("loaded recording is %q with %d steps", name, stepTotal)
	}

	workflow, err := parseWorkflowPayload(payload)
	if err != nil {
		t.Fatalf("the loaded recording does not parse as one: %v", err)
	}
	if workflow.Host != "mainframe.example.test" || workflow.Port != 992 {
		t.Errorf("recording targets %s:%d, want the session's own host", workflow.Host, workflow.Port)
	}
	if !strings.Contains(workflow.Description, "did not translate") {
		t.Errorf("description = %q, want it to carry the untranslated count", workflow.Description)
	}
	types := make([]string, 0, len(workflow.Steps))
	for _, step := range workflow.Steps {
		types = append(types, step.Type)
	}
	want := "Connect FillString PressEnter FillString PressEnter If PressPF3 EndIf Disconnect"
	if got := strings.Join(types, " "); got != want {
		t.Errorf("steps = %q, want %q", got, want)
	}

	// Every step the translator emits has to be one playback recognises;
	// otherwise the import produces a file that stops at the first step.
	for _, step := range workflow.Steps {
		switch step.Type {
		case "Connect", "Disconnect", "FillString", "CheckValue":
			continue
		}
		if isControlFlowStepType(step.Type) {
			continue
		}
		if _, ok := workflowKeyForStepType(step.Type); !ok {
			t.Errorf("step type %q is not one playback can run", step.Type)
		}
	}

	// And the blocks it emitted have to resolve, because a recording whose
	// blocks do not close is refused as a whole when it is loaded — an import
	// that produced one would have translated a branch into a file nobody can
	// play.
	if _, err := compileWorkflowSteps(workflow.Steps); err != nil {
		t.Errorf("the translated recording does not compile: %v", err)
	}
}

// TestImportedBranchesCompile puts the shapes that emit decision steps through
// the same compilation playback does. The blocks are written by one piece of
// code and closed by another, and an If left open is not a step that fails —
// it is a recording that cannot be loaded at all.
func TestImportedBranchesCompile(t *testing.T) {
	macros := map[string]string{
		"if-else": `If Session.Screen.GetString(1, 1, 5) = "READY" Then
    Session.Screen.SendKeys "<Enter>"
Else
    Session.Screen.SendKeys "<PF3>"
End If
`,
		"elseif-chain": `If Session.Screen.GetString(1, 1, 1) = "A" Then
    Session.Screen.SendKeys "<PF1>"
ElseIf Session.Screen.GetString(1, 1, 1) = "B" Then
    Session.Screen.SendKeys "<PF2>"
ElseIf Session.Screen.GetString(1, 1, 1) = "C" Then
    Session.Screen.SendKeys "<PF3>"
Else
    Session.Screen.SendKeys "<PF4>"
End If
`,
		"nested-loop": `balance = Session.Screen.GetString(12, 20, 10)
Do While Session.Screen.GetString(24, 2, 4) = "MORE"
    If balance > 1000 Then
        Session.Screen.PutString balance, 5, 20
    End If
    Session.Screen.SendKeys "<Enter>"
Loop
`,
		"inline-if": `If Session.Screen.GetString(1, 1, 2) = "OK" Then Session.Screen.SendKeys "<Enter>" Else Session.Screen.SendKeys "<PF3>"
`,
		"exit-sub": `Sub Main
    If Session.Screen.GetString(1, 1, 1) = "X" Then
        Exit Sub
    End If
    Session.Screen.SendKeys "<Enter>"
End Sub
`,
	}
	for name, src := range macros {
		result := macroimport.Translate(src)
		if len(result.Notes) != 0 {
			t.Errorf("%s: expected a clean translation, got %+v", name, result.Notes)
		}
		program, err := compileWorkflowSteps(result.Steps)
		if err != nil {
			t.Errorf("%s: the translated recording does not compile: %v", name, err)
			continue
		}
		if !program.hasControlFlow {
			t.Errorf("%s: nothing in the translation decides anything", name)
		}
	}
}

func TestImportMacroRefusesAFileWithNoStepsButStillReportsWhy(t *testing.T) {
	_, r, sess := macroTestApp(t)

	w := postMacroFile(t, r, sess.ID, "loop.mac", "For i = 1 To 10\nNext\n")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d (body %s), want 422", w.Code, w.Body.String())
	}
	var res struct {
		Error  string `json:"error"`
		Report struct {
			Notes []struct {
				Line   int    `json:"line"`
				Reason string `json:"reason"`
			} `json:"notes"`
		} `json:"report"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.Error == "" {
		t.Error("a refusal with no message tells the operator nothing")
	}
	if len(res.Report.Notes) != 1 {
		t.Errorf("notes = %+v, want the loop reported once", res.Report.Notes)
	}

	// Nothing was loaded, so a recording that was already there survives a
	// failed import — there is no half-loaded state.
	loaded := false
	withSessionLock(sess, func() { loaded = sess.LoadedWorkflow != nil })
	if loaded {
		t.Error("a file that produced no steps was loaded anyway")
	}
}

func TestImportMacroNeedsASession(t *testing.T) {
	_, r, _ := macroTestApp(t)
	if w := postMacroFile(t, r, "", "x.mac", sampleMacro); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestImportMacroRefusesDuringPlayback(t *testing.T) {
	_, r, sess := macroTestApp(t)
	withSessionLock(sess, func() {
		sess.Playback = &session.WorkflowPlayback{Active: true}
	})
	if w := postMacroFile(t, r, sess.ID, "x.mac", sampleMacro); w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestImportMacroRefusesAnEmptyFile(t *testing.T) {
	_, r, sess := macroTestApp(t)
	if w := postMacroFile(t, r, sess.ID, "x.mac", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestImportMacroRefusesAnOversizedFile(t *testing.T) {
	_, r, sess := macroTestApp(t)
	huge := strings.Repeat("' padding\n", (maxMacroUploadBytes/10)+64)
	w := postMacroFile(t, r, sess.ID, "big.mac", huge)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d (body %s), want 400", w.Code, w.Body.String())
	}
}

// A filename is somebody else's text. It names a recording that is rendered
// into a page and offered as a download, so nothing from it may carry a path
// or markup into either.
func TestRecordingNameForMacroKeepsTheNameHarmless(t *testing.T) {
	cases := map[string]string{
		"account-inquiry.mac":           "account-inquiry.json",
		"ACCOUNT.MAC":                   "ACCOUNT.json",
		"logon":                         "logon.json",
		`C:\macros\daily run.mac`:       "daily run.json",
		"../../etc/passwd":              "passwd.json",
		"/var/tmp/x.mac":                "x.json",
		"<script>alert(1)</script>.mac": "script.json",
		"":                              "imported-macro.json",
		"   ":                           "imported-macro.json",
		"...":                           "imported-macro.json",
		"..":                            "imported-macro.json",
		"\u0000\u0001.mac":              "imported-macro.json",
	}
	for in, want := range cases {
		if got := recordingNameForMacro(in); got != want {
			t.Errorf("recordingNameForMacro(%q) = %q, want %q", in, got, want)
		}
	}
	for in := range cases {
		got := recordingNameForMacro(in)
		if strings.ContainsAny(got, `/\<>"'`) {
			t.Errorf("recordingNameForMacro(%q) = %q, which carries a path or markup character", in, got)
		}
	}
}

func TestAPITranslateMacroReturnsTheRecordingWithoutASession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const token = "test-token"
	t.Setenv("API_TOKEN", token)

	app := &App{SessionManager: session.NewManager(), chaosEngines: newChaosEngineStore(), baseDir: t.TempDir()}
	r := gin.New()
	app.registerAPIv1(r)

	body, err := json.Marshal(map[string]string{"name": "account-inquiry.mac", "content": sampleMacro})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/macros/translate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", w.Code, w.Body.String())
	}

	var res struct {
		Name      string `json:"name"`
		StepTotal int    `json:"stepTotal"`
		Notes     []struct {
			Line int `json:"line"`
		} `json:"notes"`
		Workflow *WorkflowConfig `json:"workflow"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.Workflow == nil {
		t.Fatal("no recording came back; converting a directory of macros needs the file itself")
	}
	if len(res.Workflow.Steps) != res.StepTotal || res.StepTotal != 9 {
		t.Errorf("stepTotal = %d with %d steps, want 9", res.StepTotal, len(res.Workflow.Steps))
	}
	if len(res.Notes) != 1 {
		t.Errorf("notes = %+v, want the same report the browser gets", res.Notes)
	}
	// Nothing about the caller's host is known here, so the recording says so
	// rather than naming one it made up.
	if res.Workflow.Host != "" {
		t.Errorf("workflow host = %q, want it left for the caller to fill in", res.Workflow.Host)
	}
}

func TestAPITranslateMacroNeedsAToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("API_TOKEN", "test-token")
	app := &App{SessionManager: session.NewManager(), chaosEngines: newChaosEngineStore(), baseDir: t.TempDir()}
	r := gin.New()
	app.registerAPIv1(r)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/macros/translate", strings.NewReader(`{"content":"Session.Connect"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
