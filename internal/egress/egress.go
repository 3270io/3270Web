// SPDX-License-Identifier: AGPL-3.0-or-later

// Package egress decides which addresses this server may open a connection
// to on a caller's behalf, and enforces that at the moment the connection is
// made rather than when the destination was typed in.
//
// The distinction is the whole point. Every destination check in this codebase
// used to run against a string: parse the URL, look at the hostname, reject it
// if it happens to be a forbidden IP literal. A hostname is not an address,
// and three ordinary things sit between the two:
//
//   - Names resolve. "localhost" is not an IP literal, so a literal check
//     never looks at it; neither is a name the caller controls in their own
//     DNS zone, pointed at 169.254.169.254.
//   - Answers change. A name checked at save time and resolved again at
//     request time is two lookups, and nothing says they agree.
//   - Servers redirect. A destination that passed every check can answer 302
//     and send the request — with whatever credential it is carrying —
//     somewhere that would not have.
//
// So the check lives in the dialer. Policy.DialContext resolves the name
// itself, tests every address the resolver returned, and connects to one it
// approved — the same address it tested, so there is no second lookup for a
// rebind to win. Redirects re-enter the same dialer, which is what makes a
// redirect hop no different from a first request.
package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Policy decides which resolved addresses may be dialled.
//
// The zero value denies nothing beyond the addresses that are never a
// legitimate destination for a server-side request (see Address). Set
// DenyPrivate to fence an instance to public networks.
type Policy struct {
	// DenyLoopback refuses 127.0.0.0/8 and ::1.
	//
	// Loopback is the one address class where "reachable from this server" and
	// "somewhere the caller meant" are furthest apart: nothing a caller names
	// is served from inside this process's own machine except by accident, and
	// what is listening there is this program's neighbours — an admin port, a
	// database, another container's sidecar.
	//
	// It is a flag rather than an unconditional refusal because one feature
	// genuinely wants it: an AI model running on the same machine is the
	// point of the Ollama provider, and its default base URL says localhost.
	DenyLoopback bool

	// DenyPrivate refuses RFC1918 and ULA addresses.
	//
	// Off by default because the features this guards are partly about
	// reaching those: a gateway on the corporate LAN, a mainframe on an
	// internal subnet. An operator who wants them fenced turns this on;
	// nobody gets it by accident.
	DenyPrivate bool

	// Lookup turns a name into addresses. nil means the system resolver.
	//
	// A function rather than a *net.Resolver so a test can describe a hostile
	// DNS answer — "this name comes back as 169.254.169.254" — without
	// standing up a DNS server or depending on what the machine running the
	// test happens to resolve.
	Lookup func(ctx context.Context, host string) ([]net.IP, error)

	// Timeout bounds one TCP connect attempt. Zero means 10 seconds.
	Timeout time.Duration
}

// ErrBlocked marks a refusal by policy rather than a failure to connect, so a
// caller can tell "this instance will never dial that" from "that host did not
// answer".
type ErrBlocked struct {
	Host   string
	IP     net.IP
	Reason string
}

func (e *ErrBlocked) Error() string {
	if e.IP == nil {
		return fmt.Sprintf("refusing to connect to %q: %s", e.Host, e.Reason)
	}
	return fmt.Sprintf("refusing to connect to %q (%s): %s", e.Host, e.IP, e.Reason)
}

// Address reports why an address may not be dialled, or "" if it may.
//
// The three refused unconditionally are the ones that never name a service a
// caller legitimately asked this server to reach:
//
//   - Link-local, which is where cloud instance-metadata services live
//     (169.254.169.254 and fe80::/10). A request there carries this instance's
//     own machine credentials.
//   - The unspecified address, which is not a destination at all.
//   - Multicast and the broadcast address, for the same reason.
func (p Policy) Address(ip net.IP) string {
	switch {
	case ip == nil:
		return "not an address"
	case ip.IsUnspecified():
		return "the unspecified address is not a destination"
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return "link-local addresses are not a destination (this range includes cloud metadata services)"
	case ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		return "multicast addresses are not a destination"
	case ip.Equal(net.IPv4bcast):
		return "the broadcast address is not a destination"
	}
	if p.DenyLoopback && ip.IsLoopback() {
		return "loopback addresses only reach services on this server itself"
	}
	if p.DenyPrivate && ip.IsPrivate() {
		return "private addresses are not permitted by this instance's egress policy"
	}
	return ""
}

// CheckHost resolves host and reports the first address that policy refuses.
//
// Every answer is tested, not just the first: a name that resolves to one
// public address and one metadata address is a name that can hand out either.
// A lookup that fails is not a refusal — the connection is going to fail on
// its own, with a message that says so.
func (p Policy) CheckHost(ctx context.Context, host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if reason := p.Address(ip); reason != "" {
			return &ErrBlocked{Host: host, IP: ip, Reason: reason}
		}
		return nil
	}
	addrs, err := p.lookup(ctx, host)
	if err != nil {
		return nil
	}
	for _, ip := range addrs {
		if reason := p.Address(ip); reason != "" {
			return &ErrBlocked{Host: host, IP: ip, Reason: reason}
		}
	}
	return nil
}

func (p Policy) lookup(ctx context.Context, host string) ([]net.IP, error) {
	if p.Lookup != nil {
		return p.Lookup(ctx, host)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.IP)
	}
	return out, nil
}

func (p Policy) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 10 * time.Second
}

// DialContext is a net.Dialer-shaped dial that only ever connects to an
// address it has approved.
//
// It resolves the name itself and dials the resulting IP literal, so the
// address that was tested is the address that is connected to. Handing the
// name to the system dialer instead would mean a second lookup nobody checked,
// which is the whole of a DNS-rebinding attack.
func (p Policy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	var candidates []net.IP
	if ip := net.ParseIP(host); ip != nil {
		candidates = []net.IP{ip}
	} else {
		candidates, err = p.lookup(ctx, host)
		if err != nil {
			return nil, err
		}
	}
	if len(candidates) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}

	// Refuse if any answer is disallowed, rather than quietly using whichever
	// one happens to be permitted. A name that can resolve to a forbidden
	// address is one this server should not be following, and picking the
	// acceptable answer out of a set the caller controls is a race waiting for
	// the ordering to change.
	for _, ip := range candidates {
		if reason := p.Address(ip); reason != "" {
			return nil, &ErrBlocked{Host: host, IP: ip, Reason: reason}
		}
	}

	dialer := &net.Dialer{Timeout: p.timeout()}
	var lastErr error
	for _, ip := range candidates {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// maxRedirects bounds a redirect chain. Go's own default is 10; this is the
// same number, restated here because the redirect hook replaces the default.
const maxRedirects = 10

// HTTPClient returns an http.Client whose every connection — first request and
// every redirect hop alike — goes through this policy.
//
// The redirect hook re-checks the scheme rather than the address: the address
// is the dialer's job and it does it on each hop already. What the dialer
// cannot see is a redirect to a scheme that never reaches it at all.
func (p Policy) HTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext:           p.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return &ErrBlocked{
					Host:   req.URL.Host,
					Reason: fmt.Sprintf("redirect to unsupported scheme %q", req.URL.Scheme),
				}
			}
			return nil
		},
	}
}

// Default is the policy every outbound client in this program uses.
//
// Loopback and private addresses stay reachable: a model running on this
// machine and a gateway on the corporate LAN are what the AI provider setting
// is for, and an internal subnet is where a mainframe lives. What is refused
// is the set that is never a destination somebody meant to name.
func Default() Policy { return Policy{} }
