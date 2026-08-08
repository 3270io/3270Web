# Enterprise Readiness Audit

An assessment of 3270Web as a **daily-driver TN3270 terminal for business
users**, benchmarked against Quick3270, IBM Personal Communications (PCOMM),
Micro Focus Rumba+ and Reflection, and BlueZone.

This page complements [Feature Roadmap](feature-roadmap.md). The roadmap
answers *"what could we build?"*. This page answers *"what does a claims
clerk who lives in this screen eight hours a day actually need, and where
does 3270Web currently fail them?"* — those are different questions, and
they produce a different priority order.

!!! info "Scope"
    This is an analysis document. Every claim below is traceable to a
    specific file and line in the repository as of the audit date.

!!! success "Phase 1 is shipped"
    The six Phase 1 items are implemented — local cursor navigation, a
    real OIA, reconnect, screen and block copy, workspace modes, and
    3270 field semantics. Section 3 and section 5 describe the state
    that motivated them and are kept as the rationale; each finding now
    carries a note on how it was resolved. See
    [Keyboard and Controls](keyboard-and-controls.md) for the resulting
    behaviour, and [Phase 1 outcome](#9-phase-1-outcome) below.

---

## 1. Verdict

3270Web is an **excellent automation and exploration platform that happens
to contain a terminal**. For its current audience — engineers profiling a
host, recording regression workflows, letting an AI drive discovery — it is
genuinely differentiated. Nothing else in the category ships a chaos
explorer, a mind-map of discovered screens, or a Copilot panel with 25 host
tools.

It is **not yet a daily-driver terminal**, and the gap is not primarily a
feature-count problem. It is three specific things:

1. **Cursor movement is a network round-trip.** Tab, Back-Tab and all four
   arrow keys POST to the server and re-render the screen. On a real 3270
   emulator these are local operations that never touch the host.
2. **The interface is built for the automation user.** Of roughly 27
   controls in the main toolbar, 3 serve a business user. The command
   palette exposes 11 chaos commands and 0 terminal operations.
3. **The one feature that would make it a category leader already exists
   and is unreachable.** Parameterized business functions are fully
   implemented in `internal/chaos/business_workflow.go` and exposed only as
   AI tool calls. There is no button, no form, no menu entry.

Fixing (1) and (2) is a matter of weeks, not quarters, and neither requires
giving up anything the automation audience currently has. Fixing (3) is
mostly presentation work over plumbing that is already written and tested.

---

## 2. What is already strong

Credit where it is due — these are not table stakes, and several
competitors charge extra for them.

| Capability | Where | Note |
|---|---|---|
| Full AID/control key coverage | `cmd/3270Web/keys.go` | PF1–24, PA1–3, Attn, SysReq, Clear, Reset, EraseEOF, EraseInput, Dup, FieldMark, Newline, Home. Complete. |
| Extended colour + highlight rendering | `internal/render/html_renderer.go:368` | All 7 3270 colours, blink, reverse, underscore, intensify. |
| Screen-reader field labelling | `internal/render/html_renderer.go:66` | Derives `aria-label` from the protected text to the field's left. Genuinely rare — most competitors announce every field as a bare "edit text". |
| Keyboard-trap escape hatch | `web/static/keyboard.js:1064` | Ctrl+Tab releases terminal focus, satisfying WCAG 2.1.2. Thoughtfully done. |
| Unsolicited host updates | `web/static/screen-live.js` | SSE push with a 1.5 s typing grace window so in-flight keystrokes aren't clobbered. |
| Public REST API | `cmd/3270Web/api_v1.go` | Nine endpoints. RPA and CI integration without licensing negotiations. |
| Recording / playback | `cmd/3270Web/workflow_playback.go` | 3270Connect-compatible JSON, with pause/step/debug. |
| Chaos exploration | `internal/chaos/` | No competitor has anything comparable. |

The accessibility work in particular is a real asset. It is close to being
a defensible RFP differentiator in regulated industries, and it is nearly
free to finish.

---

## 3. P0 — Defects that block daily use

These are not missing features. These are behaviours that will make an
experienced 3270 operator abandon the product inside an hour.

### 3.1 Cursor movement round-trips to the host

**The single most important finding in this audit.**

`web/static/keyboard.js:1080` sends Tab and Back-Tab to the server:

```js
sendFormWithKey(event.shiftKey ? "BackTab" : "Tab", formId, ...);
```

`web/static/keyboard.js:1112` does the same for all four arrow keys.

Each one is an HTTP POST, an s3270 action, a screen re-read, an HTML
re-render, and a DOM replacement. On a LAN this reads as sluggish. Across a
WAN at 80 ms RTT it is unusable: a clerk tabbing through a twelve-field
account-maintenance screen pays a dozen round-trips to do something a real
emulator does with zero.

In a genuine 3270 emulator, the cursor lives in the client's screen buffer.
Tab, Back-Tab, Home, and arrows move it locally and instantly. The host
learns the cursor position exactly once — in the inbound data stream, when
an AID key is pressed.

**Fix:** move cursor navigation client-side. The field geometry is already
in the DOM (`data-x`, `data-y`, `data-w` on every input,
`html_renderer.go:213`), and the machinery for reporting cursor position at
submit time already exists (`cursor_row`/`cursor_col` hidden inputs,
`html_renderer.go:42`). This is a client-side change with no server work.

!!! success "Resolved"
    Tab, Back-Tab, the arrows and Home now move the caret in the DOM and
    contact no one. Two deviations from a hardware terminal are documented
    in [Cursor movement is local](keyboard-and-controls.md#cursor-movement-is-local):
    a field's caret range is bounded by its current text length, and rows
    of purely protected text are skipped.

### 3.2 Field semantics the host expects are not enforced

The renderer emits every field as `<input type="text" ... inputmode="text">`
(`html_renderer.go:226`), regardless of the 3270 field attribute.
Consequences:

- **Numeric-only fields accept letters.** The 3270 numeric attribute is
  parsed but not applied. A real emulator locks the keyboard and shows an
  operator-error indicator; 3270Web sends the letters to the host and lets
  the application reject them a round-trip later.
- **No insert/overtype mode.** There is a typeover approximation
  (`keyboard.js:980`) but no Insert-key toggle and no mode indicator.
  Operators who rely on insert mode to edit mid-field cannot.
- **Auto-skip fires on every full field** (`keyboard.js:941`), not only
  where the field attribute requests it. This is subtly wrong and will
  surprise users on screens where it shouldn't happen.
- **No type-ahead.** Keystrokes during a host wait are dropped rather than
  buffered. Fast operators type ahead constantly.

!!! success "Resolved"
    The numeric attribute now reaches the browser as `data-numeric` and is
    enforced with a real operator-error lock; Insert is a local
    insert/overtype toggle shown in the OIA; characters typed during a host
    wait are buffered and replayed (AID keys are deliberately not).
    Auto-skip was narrowed to fire only out of full *numeric* fields, which
    removes the surprising case without changing behaviour operators rely
    on — the strict field-attribute rule remains open on the roadmap.

### 3.3 The status line is not an OIA

The status line shows `KB / MODEL / SIZE / CURSOR`
(`web/templates/screen.html:283`). The 3270 Operator Information Area that
every terminal user has read reflexively for forty years shows:

- `X SYSTEM` / `X []` — input inhibited, host thinking
- `X -f` — operator error, keyboard locked, press Reset
- Insert-mode indicator
- Connection and LU indicator
- Shift/caps state

Without `X SYSTEM`, users cannot tell "the host is busy" from "the app is
broken", and they will hammer Enter. Without an operator-error indicator,
a locked keyboard looks like a hung application. The `ui-polish.js:6`
comment says the bezel reflects OIA keyboard-lock state — that is a good
instinct, but a coloured bezel is not a substitute for the literal
indicators operators are trained on.

### 3.4 No screen-accurate copy

Because the screen is a `<pre>` interleaved with `<input>` elements
(`html_renderer.go:57`), browser selection across the screen produces
mangled text — input values drop out of the selection entirely.

There is no rectangular block copy, which in a 3270 shop is not a nicety:
it is how people get a column of account numbers into Excel. Quick3270,
PCOMM, and Rumba+ all ship it.

!!! success "Resolved"
    The renderer now emits the character grid as `data-screen-text`, and the
    client overlays live input values on it. Copy Screen returns what is
    actually displayed, including unsubmitted input and excluding hidden
    password fields. Alt+drag marks a rectangle; Ctrl+C copies it.

### 3.5 No reconnect

When the host drops, the session is gone. No auto-reconnect, no
"Reconnect" button, no preserved host/port for a one-click retry. Every
competitor reconnects automatically. Mainframe sessions drop for routine
reasons — LPAR maintenance, VTAM timeouts, network blips — and losing your
place several times a day is a serious irritant.

---

## 4. P1 — Table-stakes parity gaps

Benchmarked against [Quick3270's published feature
list](https://www.dn-computing.com/Quick3270.htm).

| Capability | Quick3270 | PCOMM / Rumba+ | 3270Web | Notes |
|---|:---:|:---:|:---:|---|
| Multiple concurrent sessions (tabs) | ✅ | ✅ | ❌ | One session per cookie — `session.Session` holds a single `Host` (`internal/session/session.go:17`). Business users routinely run 3–6 sessions. |
| IND$FILE file transfer | ✅ | ✅ | ❌ | s3270 `Transfer()` is not wired up. Referenced in the profiler schema only. |
| Customisable keyboard mapping | ✅ | ✅ | ❌ | Mappings are hard-coded in `keyboard.js:684–795`. Quick3270 imports PCOMM keyboard files — that is a migration accelerator. |
| Macro / scripting language | ✅ VBScript + debugger | ✅ | ⚠️ | 3270Web has recording/playback JSON and a REST API — arguably better for CI, worse for a power user who wants a loop and an `IF`. |
| EHLLAPI / WinHLLAPI | ✅ | ✅ | ❌ | The blocker for migrating shops with existing HLLAPI screen-scrapers. The REST API is the modern answer but is not drop-in. |
| Hotspots (clickable PF labels, URLs) | ✅ | ✅ | ❌ | Cheap to add and disproportionately loved. The chaos engine already parses the bottom-row PF legend — that parser is directly reusable. |
| Per-session TLS / LU / model | ✅ | ✅ | ❌ | These are server-wide env vars (`internal/config/s3270_env.go:218`). The connect form takes `hostname:port` only, and `parseHostPort` (`cmd/3270Web/validator.go:9`) cannot parse s3270's `L:lu@host:port` form. You cannot have one TLS host and one plaintext host. |
| Named connection profiles | ✅ | ✅ | ⚠️ | Saved hosts exist but are `localStorage` (`connect-page.js:22`) — browser-local, lost on cache clear, not admin-distributable, and carry only a hostname. |
| Screen history / scrollback | ✅ | ✅ | ❌ | "What did the previous screen say?" is unanswerable today. |
| Find on screen | ✅ | ✅ | ❌ | Ctrl+F is browser-find, which misses input values. |
| Print screen | ✅ | ✅ | ✅ | Shipped. |
| Host print (LU1/LU3 printer session) | ✅ | ✅ | ❌ | Lower priority — increasingly replaced by output management systems. |

### Deployment-blocking gaps

Separate from feature parity, three things will stop a security review:

- **No user authentication.** The app has no identity model at all —
  `main.go` authenticates nothing but the session cookie. Anyone who
  reaches the port reaches the mainframe login screen.
- **No audit trail.** Regulated industries require per-user records of
  which host was accessed, when, and by whom. Session-scoped logs exist;
  attributable audit records do not.
- **No SSO.** OIDC/SAML is the entry ticket for BYOD and Azure AD shops.

These are already on the roadmap as unchecked items. They are called out
here because they gate *deployment*, not *usability* — a pilot can proceed
without them, a production rollout cannot.

---

## 5. The balance problem, quantified

The user-facing surface is weighted heavily toward automation.

**Main toolbar** (`web/templates/screen.html:93–255`):

| Group | Controls |
|---|---:|
| Chaos | 12 |
| Recording + playback | 12 |
| Session / terminal (Disconnect, Logs, Print) | 3 |

**Command palette** (`web/static/ui-polish.js:46–80`) — 34 entries:

| Group | Entries |
|---|---:|
| Chaos | 11 |
| Recording + playback | 8 |
| Session | 6 |
| Themes | ~9 |
| **Terminal operations** | **0** |

There is no palette entry to send a PF key, copy the screen, find text,
reconnect, or switch session. A business user opening Ctrl+K sees a control
panel for a testing tool.

### The recommendation is *not* to remove anything

Chaos and AI are the product's differentiators, and the automation audience
is real. The fix is **role-based surfacing**, not deletion:

- Ship a **Business** mode as the default: terminal, keypad, sessions,
  copy, find, print, reconnect, business tasks. Chaos and recording live
  behind one "Automation" toggle.
- Keep an **Engineering** mode with today's full toolbar, one click away
  and remembered per user.
- Populate the palette with terminal operations so Ctrl+K is useful to
  someone who has never heard of chaos mode.

This is a ~2-day change that transforms first-run impressions, and the
automation audience loses one click.

!!! success "Resolved"
    Business mode is the default and hides the recording and chaos groups
    plus the workflow status widget; Engineering mode restores them and the
    choice persists. Terminal operations and every host key are now in the
    palette, and palette entries whose control is hidden are hidden too, so
    Business mode does not offer chaos commands. One carve-out: if a run
    starts while in Business mode — Copilot can start one — the status
    widget appears anyway and is put away when the run ends.

---

## 6. Flagship enhancement — Guided Business Tasks

This is the capability described in the original request: *record a business
screen flow, prompt the user for key inputs up front, execute the navigation
automatically, and present the outcome.*

**The remarkable finding: roughly 80 % of this is already built.**

`internal/chaos/business.go` and `internal/chaos/business_workflow.go`
already implement:

- `BusinessFunction` — a named, parameterized operation with an entry
  screen, ordered steps, and typed parameters (`business.go:56`)
- `BusinessParameter` — named input bound to a concrete field via
  `R<row>C<col>L<len>` (`business.go:22`)
- **Automatic navigation bridging** — `mm.findKeyPath()` computes the key
  presses needed to get from where you are to the screen a step needs
  (`business_workflow.go:85`)
- **Sensitive-value suppression** — parameters landing in hidden or
  AI-flagged fields are stripped from the metadata
  (`business_workflow.go:55`)
- **Landing-screen assertions** — `ExpectHash` emits `CheckValue` guards
  (`business.go:47`)
- A REST surface at `/chaos/business/*` (`cmd/3270Web/main.go:253`)

What is missing is everything the *user* touches:

| Gap | Impact |
|---|---|
| **No UI whatsoever** | Reachable only as Copilot tool calls (`copilot-tools.js:261`). No button, no form, no menu. |
| **Gated behind chaos** | Requires a chaos mind-map and annotated screens. A business analyst who just wants to record "check a balance" cannot get there. |
| **Output is a JSON download** | You must download the workflow, re-upload it, and play it. Not "fill in a form, get an answer". |
| **No result extraction** | Playback navigates and stops. Nothing captures *the balance was £1,240.55* and shows it. This is the entire point of the feature. |
| **No parameter prompting** | Values must be supplied programmatically up front. There is no "please enter an account number" dialog. |

### Proposed design

**Author** — a business analyst records a flow the normal way, then clicks
*Save as Business Task*. 3270Web replays the recording and asks two
questions per screen:

- *Which of these typed values are inputs?* — each becomes a named,
  described, optionally-validated parameter, defaulting to the label text
  to its left (the renderer already derives these labels for accessibility,
  `html_renderer.go:66`).
- *Which fields on the final screen are the answer?* — the missing piece.
  Selected regions become named outputs.

The AI panel can propose all of this — Copilot already has `get_screen` and
screen-annotation tools — but a human confirms. Nothing about the feature
should *require* AI; that is the difference between a differentiator and a
dependency.

**Run** — the task appears in a **Tasks** menu. Selecting *Account balance
enquiry* opens a form:

```
Account balance enquiry
Retrieves the current cleared balance for a customer account.

  Account number  [ 40218855      ]  8 digits, required
  As-at date      [ 2026-08-08    ]  optional, defaults to today

                              [ Cancel ]  [ Run ]
```

3270Web navigates, filling fields and pressing keys, with a progress
indicator and a visible **Cancel**. The terminal stays visible — hiding it
would be a mistake; operators need to see what the host is doing, and it is
how they build trust in the automation.

**Outcome** — a result card, not a screen:

```
✅ Account balance enquiry — completed in 2.4 s

  Account       40218855
  Name          J MARGOLIS
  Cleared bal   £1,240.55
  Available     £1,190.55
  Last movement 2026-08-06

  [ Copy ]  [ Export CSV ]  [ Show screens ]  [ Run again ]
```

On failure — an unexpected screen, a host error message, a timeout — the
task stops at the divergence point, leaves the terminal exactly where it
stopped so the user can take over manually, and reports which step failed
and what it saw instead. Silent failure or blind continuation would be far
worse than no automation at all.

### Why this is the right bet

- **It is the demo.** "Type an account number, get a balance, never see a
  green screen" is a story a CIO understands in ten seconds. No competitor
  tells it.
- **It changes the addressable user.** Today's product needs someone who
  knows the application. This one serves someone who knows only the
  business question — a much larger population, and the one that actually
  justifies replacing a paid emulator.
- **It rehabilitates chaos mode commercially.** Chaos stops being "a
  testing curiosity" and becomes "the thing that discovers your business
  tasks automatically". The mind-map is the asset; tasks are the product.
- **The hard parts are done.** Path-finding, parameter binding, sensitive
  handling, and assertions are written and tested. The remaining work is
  output extraction, a form, and a result card.

### Sequencing

1. **Output extraction** — add `Outputs []BusinessOutput` (name + field key
   or region + optional regex) to `BusinessFunction`, and capture on the
   final screen during playback. Everything else depends on this.
2. **Decouple from chaos** — allow a `BusinessFunction` to be built from a
   plain recording, not only from a chaos mind-map. Falls back to literal
   screen-hash matching where no learned key graph exists.
3. **Run UI** — Tasks menu, parameter form, progress, result card.
4. **Author UI** — the post-recording parameterisation wizard.
5. **Sharing** — export/import task definitions; server-side task library
   so a team shares one catalogue rather than one JSON file each.

---

## 7. Further differentiators

Ranked by advantage-per-unit-effort. All build on plumbing that exists.

**Task API.** Expose Guided Business Tasks over `/api/v1/tasks/{name}/run`
with JSON in and JSON out. A 1970s CICS transaction becomes a REST endpoint
with no mainframe change, no middleware, and no integration project. This
is a mainframe-modernisation product hiding inside a terminal emulator, and
it is worth more than the terminal.

**Finish accessibility.** The field-labelling and keyboard-trap work is
already better than the competition's. A documented WCAG 2.1 AA conformance
statement is a scored line item in public-sector RFPs, and today essentially
nobody in this category can produce one.

**Explain this screen.** One button, `get_screen` plus a fixed prompt. Cheap,
and it addresses the real problem with mainframe applications: the last
person who understood this screen retired. Extend to *"what changed?"*
between two screens.

**Session recovery.** Reconnect and restore to the last screen after a drop
or a browser refresh. Nobody does this well; the SSE plumbing is already in
place.

**Safety rails.** Warn before a destructive AID key on a screen the mind-map
has flagged as a commit or delete. The chaos engine already classifies
exit/destructive keys — point that intelligence at the human user instead of
the explorer.

**Real macros.** Recording JSON with conditionals and loops, edited in the
browser. Closes the Quick3270 VBScript gap without shipping a script engine.

---

## 8. Prioritised plan

Rough sizing; sequence matters more than the estimates.

### Phase 1 — Make it usable daily ✅ shipped

| # | Item | Size |
|---|---|---|
| 1 | Client-side cursor navigation (Tab/Back-Tab/arrows/Home) | M |
| 2 | Proper OIA status line — `X SYSTEM`, operator error, insert | S |
| 3 | Auto-reconnect + one-click Reconnect | S |
| 4 | Screen-accurate copy, including rectangular block copy | M |
| 5 | Business / Engineering mode toggle; terminal ops in the palette | S |
| 6 | Numeric field enforcement; insert-mode toggle; type-ahead buffer | M |

### Phase 2 — Table stakes (~6 weeks)

| # | Item | Size |
|---|---|---|
| 7 | Multiple concurrent sessions with tabs | L |
| 8 | Server-side connection profiles: TLS, LU, model, codepage per profile | M |
| 9 | IND$FILE via s3270 `Transfer()` | M |
| 10 | Hotspots — clickable PF labels and URLs | S |
| 11 | Screen history / scrollback | M |
| 12 | Find on screen | S |
| 13 | Keyboard remapping UI, with PCOMM keymap import | M |

### Phase 3 — Guided Business Tasks (~6 weeks)

| # | Item | Size |
|---|---|---|
| 14 | Output extraction on `BusinessFunction` | M |
| 15 | Decouple business functions from the chaos mind-map | M |
| 16 | Tasks menu, parameter form, progress, result card | L |
| 17 | Post-recording parameterisation wizard | L |
| 18 | Task API — `/api/v1/tasks/{name}/run` | M |

### Phase 4 — Enterprise deployment

| # | Item | Size |
|---|---|---|
| 19 | OIDC / SAML SSO | L |
| 20 | Attributable audit logging | M |
| 21 | WCAG 2.1 AA conformance statement | M |
| 22 | Shared server-side task and profile library | M |

Phases 1 and 2 buy the right to be evaluated against Quick3270. Phase 3 is
the reason to choose 3270Web over it. Phase 4 is the reason procurement
signs.

---

## 9. Phase 1 outcome

All six Phase 1 items are implemented. What actually changed:

| # | Item | Outcome |
|---|---|---|
| 1 | Client-side cursor navigation | Tab, Back-Tab, arrows and Home resolve in the DOM. Zero host round-trips for navigation, verified in a browser. |
| 2 | Real OIA | Online block, `X SYSTEM`, `X -f`, insert caret. The wait indicator is optimistic on send, then reconciled against the host. |
| 3 | Reconnect | `POST /reconnect`, drop detection from both 401s and the OIA, backoff retry, manual fallback. |
| 4 | Screen and block copy | Character grid on the form, live input values overlaid, hidden fields excluded. Alt+drag marks, Ctrl+C copies. |
| 5 | Workspace modes | Business default, Engineering one click away, palette filtered by mode and given terminal operations plus all host keys. |
| 6 | Field semantics | Numeric enforcement with an operator-error lock, local insert/overtype toggle, type-ahead buffering. |

### Two things fixed along the way

Neither was in scope; both were real.

- **Custom themes never loaded on the connect page.** `theme.js` fetched
  the session-gated `/api/themes` before any session existed, took a 401,
  and silently fell back to an empty list — so the connect page's theme
  picker only ever showed built-ins, and every page load logged a console
  error. The endpoint stays gated (an explicit test asserts that); the
  connect page is served the list inline instead.
- **The OIA bar was `aria-live`.** It carries the cursor position, so a
  screen reader announced row and column on every keystroke. Only the
  inhibit explanation is announced now.

### Known deviations

Deliberate, and worth stating rather than discovering:

- A field's caret range is bounded by its current text length, so arrowing
  right past a short value moves to the next field instead of walking the
  field's blank tail.
- Up and down move between input fields; rows of purely protected text are
  skipped, because the cursor can only rest in an input.
- Auto-skip fires only out of full numeric fields. The strict rule keys off
  the *next* field's auto-skip attribute; that remains open.
- AID keys are never buffered by type-ahead — only characters are.
- Reconnecting starts a new host session, so recording and chaos state from
  the dead connection does not survive.

---

## Sources

- [Quick3270 — DN Computing](https://www.dn-computing.com/Quick3270.htm)
- [Quick3270 configuration reference (PDF)](https://dn-computing.com/download/Quick3270%20configuration.pdf)
