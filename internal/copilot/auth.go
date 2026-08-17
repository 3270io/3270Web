// SPDX-License-Identifier: AGPL-3.0-or-later

// Package copilot implements GitHub Copilot integration for the 3270 web UI.
//
// The wire protocol mirrors the GitHub Copilot Chat plugin: an OAuth device
// flow against github.com produces a long-lived OAuth access token, which is
// exchanged at /copilot_internal/v2/token for a short-lived Copilot API
// token. Chat completions are then served from the API endpoint reported in
// that response (api.githubcopilot.com by default).
package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jnnngs/3270Web/internal/egress"
)

// Default identification headers — these match the VS Code Copilot Chat
// extension and are required by the Copilot API. They are not secrets.
const (
	defaultEditorVersion       = "vscode/1.107.0"
	defaultEditorPluginVersion = "copilot-chat/0.35.0"
	defaultUserAgent           = "GitHubCopilotChat/0.35.0"
	defaultIntegrationID       = "vscode-chat"
	defaultCopilotAPIHost      = "https://api.githubcopilot.com"

	// GitHub OAuth client ID used by Copilot Chat. The client ID is public
	// (it appears in every Copilot Chat editor extension) so embedding it
	// here is safe.
	defaultGitHubClientID = "Iv1.b507a08c87ecfe98"

	tokenRefreshBuffer = 5 * time.Minute
)

// cachedAuth is what we persist on disk between sessions.
type cachedAuth struct {
	// OAuth access token from the device flow ("refresh" in REA's terms).
	OAuth string `json:"oauth"`
	// Optional GitHub Enterprise hostname (e.g. "ghe.example.com"). When set
	// the token endpoint becomes https://api.<host>/copilot_internal/v2/token.
	EnterpriseURL string `json:"enterpriseUrl,omitempty"`

	// Short-lived Copilot API token + expiry (ms since epoch).
	CopilotToken   string `json:"copilotToken,omitempty"`
	CopilotExpires int64  `json:"copilotExpires,omitempty"`
	// API endpoint reported by the token response, e.g. "https://api.githubcopilot.com".
	APIEndpoint string `json:"apiEndpoint,omitempty"`
}

// AuthManager persists OAuth + Copilot tokens and refreshes them on demand.
//
// Operations are goroutine-safe.
type AuthManager struct {
	path string

	mu        sync.Mutex
	cache     cachedAuth
	loaded    bool
	clientID  string
	userAgent string

	// Overridable for tests.
	httpClient *http.Client
	githubHost string // e.g. "https://github.com"
	apiHost    string // e.g. "https://api.github.com"
}

// NewAuthManager creates a manager backed by a JSON file at path.
func NewAuthManager(path string) *AuthManager {
	return &AuthManager{
		path:       path,
		clientID:   defaultGitHubClientID,
		userAgent:  defaultUserAgent,
		httpClient: egress.Default().HTTPClient(30 * time.Second),
		githubHost: "https://github.com",
		apiHost:    "https://api.github.com",
	}
}

// DefaultAuthPath returns the recommended location for the auth cache file.
// Honors COPILOT_AUTH_PATH if set, then falls back to
// $XDG_CONFIG_HOME/3270Web/copilot-auth.json (or the OS user config dir).
func DefaultAuthPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("COPILOT_AUTH_PATH")); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "3270Web", "copilot-auth.json"), nil
}

// identityAuth bundles one browser identity's AuthManager, Client, and any
// in-flight device-login attempt (previously fields directly on Handlers,
// which made them shared by every browser hitting the server).
type identityAuth struct {
	auth   *AuthManager
	client *Client

	// lastUsed is read/written only while holding the owning Store's mu, not
	// this struct's own mu (which guards only device below).
	lastUsed time.Time

	mu     sync.Mutex
	device deviceState
}

// Store scopes Copilot auth state to one browser identity per entry, keyed
// by an opaque ID (see the identity cookie in handlers.go). Without this, a
// single AuthManager was shared by every browser that hit the server: one
// person's login — or logout — silently applied to everyone else's Copilot
// session too.
type Store struct {
	baseDir string

	mu      sync.Mutex
	entries map[string]*identityAuth
}

// NewAuthStore creates a Store whose per-identity token caches live under
// baseDir, one JSON file per identity.
func NewAuthStore(baseDir string) *Store {
	return &Store{baseDir: baseDir, entries: make(map[string]*identityAuth)}
}

// get returns the identityAuth for id, creating (and persisting to its own
// file) one if this is the first time this identity has been seen.
func (st *Store) get(id string) *identityAuth {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.entries[id]
	if !ok {
		auth := NewAuthManager(filepath.Join(st.baseDir, "copilot-auth-"+id+".json"))
		e = &identityAuth{auth: auth, client: NewClient(auth)}
		st.entries[id] = e
	}
	e.lastUsed = time.Now()
	return e
}

// Get returns the AuthManager and Client belonging to one browser identity,
// creating them on first sight. Used by internal/aiprovider, which owns the
// provider-selection layer above this package and delegates back to it when
// the selected provider is GitHub Copilot.
func (st *Store) Get(id string) (*AuthManager, *Client) {
	e := st.get(id)
	return e.auth, e.client
}

