---
seo_title: "Chaos Mode: automatic 3270 screen discovery in 3270Web"
description: >-
  Chaos mode explores host screens on its own by filling input fields and
  submitting AID keys, discovering navigation paths and producing reusable
  workflow JSON.
---

# Chaos Mode

Chaos mode explores host screens by filling input fields with generated values and submitting AID keys (`Enter`, `Tab`, `PF*`, and others). It is useful for discovering navigation paths and producing reusable workflow JSON.

## How Exploration Chooses Actions

Each step, the engine reads the screen, fills a chosen subset of the
unprotected fields, and presses one AID key. The key is picked by weighted
random selection, where the configured key weights are adjusted by several
learned signals:

- **Learned progressions** — keys that previously moved this screen forward
  are boosted; keys pressed many times with no effect are penalised.
- **Hints** — user-supplied key assignments and per-screen known keys.
- **On-screen legend** — the bottom rows are scanned for `PFn Label` pairs.
  Navigation-style labels (Help/Menu/Next/Prev) are boosted
  (`auto_prefer_navigation_keys`, default on), and exit-style labels
  (Exit/Quit/Logoff) are penalised — and, with `Auto-block exit keys` on,
  refused outright.
- **Novelty** — once a screen has some history, candidate keys that have
  never been pressed from it get a boost, so low-weight keys are not starved
  by an early winner.
- **Frontier steering** — when every candidate key on the current screen has
  been tried, keys whose known destinations lead (over the discovered
  transition graph) toward screens that still have untried keys are boosted.
  A run that wanders into a fully-explored corner finds its way back out
  instead of idling to saturation.

The novelty and frontier boosts are exploration signals: unlike the learned
boosts they are not scaled down by `CHAOS_LEARNED_KEY_REUSE_BIAS`, so setting
that bias low genuinely shifts the run toward exploration. The remaining
exploration frontier is visible while a run is active as `frontierAreas` and
`untriedKeysTotal` in the coverage stats (`GET /chaos/status`), per screen as
`untriedKeys` in `GET /chaos/insights`, and in the discovery report.

A run from start to stop — exploration, the live map, and where it ends up:

![type:video](videos/howto-chaos.webm)

## Start a Chaos Run

1. Connect to a host. With no mainframe to hand, `sampleapp:petstore` is a
   bundled application with enough menus, forms and guarded screens to give
   a run something to map — see
   [Bundled Sample Applications](sample-apps.md).
2. Click **Automation → Start exploration**.
3. Watch run activity in:
   - the active-run strip under the menu bar, and
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

- `CHAOS COMPLETE` in the active-run strip
- Final run statistics (steps, transitions, unique screens/inputs)
- Completion details in the Workflow Status widget

This gives immediate confirmation that the run finished and data is ready for export.

## Download the JSON Output

After a run has data, click **Automation → Download workflow JSON**.

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
2. Click **Automation → Load the recording into chaos**.
3. Start or resume chaos exploration.

When chaos output is saved, its filename is kept separate from the loaded recording filename to avoid overwriting the recording JSON.

## Chaos Hints

Chaos Hints let you guide generated input values during exploration.

1. Click **Automation → Edit hints**.
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

### Learned-value reuse bias

The engine remembers values and AID keys that have produced screen
transitions, then preferentially reuses them on later attempts. Three
env vars (also exposed in **Settings -> Chaos**) tune that bias:

| Setting | Default | Effect |
|---|---|---|
| `CHAOS_LEARNED_INPUT_REUSE_BIAS` | `1.0` | Weight applied to known-good input values when generating new field writes, in the range `0`–`1`. `0` disables reuse and forces fresh values; `1` is the full default reuse bias. |
| `CHAOS_LEARNED_KEY_REUSE_BIAS` | `1.0` | Same idea for AID keys (range `0`–`1`) — how often the engine retries a key that has previously caused a transition versus exploring untried keys. The novelty/frontier exploration boosts are unaffected by this bias. |
| `CHAOS_EXPORT_SUCCESS_BALANCE` | `1.0` | When exporting the chaos workflow JSON, balances steps drawn from successful transitions against exploratory steps. `1.0` is the neutral default; higher values favour reliability, lower values keep more discovery noise in the export. |

Use small limits first when testing new host flows, then increase limits for broader exploration.

## Discovery Report

Click **Automation → Discovery report**, or call `POST /chaos/report`, for a Markdown report of the current (or most recently loaded) run:

- TL;DR and executive summary: steps, transitions, unique screens/inputs, most-visited screen, confirmed working keys, and how many screens still have untried candidate keys (the exploration frontier)
- Global AID key usage table
- A Mermaid flow map of the discovered screens and the keys that move between them
- Per-screen detail: a captured screen example, known working function keys with destinations, untried keys, and input field discovery (allowed data types, observed working values, tried values)
- Any analyst hints configured for each screen

