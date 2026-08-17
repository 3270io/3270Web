// SPDX-License-Identifier: AGPL-3.0-or-later

package aiprovider

import (
	"context"
	"net/http"
	"net/url"

	"github.com/jnnngs/3270Web/internal/egress"
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
// the first request and on every redirect hop. Loopback and private addresses
// stay reachable, deliberately: "Ollama on localhost" and "a gateway on the
// corporate LAN" are two of the providers this package exists to offer.
//
// There is no client-wide timeout. A chat completion is a stream that can
// legitimately run for minutes; it is bounded by the handler's context
// deadline and by the per-stream idle watchdog, both of which understand the
// difference between a slow answer and a stalled one. A blanket Timeout here
// would cut off long completions mid-sentence.
func outboundClient() *http.Client {
	return egress.Default().HTTPClient(0)
}

// checkBaseURLDestination reports whether a base URL about to be saved names
// somewhere this server is willing to send a request.
//
// Purely so the answer arrives at a useful moment. The dialer refuses the same
// destination whether or not this ran, but it refuses it later — at the first
// chat, as a failed completion — and by then the settings dialog is closed and
// the message reads as "the provider is down" rather than "that address is not
// one this server will call".
//
// An empty or syntactically invalid value is left alone: ValidateBaseURL is
// about to have its own say, and reporting "cannot resolve" for a string that
// is not a URL would be answering the wrong question.
func checkBaseURLDestination(ctx context.Context, raw string) error {
	clean, err := ValidateBaseURL(raw)
	if err != nil || clean == "" {
		return nil
	}
	u, err := url.Parse(clean)
	if err != nil {
		return nil
	}
	return egress.Default().CheckHost(ctx, u.Hostname())
}
