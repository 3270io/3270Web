// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"
)

// Pressing a key on the on-screen keyboard must not move the page.
//
// The keyboard sits below the terminal, so an operator using it has usually
// scrolled down to reach it. Two things then have to hold, or the keyboard
// walks off the screen under their finger:
//
//   - A key press must not take focus off the field being typed into. The
//     focus lock refuses the pointer default for exactly this reason, and
//     refusing it on pointerdown alone is not enough: a browser that fires the
//     compatibility mousedown anyway moves focus to the button regardless.
//   - When focus does have to be put back — a control the lock does not cover,
//     a browser that ignored both refusals — putting it back must not scroll.
//     focus() scrolls its target into view by default, and the target is a
//     field in the terminal above, so the keyboard goes off the bottom of the
//     display.
//
// Shift is where this was noticed and where it hurts most, because it is the
// one key that changes nothing on the screen: the scroll buys the operator
// nothing and costs them the keyboard they were halfway through using.
//
// Asserted against the source because the behaviour is one option on one call.
// Nothing fails when it is dropped, and the next person to notice is an
// operator on a small display.
func TestTerminalFocusRestoreDoesNotScrollThePage(t *testing.T) {
	keyboard := staticSource(t, "keyboard.js")

	if !strings.Contains(keyboard, "preventScroll: true") {
		t.Error("keyboard.js restores terminal focus with a plain focus(), which scrolls the field into view and takes the on-screen keyboard off the display")
	}

	// The two ways focus comes back to the terminal after the operator has
	// touched something else. Both are bookkeeping, and neither is a request
	// to go anywhere.
	for _, call := range []string{
		"focusQuietly(target)",
		"focusQuietly(shell)",
	} {
		if !strings.Contains(keyboard, call) {
			t.Errorf("keyboard.js does not use %s; that focus restore will scroll the page away from whatever the operator was looking at", call)
		}
	}
}

// The focus lock has to refuse the pointer default on both events.
//
// preventDefault on pointerdown asks the browser not to fire the compatibility
// mouse events that would move focus. Where that is honoured, mousedown never
// arrives and the second listener costs nothing. Where it is not, mousedown is
// the guard that still works — and without it a keypad button takes focus, the
// field loses it, and the click handler has to drag focus back across the page.
func TestTerminalFocusLockRefusesMouseDownAsWellAsPointerDown(t *testing.T) {
	keyboard := staticSource(t, "keyboard.js")

	lock := keyboard[strings.Index(keyboard, "function installTerminalFocusLock"):]
	if lock == keyboard {
		t.Fatal("keyboard.js has no installTerminalFocusLock; this test is pinned to the wrong thing")
	}
	end := strings.Index(lock, "\n  function findInputAtCursor")
	if end > 0 {
		lock = lock[:end]
	}

	for _, event := range []string{`"pointerdown"`, `"mousedown"`} {
		if !strings.Contains(lock, event) {
			t.Errorf("installTerminalFocusLock does not listen for %s; a keypad press can take focus off the field the operator is typing into", event)
		}
	}
}
