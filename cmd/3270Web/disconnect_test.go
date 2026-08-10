package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/session"
)

func TestDisconnectMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup minimal app
	app := &App{
		SessionManager: session.NewManager(),
	}

	r := gin.New()
	// Register the handler as per the changes
	r.POST("/disconnect", app.DisconnectHandler)

	// Enable HandleMethodNotAllowed to return 405 instead of 404
	r.HandleMethodNotAllowed = true

	// Test GET request (should fail with 405 Method Not Allowed)
	t.Run("GET request rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/disconnect", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("Expected 405 Method Not Allowed for GET /disconnect, got %d", w.Code)
		}
	})

	// Test POST request (should succeed)
	t.Run("POST request allowed", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/disconnect", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// DisconnectHandler redirects to the landing page, marked so that the
		// page does not simply reconnect; see TestDisconnectDoesNotLandBackOnTheHost.
		if w.Code != http.StatusFound {
			t.Errorf("Expected 302 for POST /disconnect, got %d", w.Code)
		}
		if loc := w.Header().Get("Location"); loc != homeAfterDisconnect() {
			t.Errorf("Expected redirect to %s, got %s", homeAfterDisconnect(), loc)
		}
	})
}

// Disconnect ended the session and then landed the operator on the page that
// decides where this account belongs — which, for an account assigned exactly
// one host, dialled it again. Offering a single preset puts every account in
// that state, so the button ended a session and looked like it did nothing.
func TestDisconnectDoesNotLandBackOnTheHost(t *testing.T) {
	if !justDisconnected(requestFor(t, homeAfterDisconnect())) {
		t.Fatalf("%s is not recognised as the end of a disconnect", homeAfterDisconnect())
	}
	if justDisconnected(requestFor(t, "/")) {
		t.Error("an ordinary visit to the landing page reads as a disconnect")
	}
}

func requestFor(t *testing.T, target string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c
}

// The same thing from the outside, against the auto-connect target: without
// the marker the landing page dials it (and reports the failure), with it the
// page is simply the connect form.
func TestTheLandingPageDoesNotDialAfterADisconnect(t *testing.T) {
	app, r := newAuthTestApp(t, "none")
	// A host nothing is listening on, so the attempt fails fast and is
	// visible in the response rather than needing a mainframe.
	app.Config.TargetHost.Value = "127.0.0.1:1"
	app.Config.TargetHost.AutoConnect = true

	dialled := httptest.NewRecorder()
	r.ServeHTTP(dialled, httptest.NewRequest(http.MethodGet, "/", nil))
	if dialled.Code != http.StatusServiceUnavailable {
		t.Fatalf("an ordinary visit returned %d, want the landing page to have tried to connect", dialled.Code)
	}

	after := httptest.NewRecorder()
	r.ServeHTTP(after, httptest.NewRequest(http.MethodGet, homeAfterDisconnect(), nil))
	if after.Code != http.StatusOK {
		t.Errorf("the page after a disconnect returned %d, want the connect form", after.Code)
	}
	if app.SessionManager.Count() != 0 {
		t.Errorf("a disconnect left %d session(s) open", app.SessionManager.Count())
	}
}

// Closing the last tab is the same ending by a different control, so it has
// to carry the same marker — otherwise the × on the final tab reconnects.
func TestClosingTheLastTabLandsWhereADisconnectDoes(t *testing.T) {
	src := staticSource(t, "session-tabs.js")
	if !strings.Contains(src, `"/?disconnected=1"`) {
		t.Error("session-tabs.js: closing the last session no longer marks the landing page as a disconnect, " +
			"so an account with one assigned host is reconnected the moment it closes its last tab")
	}
}

// PF3 on the selection screen is "I did not want any of these". Landing on
// the page that draws that menu would make signing off redisplay it.
func TestSigningOffTheSelectionScreenDoesNotRedrawIt(t *testing.T) {
	src := readSource(t, "cmd/3270Web/main.go")
	marker := "// settleSelection ended the session — PF3 on the menu"
	at := strings.Index(src, marker)
	if at < 0 {
		t.Fatal("main.go: the PF3 branch is gone")
	}
	tail := src[at : at+400]
	if !strings.Contains(tail, "homeAfterDisconnect()") {
		t.Error("main.go: signing off the selection screen lands on the page that draws it again")
	}
}

// The terminal submits without navigating and then asks for the screen, so
// that request is the only one most operators ever make. Settling the menu
// choice only on the next submit left somebody who had picked a system
// looking at "the session opens in a moment" until they pressed another key.
func TestChoosingASystemSettlesOnTheScreenTheTerminalAsksFor(t *testing.T) {
	src := readSource(t, "cmd/3270Web/main.go")
	at := strings.Index(src, "func (app *App) ScreenContentHandler(")
	if at < 0 {
		t.Fatal("main.go: ScreenContentHandler is gone")
	}
	body := src[at : at+1200]
	if !strings.Contains(body, "app.settleSelection(c, s)") {
		t.Error("main.go: the screen the terminal fetches after a keystroke no longer settles a " +
			"selection, so choosing a system waits for the key after it")
	}
	// Checked as "reads the host under the lock, then decides on that value"
	// rather than as the literal `s.Host == nil` this once pinned. That
	// expression was itself the bug: an unlocked read racing the swap in
	// resetSessionHost. Pinning the safe form keeps the behaviour this test
	// exists for without inviting the race back.
	if !strings.Contains(body, "h := app.sessionHost(s)") || !strings.Contains(body, "if h == nil {") {
		t.Error("main.go: ScreenContentHandler does not notice that settling ended the session, " +
			"or reads s.Host without the session lock")
	}
}

// A key pressed against a session that no longer exists must not be answered
// with a brand new one — which is how signing off the selection screen came
// back to the selection screen.
func TestSubmittingWithNoSessionDoesNotStartOne(t *testing.T) {
	app := &App{SessionManager: session.NewManager()}
	r := gin.New()
	r.POST("/submit", app.SubmitHandler)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/submit", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("submitting with no session returned %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != homeAfterDisconnect() {
		t.Errorf("submitting with no session redirects to %q, want %q", loc, homeAfterDisconnect())
	}
}

func readSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
