// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/host"
)

// A terminal whose host has hung up stays running: its subprocess is healthy
// and it answers every request, with a refusal. These doubles answer the way
// the real one does, so the handlers are exercised on what they actually meet.

const droppedStatusLine = "L F U N N 2 24 80 0 11 0x0 0.000"

func errWriteRefused() error {
	return &host.ActionError{Command: `String("ADMIN")`, Status: droppedStatusLine, Detail: "Canceled"}
}

func errRedialRefused() error {
	return &host.ActionError{Command: "Connect(host.example:23)", Status: droppedStatusLine, Detail: "Connection failed"}
}

// droppedHost can be told to hang up, after which every write and read fails
// the way a real terminal's does.
type droppedHost struct {
	*host.MockHost
	dead *atomic.Bool
}

func newDroppedHost(t *testing.T, dead bool) droppedHost {
	t.Helper()
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	flag := &atomic.Bool{}
	flag.Store(dead)
	return droppedHost{MockHost: mh, dead: flag}
}

func (h droppedHost) UpdateScreen() error {
	if h.dead.Load() {
		return errRedialRefused()
	}
	return h.MockHost.UpdateScreen()
}

func (h droppedHost) SubmitOperatorInput(edit func(*host.Screen) string) error {
	if h.dead.Load() {
		return errWriteRefused()
	}
	return h.MockHost.SubmitOperatorInput(edit)
}

func (h droppedHost) SendKey(key string) error {
	if h.dead.Load() {
		return errWriteRefused()
	}
	return h.MockHost.SendKey(key)
}

// brokenHost fails in a way that is not a lost connection.
type brokenHost struct{ *host.MockHost }

func (h brokenHost) SubmitOperatorInput(func(*host.Screen) string) error {
	return &host.ActionError{Command: `String("A")`, Status: "L F U C(h) I 2 24 80 0 11 0x0 0.000", Detail: "Keyboard locked"}
}

// lostHostRouter is the full router with one session open on h, and the
// cookie that addresses it.
func lostHostRouter(t *testing.T, h host.Host) (*App, *gin.Engine, *http.Cookie) {
	t.Helper()
	app, r := newAuthTestApp(t, "none")
	sess := app.SessionManager.CreateSessionFor(authz.LocalUserID, h)
	sess.TargetHost, sess.TargetPort = "host.example", 23
	return app, r, &http.Cookie{Name: sessionCookieName, Value: sess.ID}
}

func postForm(r *gin.Engine, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Host = "example.test"
	req.RemoteAddr = "10.0.0.1:40000"
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func getWithCookie(r *gin.Engine, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "10.0.0.1:40000"
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func jsonBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON (status %d): %s", w.Code, w.Body.String())
	}
	return body
}

// Pressing Enter on a host that has gone used to answer with a bare 500 and an
// error string naming an s3270 command; the page could do nothing with that but
// give up and navigate to an error page. The answer is now one the page can
// act on.
func TestSubmitAsync_ALostHostIsAStructuredError(t *testing.T) {
	_, r, cookie := lostHostRouter(t, newDroppedHost(t, true))

	w := postForm(r, "/submit/async", url.Values{"key": {"Enter"}}, cookie)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
	body := jsonBody(t, w)
	if body["code"] != "host_disconnected" {
		t.Errorf("code = %v, want host_disconnected (body %s)", body["code"], w.Body.String())
	}
	message, _ := body["error"].(string)
	if !strings.Contains(strings.ToLower(message), "connection") || strings.Contains(message, "s3270") {
		t.Errorf("error = %q, want a plain statement that the connection was lost", message)
	}
}

// A failure that is not a lost connection stays a failure, with no code that
// would send the page into reconnecting.
func TestSubmitAsync_OtherFailuresAreNotReportedAsALostHost(t *testing.T) {
	mh, _ := host.NewMockHost("")
	mh.Connected = true
	_, r, cookie := lostHostRouter(t, brokenHost{mh})

	w := postForm(r, "/submit/async", url.Values{"key": {"Enter"}}, cookie)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if code := jsonBody(t, w)["code"]; code != nil {
		t.Errorf("code = %v, want none for a failure that is not a lost connection", code)
	}
}

// The full-page submit is what the terminal falls back to, and what a form
// posted with scripting off lands on. It must not say the session is fine.
func TestSubmitPage_ALostHostIsNamedAsSuch(t *testing.T) {
	_, r, cookie := lostHostRouter(t, newDroppedHost(t, true))

	w := postForm(r, "/submit", url.Values{"key": {"Enter"}}, cookie)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "connection to the host was lost") {
		t.Errorf("the page does not say the connection was lost:\n%s", body)
	}
	if strings.Contains(body, "Something went wrong") {
		t.Errorf("a lost host is titled as a crash")
	}
}

