// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Mechanical fixes from IMPECCABLE_AUDIT.md. Each assertion pins a one-line
// convention that nothing breaks visibly when it regresses, so a source check
// is the cheapest way to keep it. See IMPECCABLE_AUDIT.md for the findings.

var interWord = regexp.MustCompile(`\bInter\b`)

func rootFile(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(b)
}

// Finding 3: connect.html dialog titles sit under the page h1 as h2.
func TestConnectDialogTitlesUseH2(t *testing.T) {
	src := templateSource(t, "connect.html")
	for _, id := range []string{"settings-modal-title", "sample-app-title", "about-modal-title"} {
		if !strings.Contains(src, `<h2 id="`+id+`"`) {
			t.Errorf("connect.html: %s is not an h2 (finding 3)", id)
		}
		if strings.Contains(src, `<h3 id="`+id+`"`) {
			t.Errorf("connect.html: %s is still an h3 (finding 3)", id)
		}
	}
}

// Finding 1: the auth error alert uses the documented danger token, not the
// terminal's red, so every theme clears 4.5:1 on the tinted alert surface.
func TestAuthErrorAlertUsesDangerToken(t *testing.T) {
	src := staticSource(t, "auth.css")
	const marker = ".auth-alert--error {"
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatal("auth.css: .auth-alert--error rule is gone")
	}
	end := strings.Index(src[start:], "}")
	if end < 0 {
		t.Fatal("auth.css: unterminated .auth-alert--error rule")
	}
	block := src[start : start+end]
	if !strings.Contains(block, "var(--danger") {
		t.Error("auth.css: .auth-alert--error does not use --danger (finding 1)")
	}
	if strings.Contains(block, "var(--term-color-red") {
		t.Error("auth.css: .auth-alert--error still uses the terminal red (finding 1)")
	}
}

// Finding 2: placeholders use the muted token, which clears 4.5:1 on the
// sunken field in every built-in theme.
func TestPlaceholderUsesMutedToken(t *testing.T) {
	src := staticSource(t, "style.css")
	if !strings.Contains(src, "input::placeholder") {
		t.Error("style.css: no input::placeholder rule (finding 2)")
	}
	if !strings.Contains(src, "textarea::placeholder") {
		t.Error("style.css: no textarea::placeholder rule (finding 2)")
	}
}

// Finding 5: will-change is not declared on every button at rest.
func TestNoBlanketWillChangeOnButtons(t *testing.T) {
	src := staticSource(t, "ui-polish.css")
	const marker = "button,\n.h3270-key,\n.controls button {"
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatal("ui-polish.css: the shared control rule changed shape; update this test")
	}
	end := strings.Index(src[start:], "}")
	if end < 0 {
		t.Fatal("ui-polish.css: unterminated shared control rule")
	}
	if strings.Contains(src[start:start+end], "will-change") {
		t.Error("ui-polish.css: the shared button rule still promotes every control (finding 5)")
	}
}

// Decision 1: custom themes get the light-tone danger ink from the luminance
// of their background.
func TestCustomThemeGetsToneAwareDanger(t *testing.T) {
	if !strings.Contains(staticSource(t, "theme.js"), "dataset.customTone") {
		t.Error("theme.js: custom themes do not set a custom tone (decision 1)")
	}
	if !strings.Contains(staticSource(t, "ui-polish.css"), `[data-custom-tone="light"]`) {
		t.Error("ui-polish.css: no light custom-tone danger override (decision 1)")
	}
}

// Finding 8: the interface face is the system UI stack, not an unshipped Inter.
func TestInterfaceUsesSystemFontStack(t *testing.T) {
	for _, name := range []string{"style.css", "ui-polish.css"} {
		if strings.Contains(staticSource(t, name), `"Inter"`) {
			t.Errorf("%s: still names Inter, which is not shipped (finding 8)", name)
		}
	}
	if interWord.MatchString(rootFile(t, "DESIGN.md")) {
		t.Error("DESIGN.md: still names Inter (finding 8)")
	}
}

// Finding 12: the groups dialog says Groups once and keeps its JS hooks.
func TestGroupsDialogCopy(t *testing.T) {
	src := templateSource(t, "admin-users.html")
	for _, want := range []string{`>Group names<`, "Membership decides", ">manage groups</", `for="groups-input"`, "data-groups-name"} {
		if !strings.Contains(src, want) {
			t.Errorf("admin-users.html: groups dialog is missing %q (finding 12)", want)
		}
	}
}

// Findings 7 and 9: the documented scales and faces match what the CSS uses.
func TestDesignDocumentRecordsObservedScales(t *testing.T) {
	md := rootFile(t, "DESIGN.md")
	for _, want := range []string{"metric:", "scale:", "IBM 3270 Semi-Condensed", "SF Mono"} {
		if !strings.Contains(md, want) {
			t.Errorf("DESIGN.md: missing %q (findings 7 and 9)", want)
		}
	}

	var doc struct {
		Extensions struct {
			TypographyMeta map[string]json.RawMessage `json:"typographyMeta"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal([]byte(rootFile(t, ".impeccable", "design.json")), &doc); err != nil {
		t.Fatalf(".impeccable/design.json is not valid JSON: %v", err)
	}
	for _, key := range []string{"metric", "scale", "variants"} {
		if _, ok := doc.Extensions.TypographyMeta[key]; !ok {
			t.Errorf(".impeccable/design.json: typographyMeta has no %q (finding 7)", key)
		}
	}
}
