# Impeccable Audit: 3270Web

Technical quality audit of the web interface, run against `DESIGN.md` (design
document stage) and the code in `web/`. This is a code-and-rendered review only:
nothing here has been fixed.

- Skill: Impeccable 4.10.0 (audit playbook `reference/audit.md`).
- Detector: `impeccable detect` 0.1.27.
- Source scan: `web/` (8 stylesheets, 15 templates, JS modules).
- Rendered scan: headless Chromium (Playwright Chromium 1254), viewports
  1280x800 and 390x844, against a locally built `3270Web` serving `/` (the
  connect page). Only the unauthenticated front door was rendered.

## Audit Health Score

| # | Dimension | Score | Key Finding |
|---|-----------|-------|-------------|
| 1 | Accessibility | 3 | Auth error text is 3.95:1 and input placeholders 4.0:1 on dark themes (WCAG AA 1.4.3). |
| 2 | Performance | 3 | `will-change: transform` is set on every button at rest. |
| 3 | Theming | 3 | Coherent token system, but `DESIGN.md`'s token scale does not cover many values the CSS uses. |
| 4 | Responsive Design | 4 | Touch targets, breakpoints and terminal scroll behaviour are handled deliberately; mobile render is clean. |
| 5 | Implementation Integrity | 4 | Product-specific, coherent system; detector output is mostly advisory or false positive. |
| **Total** | | **17/20** | **Good** |

## Implementation Integrity Verdict

**Pass.** The interface expresses a specific product system, not a generic
app shell: semantic custom properties per theme, a fixed terminal character
grid with chrome kept on the shell, one app bar, a documented elevation ramp,
and a shared modal focus stack. The detector's design-system findings are
mostly `advisory` because `DESIGN.md` records a subset of the real token set,
not because components hardcode arbitrary values. Verified exceptions are the
typography naming (Inter is not shipped) and the contrast issues below.

## Executive Summary

- Audit Health Score: **17/20 (Good)**
- Issues found: **P0 0, P1 2, P2 2, P3 3**
- Top issues:
  1. Auth error messages fail WCAG AA contrast on default dark themes.
  2. Connect-page input placeholder fails WCAG AA contrast.
  3. `will-change` is applied to every button, including at rest.
  4. `DESIGN.md`'s token scale is narrower than what the CSS actually uses.
- Recommended next steps: fix the two contrast failures first, remove the broad
  `will-change`, then reconcile the design document with the real token scale.

## Detailed Findings by Severity

### [P1] Auth error text fails WCAG AA contrast on default dark themes

- **Location**: `web/static/auth.css:268-271` (`--term-color-red`), with the
  default token at `web/static/style.css:105`; rendered in
  `web/templates/login.html:31`, `change-password.html:40`, `setup.html:33`.
- **Category**: Accessibility
- **Impact**: A failed sign-in, setup or password change shows its explanation
  at 3.95:1. Error copy is the one message a user must read to recover.
- **Standard**: WCAG 2.1 AA 1.4.3 Contrast (Minimum), 4.5:1 for body text.
- **Evidence**: `#ff0000` (default `--term-color-red`) on the alert's
  `color-mix(red 12%, panel)` background computes to 3.95:1. Themes that
  override `--term-color-red` (`theme-light`, `theme-modern`, `theme-io-*`) are
  fine; classic, dark, authentic, minimal, slick, yorkshire and custom are not.
- **Recommendation**: Use the documented danger token (`--danger` / `#ff7474`,
  6.13:1 on the panel) or raise the fallback and per-theme values so every
  theme clears 4.5:1 on the tinted error surface.
- **Suggested command**: `$impeccable harden`

### [P1] Connect-page input placeholder fails WCAG AA contrast

- **Location**: `web/templates/connect.html:149` (`placeholder` attribute);
  no `::placeholder` colour rule exists in `web/static/style.css`.
- **Category**: Accessibility
- **Impact**: The example address is 4.0:1 and is the field's only in-place
  hint. The visible label remains, so the page is still usable.
