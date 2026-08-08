package host

import (
	"strings"
	"testing"
)

func TestParseSettingsReadsNameValueLines(t *testing.T) {
	got := parseSettings("monoCase: false\ncrossHair: true\n\nnoColonHere\n")
	if got["monoCase"] != "false" {
		t.Errorf("monoCase = %q, want false", got["monoCase"])
	}
	if got["crossHair"] != "true" {
		t.Errorf("crossHair = %q, want true", got["crossHair"])
	}
	if _, ok := got["noColonHere"]; ok {
		t.Error("a line without a colon is not a setting and should be skipped")
	}
}

// Builds differ on how they spell a boolean, and guessing at an unrecognised
// spelling would report a toggle as off when nobody said it was.
func TestParseToggleValueAcceptsEverySpellingAndNoOthers(t *testing.T) {
	for _, s := range []string{"true", "True", "set", "on", "1", "yes"} {
		if v, ok := parseToggleValue(s); !ok || !v {
			t.Errorf("parseToggleValue(%q) = (%v, %v), want (true, true)", s, v, ok)
		}
	}
	for _, s := range []string{"false", "clear", "off", "0", "no"} {
		if v, ok := parseToggleValue(s); !ok || v {
			t.Errorf("parseToggleValue(%q) = (%v, %v), want (false, true)", s, v, ok)
		}
	}
	for _, s := range []string{"", "maybe", "3"} {
		if _, ok := parseToggleValue(s); ok {
			t.Errorf("parseToggleValue(%q) reported a value it cannot know", s)
		}
	}
}

func TestCanonicalToggleNameIsCaseInsensitiveAndReturnsOurSpelling(t *testing.T) {
	got, ok := canonicalToggleName("MONOCASE")
	if !ok || got != "monoCase" {
		t.Errorf("canonicalToggleName(MONOCASE) = (%q, %v), want (monoCase, true)", got, ok)
	}
	if _, ok := canonicalToggleName(" crossHair "); !ok {
		t.Error("surrounding whitespace should not stop a name matching")
	}
}

// The allowlist is the whole of the defence here: Set() also reaches trace
// files, printer sessions and the proxy configuration. A name outside it must
// never resolve, however it is spelled.
func TestCanonicalToggleNameRefusesEverythingOutsideTheAllowlist(t *testing.T) {
	for _, name := range []string{
		"traceFile", "trace", "screenTrace", "printerLu", "proxy",
		"monoCase)", "monoCase,traceFile", "", "oversize",
	} {
		if got, ok := canonicalToggleName(name); ok {
			t.Errorf("canonicalToggleName(%q) resolved to %q; only display toggles may be settable", name, got)
		}
	}
}

func TestSafeToggleNamesIsSortedAndCoversTheAllowlist(t *testing.T) {
	names := SafeToggleNames()
	if len(names) != len(safeToggles) {
		t.Fatalf("SafeToggleNames() has %d entries, allowlist has %d", len(names), len(safeToggles))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("SafeToggleNames() is not sorted: %q before %q", names[i-1], names[i])
		}
	}
	for _, name := range names {
		if strings.TrimSpace(safeToggles[name]) == "" {
			t.Errorf("%q has no description; the list is shown to callers", name)
		}
	}
}
