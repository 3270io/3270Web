// SPDX-License-Identifier: AGPL-3.0-or-later

package safepath

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateNameAccepts(t *testing.T) {
	for _, name := range []string{
		"run.json",
		"chaos-workflow.json",
		"a",
		"UPPER_and_lower-123.json",
		strings.Repeat("a", MaxNameLength),
	} {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}
}

// The traversal payloads here are the point of the package; if any of them
// ever validates, Join is handing a handler arbitrary filesystem write.
func TestValidateNameRejects(t *testing.T) {
	for _, name := range []string{
		"",
		"   ",
		".",
		"..",
		"../etc/passwd",
		"..\\windows\\system32",
		"/etc/passwd",
		"C:\\Windows\\System32\\drivers\\etc\\hosts",
		"dir/file.json",
		"dir\\file.json",
		".hidden",
		".tmp-1234",
		"file\x00.json",
		"file\n.json",
		"sp ace.json",
		"emoji\U0001F600.json",
		"caf\u00e9.json",
		strings.Repeat("a", MaxNameLength+1),
	} {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", name)
		}
	}
}

func TestJoinStaysInsideBase(t *testing.T) {
	base := t.TempDir()

	got, err := Join(base, "run.json")
	if err != nil {
		t.Fatalf("Join returned %v, want nil", err)
	}
	if want := filepath.Join(base, "run.json"); got != want {
		t.Errorf("Join = %q, want %q", got, want)
	}
}

func TestJoinRejectsTraversal(t *testing.T) {
	base := t.TempDir()

	for _, name := range []string{
		"../escape.json",
		"../../etc/passwd",
		"/etc/passwd",
		"sub/dir.json",
		"..",
	} {
		got, err := Join(base, name)
		if err == nil {
			t.Errorf("Join(base, %q) = %q, want error", name, got)
			continue
		}
		if got != "" {
			t.Errorf("Join(base, %q) returned path %q alongside error", name, got)
		}
	}
}

func TestJoinRequiresBase(t *testing.T) {
	if _, err := Join("", "run.json"); err == nil {
		t.Error("Join with empty base = nil, want error")
	}
}

func TestSanitizeName(t *testing.T) {
	const fallback = "chaos-workflow.json"

	tests := []struct {
		in   string
		want string
	}{
		{"run.json", "run.json"},
		{"/etc/passwd", "passwd"},
		{"../../etc/passwd", "passwd"},
		{"..\\..\\windows\\system32\\cmd.exe", "cmd.exe"},
		{"/home/user/.ssh/authorized_keys", "authorized_keys"},
		{"my run.json", "my_run.json"},
		{"caf\u00e9.json", "caf_.json"},
		{"", fallback},
		{"   ", fallback},
		{"..", fallback},
		{".", fallback},
		{"/", fallback},
		{".hidden", "hidden"},
		{"...", fallback},
	}

	for _, tt := range tests {
		got := SanitizeName(tt.in, fallback)
		if got != tt.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if err := ValidateName(got); err != nil {
			t.Errorf("SanitizeName(%q) returned invalid name %q: %v", tt.in, got, err)
		}
	}
}

// SanitizeName's contract is that its output always survives ValidateName, so
// callers can feed it straight into Join. Fuzzing is the cheapest way to keep
// that true as the character rules change.
func FuzzSanitizeNameAlwaysValid(f *testing.F) {
	for _, seed := range []string{
		"run.json", "../../etc/passwd", "", "..", "\x00", "a/b\\c", strings.Repeat("x", 500),
	} {
		f.Add(seed)
	}

	const fallback = "fallback.json"
	base := f.TempDir()

	f.Fuzz(func(t *testing.T, in string) {
		got := SanitizeName(in, fallback)
		if err := ValidateName(got); err != nil {
			t.Fatalf("SanitizeName(%q) = %q, which fails ValidateName: %v", in, got, err)
		}
		joined, err := Join(base, got)
		if err != nil {
			t.Fatalf("Join rejected sanitized name %q: %v", got, err)
		}
		if !strings.HasPrefix(joined, filepath.Clean(base)+string(filepath.Separator)) {
			t.Fatalf("Join(%q) escaped base: %q", got, joined)
		}
	})
}
