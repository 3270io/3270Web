package mcptools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// This file is the Go counterpart of web/static/copilot-tools.js: for each
// tool the model can call, which request carries it out.
//
// The two exist because the browser panel runs its tool loop in the page and
// an MCP client runs it in this process. They are kept honest about each
// other by the catalogue join in registry.go and by
// TestBrowserHandlersMatchToolCatalogue in cmd/3270Web.

// settleDelay is how long to wait after an action before re-reading the
// screen.
//
// The host has not necessarily finished painting when the request returns, so
// reading immediately can hand the model the screen it just left. The status
// line is the real signal and wait_for_unlock is how a caller uses it
// properly; this is the cheap version applied automatically after the actions
// that usually complete quickly, matching what the browser panel does.
const settleDelay = 250 * time.Millisecond

// sessionPath builds a path under the caller's session.
func sessionPath(sessionID, suffix string) string {
	return "/api/v1/sessions/" + url.PathEscape(sessionID) + suffix
}

// bindings returns the binding for every tool. Keys must match
// copilot.DefaultTools() exactly; registry.go enforces that at init.
func bindings() map[string]Tool {
	return map[string]Tool{
		// --- Screen: reading -------------------------------------------
		"get_screen": {
			Tier: TierRead, OpenWorld: true, HostData: true, NeedsSession: true,
			Handle: func(c Call) (string, error) { return readScreen(c) },
		},
		"wait_for_unlock": {
			Tier: TierRead, OpenWorld: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				body := map[string]any{}
				if ms, ok := c.Args["timeout_ms"]; ok {
					body["timeout_ms"] = ms
				}
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/wait"), nil, body)
			},
		},

		// --- Screen: driving -------------------------------------------
		"send_key": {
			Tier: TierInteract, Destructive: true, OpenWorld: true, HostData: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				key := stringArg(c.Args, "key")
				if key == "" {
					key = "Enter"
				}
				res, err := c.Inv.Do(c.Ctx, http.MethodPost,
					sessionPath(c.SessionID, "/key"), nil, map[string]any{"key": key})
				if err != nil {
					return "", err
				}
				if !res.OK() {
					// An unrecognised key is rejected rather than treated as
					// Enter, and the message says which key. Passing that
					// through unchanged is the whole value of the check.
					return string(res.Body), nil
				}
				return withScreen(c, fmt.Sprintf("Sent %s.", key))
			},
		},
		"submit_screen": {
			Tier: TierInteract, Destructive: true, OpenWorld: true, HostData: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				res, err := c.Inv.Do(c.Ctx, http.MethodPost,
					sessionPath(c.SessionID, "/submit"), nil, map[string]any{})
				if err != nil {
					return "", err
				}
				if !res.OK() {
					return string(res.Body), nil
				}
				return withScreen(c, "Submitted modified fields and pressed Enter.")
			},
		},
		"write_field": {
			Tier: TierInteract, Destructive: true, OpenWorld: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				body := map[string]any{"text": stringArg(c.Args, "text")}
				// field_key is 1-indexed and row/col are 0-indexed; the
				// server converts. Forwarding whichever the caller supplied,
				// unchanged, is what keeps that conversion in one place.
				if fk := stringArg(c.Args, "field_key"); fk != "" {
					body["field_key"] = fk
				} else {
					body["row"] = intArg(c.Args, "row")
					body["col"] = intArg(c.Args, "col")
				}
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/write"), nil, body)
			},
		},
		"move_cursor": {
			Tier: TierInteract, OpenWorld: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/cursor"), nil,
					map[string]any{"row": intArg(c.Args, "row"), "col": intArg(c.Args, "col")})
			},
		},
		"connect_session": {
			Tier: TierInteract, Destructive: true, OpenWorld: true, HostData: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				hostname := stringArg(c.Args, "hostname")
				if hostname == "" {
					return "", fmt.Errorf("hostname is required")
				}
				res, err := c.Inv.Do(c.Ctx, http.MethodPost,
					sessionPath(c.SessionID, "/connect"), nil, map[string]any{"hostname": hostname})
				if err != nil {
					return "", err
				}
				if !res.OK() {
					return string(res.Body), nil
				}
				return withScreen(c, "Connected to "+hostname+".")
			},
		},

		// --- Chaos: observing ------------------------------------------
		"chaos_status": {
			Tier: TierRead, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				q := url.Values{}
				if boolArg(c.Args, "verbose") {
					q.Set("verbose", "true")
				}
				return passthrough(c, http.MethodGet, sessionPath(c.SessionID, "/chaos/status"), q, nil)
			},
		},
		"chaos_report": {
			Tier: TierRead, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				// Returns markdown, not JSON.
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/report"), nil, map[string]any{})
			},
		},
		"chaos_insights": {
			Tier: TierRead, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodGet, sessionPath(c.SessionID, "/chaos/insights"), nil, nil)
			},
		},
		"chaos_list_screens": {
			Tier: TierRead, HostData: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				q := url.Values{}
				if v, ok := c.Args["include_previews"]; ok {
					q.Set("include_previews", fmt.Sprint(v))
				}
				return passthrough(c, http.MethodGet, sessionPath(c.SessionID, "/chaos/screens"), q, nil)
			},
		},
		"chaos_get_hints": {
			Tier: TierRead, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				// The browser merges two endpoints here; so do we, so the
				// model sees one answer to one question.
				run, err := passthrough(c, http.MethodGet, sessionPath(c.SessionID, "/chaos/hints"), nil, nil)
				if err != nil {
					return "", err
				}
				screen, err := passthrough(c, http.MethodGet, sessionPath(c.SessionID, "/chaos/screen-hints"), nil, nil)
				if err != nil {
					return "", err
				}
				return "Run-wide hints:\n" + run + "\n\nPer-screen hints:\n" + screen, nil
			},
		},
		"chaos_export_workflow": {
			Tier: TierRead, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/export"), nil, c.Args)
			},
		},

		// --- Chaos: recording what was learned -------------------------
		"chaos_save_screen_hint": {
			Tier: TierInteract, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/screen-hints"), nil, c.Args)
			},
		},
		"chaos_update_hints": {
			Tier: TierInteract, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/hints"), nil, c.Args)
			},
		},
		"chaos_annotate_screen": {
			Tier: TierInteract, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/screens/annotate"), nil, c.Args)
			},
		},

		// --- Chaos: running --------------------------------------------
		// Exploration presses keys chosen partly at random against a live
		// region, unattended. That is why it is a tier of its own.
		"chaos_start": {
			Tier: TierChaos, Destructive: true, OpenWorld: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/start"), nil, c.Args)
			},
		},
		"chaos_stop": {
			Tier: TierChaos, Destructive: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/stop"), nil, map[string]any{})
			},
		},
		"chaos_resume": {
			Tier: TierChaos, Destructive: true, OpenWorld: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/resume"), nil, map[string]any{})
			},
		},

		// --- Business understanding ------------------------------------
		"business_list_functions": {
			Tier: TierRead, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodGet, sessionPath(c.SessionID, "/chaos/business/functions"), nil, nil)
			},
		},
		"business_app_overview": {
			Tier: TierRead, HostData: true, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodGet, sessionPath(c.SessionID, "/chaos/business/overview"), nil, nil)
			},
		},
		"business_save_function": {
			Tier: TierInteract, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				if stringArg(c.Args, "name") == "" {
					return "", fmt.Errorf("name is required")
				}
				return passthrough(c, http.MethodPost, sessionPath(c.SessionID, "/chaos/business/functions"), nil, c.Args)
			},
		},
		"business_generate_workflow": {
			Tier: TierRead, NeedsSession: true,
			Handle: func(c Call) (string, error) {
				if stringArg(c.Args, "name") == "" {
					return "", fmt.Errorf("name is required")
				}
				return passthrough(c, http.MethodPost,
					sessionPath(c.SessionID, "/chaos/business/generate-workflow"), nil, c.Args)
			},
		},

		// --- Skills, instructions, extensions --------------------------
		// Not session-scoped: the catalogue is the same whichever host you
		// are connected to, and is usually read before connecting at all.
		"list_skills": {
			Tier: TierRead,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodGet, "/api/v1/skills", nil, nil)
			},
		},
		"load_skill": {
			Tier: TierRead,
			Handle: func(c Call) (string, error) {
				name := stringArg(c.Args, "name")
				if name == "" {
					return "", fmt.Errorf("name is required")
				}
				return passthrough(c, http.MethodGet, "/api/v1/skills/load", url.Values{"name": {name}}, nil)
			},
		},
		"list_instructions": {
			Tier: TierRead,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodGet, "/api/v1/instructions", nil, nil)
			},
		},
		"load_instruction": {
			Tier: TierRead,
			Handle: func(c Call) (string, error) {
				name := stringArg(c.Args, "name")
				if name == "" {
					return "", fmt.Errorf("name is required")
				}
				return passthrough(c, http.MethodGet, "/api/v1/instructions/load", url.Values{"name": {name}}, nil)
			},
		},
		"list_extensions": {
			Tier: TierRead,
			Handle: func(c Call) (string, error) {
				return passthrough(c, http.MethodGet, "/api/v1/extensions", nil, nil)
			},
		},
	}
}

