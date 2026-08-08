package main

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/reqsec"
	"github.com/jnnngs/3270Web/internal/users"
)

// authCookieName holds the login session identifier. It is separate from the
// terminal-session cookie so logging out ends the login without disturbing
// which terminal a tab is looking at.
const authCookieName = "3270Web_auth"

// loginPath and related routes are reachable without a login, for the obvious
// reason.
const (
	loginPath          = "/login"
	logoutPath         = "/logout"
	changePasswordPath = "/account/password"
)

// publicPaths are served without authentication when AUTH_MODE requires it.
//
// Kept as an explicit set rather than a prefix rule so adding a route never
// silently makes it public: anything not named here needs a login.
var publicPaths = map[string]bool{
	loginPath:  true,
	logoutPath: true,
	setupPath:  true,
	"/healthz": true,
}

// publicPrefixes cover static assets, which must load before login so the
// login page is not unstyled.
var publicPrefixes = []string{"/static/", "/assets/", "/favicon"}

// isStaticAssetPath reports whether a path serves page furniture that must
// load before any gate, so the login and setup pages are not unstyled.
func isStaticAssetPath(path string) bool {
	for _, prefix := range publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func isPublicPath(path string) bool {
	if publicPaths[path] {
		return true
	}
	for _, prefix := range publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// setAuthCookie writes (or clears, when value is empty) the login cookie.
func (app *App) setAuthCookie(c *gin.Context, value string) {
	secure := reqsec.IsTLS(c.Request)
	maxAge := int(app.authAbsoluteTimeout.Seconds())
	if value == "" {
		maxAge = -1
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(authCookieName, value, maxAge, "/", "", secure, true)
}

// bindSessionIP reports whether a login is pinned to the address that created
// it.
//
// Defaulting to on for plain HTTP is the point: there, the cookie travels in
// the clear and can be copied off the wire, so requiring the replay to come
// from the same address is the main thing standing between a passive
// eavesdropper and a usable session. Behind TLS the cookie is not visible in
// the first place, and pinning mostly punishes people whose address changes.
func (app *App) bindSessionIP(c *gin.Context) bool {
	switch strings.ToLower(strings.TrimSpace(app.authBindIP)) {
	case "true":
		return true
	case "false":
		return false
	default: // "auto" or unset
		return !reqsec.IsTLS(c.Request)
	}
}

// RequireLogin refuses requests that carry no valid login.
//
// It runs after Authenticate, which has already resolved the principal; this
// only decides whether an anonymous one may continue. With AUTH_MODE=none the
// principal is never anonymous, so this middleware admits everything and the
// single-operator deployment is unaffected.
func (app *App) RequireLogin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if app.authMode == authz.ModeNone || isPublicPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		principal := principalFrom(c)
		if principal.IsAnonymous() {
			app.rejectUnauthenticated(c)
			return
		}

		// An account with a system-issued password may do exactly one thing
		// until it is changed. Letting it roam would leave a credential the
		// operator printed to a terminal in circulation indefinitely.
		if mustChangePasswordFrom(c) && c.Request.URL.Path != changePasswordPath {
			if wantsJSON(c) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": "password change required",
				})
				return
			}
			c.Redirect(http.StatusFound, changePasswordPath)
			c.Abort()
			return
		}

		c.Next()
	}
}

// rejectUnauthenticated answers in the shape the caller expects: a redirect
// for a browser, JSON for anything scripted.
func (app *App) rejectUnauthenticated(c *gin.Context) {
	if wantsJSON(c) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	c.Redirect(http.StatusFound, loginPath)
	c.Abort()
}

