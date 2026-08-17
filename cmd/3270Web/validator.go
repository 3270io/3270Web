// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net"
	"strconv"
	"strings"
)

func parseHostPort(hostname string) (string, int) {
	host := strings.TrimSpace(hostname)
	port := 3270
	if host == "" {
		return "", port
	}
	if id, samplePort, ok := parseSampleAppHost(host); ok {
		host = sampleAppHostname(id)
		if samplePort > 0 {
			if !isAllowedSampleAppPort(samplePort) {
				return "", port
			}
			port = samplePort
		}
		return host, port
	}
	if strings.Contains(host, ":") {
		if h, p, err := net.SplitHostPort(host); err == nil {
			host = h
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		}
	}
	return host, port
}

func isValidHostname(hostname string) bool {
	host := strings.TrimSpace(hostname)
	if host == "" {
		return false
	}

	if _, port, ok := parseSampleAppHost(host); ok {
		return port == 0 || isAllowedSampleAppPort(port)
	}

	// Extract port, if present.
	if strings.HasPrefix(host, "[") {
		h, p, err := net.SplitHostPort(host)
		if err == nil {
			host = h
			if p != "" {
				n, err := strconv.Atoi(p)
				if err != nil || n <= 0 || n > 65535 {
					return false
				}
			}
		} else {
			// Failed to split. Check if it is a bracketed IPv6 literal without port (e.g. "[::1]")
			if strings.HasSuffix(host, "]") {
				host = host[1 : len(host)-1]
			} else {
				return false
			}
		}
	} else if strings.Count(host, ":") == 1 {
		h, p, err := net.SplitHostPort(host)
		if err != nil {
			return false
		}
		host = h
		if n, err := strconv.Atoi(p); err != nil || n <= 0 || n > 65535 {
			return false
		}
	}

	if ip := net.ParseIP(host); ip != nil {
		if isRestrictedIP(ip) {
			return false
		}
		return true
	}

	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !isValidDomainLabel(label) {
			return false
		}
	}
	return true
}

func isValidDomainLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	return true
}

func isRestrictedIP(ip net.IP) bool {
	// Block link-local addresses (e.g. 169.254.x.x, fe80::) which include
	// cloud metadata services and local network segments.
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// Block loopback (127.0.0.0/8, ::1). A real TN3270 mainframe is never
	// reachable via loopback from this app's perspective — loopback only
	// exposes services co-located with this process itself (e.g. an
	// internal admin port on the same host), so there is no legitimate
	// connect target here. The built-in sample-app feature is unaffected:
	// its "sampleapp:" pseudo-hostname is resolved and short-circuited
	// earlier in isValidHostname, before this function is ever reached.
	if ip.IsLoopback() {
		return true
	}
	// Reject unspecified addresses (0.0.0.0, ::) too — never a valid target.
	if ip.IsUnspecified() {
		return true
	}
	return false
}

func parseSampleAppHost(hostname string) (string, int, bool) {
	trimmed := strings.TrimSpace(hostname)
	if !strings.HasPrefix(trimmed, sampleAppPrefix) {
		return "", 0, false
	}
	id := strings.TrimPrefix(trimmed, sampleAppPrefix)
	if id == "" {
		return "", 0, false
	}
	parts := strings.Split(id, ":")
	if len(parts) > 2 {
		return "", 0, false
	}
	if parts[0] == "" {
		return "", 0, false
	}
	if len(parts) == 2 {
		if parts[1] == "" {
			return "", 0, false
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", 0, false
		}
		return parts[0], port, true
	}
	return parts[0], 0, true
}
