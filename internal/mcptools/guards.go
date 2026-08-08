package mcptools

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/jnnngs/3270Web/internal/hostpolicy"
)

// Two guards that turn advice in the built-in skills into something the
// server actually enforces.
//
// A skill can tell a model to blacklist the exit key before exploring, and to
// check which host it is pointed at. Both are the right advice and neither
// survives a model that is in a hurry, so the cases where getting it wrong is
// expensive are checked here instead.

// hostAllowed reports whether an AI client may point a session at a hostname.
//
// This is narrower than the deployment-wide ALLOWED_HOSTS, which the server
// enforces on every connection path including this one. MCP_ALLOWED_HOSTS
// fences the model specifically: a deployment can be willing to reach its
// whole estate from a browser while letting an AI client near only the test
// LPAR. Unset means "whatever the server allows", not "everything".
//
// Checking here as well as at the server is deliberate. The refusal a model
// can act on is one that names the tool argument it got wrong, before a
// session has been opened or a connection attempted.
func hostAllowed(hostname string) bool {
	return hostpolicy.Allowed(os.Getenv("MCP_ALLOWED_HOSTS"), hostname)
}

// unguardedChaosAllowed reports whether exploration may start with no key
// blacklist configured.
func unguardedChaosAllowed() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MCP_ALLOW_UNGUARDED_CHAOS"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// chaosGuarded reports whether anything has been recorded that would stop
// exploration pressing a session-ending key.
//
// Exploration presses keys chosen partly at random. On most applications one
// of them is labelled Exit or Sign Off, and pressing it ends the run at step
// four from a screen you may not be able to reach again. The chaos-monkey
// skill's first phase is to record a blacklist; this makes that a
// precondition rather than a suggestion.
//
// The check is deliberately loose — any blacklist at all counts, whether
// run-wide or for one screen. It is a guard against starting with nothing
// configured, not an attempt to judge whether the right keys were chosen.
func chaosGuarded(c Call) bool {
	if args, ok := c.Args["key_blacklist"]; ok {
		if list, ok := args.([]any); ok && len(list) > 0 {
			return true
		}
	}

	if hasBlacklist(c, "/chaos/hints") {
		return true
	}
	return hasBlacklist(c, "/chaos/screen-hints")
}

// hasBlacklist reports whether a hints endpoint mentions any blocked key.
//
// A parse failure counts as guarded: refusing to explore because the hints
// endpoint returned something unexpected would turn a cosmetic change into a
// blocked run, and this guard is worth less than that costs.
func hasBlacklist(c Call, path string) bool {
	res, err := c.Inv.Do(c.Ctx, http.MethodGet, sessionPath(c.SessionID, path), nil, nil)
	if err != nil || !res.OK() {
		return false
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body, &payload); err != nil {
		return true
	}
	return containsBlockedKeys(payload)
}

// containsBlockedKeys walks a decoded hints payload looking for a non-empty
// blacklist under any of the names the two endpoints use.
func containsBlockedKeys(v any) bool {
	switch value := v.(type) {
	case map[string]any:
		for key, inner := range value {
			switch strings.ToLower(key) {
			case "keyblacklist", "key_blacklist", "blockedkeys", "blocked_keys":
				if list, ok := inner.([]any); ok && len(list) > 0 {
					return true
				}
			}
			if containsBlockedKeys(inner) {
				return true
			}
		}
	case []any:
		for _, inner := range value {
			if containsBlockedKeys(inner) {
				return true
			}
		}
	}
	return false
}

const unguardedChaosMessage = `Refusing to start chaos exploration: no key blacklist is configured for this session.

Exploration presses AID keys chosen partly at random. On most applications one
of them ends the session — a PF key labelled Exit, Sign Off or Return — and a
run that presses it stops at step four, from a screen you may not be able to
reach again.

Read the current screen for keys like that, then record them with
chaos_update_hints (key_blacklist) for the whole run, or
chaos_save_screen_hint (blocked_keys) for one screen. Then start again.

If the application genuinely has no such key, set MCP_ALLOW_UNGUARDED_CHAOS=1.`
