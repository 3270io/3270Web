package main

import (
	"strings"
	"testing"
	"time"
)

func newTestLimiter() (*loginLimiter, *time.Time) {
	l := newLoginLimiter()
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return clock }
	return l, &clock
}

func TestLimiterAllowsUpToTheFreeAllowance(t *testing.T) {
	l, _ := newTestLimiter()
	keys := loginLimitKeys("alice", "10.0.0.1")

	for i := 0; i < loginFreeAttempts; i++ {
		if ok, _ := l.Allow(keys...); !ok {
			t.Fatalf("locked out after %d failures, before the allowance was used", i)
		}
		l.RecordFailure(keys...)
	}

	ok, retry := l.Allow(keys...)
	if ok {
		t.Fatal("no lockout after the free allowance was exhausted")
	}
	if retry <= 0 {
		t.Errorf("retry-after = %v, want positive", retry)
	}
}

func TestLimiterBacksOffAndCaps(t *testing.T) {
	l, clock := newTestLimiter()
	keys := loginLimitKeys("alice", "10.0.0.1")

	var previous time.Duration
	for i := 0; i < loginFreeAttempts+12; i++ {
		l.RecordFailure(keys...)
		_, retry := l.Allow(keys...)
		if retry > loginMaxLockout {
			t.Fatalf("lockout %v exceeds the cap %v", retry, loginMaxLockout)
		}
		if retry > 0 && previous > 0 && retry < previous {
			t.Errorf("lockout shrank from %v to %v", previous, retry)
		}
		if retry > 0 {
			previous = retry
		}
	}
	if previous != loginMaxLockout {
		t.Errorf("lockout settled at %v, want the cap %v", previous, loginMaxLockout)
	}

	// Waiting out the lockout lets an attempt through again.
	*clock = clock.Add(loginMaxLockout + time.Second)
	if ok, _ := l.Allow(keys...); !ok {
		t.Error("still locked out after the lockout elapsed")
	}
}

func TestLimiterResetClearsCounters(t *testing.T) {
	l, _ := newTestLimiter()
	keys := loginLimitKeys("alice", "10.0.0.1")

	for i := 0; i < loginFreeAttempts+2; i++ {
		l.RecordFailure(keys...)
	}
	if ok, _ := l.Allow(keys...); ok {
		t.Fatal("expected a lockout")
	}

	l.Reset(keys...)
	if ok, _ := l.Allow(keys...); !ok {
		t.Error("Reset did not clear the lockout")
	}
}

// Address-only throttling lets a distributed attacker spread guesses at one
// account; username-only lets anyone lock a known account out. Both keys are
// checked, so either alone is enough to refuse.
func TestLimiterKeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter()

	// Exhaust alice's allowance from one address.
	for i := 0; i < loginFreeAttempts+2; i++ {
		l.RecordFailure(loginLimitKeys("alice", "10.0.0.1")...)
	}

	// alice is locked out even from a fresh address, because the username key
	// is shared.
	if ok, _ := l.Allow(loginLimitKeys("alice", "10.0.0.99")...); ok {
		t.Error("moving to a new address bypassed the username lockout")
	}
	// A different account from a clean address is unaffected.
	if ok, _ := l.Allow(loginLimitKeys("bob", "10.0.0.99")...); !ok {
		t.Error("an unrelated account was caught by another's lockout")
	}
	// A different account from the offending address is still refused.
	if ok, _ := l.Allow(loginLimitKeys("bob", "10.0.0.1")...); ok {
		t.Error("the offending address was not throttled for other usernames")
	}
}

// Case variation must not multiply the allowance.
func TestLimiterFoldsUsernameCase(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 0; i < loginFreeAttempts+2; i++ {
		l.RecordFailure(loginLimitKeys("alice", "10.0.0.1")...)
	}
	if ok, _ := l.Allow(loginLimitKeys("ALICE", "10.0.0.2")...); ok {
		t.Error("changing the case of the username bypassed the lockout")
	}
}

func TestLimiterIgnoresEmptyKeys(t *testing.T) {
	l, _ := newTestLimiter()
	if ok, _ := l.Allow("", ""); !ok {
		t.Error("empty keys produced a lockout")
	}
	l.RecordFailure("", "")
	if len(l.buckets) != 0 {
		t.Errorf("empty keys created %d buckets", len(l.buckets))
	}
}

// Buckets for quiet keys must be dropped, or the map grows with every address
// that ever mistyped a password.
func TestLimiterPrunesIdleBuckets(t *testing.T) {
	l, clock := newTestLimiter()
	l.RecordFailure(loginLimitKeys("alice", "10.0.0.1")...)
	if len(l.buckets) == 0 {
		t.Fatal("no buckets recorded")
	}

	*clock = clock.Add(loginAttemptTTL + time.Minute)
	l.RecordFailure(loginLimitKeys("bob", "10.0.0.2")...)

	for key := range l.buckets {
		if strings.Contains(key, "alice") || key == "ip:10.0.0.1" {
			t.Errorf("stale bucket %q survived pruning", key)
		}
	}
}

func TestLowerASCII(t *testing.T) {
	cases := map[string]string{
		"Alice": "alice", "ALICE": "alice", "alice": "alice",
		"": "", "A1-b_C": "a1-b_c",
		// Non-ASCII is left alone rather than mangled; it cannot reach a
		// username anyway, since ValidateUsername rejects it.
		"café": "café",
	}
	for in, want := range cases {
		if got := lowerASCII(in); got != want {
			t.Errorf("lowerASCII(%q) = %q, want %q", in, got, want)
		}
	}
}
