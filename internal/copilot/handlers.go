package copilot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// chatStreamTimeout is the server-side cap on one proxied /chat/completions
// stream, including all streamed tokens.
const chatStreamTimeout = 10 * time.Minute

// Handlers groups the routes the package exposes.
type Handlers struct {
	auth   *AuthManager
	client *Client

	mu     sync.Mutex
	device deviceState // most recent device-flow attempt
}

type deviceState struct {
	loginID   string
	code      string
	startedAt time.Time
	expiresIn time.Duration
}

// newLoginID returns a random token identifying one device-flow attempt.
// LoginPoll requires the caller to echo it back so that a second, unrelated
// /login/start (e.g. from another browser tab or a different user on a
// shared instance) can't silently redirect an in-flight poll onto its device
// code, which would attach whichever GitHub account finishes first to the
// single shared Copilot login.
func newLoginID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is effectively unrecoverable; fall back to a
		// time-derived value so login can still proceed.
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b)
}

// NewHandlers wires an AuthManager and Client together for HTTP routing.
func NewHandlers(auth *AuthManager) *Handlers {
	return &Handlers{
		auth:   auth,
		client: NewClient(auth),
	}
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
	c.JSON(http.StatusOK, gin.H{
		"logged_in":  h.auth.LoggedIn(),
		"enterprise": h.auth.EnterpriseURL(),
		"model":      DefaultModel,
	})
}

// Tools exposes the tool definitions to the frontend so the chat panel
// can build the chat/completions payload without duplicating the schema.
func (h *Handlers) Tools(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"tools":         DefaultTools(),
		"model":         DefaultModel,
		"system_prompt": DefaultSystemPrompt,
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
	if err := h.auth.SetEnterpriseURL(req.URL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enterprise": h.auth.EnterpriseURL()})
}

// Models returns the list of model IDs available through the Copilot API.
// If the user is not logged in, or the upstream /models call fails, we
// fall back to the static SupportedModels allowlist so the dropdown still
// populates and the user can pick a model before/independent of sign-in.
func (h *Handlers) Models(c *gin.Context) {
	if !h.auth.LoggedIn() {
		c.JSON(http.StatusOK, gin.H{"models": SupportedModels})
		return
	}
	ids, err := h.client.ListModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"models": SupportedModels})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": ids})
}

// LoginStart kicks off a GitHub OAuth device flow.
func (h *Handlers) LoginStart(c *gin.Context) {
	dc, err := h.auth.StartDeviceLogin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	loginID := newLoginID()
	h.mu.Lock()
	h.device = deviceState{
		loginID:   loginID,
		code:      dc.DeviceCode,
		startedAt: time.Now(),
		expiresIn: time.Duration(dc.ExpiresIn) * time.Second,
	}
	h.mu.Unlock()
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

	h.mu.Lock()
	state := h.device
	h.mu.Unlock()
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
	res, err := h.auth.PollDeviceLogin(c.Request.Context(), state.code)
	if err != nil && res.Status != "pending" && res.Status != "slow_down" {
		c.JSON(http.StatusOK, gin.H{"status": res.Status, "error": res.Error})
		return
	}
	if res.Status == "success" {
		// Clear the cached device code so it can't be reused.
		h.mu.Lock()
		if h.device.loginID == state.loginID {
			h.device = deviceState{}
		}
		h.mu.Unlock()
	}
	c.JSON(http.StatusOK, gin.H{"status": res.Status, "error": res.Error})
}

// Logout clears all cached tokens.
func (h *Handlers) Logout(c *gin.Context) {
	if err := h.auth.Logout(); err != nil {
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
	if !h.auth.LoggedIn() {
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

	if err := h.client.StreamChat(ctx, ChatRequest{Raw: payload}, pw); err != nil {
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
