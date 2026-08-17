// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ProviderConfig is one provider's saved settings for one browser identity.
type ProviderConfig struct {
	// BaseURL overrides the provider's DefaultBaseURL. Empty means "use the
	// default".
	BaseURL string `json:"baseUrl,omitempty"`
	// APIKey is the credential. It is written to disk 0600 and is never
	// echoed back to the browser — see PublicSettings.
	APIKey string `json:"apiKey,omitempty"`
	// Model is the last model the user picked for this provider.
	Model string `json:"model,omitempty"`
}

// Settings is the whole per-identity document.
type Settings struct {
	// Provider is the selected provider ID.
	Provider string `json:"provider,omitempty"`
	// Providers holds per-provider settings, keyed by provider ID. Keeping
	// one entry per provider (rather than a single active config) means
	// switching back to a provider you used earlier does not make you
	// re-enter its key.
	Providers map[string]ProviderConfig `json:"providers,omitempty"`
}

// PublicProviderConfig is the browser-visible projection of a
// ProviderConfig: same shape minus the secret, plus a flag saying whether a
// secret is on file.
type PublicProviderConfig struct {
	BaseURL string `json:"baseUrl,omitempty"`
	Model   string `json:"model,omitempty"`
	HasKey  bool   `json:"hasKey"`
}

// PublicSettings is what GET /api/ai/config returns.
type PublicSettings struct {
	Provider  string                          `json:"provider"`
	Providers map[string]PublicProviderConfig `json:"providers"`
}

// identitySettings is one browser identity's settings plus its load state.
type identitySettings struct {
	path string

	mu       sync.Mutex
	settings Settings
	loaded   bool

	// lastUsed is read/written only while holding the owning ConfigStore's
	// mu, mirroring how internal/copilot ages out its own per-identity cache.
	lastUsed time.Time
}

// ConfigStore holds per-identity AI settings, one JSON file per identity
// under baseDir.
type ConfigStore struct {
	baseDir string

	mu      sync.Mutex
	entries map[string]*identitySettings
}

// NewConfigStore creates a store whose per-identity files live under baseDir.
func NewConfigStore(baseDir string) *ConfigStore {
	return &ConfigStore{baseDir: baseDir, entries: make(map[string]*identitySettings)}
}

// get returns the settings for id, creating an entry on first sight. The
// caller is responsible for having validated id as a safe path component
// (handlers.go only ever passes the copilot identity cookie, which is
// checked against a 32-hex-character pattern before use).
func (st *ConfigStore) get(id string) *identitySettings {
	st.mu.Lock()
	defer st.mu.Unlock()
	e, ok := st.entries[id]
	if !ok {
		e = &identitySettings{path: filepath.Join(st.baseDir, "ai-config-"+id+".json")}
		st.entries[id] = e
	}
	e.lastUsed = time.Now()
	return e
}

// EvictIdle drops in-memory entries untouched for at least maxIdle. The
// on-disk file is left alone and reloaded lazily if the browser returns.
func (st *ConfigStore) EvictIdle(maxIdle time.Duration) {
	cutoff := time.Now().Add(-maxIdle)
	st.mu.Lock()
	defer st.mu.Unlock()
	for id, e := range st.entries {
		if e.lastUsed.Before(cutoff) {
			delete(st.entries, id)
		}
	}
}

