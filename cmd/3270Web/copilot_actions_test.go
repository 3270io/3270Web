// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// The endpoints an assistant drives the terminal through, tested as the
// assistant uses them: by position, without sight of the screen.

const unlockedStatus = "U F P C(localhost) I 4 24 80 0 0 0x0 0.000"

// formScreen is a small maintenance form:
//
//	row 0: CUSTOMER MAINTENANCE
//	row 2: NAME . . :  [SMITH               ]
//	row 3: PIN         [SECRET  ]   (hidden)
//	row 5:             AGE
//	row 6:             [    ]       (numeric, labelled from the row above)
func formScreen() *host.Screen {
	s := &host.Screen{Width: 80, Height: 24, IsFormatted: true, Status: unlockedStatus}
	s.Buffer = make([][]rune, s.Height)
	for y := range s.Buffer {
		s.Buffer[y] = []rune(strings.Repeat(" ", s.Width))
	}
	put := func(row, col int, text string) {
		copy(s.Buffer[row][col:], []rune(text))
	}
	field := func(code byte, row, startCol, endCol int) {
		s.Fields = append(s.Fields, host.NewField(s, code, startCol, row, endCol, row, 0, 0))
	}

	put(0, 1, "CUSTOMER MAINTENANCE")
	field(host.AttrProtected, 0, 1, 79)

	put(2, 1, "NAME . . :")
	field(host.AttrProtected, 2, 1, 11)
	put(2, 13, "SMITH")
	field(0, 2, 13, 32)
	field(host.AttrProtected, 2, 34, 79)

	put(3, 1, "PIN")
	field(host.AttrProtected, 3, 1, 11)
	put(3, 13, "SECRET")
	field(host.AttrDisp1|host.AttrDisp2, 3, 13, 20)
	field(host.AttrProtected, 3, 22, 79)

	put(5, 13, "AGE")
	field(host.AttrProtected, 5, 1, 79)
	field(host.AttrNumeric, 6, 13, 16)
	field(host.AttrProtected, 6, 18, 79)
	return s
}

func newFormApp(t *testing.T) (*App, *host.MockHost, *session.Session, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.Screen = formScreen()
	app := &App{SessionManager: session.NewManager(), chaosEngines: newChaosEngineStore()}
	sess := app.SessionManager.CreateSession(mh)

	r := gin.New()
	r.GET("/screen.json", app.ScreenJSONHandler)
	r.POST("/screen/key", app.ScreenKeyHandler)
	r.POST("/screen/write", app.ScreenWriteHandler)
	r.POST("/screen/submit", app.ScreenSubmitHandler)
	r.POST("/screen/wait", app.ScreenWaitHandler)
	r.GET("/copilot/context", app.CopilotContextHandler)
	r.GET("/copilot/known-screen", app.KnownScreenHandler)
	return app, mh, sess, r
}

func decodeActionBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, w.Body.String())
	}
	return out
}

func keysSent(mh *host.MockHost) []string {
	var out []string
	for _, cmd := range mh.Commands {
		if key, ok := strings.CutPrefix(cmd, "key:"); ok {
			out = append(out, key)
		}
	}
	return out
}

// --- submit_screen -----------------------------------------------------------

// submit_screen used to report success and press nothing: the handler wrote
// fields marked changed — of which there were never any, since write_field
// types straight into the terminal — and sent no key at all.
func TestScreenSubmitHandlerPressesEnterAndReturnsTheScreen(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/submit", sess.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := keysSent(mh); len(got) != 1 || got[0] != "Enter" {
		t.Fatalf("submit sent keys %v, want exactly [Enter]", got)
	}
	body := decodeActionBody(t, w)
	if ok, _ := body["ok"].(bool); !ok {
		t.Errorf("ok should be true: %v", body)
	}
	screen, _ := body["screen"].(map[string]any)
	if screen == nil || !strings.Contains(screen["text"].(string), "CUSTOMER MAINTENANCE") {
		t.Errorf("submit should return the resulting screen, got %v", body["screen"])
	}
}

