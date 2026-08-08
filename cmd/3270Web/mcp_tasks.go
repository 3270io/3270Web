package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jnnngs/3270Web/internal/mcptools"
	"github.com/jnnngs/3270Web/internal/task"
)

// Guided Business Tasks as MCP tools.
//
// A task is already a validated, parameterised contract — named inputs, a
// guarded path, named outputs — which is most of what a tool descriptor is.
// Offering each one under its own name rather than only through a generic
// run_task(name, parameters) changes what a model can do with the catalogue:
// it can see that "check a balance" exists and what it needs, in the same list
// as every other tool, instead of having to know to go looking. The generic
// pair stays for clients that do not act on tools/list_changed.
//
// These are built at runtime from the catalogue, which is why they do not
// appear in `3270Web mcp --list-tools`: that runs without a target and there
// is no catalogue to read.

// taskTools keeps the generated tools in step with the catalogue.
type taskTools struct {
	server *mcp.Server
	inv    mcptools.Invoker
	held   *sessionHolder
	tier   mcptools.Tier

	mu sync.Mutex
	// current maps a registered tool name to a fingerprint of the descriptor
	// it was registered with.
	current map[string]string
	skipped map[string]string
}

// taskRefreshTimeout bounds a catalogue read. It happens on the path of a
// tool call, and a slow answer must not hold the conversation open.
const taskRefreshTimeout = 5 * time.Second

// addTaskTools registers the task surface and does a first catalogue read.
func addTaskTools(server *mcp.Server, inv mcptools.Invoker, held *sessionHolder, tier mcptools.Tier) *taskTools {
	tt := &taskTools{
		server:  server,
		inv:     inv,
		held:    held,
		tier:    tier,
		current: map[string]string{},
		skipped: map[string]string{},
	}

	addMCPOnlyTool(server, tier, "list_tasks", func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tasks, err := tt.refresh(ctx)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		return textResult(tt.describe(tasks)), nil
	})

	addMCPOnlyTool(server, tier, "run_task", func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Name       string            `json:"name"`
			Parameters map[string]string `json:"parameters"`
		}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return errorResult(fmt.Sprintf("arguments are not valid JSON: %v", err)), nil
			}
		}
		if strings.TrimSpace(args.Name) == "" {
			return errorResult("name is required — call list_tasks to see what is saved"), nil
		}
		return tt.run(ctx, args.Name, args.Parameters), nil
	})

	// A first read at build time, so the tools are in the very first
	// tools/list rather than appearing only after something asks. A failure
	// here is not fatal: the server is still useful without the task
	// catalogue, and list_tasks will report the reason when it is called.
	ctx, cancel := context.WithTimeout(context.Background(), taskRefreshTimeout)
	defer cancel()
	if _, err := tt.refresh(ctx); err != nil {
		log.Printf("3270Web mcp: task catalogue unavailable at startup: %v", err)
	}
	return tt
}

// refresh reads the catalogue and brings the generated tools into line with
// it, adding and removing so the SDK emits tools/list_changed on its own.
func (tt *taskTools) refresh(ctx context.Context) ([]task.Task, error) {
	res, err := tt.inv.Do(ctx, "GET", "/api/v1/tasks", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the task catalogue: %w", err)
	}
	if !res.OK() {
		return nil, fmt.Errorf("could not read the task catalogue: %s", strings.TrimSpace(string(res.Body)))
	}
	var payload struct {
		Tasks []task.Task `json:"tasks"`
	}
	if err := json.Unmarshal(res.Body, &payload); err != nil {
		return nil, fmt.Errorf("the task catalogue did not parse: %w", err)
	}

	byTool, skipped := task.ToolNames(payload.Tasks)

	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.skipped = skipped

	// A task only becomes a tool at a tier that can run one. At TierRead the
	// catalogue is still listed — knowing what the application can do is
	// reading — but there is nothing to call.
	if tt.tier < mcptools.TierInteract {
		return payload.Tasks, nil
	}

	for name, t := range byTool {
		// Registered when it is new *or* when its descriptor moved: a task
		// can be edited in the browser without its name changing, and the
		// schema from the old version would then be wrong. Comparing first
		// is what keeps an unchanged catalogue from emitting a
		// tools/list_changed on every read — a notification storm would
		// train a client to ignore the one that matters.
		descriptor := taskToolFor(name, t)
		print := toolFingerprint(descriptor)
		if tt.current[name] == print {
			continue
		}
		tt.server.AddTool(descriptor, tt.handlerFor(t.Name))
		tt.current[name] = print
	}

	var gone []string
	for name := range tt.current {
		if _, ok := byTool[name]; !ok {
			gone = append(gone, name)
			delete(tt.current, name)
		}
	}
	if len(gone) > 0 {
		sort.Strings(gone)
		tt.server.RemoveTools(gone...)
	}

	return payload.Tasks, nil
}

