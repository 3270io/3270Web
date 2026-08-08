package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/reqsec"
)

func TestNormalizeOriginAcceptsWhatABrowserSends(t *testing.T) {
	cases := map[string]string{
		"https://portal.example.com":       "https://portal.example.com",
		"https://PORTAL.example.com":       "https://portal.example.com",
		"HTTPS://portal.example.com":       "https://portal.example.com",
		"https://portal.example.com:8443":  "https://portal.example.com:8443",
		"https://portal.example.com/":      "https://portal.example.com",
		"  https://portal.example.com   ":  "https://portal.example.com",
		"http://localhost:3000":            "http://localhost:3000",
		"https://portal.example.com:443/ ": "https://portal.example.com:443",
	}
	for in, want := range cases {
		got, err := normalizeOrigin(in)
		if err != nil {
			t.Errorf("normalizeOrigin(%q) = %v, want %q", in, err, want)
			continue
		}
		if got != want {
			t.Errorf("normalizeOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}

// The refusals are the point. A wildcard would make the allowlist decorative;
// a bare hostname is ambiguous about scheme, and guessing http for it would
// let a plaintext page frame a terminal deployed over TLS.
func TestNormalizeOriginRefusesAnythingLoose(t *testing.T) {
	for _, in := range []string{
		"", "   ", "*", "https://*.example.com", "*.example.com",
		"portal.example.com", "//portal.example.com",
		"ftp://portal.example.com", "javascript:alert(1)", "null",
		"https://portal.example.com/app", "https://portal.example.com?x=1",
		"https://user:pw@portal.example.com",
	} {
		if got, err := normalizeOrigin(in); err == nil {
			t.Errorf("normalizeOrigin(%q) = %q, want a refusal", in, got)
		}
	}
}

func TestParseEmbedOriginsSplitsAndDeduplicates(t *testing.T) {
	origins, problems := parseEmbedOrigins("https://a.example.com, https://b.example.com https://a.example.com\nnot-an-origin")
	if len(origins) != 2 {
		t.Fatalf("origins = %v, want two", origins)
	}
	if origins[0] != "https://a.example.com" || origins[1] != "https://b.example.com" {
		t.Errorf("origins = %v, want them in the order given", origins)
	}
	if len(problems) != 1 {
		t.Errorf("problems = %v, want the one unusable entry reported", problems)
	}
}

// An unparseable entry is dropped, never taken as permission for anything —
// a typo must not be able to widen the allowlist.
func TestAllowedEmbedOriginsIgnoresUnusableEntries(t *testing.T) {
	t.Setenv(embedOriginsEnv, "*, https://good.example.com, ftp://bad.example.com")
	origins := allowedEmbedOrigins()
	if len(origins) != 1 || origins[0] != "https://good.example.com" {
		t.Fatalf("origins = %v, want only the usable one", origins)
	}
	if isAllowedEmbedOrigin("*") || isAllowedEmbedOrigin("ftp://bad.example.com") {
		t.Error("a dropped entry is still matching")
	}
}

func TestIsAllowedEmbedOriginComparesSchemeHostAndPort(t *testing.T) {
	t.Setenv(embedOriginsEnv, "https://portal.example.com")

	if !isAllowedEmbedOrigin("https://portal.example.com") {
		t.Error("the configured origin does not match itself")
	}
	// Each of these is a different origin to a browser, so each must be a
	// different decision here.
	for _, other := range []string{
		"http://portal.example.com",
		"https://portal.example.com:8443",
		"https://evil.example.com",
		"https://portal.example.com.evil.test",
		"https://notportal.example.com",
	} {
		if isAllowedEmbedOrigin(other) {
			t.Errorf("%q matched an allowlist of only https://portal.example.com", other)
		}
	}
}

func securityHeaderFor(t *testing.T, header string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeadersMiddleware())
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	return w.Header().Get(header)
}

// With nothing configured the terminal frames nowhere but its own pages,
// which is what it did before embedding was configurable.
func TestFrameAncestorsIsSelfWhenEmbeddingIsOff(t *testing.T) {
	t.Setenv(embedOriginsEnv, "")
	if got := securityHeaderFor(t, "Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'self';") {
		t.Errorf("CSP = %q, want frame-ancestors 'self'", got)
	}
	if got := securityHeaderFor(t, "X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
}

// X-Frame-Options has SAMEORIGIN and nothing else, so a browser honouring
// both headers would refuse the frame the allowlist just permitted. It is
// left off rather than set to something that contradicts frame-ancestors.
func TestFrameAncestorsListsTheOriginsAndDropsXFrameOptions(t *testing.T) {
	t.Setenv(embedOriginsEnv, "https://portal.example.com https://b.example.com")

	csp := securityHeaderFor(t, "Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'self' https://portal.example.com https://b.example.com;") {
		t.Errorf("CSP = %q, want both origins in frame-ancestors", csp)
	}
	if got := securityHeaderFor(t, "X-Frame-Options"); got != "" {
		t.Errorf("X-Frame-Options = %q, want it absent so it cannot contradict frame-ancestors", got)
	}
}

func corsRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(EmbedCORSMiddleware())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.OPTIONS("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r
}

func corsRequest(r *gin.Engine, method, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/x", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestEmbedCORSAnswersAnAllowedOrigin(t *testing.T) {
	t.Setenv(embedOriginsEnv, "https://portal.example.com")
	r := corsRouter(t)

	w := corsRequest(r, http.MethodOptions, "https://portal.example.com")
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight: status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Authorization") {
		t.Errorf("Access-Control-Allow-Headers = %q, want Authorization — the API is bearer-authenticated", got)
	}
	// A cache that did not vary on Origin could serve one origin's permission
	// to another.
	if got := w.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, want Origin", got)
	}

	w = corsRequest(r, http.MethodGet, "https://portal.example.com")
	if w.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.example.com" {
		t.Errorf("GET Access-Control-Allow-Origin = %q", got)
	}
}

// This surface authenticates with a bearer token and never with the session
// cookie. Allowing credentials would let a page that merely happens to be on
// the list borrow a browser's open 3270Web session.
func TestEmbedCORSNeverAllowsCredentials(t *testing.T) {
	t.Setenv(embedOriginsEnv, "https://portal.example.com")
	r := corsRouter(t)

	for _, method := range []string{http.MethodOptions, http.MethodGet} {
		w := corsRequest(r, method, "https://portal.example.com")
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Errorf("%s Access-Control-Allow-Credentials = %q, want it absent", method, got)
		}
	}
}

