# Impeccable Audit: 3270Web

A technical quality audit of the 3270Web web interface, written so that another
agent can implement each finding without having seen the audit run. Nothing in
this document has been fixed; implementation belongs to a later stage.

## How this audit was run

- **Date:** 2026-11-28
- **Commit audited:** `7c642df9f6d8d2d13168af72284147a6f118bcc1` (`origin/main`; a
  merge of the document stage over the audit-stage parent `4b2d61e`).
- **Skill / tooling:** Impeccable 4.10.0, audit playbook
  `reference/audit.md`; detector `impeccable detect` 0.1.27.
- **Steps used:** `impeccable context` (loaded `PRODUCT.md`, `DESIGN.md` and
  `.impeccable/design.json` with no `CONTEXT_STALE`); source scan
  `impeccable detect web/ --json`; rendered scan
  `impeccable detect http://127.0.0.1:33271/ --viewport 1280x800 --json` and
  the same URL at `--viewport 390x844`, against a locally built `3270Web`
  serving the unauthenticated connect page.
- **Scope:** the `web/` tree (stylesheets, templates, JS) and the rendered
  connect page. This is a code-and-rendered review, not a manual screen-reader
  or physical-device test.
- **Prior report:** a pre-existing `AUDIT.md` at the repo root records an
  earlier audit pass over the same code. It was not modified. This file is the
  deliverable for the current stage and re-verifies and expands that pass
  against the commit above.

### Audit health score

| # | Dimension | Score | Key finding |
|---|-----------|-------|-------------|
| 1 | Accessibility | 3 | Auth error text is 3.9:1 and the connect placeholder 4.0:1 (WCAG AA 1.4.3). |
| 2 | Performance | 3 | `will-change: transform` is declared on every button at rest. |
| 3 | Theming | 3 | A coherent token system, but `DESIGN.md` records far fewer type, radius and colour values than the CSS uses. |
| 4 | Responsive Design | 4 | Mobile render at 390×844 is clean; touch targets, breakpoints and terminal scrolling are deliberate. |
| 5 | Implementation Integrity | 4 | Product-specific and coherent; most detector output is intentional or a heuristic false positive. |
| **Total** | | **17/20** | **Good** |

### Issue counts

**P0 0 · P1 2 · P2 2 · P3 16** (20 numbered findings). Twelve of the P3 items
are detector heuristics that this audit reviewed and disputes or marks
intentional; they are retained below as required, each with the reasoning. Two
detector findings are merged into others where they describe the same
underlying issue.

### Implementation integrity verdict

**Pass.** The interface expresses a specific product system rather than a
generic app shell: semantic custom properties per theme, a fixed terminal
character grid with chrome kept on the shell, a single app bar, a documented
elevation ramp and a shared modal focus stack. Verified exceptions are the
typography (Inter is named but not shipped), the contrast issues, the broad
`will-change`, and documentation drift in `DESIGN.md`.

---

## Findings

### A. Accessibility

#### 1. [P1] Auth error text fails WCAG AA contrast on the default dark themes

- **Where:** `web/static/auth.css:268-272` (`.auth-alert--error` consumes
  `var(--term-color-red)`); default token `--term-color-red: #ff0000` at
  `web/static/style.css:105`. Rendered by `web/templates/login.html` (error
  alert), `web/templates/change-password.html` and `web/templates/setup.html`.
- **What is wrong:** The error alert paints `#ff0000` text on the tinted error
  surface `color-mix(red 12%, panel)`, measured at **3.9:1** (`#ff0000` on
  `#2c210d`). WCAG 2.1 AA 1.4.3 requires 4.5:1 for body text. Themes that
  override `--term-color-red` (`theme-light`, `theme-modern`, `theme-io-*`)
  pass; classic, dark, authentic, minimal, slick, yorkshire and custom do not.
- **Why it matters:** Error copy is the one message a user must read to recover
  from a failed sign-in, setup or password change. Failing contrast makes the
  recovery path hardest to read for the users who need it most.
- **Concrete fix:** Point `.auth-alert--error` at the documented danger token
  (`--danger` / `#ff7474`, which is 6.13:1 on the panel) or raise the fallback
  and every per-theme `--term-color-red` so the tinted error surface clears
  4.5:1. Keep the terminal's own red for terminal cells; change only the alert
  colour. Re-measure each theme after the token change.
