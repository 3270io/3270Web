package host

import (
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