// wantsJSON reports whether the caller is a script rather than a browser
// following links. A redirect to an HTML login page is useless to fetch() and
// actively confusing in an API client.
func wantsJSON(c *gin.Context) bool {
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		return true
	}
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	accept := c.GetHeader("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}

// LoginPageHandler serves the login form.
func (app *App) LoginPageHandler(c *gin.Context) {
	if app.authMode == authz.ModeNone {
		c.Redirect(http.StatusFound, "/")
		return
	}
	if !principalFrom(c).IsAnonymous() {
		c.Redirect(http.StatusFound, "/")
		return
	}
	app.renderLogin(c, http.StatusOK, "")
}

func (app *App) renderLogin(c *gin.Context, status int, errMessage string) {
	c.HTML(status, "login.html", gin.H{
		"Error":      errMessage,
		"AppName":    "3270Web",
		"ShowNoTLS":  !reqsec.IsTLS(c.Request),
		"CSRFTokenH": "",
	})
}

// LoginHandler verifies credentials and starts a login session.
func (app *App) LoginHandler(c *gin.Context) {
	if app.authMode == authz.ModeNone {
		c.Redirect(http.StatusFound, "/")
		return
	}

	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	clientIP := reqsec.ClientIP(c.Request)
	keys := loginLimitKeys(username, clientIP)

	if ok, retryIn := app.loginLimiter.Allow(keys...); !ok {
		log.Printf("auth: login throttled for %q from %s (%s remaining)",
			username, clientIP, retryIn.Round(time.Second))
		app.failLogin(c, "Too many failed attempts. Try again shortly.", http.StatusTooManyRequests)
		return
	}

	user, err := app.userStore().Authenticate(username, password)
	if err != nil {
		app.loginLimiter.RecordFailure(keys...)
		switch {
		case errors.Is(err, users.ErrUserDisabled):
			log.Printf("auth: login refused for disabled account %q from %s", username, clientIP)
		case errors.Is(err, users.ErrInvalidCredentials):
			log.Printf("auth: failed login for %q from %s", username, clientIP)
		default:
			log.Printf("auth: login error for %q from %s: %v", username, clientIP, err)
		}
		// One message for every failure. Distinguishing them would report
		// which usernames exist and which accounts are disabled.
		app.failLogin(c, "Incorrect username or password.", http.StatusUnauthorized)
		return
	}

	// Rotate: any pre-existing cookie value is discarded rather than adopted,
	// so a value planted before login cannot become an authenticated one.
	if existing := getCookieValue(c, authCookieName); existing != "" {
		app.authSessions.Delete(existing)
	}

	sess, err := app.authSessions.Create(user.ID, user.Username, user.Role, clientIP, user.MustChangePassword)
	if err != nil {
		log.Printf("auth: could not create session for %q: %v", username, err)
		app.failLogin(c, "Could not start a session. Try again.", http.StatusInternalServerError)
		return
	}

	app.loginLimiter.Reset(keys...)
	app.setAuthCookie(c, sess.ID)
	log.Printf("auth: %s logged in from %s", user.Username, clientIP)

	if user.MustChangePassword {
		c.Redirect(http.StatusFound, changePasswordPath)
		return
	}
	c.Redirect(http.StatusFound, "/")
}

func (app *App) failLogin(c *gin.Context, message string, status int) {
	if wantsJSON(c) {
		c.AbortWithStatusJSON(status, gin.H{"error": message})
		return
	}
	app.renderLogin(c, status, message)
	c.Abort()
}

// LogoutHandler ends the login session and clears the cookie.
func (app *App) LogoutHandler(c *gin.Context) {
	if id := getCookieValue(c, authCookieName); id != "" {
		app.authSessions.Delete(id)
	}
	app.setAuthCookie(c, "")
	if wantsJSON(c) {
		c.JSON(http.StatusOK, gin.H{"loggedOut": true})
		return
	}
	c.Redirect(http.StatusFound, loginPath)
}

// ChangePasswordPageHandler serves the forced password-change form.
func (app *App) ChangePasswordPageHandler(c *gin.Context) {
	if app.authMode == authz.ModeNone {
		c.Redirect(http.StatusFound, "/")
		return
	}
	c.HTML(http.StatusOK, "change-password.html", gin.H{
		"Error":     "",
		"MinLength": users.MinPasswordLength,
		"Forced":    mustChangePasswordFrom(c),
	})
}

// ChangePasswordHandler updates the caller's own password.
//
// Changing a password ends every other login for the account. A password
// change that left old sessions working would not actually revoke anything,
// which is the main reason people change one.
func (app *App) ChangePasswordHandler(c *gin.Context) {
	if app.authMode == authz.ModeNone {
		c.Redirect(http.StatusFound, "/")
		return
	}

	principal := principalFrom(c)
	if principal.IsAnonymous() {
		app.rejectUnauthenticated(c)
		return
	}

	current := c.PostForm("currentPassword")
	next := c.PostForm("newPassword")
	confirm := c.PostForm("confirmPassword")

	user, ok, err := app.userStore().ByID(principal.UserID)
	if err != nil || !ok {
		app.failChangePassword(c, "Could not load your account.", http.StatusInternalServerError)
		return
	}

	// Re-authenticate even though the caller is logged in: this is the check
	// that stops a borrowed session from locking its owner out of the account.
	if _, err := app.userStore().Authenticate(user.Username, current); err != nil {
		app.failChangePassword(c, "Your current password is incorrect.", http.StatusUnauthorized)
		return
	}
	if next != confirm {
		app.failChangePassword(c, "The new passwords do not match.", http.StatusBadRequest)
		return
	}
	if err := users.ValidatePassword(next); err != nil {
		app.failChangePassword(c, humanPasswordError(err), http.StatusBadRequest)
		return
	}
	if next == current {
		app.failChangePassword(c, "The new password must be different.", http.StatusBadRequest)
		return
	}

	if err := app.userStore().SetPassword(user.Username, next); err != nil {
		app.failChangePassword(c, "Could not save the new password.", http.StatusInternalServerError)
		return
	}

	app.authSessions.DeleteAllFor(user.ID)
	log.Printf("auth: %s changed their password; other sessions ended", user.Username)

	// Issue a fresh login so the person who just changed it stays signed in.
	sess, err := app.authSessions.Create(user.ID, user.Username, user.Role, reqsec.ClientIP(c.Request), false)
	if err != nil {
		app.setAuthCookie(c, "")
		c.Redirect(http.StatusFound, loginPath)
		return
	}
	app.setAuthCookie(c, sess.ID)

	if wantsJSON(c) {
		c.JSON(http.StatusOK, gin.H{"changed": true})
		return
	}
	c.Redirect(http.StatusFound, "/")
}

func (app *App) failChangePassword(c *gin.Context, message string, status int) {
	if wantsJSON(c) {
		c.AbortWithStatusJSON(status, gin.H{"error": message})
		return
	}
	c.HTML(status, "change-password.html", gin.H{
		"Error":     message,
		"MinLength": users.MinPasswordLength,
		"Forced":    mustChangePasswordFrom(c),
	})
	c.Abort()
}

// humanPasswordError strips the package prefix from a validation error so the
// form does not show "users: " to somebody choosing a password.
func humanPasswordError(err error) string {
	msg := err.Error()
	if idx := strings.Index(msg, ": "); idx >= 0 && strings.HasPrefix(msg, "users: ") {
		msg = msg[idx+2:]
	}
	if msg == "" {
		return "That password cannot be used."
	}
	return strings.ToUpper(msg[:1]) + msg[1:] + "."
}

// WhoAmIHandler reports the current login, for the UI to show who is signed in
// and whether a sign-out control belongs on the page.
func (app *App) WhoAmIHandler(c *gin.Context) {
	principal := principalFrom(c)
	c.JSON(http.StatusOK, gin.H{
		"authenticated": !principal.IsAnonymous(),
		"authMode":      string(app.authMode),
		"userId":        principal.UserID,
		"username":      usernameFrom(c),
		"role":          string(principal.Role),
		"isAdmin":       principal.IsAdmin(),
	})
}

// authView is the small bundle of identity a page template needs to render
// the signed-in chip. Kept as one value so a template never has to reason
// about which of several flags to consult.
type authView struct {
	// Enabled is false under AUTH_MODE=none, where there is nobody to show.
	Enabled  bool
	Username string
	IsAdmin  bool
}

func (app *App) authView(c *gin.Context) authView {
	if app.authMode == authz.ModeNone {
		return authView{}
	}
	principal := principalFrom(c)
	if principal.IsAnonymous() {
		return authView{}
	}
	return authView{
		Enabled:  true,
		Username: usernameFrom(c),
		IsAdmin:  principal.IsAdmin(),
	}
}