// EvictIdle drops cached identities untouched for at least maxIdle, bounding
// the store's memory on a long-running server that many different browsers
// connect to over time. Eviction only drops the in-memory cache — the
// identity's on-disk token file is untouched and reloaded lazily the next
// time that browser is seen again.
func (st *Store) EvictIdle(maxIdle time.Duration) {
	cutoff := time.Now().Add(-maxIdle)
	st.mu.Lock()
	defer st.mu.Unlock()
	for id, e := range st.entries {
		if e.lastUsed.Before(cutoff) {
			delete(st.entries, id)
		}
	}
}

// LoggedIn reports whether the manager holds an OAuth token.
func (m *AuthManager) LoggedIn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLoadedLocked()
	return strings.TrimSpace(m.cache.OAuth) != ""
}

// EnterpriseURL returns the configured GHE hostname (empty if dotcom).
func (m *AuthManager) EnterpriseURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLoadedLocked()
	return m.cache.EnterpriseURL
}

// enterpriseHostPattern matches a bare hostname with an optional trailing
// :port — no scheme, path, userinfo, or whitespace. StartDeviceLogin,
// PollDeviceLogin, and refreshCopilotTokenLocked build request URLs by
// concatenating this value directly (e.g. "https://"+host), so anything
// other than a plain hostname here would let the value redirect those
// requests (and the bearer token they carry) to an attacker-controlled
// origin.
var enterpriseHostPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*(:[0-9]{1,5})?$`)

func isValidEnterpriseHost(host string) bool {
	if host == "" || len(host) > 253 || !enterpriseHostPattern.MatchString(host) {
		return false
	}
	hostOnly := host
	if i := strings.LastIndex(host, ":"); i != -1 {
		hostOnly = host[:i]
	}
	// No legitimate GHE deployment lives at a link-local address; that
	// range also covers cloud metadata services (e.g. 169.254.169.254).
	//
	// This catches the literal only, which is as far as a syntax check can
	// see: "ghe.example.com" is a valid hostname whoever owns it can point
	// wherever they like, and this function has no idea where that is. The
	// address is checked where it is known instead — internal/egress resolves
	// the name inside the dialer and refuses the connection there, on the
	// first request and on every redirect after it. Keeping the literal check
	// as well means an obviously wrong value is refused when it is typed
	// rather than when it is first used.
	if ip := net.ParseIP(hostOnly); ip != nil && (ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return false
	}
	return true
}

// SetEnterpriseURL persists a GitHub Enterprise hostname (without scheme).
// An empty value clears it.
func (m *AuthManager) SetEnterpriseURL(host string) error {
	host = strings.TrimSpace(host)
	if host != "" && !isValidEnterpriseHost(host) {
		return fmt.Errorf("invalid enterprise host %q: expected a bare hostname, e.g. ghe.example.com", host)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLoadedLocked()
	m.cache.EnterpriseURL = host
	// Invalidate the Copilot token since it depends on the host.
	m.cache.CopilotToken = ""
	m.cache.CopilotExpires = 0
	m.cache.APIEndpoint = ""
	return m.saveLocked()
}

// Logout clears the cached OAuth token and Copilot token.
func (m *AuthManager) Logout() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache = cachedAuth{}
	m.loaded = true
	if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// DeviceCode is the response from POST https://github.com/login/device/code.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// StartDeviceLogin begins the GitHub OAuth device flow and returns the
// user_code + verification URI that the browser should display.
func (m *AuthManager) StartDeviceLogin(ctx context.Context) (*DeviceCode, error) {
	base := m.githubHost
	if ghe := m.EnterpriseURL(); ghe != "" {
		base = "https://" + ghe
	}
	form := url.Values{
		"client_id": {m.clientID},
		"scope":     {"read:user"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/login/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", m.userAgent)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed: status %d: %s", resp.StatusCode, truncate(readErrorBody(resp), 200))
	}
	body, err := readCapped(resp, maxControlResponseBytes, "device code response")
	if err != nil {
		return nil, err
	}
	var dc DeviceCode
	if err := json.Unmarshal(body, &dc); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	return &dc, nil
}

// PollDeviceLogin exchanges a device_code for an OAuth access token.
// Returns ("authorization_pending", nil) when the user has not yet finished.
type PollResult struct {
	Status string // "success", "pending", "slow_down", "expired", "denied", "error"
	Error  string // human-readable error description if Status != "success"
}

// PollDeviceLogin polls /login/oauth/access_token. On success the OAuth
// token is persisted on the manager and used for subsequent Copilot calls.
func (m *AuthManager) PollDeviceLogin(ctx context.Context, deviceCode string) (PollResult, error) {
	base := m.githubHost
	if ghe := m.EnterpriseURL(); ghe != "" {
		base = "https://" + ghe
	}
	form := url.Values{
		"client_id":   {m.clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return PollResult{Status: "error", Error: err.Error()}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", m.userAgent)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return PollResult{Status: "error", Error: err.Error()}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("device token poll failed: status %d: %s", resp.StatusCode, truncate(readErrorBody(resp), 200))
		return PollResult{Status: "error", Error: errMsg}, errors.New(errMsg)
	}
	body, err := readCapped(resp, maxControlResponseBytes, "device token response")
	if err != nil {
		return PollResult{Status: "error", Error: err.Error()}, err
	}
	var payload struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return PollResult{Status: "error", Error: "decode response: " + err.Error()}, err
	}

	switch payload.Error {
	case "":
		// Success
	case "authorization_pending":
		return PollResult{Status: "pending"}, nil
	case "slow_down":
		return PollResult{Status: "slow_down"}, nil
	case "expired_token":
		return PollResult{Status: "expired", Error: payload.ErrorDescription}, nil
	case "access_denied":
		return PollResult{Status: "denied", Error: payload.ErrorDescription}, nil
	default:
		return PollResult{Status: "error", Error: payload.ErrorDescription}, fmt.Errorf("oauth error: %s", payload.Error)
	}

	if strings.TrimSpace(payload.AccessToken) == "" {
		return PollResult{Status: "error", Error: "empty access_token"}, errors.New("empty access_token")
	}

	m.mu.Lock()
	m.ensureLoadedLocked()
	m.cache.OAuth = payload.AccessToken
	// Invalidate any previous Copilot token from a different account.
	m.cache.CopilotToken = ""
	m.cache.CopilotExpires = 0
	m.cache.APIEndpoint = ""
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return PollResult{Status: "error", Error: err.Error()}, err
	}
	m.mu.Unlock()
	return PollResult{Status: "success"}, nil
}

// CopilotToken returns a valid Copilot API token and its API endpoint,
// refreshing if necessary. Returns an error if not logged in.
func (m *AuthManager) CopilotToken(ctx context.Context) (token, apiEndpoint string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureLoadedLocked()

	if strings.TrimSpace(m.cache.OAuth) == "" {
		return "", "", ErrNotLoggedIn
	}

	if m.cache.CopilotToken != "" && m.cache.APIEndpoint != "" &&
		time.Now().UnixMilli() < m.cache.CopilotExpires {
		return m.cache.CopilotToken, m.cache.APIEndpoint, nil
	}

	if err := m.refreshCopilotTokenLocked(ctx); err != nil {
		return "", "", err
	}
	return m.cache.CopilotToken, m.cache.APIEndpoint, nil
}

// ErrNotLoggedIn is returned when no OAuth token has been cached.
var ErrNotLoggedIn = errors.New("copilot: not logged in")

func (m *AuthManager) refreshCopilotTokenLocked(ctx context.Context) error {
	host := m.apiHost
	if ghe := m.cache.EnterpriseURL; ghe != "" {
		host = "https://api." + ghe
	}
	req, err := http.NewRequestWithContext(ctx, "GET", host+"/copilot_internal/v2/token", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.cache.OAuth)
	req.Header.Set("User-Agent", m.userAgent)
	req.Header.Set("Editor-Version", defaultEditorVersion)
	req.Header.Set("Editor-Plugin-Version", defaultEditorPluginVersion)
	req.Header.Set("Copilot-Integration-Id", defaultIntegrationID)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("copilot token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("copilot token: status %d: %s", resp.StatusCode, truncate(readErrorBody(resp), 200))
	}
	body, err := readCapped(resp, maxControlResponseBytes, "copilot token response")
	if err != nil {
		return err
	}
	var payload struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"` // seconds since epoch
		Endpoints struct {
			API string `json:"api"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("decode copilot token: %w", err)
	}
	if strings.TrimSpace(payload.Token) == "" {
		return errors.New("copilot token: empty token")
	}
	endpoint := strings.TrimSpace(payload.Endpoints.API)
	if endpoint == "" {
		endpoint = defaultCopilotAPIHost
	}
	m.cache.CopilotToken = payload.Token
	m.cache.APIEndpoint = endpoint
	// Guard against a missing/zero expires_at. Without this, expiresMS would be
	// negative, so the freshly-fetched token would be treated as already
	// expired and re-fetched on every call (and the bad value persisted).
	now := time.Now().UnixMilli()
	var expiresMS int64
	if payload.ExpiresAt > 0 {
		expiresMS = payload.ExpiresAt*1000 - int64(tokenRefreshBuffer/time.Millisecond)
	}
	if expiresMS <= now {
		// Fall back to a conservative TTL so the token is usable for at least
		// one refresh window rather than being considered instantly stale.
		expiresMS = now + int64(tokenRefreshBuffer/time.Millisecond)
	}
	m.cache.CopilotExpires = expiresMS
	return m.saveLocked()
}

func (m *AuthManager) ensureLoadedLocked() {
	if m.loaded {
		return
	}
	m.loaded = true
	data, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var c cachedAuth
	if err := json.Unmarshal(data, &c); err != nil {
		return
	}
	m.cache = c
}

func (m *AuthManager) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, data, 0o600)
}

// truncate shortens s to at most n runes (not bytes), appending an ellipsis.
// Counting by runes avoids slicing through the middle of a multibyte UTF-8
// sequence, which would produce invalid text in error messages.
func truncate(s string, n int) string {
	if n < 0 {
		n = 0
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
