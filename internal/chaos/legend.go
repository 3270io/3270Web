// SPDX-License-Identifier: AGPL-3.0-or-later

package chaos

import (
	"sort"
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
)

// Key-legend classification.
//
// A 3270 screen usually carries a legend along its bottom rows saying what
// each function key does ("PF3=Exit  PF8=Forward"). The engine reads it for
// two opposite purposes: to stay away from keys that end the session or
// change data, and to lean towards keys that move around the application.
// Everything here matches whole words: a substring test reads "UP" out of
// "GROUP UPDATE" and "END" out of "PENDING".

// legendRowCount is how many rows at the bottom of the screen are treated as
// the key legend.
const legendRowCount = 4

// exitStyleLabelWords name actions that leave the application or the
// session. A key carrying one of these is never boosted, and is blocked
// outright while AutoBlockExitKeys is on.
var exitStyleLabelWords = []string{
	"END", "RETURN", "EXIT", "QUIT", "CANCEL", "BYE",
	"SIGN OFF", "SIGNOFF", "SIGN OUT", "SIGNOUT",
	"LOGOFF", "LOG OFF", "LOGOUT", "LOG OUT",
	"DISCONNECT", "TERMINATE",
}

// sessionEndingLabelWords is the subset of exit-style words that end the
// whole session rather than just the current screen. They carry the heaviest
// penalty when blocking is switched off.
var sessionEndingLabelWords = []string{
	"SIGN OFF", "SIGNOFF", "SIGN OUT", "SIGNOUT",
	"LOGOFF", "LOG OFF", "LOGOUT", "LOG OUT",
	"DISCONNECT",
}

// destructiveLabelWords name actions that change or commit data. A function
// key carrying one is never boosted and is blocked unless the user names the
// key in a hint for that screen.
var destructiveLabelWords = []string{
	"DELETE", "PURGE", "REMOVE", "CONFIRM", "COMMIT", "UPDATE", "SAVE", "SUBMIT",
}

// deniedTypedWords are command words the engine never types of its own
// accord, whether it read them off the screen or carries them as a built-in
// default. Values the user supplies as hints are not filtered by this list.
var deniedTypedWords = map[string]struct{}{
	"LOGOFF": {}, "LOGOUT": {}, "SIGNOFF": {}, "SIGNOUT": {}, "OFF": {},
	"QUIT": {}, "EXIT": {}, "BYE": {}, "END": {}, "DISCONNECT": {},
	"SHUTDOWN": {}, "CANCEL": {}, "DELETE": {}, "PURGE": {}, "REMOVE": {},
	"DROP": {}, "KILL": {}, "STOP": {}, "HALT": {}, "FORMAT": {}, "RESET": {},
	"CLOSE": {}, "VOID": {},
}

// isDeniedTypedValue reports whether the first word of value is one the
// engine must not type on its own initiative.
func isDeniedTypedValue(value string) bool {
	fields := strings.Fields(strings.ToUpper(value))
	if len(fields) == 0 {
		return false
	}
	_, denied := deniedTypedWords[fields[0]]
	return denied
}

