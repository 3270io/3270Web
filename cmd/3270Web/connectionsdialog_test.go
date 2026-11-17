// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"regexp"
	"strings"
	"testing"
)

// The saved-connections dialog used to do four jobs at once — open a profile,
// open a bundled sample app, dial a typed host, and edit the list — and wore
// three different titles depending on which button had opened it. It is two
// dialogs now: "Open a connection" for starting a session, "Manage
// connections" for editing what is saved.
//
// Nothing here is server logic. These are source contracts, in the same spirit
// as the menu-bar test beside them: the split is made of strings in a script
// and a template, and a later tidy-up that puts a control back in the wrong
// dialog breaks no build and fails no handler test — it just brings the
// overloaded dialog back one control at a time.

// jsFunctionBody returns the text of a two-space-indented function declared
// inside connection-profiles.js's wrapper, from its signature to its closing
// brace.
func jsFunctionBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "function "+name+"(")
	if start < 0 {
		t.Fatalf("connection-profiles.js: function %s is gone", name)
	}
	end := strings.Index(src[start:], "\n  }\n")
	if end < 0 {
		t.Fatalf("connection-profiles.js: could not find the end of %s", name)
	}
	return src[start : start+end]
}

func TestOpenAndManageAreTwoDialogsWithOneTitleEach(t *testing.T) {
	src := staticSource(t, "connection-profiles.js")
	open := jsFunctionBody(t, src, "buildOpenDialog")
	manage := jsFunctionBody(t, src, "buildManageDialog")

	if !strings.Contains(open, ">Open a connection</h3>") {
		t.Error(`the Open dialog is not titled exactly "Open a connection"`)
	}
	if !strings.Contains(manage, ">Manage connections</h3>") {
		t.Error(`the Manage dialog is not titled exactly "Manage connections"`)
	}
	// The three titles of the old dialog. A title chosen at open time is how
	// one dialog came to be called three things.
	for _, gone := range []string{"Open a saved connection", `"Saved connections"`, "data-profiles-title"} {
		if strings.Contains(src, gone) {
			t.Errorf("connection-profiles.js: %q is back — a dialog has one title, and it is fixed in its markup", gone)
		}
	}

	// Open is for starting a session and nothing else.
	for _, notHere := range []struct{ needle, why string }{
		{`name="sampleApp"`, "the sample-app selector that fills a profile in"},
		{"data-profiles-form", "the profile editor"},
		{"data-import-connections", "import and export"},
		{"data-profiles-delete-confirm", "the delete confirmation"},
	} {
		if strings.Contains(open, notHere.needle) {
			t.Errorf("the Open dialog carries %s (%q); it belongs in Manage", notHere.why, notHere.needle)
		}
	}
	for _, want := range []struct{ needle, why string }{
		{"data-profiles-list", "the saved connections"},
		{"data-profiles-sample-list", "the bundled sample apps, each with their port"},
		{"data-profiles-direct", "the typed host and its Connect button"},
		{"data-profiles-manage-link", "the hand-off to Manage"},
		{"Manage connections…", "the hand-off's words"},
	} {
		if !strings.Contains(open, want.needle) {
			t.Errorf("the Open dialog lost %s (%q)", want.why, want.needle)
		}
	}

	// Manage is for editing and nothing else: it opens nothing.
	for _, notHere := range []struct{ needle, why string }{
		{"data-profiles-samples", "the list of sample apps to open"},
		{"data-profiles-direct", "the typed-host box"},
	} {
		if strings.Contains(manage, notHere.needle) {
			t.Errorf("the Manage dialog carries %s (%q); it belongs in Open", notHere.why, notHere.needle)
		}
	}
	for _, want := range []struct{ needle, why string }{
		{"data-profiles-form", "the profile editor"},
		{`name="sampleApp"`, "the sample-app selector that fills host and port in"},
		{"data-import-connections", "import"},
		{"data-export-connections", "export"},
		{"data-profiles-delete-confirm", "the delete confirmation"},
		{"data-legacy-import", "the browser-list import"},
	} {
		if !strings.Contains(manage, want.needle) {
			t.Errorf("the Manage dialog lost %s (%q)", want.why, want.needle)
		}
	}
}

