// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"regexp"
	"strings"
	"testing"
)

// The session's controls used to be spread across a page header, a toolbar row
// and a floating panel over the terminal. They are one menu bar now, and the
// move was a relocation rather than a rewrite: every control is the same
// element it was, with the same data-attribute, because a dozen other modules
// find it by that attribute and the command palette drives the session by
// clicking it.
//
// Which is exactly why this list is worth having. A control dropped during a
// later tidy-up of the markup does not break a build or fail a handler test —
// it just quietly stops being reachable, and the module that owns it goes on
// working perfectly against an element that is no longer on the page.
func TestEveryControlSurvivedTheMoveIntoTheMenus(t *testing.T) {
	src := templateSource(t, "screen.html")

	controls := []string{
		// Session
		"data-profiles-open",
		"data-host-details-open",
		"data-reconnect",
		"data-print-screen",
		"data-disconnect-open",
		"data-session-new",
		// Terminal
		"data-find-open",
		"data-copy-screen",
		"data-history-open",
		"data-transfer-open",
		"data-keymap-open",
		// View
		"data-focus-toggle",
		"data-hotspots-toggle",
		"data-keypad-toggle",
		"data-terminal-size-slider",
		"data-terminal-size-up",
		"data-terminal-size-down",
		"data-terminal-fit",
		"data-terminal-zoom-reset",
		// Automation — recording
		"data-recording-start",
		"data-recording-stop",
		"data-workflow-trigger",
		"data-modal-open",
		"data-wizard-open",
		// Automation — chaos
		"data-chaos-start",
		"data-chaos-stop",
		"data-chaos-load",
		"data-chaos-resume",
		"data-chaos-extend",
		"data-chaos-hints-open",
		"data-chaos-map-open",
		"data-chaos-report",
		"data-chaos-export",
		"data-chaos-remove",
		// The right-hand cluster
		"data-tasks-open",
		"data-copilot-toggle",
		"data-command-palette-open",
		"data-chrome-toggle",
		"data-workspace-toggle",
		"data-settings-open",
		"data-logs-open",
		"data-about-open",
		// Playback transport, which stays out of the menus: Stop behind a
		// drop-down is a playback nobody can stop in a hurry.
		"data-playback-debug-controls",
		"data-playback-play-controls",
		"data-playback-pause-button",
	}

	for _, control := range controls {
		if !strings.Contains(src, control) {
			t.Errorf("screen.html: %s is gone — the control it names is now unreachable, "+
				"and whichever module binds to it is silently doing nothing", control)
		}
	}
}

// A trigger that does not say what it controls, or says it is expanded when it
// is not, is a drop-down a screen reader cannot describe.
func TestMenuTriggersDeclareTheirPanels(t *testing.T) {
	src := templateSource(t, "screen.html")

	triggerRe := regexp.MustCompile(`<button[^>]*data-app-menu-trigger[^>]*>`)
	triggers := triggerRe.FindAllString(src, -1)
	if len(triggers) < 4 {
		t.Fatalf("screen.html: found %d menu triggers, expected the four menus plus the account one", len(triggers))
	}

	controlsRe := regexp.MustCompile(`aria-controls="([^"]+)"`)
	for _, trigger := range triggers {
		if !strings.Contains(trigger, `aria-haspopup="true"`) {
			t.Errorf("menu trigger has no aria-haspopup: %s", trigger)
		}
		if !strings.Contains(trigger, `aria-expanded="false"`) {
			t.Errorf("menu trigger does not start collapsed: %s", trigger)
		}
		m := controlsRe.FindStringSubmatch(trigger)
		if m == nil {
			t.Errorf("menu trigger has no aria-controls: %s", trigger)
			continue
		}
		if !strings.Contains(src, `id="`+m[1]+`"`) {
			t.Errorf("menu trigger points at %q, which is not an element on the page", m[1])
		}
	}
}

// The bar is one strip of chrome, and the point of the redesign is that it
// stays one. The floating tools panel over the terminal and the second icon
// row under the tabs are the two things that must not come back.
func TestTheSessionCarriesOneControlSurface(t *testing.T) {
	src := templateSource(t, "screen.html")

	for _, gone := range []string{
		"data-terminal-tools-widget",
		"data-main-toolbar",
		"data-recording-toggle",
		"data-chaos-toggle",
	} {
		if strings.Contains(src, gone) {
			t.Errorf("screen.html: %s is back — the session had three control surfaces "+
				"(header, toolbar, floating panel) and the redesign left it with one", gone)
		}
	}
}