func TestScreenSubmitHandlerHonoursTheNamedAID(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/submit", sess.ID, map[string]any{"aid": "pf5"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := keysSent(mh); len(got) != 1 || got[0] != "PF(5)" {
		t.Errorf("submit sent keys %v, want [PF(5)]", got)
	}

	w = doJSON(r, http.MethodPost, "/screen/submit", sess.ID, map[string]any{"aid": "PF99"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("an unknown aid answered %d, want 400", w.Code)
	}
	// Submitting with a dangerous key is pressing a dangerous key.
	w = doJSON(r, http.MethodPost, "/screen/submit", sess.ID, map[string]any{"aid": "F3"})
	if w.Code != http.StatusConflict {
		t.Errorf("submit with F3 answered %d, want 409", w.Code)
	}
	if got := keysSent(mh); len(got) != 1 {
		t.Errorf("a refused submit still sent a key: %v", got)
	}
}

// The REST submit and the panel's submit are one implementation; this is the
// half of that claim a test can hold: the API route presses the key too.
func TestAPISubmitAndPanelSubmitAgree(t *testing.T) {
	app, mh, sess, _ := newFormApp(t)
	if err := app.submitScreen(sess, mh, "Enter"); err != nil {
		t.Fatal(err)
	}
	if got := mh.Commands; len(got) != 2 || got[0] != "submit" || got[1] != "key:Enter" {
		t.Errorf("submit ran %v, want [submit key:Enter]", got)
	}
}

// --- send_key approval -------------------------------------------------------

// The approval rule lived in the browser. The endpoint pressed whatever it was
// sent, and "F3" — which the key parser reads as PF3 — was not even on the
// browser's list.
func TestScreenKeyHandlerRefusesDangerousKeysWithoutConfirmation(t *testing.T) {
	for _, key := range []string{"PF3", "pf3", "PF(3)", "F3", "f3", "f03", "PF03", "Clear", "clear"} {
		t.Run(key, func(t *testing.T) {
			_, mh, sess, r := newFormApp(t)

			w := doJSON(r, http.MethodPost, "/screen/key", sess.ID, map[string]any{"key": key})
			if w.Code != http.StatusConflict {
				t.Fatalf("%q answered %d, want 409: %s", key, w.Code, w.Body.String())
			}
			body := decodeActionBody(t, w)
			if needs, _ := body["requiresApproval"].(bool); !needs {
				t.Errorf("the refusal should carry requiresApproval:true, got %v", body)
			}
			if body["key"] == nil || body["error"] == nil {
				t.Errorf("the refusal should name the key and say why, got %v", body)
			}
			if got := keysSent(mh); len(got) != 0 {
				t.Fatalf("a refused key was sent anyway: %v", got)
			}

			// "confirmed": false is not a confirmation.
			w = doJSON(r, http.MethodPost, "/screen/key", sess.ID, map[string]any{"key": key, "confirmed": false})
			if w.Code != http.StatusConflict {
				t.Errorf("%q with confirmed:false answered %d, want 409", key, w.Code)
			}

			w = doJSON(r, http.MethodPost, "/screen/key", sess.ID, map[string]any{"key": key, "confirmed": true})
			if w.Code != http.StatusOK {
				t.Fatalf("%q confirmed answered %d: %s", key, w.Code, w.Body.String())
			}
			if got := keysSent(mh); len(got) != 1 {
				t.Errorf("a confirmed key should be sent once, got %v", got)
			}
		})
	}
}

func TestScreenKeyHandlerSendsOrdinaryKeysAndReturnsTheScreen(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/key", sess.ID, map[string]any{"key": "f7"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := keysSent(mh); len(got) != 1 || got[0] != "PF(7)" {
		t.Errorf("sent %v, want [PF(7)]", got)
	}
	body := decodeActionBody(t, w)
	if body["key"] != "PF(7)" {
		t.Errorf("key = %v, want PF(7)", body["key"])
	}
	if _, ok := body["screen"].(map[string]any); !ok {
		t.Errorf("the key result should carry the resulting screen, got %v", body)
	}
}

// --- write_field -------------------------------------------------------------

func TestScreenWriteHandlerRequiresAPosition(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	for _, body := range []map[string]any{
		{"text": "X"},
		{"row": 2, "text": "X"},
		{"col": 13, "text": "X"},
	} {
		w := doJSON(r, http.MethodPost, "/screen/write", sess.ID, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%v answered %d, want 400", body, w.Code)
		}
	}
	if len(mh.Commands) != 0 {
		t.Errorf("a write with no position reached the host: %v", mh.Commands)
	}
}

