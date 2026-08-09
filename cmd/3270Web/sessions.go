package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/session"
)

// Concurrent sessions.
//
// A business user routinely keeps three to six host sessions open at once —
// one for the application they are working in, one for a lookup, one for
// TSO. Until now a browser could hold exactly one, because the
// 3270Web_session cookie was both "which session" and "the only session".
//
// The design keeps that cookie meaning "the ACTIVE session", so every
// existing handler — Screen, Submit, Chaos, Copilot, the REST API — resolves
// a session exactly as before and needed no changes. A second cookie carries
// the roster of session IDs this browser owns. Switching tabs is therefore
// just a matter of which ID is in the active cookie.
//
// Possession of a session ID is already the only credential this app has (see
// the audit's note on there being no authentication), so a roster of IDs is
// exactly as strong as the single ID it replaces — no more, no less. What the
// roster does add is a boundary: switching to or closing a session requires
// that ID to be in the caller's own roster, so these endpoints cannot be used
// to enumerate or hop into a session the caller does not already hold.

const (
	sessionRosterCookieName = "3270Web_sessions"
	// maxConcurrentSessions caps how many s3270 subprocesses one user may have
	// running. Six covers the realistic working set; the cap exists so a stuck
	// client cannot fork processes without bound.
	//
	// It is enforced in startHostSessionWithProfile, against the session
	// manager. It used to be enforced here against the length of the roster
	// cookie, which the client sends: clearing the cookie reset the count to
	// zero while the sessions and their subprocesses kept running, and two of
	// the three ways to open a session did not consult it at all.
	maxConcurrentSessions = 6
	// maxTotalSessionsDefault bounds the whole instance. Per-user caps do not
	// bound anything on their own once there can be many users.
	maxTotalSessionsDefault = 64
)

// sessionRoster returns the session IDs this browser owns, dropping any that
// no longer exist. Sessions die independently of the cookie — the idle reaper
// removes them, a host drops — so the roster is always filtered against the
// manager rather than trusted as-is.
func (app *App) sessionRoster(c *gin.Context) []string {
	raw := getCookieValue(c, sessionRosterCookieName)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var live []string
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		// PeekSession, not GetSession: merely listing tabs must not count as
		// activity on every session, or none of them would ever be reaped.
		sess, ok := app.SessionManager.PeekSession(id)
		if !ok {
			continue
		}
		// A cookie is client-supplied, so an ID in it proves nothing about
		// who may use the session. Filtering here keeps a stale or planted
		// entry from showing up as somebody else's tab.
		if !app.mayUseSession(c, sess) {
			continue
		}
		seen[id] = struct{}{}
		live = append(live, id)
	}
	return live
}

func (app *App) setSessionRoster(c *gin.Context, ids []string) {
	setSessionCookie(c, sessionRosterCookieName, strings.Join(ids, ","))
}

// rememberSession adds a session to the roster and makes it active. Returns
// the resulting roster.
func (app *App) rememberSession(c *gin.Context, id string) []string {
	roster := app.sessionRoster(c)
	for _, existing := range roster {
		if existing == id {
			app.setSessionRoster(c, roster)
			return roster
		}
	}
	roster = append(roster, id)
	app.setSessionRoster(c, roster)
	return roster
}

// forgetSession drops a session from the roster. If it was the active one,
// the caller is moved to whichever tab is left (the one before it, matching
// what closing a browser tab does), or to no session at all if it was the
// last.
func (app *App) forgetSession(c *gin.Context, id string) []string {
	roster := app.sessionRoster(c)
	next := make([]string, 0, len(roster))
	removedAt := -1
	for i, existing := range roster {
		if existing == id {
			removedAt = i
			continue
		}
		next = append(next, existing)
	}
	app.setSessionRoster(c, next)

	if getCookieValue(c, sessionCookieName) != id {
		return next
	}
	if len(next) == 0 {
		setSessionCookie(c, sessionCookieName, "")
		return next
	}
	fallback := removedAt - 1
	if fallback < 0 {
		fallback = 0
	}
	if fallback >= len(next) {
		fallback = len(next) - 1
	}
	setSessionCookie(c, sessionCookieName, next[fallback])
	return next
}

