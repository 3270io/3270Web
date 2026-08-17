// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/host"
	"github.com/jnnngs/3270Web/internal/session"
)

// Display toggles are the terminal's own preferences — monocase, cursor
// blink, the crosshair, whether an underscore is drawn under input fields.
// They are settings of the *terminal*, not of this application, which is why
// they are read from and written to the terminal rather than stored here:
// there is no second copy to disagree with, and what the panel shows is what
// the terminal will actually do.
//
// One implementation, two doors, as with connection details: the browser
// arrives with its session cookie, the API with a bearer token.

// toggler is the part of a host that can read and write display toggles.
// Kept out of host.Host so a fake that only drives a terminal need not
// implement it.
type toggler interface {
	Toggles() ([]host.Toggle, error)
	SetToggle(name string, value bool) (*host.Toggle, error)
}

// sessionToggler resolves the session's host to a toggler, or writes the
// reason it could not and returns nil.
func (app *App) sessionToggler(c *gin.Context, s *session.Session) toggler {
	h := app.sessionHost(s)
	if h == nil {
		c.JSON(http.StatusConflict, gin.H{"error": errSessionHostMissing.Error()})
		return nil
	}
	if !h.IsConnected() {
		c.JSON(http.StatusConflict, gin.H{"error": errSessionNotConnected.Error()})
		return nil
	}
	t, ok := h.(toggler)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "this terminal does not expose display toggles"})
		return nil
	}
	return t
}

// writeToggleList answers a request for the current toggles.
func (app *App) writeToggleList(c *gin.Context, s *session.Session) {
	t := app.sessionToggler(c, s)
	if t == nil {
		return
	}
	toggles, err := t.Toggles()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": s.ID, "toggles": toggles})
}

// writeToggleSet applies one change and answers with the toggle as it stands
// afterwards.
func (app *App) writeToggleSet(c *gin.Context, s *session.Session) {
	var body struct {
		Name  string `json:"name"`
		Value *bool  `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	// A pointer, so that a body without "value" is refused rather than read as
	// "turn it off". Half of the possible requests would otherwise be
	// indistinguishable from a typo in the field name.
	if body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value is required and must be true or false"})
		return
	}
	t := app.sessionToggler(c, s)
	if t == nil {
		return
	}
	result, err := t.SetToggle(name, *body.Value)
	if err != nil {
		// A name outside the allowlist is the caller's mistake, and saying so
		// with the list of names that would have worked is more use than a
		// bare refusal.
		if strings.Contains(err.Error(), "not a settable display toggle") {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "available": host.SafeToggleNames()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": s.ID, "toggle": result})
}

// HostTogglesHandler handles GET /host/toggles — the caller's own session, by
// cookie. This is what the Connection details panel reads.
func (app *App) HostTogglesHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": errSessionNotFound.Error()})
		return
	}
	app.writeToggleList(c, s)
}

// HostToggleSetHandler handles POST /host/toggles — the cookie door.
func (app *App) HostToggleSetHandler(c *gin.Context) {
	s := app.getSession(c)
	if s == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": errSessionNotFound.Error()})
		return
	}
	app.writeToggleSet(c, s)
}

// APIToggles handles GET /api/v1/sessions/:id/toggles.
func (app *App) APIToggles(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	app.writeToggleList(c, s)
}

// APISetToggle handles POST /api/v1/sessions/:id/toggles.
func (app *App) APISetToggle(c *gin.Context) {
	s, ok := app.apiSessionFromPath(c)
	if !ok {
		return
	}
	app.writeToggleSet(c, s)
}