// Every way in says the name of the dialog it opens. The Session menu item and
// the connect page's button used to say "Saved connections" while the dialog
// said "Open a saved connection", and nothing said what the dialog was for.
func TestEveryEntryPointCarriesTheNameOfItsDialog(t *testing.T) {
	screen := templateSource(t, "screen.html")

	itemRe := func(hook string) *regexp.Regexp {
		return regexp.MustCompile(`(?s)<button[^>]*` + hook + `[^>]*>.*?</button>`)
	}
	open := itemRe("data-profiles-open").FindString(screen)
	manage := itemRe("data-profiles-manage").FindString(screen)
	if open == "" || manage == "" {
		t.Fatal("screen.html: the Session menu needs a data-profiles-open item and a data-profiles-manage item")
	}
	if !strings.Contains(open, "Open a connection…") {
		t.Error(`the Session menu item that opens the Open dialog is not labelled "Open a connection…"`)
	}
	if !strings.Contains(manage, "Manage connections…") {
		t.Error(`the Session menu item that opens the Manage dialog is not labelled "Manage connections…"`)
	}
	// Directly below, so the pair reads as one place.
	between := screen[strings.Index(screen, open)+len(open) : strings.Index(screen, manage)]
	if strings.Contains(between, "appmenu-item") {
		t.Error("another menu item sits between Open a connection… and Manage connections…")
	}
	if strings.Index(screen, manage) < strings.Index(screen, open) {
		t.Error("Manage connections… comes before Open a connection… in the Session menu")
	}

	connect := templateSource(t, "connect.html")
	button := itemRe("data-profiles-connect").FindString(connect)
	if !strings.Contains(button, "Open a connection…") {
		t.Errorf(`the connect page's button is not labelled "Open a connection…": %q`, button)
	}
	if strings.Contains(connect, ">Saved connections<") {
		t.Error(`connect.html still has a "Saved connections" button; the dialog it opens is called Open a connection`)
	}

	palette := staticSource(t, "ui-polish.js")
	for _, want := range []string{
		`label: "Open a connection…"`,
		`sel: "[data-profiles-open]"`,
		`label: "Manage connections…"`,
		`sel: "[data-profiles-manage]"`,
	} {
		if !strings.Contains(palette, want) {
			t.Errorf("ui-polish.js: the command palette has no entry matching %s", want)
		}
	}
}

// "+ New session" and Alt+N leave the choice of chooser to the tab bar, which
// sends an account that picks its host from the selection screen there. The
// Session menu's "Open a connection…" is named for the dialog, so it asks for
// the dialog outright: a menu item that opens something other than what its
// label says is the mismatch this split was meant to remove.
func TestTheMenuItemAsksForTheDialogByName(t *testing.T) {
	tabs := staticSource(t, "session-tabs.js")
	profiles := staticSource(t, "connection-profiles.js")

	if !strings.Contains(tabs, "openDialog: function") {
		t.Error("session-tabs.js no longer exposes openDialog, which the Session menu's item uses")
	}
	body := jsFunctionBody(t, tabs, "openNew")
	if !strings.Contains(body, "sessionManager && dialogOnly !== true") {
		t.Error("openNew no longer lets a caller skip the selection-screen rule and ask for the dialog")
	}
	if !strings.Contains(profiles, "tabs.openDialog()") {
		t.Error("the Session menu's Open a connection… does not ask the tab bar for the dialog")
	}
	// The tab bar's own button must not pass its click event in as the flag.
	if strings.Contains(tabs, `addEventListener("click", openNew)`) {
		t.Error("the + New session button hands openNew its click event as an argument")
	}
}

// A delete cannot be undone, so the confirmation opens on the safe button: a
// stray Enter or Space as it appears should cancel, not delete.
func TestDeletingAProfileOpensOnCancel(t *testing.T) {
	src := staticSource(t, "connection-profiles.js")
	body := jsFunctionBody(t, src, "openDeleteConfirm")

	if strings.Contains(body, "initialFocus: deleteConfirmAccept") {
		t.Error("the delete confirmation opens with focus on the destructive button")
	}
	if !strings.Contains(body, "initialFocus: deleteConfirmCancel") {
		t.Error("the delete confirmation does not name Cancel as its initial focus")
	}
}
