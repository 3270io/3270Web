// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// What an account is given and what an account has saved are two lists.
//
// An administrator assigns hosts; the first screen is decided by how many an
// account was given — none is the connect form, one is a connection made for
// them, several is the session manager. A connection somebody saved for
// themselves is theirs to open from "Open a connection", and is never a reason
// for the instance to dial something at sign-in or to put a menu where the
// connect form was.
//
// The two lists used to be one. Every route that decided the first screen read
// the list of everything an account may connect to by name, which includes its
// own saved connections, so saving two of them was enough to have the instance
// stop offering a connect form, and saving one was enough to have it dial that
// host the moment the account signed in.

// saveOwnConnection writes a connection into username's own store, as the
// "Manage connections" dialog's save does.
func saveOwnConnection(t *testing.T, app *App, username string, p ConnectionProfile) {
	t.Helper()
	if _, err := app.ownProfiles(requestAs(t, app, username)).upsert(p); err != nil {
		t.Fatal(err)
	}
}

func TestSavedConnectionsAreNotAssignedHosts(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	alice := func() *gin.Context { return requestAs(t, app, "alice") }

	saveOwnConnection(t, app, "alice", ConnectionProfile{Name: "MINE-ONE", Host: "one.example", Port: 23})
	saveOwnConnection(t, app, "alice", ConnectionProfile{Name: "MINE-TWO", Host: "two.example", Port: 23})
	if app.selectionScreenApplies(alice()) {
		t.Error("an account that has saved two connections and been given none uses the selection screen")
	}

	publishProfiles(t, app, admin, ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23})
	if app.selectionScreenApplies(alice()) {
		t.Error("an account given one host and holding two of its own uses the selection screen; " +
			"it is connected to the one it was given")
	}

	publishProfiles(t, app, admin, ConnectionProfile{Name: "TSO", Host: "tso.example", Port: 23})
	if !app.selectionScreenApplies(alice()) {
		t.Error("an account given two hosts does not use the selection screen")
	}
}

// A saved connection that shares a name with an assigned host replaces it in
// the lists an account picks from — the account's own settings win — but it is
// still the host the account was given. Counting by who wrote the entry rather
// than by whether the name was assigned would lose one here.
func TestASavedConnectionThatOverridesAnAssignedHostStillCountsOnce(t *testing.T) {
	app, _ := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "PAYMENTS", Host: "pay.example", Port: 23},
		ConnectionProfile{Name: "TSO", Host: "tso.example", Port: 23})
	saveOwnConnection(t, app, "alice", ConnectionProfile{Name: "PAYMENTS", Host: "pay-mine.example", Port: 992})

	if !app.selectionScreenApplies(requestAs(t, app, "alice")) {
		t.Error("overriding one of two assigned hosts took the account off the selection screen")
	}
}

// Asked for by the tab bar's "+ New session" where it believes the menu
// applies. With nothing assigned it does not, whatever has been saved.
func TestANewMenuTabIsRefusedWhenOnlySavedConnectionsExist(t *testing.T) {
	app, r := audienceTestApp(t)
	saveOwnConnection(t, app, "alice", ConnectionProfile{Name: "MINE-ONE", Host: "one.example", Port: 23})
	saveOwnConnection(t, app, "alice", ConnectionProfile{Name: "MINE-TWO", Host: "two.example", Port: 23})

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
		t.Errorf("asking for a menu tab with only saved connections returned %d, want 409: %s", w.Code, w.Body.String())
	}
	if app.SessionManager.Count() != 0 {
		t.Errorf("the refused request left %d session(s) open", app.SessionManager.Count())
	}
}

// The case that dials a host on its own. Pointed at a real sample application
// for the reason TestASingleAssignedHostConnectsWithoutAsking is: a host
// nothing listens on would fail, fall back to the connect form, and pass this
// test without it ever taking the path it is about.
func TestASavedConnectionDoesNotTakeTheLandingPageAway(t *testing.T) {
	requireS3270(t)

	hostname := routableAddress(t)
	target, err := startSampleHostOn(hostname + ":0")
	if err != nil {
		t.Skipf("could not start a sample host: %v", err)
	}
	defer target.Stop()
	_, port := hostPortOf(t, target.Addr())

	app, r := audienceTestApp(t)
	saveOwnConnection(t, app, "bob", ConnectionProfile{Name: "MINE", Host: hostname, Port: port})

	cookie := authCookieFrom(doLogin(t, r, "bob", loginTestPassword, "10.0.0.1"))
	w := homeAs(t, r, cookie)

	if w.Code == http.StatusFound {
		t.Fatalf("redirected to %q: a connection the account saved for itself was dialled at sign-in",
			w.Header().Get("Location"))
	}
	if id := sessionCookieFrom(w); id != "" {
		if sess, found := app.SessionManager.GetSession(id); found {
			defer app.cleanupSession(sess)
		}
		t.Errorf("a terminal session was opened anyway: %s", id)
	}
}

// One assigned host and a saved one: the account is on the assigned host, and
// there is no menu to choose from because there was nothing to choose.
func TestOneAssignedHostAndASavedOneStillConnectsWithoutAsking(t *testing.T) {
	requireS3270(t)

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
		ConnectionProfile{Name: "ONLYONE", Host: hostname, Port: port, Groups: []string{"payments"}})
	saveOwnConnection(t, app, "alice", ConnectionProfile{Name: "MINE", Host: "mine.example", Port: 23})

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	w := homeAs(t, r, cookie)

	if w.Code != http.StatusFound || w.Header().Get("Location") != "/screen" {
		t.Fatalf("landing page returned %d -> %q, want a redirect straight to the terminal",
			w.Code, w.Header().Get("Location"))
	}
	id := sessionCookieFrom(w)
	if id == "" {
		t.Fatal("no terminal session was opened")
	}
	sess, found := app.SessionManager.GetSession(id)
	if !found {
		t.Fatal("the session went away")
	}
	defer app.cleanupSession(sess)

	if _, hasMenu := app.menus().get(id); hasMenu {
		t.Error("a selection screen was drawn for an account given a single host")
	}
	if sess.TargetHost != hostname {
		t.Errorf("TargetHost = %q, want the assigned host %q", sess.TargetHost, hostname)
	}
}

// What the menu lists is not what decides whether there is one. An account
// given several hosts still sees the connections it saved on that menu, as it
// always has; only the decision stopped counting them.
func TestTheMenuStillListsSavedConnectionsWhereItApplies(t *testing.T) {
	requireS3270(t)
	app, r := audienceTestApp(t)
	admin := requestAs(t, app, "root")
	publishProfiles(t, app, admin,
		ConnectionProfile{Name: "CICSPROD", Host: "127.0.0.1", Port: 23},
		ConnectionProfile{Name: "CICSTEST", Host: "127.0.0.1", Port: 24})
	saveOwnConnection(t, app, "alice", ConnectionProfile{Name: "MYSANDBOX", Host: "127.0.0.1", Port: 25})

	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	w := homeAs(t, r, cookie)
	if w.Code != http.StatusFound {
		t.Fatalf("landing page returned %d, want a redirect to the selection screen", w.Code)
	}
	id := sessionCookieFrom(w)
	sess, found := app.SessionManager.GetSession(id)
	if !found {
		t.Fatal("no terminal session was opened for the selection screen")
	}
	defer app.cleanupSession(sess)

	if !waitForScreen(t, app, sess, "MYSANDBOX") {
		t.Error("the menu no longer lists a connection the account saved")
	}
}
