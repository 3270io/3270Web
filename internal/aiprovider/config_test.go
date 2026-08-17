// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func strptr(s string) *string { return &s }

func TestApplyStoresAndHidesTheAPIKey(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	const id = "abc"

	if _, err := st.Apply(id, Update{Provider: strptr("anthropic"), APIKey: strptr("sk-ant-secret")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pub := st.Public(id)
	if pub.Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", pub.Provider)
	}
	cfg := pub.Providers["anthropic"]
	if !cfg.HasKey {
		t.Error("HasKey = false, want true")
	}

	// The public projection must have no field capable of carrying the
	// secret at all — a browser must never be able to read a stored key back.
	_, resolved, err := st.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.APIKey != "sk-ant-secret" {
		t.Errorf("Resolve lost the API key: got %q", resolved.APIKey)
	}
}

func TestApplyLeavesOmittedFieldsAlone(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	const id = "abc"

	if _, err := st.Apply(id, Update{Provider: strptr("openai"), APIKey: strptr("sk-key")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Saving just a model change (the dialog's common case) must not wipe the
	// key the browser never had a copy of.
	if _, err := st.Apply(id, Update{Model: strptr("gpt-4o")}); err != nil {
		t.Fatalf("Apply model: %v", err)
	}

	_, cfg, err := st.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.APIKey != "sk-key" {
		t.Errorf("APIKey = %q, want it preserved", cfg.APIKey)
	}
	if cfg.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", cfg.Model)
	}
}

func TestApplyEmptyKeyClearsIt(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	const id = "abc"
	if _, err := st.Apply(id, Update{Provider: strptr("openai"), APIKey: strptr("sk-key")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := st.Apply(id, Update{APIKey: strptr("")}); err != nil {
		t.Fatalf("Apply clear: %v", err)
	}
	if st.Public(id).Providers["openai"].HasKey {
		t.Error("HasKey = true after an explicit empty key")
	}
}

func TestApplyKeepsPerProviderSettingsSeparate(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	const id = "abc"

	if _, err := st.Apply(id, Update{Provider: strptr("openai"), APIKey: strptr("sk-openai")}); err != nil {
		t.Fatalf("Apply openai: %v", err)
	}
	if _, err := st.Apply(id, Update{Provider: strptr("anthropic"), APIKey: strptr("sk-anthropic")}); err != nil {
		t.Fatalf("Apply anthropic: %v", err)
	}
	// Switching back must not require re-entering the first provider's key.
	if _, err := st.Apply(id, Update{Provider: strptr("openai")}); err != nil {
		t.Fatalf("Apply switch back: %v", err)
	}
	_, cfg, err := st.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.APIKey != "sk-openai" {
		t.Errorf("APIKey = %q, want sk-openai", cfg.APIKey)
	}
}

func TestApplyRejectsBadInput(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	const id = "abc"

	if _, err := st.Apply(id, Update{Provider: strptr("not-a-provider")}); err == nil {
		t.Error("Apply accepted an unknown provider")
	}
	if _, err := st.Apply(id, Update{Provider: strptr("openai"), BaseURL: strptr("http://169.254.169.254")}); err == nil {
		t.Error("Apply accepted a link-local base URL")
	}
	// Copilot has no base URL to override.
	if _, err := st.Apply(id, Update{Provider: strptr("github-copilot"), BaseURL: strptr("https://example.com")}); err == nil {
		t.Error("Apply accepted a base URL for a provider that has none")
	}
}

func TestResolveReportsMissingCredentials(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	const id = "abc"

	if _, err := st.Apply(id, Update{Provider: strptr("anthropic")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, _, err := st.Resolve(id); err == nil {
		t.Error("Resolve reported a key-based provider as ready with no key")
	}

	if _, err := st.Apply(id, Update{Provider: strptr("openai-compatible")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, _, err := st.Resolve(id); err == nil {
		t.Error("Resolve reported a base-URL-required provider as ready with no base URL")
	}

	// Local Ollama needs neither, and inherits the default endpoint.
	if _, err := st.Apply(id, Update{Provider: strptr("ollama")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	prov, cfg, err := st.Resolve(id)
	if err != nil {
		t.Fatalf("Resolve(ollama): %v", err)
	}
	if cfg.BaseURL != prov.DefaultBaseURL {
		t.Errorf("BaseURL = %q, want the provider default %q", cfg.BaseURL, prov.DefaultBaseURL)
	}
	if cfg.Model != prov.DefaultModel {
		t.Errorf("Model = %q, want the provider default %q", cfg.Model, prov.DefaultModel)
	}
}

func TestResolveDefaultsToCopilotForANewBrowser(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	prov, _, _ := st.Resolve("brand-new")
	if prov.ID != DefaultProviderID {
		t.Errorf("new identity resolved to %q, want %q", prov.ID, DefaultProviderID)
	}
}

func TestForgetClearsOneProviderOnly(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	const id = "abc"
	if _, err := st.Apply(id, Update{Provider: strptr("openai"), APIKey: strptr("sk-openai")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := st.Apply(id, Update{Provider: strptr("anthropic"), APIKey: strptr("sk-anthropic")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := st.Forget(id, "anthropic"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	pub := st.Public(id)
	if pub.Providers["anthropic"].HasKey {
		t.Error("anthropic key survived Forget")
	}
	if !pub.Providers["openai"].HasKey {
		t.Error("Forget cleared an unrelated provider")
	}
}

func TestSettingsFileIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	st := NewConfigStore(dir)
	if _, err := st.Apply("abc", Update{Provider: strptr("openai"), APIKey: strptr("sk-secret")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "ai-config-abc.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Errorf("settings file mode = %o, want 600 (it holds an API key)", perm)
	}
}

func TestSettingsSurviveAReload(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewConfigStore(dir).Apply("abc", Update{Provider: strptr("ollama-cloud"), APIKey: strptr("sk-cloud")}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// A fresh store (as after a restart) must read the same values back.
	prov, cfg, err := NewConfigStore(dir).Resolve("abc")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if prov.ID != "ollama-cloud" || cfg.APIKey != "sk-cloud" {
		t.Errorf("reload gave provider %q key %q", prov.ID, cfg.APIKey)
	}
}