For machine-readable guidance — per-screen productive/dead/untried keys, unproductive fields, conditional transitions, and ranked suggested experiments — call `GET /chaos/insights` (also available to the AI panel as the `chaos_insights` tool).

The report is also exposed to the AI chat side panel via the `chaos_report` tool.

## Mind Map Export / Import

Chaos learning can carry across sessions:

- `GET /chaos/mindmap/export` returns the engine's current mind map as JSON.
- `POST /chaos/mindmap/import` merges a previously exported mind map into the current engine (rejected while a run is active).

Imported areas are merged with existing areas of the same hash: visit counts, key-press statistics, and known working/tried values accumulate, while descriptive fields and business annotations keep the local value when both sides have one. Use this to seed a new chaos run with everything a previous run learned about a host's screen layouts and working keys.

## Business Understanding

The mind map can carry business knowledge on top of the mechanical discovery data. Each area supports a `businessPurpose`, free-form `businessNotes`, and per-field `fieldSemantics` (a business name, description, example, and a sensitive flag per input field, keyed `R<row>C<col>L<len>`). The mind map also holds a catalog of **business functions** — named, parameterized multi-screen operations such as *"Account inquiry"*.

This layer is normally populated by the [AI Chat panel](ai-chat.md#business-understanding) after a run, but the endpoints are plain HTTP and can be driven by any client:

- `GET /chaos/screens` — review every discovered screen (fields, learned values, key destinations, annotations, preview).
- `POST /chaos/screens/annotate` — record a screen's business purpose and field semantics.
- `GET` / `POST /chaos/business/functions` — list or upsert cataloged business functions.
- `POST /chaos/business/generate-workflow` — resolve a function plus parameter values into a playback-compatible workflow JSON.

Because annotations live inside the mind map, they persist with saved runs, survive resume, and travel through mind-map export/import. The catalog is **scoped to a run**: to reuse it in another session, load the annotated run or import its mind map.

## Mind Map Compare

Two exported mind maps can be diffed against each other to surface
divergence between hosts (e.g. IBM z/OS vs Rocket Enterprise Server) or
to confirm a fix did not regress an existing transaction. Send both
documents to `POST /chaos/mindmap/compare` and the endpoint returns
per-area field and transition deltas, plus rolled-up summary counters.
The response is JSON by default; pass `Accept: text/html` (or
`?format=html`) for the HTML report. See
[Chaos Mind-Map Compare](chaos-compare.md) for the full request/response
walkthrough and a migration-readiness recipe.

## JSONL Transition Log

Set `CHAOS_TRANSITION_LOG_PATH` (or `transition_log_path` in the JSON config body) to a **file name** and chaos will append one JSON object per attempt to that file (newline-delimited / JSONL). Each line includes the full `Attempt` record: from/to hash, AID key sent, field writes, whether the screen transitioned, and any error. This is intended for offline analysis or feeding into another pipeline.

!!! note "The server chooses the directory"
    This setting names a file, not a path. The log is written inside the chaos
    runs directory alongside saved runs, and any directory component in the
    value is dropped — `/var/log/chaos.jsonl` and `chaos.jsonl` both produce
    `chaos.jsonl` in the runs directory. The same applies to
    `CHAOS_OUTPUT_FILE` / `outputFile`. To collect these artefacts elsewhere,
    mount the runs directory where you want it — under Docker, add a volume as
    shown in [Installation](installation.md).

## Running Chaos via AI Chat

You can drive chaos exploration entirely through the [AI Chat side panel](ai-chat.md) instead of the menus. The two approaches share the same underlying engine; the difference is in how you control it.

### Manual flow vs. AI Chat

| | Manual (Automation menu) | AI Chat panel |
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

- Start a run from the Automation menu, then ask the AI: *"What screens have been found so far?"* — the AI calls `chaos_status` against the active run.
- Let the AI start a run in Auto Mode overnight, then use the Automation menu to review and export results the next morning.
- Add hints through the modal, then tell the AI to resume and push further.

## API Field Naming

Chaos HTTP/MCP tools accept **both** snake_case (the documented public form) and camelCase (the legacy in-browser UI form) for every request body. For example, `chaos_save_screen_hint` accepts `{"screen_hash": "..."}` and `{"screenHash": "..."}` equivalently. Snake_case wins when both are present. New integrations should use snake_case.

## Running chaos from an AI client

Chaos exploration is available over the [MCP Server](mcp.md), so an assistant
outside the browser can drive it. Two things differ from the menu:

- The tools are in the `full` tier, which is not the default. Exploration
  presses keys unattended, so it is opted into rather than out of.
- `chaos_start` refuses to begin when no key blacklist is configured. The
  first phase of the chaos playbook is to record which keys end the session,
  and a run that presses one stops at step four from a screen you may not be
  able to reach again. Set `MCP_ALLOW_UNGUARDED_CHAOS=1` if an application
  genuinely has no such key.
