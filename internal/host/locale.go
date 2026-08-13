package host

import "strings"

// The terminal reads and writes the screen in the *local* codeset — whatever
// the C library says the environment asked for — and not in the host code page.
// Those are different things: the host code page decides which EBCDIC byte is a
// pound sign, and the local codeset decides how the terminal spells that pound
// sign back to us.
//
// A server process usually inherits no locale at all. A container image is the
// normal case rather than the awkward one: nothing sets LANG, so the codeset is
// "C", which is ASCII, and every character outside it is replaced with a
// question mark *inside the terminal* before this program ever sees it. A UK
// screen loses its pound signs, a German one its umlauts, a Russian one every
// word. Nothing downstream can recover them, because by then they are literally
// question marks.
//
// So the subprocess is given a UTF-8 codeset explicitly rather than being left
// to inherit whatever the deployment happened to have. A locale the platform
// does not know falls back to "C", which is exactly where it would have been
// without this, so the worst case is the old behaviour rather than a new one.

// utf8Locale is the codeset-only locale asked for. It is the one spelling that
// needs no locale data installed on systems that have it built in, which is
// what a minimal container image has.
const utf8Locale = "C.UTF-8"

// localeVars are the environment variables that decide the codeset, in the
// order POSIX resolves them: LC_ALL wins over LC_CTYPE, which wins over LANG.
var localeVars = []string{"LC_ALL", "LC_CTYPE", "LANG"}

// s3270Environment returns the environment for the subprocess: the server's
// own, with the codeset pinned to UTF-8 unless the deployment has already
// chosen a UTF-8 one. A deployment that set a locale deliberately keeps it —
// this is here for the deployment that set none.
func s3270Environment(env []string) []string {
	if effectiveCodesetIsUTF8(env) {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if name, _, ok := strings.Cut(entry, "="); ok && name == "LC_ALL" {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "LC_ALL="+utf8Locale)
}

// effectiveCodesetIsUTF8 reports whether env already selects a UTF-8 codeset,
// resolving the locale variables in POSIX precedence order.
func effectiveCodesetIsUTF8(env []string) bool {
	values := make(map[string]string, len(localeVars))
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		for _, candidate := range localeVars {
			if name == candidate {
				values[name] = value
			}
		}
	}
	for _, name := range localeVars {
		value, ok := values[name]
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		return isUTF8Locale(value)
	}
	return false
}

// isUTF8Locale reports whether a locale name selects the UTF-8 codeset. The
// codeset is the part after the dot and is spelled several ways in the wild —
// "en_GB.UTF-8", "C.utf8", plain "UTF-8" — so the test is on the spelling
// rather than on an exact match.
func isUTF8Locale(value string) bool {
	normalised := strings.ToLower(value)
	normalised = strings.ReplaceAll(normalised, "-", "")
	return strings.Contains(normalised, "utf8")
}
