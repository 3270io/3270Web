---
seo_title: "Chaos Mode: automatic 3270 screen discovery in 3270Web"
description: >-
  Chaos mode explores host screens on its own by filling input fields and
  submitting AID keys, discovering navigation paths and producing reusable
  workflow JSON.
---

# Chaos Mode

Chaos mode explores host screens by filling input fields with generated values and submitting AID keys (`Enter`, `Tab`, `PF*`, and others). It is useful for discovering navigation paths and producing reusable workflow JSON.

## Before You Start: This Types Into a Live Host

!!! danger "Chaos mode submits real input to the host you are connected to"
    A chaos run is not a simulation and it is not read-only. Every step
    **types generated values into the unprotected fields of the current
    screen and presses a key that submits them**, on the session you are
    signed in to, with your user's authority. The host cannot tell the
    difference between that and you typing.

    On an application that updates data, that means a run can create,
    change or delete records, submit transactions, send messages, use up a
    sign-on attempt, or lock an account — whatever the screens it reaches
    allow your user to do.

Treat a run the way you would treat handing the keyboard to someone who
presses keys at random:

- **Use a test or training system, or a user that can only read.** This is
  the only complete protection. Do not point a run at production with an
  account that can update anything you would mind being updated.
- **Block the keys that do damage before the first run.** `PF3` and `Clear`
  are left out of the default key set because they so often sign the user
  off or wipe the screen, but the keys that commit, delete or confirm on
  *your* application are yours to name. Add them under **Automation → Edit
  hints** (*keys chaos must never press*), or per screen in the screen
  hints. Exit-style keys named in a screen's own key legend are blocked
  automatically, but only when the legend says so in words the run
  recognises.
- **Paging keys are the one addition the run makes for itself.** The
  default key set has no `PF7` or `PF8`. Where a screen's own legend labels
  a key as paging (*Next*, *Prev*, *Forward*, *Backward*, *Up*, *Down*,
  *Scroll*, *More*), the run may press that key on that screen so that a
  list is seen past its first page. A key the legend labels as leaving the
  screen (*Back*, *Menu*) is not added this way, and a blocked key stays
  blocked.
- **Start small.** A first run of 20–50 steps with a one-second delay shows
  you where the run goes before it has gone far. Raise the limits once you
  have seen it stay where you want it.
- **Check what the start dialog shows.** It names the host the run will
  type into, the step and time limits, and the keys that are blocked. If
  the host is not the one you meant, or no key is blocked, stop there.
- **Watch the first run, and know where Stop is.** The **Stop** button is
  on the run strip under the menu bar for as long as a run is active.
  Stopping takes effect before the next step.
