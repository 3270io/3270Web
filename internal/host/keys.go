// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"fmt"
	"strconv"
	"strings"
)

// The one place a key name is read.
//
// Two things used to read key names and they disagreed. The server's key
// parser accepted a bare "F3" for PF3; the rule that decides which keys a
// model may not press unattended folded only the PF and PA spellings, so "F3"
// reached the host as PF3 while being classified as a key nobody had to
// approve. Anything that asks "which key is this?" asks here, so the answer
// to "is it dangerous?" is always about the key that will actually be sent.

// NumberedFnKeyMax and NumberedPAKeyMax are the highest key numbers a 3270
// keyboard defines.
const (
	NumberedFnKeyMax = 24
	NumberedPAKeyMax = 3
)

// numberedKey matches upper against one PF/PA-style prefix — the bare form
// ("PF12") or, when allowParen, the parenthesized one ("PF(12)") — and
// reports it in canonical "canon(N)" form when N falls within [1, max].
func numberedKey(upper, prefix, canon string, max int, allowParen bool) (string, bool) {
	body, matched := "", false
	if allowParen && strings.HasPrefix(upper, prefix+"(") && strings.HasSuffix(upper, ")") {
		body, matched = strings.TrimSuffix(strings.TrimPrefix(upper, prefix+"("), ")"), true
	} else if strings.HasPrefix(upper, prefix) {
		body, matched = strings.TrimPrefix(upper, prefix), true
	}
	if !matched {
		return "", false
	}
	// Atoi accepts a leading sign, so "PF+3" would otherwise be taken for
	// PF(3) although it is not a key name anyone typed on purpose.
	if body == "" || strings.Trim(body, "0123456789") != "" {
		return "", false
	}
	n, err := strconv.Atoi(body)
	if err != nil || n < 1 || n > max {
		return "", false
	}
	return fmt.Sprintf("%s(%d)", canon, n), true
}

// namedKeys maps the lower-case spellings callers use onto the name SendKey
// expects.
var namedKeys = map[string]string{
	"enter":       "Enter",
	"tab":         "Tab",
	"backtab":     "BackTab",
	"clear":       "Clear",
	"reset":       "Reset",
	"eraseeof":    "EraseEOF",
	"erase_eof":   "EraseEOF",
	"eraseinput":  "EraseInput",
	"erase_input": "EraseInput",
	"dup":         "Dup",
	"fieldmark":   "FieldMark",
	"field_mark":  "FieldMark",
	"sysreq":      "SysReq",
	"sys_req":     "SysReq",
	"attn":        "Attn",
	"newline":     "Newline",
	"new_line":    "Newline",
	"backspace":   "BackSpace",
	"delete":      "Delete",
	"insert":      "Insert",
	"home":        "Home",
	"up":          "Up",
	"down":        "Down",
	"left":        "Left",
	"right":       "Right",
}

// NormalizeKey canonicalizes a caller-supplied key name (e.g. "pf3", "PF(3)",
// "F3", "enter") to the exact form SendKey expects. It returns ok=false for
// empty, malformed, or unrecognized input — callers must reject the request
// rather than substitute a default, since silently falling back to Enter for
// anything unrecognized means a typo submits the current screen instead of
// failing loudly.
func NormalizeKey(key string) (normalized string, ok bool) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", false
	}
	// Characters that could be read as further s3270 script commands.
	if strings.ContainsAny(trimmed, "\n\r\t;") {
		return "", false
	}

	upper := strings.ToUpper(trimmed)
	if k, ok := numberedKey(upper, "PF", "PF", NumberedFnKeyMax, true); ok {
		return k, true
	}
	if k, ok := numberedKey(upper, "PA", "PA", NumberedPAKeyMax, true); ok {
		return k, true
	}
	// "F" is a bare alias for "PF" (e.g. "F1" means PF(1)); unlike PF/PA it
	// never had a parenthesized spelling, so "F(1)" is rejected exactly as it
	// always was.
	if k, ok := numberedKey(upper, "F", "PF", NumberedFnKeyMax, false); ok {
		return k, true
	}
	if k, ok := namedKeys[strings.ToLower(trimmed)]; ok {
		return k, true
	}
	return "", false
}

// IsAIDKey reports whether a key hands the screen to the host — the keys after
// which the keyboard locks until the host answers — as opposed to one that
// only moves the cursor or edits the local buffer.
func IsAIDKey(key string) bool {
	normalized, ok := NormalizeKey(key)
	if !ok {
		return false
	}
	return isAidKey(normalized)
}
