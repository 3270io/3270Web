// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
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
			if exponent > 32 {
				exponent = 32
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

func loginLimitKeys(username, clientIP string) []string {
	return []string{"user:" + normaliseUsernameKey(username), "ip:" + clientIP}
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
