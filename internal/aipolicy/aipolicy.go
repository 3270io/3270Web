// SPDX-License-Identifier: AGPL-3.0-or-later

// Package aipolicy is what the person running an instance has decided about
// the AI chat: whether it is on, which providers may be used, and where the
// server may be made to send requests on a user's behalf.
//
// It exists because those were not decisions anybody could make. Any signed-in
// user could point a provider's base URL at an address on the server's own
// network and read back whatever answered with a 2xx — the chat proxy is an
// HTTP client that runs inside the deployment and goes where it is told. The
// settings here are read from the environment, which only the operator
// writes; none of them can be set through the settings API or the chat
// itself.
//
// It is a package of its own, rather than part of internal/aiprovider, because
// internal/copilot needs the same answers and aiprovider already imports it.
package aipolicy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jnnngs/3270Web/internal/egress"
	"github.com/jnnngs/3270Web/internal/hostpolicy"
)

const (
	// EnvDisabled turns the AI chat off for the whole instance.
	EnvDisabled = "AI_CHAT_DISABLED"
	// EnvAllowedProviders is a comma-separated list of provider ids that may
	// be selected. Empty means all of them.
	EnvAllowedProviders = "AI_ALLOWED_PROVIDERS"
	// EnvAllowedEndpointHosts is a comma-separated glob list of hosts a custom
	// base URL may name. Empty means any host the address rules allow.
	EnvAllowedEndpointHosts = "AI_ALLOWED_ENDPOINT_HOSTS"
	// EnvAllowPrivateEndpoints says whether a provider endpoint may be on a
	// loopback or private address. Unset, it follows the sign-in mode.
	EnvAllowPrivateEndpoints = "AI_ALLOW_PRIVATE_ENDPOINTS"

	// authModeEnv is the sign-in mode. Read here by name rather than through
	// the authorization package, which this must not depend on.
	authModeEnv = "AUTH_MODE"
)

// ErrDisabled is returned when the operator has turned the AI chat off.
var ErrDisabled = errors.New("the AI chat is turned off on this instance")

// DisabledMessage is what a caller is told when the feature is off.
const DisabledMessage = "The AI chat is turned off on this instance by its administrator."

func truthy(value string) (set, on bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return false, false
	case "1", "true", "yes", "on":
		return true, true
	default:
		return true, false
	}
}

// Disabled reports whether the operator has turned the AI chat off.
func Disabled() bool {
	_, on := truthy(os.Getenv(EnvDisabled))
	return on
}

// AllowedProviders returns the provider ids the operator permits, lower-cased,
// or nil when every provider is permitted.
func AllowedProviders() []string {
	var out []string
	for _, part := range strings.Split(os.Getenv(EnvAllowedProviders), ",") {
		if id := strings.ToLower(strings.TrimSpace(part)); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// ProviderAllowed reports whether a provider may be used on this instance.
func ProviderAllowed(id string) bool {
	allowed := AllowedProviders()
	if len(allowed) == 0 {
		return true
	}
	id = strings.ToLower(strings.TrimSpace(id))
	for _, candidate := range allowed {
		if candidate == id {
			return true
		}
	}
	return false
}

// SingleOperator reports whether the instance runs without sign-in, where the
// one person using it is also the person who administers it.
func SingleOperator() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(authModeEnv)))
	return mode == "" || mode == "none"
}

// PrivateEndpointsAllowed reports whether a provider endpoint may be on a
// loopback or private address.
//
// The default depends on who can set an endpoint. With no sign-in there is one
// operator, the machine is theirs, and a model running on it is the most
// ordinary thing they could want — so it works without configuration. Once
// accounts exist, "any signed-in user" and "the person who administers the
// network this server sits in" are different people, and an endpoint on a
// private address is a request the server makes inside that network for
// whoever asked. That is refused unless the operator says otherwise.
func PrivateEndpointsAllowed() bool {
	if set, on := truthy(os.Getenv(EnvAllowPrivateEndpoints)); set {
		return on
	}
	return SingleOperator()
}

// Egress is the address policy for every request the AI chat makes: provider
// calls, model listings, and the Copilot sign-in and token exchange.
func Egress() egress.Policy {
	policy := egress.Default()
	policy.DenyCrossHostRedirects = true
	if !PrivateEndpointsAllowed() {
		policy.DenyLoopback = true
		policy.DenyPrivate = true
	}
	return policy
}

type clientKey struct {
	denyPrivate bool
	timeout     time.Duration
}

var (
	clientsMu sync.Mutex
	clients   = map[clientKey]*http.Client{}
)

// HTTPClient returns the client for the current policy, shared across the
// process.
//
// One per policy and timeout, not one per request. A client owns a transport,
// and a transport owns the pool of connections that makes the second request
// to a provider cheaper than the first; building a new one for every chat
// turn threw that pool away each time and left the old connections to idle
// out on their own.
func HTTPClient(timeout time.Duration) *http.Client {
	policy := Egress()
	key := clientKey{denyPrivate: policy.DenyPrivate, timeout: timeout}
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if client, ok := clients[key]; ok {
		return client
	}
	client := policy.HTTPClient(timeout)
	clients[key] = client
	return client
}

// ErrEndpointRefused marks an endpoint this instance will not call, as
// distinct from one that could not be reached.
type ErrEndpointRefused struct{ Reason string }

func (e *ErrEndpointRefused) Error() string { return e.Reason }

// CheckEndpoint reports whether the server may send provider requests to a
// URL. custom says the URL was supplied by a user rather than being a
// provider's own published endpoint; only those are held to the host
// allowlist, since the built-in endpoints are already governed by which
// providers are allowed.
//
// The address rules are applied again by the dialer on every connection. This
// is the same answer given early, while there is still a settings dialog open
// to show it in.
func CheckEndpoint(ctx context.Context, rawURL string, custom bool) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		// Not this function's question; the URL validator has its own say.
		return nil
	}
	host := u.Hostname()
	if custom {
		if patterns := strings.TrimSpace(os.Getenv(EnvAllowedEndpointHosts)); patterns != "" && !hostpolicy.Allowed(patterns, host) {
			return &ErrEndpointRefused{Reason: fmt.Sprintf(
				"this instance does not permit %q as an AI provider endpoint (see %s)", host, EnvAllowedEndpointHosts)}
		}
	}
	if err := Egress().CheckHost(ctx, host); err != nil {
		var blocked *egress.ErrBlocked
		if errors.As(err, &blocked) {
			reason := blocked.Reason
			if !PrivateEndpointsAllowed() {
				reason += fmt.Sprintf(" (an administrator can permit local and private endpoints with %s=true)", EnvAllowPrivateEndpoints)
			}
			return &ErrEndpointRefused{Reason: fmt.Sprintf("this instance will not call %q: %s", host, reason)}
		}
		return err
	}
	return nil
}
