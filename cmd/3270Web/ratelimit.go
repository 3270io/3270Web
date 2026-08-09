package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/reqsec"
)

// Rate limits on the handful of routes where one caller can consume something
// the whole instance shares.
//
// Not a general request limiter. Reading a screen is cheap and people do it
// constantly; the routes below each start a subprocess, press keys at a
// mainframe unattended, move a file, or spend an upstream quota. Limiting
// those and nothing else keeps the rule explainable, which is what stops it
// being switched off wholesale the first time it inconveniences somebody.
type rateCategory string

const (
	rateConnect  rateCategory = "connect"
	rateChaos    rateCategory = "chaos"
	rateTransfer rateCategory = "transfer"
	rateAI       rateCategory = "ai"
	// rateAIControl covers the AI control plane — signing in, polling that
	// sign-in, listing models, naming the endpoint those go to. None of it is
	// a completion, so none of it belongs under the chat allowance, and all of
	// it makes this server fetch from somewhere the caller nominated. That is
	// the part that needs a limit: the response comes back into this process's
	// memory, and how large it is was chosen at the other end.
	rateAIControl rateCategory = "aicontrol"
)

// Defaults, in requests per minute per caller. Set generously: these are
// meant to stop a runaway loop or a deliberate flood, not to pace ordinary
// work, and a limit an honest user meets is a limit that gets removed.
//
// Each can be overridden with RATE_LIMIT_<CATEGORY>; 0 turns one off.
var rateDefaults = map[rateCategory]int{
	rateConnect:  20,
	rateChaos:    10,
	rateTransfer: 20,
	rateAI:       60,
	// Higher than the others because one of these routes is a poll: the
	// device-login flow asks GitHub every few seconds whether the code has
	// been entered yet, for up to fifteen minutes. Generous enough never to
	// interrupt that, small enough that a flood cannot hold thousands of
	// upstream responses in memory at once.
	rateAIControl: 120,
}

// rateLimitedRoutes names the routes each category covers.
//
// Keyed by method and Gin's route pattern rather than the request path, so
// "/api/v1/sessions/:id/chaos/start" is one entry rather than one per session.
// TestRateLimitedRoutesExist walks the router and fails if an entry here
// matches nothing — otherwise renaming a route would quietly remove its limit,
// and nothing about the running system would look different.
var rateLimitedRoutes = map[string]rateCategory{
	"POST /chaos/start":                      rateChaos,
	"POST /api/v1/sessions/:id/chaos/start":  rateChaos,
	"POST /chaos/resume":                     rateChaos,
	"POST /api/v1/sessions/:id/chaos/resume": rateChaos,
	"POST /transfer/send":                    rateTransfer,
	"POST /transfer/receive":                 rateTransfer,
	"POST /api/ai/chat":                      rateAI,
	"POST /api/copilot/chat":                 rateAI,

	// The control plane. Every one of these makes this server issue a request
	// to an origin the caller chose and read the answer into memory, so they
	// are limited for the same reason the routes above are: one caller can
	// spend something the whole instance shares.
	"POST /api/copilot/login/start": rateAIControl,
	"POST /api/copilot/login/poll":  rateAIControl,
	"POST /api/copilot/enterprise":  rateAIControl,
	"GET /api/copilot/models":       rateAIControl,
	"POST /api/ai/config":           rateAIControl,
	"GET /api/ai/models":            rateAIControl,
}

// errRateLimited marks a refusal for rate rather than for anything about the
// request, so a caller gets 429 and a Retry-After instead of a status that
// invites an immediate retry.
var errRateLimited = errors.New("rate limited")

// rateLimitFor reads the configured allowance for a category.
func rateLimitFor(category rateCategory) int {
	return envInt("RATE_LIMIT_"+strings.ToUpper(string(category)), rateDefaults[category])
}

