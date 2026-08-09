package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/render"
	"github.com/jnnngs/3270Web/internal/session"
)

func newSessionsApp(t *testing.T) *App {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return &App{
		SessionManager: session.NewManager(),
		Renderer:       render.NewHtmlRenderer(),
	}
}

// newOwnedMockSession is newMockSession with an owner, for the tests that
// exercise ownership or the per-user cap.
func newOwnedMockSession(t *testing.T, app *App, ownerID, target string) *session.Session {
	t.Helper()
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.Screen = buildSampleApp1Screen()
	s := app.SessionManager.CreateSessionFor(ownerID, mh)
	s.TargetHost, s.TargetPort = parseHostPort(target)
	return s
}

func newMockSession(t *testing.T, app *App, target string) *session.Session {
	t.Helper()
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	mh.Screen = buildSampleApp1Screen()
	s := app.SessionManager.CreateSession(mh)
	s.TargetHost, s.TargetPort = parseHostPort(target)
	return s
}

// withCookies runs fn against a throwaway request/response carrying the given
// cookies, and returns the cookies the handler set.
func withCookies(app *App, cookies map[string]string, fn func(c *gin.Context)) (*httptest.ResponseRecorder, map[string]string) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	for name, value := range cookies {
		c.Request.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	fn(c)

	out := make(map[string]string)
	for _, sc := range w.Result().Cookies() {
		out[sc.Name] = sc.Value
	}
	return w, out
}

// rosterAfter feeds a Set-Cookie value back through a request the way a
// browser would and returns the roster the server reads from it. gin escapes
// cookie values on write and unescapes them on read, so asserting the raw
// header would be testing the wire format rather than the round trip.
func rosterAfter(app *App, setCookies map[string]string) []string {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	for name, value := range setCookies {
		c.Request.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	return app.sessionRoster(c)
}

// A session that has died — reaped for idleness, or closed in another tab —
// must not linger on the tab bar as a control that goes nowhere.
func TestSessionRosterDropsDeadSessions(t *testing.T) {
	app := newSessionsApp(t)
	alive := newMockSession(t, app, "host-a:3270")
	dead := newMockSession(t, app, "host-b:3270")
	app.SessionManager.RemoveSession(dead.ID)

	_, _ = withCookies(app, map[string]string{
		sessionRosterCookieName: alive.ID + "," + dead.ID + ",   ,",
	}, func(c *gin.Context) {
		got := app.sessionRoster(c)
		if len(got) != 1 || got[0] != alive.ID {
			t.Errorf("roster = %v, want just the live session %s", got, alive.ID)
		}
	})
}

func TestSessionRosterDeduplicates(t *testing.T) {
	app := newSessionsApp(t)
	s := newMockSession(t, app, "host:3270")

	_, _ = withCookies(app, map[string]string{
		sessionRosterCookieName: s.ID + "," + s.ID,
	}, func(c *gin.Context) {
		if got := app.sessionRoster(c); len(got) != 1 {
			t.Errorf("roster = %v, want one entry", got)
		}
	})
}

// Closing the active tab should land on a neighbour, the way closing a
// browser tab does — not dump the operator back at the connect page while
// other sessions are still running.
func TestForgetSessionActivatesNeighbour(t *testing.T) {
	app := newSessionsApp(t)
	a := newMockSession(t, app, "host-a:3270")
	b := newMockSession(t, app, "host-b:3270")
	cSess := newMockSession(t, app, "host-c:3270")

	_, set := withCookies(app, map[string]string{
		sessionRosterCookieName: strings.Join([]string{a.ID, b.ID, cSess.ID}, ","),
		"3270Web_session":       b.ID,
	}, func(ctx *gin.Context) {
		app.forgetSession(ctx, b.ID)
	})

	if set["3270Web_session"] != a.ID {
		t.Errorf("active session = %q, want the preceding tab %q", set["3270Web_session"], a.ID)
	}
	got := rosterAfter(app, set)
	want := []string{a.ID, cSess.ID}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("roster = %v, want %v", got, want)
	}
}

func TestForgetLastSessionClearsActive(t *testing.T) {
	app := newSessionsApp(t)
	only := newMockSession(t, app, "host:3270")

	_, set := withCookies(app, map[string]string{
		sessionRosterCookieName: only.ID,
		"3270Web_session":       only.ID,
	}, func(ctx *gin.Context) {
		app.forgetSession(ctx, only.ID)
	})

	if set["3270Web_session"] != "" {
		t.Errorf("active session = %q, want empty", set["3270Web_session"])
	}
	if set[sessionRosterCookieName] != "" {
		t.Errorf("roster = %q, want empty", set[sessionRosterCookieName])
	}
}

