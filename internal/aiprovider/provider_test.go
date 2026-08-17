// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"strings"
	"testing"
)

func TestValidateBaseURLAccepts(t *testing.T) {
	cases := map[string]string{
		"http://localhost:11434/v1":                               "http://localhost:11434/v1",
		"https://api.openai.com/v1/":                              "https://api.openai.com/v1",
		"https://generativelanguage.googleapis.com/v1beta/openai": "https://generativelanguage.googleapis.com/v1beta/openai",
		// Private LAN addresses are legitimate here: "the gateway on our
		// network" is a first-class use case, unlike the GHE host check.
		"http://10.0.0.5:8000/v1": "http://10.0.0.5:8000/v1",
		"":                        "",
	}
	for in, want := range cases {
		got, err := ValidateBaseURL(in)
		if err != nil {
			t.Errorf("ValidateBaseURL(%q) returned error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ValidateBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateBaseURLRejects(t *testing.T) {
	bad := []string{
		"ftp://example.com/v1",
		"api.openai.com/v1",              // no scheme
		"https://user:pass@example.com",  // credentials in URL
		"https://example.com/v1?key=abc", // query smuggling
		"https://example.com/v1#frag",
		"http://169.254.169.254/latest", // cloud instance metadata
		"http://",
	}
	for _, in := range bad {
		if _, err := ValidateBaseURL(in); err == nil {
			t.Errorf("ValidateBaseURL(%q) accepted a value it should reject", in)
		}
	}
}

func TestLookupCoversEveryCatalogueEntry(t *testing.T) {
	for _, p := range Providers() {
		got, ok := Lookup(p.ID)
		if !ok {
			t.Fatalf("Lookup(%q) missed a catalogued provider", p.ID)
		}
		if got.Label == "" {
			t.Errorf("provider %q has no label", p.ID)
		}
		if got.Wire != WireOpenAI && got.Wire != WireAnthropic {
			t.Errorf("provider %q has unknown wire %q", p.ID, got.Wire)
		}
		// Every provider must be able to name a model without a live /models
		// call, or its dropdown renders empty on first open.
		if !got.BaseURLRequired && len(got.Models) == 0 {
			t.Errorf("provider %q has no fallback model list", p.ID)
		}
		if got.DefaultBaseURL != "" {
			if strings.HasSuffix(got.DefaultBaseURL, "/") {
				t.Errorf("provider %q default base URL has a trailing slash", p.ID)
			}
			if _, err := ValidateBaseURL(got.DefaultBaseURL); err != nil {
				t.Errorf("provider %q default base URL is invalid: %v", p.ID, err)
			}
		}
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup returned a provider for an unknown ID")
	}
}

func TestDefaultProviderIsCopilot(t *testing.T) {
	if !IsCopilot(DefaultProviderID) {
		t.Fatalf("DefaultProviderID %q is not the Copilot provider", DefaultProviderID)
	}
	if _, ok := Lookup(DefaultProviderID); !ok {
		t.Fatal("DefaultProviderID is not in the catalogue")
	}
}
