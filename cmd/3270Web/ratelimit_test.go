package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
)

func TestRateLimiterSpendsAndRefills(t *testing.T) {
	l := newRateLimiter()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	// A caller starts with a full bucket, so the first request is never the
	// one that is refused.
	for i := 0; i < 6; i++ {
		if ok, _ := l.Allow("k", 6); !ok {
			t.Fatalf("request %d was refused while the bucket should still hold tokens", i+1)
		}
	}
	ok, retryIn := l.Allow("k", 6)
	if ok {
		t.Fatal("a seventh request was allowed against a limit of six")
	}
	if retryIn <= 0 || retryIn > 11*time.Second {
		t.Errorf("retryIn = %v, want about ten seconds", retryIn)
	}

	// Ten seconds is one token at six a minute.
	now = now.Add(10 * time.Second)
	if ok, _ := l.Allow("k", 6); !ok {
		t.Error("the bucket did not refill")
	}
	if ok, _ := l.Allow("k", 6); ok {
		t.Error("the refill produced more than the one token that was due")
	}

	// It refills to the limit and no further, so an idle hour does not buy a
	// burst of sixty.
	now = now.Add(time.Hour)
	for i := 0; i < 6; i++ {
		if ok, _ := l.Allow("k", 6); !ok {
			t.Fatalf("request %d after a long idle period was refused", i+1)
		}
	}
	if ok, _ := l.Allow("k", 6); ok {
		t.Error("the bucket refilled past its capacity")
	}
}

// A counted window resets on a boundary, so a caller who spends everything
// just before it gets a second allowance a moment later — twice the
// configured rate. A bucket has no boundary to sit on.
func TestRateLimiterHasNoWindowBoundary(t *testing.T) {
	l := newRateLimiter()
	now := time.Date(2026, 1, 1, 12, 0, 59, 0, time.UTC)
	l.now = func() time.Time { return now }

	for i := 0; i < 10; i++ {
		l.Allow("k", 10)
	}
	now = now.Add(time.Second) // over a minute boundary
	if ok, _ := l.Allow("k", 10); ok {
		t.Error("crossing a minute boundary handed out a fresh allowance")
	}
}

func TestRateLimiterSeparatesCallers(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 3; i++ {
		l.Allow("alice", 3)
	}
	if ok, _ := l.Allow("alice", 3); ok {
		t.Fatal("alice was not limited")
	}
	if ok, _ := l.Allow("bob", 3); !ok {
		t.Error("bob was limited by alice's requests")
	}
}

func TestRateLimiterZeroDisables(t *testing.T) {
	l := newRateLimiter()
	for i := 0; i < 100; i++ {
		if ok, _ := l.Allow("k", 0); !ok {
			t.Fatal("a limit of zero refused a request; it should mean off")
		}
	}
}

// Buckets are keyed by caller, so without a sweep a long-running instance
// accumulates one for every account and address it has ever seen.
func TestRateLimiterForgetsIdleCallers(t *testing.T) {
	l := newRateLimiter()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	l.Allow("old", 10)
	now = now.Add(2 * time.Hour)
	l.Allow("new", 10)

	l.mu.Lock()
	_, stillThere := l.buckets["old"]
	count := len(l.buckets)
	l.mu.Unlock()
	if stillThere {
		t.Error("an idle bucket was kept")
	}
	if count != 1 {
		t.Errorf("%d buckets remain, want only the active one", count)
	}
}

// A limit that names routes is a limit that goes stale: rename a route and it
// silently stops applying, with nothing about the running system looking
// different. This walks the real router instead.
func TestRateLimitedRoutesExist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(authz.ModeEnv, "none")
	r, err := buildRouter(newApp(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}

	registered := map[string]bool{}
	for _, route := range r.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for entry := range rateLimitedRoutes {
		if !registered[entry] {
			t.Errorf("%q is rate-limited but is not a route; the limit applies to nothing", entry)
		}
	}
}

// The middleware has to actually refuse, with something a client can act on.
func TestRateLimitMiddlewareRefusesWithRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("RATE_LIMIT_CHAOS", "2")
	app := &App{authMode: authz.ModeNone}

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(principalContextKey, authz.Local()) })
	r.Use(app.RateLimit())
	r.POST("/chaos/start", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/screen/key", func(c *gin.Context) { c.Status(http.StatusOK) })

	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "10.0.0.9:5000"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	for i := 0; i < 2; i++ {
		if w := post("/chaos/start"); w.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i+1, w.Code)
		}
	}
	w := post("/chaos/start")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After header, so a client has nothing to wait on")
	}
	if !strings.Contains(w.Body.String(), "RATE_LIMIT_CHAOS") {
		t.Errorf("the refusal does not name the setting: %s", w.Body.String())
	}

	// Everything else is untouched. Reading a screen is cheap and constant,
	// and limiting it would be the change that gets the whole thing removed.
	for i := 0; i < 50; i++ {
		if w := post("/screen/key"); w.Code != http.StatusOK {
			t.Fatalf("an unlimited route was throttled at request %d", i+1)
		}
	}
}

// Two accounts must not share an allowance, or one busy person throttles
// everybody.
func TestRateLimitIsPerAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("RATE_LIMIT_CHAOS", "1")
	app := &App{authMode: authz.ModeLocal}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(principalContextKey, userPrincipal(c.GetHeader("X-Test-User")))
	})
	r.Use(app.RateLimit())
	r.POST("/chaos/start", func(c *gin.Context) { c.Status(http.StatusOK) })

	post := func(user string) int {
		req := httptest.NewRequest(http.MethodPost, "/chaos/start", nil)
		req.Header.Set("X-Test-User", user)
		req.RemoteAddr = "10.0.0.9:5000" // the same address for both
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := post("alice"); code != http.StatusOK {
		t.Fatalf("alice's first request = %d", code)
	}
	if code := post("alice"); code != http.StatusTooManyRequests {
		t.Errorf("alice's second request = %d, want 429", code)
	}
	// Same address, different account.
	if code := post("bob"); code != http.StatusOK {
		t.Errorf("bob was throttled by alice's request: %d", code)
	}
}

// Opening sessions is limited at the function every path funnels through
// rather than on the routes, because there are three of them and each starts
// a subprocess.
func TestConnectRateLimitAppliesAtTheChokepoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("RATE_LIMIT_CONNECT", "1")
	t.Setenv("MAX_SESSIONS_PER_USER", "50")
	app, _ := newAuthTestApp(t, "local")
	ctx := asPrincipal(userPrincipal("aaaa1111"))

	// The first attempt spends the allowance. It fails to dial — nothing is
	// listening — which is fine; the token is spent either way.
	_, _ = app.startHostSession(ctx, "mainframe.example.test:23")

	_, err := app.startHostSession(ctx, "mainframe.example.test:23")
	if err == nil {
		t.Fatal("a second connection attempt was allowed against a limit of one")
	}
	if !strings.Contains(err.Error(), "too many connection attempts") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}