// passthrough performs a request and returns its body as the model sees it.
//
// A non-2xx reply is returned as text rather than raised as an error: the
// handlers answer with a JSON object carrying a message, and that message is
// usually what tells the model what to do differently. Turning it into a
// transport error would discard it.
func passthrough(c Call, method, path string, query url.Values, body any) (string, error) {
	res, err := c.Inv.Do(c.Ctx, method, path, query, body)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(string(res.Body))
	if out == "" {
		if res.OK() {
			return "OK", nil
		}
		return fmt.Sprintf("request failed with status %d", res.Status), nil
	}
	return out, nil
}

// readScreen fetches the current screen.
func readScreen(c Call) (string, error) {
	return passthrough(c, http.MethodGet, sessionPath(c.SessionID, "/screen.json"), nil, nil)
}

// withScreen reports an action and the screen it produced, after letting the
// host settle. The pair is what the model needs: on its own, "sent PF3" says
// nothing about where that landed.
func withScreen(c Call, note string) (string, error) {
	select {
	case <-time.After(settleDelay):
	case <-c.Ctx.Done():
		return note + "\n\n(cancelled before the screen could be re-read)", nil
	}
	screen, err := readScreen(c)
	if err != nil {
		return note + "\n\n(could not re-read the screen: " + err.Error() + ")", nil
	}
	return note + "\n\n" + screen, nil
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

func boolArg(args map[string]any, key string) bool {
	b, _ := args[key].(bool)
	return b
}

func normalise(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
