// Package reqsec answers security questions about an inbound request that
// depend on how the deployment is fronted.
package reqsec

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// TrustProxyHeadersEnv names the opt-in for believing forwarding headers.
const TrustProxyHeadersEnv = "TRUST_PROXY_HEADERS"

// TrustProxyHeaders reports whether X-Forwarded-* may be believed.
//
// This is opt-in and must stay that way. The header is set by whoever sends
// the request, so trusting it unconditionally lets any client assert its own
// connection is secure — which would hand back exactly the property the
// Secure flag is supposed to guarantee.
func TrustProxyHeaders() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(TrustProxyHeadersEnv)), "true")
}

// IsTLS reports whether the client's connection to the edge used TLS.
//
// Checking r.TLS alone is right for a direct listener and wrong behind a
// TLS-terminating reverse proxy: the hop into the app is plain HTTP, so r.TLS
// is nil and cookies minted from it lose their Secure flag on a site the user
// is browsing over HTTPS. With TRUST_PROXY_HEADERS set, X-Forwarded-Proto
// carries the answer the app cannot otherwise see.
func IsTLS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if !TrustProxyHeaders() {
		return false
	}

	// A proxy chain appends, so the client-facing scheme is the first entry.
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		return false
	}
	if idx := strings.IndexByte(proto, ','); idx >= 0 {
		proto = proto[:idx]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// ClientIP returns the address the request came from.
//
// Behind a proxy every request appears to originate from the proxy, which
// would make per-address rate limiting and session-to-address binding
// meaningless — one bucket for the whole world, and one address for every
// session. X-Forwarded-For carries the real one, but only where a proxy is
// known to be rewriting it: the header is otherwise chosen by the client, and
// believing it would let an attacker pick their own rate-limit bucket or
// impersonate a bound address.
func ClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if TrustProxyHeaders() {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			// Left-most entry is the original client; proxies append.
			if idx := strings.IndexByte(forwarded, ','); idx >= 0 {
				forwarded = forwarded[:idx]
			}
			if ip := strings.TrimSpace(forwarded); ip != "" {
				return ip
			}
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		// RemoteAddr is not always host:port (httptest, unix sockets).
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