- **Standard**: WCAG 2.1 AA 1.4.3 Contrast (Minimum), 4.5:1 for text.
- **Evidence**: Rendered scan reports `#757575` on `#0f140f` = 4.0:1 at both
  desktop and 390px viewports.
- **Recommendation**: Give `::placeholder` a token colour at or above 4.5:1
  against the sunken input surface (for example `--fg-muted`).
- **Suggested command**: `$impeccable harden`

### [P2] `will-change: transform` is set on every button

- **Location**: `web/static/ui-polish.css:146-156` (`button`, `.h3270-key`,
  `.controls button`).
- **Category**: Performance
- **Impact**: Every button on every page is promoted for a transform
  transition even while idle, spending GPU memory and layer bookkeeping across
  a UI that can show many controls at once.
- **Standard**: Not a WCAG criterion; performance guidance on `will-change`
  overuse.
- **Evidence**: Detector/source: the shared control rule carries
  `will-change: transform`; the two other uses (`style.css:3497` map canvas,
  `style.css:5226` off-screen notification) are scoped and reasonable.
- **Recommendation**: Remove `will-change` from the base button rule and rely
  on the transition; add it only on the specific element about to animate.
- **Suggested command**: `$impeccable optimize`

### [P2] `DESIGN.md` token scale is narrower than the implementation

- **Location**: `DESIGN.md` frontmatter (`rounded`, `spacing`, `typography`,
  `colors`) versus `web/static/*.css`.
- **Category**: Implementation Integrity / Theming
- **Impact**: The detector cannot tell intentional theme tokens from drift, so
  every future audit reports a long advisory tail that hides real issues.
- **Evidence**: 306 `design-system-font-size` advisories (e.g. `.9rem`,
  `.82rem`, `.78rem`), 137 `design-system-radius` advisories (8px, 10px, 4px,
  2px), 112 `design-system-color` advisories (theme palette declarations such
  as `#ffd166`, `#ff6b6b`, plus auth fallbacks). Most are legitimate token
  declarations or intermediate sizes, not arbitrary component values.
- **Recommendation**: Reconcile the document stage: record the intermediate
  type sizes and radii the CSS actually uses, and state that per-theme palette
  declarations are expected. Do not change UI to satisfy the detector.
- **Suggested command**: `$impeccable document`

### [P3] Inter is named as the interface face but not shipped

- **Location**: `web/static/style.css:42`, `web/static/ui-polish.css:119`;
  `web/static/fonts/` contains only the IBM 3270 faces.
- **Category**: Theming / Implementation Integrity
- **Impact**: Rendering depends on whether the viewer has Inter installed;
  otherwise the UI falls back to Segoe UI or `system-ui`, so the recorded
  typography is not what every user sees.
- **Evidence**: No `@font-face` for Inter and no Inter font file; only
  `3270-Regular`, `3270Condensed`, `3270SemiCondensed`.
- **Recommendation**: Either vendor Inter (with a licence check) or document
  the system-UI stack as the real interface face in `DESIGN.md`.
- **Suggested command**: `$impeccable typeset`

### [P3] Connect dialogs skip from `h1` to `h3`

- **Location**: `web/templates/connect.html:88` (`h1`) and dialog titles at
  `:184`, `:237`, `:261` (`h3`).
- **Category**: Accessibility
- **Impact**: Heading order is not purely sequential for screen-reader users
  navigating by heading; each dialog is still labelled via `aria-labelledby`.
- **Standard**: WCAG 1.3.1 is met by the label; heading hierarchy is a best
  practice, not a failure on its own.
- **Recommendation**: Promote the dialog titles to `h2`, or confirm the modal
  pattern intentionally starts a new outline.
- **Suggested command**: `$impeccable harden`

### [P3] Terminal font faces are missing from `DESIGN.md` typography

- **Location**: `web/static/style.css:11-27` (Semi-Condensed, Condensed
  `@font-face`), `web/static/menu-bar.css:139,443` (`SF Mono`).