// Closing a background tab must leave the active one alone.
func TestForgetInactiveSessionKeepsActive(t *testing.T) {
	app := newSessionsApp(t)
	a := newMockSession(t, app, "host-a:3270")
	b := newMockSession(t, app, "host-b:3270")

	_, set := withCookies(app, map[string]string{
		sessionRosterCookieName: a.ID + "," + b.ID,
		"3270Web_session":       a.ID,
	}, func(ctx *gin.Context) {
		app.forgetSession(ctx, b.ID)
	})

	if _, changed := set["3270Web_session"]; changed {
		t.Error("closing a background tab rewrote the active session cookie")
	}
	if got := rosterAfter(app, set); len(got) != 1 || got[0] != a.ID {
		t.Errorf("roster = %v, want just %s", got, a.ID)
	}
}

// Reconnecting mints a new session ID; the tab must stay where it was rather
// than jumping to the end of the bar.
func TestReplaceSessionInRosterKeepsPosition(t *testing.T) {
	app := newSessionsApp(t)
	a := newMockSession(t, app, "host-a:3270")
	b := newMockSession(t, app, "host-b:3270")
	cSess := newMockSession(t, app, "host-c:3270")
	replacement := newMockSession(t, app, "host-b:3270")
	app.SessionManager.RemoveSession(b.ID)

	_, set := withCookies(app, map[string]string{
		sessionRosterCookieName: strings.Join([]string{a.ID, b.ID, cSess.ID}, ","),
	}, func(ctx *gin.Context) {
		app.replaceSessionInRoster(ctx, b.ID, replacement.ID)
	})

	got := rosterAfter(app, set)
	want := []string{a.ID, replacement.ID, cSess.ID}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("roster = %v, want %v", got, want)
	}
}

// Possession of a session ID is this app's only credential, so these
// endpoints must not become a way to step into a session the caller does not
// already hold in their own roster.
func TestSwitchRejectsSessionOutsideRoster(t *testing.T) {
	app := newSessionsApp(t)
	mine := newMockSession(t, app, "host-a:3270")
	theirs := newMockSession(t, app, "host-b:3270")

	r := gin.New()
	r.POST("/sessions/switch", app.SessionsSwitchHandler)

	body := url.Values{"id": {theirs.ID}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/sessions/switch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionRosterCookieName, Value: mine.ID})
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: mine.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a session outside the caller's roster", w.Code)
	}
	for _, sc := range w.Result().Cookies() {
		if sc.Name == "3270Web_session" && sc.Value == theirs.ID {
			t.Fatal("handler switched the active session to one outside the roster")
		}
	}
}

func TestCloseRejectsSessionOutsideRoster(t *testing.T) {
	app := newSessionsApp(t)
	mine := newMockSession(t, app, "host-a:3270")
	theirs := newMockSession(t, app, "host-b:3270")

	r := gin.New()
	r.POST("/sessions/close", app.SessionsCloseHandler)

	body := url.Values{"id": {theirs.ID}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/sessions/close", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionRosterCookieName, Value: mine.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if _, alive := app.SessionManager.PeekSession(theirs.ID); !alive {
		t.Fatal("handler killed a session outside the caller's roster")
	}
}

// An unbounded roster means unbounded s3270 subprocesses.
func TestNewSessionRefusedPastTheCap(t *testing.T) {
	app := newSessionsApp(t)
	for i := 0; i < maxConcurrentSessions; i++ {
		newOwnedMockSession(t, app, authz.LocalUserID, "host:3270")
	}

	r := gin.New()
	r.Use(app.Authenticate())
	r.POST("/sessions/new", app.SessionsNewHandler)

	body := url.Values{"hostname": {"sampleapp:app1"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/sessions/new", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 once the cap is reached", w.Code)
	}
}

