// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

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

	// There is deliberately no exemption for API tokens. A token is issued to
	// an account and carries that account's UserID, so the clause above is the
	// whole of its reach — the same reach the person has in a browser. Where
	// there is a single operator the shared token resolves to that operator,
	// so it matches there too; see authz.Service.
	//
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

// reserveSession admits one more session for ownerID, or refuses.
//
// Each session is an s3270 subprocess, so this is process-exhaustion control
// rather than tidiness. Both caps are checked at the single point every
// creation path goes through — the browser connect form, the tab bar and the
// REST API — because a limit only two of the three consult is not a limit.
//
// It reserves rather than merely checks. Starting a subprocess and waiting for
// its first formatted screen takes long enough for many requests to be inside
// it at once, and a cap read from the session manager cannot see any of them:
// every one of those requests reads the same count, every one passes, and an
// instance capped at four ends up with a dozen. The reservation is taken under
// the same lock that reads the counts and is only converted into a session
// once one exists, so what is compared against the cap is "open plus opening"
// rather than "open".
//
// The returned release must be called on every path out — it is safe to call
// more than once, so `defer release()` alongside an explicit call after the
// session is registered is the intended shape.
//
// An unowned session (ownerID empty) is not counted against a per-user cap:
// there is no user to attribute it to. The instance-wide cap still applies,
// which is what stops that from being a way around the limit.
func (app *App) reserveSession(ownerID string) (release func(), err error) {
	app.admissionMu.Lock()
	defer app.admissionMu.Unlock()

	if total := app.totalSessionCap(); total > 0 &&
		app.SessionManager.Count()+app.admissionsTotal >= total {
		return nil, refuse(errSessionLimit, fmt.Sprintf(
			"This 3270Web instance already has its maximum of %s open. "+
				"Wait for one to close, or ask an administrator to raise the limit.", sessionCount(total)))
	}
	if ownerID != "" {
		if perUser := app.perUserSessionCap(); perUser > 0 &&
			app.SessionManager.CountFor(ownerID)+app.admissions[ownerID] >= perUser {
			return nil, refuse(errSessionLimit, fmt.Sprintf(
				"You already have the maximum of %s open. Close one before opening another.", sessionCount(perUser)))
		}
	}

	app.admissionsTotal++
	if ownerID != "" {
		if app.admissions == nil {
			app.admissions = make(map[string]int)
		}
		app.admissions[ownerID]++
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			app.admissionMu.Lock()
			defer app.admissionMu.Unlock()
			if app.admissionsTotal > 0 {
				app.admissionsTotal--
			}
			if ownerID == "" {
				return
			}
			if app.admissions[ownerID] <= 1 {
				delete(app.admissions, ownerID)
				return
			}
			app.admissions[ownerID]--
		})
	}, nil
}

// sessionCount writes a count the way somebody reads it, so a cap of one does
// not produce "the maximum of 1 sessions".
func sessionCount(n int) string {
	if n == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", n)
}

// errSessionLimit marks a refusal for how many sessions are already open,
// rather than for anything about the host being dialled.
//
// It is a refusal the person can act on — close a tab — so it must reach them
// intact. Reported as a plain error it was indistinguishable from a host that
// did not answer, and the connect page told an operator at their limit to
// check the address and whether the TN3270 service was up.
var errSessionLimit = errors.New("session limit reached")

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
