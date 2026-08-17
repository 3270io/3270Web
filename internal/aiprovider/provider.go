// SPDX-License-Identifier: AGPL-3.0-or-later

// Package aiprovider lets the AI Chat panel talk to something other than
// GitHub Copilot.
//
// Everything the browser sends and receives is OpenAI /chat/completions
// shaped, because that is what web/static/copilot-panel.js was written
// against. Providers that already speak that protocol (OpenAI, Ollama,
// Google AI, any OpenAI-compatible gateway) are proxied through
// openaiClient unchanged; Anthropic speaks its own Messages protocol and is
// translated in both directions by anthropicClient. GitHub Copilot keeps its
// existing OAuth device flow and is delegated back to internal/copilot.
package aiprovider

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/jnnngs/3270Web/internal/copilot"
)

// Wire identifies which request/response protocol a provider speaks.
type Wire string

const (
	// WireOpenAI is the OpenAI /chat/completions protocol (SSE chunks with
	// choices[].delta). Proxied verbatim.
	WireOpenAI Wire = "openai"
	// WireAnthropic is the Anthropic /v1/messages protocol. Translated to
	// and from WireOpenAI so the frontend never sees the difference.
	WireAnthropic Wire = "anthropic"
)

// Auth identifies how a provider is authenticated.
type Auth string

const (
	// AuthOAuthDevice is the GitHub OAuth device flow (Copilot only).
	AuthOAuthDevice Auth = "oauth-device"
	// AuthAPIKey is a bearer/API key the user pastes into the settings panel.
	AuthAPIKey Auth = "api-key"
	// AuthOptionalKey is a key that may be omitted — local Ollama and most
	// self-hosted OpenAI-compatible servers accept unauthenticated requests.
	AuthOptionalKey Auth = "optional-key"
)

// Provider describes one selectable AI backend.
type Provider struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Blurb is one line of UI help text.
	Blurb string `json:"blurb"`
	Wire  Wire   `json:"wire"`
	Auth  Auth   `json:"auth"`

	// DefaultBaseURL is the API root (without a trailing slash) used when the
	// user does not override it. For WireOpenAI providers this is the
	// directory containing /chat/completions and /models.
	DefaultBaseURL string `json:"defaultBaseUrl"`
	// BaseURLEditable reports whether the settings UI offers a base-URL box.
	BaseURLEditable bool `json:"baseUrlEditable"`
	// BaseURLRequired reports whether the provider is unusable until the user
	// supplies one (true only for the generic OpenAI-compatible entry).
	BaseURLRequired bool `json:"baseUrlRequired"`

	// DefaultModel is preselected in the model dropdown.
	DefaultModel string `json:"defaultModel"`
	// Models is the fallback catalogue used before (or instead of) a live
	// /models call. Live results win when the upstream answers.
	Models []string `json:"models"`

	// KeyURL points at the page where the user creates an API key.
	KeyURL string `json:"keyUrl,omitempty"`
	// KeyLabel names the credential in the settings UI ("API key",
	// "Personal access token", ...).
	KeyLabel string `json:"keyLabel,omitempty"`
}

// DefaultProviderID is what a browser with no saved settings gets. Copilot
// stays the default so existing installs are unaffected by this feature.
const DefaultProviderID = "github-copilot"

