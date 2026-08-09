package main

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
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
	loginPath:       true,
	logoutPath:      true,
	setupPath:       true,
	ssoStartPath:    true,
	ssoCallbackPath: true,
	"/healthz":      true,
}

// publicPrefixes cover static assets, which must load before login so the
// login page is not unstyled.
var publicPrefixes = []string{"/static/", "/assets/", "/favicon"}

// tokenAuthPrefixes carry their own authentication and must not be sent to a
// login page.
//
// These are not public. /api/v1 — and MCP over HTTP inside it — is gated by
// RequireAPIToken, which authenticates a bearer token rather than a browser
// session. Letting the login gate run first would turn on authentication and
// silently break every API and MCP client at the same time: they present a
// valid token, hold no cookie, and would be answered with "authentication
// required" by a check that was never meant to judge them.
var tokenAuthPrefixes = []string{"/api/v1/", "/api/v1"}

// hasOwnAuth reports whether a path is authenticated by something other than
// the login session.
func hasOwnAuth(path string) bool {
	for _, prefix := range tokenAuthPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

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
// Scoped by the same rule as the session cookie, for the same reason: a framed
// terminal is a cross-site context, so a Lax login cookie is never sent and an
// embedded deployment with authentication on would show the sign-in page
// forever. See sessionCookieSameSite in embedding.go.
func (app *App) setAuthCookie(c *gin.Context, value string) {
	sameSite, secure := sessionCookieSameSite(c)
	maxAge := int(app.authAbsoluteTimeout.Seconds())
	if value == "" {
		maxAge = -1
	}
	c.SetSameSite(sameSite)
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
		path := c.Request.URL.Path
		if app.authMode == authz.ModeNone || isPublicPath(path) || hasOwnAuth(path) {
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

// proxyClaimsHTTPS reports that something in front of this server said the
// browser reached it over HTTPS, and that this server has not been told to
// believe it.
//
// It changes nothing about how the request is treated — IsTLS has already had
// its say, and the header is chosen by whoever sent the request. It only
// changes what the sign-in page says. "This connection is not encrypted" is
// alarming, correct, and useless to somebody whose CDN or ingress is doing
// exactly what they configured it to do; what they need is the name of the
// setting that closes the gap. Distinguishing the two cases is the difference
// between a warning that gets acted on and one that gets ignored.
func proxyClaimsHTTPS(c *gin.Context) bool {
	if c == nil || reqsec.IsTLS(c.Request) {
		return false
	}
	return reqsec.ForwardedProtoClaimsHTTPS(c.Request)
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
		"Error":          errMessage,
		"AppName":        "3270Web",
		"ShowNoTLS":      !reqsec.IsTLS(c.Request),
		"ProxySaysHTTPS": proxyClaimsHTTPS(c),
		"CSRFTokenH":     "",
		"SSO":            app.ssoView(),
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
		// Recorded separately from a plain failure: a run of these is the
		// shape of somebody working through a password list, and it reads
		// differently from one person mistyping.
		app.auditRecorder().Log(audit.Entry{
			Event:    audit.EventLoginLockedOut,
			Outcome:  audit.Denied,
			ClientIP: clientIP,
			Target:   username,
			Detail:   map[string]string{"retryIn": retryIn.Round(time.Second).String()},
		})
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
		// The trail may say why, even though the reply must not: it is read
		// by an administrator who is entitled to know the account was
		// disabled rather than the password wrong.
		app.auditRecorder().Log(audit.Entry{
			Event:    audit.EventLoginFailed,
			Outcome:  audit.Failure,
			ClientIP: clientIP,
			Target:   username,
			Detail:   map[string]string{"reason": loginFailureReason(err)},
		})
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

	// The login carries the effective role — the account's own, or one a
	// group it is in grants — because the session is where the role is read
	// from on every request that follows.
	sess, err := app.authSessions.Create(user.ID, user.Username, app.effectiveRoleFor(user), clientIP, user.MustChangePassword)
	if err != nil {
		log.Printf("auth: could not create session for %q: %v", username, err)
		app.failLogin(c, "Could not start a session. Try again.", http.StatusInternalServerError)
		return
	}

	app.loginLimiter.Reset(keys...)
	app.setAuthCookie(c, sess.ID)
	log.Printf("auth: %s logged in from %s", user.Username, clientIP)
	app.auditRecorder().Log(audit.Entry{
		Event:    audit.EventLoginSucceeded,
		Actor:    audit.Actor{UserID: user.ID, Username: user.Username, Role: string(user.Role), Kind: string(authz.KindWeb)},
		ClientIP: clientIP,
	})

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
		// Before the delete, while the principal is still resolvable.
		app.auditRequest(c, audit.EventLogout, audit.Success, "", nil)
		app.authSessions.Delete(id)
	}
	app.setAuthCookie(c, "")
	if wantsJSON(c) {
		c.JSON(http.StatusOK, gin.H{"loggedOut": true})
		return
	}
	// Where the deployment asked for it, signing out here also ends the
	// session at the provider — otherwise the next visit is signed straight
	// back in without being asked for anything, which on a shared machine is
	// not what anybody means by "sign out".
	if target := app.ssoLogoutURL(c); target != "" {
		c.Redirect(http.StatusFound, target)
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
	app.auditRequest(c, audit.EventPasswordChanged, audit.Success, user.Username,
		map[string]string{"otherSessions": "ended"})

	// Issue a fresh login so the person who just changed it stays signed in.
	sess, err := app.authSessions.Create(user.ID, user.Username, app.effectiveRoleFor(user), reqsec.ClientIP(c.Request), false)
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
	// UserID is the account's immutable identifier, rendered onto the page so
	// that browser-side state can be filed under the account rather than under
	// the browser. The AI chat transcript is the one that matters: it is kept
	// in localStorage, it quotes whole screens back, and keyed by host alone it
	// was the previous person's conversation waiting for the next person to
	// open the panel on a shared device.
	//
	// Not a secret — it identifies an account to itself, the way the username
	// already displayed beside it does — and empty where there is a single
	// operator, who has nobody to be separated from.
	UserID string
	// CanAdminister is what a template should ask before rendering a control
	// that only an administrator may use — Settings, the log viewer, restart.
	//
	// It is not IsAdmin. Under AUTH_MODE=none there is no principal to be an
	// administrator, but the single operator may do everything, which is
	// exactly what RequireAdmin allows. A template gating on IsAdmin would
	// hide Settings from the one person the default deployment is for; one
	// gating on Enabled would offer it to every ordinary account. This says
	// what the templates actually mean.
	CanAdminister bool
}

func (app *App) authView(c *gin.Context) authView {
	if app.authMode == authz.ModeNone {
		return authView{CanAdminister: true}
	}
	principal := principalFrom(c)
	if principal.IsAnonymous() {
		return authView{}
	}
	return authView{
		Enabled:       true,
		Username:      usernameFrom(c),
		UserID:        principal.UserID,
		IsAdmin:       principal.IsAdmin(),
		CanAdminister: principal.IsAdmin(),
	}
}

// loginFailureReason names why a sign-in was refused, for the audit trail
// only. The reply to the browser stays the same either way; an administrator
// reading the trail is entitled to the distinction the caller is not.
func loginFailureReason(err error) string {
	switch {
	case errors.Is(err, users.ErrUserDisabled):
		return "account disabled"
	case errors.Is(err, users.ErrInvalidCredentials):
		return "bad credentials"
	default:
		return "store error"
	}
}