func isLabelWordRune(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// containsLabelWord reports whether phrase occurs in text as a whole word (or
// run of whole words). Both must already be normalized with
// normalizeHintAssignmentLabel.
func containsLabelWord(text, phrase string) bool {
	if text == "" || phrase == "" {
		return false
	}
	from := 0
	for {
		idx := strings.Index(text[from:], phrase)
		if idx < 0 {
			return false
		}
		start := from + idx
		end := start + len(phrase)
		beforeOK := start == 0 || !isLabelWordRune(rune(text[start-1]))
		afterOK := end == len(text) || !isLabelWordRune(rune(text[end]))
		if beforeOK && afterOK {
			return true
		}
		from = start + 1
		if from >= len(text) {
			return false
		}
	}
}

func containsAnyLabelWord(text string, phrases []string) bool {
	for _, p := range phrases {
		if containsLabelWord(text, p) {
			return true
		}
	}
	return false
}

// labelIsExitStyle returns true when a help-line label probably maps to an
// "exit / quit / cancel / logoff / end / return" action that chaos must never
// trigger.
func labelIsExitStyle(label string) bool {
	n := normalizeHintAssignmentLabel(label)
	if n == "" {
		return false
	}
	return containsAnyLabelWord(n, exitStyleLabelWords)
}

// labelIsDestructive returns true when a help-line label names an action
// that changes or commits data.
func labelIsDestructive(label string) bool {
	n := normalizeHintAssignmentLabel(label)
	if n == "" {
		return false
	}
	return containsAnyLabelWord(n, destructiveLabelWords)
}

// labelIsDenied reports whether a legend label is on the deny list: exit
// style or destructive. Denied labels are never boosted.
func labelIsDenied(label string) bool {
	return labelIsExitStyle(label) || labelIsDestructive(label)
}

func labelIsNavigationStyle(label string) bool {
	n := normalizeHintAssignmentLabel(label)
	if n == "" || labelIsDenied(n) {
		return false
	}
	return containsAnyLabelWord(n, []string{
		"HELP", "MENU", "NEXT", "PREV", "PREVIOUS", "FORWARD", "BACK",
		"PAGE", "SCROLL",
	})
}

// chaosHelpLabelBoost scores a legend label. Denied labels are penalised,
// recognised navigation labels are boosted, and a label the engine does not
// recognise scores nothing: an unknown action is not a reason to press a key
// more often.
func chaosHelpLabelBoost(label string) int {
	n := normalizeHintAssignmentLabel(label)
	if n == "" {
		return 0
	}
	if containsAnyLabelWord(n, sessionEndingLabelWords) {
		return -200
	}
	if labelIsDestructive(n) {
		return -100
	}
	if labelIsExitStyle(n) {
		return -40
	}
	if containsLabelWord(n, "HELP") {
		return 45
	}
	if containsAnyLabelWord(n, []string{
		"NEXT", "FORWARD", "MORE", "CONTINUE", "SELECT", "DETAIL", "DETAILS", "OPEN", "ENTER",
	}) {
		return 70
	}
	if containsAnyLabelWord(n, []string{"PAGE", "SCROLL", "UP", "DOWN", "LEFT", "RIGHT"}) {
		return 50
	}
	if containsAnyLabelWord(n, []string{"MENU", "MAIN", "HOME", "BACK", "PREV", "PREVIOUS", "LIST"}) {
		return 40
	}
	return 0
}

// isAIDKeyName reports whether a normalized key name is an attention key:
// one that sends the screen to the host. Cursor and editing keys are not.
func isAIDKeyName(key string) bool {
	switch {
	case key == "Enter", key == "Clear":
		return true
	case strings.HasPrefix(key, "PF("), strings.HasPrefix(key, "PA("):
		return true
	}
	return false
}

// legendText returns the normalized text of the screen's legend rows.
func legendText(screen *host.Screen) string {
	if screen == nil || screen.Height <= 0 {
		return ""
	}
	startRow := screen.Height - legendRowCount
	if startRow < 0 {
		startRow = 0
	}
	var b strings.Builder
	for y := startRow; y < screen.Height; y++ {
		b.WriteString(screenRowText(screen, y))
		b.WriteByte(' ')
	}
	return normalizeHintAssignmentLabel(b.String())
}

// inferKeysWithLabel returns the sorted function keys whose legend label
// satisfies match. Enter is never returned: it is the only way forward on
// most screens, so a legend naming it is recorded but never used to block it.
func inferKeysWithLabel(screen *host.Screen, match func(string) bool) []string {
	assignments := inferScreenHelpKeyAssignments(screen)
	if len(assignments) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	for label, key := range assignments {
		k := normalizeChaosKeyName(key)
		if k == "" || k == "Enter" {
			continue
		}
		if match(label) {
			seen[k] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// inferAutoBlockedKeys scans the bottom legend rows for "PFn <Exit-style>"
// labels and returns the keys that should be treated as blocked. Used by the
// AutoBlockExitKeys config to prevent chaos from accidentally logging the
// session out or exiting the screen.
func inferAutoBlockedKeys(screen *host.Screen) []string {
	return inferKeysWithLabel(screen, labelIsExitStyle)
}

// inferDestructiveKeys returns the function keys whose legend label names a
// data-changing action (delete, save, confirm, ...).
func inferDestructiveKeys(screen *host.Screen) []string {
	return inferKeysWithLabel(screen, labelIsDestructive)
}

// inferAutoNavigationKeys returns keys whose help-line labels look like
// navigation actions (Help/Menu/Next/Prev/Forward/Back). These are surfaced
// in the mind map so chaos_report can highlight them.
func inferAutoNavigationKeys(screen *host.Screen) []string {
	return inferKeysWithLabel(screen, labelIsNavigationStyle)
}

// labelIsPagingStyle reports whether a legend label names scrolling within
// the screen the run is already on: the next or previous page of a list.
//
// "Back" and "Menu" are deliberately not here. They leave the screen, and
// which screen they leave it for is exactly what the configured key set is
// there to decide.
func labelIsPagingStyle(label string) bool {
	n := normalizeHintAssignmentLabel(label)
	if n == "" || labelIsDenied(n) {
		return false
	}
	return containsAnyLabelWord(n, []string{
		"NEXT", "PREV", "PREVIOUS", "FORWARD", "BACKWARD", "SCROLL",
		"UP", "DOWN", "MORE", "NEXT PAGE", "PREV PAGE", "PAGE UP", "PAGE DOWN",
		"PAGE FORWARD", "PAGE BACK",
	})
}

// inferPagingKeys returns the function keys a screen's own legend labels as
// paging. These are the one case where the legend may bring in a key the
// configuration did not list: the default key set has no paging keys, so a
// list was only ever seen one page deep, and a key the screen itself says
// scrolls it is not one that can sign the session off or change data.
func inferPagingKeys(screen *host.Screen) []string {
	return inferKeysWithLabel(screen, labelIsPagingStyle)
}

// restrictBoostsToKeys drops boosts for keys outside allowed. Built-in hints
// and legend-derived boosts go through this so that they can only re-weight
// keys the configuration already allows, never introduce one.
func restrictBoostsToKeys(boosts map[string]int, allowed []string) map[string]int {
	if len(boosts) == 0 {
		return nil
	}
	out := make(map[string]int, len(boosts))
	for rawKey, v := range boosts {
		key := normalizeChaosKeyName(rawKey)
		if key == "" || !isAIDKeyName(key) {
			continue
		}
		for _, a := range allowed {
			if a == key {
				out[key] += v
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