- **Size / risk:** **Small** code change, but touches up to 12 theme token
  blocks — **medium risk** of one theme being missed. Verify with the detector
  on each theme.

#### 2. [P1] Connect-page input placeholder fails WCAG AA contrast

- **Where:** `web/templates/connect.html:149` (`<input id="hostname-input" …
  placeholder="mainframe.example.com:992">`); there is no `::placeholder`
  colour rule in `web/static/style.css`. The rendered scan reports the
  placeholder at **4.0:1** (`#757575` on `#0f140f`) at both 1280×800 and
  390×844.
- **What is wrong:** The in-field example address is below 4.5:1. The visible
  `<label>` remains, so the page is still usable, but the only in-place hint
  is under-contrast.
- **Why it matters:** WCAG 1.4.3 applies to placeholder text. The address
  format is the field's only example, and low-vision users lose it.
- **Concrete fix:** Add a `::placeholder` colour at or above 4.5:1 against the
  sunken input surface (for example the documented `--fg-muted`, after
  verifying its computed value on `--bg`). Do not lighten the input background
  instead; keep the terminal-like sunken field.
- **Size / risk:** **Small**, **low risk**. One shared rule plus a re-render at
  both viewports.

#### 3. [P3] Connect dialogs skip from `h1` to `h3`

- **Where:** `web/templates/connect.html:88` (`<h1>3270Web</h1>`) and the dialog
  titles at approximately `:184` (Settings), `:237` (Start sample app) and
  `:261` (About).
- **What is wrong:** The dialog titles use `<h3>` with no `<h2>` between them
  and the page `h1`.
- **Why it matters:** Each dialog is already labelled via `aria-labelledby`, so
  WCAG 1.3.1 is met, but screen-reader users navigating by heading see a broken
  outline. Heading hierarchy is a best practice here, not a standalone failure.
- **Concrete fix:** Promote the three dialog titles to `<h2>` (or add a visible
  or visually-hidden `h2` section heading if the modal pattern intentionally
  starts a new outline). Do not change the dialog structure otherwise.
- **Size / risk:** **Small**, **low risk**.

#### 4. [P3] [Disputed] Tiny body text at 11px

- **Where:** `web/templates/screen.html`; detector reports "11px body text".
- **What is wrong (detector):** Body text below 12px is hard to read.
- **Dispute:** 11px is the documented `label` size (`0.6875rem` in `DESIGN.md`)
  used for status and metadata, not body prose, and it is paired with the
  terminal-sized type system. The detector cannot tell a label from body copy.
  No change is recommended unless a specific 11px element is confirmed to carry
  sentence-length reading text.
- **Concrete fix if it is genuinely body copy:** raise that element to at least
  12px (ideally 14px); otherwise leave it and add no ignore rule.
- **Size / risk:** **Small**, **low risk**.

---

### B. Performance

#### 5. [P2] `will-change: transform` is set on every button at rest

- **Where:** `web/static/ui-polish.css:146-156` — the shared `button`,
  `.h3270-key`, `.controls button` rule carries `will-change: transform`.
- **What is wrong:** Every button on every page is promoted to its own
  compositing layer permanently, even while idle. The other two `will-change`
  uses are scoped and reasonable: `web/static/style.css:3497` (map canvas) and
  `web/static/style.css:5226` (the off-screen notification as it slides).
- **Why it matters:** A terminal UI can show many controls at once; keeping a
  layer for each spends GPU memory and layer bookkeeping for a one-pixel hover
  lift. It is a targeted hint for a known expensive animation, not a baseline.
- **Concrete fix:** Remove `will-change: transform` from the base control rule
  and rely on the existing `transform` transition. If a specific element
  stutters, add `will-change` only to that element and only while it is about
  to animate.
- **Size / risk:** **Small**, **low risk**.

#### 6. [P3] [Disputed] Bounce / elastic easing

- **Where:** `web/static/style.css:5225` (`cubic-bezier(0.34, 1.42, 0.64, 1)` on
  the `h3270-notification` slide-in), `web/static/style.css:6052`
  (`copilot-typing-bounce`), and the vendored
  `web/static/lib/tippy-bundle.umd.min.js` / `tippy.css`
  (`cubic-bezier(.54, 1.5, .38, 1.11)`).
