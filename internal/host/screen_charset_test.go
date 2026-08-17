// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"strings"
	"testing"
)

// The terminal writes the screen in the local codeset, and a character outside
// ASCII is more than one byte of it: "c3a9" is é, "e282ac" is €. Reading only
// two-digit tokens meant every one of those was replaced with a null to keep
// the columns lined up — the geometry survived and the text did not, on every
// code page that has such a character in it, which is most of them outside the
// United States.
func TestScreenReadsMultiByteCharacters(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  rune
	}{
		{"pound sign", "c2a3", '£'},
		{"e acute", "c3a9", 'é'},
		{"euro sign", "e282ac", '€'},
		{"cyrillic de", "d094", 'Д'},
		{"cjk", "e4b8ad", '中'},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dump := "data: 41 " + tt.token + " 42\n" +
				"U F U C(127.0.0.1) I 2 1 3 0 0 0x0 0.000\nok"
			s, err := NewScreenFromDump(strings.NewReader(dump))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if s.Width != 3 || s.Height != 1 {
				t.Fatalf("screen is %dx%d, want 3x1 — one character is one cell however many bytes it takes", s.Width, s.Height)
			}
			if got := string(s.Buffer[0]); got != "A"+string(tt.want)+"B" {
				t.Errorf("row reads %q, want %q", got, "A"+string(tt.want)+"B")
			}
		})
	}
}

// A token that is not a character at all is still replaced with a null, because
// dropping it would shorten the row and shift every cell after it. That is what
// the resiliency test is about, and widening what counts as a character must
// not widen that.
func TestScreenStillPadsTokensThatAreNotCharacters(t *testing.T) {
	dump := "data: 41 ZZZZ 42\n" +
		"U F U C(127.0.0.1) I 2 1 3 0 0 0x0 0.000\nok"
	s, err := NewScreenFromDump(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Width != 3 {
		t.Fatalf("width %d, want 3", s.Width)
	}
	if s.Buffer[0][1] != 0 {
		t.Errorf("unreadable token became %q, want a null placeholder", s.Buffer[0][1])
	}
}

func TestParseScreenRune(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		want    rune
		wantErr bool
	}{
		{name: "ascii", token: "41", want: 'A'},
		{name: "single byte above ascii", token: "e9", want: 0xE9},
		{name: "utf-8 two byte", token: "c3a9", want: 'é'},
		{name: "utf-8 three byte", token: "e282ac", want: '€'},
		{name: "utf-8 four byte", token: "f09f9880", want: '\U0001F600'},
		// Not valid UTF-8, so it is read as the code point it spells instead —
		// a build that reports characters that way lands on the right one.
		{name: "bare code point", token: "20ac", want: '€'},
		{name: "not hex", token: "zzzz", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseScreenRune(tt.token)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseScreenRune(%q) = %q, want an error", tt.token, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseScreenRune(%q): %v", tt.token, err)
			}
			if got != tt.want {
				t.Errorf("parseScreenRune(%q) = %q (%U), want %q (%U)", tt.token, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestIsCharacterToken(t *testing.T) {
	for _, token := range []string{"41", "c3a9", "e282ac", "f09f9880"} {
		if !isCharacterToken(token) {
			t.Errorf("isCharacterToken(%q) = false, want true", token)
		}
	}
	for _, token := range []string{"", "4", "abc", "ZZ", "SF(c0=e0)"} {
		if isCharacterToken(token) {
			t.Errorf("isCharacterToken(%q) = true, want false", token)
		}
	}
}
