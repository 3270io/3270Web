// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"sync"
	"time"
)

// Login throttling parameters.
//
// Without TLS, credentials cross the network in the clear and an attacker on
// the segment does not need to guess. Throttling is aimed at the case they
// cannot see traffic and are simply trying passwords, where an unthrottled
// endpoint on a LAN is worth thousands of attempts a second.
const (
	// loginFreeAttempts is how many failures may occur before any delay is
	// imposed; the next attempt after this many is refused. Generous enough
	// to absorb ordinary mistyping.
	loginFreeAttempts = 5
	// loginBaseLockout is the first lockout, doubling with each further
	// failure.
	loginBaseLockout = 2 * time.Second
	// loginMaxLockout caps the doubling. Long enough to make bulk guessing
	// pointless, short enough that a locked-out person is not stuck for the
	// afternoon.
	loginMaxLockout = 15 * time.Minute
	// loginAttemptTTL forgets a quiet key, so an idle bucket does not hold
	// somebody's old failures against them forever.
	loginAttemptTTL = 1 * time.Hour
	// loginMaxLockoutShift clamps the doubling exponent. loginMaxLockout is
	// reached long before 2^32 times the base, so the clamp only has to keep
	// the shift well inside a 64-bit Duration.
	loginMaxLockoutShift = 32
)

// loginLimiter throttles failed logins per key.
//
// Two keys are checked for every attempt: the username and the client
// address. Username alone lets one attacker lock out a known account by
// failing on purpose; address alone lets a distributed attacker spread guesses
// across a single account. Requiring both to be clear covers each gap with the
// other, and only failures count, so ordinary use never accumulates.
type loginLimiter struct {
	mu      sync.Mutex
	buckets map[string]*loginBucket
	now     func() time.Time
}

type loginBucket struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		buckets: make(map[string]*loginBucket),
		now:     time.Now,
	}
}

// Allow reports whether an attempt may proceed, and if not, how long remains.
func (l *loginLimiter) Allow(keys ...string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	var longest time.Duration
	for _, key := range keys {
		if key == "" {
			continue
		}
		b, ok := l.buckets[key]
		if !ok {
			continue
		}
		if remaining := b.lockedUntil.Sub(now); remaining > longest {
			longest = remaining
		}
	}
	if longest > 0 {
		return false, longest
	}
	return true, 0
}

// RecordFailure counts a failed attempt against every key and extends the
// lockout once the free allowance is used up.
func (l *loginLimiter) RecordFailure(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.pruneLocked(now)

	for _, key := range keys {
		if key == "" {
			continue
		}
		b, ok := l.buckets[key]
		if !ok {
			b = &loginBucket{}
			l.buckets[key] = b
		}
		b.failures++
		b.lastSeen = now

		if b.failures >= loginFreeAttempts {
			// Shifting by an unbounded exponent is undefined for widths past
			// 63, so clamp before shifting rather than relying on the
			// overflow landing somewhere the cap below happens to catch.
			exponent := b.failures - loginFreeAttempts
			if exponent > loginMaxLockoutShift {
				exponent = loginMaxLockoutShift
			}
			lockout := loginBaseLockout << exponent
			if lockout > loginMaxLockout || lockout <= 0 {
				lockout = loginMaxLockout
			}
			b.lockedUntil = now.Add(lockout)
		}
	}
}

// Reset clears the counters for keys after a successful login.
func (l *loginLimiter) Reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		if key != "" {
			delete(l.buckets, key)
		}
	}
}

// pruneLocked drops buckets nobody has touched recently. Called from
// RecordFailure so the map is bounded by active failure sources rather than by
// every address that has ever mistyped a password.
func (l *loginLimiter) pruneLocked(now time.Time) {
	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) > loginAttemptTTL && now.After(b.lockedUntil) {
			delete(l.buckets, key)
		}
	}
}

// loginLimitKeys returns the buckets a failed attempt counts against, dropping
// the half whose value is empty.
//
// "user:" and "ip:" alone — the prefix without a value — are non-empty keys,
// so the limiter would accept them and create a bucket shared by every caller
// who presented the same empty half. One attacker repeatedly submitting an
// empty username (or arriving with no resolvable address) could then exhaust
// that shared bucket and lock out every other caller landing in it.
func loginLimitKeys(username, clientIP string) []string {
	var keys []string
	if u := normaliseUsernameKey(username); u != "" {
		keys = append(keys, "user:"+u)
	}
	if ip := strings.TrimSpace(clientIP); ip != "" {
		keys = append(keys, "ip:"+ip)
	}
	return keys
}

// normaliseUsernameKey folds case so "Alice" and "alice" share one bucket;
// otherwise case variation multiplies the free allowance.
func normaliseUsernameKey(username string) string {
	return lowerASCII(username)
}

func lowerASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + ('a' - 'A')
		}
	}
	return string(out)
}

// retryAfterSeconds is how many whole seconds to ask a client to wait: the
// remaining lockout rounded up, never zero, so retrying on the dot is not
// refused again.
func retryAfterSeconds(d time.Duration) int {
	seconds := int((d + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// retryAfterText says how long to wait the way somebody reads it, rounded up so
// that doing as it says is enough: "1 second", "5 minutes", "2 hours".
func retryAfterText(d time.Duration) string {
	seconds := retryAfterSeconds(d)
	if seconds < 60 {
		return countOf(seconds, "second")
	}
	minutes := (seconds + 59) / 60
	if minutes < 60 {
		return countOf(minutes, "minute")
	}
	return countOf((minutes+59)/60, "hour")
}
