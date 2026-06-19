package copilot

// DefaultModel is the Copilot model name used when the frontend does not
// supply one. The dot-separated format matches what the GitHub Copilot
// /models endpoint returns and what /chat/completions accepts.
const DefaultModel = "claude-sonnet-4.6"

// SupportedModels is the curated allowlist of Copilot models exposed in
// the chat panel dropdown. We intersect this with whatever /models
// returns at runtime, so unsupported entries Copilot might advertise
// (e.g. preview IDs that 400 on /chat/completions) never reach the UI.
// This mirrors REA's COPILOT_MODELS approach in
// proper-mainframe-agent/webui-server.cjs.
var SupportedModels = []string{
	"claude-opus-4.7",
	"claude-sonnet-4.6",
	"claude-sonnet-4.5",
	"claude-opus-4.5",
	"claude-haiku-4.5",
}

// DefaultSystemPrompt is sent at the head of every chat unless the user
// has customized it. It explains the tool surface and the per-call
// confirmation model so Copilot keeps tool invocations small and
// readable.
const DefaultSystemPrompt = `You are an assistant embedded inside a 3270 mainframe terminal web app. You help the user explore screens, drive transactions, and run the "chaos monkey" automated exploration tool.

The user already has a live session against a real or sample 3270 host. You can:

- Read the current screen with get_screen. The result includes the screen as plain text plus a list of fields with row/column, value, and protection flags. Always look at the screen before suggesting actions.
- Drive the session with send_key (Enter, PF1..PF24, PA1..PA3, Tab, Clear, Reset, ...), write_field (writes text into a single field by row/column), and submit_screen.
- Manage chaos exploration with chaos_status, chaos_start, chaos_stop, chaos_resume, chaos_report, chaos_save_screen_hint, chaos_get_hints, chaos_update_hints, and chaos_export_workflow.
- Build business understanding with chaos_list_screens (review discovered screens), chaos_annotate_screen (record what a screen does and what each field means), business_save_function / business_list_functions (catalog named business operations), and business_generate_workflow (turn a cataloged function plus user values into a workflow JSON file).
- Ask the user to make a decision with ask_user — it presents a question and clickable option buttons; use it whenever you need user input before proceeding.

## Chaos Monkey Skill

When the user asks you to run "chaos monkey", "explore the app", or "discover screens", follow these phases:

**Phase 1 — Read & Review**
1. Call get_screen to see the current screen.
2. Call chaos_get_hints to review existing hints (transaction codes, known values, key blacklist, per-screen hints).
3. Identify any obviously dangerous keys (labels like "Exit", "Logout", "Sign Off") and note their PF numbers.

**Phase 2 — Setup**
4. Call ask_user to choose the run mode:
   - Options: "Full Auto (run, monitor, export automatically)", "Guided (ask me at each key decision)"
5. If dangerous keys were found, call ask_user: "Block these keys to prevent accidental logout/exit?" with options listing the keys + "Block all", "Block none", "Let me choose".
6. Apply the blacklist with chaos_update_hints or chaos_save_screen_hint as needed.

**Phase 3 — Run**
7. Call chaos_start. In guided mode, poll chaos_status every ~20 steps and narrate progress to the user. In full auto, set max_steps=200 and let it run; check status when it stops.

**Phase 4 — Adapt**
8. When chaos finishes, call chaos_status(verbose=true) to examine the mind map and the terminationReason field, which tells you WHY it stopped:
   - "max_steps" / "time_budget": it ran to the configured budget. Offer to resume with a higher budget if coverage looks thin.
   - "saturated": it stopped finding new screens. If saturatedNoProgress is also true, the run discovered NO transitions at all — do NOT just resume (it will only re-saturate); instead add hints (transaction codes, field values, key boosts) or navigate manually first, then resume.
   - "blocked": every usable key was blacklisted for a screen. Relax the key blacklist or add a per-screen hint with the right key, then resume.
   - "error": a host failure stopped the run (see the error field). Report it; resuming may help if it was transient.
9. Identify screens with low visit counts or no productive transitions. Suggest new hints.
10. In guided mode, call ask_user: "Chaos has stopped (<terminationReason>). What next?" with options tailored to the reason above (e.g. "Update hints & resume", "Export workflow & finish", "Generate report first"). Never resume the same run more than twice without changing hints — if nothing new is being discovered, stop and tell the user.

**Phase 5 — Export & Report**
11. Call chaos_report to get the discovery Markdown report.
12. Call chaos_export_workflow to get the 3270Connect-compatible workflow JSON.
13. If new knowledge was gained (new transaction codes, dangerous keys), call chaos_update_hints and chaos_save_screen_hint to persist the learnings for future runs.
14. Run the Business Understanding skill (below) so the discoveries are captured with business meaning, not just coordinates.

## Business Understanding Skill

Chaos discovers *what works* (inputs, keys, screens); your job is to add *what it means*. After a chaos run finishes — or whenever the user asks you to "understand the app", "map the business functions", or similar — build a business model of the application:

**Phase A — Review**
1. Call chaos_list_screens. For each discovered screen, read previewText, fieldMetadata, knownWorkingValues, and keyPresses destinations.
2. Infer the business purpose of each screen (e.g. "Customer account inquiry — enter an account number to view balances") from its preview text, and the meaning of each input field from the on-screen labels near the field's row/column.

**Phase B — Annotate**
3. Call chaos_annotate_screen for each screen you understand: a short business_purpose plus field_semantics keyed by the field key from fieldMetadata (e.g. "R5C20L8": {"name": "account_number", "example": "1234"}). Mark hidden/password fields as sensitive. Annotations persist in the chaos run's mind map.

**Phase C — Catalog business functions**
4. Identify complete business operations by following keyPresses destinations across screens (e.g. menu → entry form → confirmation) and using knownWorkingValues as evidence of what each step accepts.
5. Save each operation with business_save_function: concrete steps (screen_hash, inputs, aid_key, expect_hash) and a parameter for every value a user would supply. Known working values become parameter *examples* — only hard-code a value as a literal input when it is a true constant such as a menu choice or transaction code.

## Performing business functions

When the user asks for a business operation in plain language (e.g. "look up account 1234", "create a new customer"):
1. Call business_list_functions and match the request against the catalog (use chaos_list_screens if you need more context).
2. To do it now on the live session: follow the function's steps with get_screen / write_field / send_key, substituting the user's values, and verify each screen with get_screen before writing.
3. To produce a reusable workflow file (the user says "save", "export", "automate", or asks for a workflow): collect any missing required parameters with ask_user, then call business_generate_workflow and offer the resulting JSON for download.
4. If nothing in the catalog matches, say so and offer to explore with chaos monkey or to navigate manually and record a new function.

## ask_user guidelines
- Use ask_user whenever the user needs to make a real decision before you proceed.
- ask_user always waits for the user even in auto mode — never skip it.
- Keep questions short and options mutually exclusive (2–5 options).
- Include "Continue (I'll decide later)" as a fallback option when appropriate.

## Style guide
- Default to one tool call at a time unless you are in a tight monitoring loop.
- Prefer get_screen before any other tool so you can describe what the user is looking at.
- When suggesting AID keys, cite what the screen labels imply and the row/column.
- Be concise. The chat panel is narrow; lean on bullet lists.
- You have a budget of 30 tool-call rounds per message. Be efficient: combine related observations rather than making redundant get_screen calls.
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
				Description: "Get the current chaos exploration status: whether a run is active, steps completed, transitions, unique screens discovered, and the latest attempt. Pass verbose=true to also include the full mind map and recent-attempt history (heavier payload).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"verbose": map[string]any{
							"type":        "boolean",
							"description": "Include the full mind map and recent attempts in the response. Defaults to false to keep the payload small.",
						},
					},
					"additionalProperties": false,
				},
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
				Name:        "chaos_report",
				Description: "Generate a Markdown discovery report for the current (or most recent) chaos run: screen graph, per-screen field/key statistics, saturation reason, and suggested next experiments.",
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
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "ask_user",
				Description: "Present a question with clickable option buttons to the user and wait for their choice. Use this whenever the user needs to make a decision before you proceed (e.g. run mode, which keys to block, what to do after chaos finishes). ask_user always requires user interaction — it never runs automatically even in auto mode.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"question": map[string]any{
							"type":        "string",
							"description": "The question or decision to present to the user.",
						},
						"options": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "The choices to show as clickable buttons (2–6 options).",
							"minItems":    2,
							"maxItems":    6,
						},
					},
					"required":             []string{"question", "options"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_get_hints",
				Description: "Get all current chaos hints: global transaction hints, the key blacklist, the first-screen hint, and all per-screen hints keyed by screen hash. Review these before starting a chaos run so you know what is already configured and can avoid duplicate work.",
				Parameters:  objNoProps,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_update_hints",
				Description: "Update the global chaos hints (transaction codes + known-good field values) and the global key blacklist. Use after discovering transaction codes or keys that are dangerous (e.g. logout). Per-screen hints set via chaos_save_screen_hint take precedence over the global blacklist.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"hints": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"transaction": map[string]any{
										"type":        "string",
										"description": "Transaction code to enter on the first screen (e.g. MENU, ACCT, INQ).",
									},
									"known_data": map[string]any{
										"type":        "array",
										"items":       map[string]any{"type": "string"},
										"description": "Known-good input values associated with this transaction.",
									},
								},
								"additionalProperties": false,
							},
							"description": "Global transaction hints applied to all runs.",
						},
						"key_blacklist": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "AID keys chaos must never press globally (e.g. PF3 if it logs out). Per-screen blocked_keys override this.",
						},
					},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_export_workflow",
				Description: "Export the current (or most recent) chaos run as a 3270Connect-compatible workflow JSON. Returns the full workflow object including discovered navigation steps, host/port configuration, and timing settings. Call this after a run completes to save the learned paths.",
				Parameters:  objNoProps,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_list_screens",
				Description: "List every screen discovered by chaos exploration: hash, label, visit count, input fields (fieldMetadata keyed R<row>C<col>L<len>), known working values, AID-key destinations, existing business annotations, and a screen preview. Use this to review the application and infer business meaning. Set include_previews=false to shrink the payload once you have already seen the previews.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"include_previews": map[string]any{
							"type":        "boolean",
							"description": "Include truncated screen preview text per screen. Defaults to true.",
						},
					},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "chaos_annotate_screen",
				Description: "Record the business meaning of a discovered screen: what the screen is for (business_purpose) and what each input field means (field_semantics keyed by the field key from chaos_list_screens, e.g. \"R5C20L8\"). Annotations persist in the chaos run's mind map and survive save/load/export.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"screen_hash": map[string]any{
							"type":        "string",
							"description": "Screen hash from chaos_list_screens or chaos_status.",
						},
						"business_purpose": map[string]any{
							"type":        "string",
							"description": "Short business description of the screen, e.g. \"Customer account inquiry — enter account number\".",
						},
						"notes": map[string]any{
							"type":        "string",
							"description": "Optional longer notes: validation rules, quirks, prerequisites.",
						},
						"field_semantics": map[string]any{
							"type":        "object",
							"description": "Map of field key (R<row>C<col>L<len>) to its business meaning.",
							"additionalProperties": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name":        map[string]any{"type": "string", "description": "Business name, e.g. \"account_number\"."},
									"description": map[string]any{"type": "string"},
									"example":     map[string]any{"type": "string", "description": "A known-working example value."},
									"sensitive":   map[string]any{"type": "boolean", "description": "True for passwords/PINs; never echo these in summaries."},
								},
								"required":             []string{"name"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"screen_hash"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "business_list_functions",
				Description: "List the cataloged business functions (name, description, steps, parameters). Call this first when the user asks to perform a business operation in plain language, so you can match their request against what is already known.",
				Parameters:  objNoProps,
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "business_save_function",
				Description: "Add or replace a named business function in the catalog (e.g. \"Account inquiry\"). Describe the operation as concrete steps over discovered screens, and declare a parameter for every value the user supplies at run time. Use literal input values only for true constants (menu choices, transaction codes).",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":        map[string]any{"type": "string", "description": "Business function name, e.g. \"Account inquiry\"."},
						"description": map[string]any{"type": "string", "description": "What the function does, in business terms."},
						"entry_screen_hash": map[string]any{
							"type":        "string",
							"description": "Screen where the function starts. Optional when steps are given.",
						},
						"steps": map[string]any{
							"type":        "array",
							"description": "Ordered screen interactions. Each fills fields on screen_hash then presses aid_key.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"screen_hash": map[string]any{"type": "string"},
									"inputs": map[string]any{
										"type": "array",
										"items": map[string]any{
											"type": "object",
											"properties": map[string]any{
												"field_key": map[string]any{"type": "string", "description": "Field key (R<row>C<col>L<len>)."},
												"value":     map[string]any{"type": "string", "description": "Literal value (constants only)."},
												"parameter": map[string]any{"type": "string", "description": "Name of a declared parameter resolved at generation time."},
											},
											"required":             []string{"field_key"},
											"additionalProperties": false,
										},
									},
									"aid_key":     map[string]any{"type": "string", "description": "AID key to press, e.g. \"Enter\", \"PF3\". Defaults to Enter."},
									"expect_hash": map[string]any{"type": "string", "description": "Screen hash the application should land on; used for CheckValue guards."},
								},
								"required":             []string{"screen_hash"},
								"additionalProperties": false,
							},
						},
						"parameters": map[string]any{
							"type":        "array",
							"description": "User-supplied inputs to the function.",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name":        map[string]any{"type": "string", "description": "Parameter name, e.g. \"account_number\"."},
									"description": map[string]any{"type": "string"},
									"screen_hash": map[string]any{"type": "string", "description": "Screen the parameter is entered on."},
									"field_key":   map[string]any{"type": "string", "description": "Field key (R<row>C<col>L<len>) the value goes into."},
									"example":     map[string]any{"type": "string", "description": "Known-working example value."},
									"required":    map[string]any{"type": "boolean"},
								},
								"required":             []string{"name"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"name"},
					"additionalProperties": false,
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "business_generate_workflow",
				Description: "Generate a business-focused, playback-compatible workflow JSON file from a cataloged business function plus parameter values (e.g. {\"account_number\": \"1234\"}). The result carries Name/Description/BusinessFunction/Parameters metadata and can be downloaded, loaded, and replayed. Collect missing required parameters with ask_user before calling.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string", "description": "Business function name from business_list_functions."},
						"parameters": map[string]any{
							"type":                 "object",
							"description":          "Parameter values keyed by parameter name.",
							"additionalProperties": map[string]any{"type": "string"},
						},
						"host": map[string]any{"type": "string", "description": "Override target host (defaults to the session's host)."},
						"port": map[string]any{"type": "integer", "description": "Override target port (defaults to the session's port)."},
					},
					"required":             []string{"name"},
					"additionalProperties": false,
				},
			},
		},
	}
}
