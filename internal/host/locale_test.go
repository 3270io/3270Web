package host

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A server that inherits no locale runs the terminal in the "C" codeset, which
// is ASCII, and every character outside it is turned into a question mark
// inside the terminal before this program sees it. That is the normal state of
// a container image, not an unusual one, and nothing downstream can undo it.
func TestS3270EnvironmentPinsTheCodesetWhenNothingElseDoes(t *testing.T) {
	got := s3270Environment([]string{"PATH=/usr/bin", "HOME=/root"})
	if !slices.Contains(got, "LC_ALL="+utf8Locale) {
		t.Errorf("environment %v does not pin the codeset", got)
	}
	for _, keep := range []string{"PATH=/usr/bin", "HOME=/root"} {
		if !slices.Contains(got, keep) {
			t.Errorf("environment lost %q", keep)
		}
	}
}

// A deployment that chose a locale keeps it. This is here for the deployment
// that chose none, and overriding a considered choice would be a different and
// less welcome thing.
func TestS3270EnvironmentLeavesAUTF8LocaleAlone(t *testing.T) {
	tests := [][]string{
		{"LANG=en_GB.UTF-8"},
		{"LC_ALL=C.utf8"},
		{"LC_CTYPE=de_DE.UTF-8", "LANG=C"},
	}
	for _, env := range tests {
		got := s3270Environment(env)
		if len(got) != len(env) {
			t.Errorf("s3270Environment(%v) = %v, want it unchanged", env, got)
		}
	}
}

// LC_ALL outranks the rest, so a deployment that set it to something non-UTF-8
// has to be overridden rather than appended to — two LC_ALL entries would leave
// which one wins up to the C library.
func TestS3270EnvironmentReplacesANonUTF8LCAll(t *testing.T) {
	got := s3270Environment([]string{"LC_ALL=POSIX", "LANG=en_GB.UTF-8"})
	count := 0
	for _, entry := range got {
		if entry == "LC_ALL=POSIX" {
			t.Error("a non-UTF-8 LC_ALL survived, and it outranks everything else")
		}
		if entry == "LC_ALL="+utf8Locale {
			count++
		}
	}
	if count != 1 {
		t.Errorf("environment has %d pinned LC_ALL entries, want exactly 1", count)
	}
}

func TestEffectiveCodesetPrecedence(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want bool
	}{
		{"nothing set", []string{"PATH=/usr/bin"}, false},
		{"LANG only", []string{"LANG=C.UTF-8"}, true},
		{"LC_ALL beats LANG", []string{"LC_ALL=C", "LANG=en_GB.UTF-8"}, false},
		{"LC_CTYPE beats LANG", []string{"LC_CTYPE=C.UTF-8", "LANG=C"}, true},
		{"empty value is not a choice", []string{"LC_ALL=", "LANG=C.UTF-8"}, true},
		{"non-utf8 locale", []string{"LANG=en_GB.ISO-8859-1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveCodesetIsUTF8(tt.env); got != tt.want {
				t.Errorf("effectiveCodesetIsUTF8(%v) = %v, want %v", tt.env, got, tt.want)
			}
		})
	}
}

// The environment cannot help on a platform with no C.UTF-8 to be given. The
// terminal's own option can, but naming one an older build does not know makes
// it refuse to start — so the build is asked rather than assumed.
func TestSupportsUTF8FlagAsksTheBinary(t *testing.T) {
	// A program that runs but says nothing about the option answers no, which
	// is the safe half of the answer.
	if SupportsUTF8Flag(os.Args[0]) {
		t.Error("the test binary does not document a codeset option; it should not be given one")
	}
	if SupportsUTF8Flag("") {
		t.Error("an empty path cannot be asked anything")
	}
	if SupportsUTF8Flag(filepath.Join(t.TempDir(), "not-a-binary")) {
		t.Error("a path with nothing at it cannot be asked anything")
	}
}

func TestContainsFlagMatchesWholeOptions(t *testing.T) {
	help := "Usage: s3270 [options] [host]\n  -trace\n  -utf8\n     Force local codeset to be UTF-8\n"
	if !containsFlag(help, "-utf8") {
		t.Error("the option is in the help and was not found")
	}
	if containsFlag(help, "-utf") {
		t.Error("a prefix of an option matched it")
	}
	if containsFlag("  -utf8bogus\n", "-utf8") {
		t.Error("a longer option starting the same way matched")
	}
}

// The host name is expected last, so the option goes first — and asking twice
// for the same thing is not asking twice.
func TestWithUTF8FlagLeavesTheHostLast(t *testing.T) {
	args := []string{"-model", "2", "mainframe:992"}
	got := withUTF8Flag(os.Args[0], args)
	if got[len(got)-1] != "mainframe:992" {
		t.Errorf("args end with %q, want the host name", got[len(got)-1])
	}
}
