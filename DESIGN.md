---
name: 3270Web
description: A browser TN3270 terminal and operations console for mainframe work.
colors:
  phosphor-green: "#39ff14"
  phosphor-bright: "#7cff3b"
  phosphor-muted: "#8bdc79"
  forest-bg: "#0b1a0b"
  forest-panel: "#0f260f"
  forest-panel-raised: "#123012"
  forest-border: "#1f3b1f"
  accent-contrast: "#000000"
  terminal-blue: "#5c7cff"
  status-ok: "#35d07f"
  status-warn: "#f5b544"
  status-danger: "#ff7474"
  status-info: "#4aa8ff"
typography:
  display:
    fontFamily: "Inter, Inter var, Segoe UI Variable Text, Segoe UI, system-ui, -apple-system, sans-serif"
    fontSize: "1.5rem"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "0em"
  title:
    fontFamily: "Inter, Inter var, Segoe UI Variable Text, Segoe UI, system-ui, -apple-system, sans-serif"
    fontSize: "1.0625rem"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "0em"
  body:
    fontFamily: "Inter, Inter var, Segoe UI Variable Text, Segoe UI, system-ui, -apple-system, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "Inter, Inter var, Segoe UI Variable Text, Segoe UI, system-ui, -apple-system, sans-serif"
    fontSize: "0.6875rem"
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: "0.01em"
  terminal:
    fontFamily: "IBM 3270, Cascadia Mono, Consolas, Courier New, monospace"
    fontSize: "12pt"
    fontWeight: 400
    lineHeight: "12pt"
    letterSpacing: "0em"
rounded:
  xs: "6px"
  sm: "9px"
  md: "12px"
  lg: "16px"
  xl: "22px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "28px"
components:
  button:
    backgroundColor: "{colors.forest-panel-raised}"
    textColor: "{colors.phosphor-green}"
    rounded: "{rounded.sm}"
    padding: "8px 10px"
  button-primary:
    backgroundColor: "{colors.phosphor-green}"
    textColor: "{colors.accent-contrast}"
    rounded: "{rounded.sm}"
    padding: "7px 12px"
  button-danger:
    backgroundColor: "{colors.forest-panel-raised}"
    textColor: "{colors.status-danger}"
    rounded: "{rounded.sm}"
    padding: "8px 10px"
  input:
    backgroundColor: "{colors.forest-bg}"
    textColor: "{colors.phosphor-green}"
    rounded: "{rounded.sm}"
    padding: "8px 10px"
  card:
    backgroundColor: "{colors.forest-panel}"
    textColor: "{colors.phosphor-green}"
    rounded: "{rounded.xl}"
    padding: "28px"
  menu-item:
    backgroundColor: "{colors.forest-panel}"
    textColor: "{colors.phosphor-green}"
    rounded: "{rounded.sm}"
    padding: "7px 9px"
  chip:
    backgroundColor: "{colors.forest-panel}"
    textColor: "{colors.phosphor-muted}"
    rounded: "{rounded.pill}"
    padding: "3px 8px"
  terminal-screen:
    backgroundColor: "{colors.forest-bg}"
    textColor: "{colors.phosphor-green}"
    rounded: "{rounded.md}"
    padding: "4px 0"
---

# Design System: 3270Web

## Overview

**Creative North Star: "The Phosphor Cockpit"**

3270Web is a modern operator console that borrows the trust and legibility of a
3270 tube without imitating one literally. The product is an *Operate* surface:
the visitor is mid-task, often in the middle of a live host session, so the
system favours scanability, predictable controls and honest state over
expression. Personality comes from the phosphor palette, the monospace terminal
face and precise hairline chrome — not from ornament.

The visual system is a **semantic token layer re-skinned by theme**. Colours,
surfaces, borders and accents are declared once as custom properties on
`:root`, overridden per theme on `body.theme-*`, and derived again in
`web/static/ui-polish.css` (`--surface-raised`, `--hairline`, `--accent-soft`,
`--ring`, `--elev-*`). Components never hardcode a palette; they consume the
tokens. The frontmatter records the `:root` fallback palette, which is the
unthemed green phosphor. Around it live roughly a dozen built-in themes, from
`Amber Phosphor` and `Paper Terminal` to the shared `3270.io` Phosphor / Amber /
Ice / Daylight palettes, plus operator-authored custom themes.