// rateKey identifies the caller a limit applies to.
//
// The account where there is one, so a person cannot get a fresh allowance by
// opening another tab or moving to another address. Falling back to the
// address covers the single-operator case, where every request is the same
// principal and per-account limiting would be one bucket for the instance.
func rateKey(c *gin.Context, category rateCategory) string {
	who := principalFrom(c).UserID
	// The single operator is one synthetic identity shared by everything, so
	// keying on it would give the whole instance one bucket. There the address
	// is the closest thing to a caller.
	if who == "" || who == authz.LocalUserID {
		who = reqsec.ClientIP(c.Request)
	}
	return string(category) + "|" + who
}

// RateLimit throttles the routes listed in rateLimitedRoutes.
//
// Installed once as engine middleware rather than attached route by route.
// Some of these routes are registered inside other packages, and a limiter
// that can only cover the ones registered here would cover them unevenly —
// which reads as "limited" while leaving the busiest path open.
func (app *App) RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		category, limited := rateLimitedRoutes[c.Request.Method+" "+c.FullPath()]
		if !limited {
			c.Next()
			return
		}
		perMinute := rateLimitFor(category)
		if perMinute <= 0 {
			c.Next()
			return
		}

		ok, retryIn := app.rateLimiter().Allow(rateKey(c, category), perMinute)
		if !ok {
			seconds := int(retryIn.Seconds()) + 1
			c.Header("Retry-After", fmt.Sprintf("%d", seconds))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": fmt.Sprintf(
					"too many %s requests; wait about %ds. The limit is %d a minute (RATE_LIMIT_%s).",
					category, seconds, perMinute, strings.ToUpper(string(category))),
			})
			return
		}
		c.Next()
	}
}

// rateLimiter lazily builds the shared limiter.
func (app *App) rateLimiter() *rateLimiter {
	app.rateOnce.Do(func() {
		if app.rates == nil {
			app.rates = newRateLimiter()
		}
	})
	return app.rates
}

// rateLimiter is a token bucket per caller and category.
//
// A bucket rather than a counted window because a window resets on a
// boundary: a caller who spends their allowance at 10:59:59 gets a fresh one
// a second later, so the real short-term rate is double the configured one.
// A bucket refills continuously and has no boundary to sit on.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	now     func() time.Time
	// lastSweep bounds the map. Buckets are keyed by account or address, so
	// without this a long-running instance accumulates one per caller it has
	// ever seen.
	lastSweep time.Time
}

type rateBucket struct {
	tokens   float64
	lastFill time.Time
}

const (
	rateSweepEvery = 10 * time.Minute
	rateBucketTTL  = 1 * time.Hour
)

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: make(map[string]*rateBucket), now: time.Now}
}

// Allow spends a token, reporting how long until the next one if the bucket
// is empty.
func (l *rateLimiter) Allow(key string, perMinute int) (bool, time.Duration) {
	if perMinute <= 0 {
		return true, 0
	}
	capacity := float64(perMinute)
	perSecond := capacity / 60

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweepLocked(now)

	bucket, ok := l.buckets[key]
	if !ok {
		// A caller starts with a full bucket, so a first request is never the
		// one that is refused.
		bucket = &rateBucket{tokens: capacity, lastFill: now}
		l.buckets[key] = bucket
	}

	if elapsed := now.Sub(bucket.lastFill); elapsed > 0 {
		bucket.tokens += elapsed.Seconds() * perSecond
		if bucket.tokens > capacity {
			bucket.tokens = capacity
		}
		bucket.lastFill = now
	}

	if bucket.tokens < 1 {
		// How long until one token is available, which is what a caller can
		// act on — rather than how long until the bucket is full.
		wait := time.Duration((1 - bucket.tokens) / perSecond * float64(time.Second))
		return false, wait
	}
	bucket.tokens--
	return true, 0
}

// sweepLocked drops buckets nobody has touched for a while. A bucket that has
// refilled to full is indistinguishable from a fresh one, so forgetting it
// costs a caller nothing.
func (l *rateLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < rateSweepEvery {
		return
	}
	l.lastSweep = now
	for key, bucket := range l.buckets {
		if now.Sub(bucket.lastFill) > rateBucketTTL {
			delete(l.buckets, key)
		}
	}
}
