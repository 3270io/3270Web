---
seo_title: "3270Web feature roadmap and where it sits in the field"
description: >-
  How 3270Web compares with the other 3270 terminal emulators available
  today, and what it may do next — a menu of candidate work rather than a
  dated schedule.
---

# Feature Roadmap

What may come next. This page is a **menu, not a schedule** — an unchecked
item is a candidate, not a commitment, and the order is not a queue.

For what 3270Web provides **today**, see
[Terminal Capabilities](terminal-capabilities.md). That page is the one to
read before an evaluation; this one is for deciding what to build.

## The field, and where 3270Web sits in it

A roadmap is a set of choices, and the choices are made against a category
that already exists. So the comparison comes first, and the rest of the page
follows from it.

The table describes **how each emulator is delivered and what it offers for
automation** — facts each vendor states plainly and that rarely change from
release to release. It is deliberately not a feature-by-feature grid: nobody
can honestly audit eight products from the outside, and a tick in a column
you cannot verify is worse than no column at all. Each vendor's own
documentation is the authority on that vendor's product. Rows were checked
against published documentation in August 2026; if one is wrong or has gone
stale, [tell us](https://github.com/3270io/3270Web/issues) and it gets
fixed.

| Emulator | How it reaches the user | Runs on | Licence | Automation surface |
|---|---|---|---|---|
| **3270Web** | A URL. One Go binary or container serves the terminal to a browser tab | Any modern browser; server on Linux, Windows, macOS or Docker | Open source (MIT) | REST/JSON API, recorded workflows replayed as JSON, Guided Business Tasks, an HLLAPI-shaped endpoint, and an MCP server |
| **Quick3270** | Installed Windows application | Windows client and server, Citrix and Terminal Server | Commercial, per seat | VBScript macro language with an editor and debugger, EHLLAPI (32- and 64-bit), a session API and COM automation |
| **Vista TN3270** | Installed Windows application | Windows | Commercial, low per-seat fee | Fully tailorable keyboard, multiple paste buffers, JCL-aware selection — aimed at the programmer at the keyboard |
| **Mocha TN3270** | Installed application, one per platform | Windows, macOS, ChromeOS, iOS, Android | Commercial, per seat | Keyboard definition and user-defined function keys |
| **HCL Z and I Emulator for Web** | Browser, served from an application server | Browser client; Windows, Linux or z server | Commercial, licensed per user | EHLLAPI and the host-access toolkit APIs, including a J2EE connector |
| **Inventu Viewer+** (formerly Flynet Viewer) | Browser, served from a Windows server — no plug-in, no applet | Browser client; Windows Server | Commercial | Server-side screen integration, with generated web services |
| **Ericom PowerTerm WebConnect HostView** | Browser, served from a central server | Browser client; Windows or Linux server | Commercial | Centrally managed sessions, with scripting in the PowerTerm client |
| **x3270 / wc3270 / c3270 / s3270** | Installed command-line or X11 application | Linux, Unix, Windows, macOS | Open source (BSD-3-Clause) | The `s3270` scripting protocol — the same engine 3270Web drives |

### Reading the table

Three delivery models, and which one a product picked decides most of
everything else about it.

**Installed desktop clients** — Quick3270, Vista TN3270, Mocha TN3270 and
the x3270 family — have the deepest terminal fidelity and the widest set of
integration points, because they have a whole operating system to
reach into: an EHLLAPI library that a twenty-year-old binary can load, a
printer session that appears as a real device, a COM object another desktop
application can drive. What that costs is per-machine. Something is
packaged, installed, patched and version-matched on every desk, and the
Windows-only ones decide what the desk has to be.

**Server-delivered browser sessions** — Z and I Emulator for Web, Inventu
Viewer+, PowerTerm WebConnect, 3270Web — put nothing on the desk. One place
to upgrade, one place to control who reaches which host, and a Chromebook or
a tablet is as good a terminal as a laptop. The trade is that anything which
had to touch the local machine has to be re-earned some other way, over an
API rather than a DLL.

**Open source** — the x3270 family and 3270Web — means the thing can be
read, forked, audited and deployed without a purchase order. It is the
smaller half of this category by a distance. 3270Web builds directly on
`s3270`; see [Acknowledgements](acknowledgements.md).

### Where 3270Web is different

Comparing against the category rather than against any one product, because
these are axes rather than checkboxes:

| Axis | Where the category generally sits | 3270Web |
|---|---|---|
| Getting a terminal onto a desk | An install, or a server product plus a client entitlement per user | A URL |
| Automating a flow | A vendor macro language, or EHLLAPI against a presentation space | The recording made in the browser **is** the JSON an API replays — no second language in between |
| Serving somebody who does not know the application | Out of scope: the operator is expected to learn the green screen | [Guided Business Tasks](business-tasks.md) — named inputs, a named answer, no green screen |
| Finding out what an application actually does | Documentation, or a person with twenty years of it | [Chaos exploration](chaos-mode.md) walks it and produces a screen mind-map, diffable between two hosts |
| Regression-testing a host application | Left to whatever test tooling the shop already has | Named screen snapshots, diffed row by row, over the API |
| Accessibility | Rarely stated in public documentation at all | A tested [WCAG 2.1 AA statement](accessibility.md), including where it falls short |
| AI | Increasingly offered, generally as the vendor's own service | Bring your own provider — [six of them](ai-providers.md) — plus an [MCP server](mcp.md) any agent can drive |
| Reading the source | Closed, with the x3270 family the long-standing exception | Open, on GitHub |

### Where the field is ahead

The honest half, and the reason the next section exists. These are
capabilities the established emulators have had for years and 3270Web does
not:

- **Printing to the operator's own printer.** A 3287 printer LU is now bound
  and collected — see [Printer sessions](printer-sessions.md) — but the job
  arrives in the browser as a file. An installed client can hand it to the
  print spooler on the desk it is installed on; a browser tab cannot, and no
  amount of work here changes that.
- **Protocols beyond TN3270.** 5250, the protocol AS/400 and IBM i hosts
  drive their terminals with, and VT for everything else, usually ship in the
  same box. 3270Web speaks TN3270 and TN3270E only — and so does `s3270`
  underneath it, which carries no 5250 at all. Those hosts are still
  reachable, through a 3270 front end of their own rather than through
  anything 3270Web does; see
  [AS/400 and IBM i hosts](terminal-model-limits.md#as400-and-ibm-i-hosts)
  for the two conditions that come with it.
- **A loadable EHLLAPI library.** The
  [HLLAPI-shaped endpoint](rest-api.md#post-apiv1sessionsidhllapi) ports a
  screen-scraper by changing *how* it calls rather than what it does — but
  only if it can be rebuilt. A binary that links the DLL and has no
  surviving source still needs the DLL.
- **DBCS and bidirectional language support.** Arabic, Hebrew, and the
  double-byte character sets. Neither is a rendering tweak; both reach the
  code page handling and the field model.
- **A macro language with decisions in it.** Macro files now import — see
  [Recordings and Playback](workflow.md#importing-a-macro-file) — but what
  imports is the straight-line part of one. A recording has no branch, no loop
  and no variable, so a macro that reads a balance and decides what to do
  about it still needs a person, and the established emulators ship full
  scripting languages with editors and debuggers around them.

## Category parity

Derived from the section above. None of these make 3270Web better at what it
is already good at — they remove reasons an evaluation stops early.

- [x] **3287 printer session** — *shipped: a printer LU bound beside the
      terminal session, by name or as the display LU's associated printer, on
      the same host and the same TLS terms. Each job the host prints arrives
      as a file to download, from the panel or over the API. `pr3287` does the
      protocol; what this adds is where a browser-delivered terminal is
      allowed to send paper. See [Printer sessions](printer-sessions.md)*
- [ ] **5250, for AS/400 and IBM i** — the same terminal, the same recording
      and task machinery, pointed at the platform whose green screens are not
      3270 ones. A larger job than it looks, and larger from here than from
      most places: `s3270` has no 5250 in it, so this is a second protocol
      engine rather than a switch on the existing one. 5250 has its own field
      model and its own AID set, and pretending otherwise is how an emulator
      ends up subtly wrong on both. Meanwhile these hosts are reachable
      through their own 3270 front end —
      [model 2 only, with the 5250 function keys arriving as PF-key
      sequences](terminal-model-limits.md#as400-and-ibm-i-hosts)
- [ ] **VT emulation** — commonly in the same box as 3270, and the reason a
      shop can standardise on one client. Worth doing only if it does not
      compromise the 3270 path, which is what 3270Web is actually for
- [ ] **DBCS and bidirectional language support** — double-byte character
      sets, and right-to-left screens with the field-level orientation rules
      that go with them. Reaches the code page handling, the field model and
      the renderer, in that order
- [ ] **A native EHLLAPI shim** — a small DLL that presents the classic
      entry points and forwards them to the HLLAPI-shaped endpoint, for the
      binaries that cannot be rebuilt. The semantics are already
      implemented; what is missing is a library for them to load
- [x] **Macro-file import** — *shipped: a macro file written against the
      session automation object model becomes a recording, and every line that
      would not translate is reported with its number, its text and the reason
      — branching, a variable, text typed at a position the file cannot know.
      One click in the Automation menu, and on the API without a session so a
      directory of them converts in one pass. See
      [Recordings and Playback](workflow.md#importing-a-macro-file)*

## Recently shipped

Newest first. Every item here is live and documented.

- **The shelf of macros that was keeping a shop where it is** — a macro file
  written for an installed emulator is read here and becomes a recording, and
  the lines it will not take are named rather than dropped: this branch, that
  variable, this text typed at a position the file cannot know. The refusals
  are the feature. Guessing a coordinate would produce a flow that types an
  account number into whatever field the host happened to leave the cursor in,
  and that is discovered in production rather than in the report. The
  translation and the report are on the API as well, needing no session,
  because a shop with one macro has three hundred. See
  [Recordings and Playback](workflow.md#importing-a-macro-file).
- **A question about the screen in front of you** — "explain this screen" was
  a starter chip on an empty chat, which is the one moment an operator is
  least likely to need it; the screen worth explaining is the one that arrived
  four transactions into a flow. It is now a click from the Terminal menu, the
  chat composer or the command palette, at any point in a conversation. The
  screen goes with the question rather than being read a round later, because
  a host is free to redraw while the question is being asked and an answer
  about the wrong screen is worse than none. See
  [AI Chat Mode](ai-chat.md#explain-this-screen).
- **An assistant that knows what this build can do** — the terminal grew past
  its own tool surface. Snapshots, the display toggles, the connection's own
  account of itself, the printer session and the task catalogue were on the
  API and nowhere an assistant could reach, so one asked whether 3270Web could
  compare a screen against the one a flow used to land on answered from what
  it could see — a keyboard and an exploration engine. They are tools now,
  declared once and offered to the chat panel and the MCP server alike, which
  also gives the panel the saved tasks it never had. See
  [AI Chat Mode](ai-chat.md#beyond-the-screen).
- **Somewhere for the batch output to go** — a 3287 printer LU bound beside
  the terminal session, by name or as the associated printer of the display
  LU the host bound, and always on the same host and TLS terms as the session
  it belongs to. What the host prints arrives as a file to download rather
  than as paper, which is the trade a browser makes; a job too large to keep
  is named as truncated rather than quietly ending early. See
  [Printer sessions](printer-sessions.md).
- **A deployment's set-up as a file** — the host presets and the recorded
  tasks in one versioned document, so a second instance is configured by
  importing rather than by retyping forty entries in the right order. It says
  what it would change before it changes anything, and a file it cannot store
  in full it does not store at all. See
  [The session manager](session-manager.md#moving-a-set-up-to-another-instance).
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
  an auto-hiding menu rail; Business is the default surface and
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
- [x] **Focus mode vs. a MAX-size keypad** — *shipped, and the call is that the
      terminal wins: focus mode exists to give the terminal the display, so the
      keyboard takes a share of it and the terminal takes the rest. Measuring
      first found the two were not merely fighting over the space — they were
      bidding for it, each sizing itself from what the other had just released,
      until both overflowed and the terminal's first rows were pushed above the
      top edge. See
      [Keyboard and Controls](keyboard-and-controls.md#focus-mode-and-the-keypad)*

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
- [x] **Distributable task and profile libraries** — *shipped: the recorded
      tasks and the host presets as one versioned document, downloaded from one
      deployment and imported into the next. The import says what it would do
      before it does it, reports every entry, and refuses a file it cannot
      store in full rather than storing half — because the state a library
      exists to prevent is two instances quietly disagreeing about which
      mainframe a name points at. Audiences naming individual accounts are left
      out, since those accounts exist only where the file came from. See
      [The session manager](session-manager.md#moving-a-set-up-to-another-instance)*

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

Complete, and kept here rather than deleted: what an AI section promises is
the part a reader is most entitled to check, and a list of what was actually
built is the only honest way to let them.

What is live: a chat panel beside the terminal that **drives the session
through a tool surface of about forty calls** — read the screen, write a
field, press an AID key, wait for the host, connect somewhere, run and steer a
chaos exploration, annotate what it learns, catalogue a business function,
generate a workflow from one, run a saved task and report its answer, freeze a
screen and say which rows moved since, read what the connection negotiated,
change a display toggle, collect what the host printed. Six AI providers to choose between, each with
its own credentials. Procedures kept as
[skills and instructions](skills.md) in files, so an installation can add its
own without editing a prompt. An [MCP server](mcp.md) over stdio and HTTP with
safety tiers and a host allowlist, offering every Guided Business Task as a
tool of its own. Per-call approval, and an auto mode that still stops at
`ask_user`. And everything read from a host wrapped as untrusted data, because
a screen can be made to read like an instruction and the assistant is told,
in the prompt, not to take one from it.

See [AI Chat Mode](ai-chat.md), [AI Providers](ai-providers.md),
[Skills and Extensions](skills.md) and [MCP Server](mcp.md).

What this section once listed as outstanding, and where each of it landed:

- [x] **Natural-language → keystrokes** — *shipped: this is what the tool
      surface is. "Fill in the account number and press Enter" resolves to
      `write_field` and `send_key` against the screen the assistant just
      read, with the call shown before it runs unless auto mode is on*
- [x] **AI-proposed task authoring** — *shipped by a different route than the
      one imagined here: a chaos run's discovered business function converts
      into a task draft, guards derived from the screen text the run captured,
      for a human to confirm. Nothing about Guided Business Tasks requires AI
      — the authoring wizard works from a plain recording — which is the
      difference between a differentiator and a dependency*
- [x] **"Explain this screen" from the terminal, at any point** — *shipped:
      one click from the Terminal menu, from the composer, or from the command
      palette, at any point in a conversation rather than only on an empty
      one. The screen is captured when the question is asked and carried with
      it, so the answer is about the screen that prompted the question and not
      whichever one the host drew while it was being asked. See
      [AI Chat Mode](ai-chat.md#explain-this-screen)*
- [x] **An assistant that knows what this build can do** — *shipped: the
      capabilities that were on the API and nowhere a model could reach are
      tools now. Snapshots — take, list, diff, delete — so a flow can be
      checked against the screen it used to land on; the display toggles; the
      connection's own account of itself; the printer session and the jobs it
      has collected; and the Guided Business Task catalogue, with a run that
      waits and returns the answer rather than the step list. Declared once
      and offered on both surfaces, the chat panel and the
      [MCP server](mcp.md), and named in the system prompt — a tool a model
      has to guess at is a capability it will tell the user this build does
      not have. See [AI Chat Mode](ai-chat.md#beyond-the-screen)*

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