// replaceSessionInRoster swaps oldID for newID in place. Reconnecting starts
// a fresh session with a new ID, and without this the reconnected tab would
// silently jump to the end of the bar — the roster filters the dead ID out
// and the new one gets appended.
func (app *App) replaceSessionInRoster(c *gin.Context, oldID, newID string) {
	raw := getCookieValue(c, sessionRosterCookieName)
	var out []string
	replaced := false
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if id == oldID {
			out = append(out, newID)
			replaced = true
			continue
		}
		if id == newID {
			continue
		}
		if _, ok := app.SessionManager.PeekSession(id); !ok {
			continue
		}
		out = append(out, id)
	}
	if !replaced {
		out = append(out, newID)
	}
	app.setSessionRoster(c, out)
}

// selectionScreenTabLabel names a tab that is still on the menu.
const selectionScreenTabLabel = "Session manager"

type sessionTab struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Label  string `json:"label"`
	Active bool   `json:"active"`
	// Menu marks a tab still sitting on the selection screen. The label says
	// so too, but only until two of them collide and are numbered apart, and a
	// client should not have to parse its own labels back.
	Menu bool `json:"menu,omitempty"`
}

// sessionTabs describes the browser's open sessions for the tab bar.
func (app *App) sessionTabs(c *gin.Context) []sessionTab {
	activeID := getCookieValue(c, sessionCookieName)
	roster := app.sessionRoster(c)
	tabs := make([]sessionTab, 0, len(roster))
	for _, id := range roster {
		s, ok := app.SessionManager.PeekSession(id)
		if !ok {
			continue
		}
		target := formatSessionTarget(s)
		label := sessionTabLabel(s, target)
		// A session still sitting on the menu has no host to be named after —
		// its address is a loopback listener this process opened — so it is
		// named for what is on the screen. Once a system is chosen the menu is
		// stopped and the tab takes that host's name.
		_, onMenu := app.menus().get(id)
		if onMenu {
			target = selectionScreenTabLabel
			label = selectionScreenTabLabel
		}
		tabs = append(tabs, sessionTab{
			ID:     id,
			Target: target,
			Label:  label,
			Active: id == activeID,
			Menu:   onMenu,
		})
	}
	disambiguateTabLabels(tabs)
	return tabs
}

// disambiguateTabLabels makes every tab distinguishable at a glance. Two
// sessions to the same host is an ordinary thing to want — two TSO sessions,
// or the same application twice — and a row of tabs all reading "mainframe"
// is a row of tabs you cannot navigate.
//
// Colliding labels fall back to the full target (which carries the port), and
// if that still collides the tabs are numbered in the order they were opened.
func disambiguateTabLabels(tabs []sessionTab) {
	counts := make(map[string]int, len(tabs))
	for _, t := range tabs {
		counts[t.Label]++
	}
	for i := range tabs {
		if counts[tabs[i].Label] < 2 {
			continue
		}
		if tabs[i].Target != "" {
			tabs[i].Label = tabs[i].Target
		}
	}

	// Second pass: same host AND port, so only ordinal numbering separates
	// them.
	final := make(map[string]int, len(tabs))
	for _, t := range tabs {
		final[t.Label]++
	}
	seen := make(map[string]int, len(tabs))
	for i := range tabs {
		if final[tabs[i].Label] < 2 {
			continue
		}
		base := tabs[i].Label
		seen[base]++
		tabs[i].Label = fmt.Sprintf("%s (%d)", base, seen[base])
	}
}

// sessionTabLabel picks what to show on a tab. A sample app gets its friendly
// name; anything else gets the bare hostname, since "mainframe.corp:992" is
// mostly port noise at tab width.
func sessionTabLabel(s *session.Session, target string) string {
	if s == nil {
		return target
	}
	hostPart := target
	if id, _, ok := parseSampleAppHost(hostPart); ok {
		if cfg, ok := sampleAppConfig(id); ok {
			return cfg.Name
		}
	}
	withoutPort := hostPart
	if idx := strings.LastIndex(hostPart, ":"); idx > 0 && !strings.Contains(hostPart[idx:], "]") {
		withoutPort = hostPart[:idx]
	}
	if withoutPort == "" {
		return target
	}
	return withoutPort
}