func TestEmbedCORSGrantsNothingToAnUnlistedOrigin(t *testing.T) {
	t.Setenv(embedOriginsEnv, "https://portal.example.com")
	r := corsRouter(t)

	w := corsRequest(r, http.MethodOptions, "https://evil.example.com")
	if w.Code != http.StatusForbidden {
		t.Errorf("preflight from an unlisted origin: status = %d, want 403", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want nothing — the origin must never be reflected", got)
	}

	w = corsRequest(r, http.MethodGet, "https://evil.example.com")
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("GET Access-Control-Allow-Origin = %q, want nothing", got)
	}
}

// Nothing changes for a deployment that never embeds: no CORS headers at all,
// and the request is served exactly as before.
func TestEmbedCORSIsInertWhenEmbeddingIsOff(t *testing.T) {
	t.Setenv(embedOriginsEnv, "")
	r := corsRouter(t)

	w := corsRequest(r, http.MethodGet, "https://portal.example.com")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want the request served normally", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want nothing", got)
	}
}

func contextFor(t *testing.T, req *http.Request) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

// A framed terminal is a cross-site context: Lax means the session cookie is
// simply not sent and the frame shows the connect page forever. None is the
// only value that works, and browsers only honour it on a Secure cookie.
func TestSessionCookieBecomesSameSiteNoneOnlyWhenEmbeddingOverTLS(t *testing.T) {
	t.Setenv(embedOriginsEnv, "https://portal.example.com")
	t.Setenv(reqsec.TrustProxyHeadersEnv, "true")

	req := httptest.NewRequest(http.MethodGet, "/screen", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	mode, secure := sessionCookieSameSite(contextFor(t, req))
	if mode != http.SameSiteNoneMode || !secure {
		t.Errorf("behind a trusted TLS proxy: (%v, %v), want (None, secure)", mode, secure)
	}

	// Plain HTTP: None would be discarded by the browser, which is not an
	// improvement on Lax not being sent.
	plain := httptest.NewRequest(http.MethodGet, "/screen", nil)
	mode, secure = sessionCookieSameSite(contextFor(t, plain))
	if mode != http.SameSiteLaxMode || secure {
		t.Errorf("plain HTTP: (%v, %v), want (Lax, not secure)", mode, secure)
	}
}

// Embedding does not decide whether a forwarding header can be believed —
// TRUST_PROXY_HEADERS does. Configuring an embed origin must not quietly
// become a second way to turn proxy trust on, because the header is set by
// whoever sends the request.
func TestEmbeddingDoesNotImplyTrustingForwardedHeaders(t *testing.T) {
	t.Setenv(embedOriginsEnv, "https://portal.example.com")
	t.Setenv(reqsec.TrustProxyHeadersEnv, "")

	req := httptest.NewRequest(http.MethodGet, "/screen", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if requestIsSecure(contextFor(t, req)) {
		t.Error("X-Forwarded-Proto was believed without TRUST_PROXY_HEADERS")
	}
	// And so the cookie stays Lax, because None on a non-Secure cookie is a
	// cookie the browser discards.
	if mode, secure := sessionCookieSameSite(contextFor(t, req)); mode != http.SameSiteLaxMode || secure {
		t.Errorf("(%v, %v), want (Lax, not secure)", mode, secure)
	}
}

// The header itself is parsed by reqsec, which owns the chain rule and is
// tested there. What this asserts is that the embedding layer asks it rather
// than answering for itself.
func TestRequestIsSecureDefersToReqsec(t *testing.T) {
	t.Setenv(embedOriginsEnv, "https://portal.example.com")
	t.Setenv(reqsec.TrustProxyHeadersEnv, "true")

	for _, tc := range []struct {
		proto string
		want  bool
	}{
		{"https, http", true},
		{"http, https", false},
		{"", false},
	} {
		req := httptest.NewRequest(http.MethodGet, "/screen", nil)
		if tc.proto != "" {
			req.Header.Set("X-Forwarded-Proto", tc.proto)
		}
		if got := requestIsSecure(contextFor(t, req)); got != tc.want {
			t.Errorf("X-Forwarded-Proto %q: requestIsSecure = %v, want %v (reqsec.IsTLS = %v)",
				tc.proto, got, tc.want, reqsec.IsTLS(req))
		}
	}
}

// Embed mode is presentation only, which is why a query parameter anyone can
// add is an acceptable way to ask for it. What may frame the server is
// decided by EMBED_ORIGINS and enforced by the browser.
func TestEmbedRequestedFromQueryThenCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/screen?embed=1", nil)
	if !embedRequested(contextFor(t, req)) {
		t.Error("?embed=1 not honoured")
	}

	// /screen is reached by redirect from the connect form, and a query
	// parameter does not survive a redirect — the cookie is what keeps the
	// frame from gaining chrome the moment it connects.
	req = httptest.NewRequest(http.MethodGet, "/screen", nil)
	req.AddCookie(&http.Cookie{Name: embedModeCookieName, Value: "1"})
	if !embedRequested(contextFor(t, req)) {
		t.Error("the remembered choice was not honoured")
	}

	// An explicit ?embed=0 wins over the cookie, which is how a session gets
	// its chrome back without closing the tab.
	req = httptest.NewRequest(http.MethodGet, "/screen?embed=0", nil)
	req.AddCookie(&http.Cookie{Name: embedModeCookieName, Value: "1"})
	if embedRequested(contextFor(t, req)) {
		t.Error("?embed=0 did not override the remembered choice")
	}

	req = httptest.NewRequest(http.MethodGet, "/screen", nil)
	if embedRequested(contextFor(t, req)) {
		t.Error("embed mode without being asked for")
	}
}

func TestEmbedConfigHandlerReportsTheConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(embedOriginsEnv, "https://portal.example.com, nonsense")

	app := &App{}
	r := gin.New()
	r.GET("/embed/config", app.EmbedConfigHandler)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/embed/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"enabled":true`,
		"https://portal.example.com",
		"ignored", // the entry that could not be used is named
		"warning", // ... as is the reason a plain-HTTP frame will not work
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %s", want, body)
		}
	}
}

// A preflight is an OPTIONS request to a path whose GET or POST route the
// router would never match it against, so without a wildcard route of its own
// it 404s before any middleware runs — and the browser reports a CORS failure
// for a surface that is in fact configured correctly.
func TestAPIPreflightIsRoutedAndAnsweredWithoutAToken(t *testing.T) {
	_, r, sess, _ := scopedTestApp(t)
	t.Setenv(embedOriginsEnv, "https://portal.example.com")

	for _, path := range []string{
		"/api/v1/sessions",
		"/api/v1/tasks",
		"/api/v1/sessions/" + sess.ID + "/screen.json",
		"/api/v1/sessions/" + sess.ID + "/snapshots",
	} {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", "https://portal.example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("OPTIONS %s: status = %d, want 204", path, w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://portal.example.com" {
			t.Errorf("OPTIONS %s: Access-Control-Allow-Origin = %q", path, got)
		}
	}
}

// The preflight is answered without a token, which is the specification's
// doing, not a decision here — so the real request had better still need one.
func TestCORSGrantsNoAccessWithoutTheToken(t *testing.T) {
	_, r, _, _ := scopedTestApp(t)
	t.Setenv(embedOriginsEnv, "https://portal.example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Origin", "https://portal.example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — CORS is not authentication", w.Code)
	}
}

// setSessionCookie is tested through the header it actually emits, not
// through sessionCookieSameSite alone.
//
// That distinction is the whole point of this test. A merge once resolved
// setSessionCookie to call c.SetSameSite(http.SameSiteLaxMode) directly,
// leaving sessionCookieSameSite correct and uncalled. Every test above still
// passed, and embedding was silently dead: a framed terminal is a cross-site
// context, so a Lax cookie is never sent and the frame shows the connect page
// forever with nothing in any log to say why.
func TestSetSessionCookieEmitsTheSameSiteItDecided(t *testing.T) {
	gin.SetMode(gin.TestMode)

	issue := func(t *testing.T) string {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/screen", nil)
		c.Request.Header.Set("X-Forwarded-Proto", "https")
		setSessionCookie(c, sessionCookieName, "abc123")
		return w.Header().Get("Set-Cookie")
	}

	t.Run("embedding over a trusted TLS proxy", func(t *testing.T) {
		t.Setenv(embedOriginsEnv, "https://portal.example.com")
		t.Setenv(reqsec.TrustProxyHeadersEnv, "true")

		got := issue(t)
		if !strings.Contains(got, "SameSite=None") {
			t.Errorf("Set-Cookie = %q, want SameSite=None so a framed terminal keeps its session", got)
		}
		// None without Secure is a cookie the browser discards outright.
		if !strings.Contains(got, "Secure") {
			t.Errorf("Set-Cookie = %q, want Secure alongside SameSite=None", got)
		}
		if !strings.Contains(got, "HttpOnly") {
			t.Errorf("Set-Cookie = %q, want HttpOnly — embedding does not relax that", got)
		}
	})

	t.Run("no embedding configured", func(t *testing.T) {
		t.Setenv(embedOriginsEnv, "")
		t.Setenv(reqsec.TrustProxyHeadersEnv, "true")

		got := issue(t)
		if !strings.Contains(got, "SameSite=Lax") {
			t.Errorf("Set-Cookie = %q, want SameSite=Lax — the default deployment is unchanged", got)
		}
	})

	t.Run("embedding configured but the request is not secure", func(t *testing.T) {
		t.Setenv(embedOriginsEnv, "https://portal.example.com")
		t.Setenv(reqsec.TrustProxyHeadersEnv, "")

		got := issue(t)
		// None on a non-Secure cookie is discarded, so Lax — which at least
		// works for the unframed terminal — is the better of two bad options.
		if !strings.Contains(got, "SameSite=Lax") {
			t.Errorf("Set-Cookie = %q, want SameSite=Lax over plain HTTP", got)
		}
		if strings.Contains(got, "Secure") {
			t.Errorf("Set-Cookie = %q, want no Secure flag on a plain-HTTP request", got)
		}
	})
}
