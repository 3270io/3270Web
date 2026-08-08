package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// snapshotTestApp returns an App with one connected mock session and a router
// carrying the whole token surface, plus the mock so a test can change what is
// on the screen between snapshots.
func snapshotTestApp(t *testing.T) (*App, *gin.Engine, *session.Session, *host.MockHost, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	const token = "test-token"
	t.Setenv("API_TOKEN", token)

	app := &App{
		SessionManager: session.NewManager(),
		chaosEngines:   newChaosEngineStore(),
		baseDir:        t.TempDir(),
	}

	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	mock.Connected = true
	sess := app.SessionManager.CreateSession(mock)

	r := gin.New()
	app.registerAPIv1(r)
	return app, r, sess, mock, token
}

// doToken issues a bearer-token request with a raw JSON body. Distinct from
// doJSON in copilot_tools_test.go, which authenticates with a session cookie.
func doToken(t *testing.T, r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSnapshotRoundTripAndDiffAgainstLiveScreen(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	base := "/api/v1/sessions/" + sess.ID

	if err := mock.WriteStringAt(0, 0, "BALANCE 100"); err != nil {
		t.Fatalf("seed screen: %v", err)
	}
	if w := doToken(t, r, http.MethodPost, base+"/snapshots", token, `{"name":"before"}`); w.Code != http.StatusCreated {
		t.Fatalf("take snapshot: status = %d (body %s)", w.Code, w.Body.String())
	}

	// The screen moves on, as it would after a transaction.
	if err := mock.WriteStringAt(0, 0, "BALANCE 250"); err != nil {
		t.Fatalf("change screen: %v", err)
	}

	// No "right" means "compare with the screen as it stands now" — the shape
	// the feature is actually used in.
	w := doToken(t, r, http.MethodPost, base+"/snapshots/diff", token, `{"left":"before"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("diff: status = %d (body %s)", w.Code, w.Body.String())
	}
	var got struct {
		Identical bool `json:"identical"`
		Lines     []struct {
			Row   int    `json:"row"`
			Left  string `json:"left"`
			Right string `json:"right"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode diff: %v (body %s)", err, w.Body.String())
	}
	if got.Identical {
		t.Fatalf("a changed screen diffed as identical: %s", w.Body.String())
	}
	if len(got.Lines) != 1 {
		t.Fatalf("Lines = %+v, want exactly the row that changed", got.Lines)
	}
	if !strings.Contains(got.Lines[0].Left, "BALANCE 100") || !strings.Contains(got.Lines[0].Right, "BALANCE 250") {
		t.Errorf("diff should carry both versions of the row, got %+v", got.Lines[0])
	}
}

func TestSnapshotDiffOfTwoStoredSnapshots(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	base := "/api/v1/sessions/" + sess.ID

	_ = mock.WriteStringAt(0, 0, "READY")
	doToken(t, r, http.MethodPost, base+"/snapshots", token, `{"name":"one"}`)
	doToken(t, r, http.MethodPost, base+"/snapshots", token, `{"name":"two"}`)

	w := doToken(t, r, http.MethodPost, base+"/snapshots/diff", token, `{"left":"one","right":"two"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("diff: status = %d (body %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"identical":true`) {
		t.Errorf("two captures of an unchanged screen should be identical, got %s", w.Body.String())
	}
}

func TestSnapshotListAndDelete(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	base := "/api/v1/sessions/" + sess.ID

	doToken(t, r, http.MethodPost, base+"/snapshots", token, `{"name":"kept"}`)

	w := doToken(t, r, http.MethodGet, base+"/snapshots", token, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kept"`) {
		t.Fatalf("list: status = %d body %s", w.Code, w.Body.String())
	}
	if w := doToken(t, r, http.MethodGet, base+"/snapshots?name=kept", token, ""); w.Code != http.StatusOK {
		t.Errorf("fetch by name: status = %d (body %s)", w.Code, w.Body.String())
	}
	if w := doToken(t, r, http.MethodGet, base+"/snapshots?name=absent", token, ""); w.Code != http.StatusNotFound {
		t.Errorf("fetch of an absent snapshot: status = %d, want 404", w.Code)
	}

	if w := doToken(t, r, http.MethodDelete, base+"/snapshots?name=kept", token, ""); w.Code != http.StatusOK {
		t.Errorf("delete: status = %d (body %s)", w.Code, w.Body.String())
	}
	// Deleting twice succeeds: a retried request must not report a failure for
	// a delete that already took effect.
	if w := doToken(t, r, http.MethodDelete, base+"/snapshots?name=kept", token, ""); w.Code != http.StatusOK {
		t.Errorf("repeat delete: status = %d, want 200", w.Code)
	}
	if w := doToken(t, r, http.MethodGet, base+"/snapshots?name=kept", token, ""); w.Code != http.StatusNotFound {
		t.Errorf("deleted snapshot still readable: status = %d", w.Code)
	}
}

func TestSnapshotNameIsValidated(t *testing.T) {
	_, r, sess, _, token := snapshotTestApp(t)
	base := "/api/v1/sessions/" + sess.ID

	for _, body := range []string{
		`{"name":""}`,
		`{"name":"../escape"}`,
		`{"name":"has/slash"}`,
		`{"name":"-starts-with-punctuation"}`,
		`{"name":"` + strings.Repeat("x", maxSnapshotNameLen+1) + `"}`,
	} {
		if w := doToken(t, r, http.MethodPost, base+"/snapshots", token, body); w.Code != http.StatusBadRequest {
			t.Errorf("POST %s: status = %d, want 400", body, w.Code)
		}
	}

	// Surrounding whitespace is trimmed rather than refused — the name is a
	// map key, not a path, and "before " and "before" are the same intent.
	if w := doToken(t, r, http.MethodPost, base+"/snapshots", token, `{"name":" before "}`); w.Code != http.StatusCreated {
		t.Errorf("padded name: status = %d, want 201 (body %s)", w.Code, w.Body.String())
	}
	if w := doToken(t, r, http.MethodGet, base+"/snapshots?name=before", token, ""); w.Code != http.StatusOK {
		t.Errorf("trimmed name not readable back: status = %d", w.Code)
	}
}

func TestSnapshotsAreBoundedPerSession(t *testing.T) {
	app, r, sess, _, token := snapshotTestApp(t)
	base := "/api/v1/sessions/" + sess.ID

	for i := 0; i < maxSnapshotsPerSession; i++ {
		body := `{"name":"snap` + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `"}`
		if w := doToken(t, r, http.MethodPost, base+"/snapshots", token, body); w.Code != http.StatusCreated {
			t.Fatalf("snapshot %d: status = %d (body %s)", i, w.Code, w.Body.String())
		}
	}
	w := doToken(t, r, http.MethodPost, base+"/snapshots", token, `{"name":"one too many"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("past the ceiling: status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
	// Replacing one already held is still allowed at the ceiling — re-capturing
	// "before" at the top of a loop is the ordinary way to use this.
	if w := doToken(t, r, http.MethodPost, base+"/snapshots", token, `{"name":"snapaa"}`); w.Code != http.StatusCreated {
		t.Errorf("replacing an existing snapshot at the ceiling: status = %d (body %s)", w.Code, w.Body.String())
	}
	if n := len(app.snapshots().list(sess.ID)); n != maxSnapshotsPerSession {
		t.Errorf("session holds %d snapshots, want %d", n, maxSnapshotsPerSession)
	}
}

// A session ending takes its snapshots with it; otherwise the store keeps a
// screen's worth of text per snapshot for the life of the process.
func TestSnapshotsAreDroppedWhenTheSessionEnds(t *testing.T) {
	app, r, sess, _, token := snapshotTestApp(t)
	doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/snapshots", token, `{"name":"held"}`)

	app.cleanupSession(sess)

	if n := len(app.snapshots().list(sess.ID)); n != 0 {
		t.Errorf("session cleanup left %d snapshots behind", n)
	}
}

func TestSnapshotOnDisconnectedSessionIs409(t *testing.T) {
	_, r, sess, mock, token := snapshotTestApp(t)
	mock.Connected = false

	w := doToken(t, r, http.MethodPost, "/api/v1/sessions/"+sess.ID+"/snapshots", token, `{"name":"x"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
}
