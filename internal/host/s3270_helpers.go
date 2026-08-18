// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// s3270 status line indices.
	// Status format example: "U F P C(localhost) I 4 24 80 0 0 0x0 0.000"
	// See: http://x3270.bgp.nu/s3270-man.html
	statusIdxKeyboard    = 0  // Keyboard state: U=Unlocked, L=Locked, E=Error
	statusIdxFormatting  = 1  // Screen formatting: F=Formatted, U=Unformatted
	statusIdxProtection  = 2  // Field protection: P=Protected, U=Unprotected
	statusIdxConnection  = 3  // Connection state: C(host)=Connected, N=Not connected
	statusIdxMode        = 4  // Emulator mode: I=Connected, C=Connected, N=Not connected
	statusIdxModel       = 5  // Model number (2-5)
	statusIdxRows        = 6  // Number of rows
	statusIdxCols        = 7  // Number of columns
	statusIdxCursorRow   = 8  // Cursor row (0-based)
	statusIdxCursorCol   = 9  // Cursor col (0-based)
	statusIdxWindowID    = 10 // Window ID
	statusIdxCommandTime = 11 // Execution time
	statusMinFields      = 12 // Minimum number of fields in a valid status line
)

// isAidKey checks if a key is an Attention Identifier (AID) key that interacts with the host.
func isAidKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	return upper == "ENTER" || strings.HasPrefix(upper, "PF") || strings.HasPrefix(upper, "PA") || upper == "CLEAR" || upper == "SYSREQ" || upper == "ATTN"
}

// isKeyboardUnlocked checks if the keyboard is unlocked based on the s3270 status line.
// The first field in the status line indicates keyboard state: "U" = Unlocked, "L" = Locked.
func isKeyboardUnlocked(status string) bool {
	// Status format is space-separated fields, e.g., "U F P C(localhost) I 4 24 80 0 0 0x0 0.000"
	// The first field is the keyboard state, followed by a space
	return len(status) >= 2 && strings.HasPrefix(status, "U ")
}

// isS3270Error checks if the status or data indicates an s3270 error.
func isS3270Error(status string, data []string) bool {
	if strings.HasPrefix(status, "E ") {
		return true
	}
	for _, line := range data {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "error") {
			return true
		}
	}
	return false
}

// isDisconnectedStatus checks if the status line indicates a disconnected state.
func isDisconnectedStatus(status string) bool {
	parts := strings.Fields(status)
	if len(parts) > statusIdxConnection {
		return parts[statusIdxConnection] == "N"
	}
	return false
}

var connectionErrorPhrases = []string{
	"not connected",
	"terminated",
	"no status received",
	"timed out",
	"pipe is being closed",
	"broken pipe",
	"pipe has been ended",
	"closed pipe",
}

// isConnectionError checks if an error message indicates a lost connection.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, phrase := range connectionErrorPhrases {
		if strings.Contains(msg, phrase) {
			return true
		}
	}
	return false
}

// keyToAction turns a key name into the action the terminal actually has.
//
// A PF or PA key arrives here spelled several ways, because the people and the
// tools that produce one do not agree: "PF3" from a keymap, "pf(3)" from a
// recording, "PF(3)" from this application's own API. isAidKey accepts every
// one of them, so by the time a key reaches here it has already been treated
// as a real AID key. The terminal accepts exactly one spelling — PF(3) — and
// answers anything else with "Nonexistent or invalid name", which is a
// confusing thing to tell somebody who pressed a function key that plainly
// exists.
//
// Normalising here rather than at each caller is what keeps the two answers
// consistent: a key this package calls an AID key is a key it can send.
// Anything that is not a PF or PA key is passed through untouched, because the
// terminal's other actions are named exactly as callers write them.
func keyToAction(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "Enter"
	}

	upper := strings.ToUpper(trimmed)
	for _, prefix := range []string{"PF", "PA"} {
		if !strings.HasPrefix(upper, prefix) {
			continue
		}
		inner := strings.TrimPrefix(upper, prefix)
		inner = strings.TrimSuffix(strings.TrimPrefix(inner, "("), ")")
		n, err := strconv.Atoi(strings.TrimSpace(inner))
		if err != nil {
			continue
		}
		limit := 24
		if prefix == "PA" {
			limit = 3
		}
		if n < 1 || n > limit {
			continue
		}
		return fmt.Sprintf("%s(%d)", prefix, n)
	}

	return trimmed
}
