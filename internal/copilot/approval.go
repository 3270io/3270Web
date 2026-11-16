// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot

import (
	"fmt"
	"strings"

	"github.com/jnnngs/3270Web/internal/host"
)

// Which tool calls a model may not make unattended.
//
// This lived in web/static/copilot-panel.js as a pair of regexes, which meant
// the rule applied only where the browser ran the tool loop. An MCP client
// runs the same tools with no panel in front of them, so the classification
// belongs here, next to the tool definitions both surfaces are built from.
//
// The list is deliberately short. It is not "keys that change something" —
// every AID key does that — but keys that commonly end the session or throw
// away work in progress, where the cost of a wrong press is not a wrong screen
// but a lost one.

// dangerousSendKeys are the AID keys that need a person.
//
// The same two that chaos exploration leaves out of its default key weights:
// PF3 commonly logs the user off and Clear wipes the screen on many 3270
// applications. Chaos's automatic exit-key blocking only catches them when the
// current screen's help legend literally names the key, so a screen with no
// legend — or an abbreviated or non-English one — leaves both fully live.
// A model driving unattended should be no less careful than exploration is.
var dangerousSendKeys = []string{"PF(3)", "Clear"}

// DangerousSendKeys returns the keys that require explicit human approval,
// in their canonical spelling.
func DangerousSendKeys() []string {
	return append([]string(nil), dangerousSendKeys...)
}

// RequiresApproval reports whether a tool call needs a human to agree to it,
// returning the key that triggered the rule so the reason can be shown.
//
// Args is the decoded arguments object. An argument shape it cannot read is
// treated as not dangerous rather than as dangerous: the only call this
// applies to is send_key with a string key, and refusing everything
// unparseable would stop the panel on malformed input that the server rejects
// anyway.
func RequiresApproval(toolName string, args map[string]any) string {
	if toolName != "send_key" {
		return ""
	}
	key, _ := args["key"].(string)
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	canonical := CanonicalKeyName(key)
	for _, dangerous := range dangerousSendKeys {
		if canonical == dangerous {
			// The caller's spelling, not the canonical one: the message is
			// shown next to the call the model made, and "PF(3)" beside a
			// request that said "pf3" reads like a different key.
			return key
		}
	}
	return ""
}

// CanonicalKeyName folds the spellings a model produces onto one form, so
// "pf3", "PF(3)", "PF 3", "pf( 3 )", "F3" and "f03" are one key rather than
// six.
//
// It reads a key through host.NormalizeKey, the same function the server's key
// handlers use, so that what is classified is what will be sent. It used to
// carry its own folding that knew the PF and PA prefixes but not the bare F
// alias: "F3" was accepted by the handler as PF3 and classified here as a key
// that needed nobody's approval.
//
// For a name the parser does not recognise it still returns a stable folded
// form, so two unknown spellings compare equal; it is never the thing that
// decides a key is valid.
func CanonicalKeyName(key string) string {
	compact := strings.Join(strings.Fields(key), "")
	if normalized, ok := host.NormalizeKey(compact); ok {
		return normalized
	}
	compact = strings.NewReplacer("(", "", ")", "").Replace(strings.ToUpper(compact))
	if compact == "" {
		return ""
	}
	return strings.ToUpper(compact[:1]) + strings.ToLower(compact[1:])
}

// IsDangerousKey reports whether a key, in any spelling the server accepts, is
// one that needs a person.
func IsDangerousKey(key string) bool {
	canonical := CanonicalKeyName(key)
	if canonical == "" {
		return false
	}
	for _, dangerous := range dangerousSendKeys {
		if canonical == dangerous {
			return true
		}
	}
	return false
}

// dangerousKeyNote is appended to the send_key description so a model knows
// which keys will stop and wait for a person before it reaches for one.
func dangerousKeyNote() string {
	spelled := make([]string, 0, len(dangerousSendKeys))
	for _, key := range dangerousSendKeys {
		spelled = append(spelled, strings.NewReplacer("(", "", ")", "").Replace(key))
	}
	return fmt.Sprintf(
		" %s need a person to approve them and will not run unattended — the server refuses them until a person has confirmed: "+
			"PF3 commonly logs the session off and Clear discards what is on the screen. "+
			"Say what you are about to press and why before asking for one.",
		strings.Join(spelled, " and "))
}
