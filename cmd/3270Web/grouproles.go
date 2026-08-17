// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

// Role inheritance through groups.
//
// A role assigned to a group is held by everyone in it, on top of whatever
// each account holds in its own right; see users.EffectiveRole. This file is
// where that resolution meets the request path: every place a principal's
// role is derived from an account goes through effectiveRoleFor, so a role
// granted to a team is indistinguishable from one granted to a person —
// which is the point.

// groupRolesOrNone reads the assignments, treating a store error as "none".
// The callers are authentication paths, where failing closed means the
// account's own role — never more — so degrading is safe.
func (app *App) groupRolesOrNone() map[string]authz.Role {
	if app.authMode == authz.ModeNone {
		return nil
	}
	roles, err := app.userStore().GroupRoles()
	if err != nil {
		log.Printf("auth: could not read group roles: %v", err)
		return nil
	}
	return roles
}

// effectiveRoleFor is the role this account actually signs in with.
func (app *App) effectiveRoleFor(u users.User) authz.Role {
	return users.EffectiveRole(u, app.groupRolesOrNone())
}

// pushEffectiveRoles recomputes every account's effective role and writes it
// into the login sessions they already hold. Called after anything that can
// change an inherited role — the assignment itself, or a group membership —
// because a promotion or demotion that waits for the next sign-in is not the
// change the administrator watched themselves make.
func (app *App) pushEffectiveRoles() {
	if app.authMode == authz.ModeNone || app.authSessions == nil {
		return
	}
	list, err := app.userStore().List()
	if err != nil {
		log.Printf("auth: could not read accounts to refresh roles: %v", err)
		return
	}
	groupRoles := app.groupRolesOrNone()
	for _, u := range list {
		app.authSessions.SetRoleFor(u.ID, users.EffectiveRole(u, groupRoles))
	}
}

type adminGroupRoleRequest struct {
	Group string `json:"group"`
	Role  string `json:"role"`
}

// AdminSetGroupRoleHandler assigns a role to a group, or clears the
// assignment by setting the role back to "user".
func (app *App) AdminSetGroupRoleHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	var req adminGroupRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	group := strings.TrimSpace(req.Group)
	role := authz.RoleUser
	if strings.EqualFold(req.Role, string(authz.RoleAdmin)) {
		role = authz.RoleAdmin
	}

	// Clearing an assignment your own administrator role depends on would
	// lock you out of the page you are standing on, exactly like demoting
	// yourself — and it is refused the same way. The store's own guard only
	// protects the last administrator; this one protects the caller.
	if role != authz.RoleAdmin && app.selfAdminDependsOnGroup(c, group) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "your own administrator role comes from this group; another administrator has to change it",
		})
		return
	}

	if err := app.userStore().SetGroupRole(group, role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
		return
	}

	log.Printf("auth: %s set group %q role to %s", adminActor(c), group, role)
	app.auditRequest(c, audit.EventGroupRoleSet, audit.Success, group,
		map[string]string{"role": string(role)})

	// The people in the group are already signed in; the change reaches them
	// now rather than at their next login.
	app.pushEffectiveRoles()

	c.JSON(http.StatusOK, gin.H{"ok": true, "groupRoles": app.groupRolesOrNone()})
}