// The cap used to be the length of a cookie the client sends, so clearing it
// reset the count to zero while the sessions — and their s3270 subprocesses —
// kept running. It is now counted against the session manager, which the
// client cannot edit.
func TestSessionCapSurvivesClearingTheRosterCookie(t *testing.T) {
	app := newSessionsApp(t)
	for i := 0; i < maxConcurrentSessions; i++ {
		newOwnedMockSession(t, app, authz.LocalUserID, "host:3270")
	}

	r := gin.New()
	r.Use(app.Authenticate())
	r.POST("/sessions/new", app.SessionsNewHandler)

	// No roster cookie at all: the old check would have counted zero.
	body := url.Values{"hostname": {"sampleapp:app1"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/sessions/new", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 — the cap must not depend on a client cookie", w.Code)
	}
}

// The two creation paths that never consulted the roster at all must be
// capped too, or the limit is advisory.
func TestSessionCapAppliesToEveryCreationPath(t *testing.T) {
	app := newSessionsApp(t)
	for i := 0; i < maxConcurrentSessions; i++ {
		newOwnedMockSession(t, app, authz.LocalUserID, "host:3270")
	}

	if _, err := app.reserveSession(authz.LocalUserID); err == nil {
		t.Fatal("reserveSession allowed a session past the per-user cap")
	}
	// startHostSession is the single point /connect, the tab bar and the REST
	// API all funnel through.
	if _, err := app.startHostSession(asPrincipal(authz.Local()), "127.0.0.1:3270"); err == nil {
		t.Error("startHostSession ignored the per-user cap")
	}
}

func TestTotalSessionCap(t *testing.T) {
	t.Setenv("MAX_TOTAL_SESSIONS", "3")
	app := newSessionsApp(t)

	// Unowned sessions dodge the per-user cap by design — there is no user to
	// attribute them to — so the instance-wide cap is what bounds them.
	for i := 0; i < 3; i++ {
		newMockSession(t, app, "host:3270")
	}
	if _, err := app.reserveSession(""); err == nil {
		t.Error("the instance-wide cap did not apply to unowned sessions")
	}
	if _, err := app.reserveSession("someone"); err == nil {
		t.Error("the instance-wide cap did not apply to an owned session")
	}
}

func TestSessionCapsAreConfigurable(t *testing.T) {
	app := newSessionsApp(t)
	if got := app.perUserSessionCap(); got != maxConcurrentSessions {
		t.Errorf("default per-user cap = %d, want %d", got, maxConcurrentSessions)
	}

	t.Setenv("MAX_SESSIONS_PER_USER", "2")
	if got := app.perUserSessionCap(); got != 2 {
		t.Errorf("configured per-user cap = %d, want 2", got)
	}
	// A nonsense value must not silently become zero, which would read as
	// "unlimited" and disable the cap.
	t.Setenv("MAX_SESSIONS_PER_USER", "not-a-number")
	if got := app.perUserSessionCap(); got != maxConcurrentSessions {
		t.Errorf("unparseable cap = %d, want the default %d", got, maxConcurrentSessions)
	}
}

