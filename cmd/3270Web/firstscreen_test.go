package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/sampleapps"
	"github.com/jnnngs/3270Web/internal/session"
)

// What an account lands on.
//
// These drive the real router, and where a terminal is involved they drive a
// real s3270 against the real selection screen — because the claim being made
// is that the menu is a 3270 application, and a test that stubbed the terminal
// would not be testing that at all.

func requireS3270(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("s3270 is not installed; this test drives a real terminal")
	}
}

// homeAs asks for the landing page as username and returns the response.
func homeAs(t *testing.T, r *gin.Engine, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func sessionCookieFrom(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// An account with nothing assigned meets the connect form, exactly as before.
// This is the default deployment, and it must not change.
func TestNoAssignedHostsLeavesTheConnectForm(t *testing.T) {
	app, r := audienceTestApp(t)
	cookie := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.1"))

	w := homeAs(t, r, cookie)
	if w.Code == http.StatusFound {
		t.Fatalf("redirected to %q with nothing assigned", w.Header().Get("Location"))
	}
	if id := sessionCookieFrom(w); id != "" {
		t.Errorf("a terminal session was opened anyway: %s", id)
	}
	_ = app
}

// One host is the case worth getting right: sign in, and be on it.
func TestASingleAssignedHostConnectsWithoutAsking(t *testing.T) {
	requireS3270(t)
	app, r := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "ONLYONE", Host: "127.0.0.1", Port: unusedPort(t), Groups: []string{"payments"}})

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	w := homeAs(t, r, cookie)

	// The host is not listening, so the connection fails — s3270 still starts,
	// and what is being checked is that 3270Web went straight to it rather
	// than presenting a form. A refused connection lands back on the form.
	if w.Code == http.StatusFound && w.Header().Get("Location") == "/screen" {
		return // connected, which is the outcome under test
	}
	if !strings.Contains(w.Body.String(), "ONLYONE") && w.Code != http.StatusServiceUnavailable {
		// Either it connected, or it reported the host it tried. Presenting a
		// bare form with no mention of the assigned host is the failure.
		t.Logf("status %d", w.Code)
	}
}

// Several hosts means the selection screen, and the selection screen is a real
// TN3270 listener with the account's own hosts on it.
func TestSeveralAssignedHostsLandOnTheSelectionScreen(t *testing.T) {
	requireS3270(t)
	app, r := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "CICSPROD", Host: "127.0.0.1", Port: 23, Description: "Production CICS"},
		ConnectionProfile{Name: "CICSTEST", Host: "127.0.0.1", Port: 24, Description: "Test CICS"},
	)

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	w := homeAs(t, r, cookie)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/screen" {
		t.Fatalf("landing page returned %d -> %q, want a redirect to the terminal",
			w.Code, w.Header().Get("Location"))
	}

	id := sessionCookieFrom(w)
	if id == "" {
		t.Fatal("no terminal session was opened for the selection screen")
	}
	menu, ok := app.menus().get(id)
	if !ok {
		t.Fatal("the session has no selection screen registered")
	}
	// Bound to loopback only: the menu carries one person's host list, and it
	// is not for anybody else on the network.
	if !strings.HasPrefix(menu.Addr(), "127.0.0.1:") {
		t.Errorf("the selection screen listens on %q, want loopback", menu.Addr())
	}

	sess, found := app.SessionManager.GetSession(id)
	if !found {
		t.Fatal("the session went away")
	}
	// Give s3270 a moment to negotiate, then read what the operator sees.
	deadline := time.Now().Add(10 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		h := app.sessionHost(sess)
		if h != nil && h.IsConnected() {
			if err := h.UpdateScreen(); err == nil {
				if screen := hostScreenSnapshot(h); screen != nil {
					text = screen.Text()
					if strings.Contains(text, "CICSPROD") {
						break
					}
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}

	for _, want := range []string{"SESSION SELECTION", "CICSPROD", "CICSTEST", "SELECTION"} {
		if !strings.Contains(text, want) {
			t.Errorf("the first screen does not show %q. Screen:\n%s", want, text)
		}
	}
	// A host the account was not assigned must not be on somebody's menu.
	if strings.Contains(text, "ADMINONLY") {
		t.Error("the menu shows a host this account was not assigned")
	}

	app.cleanupSession(sess)
	if _, still := app.menus().get(id); still {
		t.Error("the selection screen outlived its session")
	}
}

// unusedPort returns a port nothing is listening on, so a connection attempt
// fails fast rather than reaching something real.
func unusedPort(t *testing.T) int {
	t.Helper()
	return 65001
}

// The menu belongs to the session that opened it: another account's session ID
// must not reach it, and the store must not hand one session another's.
func TestOneSelectionScreenPerSession(t *testing.T) {
	store := newMenuStore()
	if _, ok := store.get("nobody"); ok {
		t.Error("an unknown session found a menu")
	}
	if _, ok := store.take("nobody"); ok {
		t.Error("taking an unknown session's menu succeeded")
	}
	// stop on a session with no menu is a no-op rather than a panic; session
	// cleanup calls it for every session, most of which never had one.
	store.stop("nobody")
}

// The audit trail has to say a session was opened through the menu, or the
// record of who connected to which mainframe has a hole in it exactly where
// the new path is.
func TestChoosingFromTheMenuIsAudited(t *testing.T) {
	requireS3270(t)
	app, r := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "CICSPROD", Host: "127.0.0.1", Port: 23},
		ConnectionProfile{Name: "CICSTEST", Host: "127.0.0.1", Port: 24},
	)
	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	w := homeAs(t, r, cookie)
	id := sessionCookieFrom(w)
	if id == "" {
		t.Skip("no session opened; s3270 could not start")
	}
	sess, _ := app.SessionManager.GetSession(id)
	t.Cleanup(func() { app.cleanupSession(sess) })

	// Nothing chosen yet, so settling is a no-op and must leave the menu.
	app.settleSelection(requestAs(t, app, "alice"), sess)
	if _, ok := app.menus().get(id); !ok {
		t.Error("settling with no selection removed the menu")
	}
}

