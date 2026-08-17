// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/session"
)

// The terminal's Query action answers questions about the connection that the
// screen cannot: whether TN3270E negotiated, what the host bound the LU as,
// which telnet options are in force, what the TLS session is, how many records
// have crossed the wire. A session can render perfectly and still be connected
// on terms nobody asked for, and this is the only place that shows.
//
// One implementation, two doors: the browser reaches it with its session cookie
// and the API with a bearer token. The alternative — two handlers — is how the
// two surfaces end up disagreeing about what a field is called.

// elidedQueryValue is what the terminal puts in the bare Query response in place
// of its longest fields (Copyright, Proxies, Tasks), meaning "ask for this one
// by name".
const elidedQueryValue = "..."

// parseQueryFields splits the bare Query response into its fields. The response
// is one "Name: value" per line; a line without a colon is kept under its own
// name with an empty value rather than dropped, so a build that reports
// something in a shape we did not expect still shows up.
//
// It returns the map and the names in the order they were given, because that
// order is the terminal's own grouping and reads better than alphabetical.
func parseQueryFields(raw string) (map[string]string, []string) {
	fields := make(map[string]string)
	order := make([]string, 0, 24)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !found {
			value = ""
		}
		if _, seen := fields[name]; !seen {
			order = append(order, name)
		}
		fields[name] = strings.TrimSpace(value)
	}
	return fields, order
}

// writeHostQuery answers a connection-details request for one session.
//
// It always issues the *bare* Query, which returns every field the terminal
// knows in one command, and resolves ?name= out of that response rather than by
// sending the name on. That is not a shortcut, it is the safe design: a query
// keyword this build does not handle can block instead of erroring —
// Tn3270eFunctions does exactly that against a plain TN3270 server — and a
// blocked command on the terminal's control pipe is unrecoverable, so the
// wrapper's only way out is to kill the subprocess. One unknown name would take
// the session with it. Deriving the valid names from the terminal itself means
// no name we did not get from the terminal is ever sent to it, and an unknown
// one can be refused with the list of names that would have worked.
func (app *App) writeHostQuery(c *gin.Context, s *session.Session, name string) {
	h := app.sessionHost(s)
	if h == nil {
		c.JSON(http.StatusConflict, gin.H{"error": errSessionHostMissing.Error()})
		return
	}
	if !h.IsConnected() {
		c.JSON(http.StatusConflict, gin.H{"error": errSessionNotConnected.Error()})
		return
	}

	raw, err := h.Query("")
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	fields, order := parseQueryFields(raw)

	name = strings.TrimSpace(name)
	if name == "" {
		c.JSON(http.StatusOK, gin.H{
			"session":   s.ID,
			"queries":   fields,
			"available": order,
		})
		return
	}

	for _, known := range order {
		if !strings.EqualFold(known, name) {
			continue
		}
		value := fields[known]
		if value == elidedQueryValue {
			// The terminal abbreviates its longest fields to "..." in the bare
			// response and expects them to be asked for by name. Forwarding the
			// name is safe here in a way that forwarding an arbitrary one is not:
			// this name came out of the terminal's own list, so it is a keyword by
			// construction.
			full, err := h.Query(known)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
				return
			}
			value = full
		}
		c.JSON(http.StatusOK, gin.H{
			"session": s.ID,
			"name":    known,
			"value":   value,
		})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{
		"error":     "unknown query " + name,
		"available": order,
	})
}

// HostQueryHandler handles GET /host/query — the caller's own session, by
// cookie. This is what the Connection details panel reads.
func (app *App) HostQueryHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": errSessionNotFound.Error()})
		return
	}
	app.writeHostQuery(c, s, c.Query("name"))
}

// APIQuery handles GET /api/v1/sessions/:id/query — the bearer-auth variant.
func (app *App) APIQuery(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	app.writeHostQuery(c, s, c.Query("name"))
}
