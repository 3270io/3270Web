# AI Chat Mode

AI Chat mode is a side panel that lets you drive a 3270 session through conversation. You type instructions in plain language; the AI reads the current screen, fills fields, presses keys, and runs chaos exploration — all with your approval before each action.

## Open the Panel

1. Connect to a host.
2. Click the **Open Copilot chat** button in the toolbar (the chat bubble icon).
3. The side panel slides open on the right.

## Sign In

The first time you open the panel you need to authenticate with GitHub:

1. Click **Sign in with GitHub**.
2. Copy the one-time code shown.
3. Visit the verification URL, enter the code, and approve the access request in your browser.
4. Return to 3270Web — the panel polls automatically and logs you in once you approve.

To sign out at any time, click the **Sign out** button in the panel header.

### GitHub Enterprise

If your organisation uses GitHub Enterprise, click **Set Enterprise URL** and enter your GitHub Enterprise instance URL before signing in.

## Send a Message

Type any instruction in the input box and press **Send** (or ++enter++).

Examples:

- *"Read the current screen and tell me what options are available."*
- *"Navigate to the account inquiry menu and look up account 12345."*
- *"Run a chaos exploration and give me a summary when it finishes."*

The AI reads the current screen before acting, then proposes one tool call at a time.

## Tool Approval

Each AI action requires explicit approval:

- The panel shows a **Run** button before executing any tool call.
- Click **Run** to approve it, or ignore it to skip.
- This prevents unintended writes or key presses.

To let the AI proceed without pausing, enable **Auto Mode** (toggle in the panel header). In Auto Mode the panel runs tool calls automatically without waiting for you to click **Run** each time.

## Choose a Model

The panel header includes a **model selector** dropdown that lists the
Copilot models your GitHub account has access to. The default is
**Claude Opus 4**; switch at any time and the next message uses the
new model. The choice persists across page reloads.

Pick a heavier model (Opus / Sonnet) for screen-reasoning-heavy
sessions and a lighter one for quick reads or repetitive automation.

## Available Actions

The AI can perform the following actions on your 3270 session:

| Action | Description |
|--------|-------------|
| Read screen | Returns the current screen as ASCII text with a full field map (row, col, value, protection flags) |
| Send key | Sends any AID key: `Enter`, `PF1`–`PF24`, `PA1`–`PA3`, `Tab`, `BackTab`, `Clear`, `Reset`, `Home`, arrow keys, and more |
| Write field | Writes text into an unprotected field at a given row and column |
| Submit screen | Writes modified fields then presses `Enter` |

## Chaos Integration

The AI can run and monitor chaos exploration directly from the chat panel. This gives you the same capability as the toolbar controls, but driven by conversation.

| Chat command | Tool used | What it does |
|---|---|---|
| Start exploration | `chaos_start` | Begins automated exploration with configurable step/time limits |
| Stop / Resume | `chaos_stop` / `chaos_resume` | Stops a running run or resumes a loaded one |
| Check progress | `chaos_status` | Returns current steps, transitions, and unique screens |
| Discovery report | `chaos_report` | Markdown report with ASCII screen graph, per-screen stats, and suggested next experiments |
| Save hints | `chaos_save_screen_hint` / `chaos_update_hints` | Adds known transaction codes, data values, or key assignments to guide exploration |
| Export workflow | `chaos_export_workflow` | Downloads learned paths as 3270Connect-compatible JSON |

The default system prompt uses a five-phase workflow: read the screen → review existing hints → ask you to choose run mode → start exploration → export results. You can override this by writing your own instructions.

Manual toolbar controls and the AI panel share the same run state — you can freely mix both. See [Chaos Mode](chaos-mode.md) for full details on toolbar controls, settings, and the discovery report format, and see [Running Chaos via AI Chat](chaos-mode.md#running-chaos-via-ai-chat) for a side-by-side comparison.

## Business Understanding

Chaos exploration discovers *what works* — screens, key presses, and input values. The business-understanding tools let the AI add *what it means*: after a run (or whenever you ask it to "understand the app" or "map the business functions"), the AI reviews each discovered screen, infers its business purpose and the meaning of each input field, and records the result in the chaos mind map.

| Chat command | Tool used | What it does |
|---|---|---|
| Review discovered screens | `chaos_list_screens` | Lists every screen with previews, fields, learned values, key destinations, and existing annotations |
| Annotate a screen | `chaos_annotate_screen` | Records a business purpose (e.g. *"Customer account inquiry"*) and per-field semantics (e.g. `R5C20L8` → `account_number`) |
| Catalog a business function | `business_save_function` | Saves a named multi-screen operation (steps + parameters), e.g. *"Account inquiry"* |
| List the catalog | `business_list_functions` | Returns all cataloged business functions with their parameters |
| Generate a workflow | `business_generate_workflow` | Turns a cataloged function plus parameter values into a downloadable, business-focused workflow JSON |

Annotations and the function catalog are stored inside the chaos run's mind map, so they persist with [saved runs](chaos-mode.md) and travel through mind-map export/import. Knowledge is **per run**: load the annotated run (or import its mind map) in a new session to reuse it.

### Performing business functions by prompt

Once functions are cataloged you can drive them in plain language:

- *"Look up account 1234"* — the AI matches the request against the catalog and drives the live session step by step, verifying each screen before writing.
- *"Create a workflow that looks up an account"* — the AI collects any missing required parameters (via clickable questions), calls `business_generate_workflow`, and offers the resulting JSON for download. The file loads and replays through the standard [workflow](workflow.md) controls.

Generated business workflows carry `Name`, `Description`, `BusinessFunction`, and `Parameters` metadata so they are self-describing; see [Workflow](workflow.md) for the format.

## Known Limitations

- Chat requests and tool endpoints are not rate-limited server-side; very long automated loops are bounded only by the per-message tool budget and your Copilot plan quotas.
- If the model stream fails mid-response, that exchange is not added to the history — re-send the prompt.
- Conversation history is persisted in browser localStorage and capped at the most recent 200 messages.
- Business annotations key on screen hashes, which are specific to the run's mind map. Re-running discovery against a changed application may produce new hashes; re-annotate or re-import the mind map in that case.

## Model Selection

Use the **Model** dropdown in the panel to switch between available Copilot models at any time. The default is Claude Opus 4.

## Clear the Conversation

Click **Clear** in the panel header to remove all messages from the current chat session. A confirmation dialog appears before the history is deleted.

## Keyboard Shortcut

There is no default keyboard shortcut for the panel. All other 3270 key bindings remain active while the panel is open; see [Keyboard and Controls](keyboard-and-controls.md).
