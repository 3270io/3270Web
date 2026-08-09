package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/oidc"
	"github.com/jnnngs/3270Web/internal/reqsec"
)

// Where a browser goes to sign in through the identity provider, and where the
// provider sends it back.
const (
	ssoStartPath    = "/auth/sso"
	ssoCallbackPath = "/auth/sso/callback"
)

// pendingLoginTTL is how long a sign-in may take.
//
// Long enough for a password, a second factor and a consent screen; short
// enough that a state value captured from a browser's history is no longer
// redeemable by the time anybody reads it.
const pendingLoginTTL = 10 * time.Minute

// maxPendingLogins bounds what an unauthenticated caller can make this server
// remember. /auth/sso is reachable without credentials by design, so without a
// ceiling it is a way to fill memory one redirect at a time.
const maxPendingLogins = 512

// ssoSettings is the deployment's identity-provider configuration.
type ssoSettings struct {
	// UsernameClaim names the claim to take a display name from. Empty means
	// try the usual ones in turn.
	UsernameClaim string
	// GroupsClaim names the claim carrying group or role membership.
	GroupsClaim string
	// AdminGroups grants the admin role. Empty means the provider says nothing
	// about roles, and an administrator sets them here instead.
	AdminGroups []string
	// AllowedGroups restricts who may sign in at all. Empty means anybody the
	// provider authenticates.
	AllowedGroups []string
	// EndSessionOnLogout also ends the session at the provider.
	EndSessionOnLogout bool
}

// pendingLogin is one sign-in in flight.
//
// The nonce and the PKCE verifier are held here rather than in a cookie: a
// cookie is something the browser carries, and both of these are values the
// browser must never be able to choose. state is the map key, so a callback
// that names one this server did not issue finds nothing.
type pendingLogin struct {
	Nonce    string
	Verifier string
	// ReturnTo is where to land afterwards. Always a path on this server; see
	// safeReturnPath.
	ReturnTo  string
	ClientIP  string
	CreatedAt time.Time
}

// pendingLoginStore holds sign-ins between the redirect out and the callback.
type pendingLoginStore struct {
	mu      sync.Mutex
	pending map[string]pendingLogin
	now     func() time.Time
}

func newPendingLoginStore() *pendingLoginStore {
	return &pendingLoginStore{pending: make(map[string]pendingLogin), now: time.Now}
}

// begin records a sign-in, dropping stale ones first.
func (s *pendingLoginStore) begin(state string, p pendingLogin) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	if len(s.pending) >= maxPendingLogins {
		return false
	}
	s.pending[state] = p
	return true
}

