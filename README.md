<picture>
  <source media="(prefers-color-scheme: dark)" srcset="brand/3270web-lockup-600.png">
  <img alt="3270Web" src="brand/3270web-lockup-light-600.png" width="320">
</picture>

**An enterprise-grade IBM 3270 terminal that runs in the browser — and understands
the application behind it.**

3270Web puts a full TN3270 terminal on any browser tab: no emulator install, no
thick client, no desktop rollout. That is the floor, not the point. On top of the
terminal sit three capabilities that a terminal emulator does not have:

| | |
|---|---|
| **Discover the application** | Point AI auto-navigation at a host and let it explore. It drives the screens itself, maps every screen and transition into a navigable graph, and annotates what each screen and field is *for* — cataloguing named business functions like "Account inquiry" or "Post a payment" as it goes. Undocumented green-screen estates map themselves. |
| **Run the business in natural language** | Once discovered, the application is addressable by prompt. *"Look up account 1234"* drives the live session step by step — or emits a self-describing workflow JSON that does it unattended. Business users work the mainframe without learning the screens; every action can require approval, or run hands-free. |
| **Record full screen coverage for load testing** | Discovery does not just document — it captures. Every screen reached and path taken is exported as a 3270Connect-compatible `workflow.json`, so the coverage AI achieved by exploration becomes the input to 3270Connect's performance and volume testing. Realistic mainframe load, generated from the real application rather than guessed at by hand. |

```
3270Web                                            3270Connect
──────────────────────────────────────────         ─────────────────────
browse  →  AI discovers  →  screen graph   ─┐
                            + business fns  ├──→  workflow.json  →  concurrent
run by prompt  ←────────────────────────────┘                       load / volume
                                                                    / CI runs
```

Discovery, business understanding and recording are the same pass. Explore an
application once and you get its map, its business vocabulary, a way to drive it
in English, and the load-test workload — not four separate projects.

## Enterprise readiness

- **No client footprint** — a browser tab per user; nothing to install, package or patch on the desktop.
- **Deploys where you do** — multi-arch Docker image on GHCR (`linux/amd64`, `linux/arm64`), non-root `app` user, `GET /healthz` liveness endpoint wired to the container `HEALTHCHECK`. Windows `.exe` and Linux binaries for on-prem.
- **Hardened by default** — CSP and security headers, origin/referer checks, CSRF token validation, request-body limits, and input validation on every handler.
- **Automatable** — token-guarded REST/JSON API at `/api/v1/*` for RPA bots and CI jobs, opt-in via `API_TOKEN`.
- **Auditable** — detailed per-session logging, exportable discovery reports, and mind-map diffs for migration-readiness checks across environments.
- **Host-agnostic** — the compatibility profiler records what a host actually negotiated, so IBM z/OS and Rocket Enterprise Server estates can be compared like for like.

## Features
- Enterprise-grade browser UI for interactive 3270 sessions, with a virtual keyboard and detailed logging
- AI auto-navigation discovery — automated exploration that maps every screen and transition into a mind map
- Business understanding — per-screen purpose, field meanings, and a catalog of named business functions
- Natural-language operation — drive a live session by prompt, or generate a workflow that replays it
- Full screen-coverage recording, exported as 3270Connect-compatible `workflow.json` for performance and volume testing
- Embedded s3270 binary support (Windows)
- Print screen (renders the current screen via s3270 `PrintText` and opens a printable view in a new tab)
- Public REST/JSON API at `/api/v1/*` for RPA bots and CI jobs (opt-in via `API_TOKEN`; see [docs/rest-api.md](docs/rest-api.md))
- Record sessions to workflow.json, compatible with 3270Connect (Connect/FillString/Press keys/Disconnect)
- Load workflow.json and play it back
- Chaos mode for automated exploration, run persistence, and workflow JSON export
- AI Chat side panel — type instructions in plain language to read screens, fill fields, send keys, and run automated exploration (GitHub Copilot / Claude), with a model selector dropdown in the panel header
- Host Compatibility Profiler at `POST /profile` and `POST /api/v1/sessions/:id/profile` — produces a `CompatibilityProfile` JSON document with the same schema as `3270Connect -profile` ([docs/host-profiler.md](docs/host-profiler.md))
- Chaos Mind-Map Compare at `POST /chaos/mindmap/compare` — diffs two exported mind maps for migration-readiness checks across hosts ([docs/chaos-compare.md](docs/chaos-compare.md))
- Three bundled IBM 3270-style terminal fonts (Regular, Semi-Condensed, Condensed) selectable from Settings ([docs/terminal-fonts.md](docs/terminal-fonts.md))
- Docker image and GHCR workflow
- Windows build script

> **Windows SmartScreen notice**  
> This app is digitally signed.  
> If Windows shows **“protected your PC”**, click **More info → Run anyway**.  
> The warning disappears automatically as usage grows.

## Screenshots

A connected session — terminal, toolbar, recording and chaos controls:

![A connected 3270Web session](docs/images/yorkshire_image.png)

The AI Chat panel, where discovery and natural-language operation happen:

