package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/session"
)

// sessionIDContextKey lets a handler group name the session a request acts on
// without going through the cookie.
//
// The REST API used to do this by appending a cookie to the inbound request.
// http.Request.AddCookie appends to the Cookie header while Request.Cookie
// returns the first match, so a client that sent both a cookie and a path
// parameter had one validated and the other used. A context value has no such
// ambiguity: there is one place to write it and one to read it.
const sessionIDContextKey = "3270web.sessionID"

// scopeToSession records which terminal session this request acts on.
func scopeToSession(c *gin.Context, id string) {
	c.Set(sessionIDContextKey, id)
}

// sessionIDFor returns the terminal session this request addresses: the value
// a handler group scoped it to, otherwise the browser's cookie.
func sessionIDFor(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(sessionIDContextKey); ok {
		if id, ok := value.(string); ok && id != "" {
			return id
		}
	}
	return getCookieValue(c, sessionCookieName)
}

// mayUseSession reports whether the caller is allowed to act on s.
//
// This is the whole of session isolation. Every browser handler reaches its
// session through getSession, so this predicate is the one place that decides
// whether holding a session ID is enough — which, before ownership existed,
// it was.
func (app *App) mayUseSession(c *gin.Context, s *session.Session) bool {
	if s == nil {
		return false
	}
	principal := principalFrom(c)

	if principal.Owns(s.OwnerID) {
		return true
	}

	// The instance-wide API token is instance-wide by definition: it is one
	// shared credential, so there is no "its own" session to confine it to.
	// Narrowing this needs per-user tokens, which is a separate change; until
	// then, treating it as unconfined is at least honest about what the
	// credential is. It is still a credential — an anonymous caller never
	// reaches here.
	if principal.Kind == authz.KindAPIToken && !principal.IsAnonymous() {
		return true
	}

	// A session with no owner predates ownership. Where there is a single
	// operator there is nobody to separate them from, so an unowned session is
	// theirs. Where users are separated this is not reachable — every creation
	// path labels what it creates — and allowing it would be the fail-open
	// reading of "owner unknown".
	if s.OwnerID == "" && !app.separatesUsers() {
		return true
	}

	return false
}

// separatesUsers reports whether this deployment can have more than one user,
// and therefore something to keep apart.
//
// Written as "which modes do NOT separate" rather than "which do" so that a
// mode added later separates by default. The other direction would mean a new
// authenticated mode silently ran without isolation until somebody remembered
// to add it to a list, and that failure is invisible: everything works, and
// everyone can see everyone else's sessions.
//
// The empty mode is an App that never went through buildRouter, which in
// practice means a test constructing one directly — production always has a
// validated mode. It counts as single-operator because that is what it is.
func (app *App) separatesUsers() bool {
	switch app.authMode {
	case "", authz.ModeNone:
		return false
	default:
		return true
	}
}

// perUserSessionCap is how many sessions one user may hold at once.
func (app *App) perUserSessionCap() int {
	return envInt("MAX_SESSIONS_PER_USER", maxConcurrentSessions)
}

// totalSessionCap bounds the instance.
func (app *App) totalSessionCap() int {
	return envInt("MAX_TOTAL_SESSIONS", maxTotalSessionsDefault)
}

// checkSessionCaps reports whether another session may be opened for ownerID.
//
// Each session is an s3270 subprocess, so this is process-exhaustion control
// rather than tidiness. Both caps are checked at the single point every
// creation path goes through — the browser connect form, the tab bar and the
// REST API — because a limit only two of the three consult is not a limit.
//
// An unowned session (ownerID empty) is not counted against a per-user cap:
// there is no user to attribute it to. The instance-wide cap still applies,
// which is what stops that from being a way around the limit.
func (app *App) checkSessionCaps(ownerID string) error {
	if total := app.totalSessionCap(); total > 0 && app.SessionManager.Count() >= total {
		return fmt.Errorf("this 3270Web instance already has its maximum of %d sessions open", total)
	}
	if ownerID == "" {
		return nil
	}
	if perUser := app.perUserSessionCap(); perUser > 0 &&
		app.SessionManager.CountFor(ownerID) >= perUser {
		return fmt.Errorf("you already have the maximum of %d sessions open; close one first", perUser)
	}
	return nil
}

// envInt reads a positive integer setting, falling back when unset or
// unparseable.
func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		log.Printf("Warning: ignoring %s=%q: want a non-negative integer", name, raw)
		return fallback
	}
	return n
}