func TestScreenWriteHandlerErasesByDefaultAndEchoesTheField(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 2, "col": 13, "text": "LEE"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := strings.TrimRight(string(mh.Screen.Buffer[2][13:33]), " "); got != "LEE" {
		t.Errorf("the field holds %q after writing LEE over SMITH, want %q", got, "LEE")
	}
	field, _ := decodeActionBody(t, w)["field"].(map[string]any)
	if field == nil {
		t.Fatalf("the answer should describe the field: %s", w.Body.String())
	}
	if field["value"] != "LEE" || field["row"] != float64(2) || field["col"] != float64(13) || field["length"] != float64(20) {
		t.Errorf("field = %v, want row 2 col 13 length 20 value LEE", field)
	}
}

func TestScreenWriteHandlerCanOvertype(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 2, "col": 13, "text": "LEE", "erase": false})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := strings.TrimRight(string(mh.Screen.Buffer[2][13:33]), " "); got != "LEETH" {
		t.Errorf("overtyping left %q, want LEETH", got)
	}
}

// Empty text was a silent no-op that answered ok.
func TestScreenWriteHandlerEmptyTextClearsTheField(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 2, "col": 13, "text": ""})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(string(mh.Screen.Buffer[2][13:33])); got != "" {
		t.Errorf("the field still holds %q after being cleared", got)
	}
	field, _ := decodeActionBody(t, w)["field"].(map[string]any)
	if field["value"] != "" {
		t.Errorf("a cleared field should echo an empty value, got %v", field["value"])
	}
}

func TestScreenWriteHandlerRefusesAProtectedTarget(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 2, "col": 3, "text": "X"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a write into a label answered %d: %s", w.Code, w.Body.String())
	}
	msg, _ := decodeActionBody(t, w)["error"].(string)
	if !strings.Contains(msg, "protected") || !strings.Contains(msg, "NAME") {
		t.Errorf("the refusal should say the target is protected and name what is there, got %q", msg)
	}
	if len(mh.Commands) != 0 {
		t.Errorf("a refused write reached the host: %v", mh.Commands)
	}
}

func TestScreenWriteHandlerRefusesTextThatDoesNotFit(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 6, "col": 13, "text": "12345"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("five characters into four cells answered %d: %s", w.Code, w.Body.String())
	}
	// From the middle of the field the room is what is left of it.
	w = doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 6, "col": 15, "text": "123"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("three characters with two cells left answered %d", w.Code)
	}
	if len(mh.Commands) != 0 {
		t.Errorf("a refused write reached the host: %v", mh.Commands)
	}
}

// A hidden field takes the write and the answer does not hand it back.
func TestScreenWriteHandlerMasksAHiddenFieldsValue(t *testing.T) {
	_, _, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 3, "col": 13, "text": "HUNTER2"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "HUNTER2") {
		t.Fatalf("the answer echoes what was typed into a hidden field: %s", w.Body.String())
	}
	field, _ := decodeActionBody(t, w)["field"].(map[string]any)
	if field["value"] != "*******" {
		t.Errorf("a hidden field's value should be masked, got %v", field["value"])
	}
	if hidden, _ := field["hidden"].(bool); !hidden {
		t.Errorf("the field should be marked hidden: %v", field)
	}
}

// --- wait_for_unlock ---------------------------------------------------------

func TestScreenWaitTimeoutIsCappedNotDiscarded(t *testing.T) {
	cases := map[int]time.Duration{
		0:      screenWaitDefaultTimeout,
		-5:     screenWaitDefaultTimeout,
		300:    300 * time.Millisecond,
		15000:  screenWaitMaxTimeout,
		15001:  screenWaitMaxTimeout,
		600000: screenWaitMaxTimeout,
	}
	for in, want := range cases {
		if got := screenWaitTimeout(in); got != want {
			t.Errorf("screenWaitTimeout(%d) = %v, want %v", in, got, want)
		}
	}
}