// SessionsListHandler returns the browser's open sessions.
func (app *App) SessionsListHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"sessions": app.sessionTabs(c),
		"max":      app.perUserSessionCap(),
		// Which chooser this account uses. Where the selection screen is what
		// they meet at sign-in, it is what another tab should be as well; see
		// SessionsNewHandler.
		"sessionManager": app.selectionScreenApplies(c),
	})
}

// SessionsNewHandler opens an additional host session and makes it active.
// The existing sessions keep running — that is the whole point.
func (app *App) SessionsNewHandler(c *gin.Context) {
	// A friendly early answer for the common case. The cap that actually
	// holds is in startHostSessionWithProfile, which every creation path goes
	// through; this one only exists so the UI can say something better than a
	// generic failure.
	if app.SessionManager.CountFor(principalFrom(c).UserID) >= app.perUserSessionCap() {
		c.JSON(http.StatusConflict, gin.H{
			"error": "You already have the maximum number of sessions open. Close one first.",
			"max":   app.perUserSessionCap(),
		})
		return
	}

	// Another tab of the selection screen, for an account that picks its host
	// there. Asking such an operator for a hostname, or offering them a second
	// chooser of our own, would answer a question the instance has already
	// decided how to ask: the menu is the host list they were given, branded
	// and audience-filtered, and it is what "open another session" should mean
	// wherever it is what "sign in" means.
	if strings.TrimSpace(c.PostForm("menu")) != "" {
		profiles, err := app.assignedProfiles(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the host list"})
			return
		}
		if len(profiles) < 2 {
			// Not a state the tab bar asks for — it is told which chooser
			// applies — so this is a stale page rather than a mistake worth a
			// paragraph.
			c.JSON(http.StatusConflict, gin.H{"error": "this account does not use the session manager"})
			return
		}
		sess, ok := app.startSelectionScreen(c, profiles)
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "We couldn't open the session manager. Please try again."})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "id": sess.ID, "sessions": app.sessionTabs(c)})
		return
	}

	// A named profile is preferred over a bare hostname: it carries TLS, LU,
	// model and code page, none of which "host:port" can express.
	var profile *ConnectionProfile
	hostname := strings.TrimSpace(c.PostForm("hostname"))
	if name := strings.TrimSpace(c.PostForm("profile")); name != "" {
		found, ok := app.findVisibleProfile(c, name)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("connection profile %q no longer exists", name)})
			return
		}
		profile = &found
		hostname = found.displayTarget()
	}
	if hostname == "" {
		hostname = strings.TrimSpace(getCookieValue(c, lastTargetCookieName))
	}
	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a host or profile is required to open a session"})
		return
	}

	sess, err := app.startHostSessionWithProfile(c, hostname, profile)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": connectErrorMessage(hostname, err)})
		return
	}
	setSessionCookie(c, sessionCookieName, sess.ID)
	setSessionCookie(c, lastTargetCookieName, hostname)
	app.rememberSession(c, sess.ID)

	c.JSON(http.StatusOK, gin.H{"ok": true, "id": sess.ID, "sessions": app.sessionTabs(c)})
}

// SessionsSwitchHandler makes another of the browser's sessions active.
func (app *App) SessionsSwitchHandler(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session id is required"})
		return
	}
	// Must be in this browser's own roster. Checking only that the session
	// exists would turn this into a way to step into someone else's.
	if !containsString(app.sessionRoster(c), id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	setSessionCookie(c, sessionCookieName, id)
	if s, ok := app.SessionManager.PeekSession(id); ok {
		if target := formatSessionTarget(s); target != "" {
			setSessionCookie(c, lastTargetCookieName, target)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id, "sessions": app.sessionTabs(c)})
}

// SessionsCloseHandler ends one session and leaves the others running.
func (app *App) SessionsCloseHandler(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		id = getCookieValue(c, sessionCookieName)
	}
	if id == "" || !containsString(app.sessionRoster(c), id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if s, ok := app.SessionManager.PeekSession(id); ok {
		app.cleanupSession(s)
	}
	remaining := app.forgetSession(c, id)
	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"remaining": len(remaining),
		"sessions":  app.sessionTabs(c),
	})
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
