package copilot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/reqsec"
)

// chatStreamTimeout is the server-side cap on one proxied /chat/completions
// stream, including all streamed tokens.
const chatStreamTimeout = 10 * time.Minute

// Handlers groups the routes the package exposes. Per-browser auth/token
// state is resolved per-request via identity (see identityCookieName) rather
// than held directly on Handlers, so one browser's login doesn't leak into
// another's.
type Handlers struct {
	store *Store
}

type deviceState struct {
	loginID   string
	code      string
	startedAt time.Time
	expiresIn time.Duration
}

// identityCookieName names the long-lived cookie that scopes Copilot login
// state to one browser. It is independent of 3270Web_session (which churns
// on every host disconnect/reconnect and reaps after 2 hours idle) so that
// signing in to Copilot survives host-connection churn instead of forcing a
// fresh OAuth device-flow login every time.
const identityCookieName = "3270Web_copilot_id"

// copilotIdentityCookieMaxAge is one year, matching the "sign in once" UX of
// the real Copilot Chat editor extensions this package's wire protocol
// mirrors.
const copilotIdentityCookieMaxAge = 365 * 24 * 3600

// identityIDPattern matches exactly the shape randomID produces (32
// lowercase hex characters). The cookie value is used to build an on-disk
// file path (Store.get), so any value that doesn't match this exactly is
// discarded and replaced rather than trusted as a path component.
var identityIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// OwnerContextKey names the authenticated account this request belongs to, set
// by the host application on every request where the deployment separates
// users (see cmd/3270Web's Authenticate middleware). Empty, absent, or a
// deployment with a single operator all mean the same thing here: there is no
// account to bind AI state to, so the browser identity stands alone.
//
// A context value rather than a parameter because Identity is called from a
// dozen handlers across two packages and every one of them already has the
// request; and rather than an import of the authorization package because the
// dependency runs the other way — the application wires this package in, not
// the reverse.
const OwnerContextKey = "3270web.aiOwner"

// Identity resolves the calling browser's AI identity.
//
// It is derived from two things, and the second is the point.
//
// The browser cookie alone is what this used to be, and on a device more than
// one person signs in to it is not an identity at all — it is a property of
// the glass. The cookie lasts a year and nothing about signing out of the
// application touches it, so on a shared phone, a tablet at a desk, or a
// kiosk, the next person to sign in inherited whatever the last one had left
// behind it: a Copilot OAuth login, a provider API key, and the chat history
// keyed against it. Inheriting a credential is bad enough; the settings dialog
// then lets an endpoint be renamed while the stored key is kept, so the
// inherited secret could be sent somewhere of the new person's choosing.
//
// Mixing the account in makes the store key change the moment the account
// does, so the successor asks for a record that does not exist and gets a
// fresh one. There is nothing to inherit and nothing to revoke on logout.
//
// Where there is no account — AUTH_MODE=none, the single-operator deployment
// this program started as — the key is the cookie exactly as before, so that
// deployment's stored logins survive this change untouched.
func Identity(c *gin.Context) string {
	browserID := browserIdentity(c)
	owner := ownerFrom(c)
	if owner == "" {
		return browserID
	}
	// SHA-256 rather than concatenation: the result is used as a filename
	// component (Store.get, ConfigStore.get), and a hash of anything is 32 hex
	// characters that match identityIDPattern, whatever the account is called.
	sum := sha256.Sum256([]byte(owner + "\x00" + browserID))
	return hex.EncodeToString(sum[:16])
}

// ownerFrom reads the authenticated account off the request, if there is one.
func ownerFrom(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, ok := c.Get(OwnerContextKey)
	if !ok {
		return ""
	}
	owner, _ := value.(string)
	return strings.TrimSpace(owner)
}

// randomID returns a random 32-character lowercase-hex token. Used both to
// identify one device-flow login attempt and one browser's Copilot identity.
func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is effectively unrecoverable; fall back to a
		// time-derived value so login can still proceed.
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// NewHandlers wires a Store for HTTP routing.
func NewHandlers(store *Store) *Handlers {
	return &Handlers{store: store}
}

