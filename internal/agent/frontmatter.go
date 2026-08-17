// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import "strings"

// frontmatter is the key/value block at the head of a SKILL.md.
//
// It is parsed by splitting each line on its first colon rather than with a
// YAML library, deliberately. The format a skill author sees documented is
// exactly this: one key per line, a scalar or a bracketed list. A YAML parser
// would accept anchors, block scalars, nested maps and multi-document files
// that nothing downstream reads, so a file could parse cleanly and still not
// mean what its author intended. Matching the documented format keeps the
// error message honest.
type frontmatter map[string]string

// splitFrontmatter separates a leading `---`-delimited block from the body.
// A file with no frontmatter yields an empty map and the whole text as body,
// so a half-written skill fails on "no name" rather than on a parse error.
func splitFrontmatter(raw string) (frontmatter, string) {
	fm := frontmatter{}

	text := strings.ReplaceAll(raw, "\r\n", "\n")
	trimmed := strings.TrimLeft(text, "\n \t")
	if !strings.HasPrefix(trimmed, "---") {
		return fm, text
	}

	// Drop the opening delimiter line.
	rest := trimmed[len("---"):]
	rest = strings.TrimPrefix(rest, "\n")

	end := strings.Index(rest, "\n---")
	if end < 0 {
		// Unterminated block: treat the whole file as body rather than
		// silently swallowing it as frontmatter.
		return fm, text
	}

	block := rest[:end]
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")

	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		value := strings.TrimSpace(line[idx+1:])
		if key == "" {
			continue
		}
		fm[key] = value
	}

	return fm, body
}

// value returns a scalar, with surrounding quotes removed.
func (f frontmatter) value(key string) string {
	return unquote(f[key])
}

// list returns a bracketed or comma-separated list. `[a, b]`, `a, b` and a
// single bare value all yield the same thing, because all three are what
// people actually write.
func (f frontmatter) list(key string) []string {
	raw := strings.TrimSpace(f[key])
	if raw == "" {
		return nil
	}
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")

	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := unquote(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	return strings.TrimSpace(s)
}
