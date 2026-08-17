// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/reqsec"
)

// Putting the terminal inside somebody else's page is not one change, it is
// four, and three of them are things this server does on purpose to keep
// other people's pages out: the frame is refused outright, the session cookie
// is not sent on a cross-site request, and a cross-origin POST is rejected as
// forgery. Loosening any of them by default would be a downgrade for every
// deployment that never embeds anything, which is most of them.
//
// So it is opt-in and it is named. EMBED_ORIGINS lists the exact origins that
// may frame the terminal — no wildcards, no "any origin", no reflecting back
// whatever the request claimed to be. An origin on that list gets a frame
// that works; every other origin is treated exactly as it was before the
// feature existed.
//
// The cost of embedding is that the session cookie has to travel on a
// cross-site request, which browsers only allow for SameSite=None, which they
// only accept on a Secure cookie. That makes HTTPS a requirement for
// embedding rather than a recommendation, and there is no configuration that
// makes it otherwise — see docs/embedding.md.

// embedOriginsEnv is the variable that names the origins allowed to frame
// this server. Comma or space separated, each a scheme://host[:port].
const embedOriginsEnv = "EMBED_ORIGINS"

// baseCSP is the policy every response carries. frame-ancestors is appended
// per request because it is the one directive that depends on configuration.
const baseCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self' ws: wss:;"

// embedOriginCache memoises the parse of EMBED_ORIGINS. The value is read on
// every request — the security headers run on every response, including every
// static asset — and the variable is writable at runtime through the settings
// page, so the cache is keyed on the raw string rather than filled once.
var embedOriginCache struct {
	mu      sync.RWMutex
	raw     string
	origins []string
}

// normalizeOrigin reduces one entry to the scheme://host[:port] form a browser
// sends in an Origin header, or reports why it cannot be used.
//
// The refusals matter more than the successes. A wildcard would make the
// allowlist meaningless; a bare hostname is ambiguous about scheme, and
// guessing http for it would mean a plaintext page could frame a terminal
// someone deployed over TLS; a path is a sign the operator has pasted a page
// URL and expects it to mean something narrower than it does.
func normalizeOrigin(entry string) (string, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", fmt.Errorf("empty origin")
	}
	if entry == "*" || strings.Contains(entry, "*") {
		return "", fmt.Errorf("%q: wildcards are not accepted; name each origin that may embed the terminal", entry)
	}
	u, err := url.Parse(entry)
	if err != nil {
		return "", fmt.Errorf("%q is not a URL: %w", entry, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%q must start with http:// or https://", entry)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%q names no host", entry)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%q: an origin is a scheme and a host, not a page — drop the path", entry)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("%q: an origin carries no query, fragment or credentials", entry)
	}
	return scheme + "://" + strings.ToLower(u.Host), nil
}

// parseEmbedOrigins turns the raw variable into the origins it names, and the
// complaints about the entries it could not use. Entries are separated by
// commas or whitespace, so a value pasted from either style works.
func parseEmbedOrigins(raw string) ([]string, []string) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	origins := make([]string, 0, len(fields))
	var problems []string
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		origin, err := normalizeOrigin(field)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if seen[origin] {
			continue
		}
		seen[origin] = true
		origins = append(origins, origin)
	}
	return origins, problems
}

// allowedEmbedOrigins returns the configured origins, or nil when embedding is
// off. An entry that cannot be parsed is dropped rather than taken as
// permission for anything.
func allowedEmbedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv(embedOriginsEnv))

	embedOriginCache.mu.RLock()
	if embedOriginCache.raw == raw {
		origins := embedOriginCache.origins
		embedOriginCache.mu.RUnlock()
		return origins
	}
	embedOriginCache.mu.RUnlock()

	origins, _ := parseEmbedOrigins(raw)

	embedOriginCache.mu.Lock()
	embedOriginCache.raw = raw
	embedOriginCache.origins = origins
	embedOriginCache.mu.Unlock()
	return origins
}

// embeddingEnabled reports whether any origin may frame this server.
func embeddingEnabled() bool {
	return len(allowedEmbedOrigins()) > 0
}

// isAllowedEmbedOrigin reports whether origin is one of the configured ones.
// The comparison is exact against a normalised form: scheme, host and port
// all have to match, because each of them is a different origin to a browser
// and should be a different decision here.
func isAllowedEmbedOrigin(origin string) bool {
	normalized, err := normalizeOrigin(origin)
	if err != nil {
		return false
	}
	for _, allowed := range allowedEmbedOrigins() {
		if allowed == normalized {
			return true
		}
	}
	return false
}

// frameAncestors is the CSP directive for the current configuration.
func frameAncestors() string {
	origins := allowedEmbedOrigins()
	if len(origins) == 0 {
		return "frame-ancestors 'self';"
	}
	return "frame-ancestors 'self' " + strings.Join(origins, " ") + ";"
}

// requestIsSecure reports whether the browser reached the edge over TLS,
// including through a proxy that terminated it.
//
// The answer comes from reqsec, which gates believing X-Forwarded-Proto on
// TRUST_PROXY_HEADERS. That gate belongs there rather than here: the header is
// set by whoever sends the request, so what makes it trustworthy is an
// operator saying a proxy sits in front of this server — not this server
// happening to have embedding configured. Every Secure-flag decision in the
// process then gets the same answer from the same place.
func requestIsSecure(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return reqsec.IsTLS(c.Request)
}

