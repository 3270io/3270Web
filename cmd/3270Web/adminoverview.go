package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// The administration area's front door. Accounts, the audit trail and the
// live session list each answer one question; this page answers the one an
// administrator arrives with — "is everything all right?" — and then points
// at whichever of the others the answer leads to.

// AdminOverviewPageHandler serves the administration landing page.
func (app *App) AdminOverviewPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "admin-overview.html", gin.H{
		"AuthEnabled": app.authMode != authz.ModeNone,
		"AuthMode":    string(app.authMode),
	})
}

// overviewRecentLimit is how many audit entries the overview carries. Enough
// to say what has been happening; the audit page is one click away for more.
const overviewRecentLimit = 12

// AdminOverviewHandler aggregates the instance's state for the landing page:
// account counts, live logins, open terminal sessions and the recent trail.
// One request rather than four, because the page paints as a unit.
func (app *App) AdminOverviewHandler(c *gin.Context) {
	now := time.Now()
	authEnabled := app.authMode != authz.ModeNone

	accounts := gin.H{"total": 0, "admins": 0, "disabled": 0, "mustChange": 0, "external": 0}
	if authEnabled {
		list, err := app.userStore().List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the account list"})
			return
		}
		groupRoles := app.groupRolesOrNone()
		var admins, disabled, mustChange, external int
		for _, u := range list {
			// Effective administrators: one whose role comes from a group
			// belongs in this count exactly as much as one appointed directly.
			if users.EffectiveRole(u, groupRoles) == authz.RoleAdmin {
				admins++
			}
			if u.Disabled {
				disabled++
			}
			if u.MustChangePassword {
				mustChange++
			}
			if u.External() {
				external++
			}
		}
		accounts = gin.H{
			"total": len(list), "admins": admins, "disabled": disabled,
			"mustChange": mustChange, "external": external,
		}
	}

	// Live logins (browsers holding an auth cookie), distinct from terminal
	// sessions: one signed-in person may hold several terminals, or none.
	logins := 0
	loginUsers := 0
	if app.authSessions != nil {
		logins = app.authSessions.Count()
		loginUsers = len(app.authSessions.UserIDs())
	}

	// The audit read serves two displays at once: the recent-activity list,
	// and the last-24-hours counters on the tiles above it. 500 newest entries
	// bound the scan the same way the audit page bounds its own view.
	entries, err := app.auditRecorder().Read(500)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the audit log"})
		return
	}
	dayAgo := now.Add(-24 * time.Hour)
	var total24h, refused24h int
	for _, e := range entries {
		if e.Time.Before(dayAgo) {
			continue
		}
		total24h++
		if e.Outcome == audit.Denied || e.Outcome == audit.Failure {
			refused24h++
		}
	}
	recent := entries
	if len(recent) > overviewRecentLimit {
		recent = recent[:overviewRecentLimit]
	}
	if recent == nil {
		recent = []audit.Entry{}
	}

	c.JSON(http.StatusOK, gin.H{
		"authEnabled": authEnabled,
		"authMode":    string(app.authMode),
		"version":     appVersion,
		"startedAt":   app.startedAt.UTC().Format(time.RFC3339),
		"accounts":    accounts,
		"signedIn":    gin.H{"logins": logins, "users": loginUsers},
		"sessions": gin.H{
			"open":       app.SessionManager.Count(),
			"capTotal":   app.totalSessionCap(),
			"capPerUser": app.perUserSessionCap(),
		},
		"audit": gin.H{
			"recent":     recent,
			"total24h":   total24h,
			"refused24h": refused24h,
		},
	})
}