- **What is wrong (detector):** Overshoot easing reads as playful rather than
  precise.
- **Dispute:** The notification overshoot is a deliberate, brief attention
  cue on a transient toast; the "typing bounce" animates opacity only, not
  position; the Tippy curve is a third-party file that should not be forked for
  this. None is used on a primary control or on the terminal grid. Keep them.
- **Concrete fix (if changed at all):** Replace only the notification curve
  with the project's `--ease-out` token; do not edit vendored files.
- **Size / risk:** **Small**, **low risk**.

---

### C. Theming and design-system integrity

#### 7. [P2] `DESIGN.md` token scale is narrower than the implementation

- **Where:** `DESIGN.md` frontmatter (`typography`, `rounded`, `spacing`,
  `colors`) versus `web/static/*.css`.
- **What is wrong:** The detector reports **306** `Font size outside DESIGN.md`
  advisories (e.g. `.9rem`, `.82rem`, `.78rem`), **137** `Radius outside
  DESIGN.md` advisories (8px, 10px, 4px, 2px) and **112** `Color outside
  DESIGN.md` advisories (per-theme palette declarations such as `#ffd166`,
  `#ff6b6b`, plus auth fallbacks). Most are legitimate token declarations or
  intermediate sizes, not arbitrary component values.
- **Why it matters:** The detector cannot separate intentional theme values
  from drift, so every future audit carries a long advisory tail that buries
  real findings.
- **Concrete fix:** Do not edit UI to satisfy the detector. Extend `DESIGN.md`
  (and `.impeccable/design.json`) to record the intermediate type sizes and
  radii the CSS actually uses, and state explicitly that per-theme palette
  declarations and auth fallbacks are expected. The design-document stage owns
  this change.
- **Size / risk:** **Medium** documentation edit, **low risk** (no code).

#### 8. [P3] Inter is named as the interface face but is not shipped

- **Where:** `web/static/style.css:42` and `:5218`,
  `web/static/ui-polish.css:119`; `web/static/fonts/` contains only the IBM 3270
  faces (`3270-Regular`, `3270Condensed`, `3270SemiCondensed`). Detector also
  reports `Overused font` (15 occurrences); **this finding merges that rule
  here** because the actionable defect is the missing shipped face, not the
  choice of face.
- **What is wrong:** Rendering depends on whether the viewer has Inter
  installed; otherwise the UI falls back to Segoe UI / `system-ui`, so the
  documented typography is not what every user sees.
- **Why it matters:** The design document and the shipped rendering disagree,
  and line lengths/weights change with the fallback.
- **Concrete fix:** Either vendor Inter with `@font-face` (check the licence and
  subset/woff2 size) or change `DESIGN.md` to name the system-UI stack as the
  real interface face and drop "Inter" from the CSS stack. Pick one; do not
  leave the document claiming a face that is not loaded. The "overused font"
  warning is not by itself a reason to change a deliberate brand face.
- **Size / risk:** **Medium** (font vendoring adds a binary asset and loading
  cost), **medium risk**; documenting the system stack is **small / low risk**.

#### 9. [P3] Condensed terminal faces and `SF Mono` are missing from `DESIGN.md`

- **Where:** `@font-face` declarations at `web/static/style.css:11-27`
  (IBM 3270 Semi-Condensed and Condensed), and `SF Mono` at
  `web/static/menu-bar.css:139` and `:443`. Detector reports these as
  `Font outside DESIGN.md`.
- **What is wrong:** The document lists IBM 3270 and monospace but not the
  condensed variants or the macOS-only host-chip face.
- **Why it matters:** Future work may treat these real faces as drift and
  "correct" them.
- **Concrete fix:** Add them to the typography section as intentional variants,
  noting that `SF Mono` is a macOS-only first choice with a documented fallback.
- **Size / risk:** **Small**, **low risk**.

#### 10. [P3] [Disputed] Side-tab accent borders (15 occurrences)

- **Where:** `web/static/style.css:1011, 1781, 4253, 6296, 6348`;
  `web/static/touch.css:410`; `web/static/ui-polish.css:1440, 1606, 1667, 1928,
  3629, 3776`; `web/templates/error.html`; `web/templates/screen.html`.
- **What is wrong (detector):** A coloured bar down one side of a rounded box is
  flagged as a generic card-decoration tell.