Density is high and chrome is quiet. A 46px app bar replaces what used to be
three separate control surfaces, session tabs attach directly beneath it, and
the terminal itself is the only large object on the page. Depth is carried by a
four-step shadow ramp and 1px translucent hairlines; almost everything is flat
at rest.

**Key Characteristics:**
- Semantic CSS custom properties per theme; no per-theme component CSS.
- Phosphor accent used as an interaction signal, not as a large fill.
- Inter for the interface, IBM 3270 for the character grid; tabular numerals.
- Hairline borders and a four-step elevation ramp instead of heavy dividers.
- A centred page shell around a wide, fixed-geometry terminal card.
- State is always visible (recording, playback, chaos, live status). Automation
  never hides what the host is doing.

## Colors

A dark, saturated phosphor palette on near-black forest surfaces, with a
single bright accent and muted secondary text.

### Primary
- **Phosphor Green** (#39ff14): the default accent and default text ink. It
  marks interactive affordances, active states and terminal output; in the
  unthemed palette `--fg` and `--accent` are the same value.
- **Phosphor Bright** (#7cff3b): the intensified terminal ink (`--accent-2`),
  used for highlighted fields and secondary emphasis.
- **Terminal Blue** (#5c7cff): the lifted host-blue ink. Pure 3270 blue is
  unreadable on dark screens, so text drawn in host blue is raised to this
  lighter blue while a host's blue *background* keeps the true colour.

### Neutral
- **Forest Background** (#0b1a0b): the page and terminal ground (`--bg`).
- **Forest Panel** (#0f260f): the card and menu-panel surface (`--panel`).
- **Forest Panel Raised** (#123012): the default button and control fill
  (`--panel-2`), and the base of `--surface-raised`.
- **Forest Border** (#1f3b1f): the base of every hairline (`--border`); actual
  borders are mixed toward transparency (`--hairline`, `--hairline-soft`).
- **Phosphor Muted** (#8bdc79): secondary text (`--fg-muted`) for captions,
  menu subgroups and metadata.

### Semantic
- **Status OK** (#35d07f base, blended toward the accent): live and success
  indicators.
- **Status Warn** (#f5b544): pending, paused and caution states.
- **Status Danger** (#ff7474): destructive actions and failures. Chosen over
  #ff6b6b because the lighter red clears 3:1 on the tinted danger button.
- **Status Info** (#4aa8ff base, blended toward the accent): informational
  notices.
- **Accent Contrast** (#000000): ink on an accent fill. Bright phosphor accents
  swallow white text; the two paper themes swap this back to white.

### Named Rules
**The Token Rule.** New UI never writes a colour literal. Declare or consume a
custom property; if a theme needs a different value, override the property on
`body.theme-*`, never the component rule.

**The Rarity Rule.** The accent is an interaction signal — focus, active,
selected, live. It fills small primary buttons and status pills; it does not
flood backgrounds or headings.

**The Lifted-Ink Rule.** Host blue text uses `--term-color-blue`, never pure
blue, except in `theme-authentic`, which exists to reproduce the original
palette.

## Typography

**Display Font:** Inter (with Inter var, Segoe UI, system-ui)
**Body Font:** Inter (with Segoe UI, system-ui, -apple-system)
**Terminal Font:** IBM 3270 (with Cascadia Mono, Consolas, Courier New)

**Character:** A neutral, high-legibility interface face set against a strict
monospace character grid. The contrast between proportional chrome and fixed
cell geometry is the type system's whole idea: prose stays readable, the host
stays a grid.

### Hierarchy
- **Display** (600, 1.5rem, 1.2): the card title / brand heading. Card headers
  use 500 in the polish layer; the 600 cap applies everywhere else.
- **Title** (600, 1.0625rem, 1.3): modal and dialog titles.
- **Body** (400, 1rem, 1.5): default interface text. Secondary copy drops to
  `--fg-muted` at 0.82–0.95rem.
- **Label** (600, 0.6875rem, 0.01em): menu headings, panel titles, chips and
  micro-labels. Uppercase is a component choice, not a global rule.
- **Terminal** (400, 12pt, tracked to the cell grid): the 3270 character grid.
  `--terminal-cell-size` defaults to 12pt and is user-adjustable; ligatures,
  kerning and optical sizing are explicitly disabled so column position holds.

### Named Rules
**The Weight Ceiling Rule.** Strong text and headings top out at 600; the
browser's 700 default is never used for emphasis.

**The Grid Rule.** Terminal text never uses the UI font stack. It uses the
`--mono` / IBM 3270 faces with tabular numerals and no ligatures, because a
column of characters is information.

## Layout

A centred page shell wraps one wide card. `.page-wrap` centres content with
`48px 20px` padding (reduced to `12px` in terminal-fit mode), and `.card` is
`width: fit-content`, `min-width: min(920px, 100%)`, `max-width: calc(100vw -
40px)` with `28px` internal padding. The terminal card is intentionally wider
than a reading column: it must fit the host's character grid.

The session page stacks a single app bar (46px minimum height), optional
session tabs beneath it, a context bar while a workflow runs, and the terminal
shell. `.terminal-shell` carries `14px` of chrome padding so terminal geometry
stays untouched; `.screen-container` is sized in JS to `cols × charWidth + 2`
and is never padded horizontally.

Spacing runs on a loose 4px rhythm — 4, 6, 8, 10, 12, 14, 16, 18, 28 — with
`8px` as the common gap, `12–16px` for group separation and `28px` for card
padding. Density is compact, not cramped: controls sit at 32–34px.

Responsive behaviour is desktop-first and collapses in stages: the app bar
drops to icon triggers around 1080px and 900px, dialogs and panels narrow at
820/720px, the card and terminal chrome tighten at 640px and 560px, and phones
below ~380px get the touch layout. The terminal itself scrolls rather than
reflows; column position is information, so a two-dimensional grid is allowed
to be larger than the viewport.

### Named Rules
**The Terminal-Geometry Rule.** Never add horizontal padding, flex shrink or
responsive reflow to `.screen-container`. Chrome belongs on the parent
(`.terminal-shell`); the grid keeps its computed width.

**The One-Bar Rule.** Session controls live in the app bar and its menus. Do
not reintroduce floating tool panels over the terminal, and do not add a second
toolbar row.

## Elevation & Depth

The system is **flat at rest with a four-step shadow vocabulary**. Depth is
tonal first (panel → panel-2 → raised), then shadow. Shadows mark lifted
objects — cards, menus, modals, the command palette — and never decorate static
text. Dark themes use deep black shadows; the light themes (`theme-light`,
`theme-not3270`, `theme-io-daylight`) override the same tokens with soft warm
shadows and a lighter scrim.

### Shadow Vocabulary
- **elev-1** (`0 1px 2px rgba(0,0,0,0.3), 0 1px 1px rgba(0,0,0,0.18)`): the app
  bar, active-run rows, resting raised controls.
- **elev-2** (`0 4px 12px -2px rgba(0,0,0,0.42), 0 2px 5px -2px rgba(0,0,0,0.28)`):
  hover elevation and small popovers.
- **elev-3** (`0 14px 32px -8px rgba(0,0,0,0.52), 0 6px 14px -8px rgba(0,0,0,0.34)`):
  menu panels.
- **elev-4** (`0 32px 68px -16px rgba(0,0,0,0.66), 0 14px 28px -14px rgba(0,0,0,0.45)`):
  cards, modals and the command palette.

A separate `--ring` (2px background gap + 4px accent) is the focus treatment,
and `--edge-accent` is a 1px accent border used for hover/selection. Neither is
a glow that spreads light outside the element.

### Named Rules
**The Flat-By-Default Rule.** Surfaces are flat at rest. A shadow appears only
to lift a real layer (card, menu, modal) or as a response to state (hover,
focus).

**The Hairline Rule.** Dividers are 1px translucent lines (`--hairline`,
`--hairline-soft`), not heavy borders and not extra shadows.

## Shapes

Rounded but not soft. Controls use small radii, surfaces use larger ones, and
pills are reserved for chips and status. The scale is `6px` (xs), `9px` (sm,
the default control radius), `12px` (md, panels and terminal), `16px` (lg,
modals), `22px` (xl, cards) and `999px` (pill). Every surface is outlined with
a 1px hairline.

The signature silhouette is the **CRT bezel**: a rounded terminal screen with a
status strip (OIA) attached beneath it, sharing a 12px bottom radius. Themes
that reproduce a real terminal (`theme-authentic`, `theme-custom`) square the
screen and card corners to zero on purpose.

### Named Rules
**The Square-Again Rule.** Where a theme asks for authenticity, radius goes to
0 for the terminal and card. Do not "round off" the authentic theme.

## Components

### Buttons
- **Shape:** gently rounded (9px radius), 1px hairline, raised panel fill.
- **Default:** `background: var(--surface-raised)`, `border: 1px solid
  var(--hairline)`, `padding: 8px 10px`, text in `var(--fg)`.
- **Hover / Focus:** lift 1px, accent hairline plus a 1px accent edge; focus
  uses the shared `--ring`; active compresses to 0.975 scale.
- **Primary:** the last action in a dialog / the copilot send button is filled:
  accent background, `--accent-contrast` ink, semibold.
- **Danger:** danger-tinted border and text; on hover it fills with the danger
  colour and switches ink to the background.
- **Icon buttons:** 34px square (32px in the app bar), transparent or raised,
  with a soft accent halo on hover.

### Inputs / Fields
- **Style:** sunken surface (`--surface-sunken`), 1px hairline, 9px radius,
  `8px 10px` padding.
- **Focus:** accent border plus a 3px `--accent-soft` ring.
- **Checkboxes** are custom-drawn at 18px with a 5px radius and an accent fill
  when checked; radios keep native geometry with `accent-color`.
- **Caption labels** must not carry field chrome; only labels that wrap a
  control look like controls.

### Cards / Containers
- **Corner Style:** 22px radius (16px in the base stylesheet).
- **Background:** `var(--panel)`.
- **Shadow:** `elev-4` plus a 1px soft outline.
- **Border:** 1px hairline.
- **Internal Padding:** 28px desktop, 14px on phones.

### Navigation
- **App bar:** one rounded bar (`elev-1`) with brand, named drop-down menus,
  a monospace host chip, and a small primary-action cluster. Triggers are
  0.8rem, 550 weight, muted at rest, full text colour with an accent-tinted
  background when open.
- **Menu panels:** fixed-position, `elev-3`, 268px minimum width, 12px radius,
  6px padding; items are 0.82rem rows with 17px icons and optional key hints.
- **Session tabs:** attached beneath the bar, squared top corners, the active
  tab marked by an inset 2px accent underline and a 10% accent wash.
- **Responsive:** the bar scrolls its menus sideways and collapses to icon
  triggers on narrow screens; labels survive on touch devices.

### Modals
One unified dialog system: `--scrim` backdrop, 16px radius, `elev-4`, hairline
border, 14px header/footer rules. The primary action is filled; close is muted
and does not turn red. Dialogs fade and rise in over `--dur-3`; the whole frame
scrolls rather than nesting a second scrollbar.

### Chips
Pill-shaped 0.6875rem status chips (recording, playback, chaos) with a tinted
border and text. Recording chips pulse; the live-status dot pulses in `--ok`.

### Terminal (signature component)
The 3270 grid is the product. It is generated server-side, uses the IBM 3270
face at the user's `--terminal-cell-size`, and disables ligatures, kerning and
optical sizing. Field underlines and carets use the accent; protected text is
muted; host attribute colours map to the `--term-color-*` tokens. An OIA status
strip sits beneath the screen in the foreground colour with the background as
ink. Chrome lives on `.terminal-shell`; the grid itself is never padded or
reflowed.

### Command Palette
A centred 620px sheet on `--scrim`, `elev-4`, with a borderless search input,
grouped result rows and a 26px icon tile per result. Selection uses the accent
soft fill and accent hairline.

## Do's and Don'ts

### Do:
- **Do** consume semantic custom properties (`--accent`, `--panel`,
  `--hairline`, `--elev-*`, `--r-*`) in new CSS, and declare anything new on
  `body` so it resolves against the active theme.
- **Do** keep the terminal on its fixed character grid and put chrome on the
  parent shell.
- **Do** use `--fg-muted` for secondary text and reserve `--accent` for
  interaction and live state.
- **Do** keep focus visible with the shared `--ring` and respect
  `prefers-reduced-motion` (motion collapses to ~0ms there).
- **Do** keep destructive actions danger-coloured and non-destructive closes
  neutral.

### Don't:
- **Don't** hardcode hex values, shadows or radii in a component; that breaks
  every theme including operator-authored ones.
- **Don't** use font weights above 600 for emphasis.
- **Don't** add horizontal padding or flex shrink to `.screen-container`, or
  reflow the terminal for small screens.
- **Don't** reintroduce floating toolbars over the terminal or a second control
  row beside the app bar.
- **Don't** fill large areas with the accent; its rarity is what makes state
  legible.
- **Don't** round the corners of the authentic/custom terminal or card themes.