// Settings returns a copy of the identity's saved settings, defaulted.
func (st *ConfigStore) Settings(id string) Settings {
	e := st.get(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureLoadedLocked()
	return e.snapshotLocked()
}

// Public returns the browser-safe projection of the identity's settings.
func (st *ConfigStore) Public(id string) PublicSettings {
	s := st.Settings(id)
	out := PublicSettings{Provider: s.Provider, Providers: map[string]PublicProviderConfig{}}
	for pid, cfg := range s.Providers {
		out.Providers[pid] = PublicProviderConfig{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			HasKey:  strings.TrimSpace(cfg.APIKey) != "",
		}
	}
	return out
}

// Update is the single mutation entry point. Nil-valued fields on the patch
// leave the stored value alone, which is what lets the settings form save a
// model change without resending (or clearing) the API key.
type Update struct {
	// Provider, when non-nil, selects the active provider.
	Provider *string
	// TargetProvider names the provider the remaining fields apply to. Empty
	// means "whichever provider is active after this update".
	TargetProvider string
	BaseURL        *string
	APIKey         *string
	Model          *string
}

// Apply validates and persists an update, returning the new public settings.
func (st *ConfigStore) Apply(id string, up Update) (PublicSettings, error) {
	e := st.get(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureLoadedLocked()

	if e.settings.Providers == nil {
		e.settings.Providers = map[string]ProviderConfig{}
	}

	if up.Provider != nil {
		pid := strings.TrimSpace(*up.Provider)
		if _, ok := Lookup(pid); !ok {
			return PublicSettings{}, errors.New("unknown provider " + pid)
		}
		e.settings.Provider = pid
	}

	target := strings.TrimSpace(up.TargetProvider)
	if target == "" {
		target = e.settings.Provider
	}
	if target == "" {
		target = DefaultProviderID
	}
	prov, ok := Lookup(target)
	if !ok {
		return PublicSettings{}, errors.New("unknown provider " + target)
	}

	cfg := e.settings.Providers[target]
	movedOrigin := false
	if up.BaseURL != nil {
		clean, err := ValidateBaseURL(*up.BaseURL)
		if err != nil {
			return PublicSettings{}, err
		}
		if clean != "" && !prov.BaseURLEditable {
			return PublicSettings{}, errors.New(prov.Label + " does not accept a custom base URL")
		}
		movedOrigin = !sameOrigin(effectiveBaseURL(cfg.BaseURL, prov), effectiveBaseURL(clean, prov))
		cfg.BaseURL = clean
	}
	if up.APIKey != nil {
		// An explicit empty string clears the stored key; omitting the field
		// entirely (nil) keeps it.
		cfg.APIKey = strings.TrimSpace(*up.APIKey)
	} else if movedOrigin {
		// The key was issued for the endpoint it was entered against, and this
		// request has just named a different one. Keeping it would mean the
		// next chat sends that credential — in an Authorization header, to an
		// origin of the caller's choosing — without anybody having said it
		// should go there. On a shared browser that is the whole exfiltration
		// path: inherit a key, retarget the endpoint, read it off your own
		// server. Somewhere else it is an ordinary mistake with the same
		// consequence.
		//
		// So the credential does not travel with the endpoint. Point the
		// provider somewhere new and the key has to be entered again, which is
		// one deliberate act rather than a silent one. Sending it along in the
		// same request still works: that is the caller saying the key belongs
		// to the new origin, which is exactly the statement being asked for.
		cfg.APIKey = ""
	}
	if up.Model != nil {
		cfg.Model = strings.TrimSpace(*up.Model)
	}
	e.settings.Providers[target] = cfg

	if err := e.saveLocked(); err != nil {
		return PublicSettings{}, err
	}
	s := e.snapshotLocked()
	out := PublicSettings{Provider: s.Provider, Providers: map[string]PublicProviderConfig{}}
	for pid, c := range s.Providers {
		out.Providers[pid] = PublicProviderConfig{
			BaseURL: c.BaseURL,
			Model:   c.Model,
			HasKey:  strings.TrimSpace(c.APIKey) != "",
		}
	}
	return out, nil
}

// Forget clears everything stored for one provider (used by "sign out" when
// the active provider is key-based).
func (st *ConfigStore) Forget(id, providerID string) error {
	e := st.get(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureLoadedLocked()
	delete(e.settings.Providers, providerID)
	return e.saveLocked()
}

// Resolve returns the effective endpoint settings for the identity's active
// provider: the provider descriptor, the base URL to call, the credential,
// and the model to default to.
func (st *ConfigStore) Resolve(id string) (Provider, ProviderConfig, error) {
	s := st.Settings(id)
	prov, ok := Lookup(s.Provider)
	if !ok {
		prov, _ = Lookup(DefaultProviderID)
	}
	cfg := s.Providers[prov.ID]
	if cfg.BaseURL == "" {
		cfg.BaseURL = prov.DefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = prov.DefaultModel
	}
	if prov.BaseURLRequired && cfg.BaseURL == "" {
		return prov, cfg, errors.New(prov.Label + " needs a base URL — set one in AI provider settings")
	}
	if prov.Auth == AuthAPIKey && strings.TrimSpace(cfg.APIKey) == "" {
		return prov, cfg, errors.New(prov.Label + " needs an API key — set one in AI provider settings")
	}
	return prov, cfg, nil
}

func (e *identitySettings) snapshotLocked() Settings {
	out := Settings{Provider: e.settings.Provider, Providers: map[string]ProviderConfig{}}
	if out.Provider == "" {
		out.Provider = DefaultProviderID
	}
	for k, v := range e.settings.Providers {
		out.Providers[k] = v
	}
	return out
}

func (e *identitySettings) ensureLoadedLocked() {
	if e.loaded {
		return
	}
	e.loaded = true
	data, err := os.ReadFile(e.path)
	if err != nil {
		return
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return
	}
	e.settings = s
}

func (e *identitySettings) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(e.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(e.path, data, 0o600)
}
