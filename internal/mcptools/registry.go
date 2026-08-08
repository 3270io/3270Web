// Package mcptools binds 3270Web's tool catalogue to a transport an AI
// client can reach.
//
// The tool *descriptions and schemas* are not defined here — they live in
// internal/copilot, which is what the browser chat panel already uses. This
// package supplies the other half: for each named tool, which HTTP request
// carries it out, which safety tier it belongs to, and how its result is
// presented. Joining the two by name is deliberate. A tool the model is told
// about with nothing behind it fails at the worst possible moment and blames
// the host; a binding for a tool nobody offers is dead code. Both are caught
// at init here rather than in a conversation.
package mcptools

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/jnnngs/3270Web/internal/copilot"
)

// Tier is how much a tool is trusted to do without being asked for.
//
// The ordering is meaningful: each tier includes everything below it.
type Tier int

const (
	// TierRead observes. It reads screens, status, reports and the skill
	// catalogue, and changes nothing on the host.
	TierRead Tier = iota
	// TierInteract drives the session the way a person at the keyboard
	// would: type into a field, press a key, connect somewhere, record what
	// was learned.
	TierInteract
	// TierChaos runs automated exploration, which presses keys chosen partly
	// at random against a live region. It is separated from TierInteract not
	// because a single key press is safer, but because a person pressing keys
	// is watching and an exploration run is not.
	TierChaos
)

// ParseTier maps the MCP_TOOLS setting onto a tier. An unrecognised value
// falls back to the default rather than failing the process, and reports so,
// because a typo in an environment variable should not stop a terminal
// emulator from starting.
func ParseTier(s string) (Tier, bool) {
	switch normalise(s) {
	case "readonly", "read", "read-only":
		return TierRead, true
	case "", "interactive", "interact":
		return TierInteract, true
	case "full", "chaos", "all":
		return TierChaos, true
	}
	return TierInteract, false
}

func (t Tier) String() string {
	switch t {
	case TierRead:
		return "readonly"
	case TierChaos:
		return "full"
	default:
		return "interactive"
	}
}

// Invoker performs one HTTP request against a 3270Web instance.
//
// Everything a tool does goes through this, including in the server's own
// process. Talking to a real listener rather than calling handlers directly
// means the browser UI stays live while a model drives the session — which is
// most of the value of watching an AI work a green screen — and means the
// attached and self-hosted cases exercise exactly the same code.
type Invoker interface {
	Do(ctx context.Context, method, path string, query url.Values, body any) (Response, error)
}

// Response is a raw HTTP reply. Tools interpret it themselves because the
// existing routes return several shapes: JSON objects, a markdown report, a
// workflow file.
type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

// OK reports whether the request succeeded.
func (r Response) OK() bool { return r.Status >= 200 && r.Status < 300 }

// Call carries everything a tool handler needs.
type Call struct {
	Ctx       context.Context
	Inv       Invoker
	SessionID string
	Args      map[string]any
}

// Handler carries out one tool call. The returned string is what the model
// sees. An error means the call could not be attempted at all; a failure
// reported *by* the host is a normal result and should be returned as text,
// because the model can often recover from it and cannot recover from an
// opaque transport error.
type Handler func(Call) (string, error)

// Tool is one entry in the catalogue: the description and schema from
// internal/copilot, plus what this package adds.
type Tool struct {
	Name        string
	Description string
	// InputSchema is copilot's ToolFunction.Parameters, unchanged. It is
	// already a JSON Schema, which is what MCP wants, so there is no second
	// definition to keep in step.
	InputSchema map[string]any

	Tier Tier
	// Destructive marks a tool that can change or destroy host state.
	Destructive bool
	// OpenWorld marks a tool that reaches the mainframe rather than only
	// this process.
	OpenWorld bool
	// HostData marks a result that contains text the host produced, which
	// must be presented to the model as data and not as instructions.
	HostData bool
	// NeedsSession marks a tool that cannot run before a session exists.
	NeedsSession bool

	Handle Handler
}

// ReadOnly reports whether the tool only observes.
func (t Tool) ReadOnly() bool { return t.Tier == TierRead && !t.Destructive }

// omitted are tools declared to the browser panel that have no meaning over
// MCP, with the reason. ask_user asks the operator through the panel's own
// UI; an MCP client is already a conversation with the user and asks them
// directly.
var omitted = map[string]string{
	"ask_user": "an MCP client asks the user directly",
}

// registry is the joined catalogue, built once at init.
var registry map[string]Tool

func init() {
	registry = buildRegistry(copilot.DefaultTools(), bindings())
}

// buildRegistry joins the declared tools to their bindings and refuses to
// produce a half-wired catalogue.
//
// Panicking here is deliberate and is the point of the whole arrangement:
// the failure is a programming mistake, it is the same on every machine, and
// the alternative is a tool that exists in a conversation until the moment
// someone needs it.
func buildRegistry(declared []copilot.Tool, binds map[string]Tool) map[string]Tool {
	out := make(map[string]Tool, len(declared))
	seen := make(map[string]bool, len(declared))

	for _, d := range declared {
		name := d.Function.Name
		seen[name] = true
		if _, skip := omitted[name]; skip {
			continue
		}
		bind, ok := binds[name]
		if !ok {
			panic(fmt.Sprintf(
				"mcptools: tool %q is offered to the model but has no binding. "+
					"Add one in bindings.go, or list it in omitted with the reason.", name))
		}
		bind.Name = name
		bind.Description = d.Function.Description
		bind.InputSchema = d.Function.Parameters
		out[name] = bind
	}

	for name := range binds {
		if !seen[name] {
			panic(fmt.Sprintf(
				"mcptools: binding %q matches no tool in copilot.DefaultTools(); "+
					"it can never be called.", name))
		}
	}
	return out
}

// All returns every bound tool, sorted by name.
func All() []Tool {
	out := make([]Tool, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Enabled returns the tools available at a tier.
func Enabled(t Tier) []Tool {
	var out []Tool
	for _, tool := range All() {
		if tool.Tier <= t {
			out = append(out, tool)
		}
	}
	return out
}

// Lookup finds a tool by name.
func Lookup(name string) (Tool, bool) {
	t, ok := registry[name]
	return t, ok
}

// Omitted returns the declared tools deliberately not bound, and why.
func Omitted() map[string]string {
	out := make(map[string]string, len(omitted))
	for k, v := range omitted {
		out[k] = v
	}
	return out
}