![3270Web AI Chat panel](docs/images/copilot-panel.png)

More — the command palette, settings, keypad and status bar — in
[docs/images/](docs/images/), and rendered in context on the
[documentation site](https://3270Web.3270.io).


## Requirements
- Go 1.22+
- Access to a 3270 host

## Run locally
```bash
go run ./cmd/3270Web
```
Then open http://localhost:8080

## Build Windows EXE
```powershell
.\scripts\build-windows.ps1
```
This produces `3270Web.exe` in the repo root.

## Build Linux binary
```bash
./scripts/build-linux.sh
```
This produces a `3270Web` binary in the repo root. Set `GOARCH`/`GOOS` to cross-compile,
e.g. `GOARCH=arm64 ./scripts/build-linux.sh 3270Web-arm64`.

Or from PowerShell:

```powershell
.\scripts\build-linux.ps1
```
Use `-Goarch arm64` or `-Goos linux -Goarch arm64` for cross-compiles.

## Docker
```bash
# Compose (recommended) — serves on http://127.0.0.1:8080
docker compose up --build

# Or build/run directly:
docker build -t 3270web .
docker run -p 8080:8080 3270web
```
The image is published multi-arch (`linux/amd64`, `linux/arm64`) to
`ghcr.io/3270io/3270web`. It installs the `s3270` package (available at `/usr/bin/s3270`),
runs as a non-root `app` user, and exposes a `GET /healthz` liveness endpoint that the
container's `HEALTHCHECK` polls.

## Recording workflow.json
1. Connect to a host.
2. Click **Start Recording**.
3. Interact with the screen (edits + Enter/PF keys).
4. Click **Stop Recording**.
5. Download `workflow.json`.

To replay a workflow, use **Load workflow and Play** on the session screen.

The output matches the 3270Connect workflow format. See [Workflow Configuration](docs/workflow.md) for file format details.

## Chaos mode

Chaos mode explores screens by writing generated values into input fields and submitting AID keys (`Enter`, `Tab`, `PF*`, and others). It is useful for discovering navigation paths and generating reusable workflow JSON.

The chaos JSON output remains a **3270Connect-compatible workflow** (`Connect` / `FillString` / `PressEnter` / `PressPF<n>` / `Disconnect` steps) — drop a chaos run's JSON straight into `3270Connect run -config workflow.json` to use it as a volume test. The output file is written automatically on any termination reason (max steps, time budget, saturation, error, stop).

Recent additions:
- **Saturation detection** — runs stop early with `terminationReason: "saturated"` once `saturation_steps` (default 15) consecutive attempts produce no new screen, transition, or productive value.
- **Auto-block exit keys** — chaos parses the bottom-row PF legend and blocks Exit/Quit/Cancel/Logoff keys for the rest of the run.
- **Structural mind-map dedup** — screens that differ only by echoed protected text collapse into one area. Set `dedup_mode: "exact"` for the previous raw-hash behaviour.
- **chaos_report tool** — `POST /chaos/report` returns a Markdown discovery report (ASCII screen graph, per-screen stats, suggested experiments).
- **Mind-map export/import** — `GET /chaos/mindmap/export` and `POST /chaos/mindmap/import` let learning carry across sessions.
- **Verbose flag on chaos_status** — pass `?verbose=true` (or `{verbose: true}` to the Copilot tool) for the full mind map; default is a slim summary.
- **JSONL transition log** — set `transition_log_path` (or `CHAOS_TRANSITION_LOG_PATH` env var) and chaos appends one JSON object per attempt to the file.
- **Snake_case + camelCase** — every chaos tool body now binds both forms, so the documented snake_case keys (`screen_hash`, `max_steps`, etc.) work directly without manual translation.

### Toolbar flow
1. Connect to a host.
2. Click **Start chaos exploration**.
3. Monitor progress from:
   - the toolbar indicators/stats, and
   - the Workflow Status widget (attempt details, transitions, errors).
4. Stop with **Stop chaos exploration** or let limits/time budget finish the run.
5. Export with **Download chaos workflow JSON**.

### Saved runs
- Completed runs are persisted and can be loaded from **Load previous chaos run**.
- You can seed chaos from the currently loaded recording with **Load recording into chaos**.
- You can continue a loaded run with **Resume chaos exploration from loaded run**.
- You can clear completed/loaded run state from the toolbar with **Remove chaos run**.
- Run artifacts are stored under the local `chaos-runs/` directory.
- Chaos output files are isolated from loaded recording filenames to avoid overwrite collisions.

### Chaos hints
- Open **Edit chaos hints** from the chaos toolbar.
- Add one or more hints with:
  - a known transaction code, and/or
  - known working data values (comma or newline separated), and/or
  - key assignment mappings (`Screen Label = Key`, for example `Return = PF3`, `Confirm = Enter`).
- Use **Load from recording** in the hints modal to import hint candidates from a previous recording JSON.
- Click **Save hints** to persist hints, or **Load saved** to reload the current saved set.
- Saved hints are stored in `chaos-hints.json` and are automatically used by chaos start/resume when request-level hints are not provided.

### Chaos settings
Chaos behavior can be tuned in **Settings -> Chaos** or via environment values:
- `CHAOS_MAX_STEPS`
- `CHAOS_TIME_BUDGET_SEC`
- `CHAOS_STEP_DELAY_SEC`
- `CHAOS_SEED`
- `CHAOS_MAX_FIELD_LENGTH`
- `CHAOS_LEARNED_INPUT_REUSE_BIAS` (default `1.0`) — weight applied to known-good input values when generating new field writes.
- `CHAOS_LEARNED_KEY_REUSE_BIAS` (default `1.0`) — how often the engine retries AID keys that have previously caused a transition versus exploring untried keys.
- `CHAOS_EXPORT_SUCCESS_BALANCE` (default `1.0`) — balances successful-transition steps against exploratory steps when exporting chaos workflow JSON.
- `CHAOS_OUTPUT_FILE`
- `CHAOS_EXCLUDE_NO_PROGRESS_EVENTS`

See [Chaos Mode](docs/chaos-mode.md) for full details, including the
[Mind-Map Compare](docs/chaos-compare.md) workflow for diffing two
exported maps across hosts.

## AI Chat mode

AI Chat mode is a side panel that lets you drive a 3270 session through conversation with an AI assistant. Type instructions in plain language; the AI reads the current screen, fills fields, presses keys, and runs chaos exploration — pausing before each action for your approval.

### Getting started

1. Connect to a host.
2. Click the **Open Copilot chat** toolbar button.
3. Sign in with GitHub (one-time OAuth device flow — copy a code, visit the verification URL, approve).
4. Type a message, for example: *"Read the current screen and tell me what options are available."*
5. Click **Run** to approve each proposed tool call, or enable **Auto Mode** to let the AI proceed automatically.

### What the AI can do

- Read the current screen (ASCII text + full field map)
- Write text into unprotected fields
- Send any AID key (`Enter`, `PF1`–`PF24`, `PA1`–`PA3`, `Tab`, arrow keys, and more)
- Start, stop, resume, and report on chaos exploration runs
- Export learned navigation paths as 3270Connect-compatible workflow JSON
- Ask you questions with clickable answer buttons (`ask_user` tool)

The model selector dropdown lets you switch between available Copilot models (default: Claude Opus 4). The choice persists across page reloads.

See [AI Chat Mode](docs/ai-chat.md) for full details.

## Host Compatibility Profiler

Probe the active session and capture a `CompatibilityProfile` JSON document with the negotiated terminal model, protocol options, discovered capabilities, and timing:

```bash
# Cookie auth — from the browser session
curl -X POST -b "$SESSION_COOKIE" http://127.0.0.1:8080/profile

# Bearer auth — from external automation
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"collect_raw": true}' \
  http://127.0.0.1:8080/api/v1/sessions/$ID/profile
```

The schema is shared byte-for-byte with `3270Connect -profile`, so profiles produced by either tool diff cleanly against each other — see [docs/host-profiler.md](docs/host-profiler.md) and [docs/compatibility-profile-schema.md](docs/compatibility-profile-schema.md).

## Chaos Mind-Map Compare

`POST /chaos/mindmap/compare` diffs two previously-exported chaos mind maps and returns per-area field and transition deltas plus rolled-up summary counters. JSON by default; pass `Accept: text/html` (or `?format=html`) for the rendered HTML report. See [docs/chaos-compare.md](docs/chaos-compare.md) for the migration-readiness recipe.

## Terminal Fonts

Three bundled IBM 3270-style web fonts ship with the binary — **3270 Regular** (default), **3270 Semi-Condensed**, and **3270 Condensed**. Switch from **Settings -> App -> Terminal Font**. See [docs/terminal-fonts.md](docs/terminal-fonts.md).

## Sample applications
Sample apps now spin up local Go-based 3270 servers (from the 3270Connect examples) and connect via s3270, instead of loading dump files. Use the **Start Example App** button to launch one on the selected port.

## Configuration
The application loads `webapp/WEB-INF/3270Web-config.xml` if present. If missing, defaults are used.

See the [Configuration Reference](docs/configuration.md) for details on available options, including:
- Customizing the `s3270` execution path and model
- Setting a default target host and auto-connect behavior
- Defining custom color schemes and fonts

An optional `.env` file (created with defaults on first run) lets you override the full set of `s3270` command-line options using `S3270_*` environment variables.

## Documentation Site

Project documentation is built with MkDocs and published from `gh-pages` at:

`https://3270Web.3270.io`

Local docs commands:

```bash
pip install -r requirements-docs.txt
mkdocs serve
```

## Brand

The 3270Web mark and its lockups live in [`brand/`](brand/) — SVG, PNG and
`.ico`, in a dark-ground and a light-ground pair, plus a themable SVG that
re-tints with the active palette. They are
generated from the shared kit in the
[3270io](https://github.com/3270io/3270io) repo (`brand/build.mjs`); regenerate
there rather than editing these by hand. The in-app mark is inlined in
`web/templates/brand.html` so it reads `--accent` from whichever theme is
active.
