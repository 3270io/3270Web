package main

import (
	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/reqsec"
)

// principalContextKey names the resolved principal on the Gin context.
// Unexported and typed as a plain string because Gin's Keys map is
// string-keyed; the value is only ever read through principalFrom.
const principalContextKey = "3270web.principal"

// mustChangePasswordContextKey and usernameContextKey carry login details that
// accompany the principal but are not part of its identity.
const (
	mustChangePasswordContextKey = "3270web.mustChangePassword"
	usernameContextKey           = "3270web.username"
)

// Authenticate resolves who is making the request and records it on the
// context. It enforces nothing — that is the job of the handlers and of the
// role middleware that will sit alongside them.
//
// Separating resolution from enforcement keeps one answer to "who is this"
// for the whole request, so a handler and an audit line cannot disagree about
// the caller, and so that turning authentication on later changes how the
// principal is derived without touching any decision made from it.
func (app *App) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(principalContextKey, app.resolvePrincipal(c))
		c.Next()
	}
}

// resolvePrincipal derives the caller's identity for the configured mode.
//
// With AUTH_MODE=none there is no identity to establish: the deployment has a
// single operator with full rights, expressed as authz.Local() so that every
// downstream check runs the same code it will run once modes that do
// authenticate exist.
func (app *App) resolvePrincipal(c *gin.Context) authz.Principal {
	switch app.authMode {
	case authz.ModeNone:
		return authz.Local()

	case authz.ModeLocal:
		id := getCookieValue(c, authCookieName)
		if id == "" {
			return authz.Anonymous()
		}
		sess, ok := app.authSessions.Get(id, reqsec.ClientIP(c.Request), app.bindSessionIP(c))
		if !ok {
			return authz.Anonymous()
		}
		// Carried alongside the principal because the forced-change gate is a
		// property of the login, not of the identity, and re-reading the
		// account store on every request to learn it would be wasteful.
		c.Set(mustChangePasswordContextKey, sess.MustChangePassword)
		c.Set(usernameContextKey, sess.Username)
		return sess.Principal()

	default:
		// Unreachable: the mode is validated at startup and startup fails on
		// anything unsupported. Denying is the right answer to a mode this
		// build does not understand.
		return authz.Anonymous()
	}
}

// mustChangePasswordFrom reports whether the current login is pinned to the
// password-change page.
func mustChangePasswordFrom(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(mustChangePasswordContextKey)
	if !ok {
		return false
	}
	flag, _ := value.(bool)
	return flag
}

// usernameFrom returns the logged-in account's name, for display and logging.
// Empty when there is no login, or when the mode has no names.
func usernameFrom(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, ok := c.Get(usernameContextKey)
	if !ok {
		return ""
	}
	name, _ := value.(string)
	return name
}

// principalFrom returns the principal resolved for this request.
//
// A request that never passed through Authenticate yields the anonymous
// principal, which owns nothing and is not an admin. Callers therefore get a
// denial rather than an escalation when the middleware is missing — the
// failure mode of a routing mistake should not be open access.
func principalFrom(c *gin.Context) authz.Principal {
	if c == nil {
		return authz.Anonymous()
	}
	value, ok := c.Get(principalContextKey)
	if !ok {
		return authz.Anonymous()
	}
	p, ok := value.(authz.Principal)
	if !ok {
		return authz.Anonymous()
	}
	return p
}
