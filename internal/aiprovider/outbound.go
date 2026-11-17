// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"context"
	"net/http"

	"github.com/jnnngs/3270Web/internal/aipolicy"
)

// outboundClient is the HTTP client every provider in this package talks
// through.
//
// It exists so that the base URL a user saves is checked against the addresses
// it actually reaches, not against the characters they typed. ValidateBaseURL
// still runs — it is what rejects a credential in the URL, a query string, a
// non-HTTP scheme, and a link-local IP literal — but it can only judge a
// string. "http://metadata.example.com/v1" passes every one of those rules and
// resolves to whatever its owner points it at, and the client that follows a
// 302 from a legitimate provider follows it to an address nobody checked at
// all.
//
// egress.Policy closes both by asking the same question at connect time, on
// the first request and on every redirect hop — and a redirect that leaves the
// host the request was sent to is not followed at all, because the request is
// carrying that host's API key.
//
// Which addresses are permitted is the operator's decision, read from
// internal/aipolicy. With no sign-in, loopback and private addresses stay
// reachable: "Ollama on localhost" is the single operator's own machine. With
// accounts, they are refused unless AI_ALLOW_PRIVATE_ENDPOINTS says otherwise,
// because the person choosing the endpoint is no longer the person who runs
// the network it would be called from.
//
// The client is shared for the life of the process (per policy), so provider
// connections are reused between turns rather than rebuilt for each one.
//
// There is no client-wide timeout. A chat completion is a stream that can
// legitimately run for minutes; it is bounded by the handler's context
// deadline and by the per-stream idle watchdog, both of which understand the
// difference between a slow answer and a stalled one. A blanket Timeout here
// would cut off long completions mid-sentence.
func outboundClient() *http.Client {
	return aipolicy.HTTPClient(0)
}

// checkBaseURLDestination reports whether a base URL about to be saved names
// somewhere this server is willing to send a request: a host on the operator's
// endpoint allowlist, where there is one, that does not resolve to an address
// the policy refuses.
//
// Purely so the answer arrives at a useful moment. The same check runs again
// before every chat and the dialer refuses the same addresses whether or not
// this ran, but those refusals come later — as a failed completion — and by
// then the settings dialog is closed and the message reads as "the provider is
// down" rather than "that address is not one this server will call".
//
// An empty or syntactically invalid value is left alone: ValidateBaseURL is
// about to have its own say, and reporting "cannot resolve" for a string that
// is not a URL would be answering the wrong question.
func checkBaseURLDestination(ctx context.Context, raw string) error {
	clean, err := ValidateBaseURL(raw)
	if err != nil || clean == "" {
		return nil
	}
	return aipolicy.CheckEndpoint(ctx, clean, true)
}

// isCustomEndpoint reports whether a resolved config calls somewhere other
// than the provider's own published endpoint.
func isCustomEndpoint(prov Provider, cfg ProviderConfig) bool {
	return !sameOrigin(cfg.BaseURL, prov.DefaultBaseURL) || prov.DefaultBaseURL == ""
}
