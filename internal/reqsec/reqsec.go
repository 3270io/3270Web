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

// TLSTerminatedUpstreamEnv names the operator's assertion that something in
// front of this server terminates TLS, whatever the request headers say.
//
// TRUST_PROXY_HEADERS is the better answer and handles most deployments,
// because most edges — a CDN, an ingress controller, an ordinary reverse proxy
// — do send X-Forwarded-Proto. This is for the ones that do not: a tunnel
// daemon dialling out to the edge, a service mesh sidecar, anything where the
// hop into this process carries no evidence of the hop the browser actually
// made.
//
// It is a claim about the deployment, not about a request, so it applies to
// every request equally and cannot be inferred. Getting it wrong fails loudly
// rather than quietly: cookies are minted Secure, and a browser on plain HTTP
// refuses to store them, so nobody can sign in. That is the right direction
// for a mistake of this kind to break in.
const TLSTerminatedUpstreamEnv = "TLS_TERMINATED_UPSTREAM"

// TrustProxyHeaders reports whether X-Forwarded-* may be believed.
//
// This is opt-in and must stay that way. The header is set by whoever sends
// the request, so trusting it unconditionally lets any client assert its own
// connection is secure — which would hand back exactly the property the
// Secure flag is supposed to guarantee.
func TrustProxyHeaders() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(TrustProxyHeadersEnv)), "true")
}

// TLSTerminatedUpstream reports the operator's standing assertion that the
// browser reached the edge over HTTPS. See TLSTerminatedUpstreamEnv.
func TLSTerminatedUpstream() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(TLSTerminatedUpstreamEnv)), "true")
}

// ForwardedProtoClaimsHTTPS reports whether the request carries an
// X-Forwarded-Proto naming https — without trusting it.
//
// It exists only so the sign-in page can tell the difference between "nothing
// is in front of this server" and "something in front says it terminated TLS
// and this server has not been told to believe it". The second is one
// environment variable away from being right, and saying so is far more use
// than a warning that only describes the symptom. It must never reach a
// security decision: the header is chosen by whoever sends the request, which
// is exactly why IsTLS gates it behind TrustProxyHeaders.
func ForwardedProtoClaimsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	return forwardedProtoIsHTTPS(r)
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
	// The operator's standing assertion, checked before the headers because it
	// applies whether or not any arrived. Deliberately not gated on
	// TrustProxyHeaders: the two answer different questions, and an edge that
	// forwards nothing is the case this exists for.
	if TLSTerminatedUpstream() {
		return true
	}
	if !TrustProxyHeaders() {
		return false
	}
	return forwardedProtoIsHTTPS(r)
}

// forwardedProtoIsHTTPS reads the client-facing scheme out of
// X-Forwarded-Proto. A proxy chain appends, so the first entry is the one the
// browser spoke.
func forwardedProtoIsHTTPS(r *http.Request) bool {
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
