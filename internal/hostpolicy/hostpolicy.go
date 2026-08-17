// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hostpolicy decides which mainframes an instance may be pointed at.
//
// Hostname *validity* is a different question, answered elsewhere: whether a
// string parses, and whether it names a loopback or link-local address this
// server should never dial. This package answers the question after that —
// whether a valid, routable host is one this deployment is willing to reach.
//
// It matters most where 3270Web is not on the operator's own laptop. A shared
// instance can dial anything its container can reach, so without an allowlist
// the terminal is a route into the network it sits in, available to anybody
// who can open a session.
package hostpolicy

import (
	"net"
	"path/filepath"
	"strings"
)

// Allowed reports whether hostname matches a comma-separated pattern list.
//
// An empty list allows everything. That is the historical behaviour and the
// right default for a laptop or a lab: an allowlist that has to be configured
// before the product works at all is one people switch off.
//
// Patterns are shell globs matched against the host part only, so a pattern
// does not have to anticipate every port somebody might use. Matching is
// case-insensitive, since hostnames are.
func Allowed(patterns, hostname string) bool {
	patterns = strings.TrimSpace(patterns)
	if patterns == "" {
		return true
	}

	host := Hostname(hostname)
	if host == "" {
		// Nothing to match. Refusing is the fail-closed reading, and a host
		// that cannot be identified is not one to dial on a fenced instance.
		return false
	}

	for _, pattern := range strings.Split(patterns, ",") {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		// A pattern may itself carry a port, which people write out of habit.
		// Compare host to host rather than refusing to match it.
		if p := Hostname(pattern); p != "" {
			pattern = p
		}
		if ok, err := filepath.Match(pattern, host); ok && err == nil {
			return true
		}
	}
	return false
}

// Hostname strips a port and IPv6 brackets, lower-casing what is left.
//
// Written with net.SplitHostPort rather than a search for ":" because an IPv6
// literal is full of colons: "[2001:db8::1]:23" cut at the first one leaves
// "[2001", which matches nothing and would silently refuse every v6 host on a
// fenced instance.
func Hostname(target string) string {
	host := strings.ToLower(strings.TrimSpace(target))
	if host == "" {
		return ""
	}

	// s3270 target prefixes: "L:" for TLS, "Y:" to skip verification, and an
	// "LU@" prefix. A profile can carry these, and they are not part of the
	// host being reached.
	for {
		switch {
		case strings.HasPrefix(host, "l:"):
			host = host[2:]
		case strings.HasPrefix(host, "y:"):
			host = host[2:]
		default:
			goto stripped
		}
	}
stripped:
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	// No port, or an unparseable one. A bare bracketed literal is still ours
	// to unwrap; anything else is returned as it stands.
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}
