// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
)

// Attempt outcomes reported via Attempt.Outcome.
const (
	// OutcomeTransition: the key press moved to a different screen.
	OutcomeTransition = "transition"
	// OutcomeContent: same screen, but its body changed (a list paged, a
	// record loaded). Counts as progress the first time each variant is seen.
	OutcomeContent = "content"
	// OutcomeRejected: the application answered with an error message, or
	// bounced through an error screen and straight back.
	OutcomeRejected = "rejected"
	// OutcomeMessage: same screen, only a message line changed, and it does
	// not read as an error.
	OutcomeMessage = "message"
	// OutcomeNoChange: nothing on the screen changed.
	OutcomeNoChange = "no_change"
	// OutcomeSessionEnded: the key press threw the run back onto its first
	// (sign-on) screen from somewhere deeper.
	OutcomeSessionEnded = "session_ended"
	// OutcomeError: the step failed before an outcome could be observed.
	OutcomeError = "error"
)

// maskedFieldValue stands in for the content of a non-display field
// everywhere a run is serialised.
const maskedFieldValue = "********"

// maxMessageRows is how many changed rows still count as "just a message".
const maxMessageRows = 2

// maxAttemptMessageLen bounds the message text kept on an attempt.
const maxAttemptMessageLen = 200

// rejectionPhrases are the fragments an application's message line uses to
// say "no". Matched on word starts, upper-cased.
var rejectionPhrases = []string{
	"INVALID", "NOT VALID", "NOT FOUND", "ERROR", "UNKNOWN", "REJECTED",
	"NOT RECOGNI", "UNRECOGNI", "DOES NOT EXIST", "NOT EXIST", "IS NOT A",
	"NOT ALLOWED", "NOT AUTHORI", "UNAUTHORI", "NOT PERMITTED", "DENIED",
	"FAILED", "ILLEGAL", "INCORRECT", "NOT DEFINED", "UNDEFINED", "NO SUCH",
	"REQUIRED", "MUST BE", "CANNOT", "CAN NOT", "NOT NUMERIC", "OUT OF RANGE",
	"TOO LONG", "TOO SHORT", "NOT AVAILABLE", "NOT SUPPORTED", "NO ACTION",
	"NOT ACCEPTED", "NOT KNOWN", "WRONG",
}

// textLooksLikeRejection reports whether a message reads as the application
// refusing the input.
func textLooksLikeRejection(text string) bool {
	upper := strings.ToUpper(text)
	if strings.TrimSpace(upper) == "" {
		return false
	}
	for _, phrase := range rejectionPhrases {
		from := 0
		for {
			idx := strings.Index(upper[from:], phrase)
			if idx < 0 {
				break
			}
			start := from + idx
			if start == 0 || !isLabelWordRune(rune(upper[start-1])) {
				return true
			}
			from = start + 1
		}
	}
	return false
}

// changedProtectedRows returns the rows whose protected text differs between
// two screens, or nil with sameShape=false when the screens are not the same
// size (in which case every row is taken to have changed).
func changedProtectedRows(before, after *host.Screen) (rows []int, sameShape bool) {
	b := protectedRowTexts(before)
	a := protectedRowTexts(after)
	if len(a) == 0 || len(a) != len(b) {
		return nil, false
	}
	for y := range a {
		if a[y] != b[y] {
			rows = append(rows, y)
		}
	}
	return rows, true
}

// messageFromRows joins the text of the given rows into one bounded line.
func messageFromRows(screen *host.Screen, rows []int) string {
	if screen == nil || len(rows) == 0 {
		return ""
	}
	texts := protectedRowTexts(screen)
	parts := make([]string, 0, len(rows))
	for _, y := range rows {
		if y < 0 || y >= len(texts) {
			continue
		}
		if t := strings.Join(strings.Fields(texts[y]), " "); t != "" {
			parts = append(parts, t)
		}
	}
	msg := strings.Join(parts, " | ")
	if r := []rune(msg); len(r) > maxAttemptMessageLen {
		msg = string(r[:maxAttemptMessageLen])
	}
	return msg
}

// messageZoneText returns the text of the rows where applications put their
// messages: the top three and the bottom five.
func messageZoneText(screen *host.Screen) string {
	texts := protectedRowTexts(screen)
	if len(texts) == 0 {
		return ""
	}
	var b strings.Builder
	for y, t := range texts {
		if y < 3 || y >= len(texts)-5 {
			b.WriteString(t)
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// sameAreaOutcome classifies a key press that stayed on the same area.
func sameAreaOutcome(before, after *host.Screen) (outcome, message string) {
	if before == nil || after == nil {
		return OutcomeNoChange, ""
	}
	rows, sameShape := changedProtectedRows(before, after)
	if !sameShape {
		return OutcomeContent, ""
	}
	if len(rows) == 0 {
		return OutcomeNoChange, ""
	}
	message = messageFromRows(after, rows)
	if len(rows) <= maxMessageRows {
		if textLooksLikeRejection(message) {
			return OutcomeRejected, message
		}
		return OutcomeMessage, message
	}
	return OutcomeContent, ""
}

// screenHasHiddenInput reports whether the screen has a non-display input
// field: the mark of a sign-on screen.
func screenHasHiddenInput(screen *host.Screen) bool {
	if screen == nil {
		return false
	}
	for _, f := range screen.Fields {
		if f != nil && !f.IsProtected() && f.IsHidden() {
			return true
		}
	}
	return false
}
