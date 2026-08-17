// SPDX-License-Identifier: AGPL-3.0-or-later

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

// templatePaths returns every template on disk.
//
// The tests below enumerate rather than list by name, and that is the whole
// point of them. The first version named the three pages that existed when it
// was written; sign-in, first-run setup, password change, account
// administration and the audit log all arrived afterwards, and every one of
// them landed with no lang attribute. A list only covers what somebody
// remembered to add to it. A glob covers the page nobody thought about — and
// the next one, which is the page this is really for.
func templatePaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "web", "templates", "*.html"))
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no templates found; the glob is wrong and this test proves nothing")
	}
	return paths
}

// eachPageTemplate calls fn for every template that is a page in its own
// right. A fragment with no <html> of its own — brand.html is one — inherits
// whatever includes it, so it has nothing to declare and nothing to land on.
func eachPageTemplate(t *testing.T, fn func(name, src string)) {
	t.Helper()
	pages := 0
	for _, path := range templatePaths(t) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Base(path), err)
		}
		src := string(b)
		if !strings.Contains(src, "<html") {
			continue
		}
		pages++
		fn(filepath.Base(path), src)
	}
	if pages == 0 {
		t.Fatal("no page templates found; this test proves nothing")
	}
}

// WCAG 3.1.1 Language of Page. Without it a screen reader announces the
// interface in whatever voice it defaulted to, which for an English UI under
// a non-English default is unintelligible rather than merely wrong.
func TestTemplatesDeclareTheirLanguage(t *testing.T) {
	eachPageTemplate(t, func(name, src string) {
		if !strings.Contains(src, `<html lang=`) {
			t.Errorf("%s: <html> has no lang attribute (WCAG 3.1.1)", name)
		}
	})
}

// WCAG 2.4.1, applied to every page rather than only the terminal: whatever
// somebody lands on needs a way past its header to the thing the page is for.
func TestEveryPageCarriesAMainLandmark(t *testing.T) {
	eachPageTemplate(t, func(name, src string) {
		if !strings.Contains(src, `role="main"`) && !strings.Contains(src, "<main") {
			t.Errorf("%s: no main landmark (WCAG 2.4.1)", name)
		}
	})
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

// A closed disclosure whose controls stay in the tab order sends focus to
// things nobody can see. The toolbar's collapsible sections used to need a
// visibility:hidden rule for this, and the floating tools widget was missed —
// its seven controls sat off the side of the viewport, focusable.
//
// The menu panels replaced all of it, and they close with the hidden
// attribute: display:none, out of the tab order, no stylesheet required. What
// is worth pinning is that they are rendered closed — a panel that ships open
// is one that a keyboard user tabs through on every page load.
func TestMenuPanelsAreRenderedClosed(t *testing.T) {
	src := templateSource(t, "screen.html")

	const marker = "data-app-menu-panel"
	panels := strings.Count(src, marker)
	if panels == 0 {
		t.Fatal("screen.html: no menu panels found; this test proves nothing")
	}

	for _, tag := range splitTags(src, marker) {
		if !strings.Contains(tag, " hidden") {
			t.Errorf("screen.html: a menu panel is not rendered closed, so its "+
				"controls are in the tab order from page load: %s", tag)
		}
	}
}

// splitTags returns the source of every tag containing marker.
func splitTags(src, marker string) []string {
	var out []string
	for rest := src; ; {
		idx := strings.Index(rest, marker)
		if idx < 0 {
			return out
		}
		start := strings.LastIndex(rest[:idx], "<")
		end := strings.Index(rest[idx:], ">")
		if start < 0 || end < 0 {
			return out
		}
		out = append(out, rest[start:idx+end+1])
		rest = rest[idx+end+1:]
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
