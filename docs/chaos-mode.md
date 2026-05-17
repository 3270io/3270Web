# Chaos Mode

Chaos mode explores host screens by filling input fields with generated values and submitting AID keys (`Enter`, `Tab`, `PF*`, and others). It is useful for discovering navigation paths and producing reusable workflow JSON.

## Start a Chaos Run

1. Connect to a host.
2. Click **Start chaos exploration** in the toolbar.
3. Watch run activity in:
   - the toolbar (`CHAOS` indicator + stats), and
   - the Workflow Status widget (attempts, writes, transitions, and errors).

## During the Run

Chaos mode continuously:

- Reads the current screen
- Writes generated values into unprotected fields
- Sends an AID key
- Records transition and attempt metadata

You can stop the run at any time with **Stop chaos exploration**.

## Completion Status

When chaos mode ends, the UI shows completed state:

- `CHAOS COMPLETE` indicator in the toolbar
- Final run statistics (steps, transitions, unique screens/inputs)
- Completion details in the Workflow Status widget

This gives immediate confirmation that the run finished and data is ready for export.

## Download the JSON Output

After a run has data, click **Download chaos workflow JSON** in the toolbar.

- The exported file is a workflow JSON compatible with workflow load/playback **and with [3270Connect](https://github.com/3270io/3270Connect) for volume testing** — drop the file into `3270Connect run -config workflow.json` to replay it as a load test against any host. The schema (`Host`, `Port`, `Steps[]` with `Connect`/`FillString`/`PressEnter`/`PressPF<n>`/`Disconnect`) has not changed; only the discovery metadata embedded under `ChaosDiscovery` has new fields (which 3270Connect ignores).
- If a run ID is available, the filename includes it for easier future reference.
- The file is also written automatically to `cfg.OutputFile` when the chaos run terminates for **any** reason (max steps, time budget, saturation, error, or user stop). This is the path most volume-testing setups use.

## Load and Resume Saved Runs

You can reuse previous chaos results:

1. Click **Load previous chaos run**.
2. Pick a saved run from the modal list.
3. Optional: click **Resume chaos exploration from loaded run** to continue discovery.
4. Export JSON again when done.

You can also seed chaos mode directly from a loaded recording:

1. Load a recording in the recording section.
2. Click **Load recording into chaos** in the chaos toolbar section.
3. Start or resume chaos exploration.

When chaos output is saved, its filename is kept separate from the loaded recording filename to avoid overwriting the recording JSON.

## Chaos Hints

Chaos Hints let you guide generated input values during exploration.

1. Click **Edit chaos hints** in the chaos toolbar.
2. Add hint rows with:
   - `Transaction` values (for example, known transaction codes), and/or
   - `Known data` values (comma or newline separated), and/or
   - `Key Assignments` lines in `Label = Key` form (for example, `Return = PF3`, `Confirm = Enter`).
3. Optional: click **Load from recording** to import hint candidates from a previous recording JSON.
4. Click **Save hints** to persist them.
5. Use **Load saved** to reload persisted hints into the modal.

Notes:

- Hints are saved to `chaos-hints.json`.
- Saved hints are automatically applied when starting or resuming chaos if request-level hints are not explicitly supplied.
- Transaction hints are preferred for early field writes, while known data values are reused across fields when they fit field constraints.
- Key assignment hints boost matching keys when the mapped label text appears on the current screen (for example, `Page Forward = PF8`).

## Chaos Settings

Chaos behavior is configurable in **Settings -> Chaos**:

- Max steps
- Time budget
- Step delay
- Random seed
- Max field length
- Optional output file path
- Exclude no-progress events (default on)
- Saturation steps (default 15) — chaos stops early once this many consecutive steps yield no new screen, transition tuple, or value that caused a transition. Set to 0 to disable.
- Dedup mode (default `structural`) — `structural` merges screens that share the same layout signature regardless of echoed values in protected text; `exact` only merges by raw hash.
- Auto-block exit keys (default on) — chaos scans the bottom legend rows for PF labels like *Exit / Quit / Cancel / Logoff / Logout* and refuses to press those keys for the rest of the run.

Use small limits first when testing new host flows, then increase limits for broader exploration.

## Discovery Report

Click **View chaos discovery report** in the toolbar, or call `POST /chaos/report`, for a Markdown summary of the current run:

- Summary line: steps, transitions, unique screens/inputs, termination reason (`max_steps`, `time_budget`, `saturated`, `stopped`, or `error`)
- Coverage stats: new screens and transitions in the last 10 steps, current saturation streak
- ASCII screen graph: nodes are screens (labelled by hash), edges are transitions with the AID key and how often that key produced the move
- Per-screen detail: input fields with success/progression counts, auto-blocked and auto-known keys, list of "working" vs "tried but no progress" AID keys
- Suggested next experiments: per-screen list of untried (and non-auto-blocked) AID keys to try next

The report is also exposed to the Copilot side panel via the `chaos_report` tool.

## Mind Map Export / Import

Chaos learning can carry across sessions:

- `GET /chaos/mindmap/export` returns the engine's current mind map as JSON.
- `POST /chaos/mindmap/import` merges a previously exported mind map into the current engine (rejected while a run is active).

Imported areas overwrite any existing areas with the same hash. Use this to seed a new chaos run with everything a previous run learned about a host's screen layouts and working keys.

## JSONL Transition Log

Set `CHAOS_TRANSITION_LOG_PATH` (or `transition_log_path` in the JSON config body) to a file path and chaos will append one JSON object per attempt to that file (newline-delimited / JSONL). Each line includes the full `Attempt` record: from/to hash, AID key sent, field writes, whether the screen transitioned, and any error. This is intended for offline analysis or feeding into another pipeline.

## Running Chaos via AI Chat

You can drive chaos exploration entirely through the [AI Chat side panel](ai-chat.md) instead of the toolbar. The two approaches share the same underlying engine; the difference is in how you control it.

### Manual toolbar flow vs. AI Chat

| | Manual (toolbar) | AI Chat panel |
|---|---|---|
| Start / stop | Toolbar buttons | Chat message or tool call |
| Monitor progress | Toolbar stats + Workflow Status widget | `chaos_status` tool, streamed to chat |
| Adjust hints mid-run | Edit chaos hints modal | `chaos_save_screen_hint` / `chaos_update_hints` tools |
| Generate report | **View chaos discovery report** button | `chaos_report` tool, rendered in chat |
| Export workflow | **Download chaos workflow JSON** button | `chaos_export_workflow` tool |
| Human decisions | N/A | `ask_user` tool — AI pauses and presents clickable options |

### Typical AI-assisted workflow

1. Open the AI Chat panel and sign in.
2. Say: *"Start a chaos exploration, let me know when it saturates, then give me a discovery report and export the workflow."*
3. The AI uses a five-phase approach:
   - Reads the current screen with `get_screen`.
   - Reviews any existing hints with `chaos_get_hints`.
   - Asks you to choose run mode (full auto vs. guided) via `ask_user`.
   - Starts and monitors the run with `chaos_start` / `chaos_status`.
   - Exports results with `chaos_export_workflow` and summarises findings with `chaos_report`.
4. Approve each tool call with **Run**, or enable **Auto Mode** in the panel header to let the AI proceed without pausing.

### Combining both approaches

Manual and AI-driven chaos use the same run state, so you can mix the two freely:

- Start a run from the toolbar, then ask the AI: *"What screens have been found so far?"* — the AI calls `chaos_status` against the active run.
- Let the AI start a run in Auto Mode overnight, then use the toolbar to review and export results the next morning.
- Add hints through the modal, then tell the AI to resume and push further.

## API Field Naming

Chaos HTTP/MCP tools accept **both** snake_case (the documented public form) and camelCase (the legacy in-browser UI form) for every request body. For example, `chaos_save_screen_hint` accepts `{"screen_hash": "..."}` and `{"screenHash": "..."}` equivalently. Snake_case wins when both are present. New integrations should use snake_case.
