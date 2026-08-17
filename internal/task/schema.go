// SPDX-License-Identifier: AGPL-3.0-or-later

package task

import (
	"fmt"
	"sort"
	"strings"
)

// ToolNamePrefix is what a task's generated tool name begins with, so a
// catalogue entry is never mistaken for a built-in tool.
const ToolNamePrefix = "task_"

// ToolName is the identifier a task is offered under to a tool-calling client.
//
// Task names are prose — "Account balance enquiry" — and a tool name is an
// identifier, so the two cannot be the same string. The mapping is lossy on
// purpose: everything that is not a letter or a digit becomes a single
// underscore, which is what makes "Check balance", "check-balance" and
// "Check  Balance" land on one name rather than three that a model would have
// to guess between. Two tasks that collide after that are reported by
// ToolNames rather than silently overwriting one another.
func ToolName(name string) string {
	var b strings.Builder
	b.Grow(len(name) + len(ToolNamePrefix))
	b.WriteString(ToolNamePrefix)

	lastUnderscore := true // suppresses a leading underscore after the prefix
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}

// ToolNames maps every task to its tool name, dropping any whose name yields
// nothing usable or collides with one already taken.
//
// The dropped entries come back with a reason instead of disappearing. A task
// present in the catalogue and absent from the tool list is otherwise the kind
// of gap nobody notices until someone asks the model to run it and is told it
// does not exist.
func ToolNames(tasks []Task) (map[string]Task, map[string]string) {
	byTool := make(map[string]Task, len(tasks))
	taken := make(map[string]string, len(tasks))
	skipped := map[string]string{}

	// Sorted so a collision resolves the same way on every run: the same task
	// keeps the name each time rather than depending on catalogue order.
	sorted := append([]Task(nil), tasks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	for _, t := range sorted {
		tool := ToolName(t.Name)
		if tool == ToolNamePrefix || tool == strings.TrimRight(ToolNamePrefix, "_") {
			skipped[t.Name] = "the name has no letters or digits to build a tool name from"
			continue
		}
		if other, clash := taken[tool]; clash {
			skipped[t.Name] = fmt.Sprintf("would be called %s, which %q already uses", tool, other)
			continue
		}
		taken[tool] = t.Name
		byTool[tool] = t
	}
	return byTool, skipped
}

// InputSchema projects the task's parameter declarations into a JSON Schema.
//
// A projection of ResolveParameters, not a second definition of it. Every
// constraint here is one that function already enforces, and it stays the
// gate: a client that ignores the schema is rejected server-side with the same
// message the form would have shown. What the schema buys is the rejection
// happening before a half-filled task touches the host.
func (t Task) InputSchema() map[string]any {
	properties := map[string]any{}
	var required []string

	for _, p := range t.Parameters {
		prop := map[string]any{
			"type":        "string",
			"description": p.schemaDescription(),
		}
		if p.MaxLength > 0 {
			prop["maxLength"] = p.MaxLength
		}
		if p.Pattern != "" {
			// Anchored, because ResolveParameters anchors. JSON Schema
			// "pattern" is a search rather than a full match, so the bare
			// expression would let a client accept a value the server then
			// refuses — the worst of both, since the model would have no
			// reason to think it had done anything wrong.
			prop["pattern"] = "^(?:" + p.Pattern + ")$"
		}
		if p.Sensitive {
			// The standard hint for a value that goes in and never comes back.
			prop["writeOnly"] = true
		}
		properties[p.Name] = prop

		// A parameter with a default can be omitted even when it is marked
		// required: ResolveParameters falls back to the default before it
		// checks. Listing it as required anyway would make a client refuse a
		// call the server would have accepted.
		if p.Required && strings.TrimSpace(p.Default) == "" {
			required = append(required, p.Name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
		// ResolveParameters rejects a value for a parameter the task does not
		// declare, on the grounds that it usually means a renamed parameter
		// and a caller that was never updated. The schema says the same.
		"additionalProperties": false,
	}
	if len(required) > 0 {
		sort.Strings(required)
		schema["required"] = required
	}
	return schema
}

// schemaDescription assembles what a model needs to fill this parameter in:
// the prose, then the concrete example, then the default it gets by leaving
// the value out.
func (p Parameter) schemaDescription() string {
	parts := make([]string, 0, 4)
	if d := strings.TrimSpace(p.Description); d != "" {
		parts = append(parts, d)
	} else if label := p.DisplayLabel(); label != p.Name {
		parts = append(parts, label+".")
	}
	if e := strings.TrimSpace(p.Example); e != "" {
		parts = append(parts, fmt.Sprintf("Example: %s.", e))
	}
	if d := strings.TrimSpace(p.Default); d != "" {
		parts = append(parts, fmt.Sprintf("Defaults to %s if omitted.", d))
	}
	if p.Sensitive {
		// Said in the description as well as in writeOnly, because the
		// description is the part a model reliably reads, and a model that
		// echoes a password back into the conversation has leaked it whatever
		// the server does afterwards.
		parts = append(parts, "Sensitive: the value is sent to the host and kept nowhere else. Do not repeat it back.")
	}
	if len(parts) == 0 {
		return p.Name
	}
	return strings.Join(parts, " ")
}

// ToolDescription is what the generated tool tells the model it does.
func (t Task) ToolDescription() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Run the saved Guided Business Task %q against the current 3270 session.", t.Name)
	if d := strings.TrimSpace(t.Description); d != "" {
		b.WriteString(" ")
		b.WriteString(d)
		if !strings.HasSuffix(d, ".") {
			b.WriteString(".")
		}
	}

	// Naming the outputs matters more than it looks: it is the difference
	// between the model calling this because it is the right operation and
	// calling it to see what happens.
	if len(t.Outputs) > 0 {
		labels := make([]string, 0, len(t.Outputs))
		for _, o := range t.Outputs {
			labels = append(labels, o.DisplayLabel())
		}
		fmt.Fprintf(&b, " Returns: %s.", strings.Join(labels, ", "))
	}

	b.WriteString(" Each step checks it is on the screen it expects before typing, ")
	b.WriteString("and stops rather than continuing against an unexpected one.")
	return b.String()
}
