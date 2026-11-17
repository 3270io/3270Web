// SPDX-License-Identifier: AGPL-3.0-or-later

package aipolicy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledReadsTheSwitch(t *testing.T) {
	for value, want := range map[string]bool{
		"": false, "false": false, "0": false, "no": false, "off": false, "nonsense": false,
		"true": true, "TRUE": true, " 1 ": true, "yes": true, "on": true,
	} {
		t.Setenv(EnvDisabled, value)
		if got := Disabled(); got != want {
			t.Errorf("%s=%q: Disabled() = %v, want %v", EnvDisabled, value, got, want)
		}
	}
}

func TestProviderAllowed(t *testing.T) {
	t.Setenv(EnvAllowedProviders, "")
	if !ProviderAllowed("anything") {
		t.Error("with no list, every provider is permitted")
	}
	t.Setenv(EnvAllowedProviders, " Anthropic , ollama,, ")
	for id, want := range map[string]bool{"anthropic": true, "ANTHROPIC": true, "ollama": true, "openai": false, "": false} {
		if got := ProviderAllowed(id); got != want {
			t.Errorf("ProviderAllowed(%q) = %v, want %v", id, got, want)
		}
	}
}

// Private endpoints follow the sign-in mode unless the operator says.
func TestPrivateEndpointsDefaultFollowsTheSignInMode(t *testing.T) {
	cases := []struct {
		authMode, explicit string
		want               bool
	}{
		{"", "", true},
		{"none", "", true},
		{"local", "", false},
		{"oidc", "", false},
		{"LOCAL", "", false},
		{"local", "true", true},
		{"oidc", "1", true},
		{"none", "false", false},
		{"", "0", false},
	}
	for _, c := range cases {
		t.Setenv("AUTH_MODE", c.authMode)
		t.Setenv(EnvAllowPrivateEndpoints, c.explicit)
		if got := PrivateEndpointsAllowed(); got != c.want {
			t.Errorf("AUTH_MODE=%q %s=%q: PrivateEndpointsAllowed() = %v, want %v",
				c.authMode, EnvAllowPrivateEndpoints, c.explicit, got, c.want)
		}
		policy := Egress()
		if policy.DenyLoopback == c.want || policy.DenyPrivate == c.want {
			t.Errorf("AUTH_MODE=%q %s=%q: egress policy does not follow (%+v)", c.authMode, EnvAllowPrivateEndpoints, c.explicit, policy)
		}
		if !policy.DenyCrossHostRedirects {
			t.Error("AI requests carry credentials and must never follow a redirect off their host")
		}
	}
}

func TestCheckEndpoint(t *testing.T) {
	ctx := context.Background()
	refused := func(err error) bool {
		var r *ErrEndpointRefused
		return errors.As(err, &r)
	}

	t.Setenv("AUTH_MODE", "none")
	if err := CheckEndpoint(ctx, "http://127.0.0.1:11434/v1", true); err != nil {
		t.Errorf("a single operator's local model was refused: %v", err)
	}
	// Never a destination, whoever is asking.
	if err := CheckEndpoint(ctx, "http://169.254.169.254/latest", true); !refused(err) {
		t.Errorf("the metadata address was not refused: %v", err)
	}

	t.Setenv("AUTH_MODE", "local")
	for _, private := range []string{"http://127.0.0.1:11434/v1", "http://10.0.0.5/v1", "http://[::1]/v1", "http://192.168.1.1/v1"} {
		err := CheckEndpoint(ctx, private, true)
		if !refused(err) {
			t.Errorf("%s was not refused with sign-in on: %v", private, err)
			continue
		}
		if !strings.Contains(err.Error(), EnvAllowPrivateEndpoints) {
			t.Errorf("the refusal should name the setting, got %q", err)
		}
	}

	// The host allowlist binds custom endpoints only.
	t.Setenv("AUTH_MODE", "none")
	t.Setenv(EnvAllowedEndpointHosts, "*.internal.example.com, gateway.example.com")
	if err := CheckEndpoint(ctx, "https://elsewhere.example.org/v1", true); !refused(err) {
		t.Errorf("a custom endpoint off the allowlist was not refused: %v", err)
	}
	if err := CheckEndpoint(ctx, "https://elsewhere.example.org/v1", false); refused(err) {
		t.Errorf("a provider's own endpoint was held to the custom-endpoint allowlist: %v", err)
	}
	if err := CheckEndpoint(ctx, "not a url at all", true); err != nil {
		t.Errorf("a string that is not a URL is the validator's business, got %v", err)
	}
}

// One client per policy, not one per request: the transport's connection pool
// is the point of having a client at all.
func TestHTTPClientIsShared(t *testing.T) {
	t.Setenv("AUTH_MODE", "none")
	t.Setenv(EnvAllowPrivateEndpoints, "")
	first := HTTPClient(0)
	if HTTPClient(0) != first {
		t.Error("two requests under one policy got two clients")
	}
	if first.Transport == nil || HTTPClient(0).Transport != first.Transport {
		t.Error("the transport is not shared")
	}

	t.Setenv("AUTH_MODE", "local")
	if HTTPClient(0) == first {
		t.Error("a stricter policy must not reuse the permissive client")
	}
	t.Setenv("AUTH_MODE", "none")
	if HTTPClient(0) != first {
		t.Error("returning to the first policy should return its client")
	}
}

func TestCheckContentType(t *testing.T) {
	respond := func(contentType string) *http.Response {
		rec := httptest.NewRecorder()
		if contentType != "" {
			rec.Header().Set("Content-Type", contentType)
		}
		return rec.Result()
	}
	for contentType, ok := range map[string]bool{
		"text/event-stream":                true,
		"text/event-stream; charset=utf-8": true,
		"TEXT/EVENT-STREAM":                true,
		"application/json":                 false,
		"text/html":                        false,
		"":                                 false,
	} {
		err := CheckContentType(respond(contentType), ContentEventStream)
		if (err == nil) != ok {
			t.Errorf("event stream check of %q: err = %v, want ok=%v", contentType, err, ok)
		}
	}
	if err := CheckContentType(respond("application/json; charset=utf-8"), ContentJSON); err != nil {
		t.Errorf("JSON check refused JSON: %v", err)
	}
	if err := CheckContentType(respond("text/event-stream"), ContentJSON); err == nil {
		t.Error("JSON check accepted an event stream")
	}
}

func TestPublicErrorSaysSomethingWithoutSayingEverything(t *testing.T) {
	status, msg := PublicError(errors.New("dial tcp 10.2.3.4:443: connect: connection refused"), false)
	if status != http.StatusBadGateway || strings.Contains(msg, "10.2.3.4") || msg == "" {
		t.Errorf("transport error: %d %q", status, msg)
	}
	status, msg = PublicError(ErrStreamStalled, false)
	if status != http.StatusGatewayTimeout || !strings.Contains(msg, "stopped sending") {
		t.Errorf("stalled stream: %d %q", status, msg)
	}
	status, msg = PublicError(ErrStreamIncomplete, false)
	if status != http.StatusBadGateway || !strings.Contains(msg, "before the response was complete") {
		t.Errorf("incomplete stream: %d %q", status, msg)
	}
	status, _ = PublicError(&ErrEndpointRefused{Reason: "no"}, false)
	if status != http.StatusForbidden {
		t.Errorf("refused endpoint: %d", status)
	}
}
