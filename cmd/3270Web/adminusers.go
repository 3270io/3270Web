package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/users"
)

const adminUsersPath = "/admin/users"

// RequireAdmin gates instance-wide administration.
//
// Under AUTH_MODE=none the single operator is an administrator, so this
// changes nothing for the default deployment.
func (app *App) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := principalFrom(c)
		if principal.IsAnonymous() {
			app.rejectUnauthenticated(c)
			return
		}
		if !principal.IsAdmin() {
			if wantsJSON(c) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "this action requires an administrator account",
				})
				return
			}
			c.HTML(http.StatusForbidden, "error.html", gin.H{
				"Error": "This page requires an administrator account.",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// AdminUsersPageHandler serves the account management page.
func (app *App) AdminUsersPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "admin-users.html", gin.H{
		"MinLength":   users.MinPasswordLength,
		"AuthMode":    string(app.authMode),
		"AuthEnabled": app.authMode != authz.ModeNone,
		"Username":    usernameFrom(c),
	})
}

// adminUserView is the wire shape for one account. It never carries a hash;
// the store strips it, and this type has nowhere to put one.
type adminUserView struct {
	ID                 string `json:"id"`
	Username           string `json:"username"`
	Role               string `json:"role"`
	Disabled           bool   `json:"disabled"`
	MustChangePassword bool   `json:"mustChangePassword"`
	CreatedAt          string `json:"createdAt"`
	PasswordChangedAt  string `json:"passwordChangedAt"`
	// Self marks the caller's own account, so the UI can grey out the actions
	// that would lock them out of the instance they are using.
	Self bool `json:"self"`
	// External marks an account that signs in through the identity provider.
	// It has no password to reset, so the page says where it comes from rather
	// than offering a control that cannot work.
	External bool   `json:"external"`
	Issuer   string `json:"issuer,omitempty"`
	// Groups the account belongs to, so a profile audience can be written
	// against a team rather than a list of people.
	Groups []string `json:"groups"`
	// EffectiveRole is what the account actually holds once group assignments
	// are counted; RoleGroups names the groups responsible when it is more
	// than Role says. The page shows both, because "why is this person an
	// administrator" must be answerable by reading the row.
	EffectiveRole string   `json:"effectiveRole"`
	RoleGroups    []string `json:"roleGroups,omitempty"`
}

func toAdminUserView(u users.User, selfID string, groupRoles map[string]authz.Role) adminUserView {
	return adminUserView{
		ID:                 u.ID,
		Username:           u.Username,
		Role:               string(u.Role),
		Disabled:           u.Disabled,
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt.UTC().Format("2006-01-02 15:04"),
		PasswordChangedAt:  u.PasswordChangedAt.UTC().Format("2006-01-02 15:04"),
		Self:               u.ID == selfID,
		External:           u.External(),
		Issuer:             u.Issuer,
		Groups:             u.Groups,
		EffectiveRole:      string(users.EffectiveRole(u, groupRoles)),
		RoleGroups:         users.RoleGrantingGroups(u, groupRoles),
	}
}

// AdminListUsersHandler returns every account.
func (app *App) AdminListUsersHandler(c *gin.Context) {
	if app.authMode == authz.ModeNone {
		c.JSON(http.StatusOK, gin.H{"authEnabled": false, "users": []adminUserView{}})
		return
	}

	list, err := app.userStore().List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the account list"})
		return
	}

	groupRoles := app.groupRolesOrNone()
	selfID := principalFrom(c).UserID
	out := make([]adminUserView, 0, len(list))
	for _, u := range list {
		out = append(out, toAdminUserView(u, selfID, groupRoles))
	}
	c.JSON(http.StatusOK, gin.H{
		"authEnabled": true,
		"users":       out,
		// Every group in use, so the page can offer them rather than relying
		// on somebody spelling one the same way twice.
		"groups": app.knownGroups(c),
		// The role each group grants its members, keyed by group name.
		"groupRoles": groupRoles,
		// Where groups come from, so the page can say why it will not let
		// them be edited on a directory-owned account.
		"groupsFromProvider": app.ssoEnabled() && app.sso.GroupsClaim != "",
	})
}

type adminCreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// AdminCreateUserHandler adds an account.
func (app *App) AdminCreateUserHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	var req adminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	role := authz.RoleUser
	if strings.EqualFold(req.Role, string(authz.RoleAdmin)) {
		role = authz.RoleAdmin
	}

	username := strings.TrimSpace(req.Username)
	if err := users.ValidateUsername(username); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
		return
	}
	if err := users.ValidatePassword(req.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
		return
	}

	// New accounts must change the password the administrator chose: it was
	// typed by somebody else and probably sent over chat.
	user, err := app.userStore().Add(username, req.Password, role, true)
	if err != nil {
		if errors.Is(err, users.ErrUserExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "an account with that name already exists"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
		return
	}

	log.Printf("auth: %s created account %q (%s)", adminActor(c), user.Username, user.Role)
	app.auditRequest(c, audit.EventAccountCreated, audit.Success, user.Username,
		map[string]string{"role": string(user.Role)})
	c.JSON(http.StatusCreated, gin.H{"user": toAdminUserView(user, principalFrom(c).UserID, app.groupRolesOrNone())})
}

type adminUpdateUserRequest struct {
	// Pointers so "not mentioned" and "set to false" stay distinguishable.
	Role     *string `json:"role"`
	Disabled *bool   `json:"disabled"`
	Password *string `json:"password"`
	// Groups replaces the whole membership list. A pointer for the same
	// reason: "not mentioned" must not clear it, and an empty list must.
	Groups *[]string `json:"groups"`
}

// AdminUpdateUserHandler changes a role, enabled state or password.
func (app *App) AdminUpdateUserHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	target, ok := app.lookupTarget(c)
	if !ok {
		return
	}

	var req adminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	self := target.ID == principalFrom(c).UserID

	if req.Role != nil {
		role := authz.RoleUser
		if strings.EqualFold(*req.Role, string(authz.RoleAdmin)) {
			role = authz.RoleAdmin
		}
		// Self-demotion is refused rather than merely warned about: it takes
		// effect immediately and the administrator would lose the page they
		// are standing on, with no way back without the CLI.
		if self && role != authz.RoleAdmin {
			c.JSON(http.StatusBadRequest, gin.H{"error": "you cannot remove your own administrator role"})
			return
		}
		if err := app.userStore().SetRole(target.Username, role); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
			return
		}
		// A login carries the role it was created with, so the change has to be
		// pushed into the sessions the account is already in. Without this a
		// demotion would not take effect until the person signed in again — and
		// they could undo it from the page they were still standing on.
		//
		// What is pushed is the effective role: demoting somebody's own role
		// while a group still grants them administration demotes nothing, and
		// the session saying otherwise would disagree with every fresh login.
		target.Role = role
		app.authSessions.SetRoleFor(target.ID, app.effectiveRoleFor(target))
		log.Printf("auth: %s set %q role to %s", adminActor(c), target.Username, role)
		app.auditRequest(c, audit.EventAccountUpdated, audit.Success, target.Username,
			map[string]string{"change": "role", "role": string(role)})
	}

	if req.Disabled != nil {
		if self && *req.Disabled {
			c.JSON(http.StatusBadRequest, gin.H{"error": "you cannot disable your own account"})
			return
		}
		if err := app.userStore().SetDisabled(target.Username, *req.Disabled); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
			return
		}
		if *req.Disabled {
			// A disabled account must lose its live sessions, or "disabled"
			// only takes effect the next time they sign in.
			app.authSessions.DeleteAllFor(target.ID)
		}
		log.Printf("auth: %s set %q disabled=%v", adminActor(c), target.Username, *req.Disabled)
		app.auditRequest(c, audit.EventAccountUpdated, audit.Success, target.Username,
			map[string]string{"change": "disabled", "disabled": strconv.FormatBool(*req.Disabled)})
	}

	if req.Groups != nil {
		// Refused on an account the directory owns, rather than written and
		// then overwritten at the next sign-in — which would look like the
		// change had worked until somebody noticed it had not.
		if target.External() && app.sso.GroupsClaim != "" && app.ssoEnabled() {
			c.JSON(http.StatusConflict, gin.H{
				"error": "this account's groups come from the identity provider; change them there",
			})
			return
		}
		// Leaving a group can now remove a role. Doing that to yourself is
		// self-demotion wearing different clothes, and is refused for the
		// same reason: it takes effect immediately and strands you.
		if self && target.Role != authz.RoleAdmin {
			groupRoles := app.groupRolesOrNone()
			after := target
			after.Groups = users.NormaliseGroups(*req.Groups)
			if users.EffectiveRole(target, groupRoles) == authz.RoleAdmin &&
				users.EffectiveRole(after, groupRoles) != authz.RoleAdmin {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "your administrator role comes from one of these groups; you cannot leave it yourself",
				})
				return
			}
		}
		if err := app.userStore().SetGroups(target.Username, *req.Groups); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
			return
		}
		// Group membership can carry a role, so the sessions this account
		// already holds are given the recomputed one.
		target.Groups = users.NormaliseGroups(*req.Groups)
		app.authSessions.SetRoleFor(target.ID, app.effectiveRoleFor(target))
		log.Printf("auth: %s set groups for %q", adminActor(c), target.Username)
		app.auditRequest(c, audit.EventAccountUpdated, audit.Success, target.Username,
			map[string]string{"change": "groups", "groups": strings.Join(users.NormaliseGroups(*req.Groups), " ")})
	}

	if req.Password != nil {
		if err := users.ValidatePassword(*req.Password); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
			return
		}
		if err := app.userStore().SetPassword(target.Username, *req.Password); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
			return
		}
		// The administrator now knows this password, so the owner has to
		// replace it before doing anything else.
		if err := app.userStore().RequirePasswordChange(target.Username); err != nil {
			log.Printf("auth: could not flag %q for a password change: %v", target.Username, err)
		}
		if !self {
			app.authSessions.DeleteAllFor(target.ID)
		}
		log.Printf("auth: %s reset the password for %q", adminActor(c), target.Username)
		app.auditRequest(c, audit.EventAccountUpdated, audit.Success, target.Username,
			map[string]string{"change": "password reset"})
	}

	updated, found, err := app.userStore().ByID(target.ID)
	if err != nil || !found {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": toAdminUserView(updated, principalFrom(c).UserID, app.groupRolesOrNone())})
}

