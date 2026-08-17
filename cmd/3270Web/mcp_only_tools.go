// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jnnngs/3270Web/internal/copilot"
	"github.com/jnnngs/3270Web/internal/mcptools"
)

// The tools this server attaches on top of the mcptools registry.
//
// Two of them are MCP's alone. The panel's session is whichever tab you are
// looking at, so list_sessions and use_session have nothing to do in a browser
// and are declared here in full.
//
// list_tasks and run_task are not: they are in copilot.DefaultTools() like any
// other tool, because the chat panel offers them too. What is here is only the
// MCP half of their wiring. The registry lists them as omitted (see
// mcptools.Omitted) precisely so this file can bind them to the handlers in
// mcp_tasks.go, which read the same catalogue that generates the per-task
// task_* tools instead of passing an HTTP body through. Their description and
// schema are taken from the shared catalogue rather than restated, so the two
// surfaces cannot end up describing the same tool differently.
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
			Tool: sharedTool("list_tasks", &mcp.ToolAnnotations{ReadOnlyHint: true}),
		},
		{
			Tier: mcptools.TierInteract,
			Tool: sharedTool("run_task", &mcp.ToolAnnotations{
				DestructiveHint: boolPtr(true),
				OpenWorldHint:   boolPtr(true),
			}),
		},
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Tool.Name < out[j].Tool.Name })
	return out
}

// sharedTool builds an MCP descriptor from the shared catalogue's declaration.
//
// The description and schema are the ones the chat panel is given, so a tool
// that exists on both surfaces is described once. Only the annotations differ,
// because they are an MCP concept and the panel has no use for them.
//
// It panics on a name the catalogue does not declare, for the same reason the
// registry does: the alternative is a tool advertised with an empty
// description, which reads to a model as a tool that does nothing.
func sharedTool(name string, annotations *mcp.ToolAnnotations) *mcp.Tool {
	for _, declared := range copilot.DefaultTools() {
		if declared.Function.Name != name {
			continue
		}
		schema := declared.Function.Parameters
		if schema == nil {
			schema = noArgsSchema()
		}
		return &mcp.Tool{
			Name:        name,
			Description: declared.Function.Description,
			InputSchema: schema,
			Annotations: annotations,
		}
	}
	panic(fmt.Sprintf(
		"mcp: tool %q is bound here but is not in copilot.DefaultTools(); "+
			"declare it there, or give it a descriptor of its own in mcpOnlyTools()", name))
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
