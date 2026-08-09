package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/sessionmenu"
)

// Another tab, for an account that picks its host from the selection screen.
//
// Where the menu is what somebody meets at sign-in, it is the host list they
// have been given — branded, and filtered to what they may reach. Offering
// them a second chooser of our own when they open another session would be a
// competing list beside the one the instance built.

func TestSelectionScreenAppliesFollowsTheAssignedHostCount(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	alice := func() *gin.Context { return requestAs(t, app, "alice") }

	if app.selectionScreenApplies(alice()) {
		t.Error("an account with no assigned host uses the selection screen")
	}

	publishProfiles(t, app, admin, ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23})
	if app.selectionScreenApplies(alice()) {
		t.Error("an account assigned exactly one host uses the selection screen; it is connected to that host")
	}

	publishProfiles(t, app, admin, ConnectionProfile{Name: "TSO", Host: "tso.example", Port: 23})
	if !app.selectionScreenApplies(alice()) {
		t.Error("an account assigned two hosts does not use the selection screen")
	}
}

// The tab bar is told which chooser applies, so it can open the right one
// without asking twice.
func TestTheTabBarIsToldWhichChooserApplies(t *testing.T) {
	app, r := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23},
		ConnectionProfile{Name: "TSO", Host: "tso.example", Port: 23})

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	w := getWithAuth(r, "/sessions", cookie, "10.0.0.1")
	if w.Code != http.StatusOK {
		t.Fatalf("/sessions returned %d", w.Code)
	}
	var body struct {
		SessionManager bool `json:"sessionManager"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.SessionManager {
		t.Error("/sessions does not report that this account uses the session manager")
	}
}

// Asked for by a page that has been open since before the host list changed.
// Refused rather than quietly falling back to a hostname prompt.
func TestANewMenuTabIsRefusedWhereTheMenuDoesNotApply(t *testing.T) {
	app, r := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin, ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23})

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	form := url.Values{"menu": {"1"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/sessions/new", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Referer", "http://example.test/screen")
	req.Host = "example.test"
	req.RemoteAddr = "10.0.0.1:40000"
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("asking for a menu tab where the menu does not apply returned %d, want 409: %s", w.Code, w.Body.String())
	}
	if app.SessionManager.Count() != 0 {
		t.Errorf("the refused request left %d session(s) open", app.SessionManager.Count())
	}
}

// A session sitting on the menu is addressed by a loopback listener this
// process opened, which is no name for a tab.
func TestATabStillOnTheMenuIsNamedForIt(t *testing.T) {
	app := newSessionsApp(t)
	s := newMockSession(t, app, "mainframe.example:23")

	menu, err := sessionmenu.Start(sessionmenu.Branding{}, []sessionmenu.Entry{{Name: "PAYMENTS"}})
	if err != nil {
		t.Fatal(err)
	}
	defer menu.Stop()
	app.menus().put(s.ID, menu)

	_, _ = withCookies(app, map[string]string{sessionRosterCookieName: s.ID, sessionCookieName: s.ID}, func(c *gin.Context) {
		tabs := app.sessionTabs(c)
		if len(tabs) != 1 {
			t.Fatalf("got %d tabs, want 1", len(tabs))
		}
		if tabs[0].Label != selectionScreenTabLabel {
			t.Errorf("a tab on the menu is labelled %q, want %q", tabs[0].Label, selectionScreenTabLabel)
		}
	})

	// Once a system is chosen the menu is stopped, and the tab takes that
	// host's name.
	app.menus().stop(s.ID)
	_, _ = withCookies(app, map[string]string{sessionRosterCookieName: s.ID, sessionCookieName: s.ID}, func(c *gin.Context) {
		tabs := app.sessionTabs(c)
		if len(tabs) != 1 || tabs[0].Label == selectionScreenTabLabel {
			t.Errorf("a tab that has left the menu is still labelled %q", selectionScreenTabLabel)
		}
	})
}

// The client half of the same rule.
func TestTheNewSessionButtonOpensTheMenuWhereItApplies(t *testing.T) {
	src := staticSource(t, "session-tabs.js")

	start := strings.Index(src, "function openNew(")
	if start < 0 {
		t.Fatal("session-tabs.js: openNew is gone")
	}
	end := strings.Index(src[start:], "\n  function promptForHost(")
	if end < 0 {
		t.Fatal("session-tabs.js: could not find the end of openNew")
	}
	body := src[start : start+end]

	if !strings.Contains(body, "sessionManager") || !strings.Contains(body, `menu: "1"`) {
		t.Error("session-tabs.js: the new-session control no longer opens another selection screen " +
			"for an account that picks its host there")
	}
	// And the branch has to come before the picker, or the picker wins.
	if strings.Index(body, "sessionManager") > strings.Index(body, "picker.pick(") {
		t.Error("session-tabs.js: the picker is offered ahead of the selection screen")
	}
	if !strings.Contains(src, "payload.sessionManager") {
		t.Error("session-tabs.js: the roster no longer carries which chooser applies")
	}
}
