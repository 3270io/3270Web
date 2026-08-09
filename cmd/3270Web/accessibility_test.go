package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Conformance details that live in markup, pinned here because they are
// one-line attributes that no feature depends on — nothing breaks visibly
// when they go missing, and the next person to notice is a screen-reader
// user. See docs/accessibility.md.
//
// These read the templates as files rather than rendering them: the
// attributes are static markup, and asserting on the source is what makes a
// failure point at the line to fix.

func templateSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "web", "templates", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// WCAG 3.1.1 Language of Page. Without it a screen reader announces the
// interface in whatever voice it defaulted to, which for an English UI under
// a non-English default is unintelligible rather than merely wrong.
func TestTemplatesDeclareTheirLanguage(t *testing.T) {
	for _, name := range []string{"screen.html", "connect.html", "error.html"} {
		src := templateSource(t, name)
		if !strings.Contains(src, `<html lang=`) {
			t.Errorf("%s: <html> has no lang attribute (WCAG 3.1.1)", name)
		}
	}
}

// WCAG 2.4.1 Bypass Blocks, via the landmark technique. The terminal is what
// the page is for; a screen-reader user reaches it by landmark rather than by
// walking the header, session tabs and toolbar.
//
// There is deliberately no skip link — inside the terminal, Tab is 3270 field
// navigation rather than browser focus movement, so a link at the top of the
// document is somewhere a keyboard user never arrives. See
// docs/accessibility.md.
func TestTerminalCarriesTheMainLandmark(t *testing.T) {
	src := templateSource(t, "screen.html")
	if !strings.Contains(src, `id="terminal-main"`) || !strings.Contains(src, `role="main"`) {
		t.Error("screen.html: the terminal has no main landmark (WCAG 2.4.1)")
	}
	if !strings.Contains(src, `data-terminal-escape-hatch`) {
		t.Error("screen.html: the keyboard-trap escape hatch is gone (WCAG 2.1.2)")
	}
	if !strings.Contains(templateSource(t, "connect.html"), `role="main"`) {
		t.Error("connect.html: the connect form has no main landmark (WCAG 2.4.1)")
	}
}

// A collapsed disclosure whose controls stay in the tab order sends focus to
// things nobody can see. The recording and chaos panels have always used
// visibility:hidden for this; the terminal tools widget did not, and its seven
// controls sat off the side of the viewport, focusable.
func TestCollapsedPanelsLeaveTheTabOrder(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "static", "style.css"))
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	css := string(b)

	for _, selector := range []string{
		".recording-controls.is-collapsed .recording-controls-panel",
		".chaos-controls.is-collapsed .chaos-controls-panel",
		".terminal-tools-widget.is-collapsed .terminal-tools-panel",
	} {
		idx := strings.Index(css, selector)
		if idx < 0 {
			t.Errorf("%s: rule not found", selector)
			continue
		}
		end := strings.Index(css[idx:], "}")
		if end < 0 {
			t.Errorf("%s: rule not closed", selector)
			continue
		}
		if !strings.Contains(css[idx:idx+end], "visibility: hidden") {
			t.Errorf("%s: collapsed panel keeps its controls focusable; it needs visibility: hidden", selector)
		}
	}
}

// staticSource reads a browser script the same way templateSource reads a
// template: as text, so a failure names the line to fix.
func staticSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "web", "static", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// The terminal focus lock must stand down for any dialog, including ones it
// has never been told about.
//
// keyboard.js keeps a list of modal markers, and a dialog missing from it is
// treated as ordinary page furniture: pointerdown is preventDefault()ed and
// focus is dragged back to the terminal on click. On a desktop that is a
// swallowed click. On a phone it is worse and stranger — the native <select>
// picker never opens, and the focus landing in a screen field raises the
// software keyboard instead, so the dropdown reads as simply broken. Four
// dialogs had been missed, the AI provider one among them.
//
// Nothing fails when a new dialog is left off the list, which is why the
// generic sweep is the part that matters. Both are pinned: the sweep, and the
// attributes that make a dialog visible to it.
func TestTerminalFocusLockStandsDownForAnyDialog(t *testing.T) {
	keyboard := staticSource(t, "keyboard.js")

	if !strings.Contains(keyboard, `[role="dialog"][aria-modal="true"]`) {
		t.Error("keyboard.js isModalOpen() has no generic aria-modal sweep; every new dialog would have to be added to its list by hand, and nothing fails when one is not")
	}

	// The dialogs that were missing. Named individually so a regression points
	// at which one came back rather than at the sweep in general.
	for _, marker := range []string{
		"[data-ai-modal]",
		"[data-tasks-modal]",
		"[data-wizard-modal]",
	} {
		if !strings.Contains(keyboard, marker) {
			t.Errorf("keyboard.js does not recognise %s as a dialog; its controls would have pointerdown prevented and focus pulled back to the terminal", marker)
		}
	}

	// The sweep only sees a dialog that says it is one. ai-provider.js builds
	// its panel from a template literal, so the attributes have to be in the
	// source rather than added later.
	provider := staticSource(t, "ai-provider.js")
	for _, attr := range []string{`role="dialog"`, `aria-modal="true"`} {
		if !strings.Contains(provider, attr) {
			t.Errorf("ai-provider.js does not set %s on its panel, so the generic dialog sweep cannot see it", attr)
		}
	}
}