- **Category**: Implementation Integrity
- **Impact**: The document lists IBM 3270 and monospace but not the condensed
  variants or the macOS-only `SF Mono` host chip, so future work may treat real
  faces as drift.
- **Recommendation**: Add them to the typography section as intentional
  variants.
- **Suggested command**: `$impeccable document`

## Patterns & Systemic Issues

- **Contrast is strong almost everywhere except red error states.** The token
  system deliberately lifted host blue and chose `#ff7474` for danger, but the
  auth error path still consumes the raw terminal red. This looks like one
  forgotten surface rather than a systemic colour problem.
- **Documentation drift, not implementation drift.** The detector's advisory
  tail comes from a design document that records a curated subset; the CSS
  itself is token-driven. Reconcile the document rather than editing UI.
- **Performance hygiene is otherwise deliberate**: lazy-ish deferred scripts,
  scoped animations, `prefers-reduced-motion` alternatives including a global
  safety net, no raster images on the front door.

## False Positives and Intentional Findings

- `ai-color-palette` on every template: driven by the 3270 host palette
  (`--term-color-pink`, host colours), not an AI-purple aesthetic.
- `broken-image` in `brand.html`: the string is inside a comment; the brand is
  inline SVG with no `<img>`.
- `pulsing-dot` (36): live, recording, typing and error indicators. Documented
  in `DESIGN.md`; reduced-motion alternatives exist.
- `tiny-text` (11px) and `overused-font` (Inter): 11px is the documented
  `label` size; Inter is the documented UI face.
- `cramped-padding` on `screen-oia` and `terminal-size-modes`: deliberate
  edge-to-edge status strip and joined segmented control.
- `nested-cards`: detector heuristic over the terminal shell and modal markup.
- `side-tab`: accent left borders are used for alerts and active rows, a
  consistent pattern in this codebase.

## Positive Findings

- Shared modal stack (`modal-utils.js`) owns Escape, initial focus, Tab
  trapping, scroll lock and focus return in one place.
- Menubar implements arrow-key, Home/End and typeahead behaviour with correct
  `menuitem`/`menuitemcheckbox` roles.
- Touch targets are raised to 44px on coarse pointers across the app bar,
  dialogs, connect form and terminal keys.
- `prefers-reduced-motion` has targeted alternatives (blink highlight, free
  cursor, OIA error pulse) plus a global safety net.
- Live status uses dedicated `aria-live` spans rather than announcing the
  cursor on every keystroke.
- Semantic tokens per theme; the terminal grid stays fixed and scrolls rather
  than reflowing, as designed.

## Checks That Could Not Be Run

These require access or tooling not available in this environment:

- A live TN3270 session and the authenticated/admin pages were not rendered.
  The bundled sample app was not launched; only `/` was scanned.
- Screen-reader testing (NVDA, VoiceOver, TalkBack) and real touch gestures on
  a physical device were not exercised.
- Per-theme contrast was not captured for all ~12 built-in themes plus
  operator-authored custom themes; only the default unthemed front door was
  measured.
- No screenshots or visual-regression goldens were compared.

## Recommended Actions

1. **[P1] `$impeccable harden`**: Fix auth error contrast (`auth.css:268-271`)
   and connect placeholder contrast (`connect.html:149` + `::placeholder`).
2. **[P2] `$impeccable optimize`**: Remove `will-change` from the shared button
   rule (`ui-polish.css:146-156`).
3. **[P2] `$impeccable document`**: Reconcile `DESIGN.md` tokens with the real
   type, radius and per-theme colour values.
4. **[P3] `$impeccable typeset`**: Decide Inter shipping versus system-UI stack.
5. **[P3] `$impeccable harden`**: Fix the connect dialog heading levels.
6. **[P3] `$impeccable document`**: Record the condensed and `SF Mono` faces.
7. **`$impeccable polish`**: Final pass once the above are addressed.

---

Target: `web` (whole web UI). Re-run `$impeccable audit` after fixes to see the
score improve.
