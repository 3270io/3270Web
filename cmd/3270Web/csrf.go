package main

import (
	"log"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// OriginRefererCheckMiddleware protects against CSRF by verifying that the
// Origin or Referer header matches the Host header for unsafe methods.
func OriginRefererCheckMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Safe methods are allowed without checks
		if isSafeMethod(c.Request.Method) {
			c.Next()
			return
		}

		// The public REST API uses Bearer-token auth and is intended for
		// non-browser clients (curl, RPA bots), which do not send Origin
		// headers. CSRF defense for the API surface is provided by the
		// Authorization header check in api_v1.go.
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/") {
			c.Next()
			return
		}

		// Check Origin header first (preferred)
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			// Note for anyone adding embedding support here: a framed terminal
			// does not need an exemption. The document inside the frame is
			// served by this origin, so its own requests are same-origin and
			// this check passes unchanged. What a page on another origin gets
			// is the /api/v1 surface with CORS (see embedding.go), which is
			// bearer-authenticated and carries no cookie — so widening this
			// check would buy nothing and cost the guarantee it exists for.
			u, err := url.Parse(origin)
			if err != nil {
				log.Printf("CSRF protection: Invalid Origin header: %q", origin)
				c.AbortWithStatus(403)
				return
			}
			if !isValidHost(u.Host, c.Request.Host) {
				log.Printf("CSRF protection: Origin mismatch. Got %q, want %q", u.Host, c.Request.Host)
				c.AbortWithStatus(403)
				return
			}
			c.Next()
			return
		}

		// Check Referer header if Origin is missing
		referer := c.Request.Header.Get("Referer")
		if referer != "" {
			u, err := url.Parse(referer)
			if err != nil {
				log.Printf("CSRF protection: Invalid Referer header: %q", referer)
				c.AbortWithStatus(403)
				return
			}
			if !isValidHost(u.Host, c.Request.Host) {
				log.Printf("CSRF protection: Referer mismatch. Got %q, want %q", u.Host, c.Request.Host)
				c.AbortWithStatus(403)
				return
			}
			c.Next()
			return
		}

		// If neither header is present for an unsafe method, block it.
		// Modern browsers consistently send Origin for cross-origin POSTs,
		// and Referer for same-origin requests (unless Referrer-Policy suppresses it).
		log.Printf("CSRF protection: Missing Origin and Referer headers for %s request", c.Request.Method)
		c.AbortWithStatus(403)
	}
}

func isSafeMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "OPTIONS", "TRACE":
		return true
	}
	return false
}

func isValidHost(got, expected string) bool {
	// Compare hostname and port (if present).
	// We use EqualFold to be case-insensitive.
	return strings.EqualFold(got, expected)
}
