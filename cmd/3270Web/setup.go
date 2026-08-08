package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/reqsec"
	"github.com/jnnngs/3270Web/internal/users"
)

const setupPath = "/setup"

// setupState tracks whether the instance still needs its first administrator.
//
// The flag is cached rather than recomputed from the account store on every
// request: this is consulted by middleware on the hot path, and re-reading a
// file per request to answer a question that changes exactly once would be
// waste. It only ever flips one way, from true to false.
type setupState struct {
	mu       sync.RWMutex
	required bool
	// code must be presented on the setup form. It is generated per start and
	// only exists while setup is pending.
	code string
}

func (s *setupState) begin(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.required = true
	s.code = code
}

func (s *setupState) complete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.required = false
	s.code = ""
}

func (s *setupState) pending() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.required
}

// matches compares a submitted code in constant time.
func (s *setupState) matches(candidate string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.code == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(normaliseSetupCode(candidate)), []byte(s.code)) == 1
}

// newSetupCode returns a short, unambiguous code for reading off a log and
// typing into a form.
//
// Base32 without padding avoids the character pairs people misread when
// copying by hand (0/O, 1/I/l), which matters because this is a value someone
// transcribes from a terminal rather than pastes.
func newSetupCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate setup code: %w", err)
	}
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	return strings.ToUpper(raw), nil
}

// normaliseSetupCode makes transcription forgiving: case and the spacing or
// dashes people insert while copying are ignored.
func normaliseSetupCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if (r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// formatSetupCode groups the code for legibility in the log.
func formatSetupCode(code string) string {
	var parts []string
	for i := 0; i < len(code); i += 4 {
		end := i + 4
		if end > len(code) {
			end = len(code)
		}
		parts = append(parts, code[i:end])
	}
	return strings.Join(parts, "-")
}

// beginSetupIfNeeded arms first-run setup when authentication is on and no
// accounts exist yet.
//
// Requiring a code from the server's own log, rather than letting the first
// visitor claim the instance, is what keeps the window between "started" and
// "has an administrator" from being a land grab. Whoever can read the log is
// already trusted; whoever merely reached the port is not.
func (app *App) beginSetupIfNeeded() error {
	if app.authMode != authz.ModeLocal {
		return nil
	}

	count, err := app.userStore().Count()
	if err != nil {
		return fmt.Errorf("read user store %s: %w", app.userStore().Path(), err)
	}
	if count > 0 {
		app.setup.complete()
		return nil
	}

	code, err := newSetupCode()
	if err != nil {
		return err
	}
	app.setup.begin(code)

	log.Printf("auth: no accounts yet — open the web interface to create the first administrator")
	log.Printf("auth: setup code: %s", formatSetupCode(code))
	log.Printf("auth: the code is required once, and stops working as soon as the account exists")
	return nil
}

// RequireSetup funnels every request to the setup page until an administrator
// exists.
//
// Without this an operator would meet a sign-in form with no account that can
// pass it, which looks like a broken instance rather than one waiting to be
// configured.
func (app *App) RequireSetup() gin.HandlerFunc {
	return func(c *gin.Context) {
		pending := app.setup.pending()
		path := c.Request.URL.Path

		if !pending {
			// Setup is a one-time door; leaving it open afterwards would be a
			// second way to create an administrator.
			if path == setupPath {
				c.Redirect(http.StatusFound, loginPath)
				c.Abort()
				return
			}
			c.Next()
			return
		}

		if path == setupPath || isStaticAssetPath(path) || path == "/healthz" {
			c.Next()
			return
		}
		if wantsJSON(c) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "3270Web is not set up yet",
				"setup": setupPath,
			})
			return
		}
		c.Redirect(http.StatusFound, setupPath)
		c.Abort()
	}
}

// SetupPageHandler serves the first-run form.
func (app *App) SetupPageHandler(c *gin.Context) {
	app.renderSetup(c, http.StatusOK, "")
}

func (app *App) renderSetup(c *gin.Context, status int, errMessage string) {
	c.HTML(status, "setup.html", gin.H{
		"Error":     errMessage,
		"MinLength": users.MinPasswordLength,
		"LogPath":   app.logFilePath,
		"ShowNoTLS": !reqsec.IsTLS(c.Request),
	})
}

// SetupHandler creates the first administrator.
func (app *App) SetupHandler(c *gin.Context) {
	if !app.setup.pending() {
		c.Redirect(http.StatusFound, loginPath)
		return
	}

	clientIP := reqsec.ClientIP(c.Request)
	code := c.PostForm("setupCode")
	username := strings.TrimSpace(c.PostForm("username"))
	password := c.PostForm("password")
	confirm := c.PostForm("confirmPassword")

	// Throttle the code as if it were a password, because that is what it is.
	limitKeys := []string{"setup:" + clientIP}
	if ok, _ := app.loginLimiter.Allow(limitKeys...); !ok {
		app.renderSetup(c, http.StatusTooManyRequests, "Too many attempts. Try again shortly.")
		return
	}

	if !app.setup.matches(code) {
		app.loginLimiter.RecordFailure(limitKeys...)
		log.Printf("auth: setup attempted from %s with an incorrect code", clientIP)
		app.renderSetup(c, http.StatusUnauthorized, "That setup code is not correct. It is printed in the server log.")
		return
	}
	if err := users.ValidateUsername(username); err != nil {
		app.renderSetup(c, http.StatusBadRequest, humanPasswordError(err))
		return
	}
	if password != confirm {
		app.renderSetup(c, http.StatusBadRequest, "The passwords do not match.")
		return
	}
	if err := users.ValidatePassword(password); err != nil {
		app.renderSetup(c, http.StatusBadRequest, humanPasswordError(err))
		return
	}

	user, err := app.userStore().AddFirstAdmin(username, password)
	if err != nil {
		if errors.Is(err, users.ErrNotFirstUser) {
			// Another request won the race; there is nothing to set up.
			app.setup.complete()
			c.Redirect(http.StatusFound, loginPath)
			return
		}
		log.Printf("auth: setup failed: %v", err)
		app.renderSetup(c, http.StatusInternalServerError, "Could not create the account. Check the server log.")
		return
	}

	app.setup.complete()
	app.loginLimiter.Reset(limitKeys...)
	log.Printf("auth: first administrator %q created from %s; setup is now closed", user.Username, clientIP)

	// Sign the new administrator straight in — making them retype the password
	// they just chose proves nothing.
	sess, err := app.authSessions.Create(user.ID, user.Username, user.Role, clientIP, false)
	if err != nil {
		c.Redirect(http.StatusFound, loginPath)
		return
	}
	app.setAuthCookie(c, sess.ID)
	c.Redirect(http.StatusFound, "/")
}