func TestSessionsListReportsActiveTab(t *testing.T) {
	app := newSessionsApp(t)
	a := newMockSession(t, app, "host-a:3270")
	b := newMockSession(t, app, "host-b:3270")

	r := gin.New()
	r.GET("/sessions", app.SessionsListHandler)

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.AddCookie(&http.Cookie{Name: sessionRosterCookieName, Value: a.ID + "," + b.ID})
	req.AddCookie(&http.Cookie{Name: "3270Web_session", Value: b.ID})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var payload struct {
		Sessions []sessionTab `json:"sessions"`
		Max      int          `json:"max"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(payload.Sessions))
	}
	if payload.Sessions[0].Active || !payload.Sessions[1].Active {
		t.Errorf("wrong tab marked active: %+v", payload.Sessions)
	}
	if payload.Sessions[0].Label != "host-a" {
		t.Errorf("label = %q, want the hostname without its port", payload.Sessions[0].Label)
	}
	if payload.Max != maxConcurrentSessions {
		t.Errorf("max = %d, want %d", payload.Max, maxConcurrentSessions)
	}
}

// A row of tabs all reading "mainframe" is a row of tabs you cannot navigate,
// and two sessions to the same host is an ordinary thing to want.
func TestDisambiguateTabLabels(t *testing.T) {
	tests := []struct {
		name string
		in   []sessionTab
		want []string
	}{
		{
			name: "distinct hosts keep their short labels",
			in: []sessionTab{
				{Label: "host-a", Target: "host-a:3270"},
				{Label: "host-b", Target: "host-b:3270"},
			},
			want: []string{"host-a", "host-b"},
		},
		{
			name: "same label different port falls back to the full target",
			in: []sessionTab{
				{Label: "Sample App 1", Target: "sampleapp:3270"},
				{Label: "Sample App 1", Target: "sampleapp:3271"},
			},
			want: []string{"sampleapp:3270", "sampleapp:3271"},
		},
		{
			name: "identical host and port are numbered in open order",
			in: []sessionTab{
				{Label: "mainframe", Target: "mainframe:992"},
				{Label: "mainframe", Target: "mainframe:992"},
				{Label: "mainframe", Target: "mainframe:992"},
			},
			want: []string{"mainframe:992 (1)", "mainframe:992 (2)", "mainframe:992 (3)"},
		},
		{
			name: "a collision does not rename the sessions it does not involve",
			in: []sessionTab{
				{Label: "tso", Target: "tso:992"},
				{Label: "tso", Target: "tso:992"},
				{Label: "cics", Target: "cics:992"},
			},
			want: []string{"tso:992 (1)", "tso:992 (2)", "cics"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disambiguateTabLabels(tt.in)
			for i := range tt.in {
				if tt.in[i].Label != tt.want[i] {
					t.Errorf("tab %d label = %q, want %q", i, tt.in[i].Label, tt.want[i])
				}
			}
			seen := make(map[string]bool, len(tt.in))
			for _, tab := range tt.in {
				if seen[tab.Label] {
					t.Errorf("duplicate label %q survived disambiguation", tab.Label)
				}
				seen[tab.Label] = true
			}
		})
	}
}

// The feature was fully built and nobody could find it: the way in was an
// unlabelled + among nine other unlabelled icons, and the tab bar that would
// have explained it stayed hidden until you already had two sessions. These
// assert the properties that fixed it, because each one is a single attribute
// away from regressing back into invisibility.

func TestSessionTabBarIsNotHiddenBehindASecondSession(t *testing.T) {
	src := templateSource(t, "screen.html")

	bar := strings.Index(src, "data-session-tabs")
	if bar < 0 {
		t.Fatal("screen.html: the session tab bar is gone")
	}
	// The opening tag only. A `hidden` anywhere inside the bar's children is
	// somebody else's business.
	end := strings.Index(src[bar:], ">")
	if end < 0 {
		t.Fatal("screen.html: unterminated session tab bar tag")
	}
	if strings.Contains(src[bar:bar+end], "hidden") {
		t.Error("screen.html: the tab bar is conditionally hidden again — " +
			"it is where somebody learns a second session is possible, " +
			"and it cannot teach that from behind a hidden attribute")
	}
}

func TestNewSessionControlCarriesVisibleWords(t *testing.T) {
	src := templateSource(t, "screen.html")

	if strings.Count(src, "data-session-new") != 1 {
		t.Fatal("screen.html: expected exactly one new-session control")
	}
	if !strings.Contains(src, "session-tab-new-label") {
		t.Error("screen.html: the new-session control lost its visible label — " +
			"a bare icon is how this feature went unnoticed the first time")
	}
	if !strings.Contains(src, ">New session<") {
		t.Error(`screen.html: the new-session control no longer reads "New session"`)
	}

	// An aria-label here would override the visible words and give a speech
	// user a different name from the one on screen (WCAG 2.5.3).
	button := strings.Index(src, "data-session-new")
	end := strings.Index(src[button:], ">")
	if end > 0 && strings.Contains(src[button:button+end], "aria-label") {
		t.Error("screen.html: aria-label on the new-session button overrides its " +
			"visible name; the words on the button are the accessible name")
	}
}

func TestNewSessionControlLivesInTheTabBar(t *testing.T) {
	src := templateSource(t, "screen.html")

	bar := strings.Index(src, `class="session-tab-bar"`)
	toolbar := strings.Index(src, "data-main-toolbar")
	trigger := strings.Index(src, "data-session-new")
	if bar < 0 || toolbar < 0 || trigger < 0 {
		t.Fatal("screen.html: tab bar, toolbar or new-session control is gone")
	}
	// Between the bar's opening tag and the toolbar that follows it — which
	// is the tab bar's own markup, without depending on its exact shape.
	if trigger < bar || trigger > toolbar {
		t.Error("screen.html: the new-session control left the tab bar — " +
			"beside the tabs it reads as 'add one of these'; back in the icon row it reads as nothing")
	}
}
