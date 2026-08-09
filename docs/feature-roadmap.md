# Feature Roadmap

What may come next. This page is a **menu, not a schedule** — an unchecked
item is a candidate, not a commitment, and the order is not a queue.

For what 3270Web provides **today**, see
[Terminal Capabilities](terminal-capabilities.md). That page is the one to
read before an evaluation; this one is for deciding what to build.

## Recently shipped

Newest first. Every item here is live and documented.

- **An accessibility claim with an audit behind it** — thirteen surfaces
  tested, four conformance failures fixed, and a statement that says where it
  falls short rather than only where it does not. See
  [Accessibility](accessibility.md).
- **A door in the shape of the old one** — an HLLAPI-shaped endpoint, so a
  screen-scraper written against numbered functions and presentation-space
  positions can be pointed at 3270Web without being rewritten. See
  [REST API](rest-api.md#post-apiv1sessionsidhllapi).
- **Touch** — a bar of terminal keys within a thumb's reach on a tablet or a
  phone, riding above the software keyboard rather than behind it, and a tap
  on protected text that places the cursor there. Without an AID key a device
  with no keyboard could read a screen and never end one. See
  [Keyboard and Controls](keyboard-and-controls.md#touch-devices).
- **The terminal inside another application** — a named allowlist of origins
  that may frame 3270Web or call its API from a page, a chrome-less
  `?embed=1` rendering, and a postMessage channel for the page around the
  frame. Documented end to end, including why HTTPS is not optional for it.
  See [Embedding 3270Web](embedding.md).
- **Screen snapshots, display toggles and screen tracing** — the screen frozen
  under a name and diffed row by row, so a flow can be checked against the
  screen it used to land on; the terminal's own display settings read and
  written where they live; and every screen recorded as it is drawn,
  including the ones replaced before anyone asked to see them. See
  [REST API](rest-api.md).
- **The connection's own account of itself** — negotiated telnet options, TLS
  state, terminal name and byte counts, none of which the screen shows, in a
  **Connection** panel one click from the terminal and on the API for scripted
  checks; and a graceful host disconnect on teardown rather than a killed
  subprocess. See
  [Keyboard and Controls](keyboard-and-controls.md#connection-details).
- **A cursor that is not confined to the fields** — it can rest on any cell of
  the display, so screens driven by cursor position rather than field content
  are operable; and auto-skip now follows the field-attribute rule instead of
  approximating it. See [Keyboard and Controls](keyboard-and-controls.md).
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
- [x] **Cursor movement over protected areas** — *shipped: the cursor can rest
      on any cell of the display, which is what makes "position the cursor
      beside your choice" screens operable. See
      [Keyboard and Controls](keyboard-and-controls.md#cursor-on-protected-text)*
- [ ] **Focus mode vs. a MAX-size keypad** — the two ask for opposite things
      and the keypad currently wins, leaving the terminal a tile in the
      corner of a full screen. Which should take priority is a product call,
      not a bug fix

## s3270 actions not yet surfaced

s3270 already supports these — wiring them up is mostly a wrapper job.
`String()`, `Transfer()` and `PrintText()` are already done.

- [x] **`Query`** — *shipped: `GET /api/v1/sessions/:id/query` returns
      everything the terminal knows about the connection — negotiated telnet
      options, TLS state, terminal name, byte counts — none of which is on the
      screen. See [REST API](rest-api.md)*
- [x] **Explicit `Disconnect`** — *shipped: teardown closes the host session
      before the subprocess goes away, instead of killing it and leaving the
      TCP connection for a gateway to notice in its own time*
- [x] **`Snap()`** — *shipped: the screen frozen under a name and compared row
      by row, against another snapshot or against the screen as it stands now.
      That is what makes a regression test against a green screen possible:
      the answer is which rows moved, not pass or fail. See
      [REST API](rest-api.md)*
- [x] **`Toggle()` / `Set()`** — *shipped: the display toggles this build
      actually has — monocase, crosshair, cursor blink, the underscore under
      input fields — read from and written to the terminal rather than
      mirrored here. A narrow allowlist, because the same action also reaches
      trace files and printer sessions*
- [ ] **`Source()` / `Macro()` / `Script()`** — native s3270 scripting. Held
      deliberately rather than pending: `Source()` reads a file of actions,
      `Script()` starts a process, and a macro would run on the same control
      pipe the session depends on. What they are wanted *for* — running a
      recorded sequence against a host — is already
      [Guided Business Tasks](business-tasks.md) and workflow replay, over
      validated steps rather than raw actions. The open question is whether
      any remaining case justifies the surface
- [x] **`ScreenTrace`** — *shipped: every screen recorded as it is drawn,
      including the ones the host replaced before anyone asked to see them —
      the screens a poller can never find. Behind `ALLOW_SCREEN_TRACE`,
      because it writes a file holding everything that crossed the display.
      See [REST API](rest-api.md)*
- [x] **A host-details panel in the browser** — *shipped: **Connection** in the
      terminal header reads the same endpoint the API does, so the connection's
      own account of itself is one click away rather than API-only. See
      [Keyboard and Controls](keyboard-and-controls.md#connection-details)*

## Enterprise deployment

These gate a production rollout rather than daily usability. A pilot can
proceed without them; procurement cannot.

- [x] **Accounts and per-user separation** — *shipped: `AUTH_MODE=local`
      gives a sign-in page, an account each with roles, and one person's
      terminal sessions, chaos runs, tasks and saved work kept from another's
      — administrators included. API tokens belong to accounts and reach only
      what their owner reaches. See
      [Running a shared instance](multi-user.md)*
- [x] **Attributable audit logging** — *shipped: who signed in, who opened a
      session against which host, who changed an account or a setting, and
      every refusal — in a file of its own, admin-readable at `/admin/audit`,
      with no switch to turn it off. See
      [The audit trail](multi-user.md#the-audit-trail)*
- [x] **OIDC single sign-on** — *shipped: `AUTH_MODE=oidc` signs people in
      through the directory an organisation already runs, provisioning an
      account on first use and mapping roles from a group claim. Local accounts
      keep working alongside it, deliberately: an instance whose only door
      depends on a service it does not run can be locked out of itself by
      somebody else's outage. See
      [Single sign-on](authentication.md#single-sign-on-oidc)*
- [ ] **SAML** — the same job for organisations whose directory does not speak
      OIDC. A different protocol rather than a different identity model: the
      account an assertion resolves to, and everything downstream of it, is
      what OIDC sign-in already builds
- [x] **WCAG 2.1 AA conformance statement** — *shipped: an audit across
      thirteen surfaces, the failures it found fixed, and a statement that
      names what conforms, what does not, and which two apparent failures are
      deliberate. See [Accessibility](accessibility.md)*
- [x] **A session manager** — *shipped: an administrator assigns published host
      profiles to groups, roles or named accounts, and an operator whose
      account reaches one mainframe is connected straight to it while one who
      reaches several lands on a real 3270 selection screen. Branded, paged,
      and driven by the terminal's own keys. See
      [The session manager](session-manager.md)*
- [ ] **Distributable task and profile libraries** — both are server-side
      already; moving them between deployments is not solved

## Web-native and integration

Complete. The terminal can be framed by a named origin, driven from the page
around it, called cross-origin as an API, used with a finger, and reached by a
screen-scraper that still speaks HLLAPI.

- [x] **Embed-in-iframe / SPA integration story** — *shipped: `EMBED_ORIGINS`
      names the origins that may frame the terminal or call the API from a
      page, `?embed=1` renders it without chrome, and a postMessage channel
      lets the surrounding page read the screen and press keys. See
      [Embedding 3270Web](embedding.md)*
- [x] **Mobile / touch UI** — *shipped: a thumb-reachable bar of AID keys that
      rides above the software keyboard, tap-to-place-cursor on protected
      text, and a screen that scrolls and zooms rather than reflowing. See
      [Keyboard and Controls](keyboard-and-controls.md#touch-devices)*
- [x] **HLLAPI-shape scripting endpoint** — *shipped: numbered functions,
      one-based linear positions and return codes, so an existing
      screen-scraper is ported by changing how it calls rather than what it
      does. `"SMITH@E"` still means what it always meant. See
      [REST API](rest-api.md#post-apiv1sessionsidhllapi)*

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
