package main

import (
	"context"
	"errors"
	"testing"
)

// isValidHostname refuses 127.0.0.1 and always has. It never refused
// "localhost", because "localhost" is not an IP literal and a syntax check has
// no way to find out what it is — s3270 resolved it a moment later and
// connected to whatever else is listening on this machine.
func TestConnectRefusesNamesThatResolveToLoopback(t *testing.T) {
	if !isValidHostname("localhost:3270") {
		t.Skip("the literal check already refuses this name; nothing left for the resolution check to catch")
	}
	err := checkHostResolves(context.Background(), "localhost:3270")
	if err == nil {
		t.Fatal("localhost was accepted as a mainframe address")
	}
	if !errors.Is(err, errHostNotAllowed) {
		t.Errorf("error = %v, want it to carry errHostNotAllowed so the caller gets 403 rather than a retryable 502", err)
	}
}

// The bundled sample apps are this process talking to itself on loopback, and
// they are reached through a pseudo-hostname that never went through the
// literal check either. Fencing them out here would break the demo.
func TestSampleAppsAreNotSubjectToTheResolutionCheck(t *testing.T) {
	if err := checkHostResolves(context.Background(), sampleAppHostname("app1")); err != nil {
		t.Errorf("the bundled sample app was refused: %v", err)
	}
}

// A name nothing can resolve is not a refusal: s3270 is about to fail on it
// with a better message, and reporting "forbidden" for "DNS is briefly down"
// sends an operator looking for an allowlist that has nothing to do with it.
func TestUnresolvableNamesAreLeftToTheConnection(t *testing.T) {
	if err := checkHostResolves(context.Background(), "no-such-host.invalid:3270"); err != nil {
		t.Errorf("an unresolvable name was refused by policy: %v", err)
	}
}

func TestIsExposedListenAddr(t *testing.T) {
	cases := map[string]bool{
		// Every interface, which is what the container image listens on.
		"0.0.0.0:3270": true,
		"[::]:3270":    true,
		// One interface, but not this machine's own.
		"192.168.1.10:3270": true,
		// Reachable from here and nowhere else.
		"127.0.0.1:3270": false,
		"[::1]:3270":     false,
		"localhost:3270": false,
	}

	for addr, want := range cases {
		if got := isExposedListenAddr(addr); got != want {
			t.Errorf("isExposedListenAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
