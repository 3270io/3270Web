// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jnnngs/3270Web/internal/reqsec"
)

// newMetaContext builds a gin context around a request the way the connect
// handler would see it.
func newMetaContext(t *testing.T, host string, headers map[string]string, overTLS bool) *gin.Context {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if overTLS {
		req.TLS = &tls.ConnectionState{}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func TestPageBaseURL(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		headers    map[string]string
		overTLS    bool
		trustProxy bool
		want       string
	}{
		{
			name: "plain http listener",
			host: "3270.example.com:3270",
			want: "http://3270.example.com:3270",
		},
		{
			name:    "direct TLS listener",
			host:    "3270.example.com",
			overTLS: true,
			want:    "https://3270.example.com",
		},
		{
			// The common reverse-proxy shape: the browser spoke HTTPS to the
			// edge, the hop into this process is plain HTTP. Without the
			// forwarded headers the card would advertise an http:// image on an
			// https:// page.
			name:       "proxy headers believed when trusted",
			host:       "internal:3270",
			headers:    map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "3270.example.com"},
			trustProxy: true,
			want:       "https://3270.example.com",
		},
		{
			// Same request, no opt-in. The headers are set by whoever sends the
			// request, so they are ignored and the internal address stands.
			name:    "proxy headers ignored by default",
			host:    "internal:3270",
			headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "3270.example.com"},
			want:    "http://internal:3270",
		},
		{
			name:       "first entry of a forwarded chain wins",
			host:       "internal:3270",
			headers:    map[string]string{"X-Forwarded-Host": "3270.example.com, inner.invalid"},
			trustProxy: true,
			want:       "http://3270.example.com",
		},
		{
			// Not an authority, so there is no URL to build. The template emits
			// no card rather than one a scraper cannot fetch.
			name:       "host with a space yields nothing",
			host:       "internal:3270",
			headers:    map[string]string{"X-Forwarded-Host": "evil host"},
			trustProxy: true,
			want:       "",
		},
		{
			name: "empty host yields nothing",
			host: "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.trustProxy {
				t.Setenv(reqsec.TrustProxyHeadersEnv, "true")
			}
			got := pageBaseURL(newMetaContext(t, tc.host, tc.headers, tc.overTLS))
			if got != tc.want {
				t.Errorf("pageBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPageBaseURLNilContext(t *testing.T) {
	if got := pageBaseURL(nil); got != "" {
		t.Errorf("pageBaseURL(nil) = %q, want empty", got)
	}
}

// The social tags are only worth having if they survive edits to the template,
// so the shape is asserted rather than left to review.
func TestConnectTemplateSocialTags(t *testing.T) {
	source := templateSource(t, "connect.html")

	for _, want := range []string{
		`<meta name="description"`,
		`property="og:image"`,
		`property="og:image:width" content="1200"`,
		`property="og:image:height" content="630"`,
		`name="twitter:card" content="summary_large_image"`,
		`{{ if .BaseURL }}`,
	} {
		if !strings.Contains(source, want) {
			t.Errorf("connect.html: missing %s", want)
		}
	}
}

// A live session and an error page both carry host state, so neither belongs in
// a search index.
func TestSessionTemplatesAreNotIndexable(t *testing.T) {
	for _, name := range []string{"screen.html", "error.html"} {
		if !strings.Contains(templateSource(t, name), `<meta name="robots" content="noindex, nofollow">`) {
			t.Errorf("%s: expected a noindex robots meta", name)
		}
	}
}