- **Dispute:** In this codebase the left rule is a consistent semantic marker
  for alerts, active rows, run state and error surfaces, not decoration; the
  colours come from `--accent`, `--warn`, `--danger` and `--info`. Removing it
  would flatten meaningful state. No change recommended.
- **Concrete fix if a future redesign wants to drop it:** replace the rule with
  a background tint plus a leading status label in the same components; do not
  change one instance in isolation.
- **Size / risk:** **Small**, **low risk** if left alone.

#### 11. [P3] [Disputed] Glowing shadow accents (13 occurrences)

- **Where:** Rendered on every template (`#35d07f` glow) and on
  `web/templates/screen.html` (`#39ff14`, zero-offset). Source:
  `web/static/ui-polish.css:465-474` (the `--ok` live-status ring pulse) and
  `web/static/style.css:605-606` (a status chip).
- **What is wrong (detector):** Coloured glow shadows are a generic "AI UI"
  tell.
- **Dispute:** These are status-indicator pulse rings tied to live state, not
  ambient card glow; the animation is documented and has a
  `prefers-reduced-motion` alternative. The terminal's phosphor aesthetic makes
  a green halo intentional. No change recommended.
- **Concrete fix if changed:** none required; if the glow is ever decorative,
  replace it with the neutral elevation ramp.
- **Size / risk:** **Small**, **low risk**.

---

### D. Copy and structure

#### 12. [P3] "Groups" is repeated three times inside one dialog

- **Where:** `web/templates/admin-users.html` — the groups dialog
  (`div.admin-dialog-content`, around `:206-261`): the heading, the column
  label and the field label all say "Groups"; detector reports three distinct
  spots.
- **What is wrong:** The same literal text is rendered three times in the same
  panel, which is redundant messaging rather than useful labelling.
- **Why it matters:** Repetition adds noise in a small modal and makes the
  primary action harder to scan.
- **Concrete fix:** Say "Groups" once as the dialog heading, keep the field
  label only if it is still needed for the control's accessible name, and use
  the table/cell label or helper copy for the third slot. Do not remove the
  control label without a replacement accessible name.
- **Size / risk:** **Small**, **low risk**.

#### 13. [P3] [Disputed] Mobile straggler: "account." alone on the last line

- **Where:** Rendered connect page at 390×844; `web/templates/connect.html`
  helper copy (`#connect-hint`).
- **What is wrong (detector):** The last line of a three-line paragraph holds a
  single short word, which reads as an orphan.
- **Dispute:** This is viewport- and font-dependent copy reflow, not a stable
  defect, and forcing a line break would break at other widths. Fix only if the
  sentence is being edited for other reasons.
- **Concrete fix if touched:** shorten the sentence or allow a slightly wider
  measure; do not add `<br>`.
- **Size / risk:** **Small**, **low risk**.

#### 14. [P3] [Disputed] Cramped padding in admin table wrappers

- **Where:** `.admin-table-wrap` in `web/templates/admin-audit.html`,
  `admin-groups.html`, `admin-overview.html`, `admin-sessionscreen.html` and
  `admin-users.html`; styles at `web/static/auth.css:469` and
  `web/static/admin.css:183`.
- **What is wrong (detector):** A bordered/background surface whose children sit
  flush against the edge.
- **Dispute:** The `table.admin-table` cells carry `11px 14px` padding, so the
  rendered text is inset even though the wrapper itself has none; at 720px and
  below the wrapper deliberately drops its border and background. The detector
  appears to measure the inner scroller, not the cell text. Verify visually; no
  change unless a real flush edge is seen.
- **Concrete fix if confirmed:** add `padding` to `.admin-table-wrap` or inset
  `.admin-table-scroll`, and recheck the 720px breakpoint where the wrapper is
  intentionally flat.
- **Size / risk:** **Small**, **low risk**.

---

### E. Detector findings reviewed and dismissed

These were produced by the detector and are retained here so the record is
complete. None requires an implementation change.

#### 15. [P3] [Disputed] Broken or placeholder image in `brand.html`

- **Where:** `web/templates/brand.html`.
- **Detector claim:** `<img>` with no `src`.
- **Dispute:** The string is inside an HTML comment; the brand is inline SVG
  with no shipped `<img>`. No change.

#### 16. [P3] [Disputed] Nested cards on the terminal screen