// An operator error was reported as "unlocked", because the test was "not L".
func TestScreenWaitHandlerReportsAnOperatorErrorAsNotUnlocked(t *testing.T) {
	_, mh, sess, r := newFormApp(t)
	mh.Screen.Status = "E F P C(localhost) I 4 24 80 0 0 0x0 0.000"

	start := time.Now()
	w := doJSON(r, http.MethodPost, "/screen/wait", sess.ID, map[string]int{"timeout_ms": 5000})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := decodeActionBody(t, w)
	if unlocked, _ := body["unlocked"].(bool); unlocked {
		t.Error("an operator error must not be reported as unlocked")
	}
	if body["keyboard"] != "error" {
		t.Errorf("keyboard = %v, want error", body["keyboard"])
	}
	if hint, _ := body["hint"].(string); !strings.Contains(hint, "Reset") {
		t.Errorf("the answer should say Reset releases it, got %q", hint)
	}
	if time.Since(start) > time.Second {
		t.Error("an operator error is not something to wait out; it should answer at once")
	}
}

// The poll loop slept on after the caller had gone.
func TestScreenWaitHandlerStopsWhenTheCallerGoesAway(t *testing.T) {
	_, mh, sess, r := newFormApp(t)
	mh.Screen.Status = "L F P C(localhost) I 4 24 80 0 0 0x0 0.000"

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/screen/wait", strings.NewReader(`{"timeout_ms":15000}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: sess.ID})

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the wait kept polling after its request was cancelled")
	}
}

// --- recording ---------------------------------------------------------------