// providers is the ordered catalogue offered in the settings UI.
//
// Model lists are deliberately short: they are a fallback for when the
// upstream /models endpoint is unreachable or the user has not entered a key
// yet, not an attempt to mirror every provider's full catalogue.
var providers = []Provider{
	{
		ID:              "github-copilot",
		Label:           "GitHub Copilot",
		Blurb:           "Sign in with your GitHub account. No API key to manage.",
		Wire:            WireOpenAI,
		Auth:            AuthOAuthDevice,
		DefaultModel:    copilot.DefaultModel,
		Models:          copilot.SupportedModels,
		BaseURLEditable: false,
	},
	{
		ID:              "anthropic",
		Label:           "Claude (Anthropic)",
		Blurb:           "Anthropic's Messages API. Needs an API key from the Claude Console.",
		Wire:            WireAnthropic,
		Auth:            AuthAPIKey,
		DefaultBaseURL:  "https://api.anthropic.com",
		BaseURLEditable: true,
		DefaultModel:    "claude-opus-5",
		Models: []string{
			"claude-opus-5",
			"claude-sonnet-5",
			"claude-opus-4-8",
			"claude-haiku-4-5",
		},
		KeyURL:   "https://platform.claude.com/settings/keys",
		KeyLabel: "API key",
	},
	{
		ID:              "openai",
		Label:           "OpenAI",
		Blurb:           "The OpenAI platform API.",
		Wire:            WireOpenAI,
		Auth:            AuthAPIKey,
		DefaultBaseURL:  "https://api.openai.com/v1",
		BaseURLEditable: true,
		DefaultModel:    "gpt-5.1",
		Models: []string{
			"gpt-5.1",
			"gpt-5",
			"gpt-5-mini",
			"gpt-4.1",
			"gpt-4o",
		},
		KeyURL:   "https://platform.openai.com/api-keys",
		KeyLabel: "API key",
	},
	{
		ID:              "google",
		Label:           "Google AI (Gemini)",
		Blurb:           "Gemini through Google's OpenAI-compatible endpoint.",
		Wire:            WireOpenAI,
		Auth:            AuthAPIKey,
		DefaultBaseURL:  "https://generativelanguage.googleapis.com/v1beta/openai",
		BaseURLEditable: true,
		DefaultModel:    "gemini-2.5-pro",
		Models: []string{
			"gemini-2.5-pro",
			"gemini-2.5-flash",
			"gemini-2.0-flash",
		},
		KeyURL:   "https://aistudio.google.com/apikey",
		KeyLabel: "API key",
	},
	{
		ID:              "ollama",
		Label:           "Ollama (local)",
		Blurb:           "A model running on this machine or your network. Usually needs no key.",
		Wire:            WireOpenAI,
		Auth:            AuthOptionalKey,
		DefaultBaseURL:  "http://localhost:11434/v1",
		BaseURLEditable: true,
		DefaultModel:    "qwen3",
		Models: []string{
			"qwen3",
			"llama3.1",
			"mistral",
		},
		KeyLabel: "API key (optional)",
	},
	{
		ID:              "ollama-cloud",
		Label:           "Ollama Cloud",
		Blurb:           "Ollama's hosted models. Needs an ollama.com API key.",
		Wire:            WireOpenAI,
		Auth:            AuthAPIKey,
		DefaultBaseURL:  "https://ollama.com/v1",
		BaseURLEditable: true,
		DefaultModel:    "gpt-oss:120b",
		Models: []string{
			"gpt-oss:120b",
			"qwen3-coder:480b",
			"deepseek-v3.1:671b",
		},
		KeyURL:   "https://ollama.com/settings/keys",
		KeyLabel: "API key",
	},
	{
		ID:              "openai-compatible",
		Label:           "OpenAI-compatible endpoint",
		Blurb:           "Anything that serves /chat/completions — vLLM, LM Studio, OpenRouter, Groq, a corporate gateway.",
		Wire:            WireOpenAI,
		Auth:            AuthOptionalKey,
		BaseURLEditable: true,
		BaseURLRequired: true,
		KeyLabel:        "API key (optional)",
	},
}

// Providers returns the catalogue in display order.
func Providers() []Provider {
	out := make([]Provider, len(providers))
	copy(out, providers)
	return out
}

// Lookup returns the provider with the given ID.
func Lookup(id string) (Provider, bool) {
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// IsCopilot reports whether id names the GitHub Copilot provider, which is
// authenticated and streamed by internal/copilot rather than by this package.
func IsCopilot(id string) bool { return id == DefaultProviderID }

// ValidateBaseURL rejects anything that isn't a plain http(s) origin we are
// willing to forward an API key to.
//
// The base URL is user-supplied and this server may be shared, so the value
// is treated the same way internal/copilot treats its GitHub Enterprise
// hostname: no credentials in the URL, no query or fragment smuggling, and
// no link-local addresses (which is where cloud instance-metadata services
// live). Private and loopback addresses are deliberately allowed — "Ollama
// on localhost" and "a gateway on the corporate LAN" are the point of the
// feature.
func ValidateBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("base URL must start with http:// or https://")
	}
	if u.Host == "" {
		return "", fmt.Errorf("base URL must include a host, e.g. http://localhost:11434/v1")
	}
	if u.User != nil {
		return "", fmt.Errorf("base URL must not contain a username or password")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("base URL must not contain a query string or fragment")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return "", fmt.Errorf("base URL must not point at a link-local address")
		}
	}
	// Normalize so joining "/chat/completions" never produces a double slash.
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// effectiveBaseURL is the URL a config will actually call: the override where
// there is one, the provider's own default where there is not. Comparing the
// stored strings instead would read "cleared back to the default" as a move,
// and moving from the default to a URL spelling the same thing as a stay.
func effectiveBaseURL(baseURL string, prov Provider) string {
	if strings.TrimSpace(baseURL) == "" {
		return prov.DefaultBaseURL
	}
	return baseURL
}

// sameOrigin reports whether two base URLs address the same scheme, host and
// port — the three things that decide where a request is delivered, and so
// which of them a credential is being handed to. The path is deliberately not
// part of it: moving from /v1 to /v1beta on one host is a different route to
// the same server, not a different server.
func sameOrigin(a, b string) bool {
	ua, erra := url.Parse(strings.TrimSpace(a))
	ub, errb := url.Parse(strings.TrimSpace(b))
	if erra != nil || errb != nil {
		// Unparseable on either side: treat it as a move. The consequence is
		// re-entering a key, which is the safe direction to be wrong in.
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host)
}
