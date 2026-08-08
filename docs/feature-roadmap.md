# Feature Roadmap

This page tracks where 3270Web stands today against the s3270 binary's
exposed action surface and against competing 3270 emulators (PCOMM,
Rumba+, Reflection, BlueZone, HostExplorer, Vista TN3270, Inventu
Viewer+, z/Scope, Mocha, Flynet, Virtel, Glink, ZOC, x3270). It is a
menu, not a schedule — items in **Future candidates** are deliberately
unprioritized.

For a prioritized view of the same ground from the perspective of a
business user running 3270Web as their everyday terminal — including the
daily-use defects that outrank most of the items below — see the
[Enterprise Readiness Audit](enterprise-readiness.md).

## Recently shipped

- **Host Compatibility Profiler** — `POST /profile` and
  `POST /api/v1/sessions/:id/profile` produce a shared
  `CompatibilityProfile` JSON document. Same schema as
  `3270Connect -profile`, so the same JSON drops into either tool's
  comparison workflow. See [Host Compatibility Profiler](host-profiler.md).
- **Chaos Mind-Map Compare** — `POST /chaos/mindmap/compare` diffs two
  exported mind maps and surfaces field and transition divergence
  between hosts. JSON by default, HTML report on `Accept: text/html`.
  See [Chaos Mind-Map Compare](chaos-compare.md).
- **IBM 3270 terminal fonts** — three bundled web fonts (Regular,
  Semi-Condensed, Condensed) selectable from the Settings modal.
  See [Terminal Fonts](terminal-fonts.md).
- **AI Chat model selector** — dropdown in the side-panel header to
  switch between Copilot models (default Claude Opus 4); choice
  persists across page reloads.
- **Chaos learned-value reuse bias controls** —
  `CHAOS_LEARNED_INPUT_REUSE_BIAS`, `CHAOS_LEARNED_KEY_REUSE_BIAS`, and
  `CHAOS_EXPORT_SUCCESS_BALANCE` let operators tune how aggressively
  the engine reuses values, keys, and successful transitions in
  exports.
- **Accessibility & UX hardening** — destructive-action confirmation
  modal for saved-host deletion, visual destructive tagging on
  Disconnect, hostname input marked required with AT-announced
  errors, collapsible toolbar sections, theme-aware toast
  notifications.

## Where 3270Web stands today

| Bucket | Current state |
|---|---|
| Core terminal | TN3270 with TLS, themed rendering (IBM 3270 fonts, 7 themes), virtual keypad, browser copy/paste, settings modal with diagnostics, sample 3270 servers for testing. **New in this release:** print screen via s3270 `PrintText()`. |
| s3270 exposure | All AID keys, navigation, `Connect`, `readbuffer ascii`, `Wait(Unlock,...)`, `movecursor`, `eraseeof`, `newline`. **New in this release:** field writes via s3270 `String()` (was per-character `key(0x..)`); `PrintText()` for print screen. |
| Modernization / web-native | Cookie-session web UI, embedded s3270, Windows desktop wrapper, Docker image, MkDocs site. **New in this release:** public REST/JSON API at `/api/v1/*`. |
| Automation / AI | Workflow recording/playback (3270Connect-compatible JSON), chaos exploration with hints, GitHub Copilot side panel driving the host via tool calls. |

## Table-stakes gaps vs. competitors

Items most enterprise emulators ship that 3270Web does not yet have.

- [ ] **IND$FILE file transfer** (s3270 `Transfer()`) — text and binary, ASCII/EBCDIC
- [x] **Print screen** (s3270 `PrintText()`) — *shipped: toolbar button → opens printable HTML in a new tab*
- [ ] **Multiple concurrent sessions / tabs in one browser** — today, one session per cookie
- [x] **Rectangular block copy/paste** — *shipped: Alt+drag marks a block, Ctrl+C copies it; plus whole-screen copy that includes unsubmitted input*
- [x] **Scrollback / screen history navigation** — *shipped: last 50 screens per session, read-only viewer*
- [x] **Find/search within current screen** — *shipped: Ctrl+F over the character grid, so it sees input values the browser's own find cannot*
- [x] **Hotspots** (clickable PF labels and URLs detected on screen) — *shipped: never placed over an input field, only real key ranges*
- [x] **Auto-reconnect on host drop** — *shipped: backoff retry with a manual Reconnect fallback*
- [ ] **Named saved sessions / connection profiles** — saved hosts exist but are browser-local and hostname-only
- [ ] **Customizable keyboard mapping in the UI** (multiple profiles)

## Daily-use fidelity

Behaviours an experienced 3270 operator expects from the terminal itself,
tracked separately from feature parity because they are not features —
they are whether the thing behaves like a terminal. See the
[Enterprise Readiness Audit](enterprise-readiness.md) for the full case.

- [x] **Local cursor navigation** — *shipped: Tab, Back-Tab, arrows and Home move the caret in the browser instead of costing a host round-trip each*
- [x] **Real OIA** — *shipped: `X SYSTEM`, `X -f`, online/application block, insert indicator*
- [x] **Numeric field enforcement** — *shipped: the 3270 numeric attribute now reaches the browser and is enforced, with a real operator-error lock*
- [x] **Insert / overtype toggle** — *shipped: local Insert key, `^` in the OIA*
- [x] **Type-ahead** — *shipped: characters typed during a host wait are buffered, not dropped*
- [x] **Business / Engineering workspace modes** — *shipped: the default surface is the terminal; automation is one click away*
- [ ] **Strict auto-skip semantics** — auto-skip currently fires out of full *numeric* fields, which covers the common real case but is not the full field-attribute rule
- [ ] **Cursor movement over protected areas** — the cursor can only rest in an input field, so all-protected rows are skipped