// browserIdentity resolves the calling browser's cookie identity, minting and
// cookie-ing a new one if the request has none (or an unrecognized one). The
// returned value always matches identityIDPattern, so it is safe to use as a
// filesystem path component.
//
// Must be called before any response headers/body are written, since it may
// itself set a cookie header.
func browserIdentity(c *gin.Context) string {
	id, err := c.Cookie(identityCookieName)
	if err != nil || !identityIDPattern.MatchString(id) {
		id = randomID()
		secure := reqsec.IsTLS(c.Request)
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(identityCookieName, id, copilotIdentityCookieMaxAge, "/", "", secure, true)
	}
	return id
}

// identity resolves the calling browser's Copilot auth state.
func (h *Handlers) identity(c *gin.Context) *identityAuth {
	return h.store.get(Identity(c))
}

// Register attaches the package's routes to the given Gin engine.
//
//	GET  /api/copilot/status
//	GET  /api/copilot/tools
//	POST /api/copilot/login/start
//	POST /api/copilot/login/poll
//	POST /api/copilot/logout
//	POST /api/copilot/enterprise   (set GHE URL)
//	POST /api/copilot/chat         (SSE proxy)
func (h *Handlers) Register(r gin.IRouter) {
	g := r.Group("/api/copilot")
	g.GET("/status", h.Status)
	g.GET("/tools", h.Tools)
	g.GET("/models", h.Models)
	g.POST("/login/start", h.LoginStart)
	g.POST("/login/poll", h.LoginPoll)
	g.POST("/logout", h.Logout)
	g.POST("/enterprise", h.SetEnterprise)
	g.POST("/chat", h.Chat)
}

// Status returns the current login state. No secrets are returned.
func (h *Handlers) Status(c *gin.Context) {
	ia := h.identity(c)
	c.JSON(http.StatusOK, gin.H{
		"logged_in":  ia.auth.LoggedIn(),
		"enterprise": ia.auth.EnterpriseURL(),
		"model":      DefaultModel,
	})
}

// Tools exposes the tool definitions to the frontend so the chat panel
// can build the chat/completions payload without duplicating the schema.
func (h *Handlers) Tools(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"tools":         DefaultTools(),
		"model":         DefaultModel,
		"system_prompt": SystemPrompt(),
		// See approval.go: the classification is the server's, so the browser
		// panel and an MCP client apply the same rule.
		"dangerous_send_keys": DangerousSendKeys(),
	})
}

type enterpriseRequest struct {
	URL string `json:"url"`
}

// SetEnterprise persists a GitHub Enterprise hostname.
func (h *Handlers) SetEnterprise(c *gin.Context) {
	var req enterpriseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ia := h.identity(c)
	if err := ia.auth.SetEnterpriseURL(req.URL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enterprise": ia.auth.EnterpriseURL()})
}

// Models returns the list of model IDs available through the Copilot API.
// If the user is not logged in, or the upstream /models call fails, we
// fall back to the static SupportedModels allowlist so the dropdown still
// populates and the user can pick a model before/independent of sign-in.
func (h *Handlers) Models(c *gin.Context) {
	ia := h.identity(c)
	if !ia.auth.LoggedIn() {
		c.JSON(http.StatusOK, gin.H{"models": SupportedModels})
		return
	}
	ids, err := ia.client.ListModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"models": SupportedModels})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": ids})
}

// LoginStart kicks off a GitHub OAuth device flow.
func (h *Handlers) LoginStart(c *gin.Context) {
	ia := h.identity(c)
	dc, err := ia.auth.StartDeviceLogin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	loginID := randomID()
	ia.mu.Lock()
	ia.device = deviceState{
		loginID:   loginID,
		code:      dc.DeviceCode,
		startedAt: time.Now(),
		expiresIn: time.Duration(dc.ExpiresIn) * time.Second,
	}
	ia.mu.Unlock()
	c.JSON(http.StatusOK, gin.H{
		"login_id":         loginID,
		"user_code":        dc.UserCode,
		"verification_uri": dc.VerificationURI,
		"expires_in":       dc.ExpiresIn,
		"interval":         dc.Interval,
	})
}

