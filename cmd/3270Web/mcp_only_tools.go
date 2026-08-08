package main

import (
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jnnngs/3270Web/internal/mcptools"
)

// The tools MCP adds that the browser panel has no use for.
//
// The panel's session is whichever tab you are looking at and its task menu
// is on screen, so list_sessions, use_session, list_tasks and run_task exist
// only here — they are not in copilot.DefaultTools() and so are not in the
// mcptools registry either.
//
// Their descriptors live in this file rather than beside their handlers
// because two things need them: buildMCPServer, which attaches a handler to
// each, and `3270Web mcp --list-tools`, which runs with no target and no
// server. Declaring them twice is how --list-tools came to report 25 tools
// while the server offered 29, and the four missing ones were the ones a
// first-run check most needed to see.

// mcpOnlyTool is a static tool descriptor plus the tier it belongs to.
type mcpOnlyTool struct {
	Tool *mcp.Tool
	Tier mcptools.Tier
}

// noArgsSchema is the schema for a tool that takes nothing.
func noArgsSchema() map[string]any {
	return map[string]any{
		"type": "object", "properties": map[string]any{}, "additionalProperties": false,
	}
}

// mcpOnlyTools returns every static MCP-only descriptor, sorted by name.
func mcpOnlyTools() []mcpOnlyTool {
	out := []mcpOnlyTool{
		{
			Tier: mcptools.TierRead,
			Tool: &mcp.Tool{
				Name: "list_sessions",
				Description: "List the 3270 sessions this server has open, with the host each is connected to. " +
					"Use it to find a session id for use_session, or to check whether anything is connected yet.",
				InputSchema: noArgsSchema(),
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			},
		},
		{
			Tier: mcptools.TierInteract,
			Tool: &mcp.Tool{
				Name: "use_session",
				Description: "Attach to an existing 3270 session by id, so subsequent screen and chaos tools act on it. " +
					"Call list_sessions first to see what is available.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{"type": "string", "description": "Session id from list_sessions."},
					},
					"required":             []string{"id"},
					"additionalProperties": false,
				},
			},
		},
		{
			Tier: mcptools.TierRead,
			Tool: &mcp.Tool{
				Name: "list_tasks",
				Description: "List the Guided Business Tasks saved on this server: named, recorded operations " +
					"such as a balance enquiry, each with the values it needs and the answer it returns. " +
					"Prefer running one of these over driving the screens by hand — a task checks it is on the " +
					"screen it expects before typing, and stops rather than continuing against an unexpected one.",
				InputSchema: noArgsSchema(),
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			},
		},
		{
			Tier: mcptools.TierInteract,
			Tool: &mcp.Tool{
				Name: "run_task",
				Description: "Run a saved Guided Business Task by name against the current 3270 session. " +
					"Each task is also offered as its own tool (task_<name>) with the parameters it declares; " +
					"use this when your client does not show those, or when the name came from list_tasks.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Task name exactly as list_tasks reports it.",
						},
						"parameters": map[string]any{
							"type":                 "object",
							"description":          "Values for the task's declared parameters, as name/value pairs of strings.",
							"additionalProperties": map[string]any{"type": "string"},
						},
					},
					"required":             []string{"name"},
					"additionalProperties": false,
				},
				Annotations: &mcp.ToolAnnotations{
					DestructiveHint: boolPtr(true),
					OpenWorldHint:   boolPtr(true),
				},
			},
		},
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Tool.Name < out[j].Tool.Name })
	return out
}

// addMCPOnlyTool attaches a handler to a declared descriptor, if the tier
// allows it, and reports whether it did.
//
// It panics on a name that is not declared, for the same reason the registry
// panics on an unbound tool: it is a programming mistake, it is the same on
// every machine, and the alternative is a tool that works but is invisible to
// --list-tools.
func addMCPOnlyTool(server *mcp.Server, tier mcptools.Tier, name string, handler mcp.ToolHandler) bool {
	for _, declared := range mcpOnlyTools() {
		if declared.Tool.Name != name {
			continue
		}
		if declared.Tier > tier {
			return false
		}
		server.AddTool(declared.Tool, handler)
		return true
	}
	panic(fmt.Sprintf(
		"mcp: tool %q has a handler but is not declared in mcpOnlyTools(); "+
			"--list-tools would not report it", name))
}