// take returns a sign-in and forgets it.
//
// One use only. A state that could be redeemed twice would let a captured
// callback URL be replayed into a second login.
func (s *pendingLoginStore) take(state string) (pendingLogin, bool) {
	if state == "" {
		return pendingLogin{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	p, ok := s.pending[state]
	delete(s.pending, state)
	return p, ok
}

func (s *pendingLoginStore) expireLocked() {
	cutoff := s.now().Add(-pendingLoginTTL)
	for state, p := range s.pending {
		if p.CreatedAt.Before(cutoff) {
			delete(s.pending, state)
		}
	}
}

// ssoEnabled reports whether this instance can sign somebody in through a
// provider.
func (app *App) ssoEnabled() bool {
	return app.authMode == authz.ModeOIDC && app.oidcProvider != nil
}

// configureSSO reads the identity-provider settings and builds the client.
//
// Called from configureAuth, so a misconfigured provider stops startup rather
// than surfacing as a broken button. The exception is reachability: discovery
// is deferred, because a provider that is down must not stop an instance from
// starting and offering the local sign-in that is the way back in.
func (app *App) configureSSO() error {
	if app.authMode != authz.ModeOIDC {
		return nil
	}

	// Named one at a time, because this is read at startup by somebody setting
	// the mode up for the first time, and "no issuer configured" does not say
	// which of four variables they missed.
	for _, required := range []struct{ name, value string }{
		{"OIDC_ISSUER", os.Getenv("OIDC_ISSUER")},
		{"OIDC_CLIENT_ID", os.Getenv("OIDC_CLIENT_ID")},
		{"OIDC_REDIRECT_URL", os.Getenv("OIDC_REDIRECT_URL")},
	} {
		if strings.TrimSpace(required.value) == "" {
			return fmt.Errorf("%s=%s needs %s to be set", authz.ModeEnv, authz.ModeOIDC, required.name)
		}
	}

	provider, err := oidc.New(oidc.Config{
		Issuer:       os.Getenv("OIDC_ISSUER"),
		ClientID:     os.Getenv("OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		RedirectURL:  strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL")),
		Scopes:       splitList(os.Getenv("OIDC_SCOPES")),
	})
	if err != nil {
		return err
	}

	app.oidcProvider = provider
	app.sso = ssoSettings{
		UsernameClaim:      strings.TrimSpace(os.Getenv("OIDC_USERNAME_CLAIM")),
		GroupsClaim:        strings.TrimSpace(os.Getenv("OIDC_GROUPS_CLAIM")),
		AdminGroups:        splitList(os.Getenv("OIDC_ADMIN_GROUPS")),
		AllowedGroups:      splitList(os.Getenv("OIDC_ALLOWED_GROUPS")),
		EndSessionOnLogout: envTruthy("OIDC_END_SESSION"),
	}
	if app.sso.GroupsClaim == "" {
		app.sso.GroupsClaim = "groups"
	}
	if app.ssoPending == nil {
		app.ssoPending = newPendingLoginStore()
	}
	log.Printf("auth: sign-in through %s is configured", provider.Issuer())
	return nil
}

// SSOStartHandler sends the browser to the identity provider.
func (app *App) SSOStartHandler(c *gin.Context) {
	if !app.ssoEnabled() {
		c.Redirect(http.StatusFound, loginPath)
		return
	}
	if !principalFrom(c).IsAnonymous() {
		c.Redirect(http.StatusFound, "/")
		return
	}

	clientIP := reqsec.ClientIP(c.Request)
	// Throttled on the same limiter as the password form. Each start costs a
	// remembered pending login and a call to the provider, so it is a cost an
	// anonymous caller can impose.
	if ok, _ := app.loginLimiter.Allow("sso:" + clientIP); !ok {
		app.renderLogin(c, http.StatusTooManyRequests, "Too many sign-in attempts. Try again shortly.")
		return
	}

	state, err1 := oidc.RandomString(32)
	nonce, err2 := oidc.RandomString(32)
	verifier, err3 := oidc.RandomString(48)
	if err1 != nil || err2 != nil || err3 != nil {
		log.Printf("auth: could not start SSO sign-in: %v %v %v", err1, err2, err3)
		app.renderLogin(c, http.StatusInternalServerError, "Could not start sign-in. Try again.")
		return
	}

	pending := pendingLogin{
		Nonce:     nonce,
		Verifier:  verifier,
		ReturnTo:  safeReturnPath(c.Query("next")),
		ClientIP:  clientIP,
		CreatedAt: time.Now(),
	}
	if !app.ssoPending.begin(state, pending) {
		app.renderLogin(c, http.StatusServiceUnavailable, "Too many sign-ins are in progress. Try again shortly.")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	target, err := app.oidcProvider.AuthCodeURL(ctx, state, nonce, verifier)
	if err != nil {
		// The provider being unreachable is the common case here, and the
		// message has to point at the way in that still works.
		log.Printf("auth: could not reach the identity provider: %v", err)
		app.renderLogin(c, http.StatusBadGateway,
			"The identity provider could not be reached. An account with a password can still sign in below.")
		return
	}
	c.Redirect(http.StatusFound, target)
}

// SSOCallbackHandler completes a sign-in the provider has sent back.
func (app *App) SSOCallbackHandler(c *gin.Context) {
	if !app.ssoEnabled() {
		c.Redirect(http.StatusFound, loginPath)
		return
	}

	clientIP := reqsec.ClientIP(c.Request)
	// Consumed before anything else is looked at, so a callback can only ever
	// be redeemed once whatever it turns out to say.
	pending, found := app.ssoPending.take(c.Query("state"))
	if !found {
		// Either forged, replayed, or a sign-in that took longer than the
		// window. All three are answered the same way: start again.
		log.Printf("auth: SSO callback from %s with an unknown state", clientIP)
		app.failSSO(c, clientIP, "unknown state", "That sign-in has expired or was not started here. Try again.")
		return
	}
	if providerErr := c.Query("error"); providerErr != "" {
		log.Printf("auth: the identity provider refused a sign-in from %s: %s", clientIP, providerErr)
		app.failSSO(c, clientIP, "provider refused: "+providerErr,
			"The identity provider did not complete the sign-in.")
		return
	}
	code := c.Query("code")
	if code == "" {
		app.failSSO(c, clientIP, "no authorization code", "That sign-in did not complete. Try again.")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	token, err := app.oidcProvider.Exchange(ctx, code, pending.Verifier, pending.Nonce)
	if err != nil {
		// Logged in full because an operator debugging a new provider needs
		// the reason; shown to the browser in one sentence because the person
		// signing in cannot act on it.
		log.Printf("auth: SSO sign-in from %s failed: %v", clientIP, err)
		app.failSSO(c, clientIP, err.Error(), "The identity provider's answer could not be accepted.")
		return
	}

	if !app.ssoGroupsAllowSignIn(token) {
		log.Printf("auth: %s is authenticated but in none of the groups allowed to sign in", token.Subject)
		app.failSSO(c, clientIP, "not in an allowed group",
			"Your account is not permitted to use this 3270Web instance.")
		return
	}

	username := app.ssoUsername(token)
	if username == "" {
		app.failSSO(c, clientIP, "no usable username claim",
			"The identity provider did not supply a username.")
		return
	}

	user, err := app.userStore().UpsertExternal(
		app.oidcProvider.Issuer(), token.Subject, username,
		app.ssoRole(token), app.ssoGroups(token))
	if err != nil {
		log.Printf("auth: could not resolve %q from the identity provider: %v", username, err)
		message := "Your account could not be set up on this instance."
		if strings.Contains(err.Error(), "local account already uses") {
			message = "A local account already uses that name. An administrator has to rename it."
		}
		app.failSSO(c, clientIP, err.Error(), message)
		return
	}
	// Disabling is this instance's own veto, and it has to outrank the
	// provider: an administrator who disables somebody here means it whether
	// or not the directory still authenticates them.
	if user.Disabled {
		log.Printf("auth: refusing SSO sign-in for disabled account %q", user.Username)
		app.failSSO(c, clientIP, "account disabled", "That account is disabled on this instance.")
		return
	}

	if existing := getCookieValue(c, authCookieName); existing != "" {
		app.authSessions.Delete(existing)
	}
	sess, err := app.authSessions.Create(user.ID, user.Username, user.Role, clientIP, false)
	if err != nil {
		log.Printf("auth: could not create a session for %q: %v", user.Username, err)
		app.failSSO(c, clientIP, "session creation failed", "Could not start a session. Try again.")
		return
	}
	app.loginLimiter.Reset("sso:" + clientIP)
	app.setAuthCookie(c, sess.ID)

	log.Printf("auth: %s signed in through the identity provider from %s", user.Username, clientIP)
	app.auditRecorder().Log(audit.Entry{
		Event: audit.EventLoginSucceeded,
		Actor: audit.Actor{UserID: user.ID, Username: user.Username,
			Role: string(user.Role), Kind: string(authz.KindWeb)},
		ClientIP: clientIP,
		Detail:   map[string]string{"method": "sso", "issuer": app.oidcProvider.Issuer()},
	})

	c.Redirect(http.StatusFound, pending.ReturnTo)
}

// failSSO records a refused sign-in and shows the login page saying why.
func (app *App) failSSO(c *gin.Context, clientIP, reason, message string) {
	app.loginLimiter.RecordFailure("sso:" + clientIP)
	app.auditRecorder().Log(audit.Entry{
		Event:    audit.EventLoginFailed,
		Outcome:  audit.Failure,
		ClientIP: clientIP,
		Detail:   map[string]string{"method": "sso", "reason": reason},
	})
	app.renderLogin(c, http.StatusUnauthorized, message)
}

// ssoUsername picks the display name for an identity.
//
// The configured claim wins. Without one the usual candidates are tried in
// turn, ending at the subject — which is always present, so a provider that
// sends nothing else still produces a usable account rather than a refusal.
//
// This is a name, not the identity: the account is found by issuer and
// subject, so a collision or a rename changes what somebody is called and
// nothing else.
func (app *App) ssoUsername(token *oidc.IDToken) string {
	candidates := []string{app.sso.UsernameClaim}
	if app.sso.UsernameClaim == "" {
		candidates = []string{"preferred_username", "email", "name"}
	}
	for _, claim := range candidates {
		if name := sanitiseUsername(token.Claim(claim)); name != "" {
			return name
		}
	}
	return sanitiseUsername(token.Subject)
}

// sanitiseUsername reshapes a claim into a name the account store accepts.
//
// The store's charset is deliberately narrow so that two accounts cannot look
// alike in a log or an audit line, and a claim is prose from somewhere else —
// an email address, a display name with a space in it. Characters outside the
// set become a dash rather than being dropped, so "a.b@c" and "a.bc" stay
// distinguishable.
func sanitiseUsername(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > 64 {
		name = strings.Trim(name[:64], "-")
	}
	return name
}

// ssoRole maps the provider's groups onto a role.
//
// An empty result means "this deployment does not map roles", which
// UpsertExternal reads as leave the account as it is. That is what lets an
// administrator promote somebody on the Accounts page without the next
// sign-in undoing it.
func (app *App) ssoRole(token *oidc.IDToken) authz.Role {
	if len(app.sso.AdminGroups) == 0 {
		return ""
	}
	if intersects(token.ClaimList(app.sso.GroupsClaim), app.sso.AdminGroups) {
		return authz.RoleAdmin
	}
	// Explicitly the user role rather than "leave it": where the provider is
	// the authority on who administers, leaving somebody an administrator
	// after they left the group would be the provider failing to revoke.
	return authz.RoleUser
}

// ssoGroups is the directory's group list for this identity, or nil where the
// deployment maps no claim.
//
// nil rather than empty, because the two mean different things to the account
// store: nil leaves whatever an administrator set here, empty says the
// directory is the authority and this person is in nothing.
func (app *App) ssoGroups(token *oidc.IDToken) []string {
	if app.sso.GroupsClaim == "" {
		return nil
	}
	groups := token.ClaimList(app.sso.GroupsClaim)
	if groups == nil {
		// The claim is configured but absent from the token. Treated as "in
		// nothing" rather than "say nothing": a provider that stopped sending
		// the claim has removed the person from every group as far as anyone
		// here can tell, and keeping stale membership would keep access it
		// was meant to withdraw.
		return []string{}
	}
	return groups
}

// ssoGroupsAllowSignIn reports whether the identity may sign in at all.
func (app *App) ssoGroupsAllowSignIn(token *oidc.IDToken) bool {
	if len(app.sso.AllowedGroups) == 0 {
		return true
	}
	return intersects(token.ClaimList(app.sso.GroupsClaim), app.sso.AllowedGroups)
}

// intersects reports whether the two lists share a value, comparing without
// regard to case because directories are inconsistent about it.
func intersects(have, want []string) bool {
	for _, h := range have {
		for _, w := range want {
			if strings.EqualFold(strings.TrimSpace(h), w) {
				return true
			}
		}
	}
	return false
}

// safeReturnPath keeps a "next" parameter pointing at this server.
//
// Only a rooted path, never a URL and never a protocol-relative "//host" —
// otherwise the sign-in page is an open redirect, and one that hands somebody
// a landing page immediately after they typed a password is the most
// convincing place to have one.
func safeReturnPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.Contains(raw, "\\") {
		return "/"
	}
	return raw
}

// ssoLogoutURL is where to send the browser after signing out, when the
// deployment asked for the provider's session to end too.
func (app *App) ssoLogoutURL(c *gin.Context) string {
	if !app.ssoEnabled() || !app.sso.EndSessionOnLogout {
		return ""
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	return app.oidcProvider.EndSessionURL(ctx, "", absoluteURL(c, loginPath))
}

// absoluteURL renders a path on this server as a full URL, which is what a
// provider's post-logout redirect has to be given.
func absoluteURL(c *gin.Context, path string) string {
	scheme := "http"
	if reqsec.IsTLS(c.Request) {
		scheme = "https"
	}
	host := c.Request.Host
	if host == "" {
		return ""
	}
	return scheme + "://" + host + path
}

// splitList reads a comma-separated setting.
func splitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// envTruthy reads the spellings of "yes" an environment variable turns up with.
func envTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ssoView is what the sign-in page needs to know about the provider.
type ssoView struct {
	Enabled bool
	// StartPath is where the button goes.
	StartPath string
	// LocalSignIn reports whether the password form is worth showing. It always
	// is: the local account is the way back in when the provider is down.
	LocalSignIn bool
}

func (app *App) ssoView() ssoView {
	if !app.ssoEnabled() {
		return ssoView{LocalSignIn: true}
	}
	return ssoView{Enabled: true, StartPath: ssoStartPath, LocalSignIn: true}
}
