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
