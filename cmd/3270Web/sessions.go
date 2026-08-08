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
	// maxConcurrentSessions caps how many s3270 subprocesses one browser can
	// spawn. Six covers the realistic working set; the cap exists so a stuck
	// client cannot fork processes without bound.
	maxConcurrentSessions = 6
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
		if _, ok := app.SessionManager.PeekSession(id); !ok {
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

	if getCookieValue(c, "3270Web_session") != id {
		return next
	}
	if len(next) == 0 {
		setSessionCookie(c, "3270Web_session", "")
		return next
	}
	fallback := removedAt - 1
	if fallback < 0 {
		fallback = 0
	}
	if fallback >= len(next) {
		fallback = len(next) - 1
	}
	setSessionCookie(c, "3270Web_session", next[fallback])
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

type sessionTab struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Label  string `json:"label"`
	Active bool   `json:"active"`
}

// sessionTabs describes the browser's open sessions for the tab bar.
func (app *App) sessionTabs(c *gin.Context) []sessionTab {
	activeID := getCookieValue(c, "3270Web_session")
	roster := app.sessionRoster(c)
	tabs := make([]sessionTab, 0, len(roster))
	for _, id := range roster {
		s, ok := app.SessionManager.PeekSession(id)
		if !ok {
			continue
		}
		target := formatSessionTarget(s)
		tabs = append(tabs, sessionTab{
			ID:     id,
			Target: target,
			Label:  sessionTabLabel(s, target),
			Active: id == activeID,
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
		"max":      maxConcurrentSessions,
	})
}

// SessionsNewHandler opens an additional host session and makes it active.
// The existing sessions keep running — that is the whole point.
func (app *App) SessionsNewHandler(c *gin.Context) {
	hostname := strings.TrimSpace(c.PostForm("hostname"))
	if hostname == "" {
		hostname = strings.TrimSpace(getCookieValue(c, lastTargetCookieName))
	}
	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a hostname is required to open a session"})
		return
	}

	if len(app.sessionRoster(c)) >= maxConcurrentSessions {
		c.JSON(http.StatusConflict, gin.H{
			"error": "You already have the maximum number of sessions open. Close one first.",
			"max":   maxConcurrentSessions,
		})
		return
	}

	sess, err := app.startHostSession(hostname)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": connectErrorMessage(hostname, err)})
		return
	}
	setSessionCookie(c, "3270Web_session", sess.ID)
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
	setSessionCookie(c, "3270Web_session", id)
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
		id = getCookieValue(c, "3270Web_session")
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
