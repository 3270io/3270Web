// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/config"
	"github.com/jnnngs/3270Web/internal/host"
)

// Every bundled sample has a port of its own. Typing "sampleapp:app2" or
// choosing it from the picker with the port left alone used to mean 3271 for
// all of them, so a second sample collided with the first and the error said
// only that the port was taken.

func TestDefaultSampleAppPortIsPerApp(t *testing.T) {
	want := map[string]int{
		"petstore": 3271, "app1": 3272, "app2": 3273, "app3": 3274,
		"wordle": 3275, "tictactoe": 3276, "snake": 3277, "pong": 3278,
	}
	for id, port := range want {
		if got := defaultSampleAppPortFor(id); got != port {
			t.Errorf("defaultSampleAppPortFor(%q) = %d, want %d", id, got, port)
		}
	}
	if got := defaultSampleAppPortFor("no-such-app"); got != defaultSampleAppPort {
		t.Errorf("defaultSampleAppPortFor(unknown) = %d, want the fallback %d", got, defaultSampleAppPort)
	}
}

// The Saved-connections list and the typed form have to agree, or the same app
// is on one port from one door and another from the next.
func TestDefaultSampleAppPortMatchesThePresetList(t *testing.T) {
	for _, option := range sampleAppPresetOptions() {
		if got := defaultSampleAppPortFor(option.ID); got != option.Port {
			t.Errorf("%s: default port %d, but the saved-connections list offers %d", option.ID, got, option.Port)
		}
	}
}

// The connect page's picker is fed from here, so it can preselect the port.
func TestAvailableSampleAppsCarryTheirDefaultPort(t *testing.T) {
	for _, option := range availableSampleApps() {
		if option.Port != defaultSampleAppPortFor(option.ID) {
			t.Errorf("%s: option port %d, want %d", option.ID, option.Port, defaultSampleAppPortFor(option.ID))
		}
	}
}

func TestTypedSampleAppWithoutAPortGetsItsOwn(t *testing.T) {
	app := &App{Config: &config.Config{ExecPath: "/usr/bin"}}
	for id, port := range map[string]int{"app1": 3272, "app2": 3273, "pong": 3278} {
		h, err := app.newHostFor("sampleapp:"+id, nil)
		if err != nil {
			t.Fatalf("newHostFor(%q): %v", id, err)
		}
		sample, ok := h.(*host.GoSampleAppHost)
		if !ok {
			t.Fatalf("newHostFor(%q) = %T, want a sample app host", id, h)
		}
		if sample.Port != port {
			t.Errorf("sampleapp:%s without a port = %d, want %d", id, sample.Port, port)
		}
	}
}

// An explicit port is still honoured, whatever the app's own default is.
func TestTypedSampleAppKeepsAnExplicitPort(t *testing.T) {
	app := &App{Config: &config.Config{ExecPath: "/usr/bin"}}
	h, err := app.newHostFor("sampleapp:app2:3278", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.(*host.GoSampleAppHost).Port; got != 3278 {
		t.Errorf("sampleapp:app2:3278 = port %d, want 3278", got)
	}
}

// The pet store typed with no port keeps choosing a free one for itself; the
// "Try the sample terminal" button depends on that.
func TestTypedPetStoreStillChoosesItsOwnPort(t *testing.T) {
	app := &App{Config: &config.Config{ExecPath: "/usr/bin"}}
	h, err := app.newHostFor("sampleapp:petstore", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sample := h.(*host.GoSampleAppHost); !sample.AutoPort {
		t.Errorf("sampleapp:petstore is on fixed port %d, want an automatic one", sample.Port)
	}
}

// What the session records as its target is what Reconnect and the "last
// target" cookie are built from, so it has to be the port actually served.
func TestParseHostPortUsesTheSampleAppsOwnPort(t *testing.T) {
	if _, port := parseHostPort("sampleapp:app2"); port != 3273 {
		t.Errorf("parseHostPort(sampleapp:app2) port = %d, want 3273", port)
	}
	if _, port := parseHostPort("sampleapp:app2:3276"); port != 3276 {
		t.Errorf("parseHostPort(sampleapp:app2:3276) port = %d, want 3276", port)
	}
}

// A refusal because the port is held by a different app has to say what to do
// about it, and the suggestion has to be a port that would work.
func TestConnectErrorMessageForASamplePortHeldByAnotherApp(t *testing.T) {
	err := fmt.Errorf("failed to start host connection: %w",
		&host.SampleAppPortInUseError{Port: 3271, HeldBy: "app1", Requested: "app2"})
	got := connectErrorMessage("sampleapp:app2:3271", err)

	if !strings.Contains(got, "3271") {
		t.Errorf("message %q does not name the contested port", got)
	}
	if !strings.Contains(got, "Sample App 1") || !strings.Contains(got, "Sample App 2") {
		t.Errorf("message %q does not name the two apps by their picker titles", got)
	}
	if !strings.Contains(got, "3273") {
		t.Errorf("message %q does not name app2's own free port, 3273", got)
	}
	if !strings.Contains(strings.ToLower(got), "advanced") {
		t.Errorf("message %q does not say where to pick another port", got)
	}
	if strings.Contains(got, "sampleapp:app2:3271") {
		t.Errorf("message %q echoes the internal target", got)
	}
}

// An address held by something that is not one of our samples is also fixable.
func TestConnectErrorMessageForASamplePortHeldByAnotherProgram(t *testing.T) {
	got := connectErrorMessage("sampleapp:app1:3272", errors.New("listen tcp 127.0.0.1:3272: bind: address already in use"))
	if !strings.Contains(strings.ToLower(got), "another port") {
		t.Errorf("message %q does not tell the person to pick another port", got)
	}
}

func TestSuggestSampleAppPortSkipsPortsInUse(t *testing.T) {
	if got := suggestSampleAppPort("app2", 3273); got == 3273 || got == 0 {
		t.Errorf("suggestSampleAppPort(app2, busy 3273) = %d, want a different free port", got)
	}
}
