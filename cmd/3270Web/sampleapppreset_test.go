// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"

	"github.com/jnnngs/3270Web/internal/host"
)

// A host preset may name one of the bundled sample apps.
//
// That is not a curiosity: an instance being evaluated, or one being taught
// on, has the sample apps and no mainframe, and a host list assigned to a
// group is exactly as useful there. The catch is that a sample app is not an
// address to dial — it is a TN3270 server this process starts on loopback —
// so every path that turns a target into a connection has to know that.
//
// One of them did not. The session-selection screen assembled s3270 arguments
// itself, so a sample-app preset was drawn on the menu, offered to whoever the
// group reached, and failed on selection with a hostname s3270 could not
// resolve. Assigning it as the *only* host worked, because that path went
// through startHostSessionWithProfile, which did know. The two disagreed, and
// which one you met depended on how many hosts your group had.

// newHostFor is now the single answer, so these assert on it rather than on
// one caller: what has to hold is that a sample-app target produces a sample
// app host whichever path asked.
func TestASampleAppPresetBuildsASampleAppHost(t *testing.T) {
	app := newApp(t.TempDir())

	profile := ConnectionProfile{Name: "Demo", Host: "sampleapp:app1"}
	if err := validateProfile(&profile); err != nil {
		t.Fatalf("validateProfile: %v", err)
	}

	built, err := app.newHostFor(profile.displayTarget(), &profile)
	if err != nil {
		t.Fatalf("newHostFor(%q): %v", profile.displayTarget(), err)
	}
	if _, ok := built.(*host.GoSampleAppHost); !ok {
		t.Fatalf("newHostFor built a %T, want the bundled sample app host — "+
			"a plain s3270 would be handed %q as a hostname and fail to resolve it",
			built, profile.Host)
	}
}

// The ordinary case is unchanged: a real mainframe still gets a plain s3270
// carrying the profile's own target.
func TestAMainframePresetStillBuildsAPlainS3270(t *testing.T) {
	app := newApp(t.TempDir())

	profile := ConnectionProfile{Name: "TSO", Host: "mainframe.example", Port: 992, TLS: true}
	if err := validateProfile(&profile); err != nil {
		t.Fatalf("validateProfile: %v", err)
	}

	built, err := app.newHostFor(profile.displayTarget(), &profile)
	if err != nil {
		t.Fatalf("newHostFor: %v", err)
	}
	s3270, ok := built.(*host.S3270)
	if !ok {
		t.Fatalf("newHostFor built a %T, want a plain s3270", built)
	}
	// The profile's own target, prefixes and all — not the display form.
	if got := strings.Join(s3270.Args, " "); !strings.Contains(got, "L:mainframe.example:992") {
		t.Errorf("args = %q, want the profile's s3270 target", got)
	}
}

// The three sample apps are offered on separate ports, so publishing all of
// them does not produce three presets fighting over one listener.
func TestSampleAppPresetOptionsAreEachConnectable(t *testing.T) {
	options := sampleAppPresetOptions()
	if len(options) != len(sampleAppConfigs) {
		t.Fatalf("got %d options for %d sample apps", len(options), len(sampleAppConfigs))
	}

	seen := make(map[int]string, len(options))
	for _, option := range options {
		if !isAllowedSampleAppPort(option.Port) {
			t.Errorf("%s is offered on port %d, which no sample app may listen on",
				option.Name, option.Port)
		}
		if previous, clash := seen[option.Port]; clash {
			t.Errorf("%s and %s are both offered on port %d", previous, option.Name, option.Port)
		}
		seen[option.Port] = option.Name

		// And what the option fills in has to be a preset the validator takes.
		profile := ConnectionProfile{Name: option.Name, Host: option.Host, Port: option.Port}
		if err := validateProfile(&profile); err != nil {
			t.Errorf("the preset %s fills in is refused: %v", option.Name, err)
		}
	}
}

func TestSampleAppPresetLabelNamesTheApp(t *testing.T) {
	label := sampleAppPresetLabel(ConnectionProfile{Host: "sampleapp:app1", Port: 3271})
	if label == "" || !strings.Contains(label, "Sample App 1") {
		t.Errorf("sampleAppPresetLabel = %q, want the name it was chosen by", label)
	}
	if got := sampleAppPresetLabel(ConnectionProfile{Host: "mainframe.example", Port: 23}); got != "" {
		t.Errorf("sampleAppPresetLabel(mainframe) = %q, want empty so the caller shows the address", got)
	}
}
