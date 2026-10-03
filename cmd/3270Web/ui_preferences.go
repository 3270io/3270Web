// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// Preferences are always scoped to the authenticated caller, never a supplied ID.
func (app *App) UIPreferencesHandler(c *gin.Context) {
	p := principalFrom(c)
	if p.IsAnonymous() {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Sign in to save preferences.", "code": "authentication_required"})
		return
	}
	if p.UserID == authz.LocalUserID {
		c.JSON(http.StatusOK, gin.H{"local": true})
		return
	}
	u, found, err := app.userStore().ByID(p.UserID)
	if err != nil || !found {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load your preferences."})
		return
	}
	if c.Request.Method == http.MethodGet {
		c.JSON(http.StatusOK, u.UIPreferences)
		return
	}
	var patch users.UIPreferencesPatch
	if c.ShouldBindJSON(&patch) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid preference payload."})
		return
	}

	if (patch.WorkspaceMode != nil && *patch.WorkspaceMode != "" && *patch.WorkspaceMode != "business" && *patch.WorkspaceMode != "engineering") || (patch.TerminalView != nil && *patch.TerminalView != "" && *patch.TerminalView != "readable" && *patch.TerminalView != "overview") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown presentation choice."})
		return
	}
	prefs, err := app.userStore().PatchUIPreferences(p.UserID, patch)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not save your preferences."})
		return
	}
	c.JSON(http.StatusOK, prefs)
}

func (app *App) uiPreferences(c *gin.Context) users.UIPreferences {
	p := principalFrom(c)
	if p.IsAnonymous() || p.UserID == authz.LocalUserID {
		return users.UIPreferences{}
	}
	u, found, err := app.userStore().ByID(p.UserID)
	if err != nil || !found {
		return users.UIPreferences{}
	}
	return u.UIPreferences
}
