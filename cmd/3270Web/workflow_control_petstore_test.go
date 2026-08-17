// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/jnnngs/3270Web/internal/config"
	"github.com/jnnngs/3270Web/internal/session"
)

// A recording that decides, run against a real terminal and a real
// application rather than against a mock screen.
//
// The unit tests beside this one check the engine: which branch an If takes,
// where an EndWhile goes back to, what stops a run. What they cannot check is
// the part that only exists when there is a host on the other end — that a
// condition reads the screen the host has actually drawn *now*, after the key
// the previous step pressed, rather than the one cached before it. That is the
// failure mode a loop would hide: a condition answered against a stale screen
// loops forever or leaves immediately, and both look plausible in a mock.
func TestPlayWorkflowDecidesAgainstThePetStore(t *testing.T) {
	requireS3270(t)

	execPath := resolveS3270Path("")
	h, err := newSampleAppHost(defaultSampleAppID, 3278, execPath, config.S3270Options{Model: "3"})
	if err != nil {
		t.Fatalf("could not build the sample app host: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Skipf("could not start the sample app: %v", err)
	}
	defer h.Stop()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := h.UpdateScreen(); err == nil {
			if screen := h.GetScreenSnapshot(); screen != nil && strings.Contains(screen.Text(), "PET010") {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}

	sess := &session.Session{Host: h}
	app := &App{}
	// Sign on, and keep signing on while the sign-on panel is still the one
	// on the display. A recording could not say that before: it had to press
	// Enter a fixed number of times and hope.
	app.playWorkflow(sess, &WorkflowConfig{Steps: []session.WorkflowStep{
		{Type: "SetVariable", Variable: "user", Text: "ADMIN"},
		{
			Type:          "While",
			Coordinates:   &session.WorkflowCoordinates{Row: 1, Column: 2, Length: 6},
			Operator:      "equals",
			Text:          "PET010",
			MaxIterations: 3,
		},
		{Type: "FillString", Coordinates: &session.WorkflowCoordinates{Row: 11, Column: 21}, Text: "${user}"},
		{Type: "FillString", Coordinates: &session.WorkflowCoordinates{Row: 12, Column: 21}, Text: "${user}"},
		{Type: "PressEnter"},
		{Type: "EndWhile"},
		{
			Type:        "SetVariable",
			Variable:    "panel",
			Coordinates: &session.WorkflowCoordinates{Row: 1, Column: 2, Length: 6},
		},
	}})

	var events []string
	withSessionLock(sess, func() {
		for _, ev := range sess.PlaybackEvents {
			events = append(events, ev.Message)
		}
	})
	log := strings.Join(events, "\n")

	vars := playbackVariablesSnapshot(sess)
	if vars["user"] != "ADMIN" {
		t.Fatalf("the literal variable did not survive the run: %#v", vars)
	}
	if vars["panel"] == "" || vars["panel"] == "PET010" {
		screen := h.GetScreenSnapshot()
		text := ""
		if screen != nil {
			text = screen.Text()
		}
		t.Fatalf("the loop did not sign on: panel=%q\nevents:\n%s\nscreen:\n%s", vars["panel"], log, text)
	}
	if !strings.Contains(log, "pass 1") {
		t.Fatalf("expected the run log to count the loop's passes:\n%s", log)
	}
	if !strings.Contains(log, "leaving the loop") {
		t.Fatalf("expected the loop to end on its condition rather than its limit:\n%s", log)
	}
	if !strings.Contains(log, "Playback completed") {
		t.Fatalf("expected the run to complete:\n%s", log)
	}
}
