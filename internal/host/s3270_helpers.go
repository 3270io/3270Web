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

// keyToKeySpec normalizes a key string into an s3270 key specification.
func keyToKeySpec(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "Enter"
	}

	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			return fmt.Sprintf("PF%d", n)
		}
	}
	if strings.HasPrefix(upper, "PA(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PA("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			return fmt.Sprintf("PA%d", n)
		}
	}

	return trimmed
}
