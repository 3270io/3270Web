// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/authz"
	"github.com/jnnngs/3270Web/internal/host"
)

// Starting a chaos exploration fills fields with generated values and presses
// keys, so against a real host it can create or change data. The terminal page
// therefore asks before it starts one — except for the bundled sample apps,
// which are demonstration screens with nothing behind them.
//
// Which side of that line a session is on is decided here, on the server,
// rather than by the browser reading the target string: the page has to be
// right about a host typed as "mock" or "demo" (which are the in-process pet
// store under another name) and about a pseudo-host that merely starts with
// "sampleapp:" but is served by somebody else's process.

func TestIsSampleAppSessionIsTrueOnlyForTheInProcessSamples(t *testing.T) {
	sample, err := host.NewGoSampleAppHost("petstore", 0, "/usr/bin/s3270", nil, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewGoSampleAppHost: %v", err)
	}
	mock, err := host.NewMockHost("")
	if err != nil {
		t.Fatalf("NewMockHost: %v", err)
	}
	cases := []struct {
		name string
		h    host.Host
		want bool
	}{
		{"a bundled sample app", sample, true},
		{"a real host reached through s3270", host.NewS3270("/usr/bin/s3270", "mainframe.example.com:23"), false},
		{"a mock host", mock, false},
		{"no host at all", nil, false},
	}
	for _, tc := range cases {
		if got := isSampleAppSession(tc.h); got != tc.want {
			t.Errorf("isSampleAppSession(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The mock/demo shorthands are the pet store under another name. They must be
// recognised by what actually serves them, not by their spelling.
func TestIsSampleAppSessionRecognisesTheShorthands(t *testing.T) {
	app := newApp(t.TempDir())
	for _, name := range []string{"mock", "demo", "sampleapp:petstore", "sampleapp:app1:3272"} {
		h, err := app.newHostFor(name, nil)
		if err != nil {
			t.Fatalf("newHostFor(%q): %v", name, err)
		}
		if !isSampleAppSession(h) {
			t.Errorf("%q is served in this process, but isSampleAppSession says it is an ordinary host", name)
		}
	}
	// An ordinary hostname, even one that looks sample-like, is not.
	h, err := app.newHostFor("sampleapp-lookalike.example.com:23", nil)
	if err != nil {
		t.Fatalf("newHostFor: %v", err)
	}
	if isSampleAppSession(h) {
		t.Error("a host that only resembles a sample app name was treated as one")
	}
}

// What the page carries is what the browser acts on. An ordinary host has to be
// marked so, because an attribute that is missing reads as "unknown" and the
// script treats unknown as "ask".
func TestScreenPageMarksAnOrdinaryHostForChaosConfirmation(t *testing.T) {
	mh, err := host.NewMockHost("")
	if err != nil {
		t.Fatal(err)
	}
	mh.Connected = true
	_, r, cookie := lostHostRouter(t, mh)

	w := getWithCookie(r, "/screen", cookie)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %.300s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `data-chaos-sample-app="false"`) {
		t.Errorf("the terminal page does not mark an ordinary host with data-chaos-sample-app=\"false\"")
	}
}

func TestScreenPageMarksABundledSampleAppSoChaosNeedsNoConfirmation(t *testing.T) {
	requireS3270(t)

	app, r := newAuthTestApp(t, "none")
	h, err := app.newHostFor("sampleapp:petstore", nil)
	if err != nil {
		t.Fatalf("newHostFor: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Skipf("could not start the sample app: %v", err)
	}
	defer h.Stop()
	sess := app.SessionManager.CreateSessionFor(authz.LocalUserID, h)
	sess.TargetHost, sess.TargetPort = "sampleapp:petstore", 0
	w := getWithCookie(r, "/screen", &http.Cookie{Name: sessionCookieName, Value: sess.ID})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %.300s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `data-chaos-sample-app="true"`) {
		t.Errorf("the terminal page does not mark a bundled sample app with data-chaos-sample-app=\"true\"")
	}
}

// The dialog is built once in the page and filled in by the script, so what
// matters is that the pieces the script reaches for exist and that it is a
// real modal dialog: the shared helper and the terminal's own key handling both
// find dialogs by role and aria-modal.
func TestChaosHostConfirmDialogIsInTheTerminalPage(t *testing.T) {
	src := templateSource(t, "screen.html")

	for _, want := range []string{
		"data-chaos-host-confirm-modal",
		"data-chaos-host-confirm-host",
		"data-chaos-host-confirm-blocked",
		"data-chaos-host-confirm-cancel",
		"data-chaos-host-confirm-ok",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("screen.html has no %s", want)
		}
	}
	i := strings.Index(src, "data-chaos-host-confirm-modal")
	if i < 0 {
		return
	}
	// The dialog's own opening markup: its backdrop line and the dialog line
	// after it.
	opening := src[i:min(len(src), i+400)]
	for _, want := range []string{`role="dialog"`, `aria-modal="true"`, `aria-labelledby=`, `aria-describedby=`} {
		if !strings.Contains(opening, want) {
			t.Errorf("the chaos host confirmation dialog lacks %s", want)
		}
	}
	for _, want := range []string{"Edit hints", "Blocked keys", "create or change data"} {
		if !strings.Contains(src, want) {
			t.Errorf("the chaos host confirmation text never mentions %q", want)
		}
	}
}