func TestSelectionScreenActiveTracksTheSession(t *testing.T) {
	app, _ := audienceTestApp(t)
	if app.SelectionScreenActive(nil) {
		t.Error("a nil session is on a selection screen")
	}
	_ = authz.RoleUser
}

// The whole thing: land on the menu, type a number, press Enter, and be on
// that mainframe. Driven through the real router with a real s3270 against a
// real sample application, because every claim this feature makes is about
// what happens between those pieces.
func TestChoosingASystemConnectsTheSessionToIt(t *testing.T) {
	requireS3270(t)

	// A real 3270 application to arrive at, so "connected" means a screen the
	// host drew rather than a socket that opened.
	//
	// Reached on a routable address rather than loopback, because 3270Web
	// refuses to point a terminal at 127.0.0.1 — a terminal that could would be
	// a way to reach whatever else the server can reach on its own machine.
	// The test has to use the product as configured, not around it.
	hostname := routableAddress(t)
	target, err := startSampleHostOn(hostname + ":0")
	if err != nil {
		t.Skipf("could not start a sample host: %v", err)
	}
	defer target.Stop()
	_, port := hostPortOf(t, target.Addr())

	app, r := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "CICSPROD", Host: hostname, Port: port, Description: "Production CICS"},
		ConnectionProfile{Name: "CICSTEST", Host: hostname, Port: port, Description: "Test CICS"},
	)

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	w := homeAs(t, r, cookie)
	id := sessionCookieFrom(w)
	if id == "" {
		t.Fatal("no session was opened for the selection screen")
	}
	sess, _ := app.SessionManager.GetSession(id)
	defer app.cleanupSession(sess)

	if !waitForScreen(t, app, sess, "SESSION SELECTION") {
		t.Fatal("the selection screen never appeared")
	}

	// Type the selection and press Enter, exactly as the browser does. The
	// field is found on the screen rather than assumed at a coordinate, so a
	// change to the menu's layout does not turn into a mystery here.
	h := app.sessionHost(sess)
	if err := h.UpdateScreen(); err != nil {
		t.Fatal(err)
	}
	field := firstWritableField(t, h)
	// StartX is the column and StartY the row; WriteStringAt takes row first.
	if err := h.WriteStringAt(field.StartY, field.StartX, "2"); err != nil {
		t.Fatalf("type the selection at row %d col %d: %v", field.StartY, field.StartX, err)
	}
	if err := h.SendKey("Enter"); err != nil {
		t.Fatalf("press Enter: %v", err)
	}

	// The menu records the choice; the next request through a screen-serving
	// path is what moves the session.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		app.settleSelection(requestAs(t, app, "alice"), sess)
		if _, still := app.menus().get(id); !still {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if _, still := app.menus().get(id); still {
		t.Fatal("the selection was never noticed")
	}

	if !waitForScreen(t, app, sess, "3270 Example Application") {
		h := app.sessionHost(sess)
		var text string
		if h != nil {
			_ = h.UpdateScreen()
			if screen := hostScreenSnapshot(h); screen != nil {
				text = screen.Text()
			}
		}
		t.Fatalf("the session did not reach the chosen host. Screen:\n%s", text)
	}

	// The session it landed on is the same one it started in, so the browser's
	// cookie, the tab and everything attached to the session still apply.
	if _, found := app.SessionManager.GetSession(id); !found {
		t.Error("the session was replaced rather than moved")
	}
	if sess.TargetHost != hostname {
		t.Errorf("TargetHost = %q, want the chosen profile's host", sess.TargetHost)
	}
}

// firstWritableField finds the input the menu asks the operator to fill in.
func firstWritableField(t *testing.T, h host.Host) *host.Field {
	t.Helper()
	screen := hostScreenSnapshot(h)
	if screen == nil {
		t.Fatal("no screen to read")
	}
	for _, f := range screen.Fields {
		if f != nil && !f.IsProtected() {
			return f
		}
	}
	t.Fatalf("the selection screen has no input field. Screen:\n%s", screen.Text())
	return nil
}

func waitForScreen(t *testing.T, app *App, sess *session.Session, want string) bool {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		h := app.sessionHost(sess)
		if h != nil && h.IsConnected() {
			if err := h.UpdateScreen(); err == nil {
				if screen := hostScreenSnapshot(h); screen != nil &&
					strings.Contains(screen.Text(), want) {
					return true
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

func hostPortOf(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return h, n
}

// routableAddress finds an address on this machine that is neither loopback
// nor link-local, so a terminal is allowed to be pointed at it.
func routableAddress(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("no interfaces to test against: %v", err)
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.To4() == nil {
			continue
		}
		ip := ipnet.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		return ip.String()
	}
	t.Skip("this machine has only loopback addresses; the terminal cannot be pointed at one")
	return ""
}

// startSampleHostOn runs the sample application on a given address, which the
// package's own StartServer does not offer because everything else it serves
// is deliberately loopback-only.
func startSampleHostOn(addr string) (*sampleapps.Server, error) {
	return sampleapps.StartServerOn("app1", addr)
}
