// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/chaos"
	"github.com/jnnngs/3270Web/internal/config"
)

// The default sample application, driven the way 3270Web itself drives it:
// through newSampleAppHost, which is what the connect form, the "mock" and
// "demo" shorthands and the headless API all end up calling.
//
// The unit tests for the pet store live beside it in internal/sampleapps and
// check its screens. What is being checked here is the wiring: that the
// default really is the pet store, that a session opened against it reaches a
// usable first screen, and that the terminal model configured for this
// instance is the one the application ends up drawing for.

func TestTheDefaultSampleAppIsThePetStore(t *testing.T) {
	if defaultSampleAppID != "petstore" {
		t.Errorf("defaultSampleAppID = %q, want the pet store", defaultSampleAppID)
	}
	options := availableSampleApps()
	if len(options) == 0 {
		t.Fatal("no sample apps are offered")
	}
	// The picker offers the default first, because the first entry is what a
	// browser select box shows without anybody choosing anything.
	if options[0].ID != defaultSampleAppID {
		t.Errorf("the sample app picker offers %q first, want %q", options[0].ID, defaultSampleAppID)
	}
	if options[0].Hostname != "sampleapp:petstore" {
		t.Errorf("default sample hostname = %q", options[0].Hostname)
	}
	if _, ok := sampleAppConfig(defaultSampleAppID); !ok {
		t.Error("the default sample app has no configuration entry")
	}
	// The shorthands have to survive the validator, or the connect form
	// refuses the one target somebody with no mainframe can reach.
	for _, hostname := range []string{"mock", "demo", "sampleapp:petstore", "sampleapp:petstore:3271"} {
		if err := checkHostAllowed(hostname); err != nil {
			t.Errorf("checkHostAllowed(%q) = %v", hostname, err)
		}
	}
	if !isValidHostname("sampleapp:petstore") {
		t.Error("sampleapp:petstore is not a valid hostname")
	}
}

func TestDefaultSampleAppServesItsFirstScreen(t *testing.T) {
	requireS3270(t)

	execPath := resolveS3270Path("")
	// A port of its own: these tests each run a listener, and the sample app
	// ports are a small fixed set shared with anything else on the machine.
	h, err := newSampleAppHost(defaultSampleAppID, 3272, execPath, config.S3270Options{Model: "3"})
	if err != nil {
		t.Fatalf("could not build the default sample app host: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Skipf("could not start the default sample app: %v", err)
	}
	defer h.Stop()

	deadline := time.Now().Add(15 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		if err := h.UpdateScreen(); err == nil {
			if screen := h.GetScreenSnapshot(); screen != nil {
				text = screen.Text()
				if strings.Contains(text, "PET010") {
					break
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}

	for _, want := range []string{"PET010", "PAWS AND CLAWS PET STORE", "User id", "Password"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the first screen does not contain %q:\n%s", want, text)
		}
	}

	// Model 3 was asked for, so model 3 is what the application should be
	// drawing for. This is the whole of the model plumbing in one assertion:
	// the configured model reaches s3270, s3270 negotiates it, and the sample
	// app uses what it negotiated.
	if !strings.Contains(text, "MODEL 3 (32x80)") {
		t.Errorf("the sample app did not report the configured model:\n%s", text)
	}
	if screen := h.GetScreenSnapshot(); screen != nil && screen.Height != 32 {
		t.Errorf("screen height = %d, want 32 rows for a model 3 terminal", screen.Height)
	}
}

// The reason the pet store is the default is that it gives the rest of
// 3270Web something to work on. This drives the real chaos engine against it
// and insists that a short run finds a screen graph rather than one screen it
// cannot get off — which is what the single-screen samples give it.
func TestChaosExploresThePetStore(t *testing.T) {
	requireS3270(t)

	execPath := resolveS3270Path("")
	h, err := newSampleAppHost(defaultSampleAppID, 3273, execPath, config.S3270Options{Model: "3"})
	if err != nil {
		t.Fatalf("could not build the default sample app host: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Skipf("could not start the default sample app: %v", err)
	}
	defer h.Stop()

	cfg := chaos.DefaultConfig()
	cfg.MaxSteps = 120
	cfg.TimeBudget = 60 * time.Second
	cfg.StepDelay = 0
	// A fixed seed so a failure here is reproducible rather than a coin toss.
	cfg.Seed = 20260809

	engine := chaos.New(h, cfg)
	if err := engine.Start(); err != nil {
		t.Fatalf("chaos would not start: %v", err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for engine.Active() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	engine.Stop()

	status := engine.Status()
	if status.Error != "" {
		t.Fatalf("chaos ended with an error: %s", status.Error)
	}
	if status.StepsRun == 0 {
		t.Fatal("chaos ran no steps")
	}
	// Six is well short of the twenty-six screens the application has, and
	// well beyond anything a run could reach by accident on a single-screen
	// application. It is deliberately a floor, not a target.
	if status.UniqueScreens < 6 {
		t.Errorf("chaos found %d unique screens in %d steps, want at least 6",
			status.UniqueScreens, status.StepsRun)
	}
	if status.Transitions == 0 {
		t.Error("chaos recorded no transitions between screens")
	}

	// And the run has to be exportable as a workflow, since that is what the
	// exploration is for.
	data, err := engine.ExportWorkflow("sampleapp:petstore", 3273)
	if err != nil {
		t.Fatalf("the discovered run would not export: %v", err)
	}
	if !strings.Contains(string(data), "\"Steps\"") {
		t.Errorf("the exported workflow has no steps:\n%s", string(data))
	}
}
