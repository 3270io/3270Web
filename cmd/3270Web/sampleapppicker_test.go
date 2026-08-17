// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// Pointing a session at a bundled sample app.
//
// The apps are addressed as "sampleapp:<id>" on a port that is not 3270.
// Neither half of that is written down anywhere an operator looks, so every
// place that asks "which host?" has to offer them by name instead of relying
// on somebody typing an address they were never given.

// The profile editor's picker is fed from here. Without this the only way to
// write a profile for a sample app is to know the pseudo-hostname.
func TestProfileListOffersTheBundledSampleApps(t *testing.T) {
	_, r := audienceTestApp(t)
	cookie := authCookieFrom(doLogin(t, r, "alice", loginTestPassword, "10.0.0.1"))
	if cookie == "" {
		t.Fatal("alice could not sign in")
	}

	w := getWithAuth(r, "/api/profiles", cookie, "10.0.0.1")
	if w.Code != http.StatusOK {
		t.Fatalf("/api/profiles returned %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		SampleApps []sampleAppPresetOption `json:"sampleApps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	want := sampleAppPresetOptions()
	if len(body.SampleApps) != len(want) {
		t.Fatalf("/api/profiles offers %d sample apps, want %d", len(body.SampleApps), len(want))
	}
	for i, got := range body.SampleApps {
		if got != want[i] {
			t.Errorf("sample app %d = %+v, want %+v", i, got, want[i])
		}
		// Every one of these is about to be saved as a profile, so each has to
		// survive the same validation an ordinary host does.
		p := ConnectionProfile{Name: got.Name, Host: got.Host, Port: got.Port}
		if err := validateProfile(&p); err != nil {
			t.Errorf("%s is offered but cannot be saved: %v", got.Name, err)
		}
	}
}

// The picker hands a sample app back as a bare "host:port" target rather than
// a profile name, and the tab bar opens the session with it. That target has
// to be one the connect path accepts.
func TestASampleAppTargetIsAcceptedAsATypedHost(t *testing.T) {
	for _, option := range sampleAppPresetOptions() {
		target := option.Host + ":" + strconv.Itoa(option.Port)
		id, port, ok := parseSampleAppHost(target)
		if !ok {
			t.Errorf("%q is not recognised as a sample app target", target)
			continue
		}
		if id != option.ID || port != option.Port {
			t.Errorf("%q parsed as %q on port %d, want %q on port %d", target, id, port, option.ID, option.Port)
		}
		if !isValidHostname(target) {
			t.Errorf("%q is refused by the hostname check", target)
		}
	}
}

// Opening a second session used to fall through to a window.prompt whenever
// there were no connection profiles yet — which is the state a fresh instance
// is in, and the state where the only thing to connect to is a sample app the
// prompt could not name. A prompt is also silently refused inside a sandboxed
// frame, so an embedded terminal had no second session at all.
func TestTheNewSessionPickerDoesNotFallBackToAPrompt(t *testing.T) {
	src := staticSource(t, "session-tabs.js")

	start := strings.Index(src, "function openNew(")
	if start < 0 {
		t.Fatal("session-tabs.js: openNew is gone")
	}
	end := strings.Index(src[start:], "\n  function promptForHost(")
	if end < 0 {
		t.Fatal("session-tabs.js: could not find the end of openNew")
	}
	body := src[start : start+end]

	if !strings.Contains(body, "picker.pick(") {
		t.Error("session-tabs.js: the new-session control no longer opens the picker")
	}
	if strings.Contains(body, "available.length") {
		t.Error("session-tabs.js: openNew is gating the picker on having profiles again — " +
			"an instance with none is exactly the one that needs the sample apps offered")
	}
	// The picker must still cope with a choice that has no profile behind it,
	// which is what a sample app and a typed address both are.
	if !strings.Contains(body, "choice.target") {
		t.Error("session-tabs.js: a choice with no profile name is dropped; " +
			"sample apps and typed addresses arrive that way")
	}
}

// The profile editor's own picker, and the pick-mode list of sample apps.
func TestTheProfileEditorOffersSampleAppsByName(t *testing.T) {
	src := staticSource(t, "connection-profiles.js")

	for _, want := range []struct {
		needle string
		why    string
	}{
		{`name="sampleApp"`, "the editor's sample-app dropdown"},
		{"data-profiles-sample-field", "the wrapper that hides the dropdown where there are no samples"},
		{"data-profiles-sample-list", "the pick-mode list of sample apps"},
		{"data-profiles-direct", "the typed-address box that replaced window.prompt"},
	} {
		if !strings.Contains(src, want.needle) {
			t.Errorf("connection-profiles.js: %s is gone (%q)", want.why, want.needle)
		}
	}

	// Choosing a sample app must clear TLS: they are plaintext listeners on
	// loopback, so a tick left behind from a previous edit only produces a
	// profile that cannot connect.
	apply := strings.Index(src, "function applySampleApp(")
	if apply < 0 {
		t.Fatal("connection-profiles.js: applySampleApp is gone")
	}
	tail := src[apply:]
	if end := strings.Index(tail, "\n  }"); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "elements.tls.checked = false") {
		t.Error("connection-profiles.js: choosing a sample app leaves TLS ticked, " +
			"which cannot connect to a plaintext loopback listener")
	}
}
