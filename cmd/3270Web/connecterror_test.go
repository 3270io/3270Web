package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A refusal by policy has to survive as far as the connect page. All of these
// used to arrive there as the same sentence about checking the address and the
// TN3270 service, which sends somebody to look at the one thing that was not
// the problem.
func TestConnectErrorMessage_CarriesARefusalThrough(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "session cap",
			err:  refuse(errSessionLimit, "You already have the maximum of 6 sessions open. Close one before opening another."),
			want: "You already have the maximum of 6 sessions open. Close one before opening another.",
		},
		{
			name: "host not on the allowlist",
			err:  refuse(errHostNotAllowed, "This 3270Web instance is not permitted to connect to elsewhere.example:23."),
			want: "This 3270Web instance is not permitted to connect to elsewhere.example:23.",
		},
		{
			name: "rate limited",
			err:  refuse(errRateLimited, "Too many connection attempts; wait about 5s."),
			want: "Too many connection attempts; wait about 5s.",
		},
		{
			name: "a refusal reached through a wrapper",
			err:  fmt.Errorf("start session: %w", refuse(errSessionLimit, "You already have the maximum of 6 sessions open.")),
			want: "You already have the maximum of 6 sessions open.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := connectErrorMessage("mainframe.example:23", tt.err)
			if got != tt.want {
				t.Errorf("connectErrorMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A connection that genuinely did not work still gets the advice, because
// there the address and the service really are the things worth checking.
func TestConnectErrorMessage_UnexplainedFailureKeepsTheAdvice(t *testing.T) {
	got := connectErrorMessage("mainframe.example:23", errors.New("dial tcp: connection refused"))
	if !strings.Contains(got, "verify the address") {
		t.Errorf("connectErrorMessage() = %q, want the address advice", got)
	}
}

// The sample-app message names the app the picker named, not the internal
// target, and reads as one sentence rather than two half-capitalised ones.
func TestConnectErrorMessage_NamesTheSampleAppItStartedFrom(t *testing.T) {
	got := connectErrorMessage("sampleapp:app1:3271", errors.New("listen tcp 127.0.0.1:3271: address already in use"))

	if strings.Contains(got, "sampleapp:app1:3271") {
		t.Errorf("connectErrorMessage() = %q, want the app's name rather than the internal target", got)
	}
	if !strings.Contains(got, "3271") {
		t.Errorf("connectErrorMessage() = %q, want the port somebody chose to be named", got)
	}
	if strings.Contains(got, ". listen") {
		t.Errorf("connectErrorMessage() = %q, want the second sentence to start with a capital", got)
	}
}

func TestCapitaliseFirst(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"already in use", "Already in use"},
		{"Already in use", "Already in use"},
		{"already in use.", "Already in use"},
		{"3 sessions open", "3 sessions open"},
	}
	for _, tt := range tests {
		if got := capitaliseFirst(tt.in); got != tt.want {
			t.Errorf("capitaliseFirst(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The API answers "you have too many open" with a status that says so, rather
// than with the 502 that invites a client to try the same thing again.
func TestConnectFailureStatus_SessionLimitIsNotABadGateway(t *testing.T) {
	got := connectFailureStatus(refuse(errSessionLimit, "You already have the maximum of 6 sessions open."))
	if got != 429 {
		t.Errorf("connectFailureStatus(session limit) = %d, want 429", got)
	}
}

func TestSessionCountReadsAsEnglish(t *testing.T) {
	if got := sessionCount(1); got != "1 session" {
		t.Errorf("sessionCount(1) = %q, want %q", got, "1 session")
	}
	if got := sessionCount(6); got != "6 sessions" {
		t.Errorf("sessionCount(6) = %q, want %q", got, "6 sessions")
	}
}