type loginPollRequest struct {
	LoginID string `json:"login_id"`
}

// LoginPoll asks GitHub whether the user has finished the device flow yet.
// Callers must echo the login_id returned by LoginStart; a mismatch means a
// different /login/start call has since superseded this one (e.g. another
// browser tab, or another user on a shared instance), so polling stops
// rather than silently tracking whichever attempt is currently in flight.
func (h *Handlers) LoginPoll(c *gin.Context) {
	var req loginPollRequest
	_ = c.ShouldBindJSON(&req)

	ia := h.identity(c)
	ia.mu.Lock()
	state := ia.device
	ia.mu.Unlock()
	if state.code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no login in progress"})
		return
	}
	if req.LoginID != state.loginID {
		c.JSON(http.StatusOK, gin.H{"status": "superseded", "error": "a newer sign-in attempt has started"})
		return
	}
	if state.expiresIn > 0 && time.Since(state.startedAt) > state.expiresIn {
		c.JSON(http.StatusOK, gin.H{"status": "expired"})
		return
	}
	res, err := ia.auth.PollDeviceLogin(c.Request.Context(), state.code)
	if err != nil && res.Status != "pending" && res.Status != "slow_down" {
		c.JSON(http.StatusOK, gin.H{"status": res.Status, "error": res.Error})
		return
	}
	if res.Status == "success" {
		// Clear the cached device code so it can't be reused.
		ia.mu.Lock()
		if ia.device.loginID == state.loginID {
			ia.device = deviceState{}
		}
		ia.mu.Unlock()
	}
	c.JSON(http.StatusOK, gin.H{"status": res.Status, "error": res.Error})
}

// Logout clears all cached tokens.
func (h *Handlers) Logout(c *gin.Context) {
	ia := h.identity(c)
	if err := ia.auth.Logout(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logged_in": false})
}

// Chat proxies a chat/completions request to Copilot and streams the SSE
// response back to the browser. The frontend is responsible for executing
// any tool calls and appending the results to the message history for the
// next /chat request.
func (h *Handlers) Chat(c *gin.Context) {
	ia := h.identity(c)
	if !ia.auth.LoggedIn() {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not logged in to copilot"})
		return
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Validate that messages exists and is non-empty.
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages required"})
		return
	}

	// Force SSE headers.
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)

	pw := &flushingWriter{w: c.Writer, f: flusher}

	// Cap the overall proxied stream so a hung upstream cannot pin the
	// connection (and its goroutine) forever. Generous enough for long
	// tool-heavy completions.
	ctx, cancel := context.WithTimeout(c.Request.Context(), chatStreamTimeout)
	defer cancel()

	if err := ia.client.StreamChat(ctx, ChatRequest{Raw: payload}, pw); err != nil {
		// If nothing has been written yet, we can still emit a clean JSON
		// error. Otherwise, send an `event: error` SSE frame the frontend
		// understands.
		if pw.bytes == 0 {
			c.Writer.Header().Set("Content-Type", "application/json")
			status := http.StatusBadGateway
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 600 {
				status = apiErr.Status
			}
			c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
			return
		}
		writeErrEvent(pw, err.Error())
	}
}

type flushingWriter struct {
	w     io.Writer
	f     http.Flusher
	bytes int
}

func (f *flushingWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	f.bytes += n
	if f.f != nil {
		f.f.Flush()
	}
	return n, err
}

func writeErrEvent(w io.Writer, msg string) {
	// Minimal escape: keep newlines out of the SSE event body.
	msg = strings.ReplaceAll(msg, "\n", " ")
	_, _ = io.WriteString(w, "event: error\ndata: "+msg+"\n\n")
}
