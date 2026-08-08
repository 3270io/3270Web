# Feature Roadmap

What may come next. This page is a **menu, not a schedule** — an unchecked
item is a candidate, not a commitment, and the order is not a queue.

For what 3270Web provides **today**, see
[Terminal Capabilities](terminal-capabilities.md). That page is the one to
read before an evaluation; this one is for deciding what to build.

## Recently shipped

Newest first. Every item here is live and documented.

- **Guided Business Tasks** — record a screen flow once, and anyone can run
  it from a form and read the answer without navigating a green screen.
  Named inputs, named outputs, and a run that stops at the first divergence
  rather than typing into a screen nobody expected. See
  [Guided Business Tasks](business-tasks.md).
- **Choice of AI provider** — GitHub Copilot, Claude, OpenAI, Google AI,
  Ollama (local or cloud), or any OpenAI-compatible endpoint, selected from
  the chat panel. Each provider keeps its own model and credentials. See
  [AI Providers](ai-providers.md).
- **Customisable keyboard mapping** — rebind by pressing the key, with JSON
  export/import and a `.KMP` keymap-file importer that reports what it could
  not map. See [Keyboard and Controls](keyboard-and-controls.md).
- **IND$FILE file transfer** — send and receive, text or binary, with TSO
  dataset-creation options and PDS member names.
- **Concurrent sessions with tabs** — up to six live host sessions in one
  browser, fully independent.
- **Server-side connection profiles** — per-host TLS, certificate
  verification, LU name, terminal model and code page.
- **Screen tools** — hotspots on the application's own key legends, find
  over the character grid (so it matches typed values), screen history for
  the last 50 screens, screen-accurate and rectangular block copy.
- **Terminal fidelity** — local cursor movement with no host round-trip, a
  real OIA (`X SYSTEM`, `X -f`, insert indicator), numeric-field
  enforcement with an operator-error lock, insert/overtype, and type-ahead.
- **Focus mode and workspace modes** — the terminal fills the display with
  an auto-hiding toolbar rail; Business is the default surface and
  Engineering is one click away.
- **Chaos exploration hardening** — saturation detection, structural screen
  dedup, smarter value generation, automatic exit-key blocking, a Markdown
  discovery report, and mind-map export/import. See
  [Chaos Mode](chaos-mode.md).
- **Host Compatibility Profiler** and **Chaos Mind-Map Compare** — see
  [Host Compatibility Profiler](host-profiler.md) and
  [Chaos Mind-Map Compare](chaos-compare.md).

## Guided Business Tasks

Complete: authoring from a recording, running from a form, the
token-authenticated API, export/import, and conversion from a chaos run. See
[Guided Business Tasks](business-tasks.md).

- [x] **Authoring wizard** — *shipped: record a flow, confirm the derived
      inputs, mark the answer by dragging on the final screen, save. See
      [Guided Business Tasks](business-tasks.md)*
- [x] **Export / import task definitions** — *shipped: `GET /api/v1/tasks`
      returns exactly what `POST /api/v1/tasks` accepts, so the catalogue
      moves between deployments and into version control with no separate
      format*
- [x] **Task API** — *shipped: `POST /api/v1/sessions/{id}/tasks/run`,
      token-authenticated and synchronous, so a bot gets the answer in the
      response. See [REST API](rest-api.md)*
- [x] **Chaos import** — *shipped: `GET /chaos/business/task-draft` converts
      a discovered `BusinessFunction` into a task draft, deriving guards from
      the screen text the run captured. See
      [Guided Business Tasks](business-tasks.md)*

## Daily-use fidelity

Behaviours an experienced 3270 operator expects from the terminal itself.
Tracked apart from features because they are not features — they are whether
the thing behaves like a terminal. The rest of this list is shipping; see
[Terminal Capabilities](terminal-capabilities.md).

- [x] **Strict auto-skip semantics** — *shipped: auto-skip now follows the
      field-attribute rule rather than approximating it as "the field is
      numeric". See
      [Keyboard and Controls](keyboard-and-controls.md#auto-skip)*
- [ ] **Cursor movement over protected areas** — the cursor can only rest in
      an input field, so rows of purely protected text are skipped
- [ ] **Focus mode vs. a MAX-size keypad** — the two ask for opposite things
      and the keypad currently wins, leaving the terminal a tile in the
      corner of a full screen. Which should take priority is a product call,
      not a bug fix

## s3270 actions not yet surfaced

s3270 already supports these — wiring them up is mostly a wrapper job.
`String()`, `Transfer()` and `PrintText()` are already done.

- [ ] **`Snap()`** — point-in-time screen snapshots; enables diffing and
      regression tests
- [ ] **`Query(host|model|cursor|...)`** — richer status surface for the UI
      and the API
- [ ] **`Toggle()` / `Set()`** — runtime resource changes (monocase, blink)
- [ ] **`Source()` / `Macro()` / `Script()`** — native s3270 scripting
- [ ] **`ScreenTrace`** — event-driven screen-change capture for
      observability
- [ ] **Explicit `Disconnect`** — today the subprocess is killed; a graceful
      disconnect is friendlier to upstream proxies

## Enterprise deployment

These gate a production rollout rather than daily usability. A pilot can
proceed without them; procurement cannot.

- [ ] **OAuth / SAML / OIDC SSO** — the entry ticket for BYOD and Azure AD
      organisations. There is no identity model at all today.
- [ ] **Attributable audit logging** — who accessed which host, when.
      Session-scoped logs exist; attributable records do not.
- [ ] **WCAG 2.1 AA conformance statement** — most of the work is done (see
      the accessibility section of
      [Terminal Capabilities](terminal-capabilities.md)); what is missing is
      the audit and the statement
- [ ] **Distributable task and profile libraries** — both are server-side
      already; moving them between deployments is not solved

## Web-native and integration

- [ ] **Embed-in-iframe / SPA integration story**, documented end to end
- [ ] **Mobile / touch UI** — the UI is desktop-first; a touch-friendly
      keypad and hotspots would unlock tablet use
- [ ] **HLLAPI-shape scripting endpoint** — partly solved by the REST API; a
      thinner compatibility wrapper would ease migration of existing HLLAPI
      screen-scrapers

## AI-assisted use

Build on the existing AI panel, chaos and workflow plumbing.

- [ ] **Natural-language → keystrokes** — "describe the screen action you
      want" as a tool call
- [ ] **One-click "Explain this screen"** — the panel already has
      `get_screen`; a button can chain it with a fixed prompt
- [ ] **AI-proposed task authoring** — let the assistant suggest the
      parameters and outputs the wizard asks about, with a human confirming.
      Nothing about Guided Business Tasks should *require* AI — that is the
      difference between a differentiator and a dependency.

## Where the leverage is

Three areas where 3270Web can lead rather than match:

1. **Guided Business Tasks.** They change who can use the product.
   Everything else here makes a terminal better for people who already use
   terminals; this serves people who only know the business question.
2. **Accessibility.** A documented WCAG and screen-reader story is rare in
   this category, and most of the work is already done — deriving each
   field's label from the screen's own text is the hard part, and it exists.
3. **Public REST API plus workflow JSON.** Paired, they make 3270Web
   straightforward to adopt for RPA and CI: a flow recorded by hand is the
   same document an automated job replays, with no separate scripting
   language in between.
