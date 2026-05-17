package copilot

// DefaultModel is the Copilot model name used when the frontend does not
// supply one. claude-opus-4.6 is the strongest model exposed by the
// GitHub Copilot Chat API at time of writing.
const DefaultModel = "claude-opus-4.6"

// DefaultSystemPrompt is sent at the head of every chat unless the user
// has customized it. It explains the tool surface and the per-call
// confirmation model so Copilot keeps tool invocations small and
// readable.
const DefaultSystemPrompt = `You are an assistant embedded inside a 3270 mainframe terminal web app. You help the user explore screens, drive transactions, and run the "chaos monkey" automated exploration tool.

The user already has a live session against a real or sample 3270 host. You can:

- Read the current screen with the get_screen tool. The result includes the screen as plain text (rows separated by \n) plus a list of fields with their row/column, value, and protection flags. Always look at the screen before suggesting actions.
- Drive the session with send_key (Enter, PF1..PF24, PA1..PA3, Tab, Clear, Reset, ...), write_field (writes text into a single field by row/column), and submit_screen (submits all modified fields without sending an AID key).
- Manage chaos exploration with chaos_status, chaos_start, chaos_stop, and chaos_resume. Use chaos_save_screen_hint to record transaction codes, known good input values, and key assignments for the screen the user is currently on; these bias future chaos runs so they explore productive paths.

Style guide:

- Default to one tool call at a time. The user must approve each tool call with a "Run" button before it executes, so chains of unverified calls waste their time.
- Prefer get_screen before any other tool so you can describe what the user is looking at.
- When suggesting AID keys, mention what the screen labels imply (e.g. "PF3 looks like Exit based on the legend at row 23"). Cite row/column numbers when you point at fields.
- Be concise. The chat panel is narrow; lean on bullet lists and short paragraphs.
`

// Tool is the OpenAI-compatible tool wrapper Copilot expects.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction holds the function-call metadata.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// DefaultTools returns the tools exposed to Copilot. The frontend resolves
// each call via the corresponding existing 3270Web HTTP route.
func DefaultTools() []Tool {
	objNoProps := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}

	return []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_screen",
				Description: "Read the current 3270 screen. Returns the screen as ASCII text plus a list of fields (label, row, column, value, protected, numeric, hidden). Call this before any other tool so you know what is on the screen.",
				Parameters:  objNoProps,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "send_key",
				Description: "Send an AID key to the host. Valid keys: Enter, PF1..PF24, PA1..PA3, Tab, BackTab, Clear, Reset, EraseEOF, EraseInput, Home, Up/Down/Left/Right. The host typically advances to a new screen after this.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"key": map[string]any{
							"type":        "string",
							"description": "AID key name (e.g. \"Enter\", \"PF3\", \"Clear\").",
						},
					},
					"required":             []string{"key"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "write_field",
				Description: "Write text into the input field that contains (row, col). Coordinates are 0-indexed. Use get_screen first to see the field map. The text is buffered locally; call submit_screen or send_key to actually transmit.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"row":  map[string]any{"type": "integer", "minimum": 0},
						"col":  map[string]any{"type": "integer", "minimum": 0},
						"text": map[string]any{"type": "string"},
					},
					"required":             []string{"row", "col", "text"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "submit_screen",
				Description: "Submit modified fields and press Enter. Use this after one or more write_field calls when the natural action is Enter; for PF/PA keys, call send_key directly.",
				Parameters:  objNoProps,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_status",
				Description: "Get the current chaos exploration status: whether a run is active, steps completed, transitions, unique screens discovered, and the latest attempt.",
				Parameters:  objNoProps,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_start",
				Description: "Start a chaos exploration run from the current screen. max_steps caps the total submissions (default 100). time_budget_sec caps wall-clock seconds. Use chaos_save_screen_hint first if you want to bias the run.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"max_steps":               map[string]any{"type": "integer", "minimum": 1},
						"time_budget_sec":         map[string]any{"type": "number", "minimum": 1},
						"step_delay_sec":          map[string]any{"type": "number", "minimum": 0},
						"seed":                    map[string]any{"type": "integer"},
						"max_field_length":        map[string]any{"type": "integer", "minimum": 1},
						"screen_dedup_similarity": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
					},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_stop",
				Description: "Stop the running chaos exploration. Has no effect if no run is active.",
				Parameters:  objNoProps,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_resume",
				Description: "Resume a previously loaded chaos run (the user must have already loaded one via the UI).",
				Parameters:  objNoProps,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_save_screen_hint",
				Description: "Persist a hint for the current screen so future chaos runs are smarter. screen_hash identifies the screen (use the firstScreenHash from chaos_status when seeding the initial screen). Provide one or more of known_data, known_keys, blocked_keys.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"screen_hash": map[string]any{
							"type":        "string",
							"description": "Screen hash from chaos_status (firstScreenHash for the starting screen).",
						},
						"known_data": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Known-good input values to seed into unprotected fields.",
						},
						"known_keys": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "AID keys (e.g. \"Enter\", \"PF3\") that are known to make productive progress.",
						},
						"blocked_keys": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "AID keys chaos should never press on this screen (e.g. PF3 if it logs the user out).",
						},
					},
					"required":             []string{"screen_hash"},
					"additionalProperties": false,
				},
			},
		},
	}
}