// sessionCookieSameSite decides how the session cookie is scoped.
//
// Lax is right for a terminal at its own URL and is what every deployment
// that does not embed keeps. A framed terminal is a cross-site context, where
// Lax means the cookie is simply not sent and the frame shows the connect
// page forever; None is the only value that works, and browsers only honour
// None on a Secure cookie. When the request did not arrive over TLS there is
// no combination that both works and is accepted, so it stays Lax: a cookie a
// browser will discard is not an improvement on one it will not send.
func sessionCookieSameSite(c *gin.Context) (http.SameSite, bool) {
	if embeddingEnabled() && requestIsSecure(c) {
		return http.SameSiteNoneMode, true
	}
	return http.SameSiteLaxMode, requestIsSecure(c)
}

// embedModeCookieName remembers that this browser arrived in embed mode, so
// the pages a redirect lands on stay in it. /screen is reached by redirect
// from the connect form, and a query parameter does not survive a redirect —
// without this the frame would show a chrome-less connect page and then a
// full-chrome terminal the moment it connected.
const embedModeCookieName = "3270Web_embed"

// embedRequested reports whether this page should render as an embedded
// terminal: no header, no toolbar, no background, just the screen.
//
// It is a presentation choice and nothing more. Nothing is permitted or
// forbidden by it, which is why it can be taken from a query parameter that
// anybody can add — what may frame this server is decided by EMBED_ORIGINS
// and enforced by the browser, not by this.
func embedRequested(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if q := c.Query("embed"); q != "" {
		return isTruthyParam(q)
	}
	return isTruthyParam(getCookieValue(c, embedModeCookieName))
}

// rememberEmbedMode records an explicit ?embed= choice for this browser so
// that later pages in the same frame agree with the first one. An explicit
// ?embed=0 clears it, which is how a session gets its chrome back without
// closing the tab.
func rememberEmbedMode(c *gin.Context) {
	q := strings.TrimSpace(c.Query("embed"))
	if q == "" {
		return
	}
	sameSite, secure := sessionCookieSameSite(c)
	c.SetSameSite(sameSite)
	if isTruthyParam(q) {
		c.SetCookie(embedModeCookieName, "1", 3600, "/", "", secure, true)
		return
	}
	c.SetCookie(embedModeCookieName, "", -1, "/", "", secure, true)
}

// embedOriginsAttr is the allowlist as it is handed to the page: a
// space-separated string on a body attribute.
//
// An attribute rather than an inline <script> because the content security
// policy this server sets has no 'unsafe-inline' for scripts, so a page that
// wrote its configuration into one would have to weaken the policy for every
// page to configure a feature used by a few. html/template escapes attribute
// values, and the values here are origins the operator wrote, so there is
// nothing to escape anyway.
func embedOriginsAttr() string {
	return strings.Join(allowedEmbedOrigins(), " ")
}

// EmbedCORSMiddleware lets a page on an allowlisted origin call /api/v1
// directly, which is the half of embedding that has nothing to do with
// frames: a single-page app that wants the screen as JSON and draws it
// itself, rather than showing the terminal in an iframe.
//
// It is deliberately narrower than the frame permission it shares a list
// with. No credentials are allowed, because this surface authenticates with a
// bearer token and never with the session cookie — so a browser that has a
// 3270Web session open cannot have it borrowed by a page that merely happens
// to be on the list. The origin is echoed back only after matching the
// allowlist exactly, never reflected, and Vary: Origin is set so a cache
// cannot serve one origin's permission to another.
func EmbedCORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" || !isAllowedEmbedOrigin(origin) {
			// A preflight from an origin that is not allowed gets an answer
			// that grants nothing, rather than being passed to a handler that
			// would 404 or 401 it. Either way the browser refuses the real
			// request; this way the reason is the CORS check, which is what an
			// integrator needs to see in the console.
			if c.Request.Method == http.MethodOptions {
				c.Header("Vary", "Origin")
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		normalized, _ := normalizeOrigin(origin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Origin", normalized)
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Max-Age", "600")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// EmbedConfigHandler handles GET /embed/config. It reports how this server is
// configured to be embedded, so an integrator can tell a misconfiguration
// from a browser problem without reading the server's environment — which is
// the difference between a five-minute setup and an afternoon of guessing at
// a blank frame.
//
// It names the origins already allowed rather than answering "may I embed
// you?" for the caller's own origin, because the second question is one an
// attacker would like answered and the first is only useful to somebody who
// can already read this page.
func (app *App) EmbedConfigHandler(c *gin.Context) {
	origins := allowedEmbedOrigins()
	_, problems := parseEmbedOrigins(strings.TrimSpace(os.Getenv(embedOriginsEnv)))
	out := gin.H{
		"enabled":         len(origins) > 0,
		"origins":         origins,
		"secure_request":  requestIsSecure(c),
		"frame_ancestors": frameAncestors(),
	}
	if len(problems) > 0 {
		out["ignored"] = problems
	}
	if len(origins) > 0 && !requestIsSecure(c) {
		out["warning"] = "the session cookie cannot be sent to a framed terminal over plain HTTP; serve this server over TLS, or through a proxy that sets X-Forwarded-Proto: https"
	}
	c.JSON(http.StatusOK, out)
}
