// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"testing"
)

// A key is issued for the endpoint it was entered against. Pointing the
// provider somewhere else without re-entering it must not send the old
// credential to the new origin — that is the exfiltration path an inherited
// key opens on a shared browser.
func TestChangingOriginDropsTheStoredKey(t *testing.T) {
	st := NewConfigStore(t.TempDir())

	if _, err := st.Apply("id", Update{
		Provider: strptr("openai"),
		APIKey:   strptr("sk-secret"),
	}); err != nil {
		t.Fatal(err)
	}
	if !st.Public("id").Providers["openai"].HasKey {
		t.Fatal("the key was not stored to begin with")
	}

	if _, err := st.Apply("id", Update{BaseURL: strptr("https://evil.example.test/v1")}); err != nil {
		t.Fatal(err)
	}
	if st.Public("id").Providers["openai"].HasKey {
		t.Error("the stored key survived a move to a different origin")
	}
	if _, _, err := st.Resolve("id"); err == nil {
		t.Error("Resolve still considered the provider usable, so a chat would have been sent with no key rather than refused")
	}
}

// Sending a key in the same request that moves the endpoint is the caller
// saying the key belongs there. That has to keep working, or changing a
// self-hosted gateway's address becomes two round trips and a support ticket.
func TestChangingOriginKeepsAKeySuppliedWithIt(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	if _, err := st.Apply("id", Update{Provider: strptr("openai"), APIKey: strptr("sk-old")}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply("id", Update{
		BaseURL: strptr("https://gateway.example.test/v1"),
		APIKey:  strptr("sk-new"),
	}); err != nil {
		t.Fatal(err)
	}
	if !st.Public("id").Providers["openai"].HasKey {
		t.Error("a key supplied alongside the new endpoint was dropped")
	}
	_, cfg, err := st.Resolve("id")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "sk-new" {
		t.Errorf("stored key = %q, want the one just supplied", cfg.APIKey)
	}
}

// Saving the settings form without touching the endpoint — a model change, a
// re-save of the same URL — must not silently log the user out of their
// provider.
func TestSavingWithoutMovingKeepsTheKey(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	if _, err := st.Apply("id", Update{
		Provider: strptr("openai"),
		BaseURL:  strptr("https://api.openai.com/v1"),
		APIKey:   strptr("sk-secret"),
	}); err != nil {
		t.Fatal(err)
	}

	for _, up := range []Update{
		{Model: strptr("gpt-5")},
		{BaseURL: strptr("https://api.openai.com/v1")},
		// A different path on the same host is the same server.
		{BaseURL: strptr("https://api.openai.com/v1beta")},
		// Clearing the override falls back to the provider's own default,
		// which for OpenAI is the URL it already had.
		{BaseURL: strptr("")},
	} {
		if _, err := st.Apply("id", up); err != nil {
			t.Fatalf("Apply(%+v) = %v", up, err)
		}
		if !st.Public("id").Providers["openai"].HasKey {
			t.Fatalf("Apply(%+v) dropped the key without the origin changing", up)
		}
	}
}

// An explicit empty key still clears it: that is how "sign out" works.
func TestExplicitEmptyKeyStillClears(t *testing.T) {
	st := NewConfigStore(t.TempDir())
	if _, err := st.Apply("id", Update{Provider: strptr("openai"), APIKey: strptr("sk-secret")}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply("id", Update{APIKey: strptr("")}); err != nil {
		t.Fatal(err)
	}
	if st.Public("id").Providers["openai"].HasKey {
		t.Error("an explicitly cleared key was kept")
	}
}