// toolFingerprint identifies a descriptor by the parts a client sees, so an
// edit that changes nothing visible does not churn the tool list.
func toolFingerprint(t *mcp.Tool) string {
	schema, err := json.Marshal(t.InputSchema)
	if err != nil {
		// Unreachable for a generated schema, and if it ever were reachable
		// the safe answer is "different", which re-registers.
		return t.Description + "\x00" + err.Error()
	}
	return t.Description + "\x00" + string(schema)
}

// taskToolFor builds the descriptor for one task.
func taskToolFor(toolName string, t task.Task) *mcp.Tool {
	return &mcp.Tool{
		Name:        toolName,
		Description: t.ToolDescription(),
		InputSchema: t.InputSchema(),
		Annotations: &mcp.ToolAnnotations{
			// Destructive by default and deliberately not per-task: nothing in
			// a task document says whether it reads a balance or posts a
			// payment, and guessing from the name is exactly the kind of
			// inference that would mark a transfer safe.
			DestructiveHint: boolPtr(true),
			OpenWorldHint:   boolPtr(true),
		},
	}
}

func (tt *taskTools) handlerFor(taskName string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := map[string]string{}
		if len(req.Params.Arguments) > 0 {
			// Decoded as strings because that is what a task parameter is; a
			// model that sends 12345678 as a number gets it coerced rather
			// than a type error it cannot act on.
			var raw map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &raw); err != nil {
				return errorResult(fmt.Sprintf("arguments are not valid JSON: %v", err)), nil
			}
			for k, v := range raw {
				params[k] = coerceToString(v)
			}
		}
		return tt.run(ctx, taskName, params), nil
	}
}

// run executes one task in the held session.
func (tt *taskTools) run(ctx context.Context, name string, params map[string]string) *mcp.CallToolResult {
	sessionID := tt.held.get()
	if sessionID == "" {
		return errorResult(
			"No 3270 session yet. Call connect_session with a hostname to open one, " +
				"or use_session to attach to one that already exists.")
	}

	body := map[string]any{"name": name}
	if len(params) > 0 {
		body["parameters"] = params
	}
	res, err := tt.inv.Do(ctx, "POST",
		"/api/v1/sessions/"+url.PathEscape(sessionID)+"/tasks/run", nil, body)
	if err != nil {
		return errorResult(err.Error())
	}
	if !res.OK() {
		// A rejected parameter or an unknown task is the caller's error and
		// the message says which — passing it through unchanged is what lets
		// the model correct itself instead of retrying the same call.
		return errorResult(strings.TrimSpace(string(res.Body)))
	}

	var result task.Result
	if err := json.Unmarshal(res.Body, &result); err != nil {
		return errorResult(fmt.Sprintf("the run result did not parse: %v", err))
	}
	return textResult(wrapHostData(formatTaskResult(result)))
}

