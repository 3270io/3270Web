package aiprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/copilot"
)

// chatStreamTimeout caps one proxied /chat stream, including all streamed
// tokens. Matches internal/copilot.
const chatStreamTimeout = 10 * time.Minute

// Handlers exposes provider selection, credential storage, and the chat proxy
// under /api/ai. Per-browser state is resolved per request from the identity
// cookie, so one person's provider choice never leaks into another's session
// on a shared instance.
type Handlers struct {
	config  *ConfigStore
	copilot *copilot.Store
}

// NewHandlers wires the settings store and the Copilot auth store together.
func NewHandlers(config *ConfigStore, copilotStore *copilot.Store) *Handlers {
	return &Handlers{config: config, copilot: copilotStore}
}

// Register attaches the package's routes to the given router.
//
//	GET  /api/ai/providers   (catalogue + current selection)
//	GET  /api/ai/status      (is the selected provider usable yet?)
//	GET  /api/ai/config      (saved settings, minus secrets)
//	POST /api/ai/config      (save provider / base URL / key / model)
//	POST /api/ai/forget      (clear the selected provider's credentials)
//	GET  /api/ai/models      (model list for the selected provider)
//	GET  /api/ai/tools       (tool schema + system prompt)
//	POST /api/ai/chat        (SSE proxy)
func (h *Handlers) Register(r gin.IRouter) {
	g := r.Group("/api/ai")
	g.GET("/providers", h.ListProviders)
	g.GET("/status", h.Status)
	g.GET("/config", h.GetConfig)
	g.POST("/config", h.SaveConfig)
	g.POST("/forget", h.Forget)
	g.GET("/models", h.Models)
	g.GET("/tools", h.Tools)
	g.POST("/chat", h.Chat)
}

// ListProviders returns the catalogue plus the caller's saved settings, so
// the settings dialog can render in one round trip.
func (h *Handlers) ListProviders(c *gin.Context) {
	id := copilot.Identity(c)
	c.JSON(http.StatusOK, gin.H{
		"providers": Providers(),
		"settings":  h.config.Public(id),
	})
}

// GetConfig returns the caller's saved settings. API keys are never included
// — only a hasKey flag per provider.
func (h *Handlers) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, h.config.Public(copilot.Identity(c)))
}

type configRequest struct {
	Provider *string `json:"provider"`
	// Target names the provider the remaining fields apply to. Omit to mean
	// "the provider selected by this same request".
	Target  string  `json:"target"`
	BaseURL *string `json:"baseUrl"`
	APIKey  *string `json:"apiKey"`
	Model   *string `json:"model"`
}