## s3270 features not yet surfaced

s3270 already supports these — wiring them up is mostly a wrapper job.

- [x] **`String()` for batched field writes** — *shipped: replaces per-character `key(0x..)` loop*
- [ ] **`Transfer()`** — IND$FILE; see *Table-stakes gaps* above
- [ ] **`Snap()`** — point-in-time screen snapshots; enables diffing and regression tests
- [ ] **`Query(host|model|cursor|...)`** — richer status surface for the UI and API
- [x] **`PrintText()`** — *shipped*
- [ ] **`Toggle()` / `Set()`** — runtime resource changes (e.g. monocase, blink)
- [ ] **`Source()` / `Macro()` / `Script()`** — native s3270 scripting
- [ ] **`ScreenTrace`** — event-driven screen-change capture for observability
- [ ] **Explicit `Disconnect`** — today the subprocess is killed; a graceful Disconnect is friendlier to upstream proxies

## Modernization / web-native

- [x] **Public REST/JSON screen API** — *shipped at `/api/v1/*` (see [REST API](rest-api.md))*
- [ ] **OAuth / SAML / OIDC SSO** — Inventu Viewer+ ships this; needed for BYOD and Azure AD orgs
- [ ] **Audit logging** for compliance (who did what, when, against which host)
- [ ] **WCAG 2.1 AA + screen-reader support** — near-zero in the competition, large untapped differentiator
- [ ] **Embed-in-iframe / SPA integration story** documented end-to-end
- [ ] **Mobile / touch UI** — today the UI is desktop-first; touch-friendly keypad and hotspots would unlock tablet use

## Automation / AI

Build on the existing Copilot + chaos + workflow plumbing.

- [ ] **HLLAPI-shape scripting endpoint** for external bots — partially solved by the new REST API; a thinner "HLLAPI compatibility" wrapper could ease migration from PCOMM/Rumba
- [ ] **Macro recorder polish** — the workflow recorder exists; surface it as "macros" with named, parameterized re-runs
- [ ] **Natural-language → keystrokes** — extend the Copilot tool surface with a "describe the screen action you want" action
- [ ] **One-click "Explain this screen"** — Copilot already has `get_screen`; a button can chain it with a fixed prompt

## Differentiator opportunities

If 3270Web wants to lead rather than match, the standouts are:

1. **Accessibility** — almost nobody in this category has a documented WCAG/screen-reader story. A modern web emulator with ARIA labels, keyboard-only operation, and high-contrast themes wins regulated-industry RFPs by default.
2. **AI-assisted use** — the Copilot side panel is already unusual; doubling down on screen-aware Copilot actions, natural-language transactions, and AI explanations puts daylight between 3270Web and every desktop competitor.
3. **Public REST API + workflow JSON** — paired, these make 3270Web the easy choice for RPA and CI integration. Competitors either gate this behind expensive enterprise tiers or don't ship it at all.

## In-flight (this release)

### Terminal / API
- E1: [s3270 `String()` for field writes](#s3270-features-not-yet-surfaced) — performance and observability win; replaces the per-character `key(0x..)` loop noted as a `TODO` since the Java port.
- E2: [Print screen](#table-stakes-gaps-vs-competitors) — table-stakes parity with PCOMM, Rumba+, BlueZone.
- E3: [Public REST API](rest-api.md) — modernization differentiator; reuses the existing Copilot tool plumbing.

### Chaos exploration
- Bug fix: `chaos_save_screen_hint` and `chaos_start` now bind both snake_case (the public MCP form) and camelCase (the legacy UI form). Tool descriptors are unchanged.
- Saturation detection: a run stops early with `terminationReason: "saturated"` once `saturation_steps` consecutive attempts produce no new screen, transition tuple, or value that caused a transition. `coverageStats` is surfaced in `chaos_status`.
- Smarter value generation: built-in corpus expanded with real-looking transaction codes (`LOGON`, `IPL`, `IKJEFT01`, `TSO`, `CICS`, `ISPF`, …) and realistic names; per-field length-probe values (empty / short / half / exact / overflow) cycle through untried length classes.
- Structural dedup as default: mind-map area keys now derive from the layout signature, so screens that differ only by echoed values (e.g. "first name is 1" vs "first name is 3") collapse into one area. Use `dedup_mode: "exact"` for the old raw-hash behaviour.
- Auto-block exit keys: chaos scans the bottom-row PF legend for Exit/Quit/Cancel/Logoff labels and blocks those keys for the rest of the run. Navigation labels (Help/Menu/Next/Prev/Forward/Back) are auto-known.
- `chaos_report` tool returns a Markdown report (ASCII screen graph, per-screen field/key stats, suggested next experiments). Backs the existing in-browser report modal.
- `chaos_status` now supports `?verbose=true`; default response is a slim mind-map summary so polling is cheap.
- Mind-map carry-across via `GET /chaos/mindmap/export` and `POST /chaos/mindmap/import`.
- Optional JSONL transition log per run via `transition_log_path` for offline analysis.