// The generic page used to end "Your session has not been disconnected", which
// it had no way of knowing and which was false for exactly the failures that
// brought somebody here.
func TestGenericErrorPageDoesNotClaimTheSessionIsIntact(t *testing.T) {
	mh, _ := host.NewMockHost("")
	mh.Connected = true
	_, r, cookie := lostHostRouter(t, brokenHost{mh})

	w := postForm(r, "/submit", url.Values{"key": {"Enter"}}, cookie)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(strings.ToLower(body), "has not been disconnected") {
		t.Errorf("the error page still claims the session is intact:\n%s", body)
	}
	for _, want := range []string{"Return to terminal", "Choose a connection"} {
		if !strings.Contains(body, want) {
			t.Errorf("the error page lost its way out: missing %q", want)
		}
	}
}

// The refresh after a submit is the other request that meets the dead host.
func TestScreenContent_ALostHostIsAStructuredError(t *testing.T) {
	_, r, cookie := lostHostRouter(t, newDroppedHost(t, true))

	w := getWithCookie(r, "/screen/content", cookie)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", w.Code, w.Body.String())
	}
	if code := jsonBody(t, w)["code"]; code != "host_disconnected" {
		t.Errorf("code = %v, want host_disconnected", code)
	}
}

// A page opened on a host that is already gone still draws, from the last
// screen the session saw, and says so in the status area where the Reconnect
// control lives, rather than showing an error page with nothing to press.
func TestScreenPage_ALostHostStillDrawsAndSaysDisconnected(t *testing.T) {
	_, r, cookie := lostHostRouter(t, newDroppedHost(t, true))

	w := getWithCookie(r, "/screen", cookie)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %.300s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "X DISCONNECTED") {
		t.Errorf("the status area does not say the host is disconnected")
	}
	if !strings.Contains(w.Body.String(), "data-reconnect") {
		t.Errorf("the page has no Reconnect control")
	}
}

// The idle stream is the page's only way of noticing a host that dropped while
// nobody was typing. A browser treats a stream that answers with anything but
// 200 as failed for good and says so, which is what sends the page into its
// reconnect handling; so a stream opened against a dead host answers 409.
func TestScreenStream_RefusesAHostThatIsAlreadyGone(t *testing.T) {
	_, r, cookie := lostHostRouter(t, newDroppedHost(t, true))

	w := getWithCookie(r, "/screen/stream", cookie)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if strings.Contains(w.Header().Get("Content-Type"), "event-stream") {
		t.Errorf("a dead host was answered with an event stream")
	}
	if code := jsonBody(t, w)["code"]; code != "host_disconnected" {
		t.Errorf("code = %v, want host_disconnected", code)
	}
}

// And one that drops while it is open ends: the browser then reconnects, and
// the reconnect is refused as above. Without this the stream polled a dead host
// every 700 ms for ever and never said a word.
func TestScreenStream_EndsWhenTheHostDrops(t *testing.T) {
	h := newDroppedHost(t, false)
	_, r, cookie := lostHostRouter(t, h)

	srv := httptest.NewServer(r)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/screen/stream", nil)
	req.AddCookie(cookie)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 while the host is up", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	// The first screen arrives.
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended before the first screen: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			break
		}
	}

	h.dead.Store(true)

	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, reader)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the stream ended with an error rather than cleanly: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream was still open five seconds after the host dropped")
	}
}

// The classification is the whole of the server's decision, so it is pinned on
// its own as well.
func TestLostHostOr(t *testing.T) {
	mh, _ := host.NewMockHost("")
	mh.Connected = true

	if err := lostHostOr(mh, errWriteRefused()); !errors.Is(err, errHostDisconnected) {
		t.Errorf("a write refused while not connected = %v, want errHostDisconnected", err)
	}
	if err := lostHostOr(mh, errors.New("boom")); errors.Is(err, errHostDisconnected) {
		t.Errorf("an ordinary failure was taken for a lost host")
	}
	// A subprocess that has gone is a lost host whatever it said on the way.
	gone, _ := host.NewMockHost("")
	gone.Connected = false
	if err := lostHostOr(gone, errors.New("boom")); !errors.Is(err, errHostDisconnected) {
		t.Errorf("a host that is not running = %v, want errHostDisconnected", err)
	}
}
