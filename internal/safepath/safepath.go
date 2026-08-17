// SPDX-License-Identifier: AGPL-3.0-or-later

// Package safepath resolves caller-supplied file names into paths that are
// guaranteed to stay inside a directory the server chose.
//
// The rule this package exists to enforce is the one internal/host's file
// transfer code already follows and documents: the caller names a file, the
// server decides where it lives. A request that carries a path rather than a
// name is a request to write wherever the process can write, which is not a
// capability any HTTP handler should hand out.
//
// Validation is an allowlist on purpose. Blocklists of "../" and separators
// keep losing to encodings, Windows' two separators, NTFS alternate data
// streams and Unicode look-alikes; a fixed character set has none of those
// edges. Join then re-checks containment after normalisation so a bug in the
// character rules still cannot escape the base directory.
package safepath

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// MaxNameLength bounds a single file name. Common filesystems stop at 255
// bytes per component; leaving headroom means callers that append a suffix
// (".json", "-chaos.json") cannot push a just-legal name over the limit.
const MaxNameLength = 128

// ErrEmptyName is returned when a name is empty or only whitespace.
var ErrEmptyName = errors.New("safepath: name must not be empty")

// ValidateName reports whether name is usable as a single path component.
//
// Accepted: ASCII letters, digits, '-', '_' and '.'. Rejected: everything
// else, including both path separators, and any name that starts with '.'.
// The leading-dot rule does double duty — it blocks "." and ".." without
// special-casing them, and it keeps callers from colliding with the ".tmp-*"
// scratch files that atomic writers leave in the same directory.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrEmptyName
	}
	if len(name) > MaxNameLength {
		return fmt.Errorf("safepath: name is longer than %d characters", MaxNameLength)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("safepath: name %q must not start with a dot", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Errorf("safepath: name %q contains an unsupported character", name)
		}
	}
	return nil
}

// Join validates name and resolves it inside base.
//
// The containment re-check after filepath.Clean is deliberate belt-and-braces:
// ValidateName already makes escape impossible, so if this check ever fires it
// means the character rules have a hole, and failing closed is the right
// response to that.
func Join(base, name string) (string, error) {
	if strings.TrimSpace(base) == "" {
		return "", errors.New("safepath: base directory not configured")
	}
	if err := ValidateName(name); err != nil {
		return "", err
	}

	cleanBase := filepath.Clean(base)
	joined := filepath.Clean(filepath.Join(cleanBase, name))

	if joined != filepath.Join(cleanBase, filepath.Base(joined)) {
		return "", fmt.Errorf("safepath: name %q does not resolve inside the base directory", name)
	}
	if !strings.HasPrefix(joined, cleanBase+string(filepath.Separator)) {
		return "", fmt.Errorf("safepath: name %q escapes the base directory", name)
	}
	return joined, nil
}

// SanitizeName coerces an arbitrary caller string into a valid name rather
// than rejecting it, for the paths where a request historically accepted
// anything and returning an error would break existing clients.
//
// It keeps only the final path component, so "/etc/passwd" and "../../etc/passwd"
// both reduce to "passwd", then replaces every unsupported character with '_'.
// An input with nothing usable left yields fallback, which callers must supply
// as an already-valid name.
//
// Prefer ValidateName for new surfaces: telling a caller their name was
// rejected beats silently storing something they did not ask for.
func SanitizeName(name, fallback string) string {
	name = strings.TrimSpace(name)

	// Cut on both separators regardless of host OS. A Windows-style path
	// arriving at a Linux server would otherwise keep its backslashes, and
	// filepath.Base on Linux does not treat '\' as a separator.
	name = strings.ReplaceAll(name, "\\", "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}

	out := strings.TrimLeft(b.String(), ".")
	if len(out) > MaxNameLength {
		out = out[:MaxNameLength]
	}
	if ValidateName(out) != nil {
		return fallback
	}
	return out
}
