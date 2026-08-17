// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/reqsec"
)

// pageBaseURL returns the absolute origin — scheme and authority, no trailing
// slash — that this browser reached the server on, or "" when it cannot be
// determined.
//
// It exists for the social preview tags on the connect page. Open Graph
// requires absolute URLs: a scraper fetches og:image out of band, with no
// document to resolve a relative path against, so "/static/og-image.png" would
// simply not render and the link would post as a bare grey box. A self-hosted
// instance has no configured public address to build one from — it is whatever
// hostname the operator put in front of it — so the request is the only thing
// that knows.
//
// Behind a proxy the request into this process carries the internal address,
// not the one the browser typed, and X-Forwarded-Host carries the real one.
// That header is set by whoever sends the request, so it is believed only
// under TRUST_PROXY_HEADERS, the same gate every other forwarded-header
// decision in this process goes through. Getting it wrong here is cosmetic —
// a preview card that does not load — which is not a reason to widen the gate.
func pageBaseURL(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}

	host := strings.TrimSpace(c.Request.Host)
	if reqsec.TrustProxyHeaders() {
		if forwarded := c.Request.Header.Get("X-Forwarded-Host"); forwarded != "" {
			// A proxy chain appends, so the first entry is the host the browser
			// actually asked for.
			if idx := strings.IndexByte(forwarded, ','); idx >= 0 {
				forwarded = forwarded[:idx]
			}
			if trimmed := strings.TrimSpace(forwarded); trimmed != "" {
				host = trimmed
			}
		}
	}
	if host == "" {
		return ""
	}

	// The Host header reaches the template as an attribute value. html/template
	// escapes it, so markup cannot break out either way, but a value with a
	// space or a control character in it is not an authority at all and would
	// produce a URL no scraper can fetch — better to emit no card than a broken
	// one.
	if strings.ContainsAny(host, " \t\r\n/\\\"'<>") {
		return ""
	}
	if _, err := url.Parse("https://" + host); err != nil {
		return ""
	}

	scheme := "http"
	if requestIsSecure(c) {
		scheme = "https"
	}
	return scheme + "://" + host
}