- **Where:** `web/templates/screen.html` (two findings; rendered connect scan
  also reports one).
- **Detector claim:** Card inside card creates visual noise.
- **Dispute:** The terminal shell, status strips and modal surfaces are
  intentionally layered chrome; the nesting is structural (terminal inside the
  session card), not decorative depth. Documented in `DESIGN.md`. No change.

#### 17. [P3] [Disputed] AI colour palette across 12 templates

- **Where:** every template.
- **Detector claim:** Purple/violet accent colours.
- **Dispute:** The palette is the 3270 host palette (`--term-color-pink`,
  host colours, the documented phosphor/amber/paper system), not an
  AI-purple gradient. The purple is semantically the host's magenta. No change.

#### 18. [P3] [Disputed] Pulsing status dots (36 occurrences)

- **Where:** `.recording-indicator`, `.copilot-typing-dot` and the
  `polish-live-pulse` chrome dot, reported against all 12 templates; source in
  `web/static/style.css:721` and `web/static/ui-polish.css`.
- **Detector claim:** Decorative pulsing simulates liveness.
- **Dispute:** They are tied to genuinely changing state (recording, typing,
  live session) and have `prefers-reduced-motion` alternatives. Documented in
  `DESIGN.md`. No change.

#### 19. [P3] [Disputed] Decorative grid-line background / repeating-gradient stripes

- **Where:** `web/static/style.css:3450` and all templates (13 + 12 reports).
  These are the same two-axis grid texture; **merged here**.
- **Detector claim:** Repeating-gradient decorative stripes.
- **Dispute:** The faint grid is the terminal/console ground texture that the
  design document establishes; it is low-contrast and non-interactive. No
  change.

#### 20. [P3] [Disputed] "Cramped padding" on `screen-oia` and `terminal-size-modes`

- **Where:** `web/templates/screen.html`.
- **Detector claim:** Edge-to-edge status strip and joined segmented control.
- **Dispute:** Both are deliberate: the OIA strip is an edge-to-edge status bar
  and the size-mode control is a joined segmented group. Documented behaviour.
  No change.

---

## Positive findings

- The shared modal stack (`web/static/modal-utils.js`) owns Escape, initial
  focus, Tab trapping, scroll lock and focus return in one place.
- The menubar implements arrow-key, Home/End and typeahead behaviour with
  correct `menuitem` / `menuitemcheckbox` roles.
- Touch targets are raised to 44px on coarse pointers across the app bar,
  dialogs, the connect form and terminal keys.
- `prefers-reduced-motion` has targeted alternatives (blink highlight, free
  cursor, OIA error pulse) plus a global safety net.
- Live status uses dedicated `aria-live` spans rather than announcing the cursor
  on every keystroke.
- Semantic tokens per theme; the terminal grid stays fixed and scrolls rather
  than reflowing, as designed.
- The mobile render at 390×844 has no horizontal overflow and keeps the
  terminal usable.

## Checks that could not be run

- A live TN3270 session and the authenticated/admin pages were not rendered or
  exercised. The bundled sample app was not launched; the rendered scan covered
  only the unauthenticated `/` (connect) page.
- Screen-reader testing (NVDA, VoiceOver, TalkBack) and real touch gestures on a
  physical device were not exercised.
- Per-theme contrast was not captured for all ~12 built-in themes plus
  operator-authored custom themes; only the default unthemed front door was
  measured. Finding 1 lists which themes remain to be checked after the token
  change.
- No screenshot or visual-regression goldens were compared.

## Suggested implementation order

1. **[P1] Accessibility:** fix auth error contrast (finding 1) and connect
   placeholder contrast (finding 2); re-run the detector on the affected pages
   and at least one dark and one light theme.
2. **[P2] Performance:** remove the base-rule `will-change` (finding 5).
3. **[P2] Documentation:** reconcile `DESIGN.md` / `.impeccable/design.json`
   tokens (finding 7).
4. **[P3]:** decide whether to ship Inter or document the system stack
   (finding 8), then add the missing faces (finding 9), fix the dialog heading
   levels (finding 3), and de-duplicate the groups dialog copy (finding 12).
5. Re-run `impeccable audit` after fixes to confirm the score and close the
   disputed items that remain intentional.

---

*No application source, styles, tests or build configuration was modified to
produce this audit. Findings are recorded only; implementation is a later
stage.*
