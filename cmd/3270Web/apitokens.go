package main

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/apitoken"
	"github.com/jnnngs/3270Web/internal/audit"
	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/reqsec"
)

// tokenStore lazily opens the issued-token database.
func (app *App) tokenStore() *apitoken.Store {
	app.apiTokensOnce.Do(func() {
		if app.apiTokens == nil {
			app.apiTokens = apitoken.NewStore(app.tokensPath())
		}
	})
	return app.apiTokens
}

// tokensPath is where issued tokens live. Kept alongside the account store,
// since the two are only meaningful together.
func (app *App) tokensPath() string {
	if override := strings.TrimSpace(os.Getenv("API_TOKENS_PATH")); override != "" {
		return override
	}
	return filepath.Join(app.baseDir, "api-tokens.json")
}

// resolveTokensPath mirrors tokensPath for the CLI, which has no App.
func resolveTokensPath() string {
	if override := strings.TrimSpace(os.Getenv("API_TOKENS_PATH")); override != "" {
		return override
	}
	return filepath.Join(resolveStateDir(), "api-tokens.json")
}

// bearerToken extracts the credential from the Authorization header.
func bearerToken(c *gin.Context) (string, bool) {
	const prefix = "Bearer "
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	value := strings.TrimSpace(header[len(prefix):])
	return value, value != ""
}

// apiAuthError is an answer already decided: a status and a message. Keeping
// the two together stops a caller from pairing "unauthorized" with a body that
// says something else.
type apiAuthError struct {
	status  int
	message string
}

func (e apiAuthError) Error() string { return e.message }

var (
	errAPIDisabled = apiAuthError{http.StatusServiceUnavailable, "API disabled: no API token is configured"}
	errNoToken     = apiAuthError{http.StatusUnauthorized, "missing Bearer token"}
	errBadToken    = apiAuthError{http.StatusUnauthorized, "invalid token"}
)

// authenticateAPIToken resolves a presented bearer token into the principal it
// speaks for.
//
// Which credential is accepted follows from the deployment shape, and the two
// are deliberately not mixed:
//
//   - With a single operator there is one person, so one shared token in the
//     environment says everything there is to say about who is calling. It
//     resolves to that operator, which is why an API client can drive a
//     session opened in the browser.
//   - Where users are separated a shared token would be a hole straight
//     through that separation: one credential, held by everyone, reaching
//     everyone's sessions. Only tokens issued to an account are accepted, and
//     each one reaches exactly what its owner reaches.
func (app *App) authenticateAPIToken(presented string) (authz.Principal, error) {
	if !app.separatesUsers() {
		configured := strings.TrimSpace(os.Getenv("API_TOKEN"))
		if configured == "" {
			return authz.Anonymous(), errAPIDisabled
		}
		if presented == "" {
			return authz.Anonymous(), errNoToken
		}
		if subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) != 1 {
			return authz.Anonymous(), errBadToken
		}
		return authz.Service(), nil
	}

	if presented == "" {
		return authz.Anonymous(), errNoToken
	}
	token, err := app.tokenStore().Verify(presented)
	if err != nil {
		// One answer for every way a token can be unusable. Telling a caller
		// that the token is real but expired, or real but revoked, confirms
		// the token is real.
		if !errors.Is(err, apitoken.ErrMalformed) && !errors.Is(err, apitoken.ErrNotFound) {
			log.Printf("auth: refusing API token: %v", err)
		}
		return authz.Anonymous(), errBadToken
	}

	owner, found, err := app.userStore().ByID(token.UserID)
	if err != nil {
		return authz.Anonymous(), apiAuthError{http.StatusInternalServerError, "could not read the account"}
	}
	// A token outliving its account, or surviving the account being disabled,
	// would make both of those things mean less than they say.
	if !found || owner.Disabled {
		log.Printf("auth: refusing token %s: its account is gone or disabled", token.ID)
		return authz.Anonymous(), errBadToken
	}

	return authz.Token(owner.ID, owner.Role, token.Scopes), nil
}

// tokenScopeAllows reports whether a principal's scopes permit this request.
//
// Scope is decided by method rather than by route: a read-only credential is
// one that cannot change anything, and "does this request change anything" is
// exactly what the method says. Deriving it from a list of routes would mean
// every endpoint added later defaulted to whatever somebody remembered.
//
// A principal with no scopes — a browser, the single local operator — is
// unrestricted within its role, so this is a no-op for them.
func tokenScopeAllows(method string, principal authz.Principal) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return principal.HasScope(apitoken.ScopeRead)
	default:
		return principal.HasScope(apitoken.ScopeWrite)
	}
}

// RequireAPIToken authenticates the bearer token on /api/v1 and the MCP HTTP
// transport, and records who it belongs to.
//
// The principal is written to the context here rather than left to
// Authenticate, because a token client is not a browser: it holds no login
// cookie, so it would otherwise be anonymous and every ownership check
// downstream would refuse it.
func (app *App) RequireAPIToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		presented, _ := bearerToken(c)
		principal, err := app.authenticateAPIToken(presented)
		if err != nil {
			// A refused credential is the event worth having: a run of them is
			// what probing looks like. Successful calls are not recorded — the
			// API is the busy surface, and a line per request would bury
			// everything else in the file. "The API is switched off" is a
			// configuration state rather than an attempt, so it is left out.
			if !errors.Is(err, errAPIDisabled) {
				app.auditRecorder().Log(audit.Entry{
					Event:    audit.EventTokenRefused,
					Outcome:  audit.Denied,
					ClientIP: reqsec.ClientIP(c.Request),
					Detail:   map[string]string{"path": c.Request.URL.Path},
				})
			}
			var answer apiAuthError
			if !errors.As(err, &answer) {
				answer = apiAuthError{http.StatusUnauthorized, "invalid token"}
			}
			c.AbortWithStatusJSON(answer.status, gin.H{"error": answer.message})
			return
		}

		if !tokenScopeAllows(c.Request.Method, principal) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": fmt.Sprintf("this token has scope %s; %s needs the %q scope",
					strings.Join(principal.Scopes, "+"), c.Request.Method, apitoken.ScopeWrite),
			})
			return
		}

		c.Set(principalContextKey, principal)
		c.Next()
	}
}