// SaveConfig persists a settings change. Fields left out of the JSON body are
// left untouched, which is how the dialog saves a base-URL or model edit
// without the browser having to hold (or resend) the API key.
func (h *Handlers) SaveConfig(c *gin.Context) {
	var req configRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := copilot.Identity(c)
	out, err := h.config.Apply(id, Update{
		Provider:       req.Provider,
		TargetProvider: req.Target,
		BaseURL:        req.BaseURL,
		APIKey:         req.APIKey,
		Model:          req.Model,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

type forgetRequest struct {
	// Provider to clear; defaults to the currently selected one.
	Provider string `json:"provider"`
}

// Forget clears the stored credentials for one provider. For Copilot it runs
// the OAuth logout; for key-based providers it deletes the saved settings.
func (h *Handlers) Forget(c *gin.Context) {
	var req forgetRequest
	_ = c.ShouldBindJSON(&req) // body optional

	id := copilot.Identity(c)
	target := strings.TrimSpace(req.Provider)
	if target == "" {
		target = h.config.Settings(id).Provider
	}
	if IsCopilot(target) {
		auth, _ := h.copilot.Get(id)
		if err := auth.Logout(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if err := h.config.Forget(id, target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider": target, "ready": false})
}

// Status reports whether the selected provider is ready to answer a chat,
// and if not, what the UI should ask the user for.
func (h *Handlers) Status(c *gin.Context) {
	id := copilot.Identity(c)
	prov, cfg, err := h.config.Resolve(id)

	ready := err == nil
	needs := ""
	if !ready {
		needs = "config"
	}
	if IsCopilot(prov.ID) {
		auth, _ := h.copilot.Get(id)
		ready = auth.LoggedIn()
		needs = ""
		if !ready {
			needs = "login"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"provider":      prov.ID,
		"providerLabel": prov.Label,
		"auth":          prov.Auth,
		"ready":         ready,
		"needs":         needs,
		"model":         cfg.Model,
		"baseUrl":       cfg.BaseURL,
		// logged_in keeps the shape of the older /api/copilot/status response
		// so existing scripts and the panel's connect prompt keep working.
		"logged_in": ready,
	})
}

// Tools exposes the tool definitions and system prompt to the frontend. These
// describe the 3270 session, not the provider, so they are identical for
// every backend.
func (h *Handlers) Tools(c *gin.Context) {
	id := copilot.Identity(c)
	_, cfg, _ := h.config.Resolve(id)
	c.JSON(http.StatusOK, gin.H{
		"tools":         copilot.DefaultTools(),
		"model":         cfg.Model,
		"system_prompt": copilot.SystemPrompt(),
	})
}

// Models returns the model IDs available from the selected provider, falling
// back to the curated catalogue whenever the live call can't be made (no key
// yet, endpoint down, provider without a /models route) so the dropdown is
// never empty.
func (h *Handlers) Models(c *gin.Context) {
	id := copilot.Identity(c)
	prov, cfg, resolveErr := h.config.Resolve(id)

	fallback := func() {
		c.JSON(http.StatusOK, gin.H{"models": prov.Models, "provider": prov.ID, "model": cfg.Model})
	}

	if IsCopilot(prov.ID) {
		auth, client := h.copilot.Get(id)
		if !auth.LoggedIn() {
			fallback()
			return
		}
		ids, err := client.ListModels(c.Request.Context())
		if err != nil || len(ids) == 0 {
			fallback()
			return
		}
		c.JSON(http.StatusOK, gin.H{"models": ids, "provider": prov.ID, "model": cfg.Model})
		return
	}
	if resolveErr != nil {
		fallback()
		return
	}
	ids, err := h.listModels(c.Request.Context(), prov, cfg)
	if err != nil || len(ids) == 0 {
		fallback()
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": ids, "provider": prov.ID, "model": cfg.Model})
}

func (h *Handlers) listModels(ctx context.Context, prov Provider, cfg ProviderConfig) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if prov.Wire == WireAnthropic {
		return newAnthropicClient(prov, cfg).ListModels(ctx)
	}
	return newOpenAIClient(prov, cfg).ListModels(ctx)
}

// Chat proxies a chat/completions request to the selected provider and
// streams an OpenAI-shaped SSE response back to the browser. Providers that
// don't speak that protocol are translated on the way through, so the panel's
// tool loop is provider-agnostic.
func (h *Handlers) Chat(c *gin.Context) {
	id := copilot.Identity(c)
	prov, cfg, resolveErr := h.config.Resolve(id)

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
	if msgs, ok := payload["messages"].([]any); !ok || len(msgs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages required"})
		return
	}

	// Resolve the streamer before writing any headers, so a misconfigured
	// provider produces a clean JSON error rather than an SSE error frame.
	var stream func(context.Context, io.Writer) error
	switch {
	case IsCopilot(prov.ID):
		auth, client := h.copilot.Get(id)
		if !auth.LoggedIn() {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "not signed in to GitHub Copilot"})
			return
		}
		stream = func(ctx context.Context, w io.Writer) error {
			return client.StreamChat(ctx, copilot.ChatRequest{Raw: payload}, w)
		}
	default:
		if resolveErr != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": resolveErr.Error()})
			return
		}
		if prov.Wire == WireAnthropic {
			client := newAnthropicClient(prov, cfg)
			stream = func(ctx context.Context, w io.Writer) error { return client.StreamChat(ctx, payload, w) }
		} else {
			client := newOpenAIClient(prov, cfg)
			stream = func(ctx context.Context, w io.Writer) error { return client.StreamChat(ctx, payload, w) }
		}
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	pw := &flushingWriter{w: c.Writer, f: flusher}

	ctx, cancel := context.WithTimeout(c.Request.Context(), chatStreamTimeout)
	defer cancel()

	if err := stream(ctx, pw); err != nil {
		// Nothing written yet means we can still emit a clean JSON error;
		// otherwise the frontend understands an `event: error` SSE frame.
		if pw.bytes == 0 {
			c.Writer.Header().Set("Content-Type", "application/json")
			status := http.StatusBadGateway
			var apiErr *APIError
			var copilotErr *copilot.APIError
			if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 600 {
				status = apiErr.Status
			} else if errors.As(err, &copilotErr) && copilotErr.Status >= 400 && copilotErr.Status < 600 {
				status = copilotErr.Status
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