// A recording made while an assistant drove the terminal had none of the
// assistant's writes or keys in it.
func TestToolDrivenActionsAreRecorded(t *testing.T) {
	_, _, sess, r := newFormApp(t)
	sess.Recording = &session.WorkflowRecording{Active: true}

	if w := doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 2, "col": 13, "text": "LEE"}); w.Code != http.StatusOK {
		t.Fatalf("write: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/screen/key", sess.ID, map[string]any{"key": "PF7"}); w.Code != http.StatusOK {
		t.Fatalf("key: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/screen/submit", sess.ID, nil); w.Code != http.StatusOK {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}

	steps := sess.Recording.Steps
	if len(steps) != 3 {
		t.Fatalf("recorded %d steps, want 3: %+v", len(steps), steps)
	}
	fill := steps[0]
	if fill.Type != "FillString" || fill.Text != "LEE" || fill.Coordinates == nil ||
		fill.Coordinates.Row != 3 || fill.Coordinates.Column != 14 {
		t.Errorf("the write was recorded as %+v (%+v), want FillString LEE at 1-indexed row 3, column 14", fill, fill.Coordinates)
	}
	if steps[1].Type != "PressPF7" || steps[2].Type != "PressEnter" {
		t.Errorf("keys recorded as %q, %q; want PressPF7, PressEnter", steps[1].Type, steps[2].Type)
	}
	if len(sess.Recording.Screens) == 0 {
		t.Error("the screen before each key should be recorded, as it is for a manual submit")
	}

	// A refused write is not a step.
	doJSON(r, http.MethodPost, "/screen/write", sess.ID, map[string]any{"row": 2, "col": 3, "text": "X"})
	if len(sess.Recording.Steps) != 3 {
		t.Errorf("a refused write was recorded: %+v", sess.Recording.Steps)
	}
}

func TestToolDrivenActionsAreNotRecordedWhenNothingIsRecording(t *testing.T) {
	_, _, sess, r := newFormApp(t)
	doJSON(r, http.MethodPost, "/screen/key", sess.ID, map[string]any{"key": "PF7"})
	if sess.Recording != nil {
		t.Errorf("a key press started a recording: %+v", sess.Recording)
	}
}

// --- what the screen JSON says ----------------------------------------------

func fieldAtJSON(t *testing.T, screen map[string]any, row, col int) map[string]any {
	t.Helper()
	fields, _ := screen["fields"].([]any)
	for _, item := range fields {
		f, _ := item.(map[string]any)
		if f["row"] == float64(row) && f["col"] == float64(col) {
			return f
		}
	}
	t.Fatalf("no field at row %d col %d in %v", row, col, fields)
	return nil
}

func TestScreenJSONCarriesLabelsKeyboardAndHash(t *testing.T) {
	_, mh, sess, r := newFormApp(t)

	w := doJSON(r, http.MethodGet, "/screen.json", sess.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	screen := decodeActionBody(t, w)

	if screen["keyboard"] != "unlocked" {
		t.Errorf("keyboard = %v, want unlocked", screen["keyboard"])
	}
	if hash, _ := screen["screenHash"].(string); hash == "" {
		t.Error("the screen should carry its hash")
	}
	if _, known := screen["knownScreen"]; known {
		t.Error("a screen exploration has never seen must not claim to be known")
	}

	// Same row, dot leaders and colon stripped.
	if got := fieldAtJSON(t, screen, 2, 13)["label"]; got != "NAME" {
		t.Errorf("the name field's label = %v, want NAME", got)
	}
	if got := fieldAtJSON(t, screen, 3, 13)["label"]; got != "PIN" {
		t.Errorf("the hidden field's label = %v, want PIN", got)
	}
	// Nothing to its left: the heading on the row above.
	if got := fieldAtJSON(t, screen, 6, 13)["label"]; got != "AGE" {
		t.Errorf("the age field's label = %v, want AGE (from the row above)", got)
	}
	// Protected fields are the labels; they do not get one.
	if _, has := fieldAtJSON(t, screen, 0, 1)["label"]; has {
		t.Error("a protected field should not be given a label")
	}

	// Hidden values stay out of every part of it.
	if strings.Contains(w.Body.String(), "SECRET") {
		t.Errorf("the screen JSON discloses a hidden field's value: %s", w.Body.String())
	}

	mh.Screen.Status = "L F P C(localhost) I 4 24 80 0 0 0x0 0.000"
	if got := decodeActionBody(t, doJSON(r, http.MethodGet, "/screen.json", sess.ID, nil))["keyboard"]; got != "locked" {
		t.Errorf("keyboard = %v, want locked", got)
	}
	mh.Screen.Status = "E F P C(localhost) I 4 24 80 0 0 0x0 0.000"
	if got := decodeActionBody(t, doJSON(r, http.MethodGet, "/screen.json", sess.ID, nil))["keyboard"]; got != "error" {
		t.Errorf("keyboard = %v, want error", got)
	}
}

// discover files the form screen in the session's mind map the way a default
// exploration run would, with annotations.
func discover(t *testing.T, app *App, sess *session.Session, screen *host.Screen) string {
	t.Helper()
	hash := chaos.StructuralScreenHash(screen)
	if hash == "" {
		t.Fatal("the form screen has no structural hash")
	}
	app.chaosEngines.setLoadedRun(sess.ID, &chaos.SavedRun{MindMap: &chaos.MindMap{
		Areas: map[string]*chaos.MindMapArea{
			hash: {
				Hash:            hash,
				Label:           "CUSTOMER MAINTENANCE",
				Visits:          4,
				ScreenWidth:     80,
				ScreenHeight:    24,
				InputFieldCount: 3,
				BusinessPurpose: "Maintain a customer record",
				BusinessNotes:   "PF5 saves",
				PreviewText:     " CUSTOMER MAINTENANCE\n\n NAME . . :  SMITH\n PIN         SECRET",
				FieldMetadata: map[string]chaos.MindMapFieldMetadata{
					"R3C14L20": {Row: 3, Column: 14, Length: 20},
					"R4C14L8":  {Row: 4, Column: 14, Length: 8, Hidden: true},
				},
				FieldSemantics: map[string]chaos.BusinessFieldSemantic{
					"R3C14L20": {Name: "customer_name", Example: "SMITH"},
					"R4C14L8":  {Name: "pin", Example: "SECRET"},
				},
				KnownWorkingValues: map[string][]string{
					"R3C14L20": {"SMITH"},
					"R4C14L8":  {"SECRET"},
				},
				KeyPresses: map[string]*chaos.MindMapKeyPress{
					"PF(5)": {Presses: 3, Progressions: 2, Destinations: map[string]int{"savedhash": 2}},
					"PF(9)": {Presses: 4, Progressions: 0},
				},
			},
			"savedhash": {Hash: "savedhash", Label: "RECORD SAVED"},
		},
	}})
	return hash
}

func TestScreenJSONRecognisesADiscoveredScreen(t *testing.T) {
	app, mh, sess, r := newFormApp(t)
	hash := discover(t, app, sess, mh.Screen)

	// What is typed into the fields does not change which screen it is.
	copy(mh.Screen.Buffer[2][13:], []rune("JONES"))

	w := doJSON(r, http.MethodGet, "/screen.json", sess.ID, nil)
	screen := decodeActionBody(t, w)
	if screen["screenHash"] != hash {
		t.Errorf("screenHash = %v, want the discovered area's key %s", screen["screenHash"], hash)
	}
	known, _ := screen["knownScreen"].(map[string]any)
	if known == nil {
		t.Fatalf("the screen should be recognised: %s", w.Body.String())
	}
	if known["label"] != "CUSTOMER MAINTENANCE" || known["businessPurpose"] != "Maintain a customer record" {
		t.Errorf("knownScreen = %v", known)
	}
	if keys, _ := known["productiveKeys"].(map[string]any); keys["PF(5)"] == nil || keys["PF(9)"] != nil {
		t.Errorf("productiveKeys should hold PF(5) and not the dead PF(9), got %v", known["productiveKeys"])
	}
	if known["fieldSemantics"] == nil {
		t.Errorf("knownScreen should carry field semantics: %v", known)
	}
	if strings.Contains(w.Body.String(), "SECRET") {
		t.Errorf("a recognised screen discloses a hidden field's learned value: %s", w.Body.String())
	}

	// The same identity travels with the result of an action.
	w = doJSON(r, http.MethodPost, "/screen/key", sess.ID, map[string]any{"key": "Enter"})
	after, _ := decodeActionBody(t, w)["screen"].(map[string]any)
	if after["screenHash"] != hash || after["knownScreen"] == nil {
		t.Errorf("a key result should carry screenHash and knownScreen, got %v", after)
	}
}

func TestKnownScreenHandler(t *testing.T) {
	app, mh, sess, r := newFormApp(t)

	if w := doJSON(r, http.MethodGet, "/copilot/known-screen?hash=nope", sess.ID, nil); w.Code != http.StatusNotFound {
		t.Errorf("with nothing discovered it answered %d, want 404", w.Code)
	}

	hash := discover(t, app, sess, mh.Screen)

	if w := doJSON(r, http.MethodGet, "/copilot/known-screen?hash=nope", sess.ID, nil); w.Code != http.StatusNotFound {
		t.Errorf("an unknown hash answered %d, want 404", w.Code)
	}

	w := doJSON(r, http.MethodGet, "/copilot/known-screen?hash="+hash, sess.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := decodeActionBody(t, w)
	if body["label"] != "CUSTOMER MAINTENANCE" || body["businessPurpose"] != "Maintain a customer record" || body["notes"] != "PF5 saves" {
		t.Errorf("label/purpose/notes wrong: %v", body)
	}

	fields, _ := body["fields"].([]any)
	if len(fields) != 2 {
		t.Fatalf("want 2 fields, got %v", fields)
	}
	name, _ := fields[0].(map[string]any)
	if name["fieldKey"] != "R3C14L20" || name["row"] != float64(2) || name["col"] != float64(13) {
		t.Errorf("the name field should come first with 0-indexed row 2 col 13, got %v", name)
	}
	if values, _ := name["knownWorkingValues"].([]any); len(values) != 1 || values[0] != "SMITH" {
		t.Errorf("a visible field keeps its learned values, got %v", name["knownWorkingValues"])
	}
	pin, _ := fields[1].(map[string]any)
	if pin["knownWorkingValues"] != nil {
		t.Errorf("a hidden field's learned values must not be returned, got %v", pin["knownWorkingValues"])
	}
	if sem, _ := pin["semantics"].(map[string]any); sem["example"] != nil && sem["example"] != "" {
		t.Errorf("a hidden field's example must not be returned, got %v", sem)
	}

	keys, _ := body["productiveKeys"].(map[string]any)
	pf5, _ := keys["PF(5)"].(map[string]any)
	dests, _ := pf5["destinations"].([]any)
	if len(dests) != 1 {
		t.Fatalf("PF(5) should list one destination, got %v", keys)
	}
	if dest, _ := dests[0].(map[string]any); dest["hash"] != "savedhash" || dest["label"] != "RECORD SAVED" {
		t.Errorf("destination = %v, want savedhash / RECORD SAVED", dests[0])
	}
	if dead, _ := body["deadKeys"].([]any); len(dead) != 1 || dead[0] != "PF(9)" {
		t.Errorf("deadKeys = %v, want [PF(9)]", body["deadKeys"])
	}

	preview, _ := body["preview"].(string)
	if !strings.Contains(preview, "SMITH") || strings.Contains(preview, "SECRET") || !strings.Contains(preview, "******") {
		t.Errorf("the preview should keep visible values and mask hidden ones, got:\n%s", preview)
	}
	if strings.Contains(w.Body.String(), "SECRET") {
		t.Errorf("the screen detail discloses a hidden value somewhere: %s", w.Body.String())
	}

	// With no hash it answers for the screen the session is on.
	w = doJSON(r, http.MethodGet, "/copilot/known-screen", sess.ID, nil)
	if w.Code != http.StatusOK || decodeActionBody(t, w)["hash"] != hash {
		t.Errorf("with no hash it should answer for the current screen, got %d %s", w.Code, w.Body.String())
	}
}

// --- the context block -------------------------------------------------------

func TestContextKeepsHostTextOutOfTheTrustedPart(t *testing.T) {
	app, mh, sess, r := newFormApp(t)
	hash := discover(t, app, sess, mh.Screen)

	w := doJSON(r, http.MethodGet, "/copilot/context", sess.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := decodeActionBody(t, w)
	trusted, _ := body["text"].(string)
	hostContext, _ := body["hostContext"].(string)

	// Nothing a host painted, and nothing recorded about a host, is in the
	// part that sits beside the system prompt.
	for _, hostText := range []string{"CUSTOMER MAINTENANCE", "SMITH", "NAME", "Maintain a customer record", "customer_name"} {
		if strings.Contains(trusted, hostText) {
			t.Errorf("the trusted part contains host-derived text %q:\n%s", hostText, trusted)
		}
	}
	for _, want := range []string{"CUSTOMER MAINTENANCE", "SMITH", "label: NAME", "Maintain a customer record", "customer_name", "PF(5)"} {
		if !strings.Contains(hostContext, want) {
			t.Errorf("hostContext is missing %q:\n%s", want, hostContext)
		}
	}
	if !strings.Contains(trusted, hash) || !strings.Contains(trusted, "unlocked") {
		t.Errorf("the trusted part should give the screen hash and the keyboard state:\n%s", trusted)
	}
	if body["screenHash"] != hash || body["keyboard"] != "unlocked" || body["knownScreen"] == nil || body["connected"] != true {
		t.Errorf("the structured fields are wrong: %v", body)
	}
	if strings.Contains(w.Body.String(), "SECRET") {
		t.Errorf("the context discloses a hidden field's value: %s", w.Body.String())
	}
	if !strings.Contains(hostContext, "******") {
		t.Errorf("the hidden field should appear masked in the screen text:\n%s", hostContext)
	}
}

// The title line was cut from the raw buffer, so a hidden field on the first
// non-blank row was quoted in clear.
func TestContextTitleDoesNotDiscloseAHiddenField(t *testing.T) {
	_, mh, sess, r := newFormApp(t)
	s := &host.Screen{Width: 80, Height: 24, IsFormatted: true, Status: unlockedStatus}
	s.Buffer = make([][]rune, s.Height)
	for y := range s.Buffer {
		s.Buffer[y] = []rune(strings.Repeat(" ", s.Width))
	}
	copy(s.Buffer[0][1:], []rune("HUNTER2"))
	s.Fields = []*host.Field{
		host.NewField(s, host.AttrDisp1|host.AttrDisp2, 1, 0, 8, 0, 0, 0),
		host.NewField(s, host.AttrProtected, 10, 0, 79, 0, 0, 0),
	}
	mh.Screen = s

	w := doJSON(r, http.MethodGet, "/copilot/context", sess.ID, nil)
	if strings.Contains(w.Body.String(), "HUNTER2") {
		t.Errorf("the context discloses a hidden field's value: %s", w.Body.String())
	}
}

func TestContextWithNoSessionIsEmptyAndValid(t *testing.T) {
	_, _, _, r := newFormApp(t)
	w := doJSON(r, http.MethodGet, "/copilot/context", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := decodeActionBody(t, w)
	if body["text"] != "" || body["hostContext"] != "" {
		t.Errorf("want empty text and hostContext, got %v", body)
	}
}
