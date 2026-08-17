// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/reqsec"
)

// auditRecorder lazily opens the audit log.
func (app *App) auditRecorder() *audit.Recorder {
	app.auditOnce.Do(func() {
		if app.audit == nil {
			app.audit = audit.NewRecorder(app.auditPath())
		}
	})
	return app.audit
}

// auditPath is where the trail is written. Beside the account store rather
// than beside the debug log, because it is kept for a different reason and
// for longer.
func (app *App) auditPath() string {
	if override := strings.TrimSpace(os.Getenv("AUDIT_LOG_PATH")); override != "" {
		return override
	}
	return filepath.Join(app.baseDir, "audit.log")
}

// resolveAuditPath mirrors auditPath for the CLI, which has no App.
func resolveAuditPath() string {
	if override := strings.TrimSpace(os.Getenv("AUDIT_LOG_PATH")); override != "" {
		return override
	}
	return filepath.Join(resolveStateDir(), "audit.log")
}

// auditActor describes the caller of a request.
//
// The username comes from the login where there is one, so a line reads as a
// person rather than an ID. A token has no username, and saying so — "(token)"
// against the owner's ID — is more honest than leaving the field blank and
// letting a reader assume the browser.
func auditActor(c *gin.Context) audit.Actor {
	principal := principalFrom(c)
	actor := audit.Actor{
		UserID:   principal.UserID,
		Username: usernameFrom(c),
		Role:     string(principal.Role),
		Kind:     string(principal.Kind),
	}
	if actor.Username == "" && principal.UserID == authz.LocalUserID {
		actor.Username = "local operator"
	}
	return actor
}

// auditRequest records an event attributed to whoever made the request.
func (app *App) auditRequest(c *gin.Context, event audit.Event, outcome audit.Outcome, target string, detail map[string]string) {
	entry := audit.Entry{
		Event:   event,
		Outcome: outcome,
		Actor:   auditActor(c),
		Target:  target,
		Detail:  detail,
	}
	if c != nil && c.Request != nil {
		entry.ClientIP = reqsec.ClientIP(c.Request)
	}
	app.auditRecorder().Log(entry)
}

// auditPageLimit is how many entries the page asks for and shows. Enough to
// answer "what just happened" without turning a browser tab into a log
// viewer; the download is there for anything longer.
const auditPageLimit = 500

// AdminAuditPageHandler serves the trail as a page.
func (app *App) AdminAuditPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "admin-audit.html", gin.H{"Limit": auditPageLimit})
}

// AdminAuditHandler serves the recent trail as JSON.
//
// Admin-only, like the debug log — this file names accounts, addresses and
// the hosts they connected to, which is exactly the material somebody would
// want in order to work out who to impersonate.
func (app *App) AdminAuditHandler(c *gin.Context) {
	limit := 200
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = min(n, 2000)
		}
	}

	entries, err := app.auditRecorder().Read(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the audit log"})
		return
	}
	if entries == nil {
		entries = []audit.Entry{}
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "path": app.auditRecorder().Path()})
}

// AdminAuditDownloadHandler streams the whole trail, oldest first, for
// keeping or for feeding to something that keeps it.
func (app *App) AdminAuditDownloadHandler(c *gin.Context) {
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", `attachment; filename="3270Web-audit.jsonl"`)
	if err := app.auditRecorder().Dump(c.Writer); err != nil {
		// Too late for a status code — the header is already out — so this
		// ends the response and leaves the note in the debug log.
		c.Error(err) //nolint:errcheck // recorded on the context for the logger
	}
}