// formatTaskResult renders a run for the model.
//
// Prose rather than the raw JSON: the outputs are the answer and everything
// else is context for a failure, and a model handed the whole document tends
// to relay the step list instead of the number the user asked for.
func formatTaskResult(r task.Result) string {
	var b strings.Builder
	if r.Completed {
		fmt.Fprintf(&b, "Task %q completed in %dms.\n", r.Task, r.DurationMS)
	} else {
		fmt.Fprintf(&b, "Task %q did NOT complete (stopped after %dms).\n", r.Task, r.DurationMS)
	}

	if len(r.Outputs) > 0 {
		b.WriteString("\nResults:\n")
		for _, o := range r.Outputs {
			if o.Found {
				fmt.Fprintf(&b, "  %s: %s\n", o.Label, o.Value)
			} else {
				fmt.Fprintf(&b, "  %s: not found on the final screen\n", o.Label)
			}
		}
	}

	if r.Failure != nil {
		f := r.Failure
		fmt.Fprintf(&b, "\nStopped at step %d: %s\n", f.Step, f.Reason)
		if f.Expected != "" {
			fmt.Fprintf(&b, "  expected: %s\n", f.Expected)
		}
		if f.Actual != "" {
			fmt.Fprintf(&b, "  actual:   %s\n", f.Actual)
		}
		if f.Screen != "" {
			b.WriteString("\nThe screen when it gave up:\n")
			b.WriteString(f.Screen)
			b.WriteString("\n")
		}
		b.WriteString("\nDo not retry the same call unchanged. A guard failing means the " +
			"application was not where the task expected; read the screen and say so.\n")
	}

	if !r.Completed && r.Failure == nil {
		b.WriteString("\nNo failure was recorded, which usually means the run was cancelled.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// describe renders the catalogue, including what could not be offered.
func (tt *taskTools) describe(tasks []task.Task) string {
	if len(tasks) == 0 {
		return "No Guided Business Tasks are saved on this server. " +
			"They are recorded in the browser UI, or contributed by an extension."
	}

	byTool, _ := task.ToolNames(tasks)
	toolFor := make(map[string]string, len(byTool))
	for toolName, t := range byTool {
		toolFor[t.Name] = toolName
	}

	sorted := append([]task.Task(nil), tasks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	fmt.Fprintf(&b, "%d saved task(s):\n", len(sorted))
	for _, t := range sorted {
		fmt.Fprintf(&b, "\n- %s", t.Name)
		if tool, ok := toolFor[t.Name]; ok && tt.tier >= mcptools.TierInteract {
			fmt.Fprintf(&b, "  (tool: %s)", tool)
		}
		b.WriteString("\n")
		if d := strings.TrimSpace(t.Description); d != "" {
			fmt.Fprintf(&b, "    %s\n", d)
		}
		if src := strings.TrimSpace(t.Source); src != "" {
			fmt.Fprintf(&b, "    source: %s\n", src)
		}
		for _, p := range t.Parameters {
			req := "optional"
			if p.Required {
				req = "required"
			}
			fmt.Fprintf(&b, "    parameter %s (%s)", p.Name, req)
			if p.Example != "" {
				fmt.Fprintf(&b, " e.g. %s", p.Example)
			}
			if p.Sensitive {
				b.WriteString(" [sensitive]")
			}
			b.WriteString("\n")
		}
		if len(t.Outputs) > 0 {
			labels := make([]string, 0, len(t.Outputs))
			for _, o := range t.Outputs {
				labels = append(labels, o.DisplayLabel())
			}
			fmt.Fprintf(&b, "    returns: %s\n", strings.Join(labels, ", "))
		}
	}

	tt.mu.Lock()
	skipped := tt.skipped
	tt.mu.Unlock()
	if len(skipped) > 0 {
		names := make([]string, 0, len(skipped))
		for n := range skipped {
			names = append(names, n)
		}
		sort.Strings(names)
		b.WriteString("\nNot offered as their own tool (use run_task by name):\n")
		for _, n := range names {
			fmt.Fprintf(&b, "  %s — %s\n", n, skipped[n])
		}
	}

	if tt.tier < mcptools.TierInteract {
		b.WriteString("\nThe current tool tier is read-only, so none of these can be run. " +
			"Set MCP_TOOLS=interactive to enable them.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// coerceToString flattens a JSON value into the string a task parameter is.
func coerceToString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case nil:
		return ""
	case float64:
		// %v on a float64 gives 1.2345678e+07 for an account number.
		if value == float64(int64(value)) {
			return fmt.Sprintf("%d", int64(value))
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", value), "0"), ".")
	case bool:
		return fmt.Sprintf("%t", value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprintf("%v", value)
		}
		return string(encoded)
	}
}

func boolPtr(b bool) *bool { return &b }