// AdminDeleteUserHandler removes an account.
func (app *App) AdminDeleteUserHandler(c *gin.Context) {
	if err := app.requireAuthEnabled(c); err != nil {
		return
	}

	target, ok := app.lookupTarget(c)
	if !ok {
		return
	}
	if target.ID == principalFrom(c).UserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "you cannot delete your own account"})
		return
	}

	if err := app.userStore().Delete(target.Username); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": humanPasswordError(err)})
		return
	}
	app.authSessions.DeleteAllFor(target.ID)
	// Its tokens already stop working — authenticateAPIToken looks the owner
	// up on every call — but marking them revoked keeps the token list honest
	// rather than showing dead credentials as active.
	//
	// Disabling does not do this, deliberately: the same live lookup refuses a
	// disabled account's tokens, and re-enabling should give the person their
	// automated clients back rather than making them reissue everything.
	if n, err := app.tokenStore().RevokeAllFor(target.ID); err != nil {
		log.Printf("auth: could not revoke tokens for %q: %v", target.Username, err)
	} else if n > 0 {
		log.Printf("auth: revoked %d API token(s) belonging to %q", n, target.Username)
		app.auditRequest(c, audit.EventTokenRevoked, audit.Success, target.Username,
			map[string]string{"count": strconv.Itoa(n), "reason": "account deleted"})
	}
	log.Printf("auth: %s deleted account %q", adminActor(c), target.Username)
	app.auditRequest(c, audit.EventAccountDeleted, audit.Success, target.Username, nil)
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// lookupTarget resolves the :id path parameter, answering the request itself
// when it cannot.
func (app *App) lookupTarget(c *gin.Context) (users.User, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing account id"})
		return users.User{}, false
	}
	user, found, err := app.userStore().ByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read the account"})
		return users.User{}, false
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such account"})
		return users.User{}, false
	}
	return user, true
}

// requireAuthEnabled rejects account management when there are no accounts to
// manage, which is clearer than silently succeeding against a store nothing
// reads.
func (app *App) requireAuthEnabled(c *gin.Context) error {
	if app.authMode != authz.ModeNone {
		return nil
	}
	err := errors.New("authentication is disabled")
	c.JSON(http.StatusConflict, gin.H{
		"error": "Account management needs AUTH_MODE=local. Restart 3270Web with it set.",
	})
	return err
}

// adminActor names the administrator for a log line.
func adminActor(c *gin.Context) string {
	if name := usernameFrom(c); name != "" {
		return name
	}
	return principalFrom(c).UserID
}