- **Hidden fields are typed into too.** A run that reaches a sign-on screen
  will try values in the password field; repeated wrong attempts can lock
  the account. What it learns there is never shown back to you — see
  [Hidden Fields](#hidden-fields).

A run started from an AI client is subject to the same rules, and
`chaos_start` over MCP refuses to begin with no blocked keys at all — see
[Running chaos from an AI client](#running-chaos-from-an-ai-client).

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
  (Exit/Quit/Logoff) are penalised — and, with `auto_block_exit_keys` on
  (the default), refused outright.
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
2. Click **Automation → Start exploration**. A start dialog shows the host
   the run will type into, the step and time limits it will run under, and
   the keys it is blocked from pressing, with the warning above in short.
   Nothing has been sent to the host at this point.
3. Confirm to start, or cancel to change the limits or the blocked keys
   first.
4. Watch run activity in:
   - the active-run strip under the menu bar, and
   - the Workflow Status widget (attempts, writes, transitions, and errors).

## During the Run

Chaos mode continuously:

- Reads the current screen
- Writes generated values into unprotected fields
- Sends an AID key
- Records transition and attempt metadata

Exploration drives the same connection as the terminal you are looking at, so
the terminal follows the run: each screen the engine reaches is shown as it
happens. The terminal does not repaint for a moment after you type into it, so
text you are typing is not wiped out — but keys you send during a run go to the
same host session the run is using, so stop the run first if you want to work
in the terminal.

You can stop the run at any time with the **Stop** button on the run strip.

## When a Run Ends

A run ends for one of these reasons, and says which:

| Reason (`terminationReason`) | What happened |
|---|---|
| `max_steps` | It reached its step limit. |
| `time_budget` | It ran out of time. |
| `saturated` | Many steps in a row found nothing new (15 by default). With `saturatedNoProgress`, it never got off the first screen at all. |
| `stopped` | It was stopped by hand. |
| `blocked` | Every key it could press on the current screen is blocked. |
| `error` | The connection to the host failed. |

When it does, a completion panel replaces the run strip's live counters. It
shows:

- why the run ended, in a sentence;
- the final statistics (steps, transitions, unique screens and inputs);
- the next actions: **Continue exploring**, view the map, open the
  discovery report, download the workflow JSON.

The reason stays with the run. It is still reported by `GET /chaos/status`
after the run has finished, it is saved with the run, and it is shown again
when the run is loaded on another day.

**Continue exploring** carries on from where the run stopped, with a fresh
allowance of steps and time on top of what the run has already used. It
replaces the separate resume and extend actions earlier versions had. If a
run saturated without leaving its first screen, continuing unchanged will
end the same way: give it a value or a key that works there — a
[hint](#chaos-hints), or a recording — first.

As soon as a run has stopped — whether it finished, you stopped it, or it was
started from the AI panel or the API rather than the menu — the Automation menu
offers **Discovery report**, **Download workflow JSON** and **Continue exploring**. They do not wait for you to load a saved run.

### What the counts mean

The same run is described in the active-run strip, the Screen map, the Flow
diagram and the Discovery report. They use the same terms:

| Term | What it counts |
|---|---|
| **Steps** | Each time the engine filled fields and pressed a key. |
| **Unique screens** | The distinct screens the run recorded, **including the screen it started on**. Screens that differ only in values the host echoes back are one screen (Dedup mode `structural`, the default). |
| **Transitions** | Distinct moves from one screen to another by a particular key. The same two screens reached by Enter and by PF8 are two transitions. |
| **Primary flow edges** (Flow diagram) | The discovery path drawn on the canvas: for each screen after the first, the link that most directly led to it. Every other link between screens is listed as an additional transition. |

The flow counts links, not keys: Enter and PF8 between the same two screens are
two transitions in the strip and the report but one link in the flow, so the flow
can show fewer edges than there are transitions.

The **Screen map** lists every screen except the one the run started from: its
hints live under **Exploration hints**, and the map's caption says so. The **Flow
diagram** draws the starting screen too, so it counts one more screen than the
map lists. With Dedup mode set to `exact` the run counts each exact screen
separately, while the map and flow draw screens that share a layout as one.

## Download the JSON Output

After a run has data, click **Automation → Download workflow JSON**.

- The exported file is a workflow JSON compatible with workflow load/playback **and with [3270Connect](https://github.com/3270io/3270Connect) for volume testing** — drop the file into `3270Connect run -config workflow.json` to replay it as a load test against any host. The schema (`Host`, `Port`, `Steps[]` with `Connect`/`FillString`/`PressEnter`/`PressPF<n>`/`Disconnect`) has not changed; only the discovery metadata embedded under `ChaosDiscovery` has new fields (which 3270Connect ignores).
- If a run ID is available, the filename includes it for easier future reference.
- The file is also written automatically to `cfg.OutputFile` when the chaos run terminates for **any** reason (max steps, time budget, saturation, error, or user stop). This is the path most volume-testing setups use.

## Saved Runs

Every run is saved when it ends. **Automation → Saved chaos runs** lists
them, newest first, with the host each explored, why it ended, and its
name if you gave it one.

From that dialog you can:

- **Load** a run into the session — its map, report and export become
  available as if it had just finished. Loading is refused while a run is
  active (stop it, or let it finish).
- **Continue exploring** from a loaded run.
- **Name** a run, so the list is more than a column of timestamps.
- **Compare** two runs — see [Mind Map Compare](#mind-map-compare).
- **Delete** runs you no longer need.

The newest 100 runs are kept.

You can also seed chaos mode directly from a loaded recording:

1. Load a recording in the recording section.
2. Click **Automation → Load the recording into chaos**.
3. Start exploring.

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

- Two fields block keys. **Blocked keys, on every screen** applies throughout the run; **Blocked on the first screen only** applies just to the first screen of a start or resume, and is for the key that is fine later but not at the sign-on screen. The grey italic text in an empty field is an example (it starts "e.g."), not a value.
- Hints are saved to `chaos-hints.json`.
- Saved hints are automatically applied when starting or continuing a run if request-level hints are not explicitly supplied.
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
- Screen dedup similarity
- Exclude no-progress events (default on)
- Force override of existing inputs (default on)

These are the defaults for every run in the deployment — one started from
the menu, one continued from a saved run, and one started over the API with
no body. A request body overrides them for that run only.

Three further options have no entry in Settings. They are per-run options,
set in the body of `POST /chaos/start` or `POST /chaos/resume` (and by the
matching arguments of the `chaos_start` tool):

| Option | Default | Effect |
|---|---|---|
| `saturation_steps` | `15` | The run stops early once this many consecutive steps yield no new screen, transition, or value that caused a transition. |
| `dedup_mode` | `structural` | `structural` treats screens with the same layout as one screen regardless of the values echoed on them; `exact` only merges identical screens. |
| `auto_block_exit_keys` | `true` | The run scans the bottom legend rows for PF labels like *Exit / Quit / Cancel / Logoff / Logout* and refuses to press those keys on that screen. |

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

## Screen Captures

The Screen map, the Flow diagram and the screen-hints editor show one captured
example of each screen. A capture is taken every time the run visits a screen,
so a screen that has been visited has one. A screen without one says so, and
that means no capture was kept for it: a run that visits the screen again takes
one. A screen that came from **Load the recording into chaos** never has one,
because a recording holds no screens to capture.

## Discovery Report

Click **Automation → Discovery report**, or call `POST /chaos/report`, for a Markdown report of the current (or most recently loaded) run:

- TL;DR: attempts, how many times the screen changed in total and along how
  many distinct transitions, why the run ended, unique screens and inputs,
  the most-visited screen, and how many screens still have keys the run
  could press but never did (the exploration frontier)
- Run summary and the **configuration used**: step and time limits, the
  keys the run could press, the keys it was blocked from pressing, and how
  many hints it had
- A Mermaid flow map of the discovered screens and the keys that move between them
- Screen inventory, with screens that are one screen seen at more than one
  terminal size grouped together
- Global AID key usage table
- Per-screen detail: business purpose and notes where the screen has been
  annotated, a captured screen example, known working function keys with
  destinations, untried and blocked keys, and input field discovery (field
  names, allowed data types, observed working values, tried values)
- Business functions catalogued for the run
- **What to do next**: what the reason the run ended suggests, then the
  highest-value suggested experiments

"Untried" is measured against the keys *that run* could press — its own key
set, less its blocked keys and the keys blocked on that screen — not
against the built-in defaults.

For machine-readable guidance — per-screen productive/dead/untried keys, unproductive fields, conditional transitions, and ranked suggested experiments — call `GET /chaos/insights` (also available to the AI panel as the `chaos_insights` tool). Its `run` object reports whether a run is active, how far it got and why it ended, and is present for a finished or loaded run as well as a live one.

The report is also exposed to the AI chat side panel via the `chaos_report` tool.

## Hidden Fields

A field the host marks non-display — a password field, in practice — is
explored like any other, and the run has to remember what it typed there:
a value that gets past a sign-on screen is the most useful thing it knows.
That memory is used, and never shown:

- In the map, the screens list, the status and the attempt log, a value
  typed into a hidden field appears as `********`. You can see that a
  working value is known; you cannot see what it is.
- The discovery report says *Known, withheld (hidden field)*.
- A mind-map export leaves the values out altogether, so an exported file
  can be shared or attached to a ticket.
- A field you annotate as **sensitive** is treated the same way, whatever
  the host says about it.

Two things follow. A map exported from one session and imported into
another arrives without the hidden values, so the new session has to be
given them again (a hint, or a recording). And the saved run files on the
server do contain them, because continuing a run needs them: protect the
chaos runs directory as you would any file holding credentials.

When a recording is loaded into chaos, the fills it marks sensitive — a
parameter flagged *Sensitive*, or a value taken from a sensitive variable —
are seeded as hidden fields. An ordinary recorded fill carries no such
mark, so a password typed into a recording that does not flag it is seeded
as an ordinary value: flag it in the recording first.

## Mind Map Export / Import

Chaos learning can carry across sessions. The chaos map dialog has
**Export map** and **Import map** buttons; the same operations are
available over HTTP:

- `GET /chaos/mindmap/export` returns the session's mind map as JSON. It
  works during a run, after one has finished, and for a loaded run. With no
  run in the session at all it answers `404`.
- `POST /chaos/mindmap/import` merges a previously exported mind map into
  the session's run. It is refused with `409` while a run is active; at any
  other time it succeeds:
    - with a finished or loaded run, the map is merged into it;
    - with no run at all, the imported map becomes a run you can continue.

  The response reports `areasImported`, `areasTotal` and which of those
  happened (`target`: `loadedRun`, `newRun`, or `engine`).

Imported areas are merged with existing areas of the same hash: visit counts, key-press statistics, and known working/tried values accumulate, while descriptive fields and business annotations keep the local value when both sides have one. Use this to start a run with everything a previous run learned about a host's screen layouts and working keys: import the map, then **Continue exploring**. The merged map is saved with the run that continues from it.

## Business Understanding

The mind map can carry business knowledge on top of the mechanical discovery data. Each area supports a `businessPurpose`, free-form `businessNotes`, and per-field `fieldSemantics` (a business name, description, example, and a sensitive flag per input field, keyed `R<row>C<col>L<len>`). The mind map also holds a catalog of **business functions** — named, parameterized multi-screen operations such as *"Account inquiry"*.

This layer is normally populated by the [AI Chat panel](ai-chat.md#business-understanding) after a run, but the endpoints are plain HTTP and can be driven by any client:

- `GET /chaos/screens` — review every discovered screen (fields, learned values, key destinations, annotations, preview).
- `POST /chaos/screens/annotate` — record a screen's business purpose and field semantics.
- `GET` / `POST /chaos/business/functions` — list or upsert cataloged business functions.
- `POST /chaos/business/generate-workflow` — resolve a function plus parameter values into a playback-compatible workflow JSON.

Because annotations live inside the mind map, they persist with saved runs, survive continuing a run, and travel through mind-map export/import. The catalog is **scoped to a run**: to reuse it in another session, load the annotated run or import its mind map.

## Mind Map Compare

Two runs can be diffed against each other to surface divergence between
two hosts (a mainframe and the platform replacing it, say) or to confirm a
fix did not regress an existing transaction.

In the **Saved chaos runs** dialog, select two runs and choose **Compare**.
Over HTTP, send `POST /chaos/mindmap/compare` either the IDs of two saved
runs —

```json
{ "baseRunId": "20260301-090000-ab12cd34", "candidateRunId": "20260308-090000-ef34ab56" }
```

— or two exported mind-map documents as `baseline` and `candidate`, or one
of each. The endpoint returns per-area field and transition deltas, plus
rolled-up summary counters. The response is JSON by default; pass
`Accept: text/html` (or `?format=html`) for the HTML report. See
[Chaos Mind-Map Compare](chaos-compare.md) for the full request/response
walkthrough and a migration-readiness recipe.

## HTTP Endpoints

The browser drives chaos mode through these endpoints, and any client with
a session can do the same. The [REST API](rest-api.md) lists the full set;
the ones below are the run lifecycle.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/chaos/start` | Start a run. The body is optional: empty means the configured defaults. |
| `POST` | `/chaos/stop` | Stop the active run. |
| `GET` | `/chaos/status` | The run's state. `?verbose=true` adds the full mind map and the recent attempts. |
| `POST` | `/chaos/resume` | Continue from the finished or loaded run. Same body as start; `extend_limits: true` makes `max_steps` an allowance on top of the steps already run. |
| `GET` | `/chaos/runs` | Saved runs, newest first. |
| `POST` | `/chaos/load` | Load a saved run. Body: `{"runID": "..."}`. `409` while a run is active. |
| `POST` | `/chaos/runs/label` | Name a saved run. Body: `{"id": "...", "label": "..."}`; an empty label clears it. |
| `POST` | `/chaos/runs/delete` | Delete a saved run. Body: `{"runID": "..."}`. |
| `POST` | `/chaos/remove` | Clear the finished or loaded run from the session. |
| `GET` | `/chaos/mindmap/export` | Export the session's mind map. |
| `POST` | `/chaos/mindmap/import` | Merge an exported mind map into the session's run. |
| `POST` | `/chaos/mindmap/compare` | Diff two runs or two exported maps. |

### A body that does not parse starts nothing

`POST /chaos/start` and `POST /chaos/resume` answer **`400`** when a body
is present and cannot be read as the options they take — invalid JSON, or
an option of the wrong type, such as a number sent as a string:

```json
{ "max_steps": "50", "key_blacklist": ["PF3"] }
```

```json
{
  "error": "invalid chaos options, nothing was started: option \"max_steps\" must be a number, not a string",
  "code": "chaos_invalid_options"
}
```

Earlier versions ignored a body like that and started a run with the
default limits and nothing blocked. If you have a script that relied on a
malformed body being ignored, it was starting runs you did not configure:
fix the body. An empty body still means "use the defaults".

### What status reports

`GET /chaos/status` answers in every state — before any run, during one,
after one has finished, and for a loaded run. Alongside the counters:

| Field | When | Meaning |
|---|---|---|
| `active` | always | Whether a run is going. |
| `targetHost` | always | The host the run explored (`host:port`); with no run, the host the session is connected to. |
| `sessionTargetHost` | always | The host the session is connected to now — different from `targetHost` when a run from another host is loaded. |
| `terminationReason`, `terminationSummary` | once a run has ended | The reason code from the table above, and the same in a sentence. |
| `saturatedNoProgress` | when true | The run saturated without leaving its first screen. |
| `coverageStats` | while active | Recent progress and the remaining frontier (`frontierAreas`, `untriedKeysTotal`). |
| `config` | for a run started in this session | The limits, the keys the run could press and the keys it was blocked from. |
| `label` | when set | The saved run's name. |
| `loadedRunID` | once saved or loaded | The saved run's ID. |
| `mindMap` | when there is one | A summary per screen; the full map with `?verbose=true`. |

Entries in `GET /chaos/runs` carry `terminationReason`, `targetHost` and
`label` in the same way.

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
| Start / stop | Start dialog, **Stop** on the run strip | Chat message or tool call |
| Monitor progress | Run strip + Workflow Status widget | `chaos_status` tool, streamed to chat |
| Adjust hints mid-run | **Edit hints** dialog | `chaos_save_screen_hint` / `chaos_update_hints` tools |
| Generate report | **Automation → Discovery report** | `chaos_report` tool, rendered in chat |
| Export workflow | **Automation → Download workflow JSON** | `chaos_export_workflow` tool |
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
4. Approve each tool call with **Run**, or enable **Auto Mode** in the panel header to let the AI proceed without pausing. The card for `chaos_start` names the host the run will press keys on.

### Combining both approaches

Manual and AI-driven chaos use the same run state, so you can mix the two freely:

- Start a run from the Automation menu, then ask the AI: *"What screens have been found so far?"* — the AI calls `chaos_status` against the active run.
- Let the AI start a run in Auto Mode overnight, then use the Automation menu to review and export results the next morning.
- Add hints through the dialog, then tell the AI to continue the run and push further.

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
