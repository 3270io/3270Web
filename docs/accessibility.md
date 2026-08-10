---
seo_title: "3270Web accessibility and WCAG 2.1 Level AA conformance"
description: >-
  Where 3270Web stands against WCAG 2.1 Level AA: what was tested, what
  conforms, and the exceptions — written to be useful in a procurement
  review.
---

# Accessibility conformance

3270Web aims to meet **WCAG 2.1 Level AA**. This page is the statement of
where it stands, what was tested, and what is known not to conform.

It is written to be useful in a procurement review: the claims are scoped,
the evidence is named, and the exceptions are stated rather than left for
somebody to find.

## Conformance claim

**3270Web partially conforms to WCAG 2.1 Level AA.** "Partially conforms"
means some parts of the content do not fully conform; the parts are named
under [Known issues](#known-issues) below.

| | |
| --- | --- |
| Standard | Web Content Accessibility Guidelines 2.1 |
| Conformance level | AA (partial — see Known issues) |
| Scope | The 3270Web browser interface: the connect page, the terminal, every dialog reachable from it, and the account pages under `AUTH_MODE=local` |
| Out of scope | The content of the host application. See [What 3270Web cannot fix](#what-3270web-cannot-fix) |
| Last reviewed | This release |

## How it was tested

Two passes, because neither alone is worth much.

**Automated.** axe-core 4.13 against the rulesets `wcag2a`, `wcag2aa`,
`wcag21a` and `wcag21aa`, driven through a real Chromium browser, over
**eighteen surfaces**:

- the connect page and the connected terminal
- eleven dialogs — settings, about, connection profiles, find on screen,
  screen history, keyboard mapping, tasks, file transfer, connection details,
  logs and disconnect
- the five account pages that appear under `AUTH_MODE=local` — first-run
  setup, sign-in, password change, account administration and the audit log

Dialogs were opened before being scanned, because a hidden dialog is a dialog
nothing checks.

**Manual.** The criteria automation cannot decide:

- keyboard traversal of every control, watching for a visible focus indicator
  at each stop
- the terminal's keyboard-trap escape hatch
- reflow at 320 CSS pixels
- text resized to 200%
- whether status messages are announced
- whether focus can land on something that cannot be seen

## What conforms

Automated testing reports **zero WCAG 2.1 A or AA violations across all
eighteen surfaces**. The manual pass adds:

**Keyboard operation (2.1.1, 2.4.3, 2.4.7).** Every control is reachable and
operable from the keyboard. All 45 tab stops sampled on the terminal page
carry a visible focus indicator — an outline or a shadow — with none relying
on colour alone.

**No keyboard trap (2.1.2).** The terminal deliberately takes `Tab` and
`Shift+Tab`, because those are 3270 field navigation and an operator expects
them to move between fields rather than out of the screen. That is exactly the
situation 2.1.2 exists for, so there is a documented way out: **`Ctrl+Tab`**
moves focus to a labelled escape-hatch control immediately after the terminal.
`Ctrl+Shift+Tab` does the same in reverse. See
[Keyboard and Controls](keyboard-and-controls.md).

**Field labelling (1.3.1, 4.1.2).** Each input's accessible name is derived
from the protected text to its left on the screen itself, so a screen reader
announces "Customer number, edit text" rather than "edit text". This is the
part worth understanding: a 3270 screen has no markup and no labels — the
association between a caption and the field beside it exists only in the
character grid, and 3270Web reconstructs it.

**Status messages (4.1.3).** The operator information area, the run-status
panel and the notification toasts are live regions, so a change in keyboard
state, a completed step or a failure is announced without moving focus.

**Bypass blocks (2.4.1).** Every page carries a `main` landmark. On the
terminal, focus starts inside it on page load — the menu bar and session tabs are
already behind you. There is deliberately no skip link; see
[Design decisions](#design-decisions).

**Resize text (1.4.4).** At 200% text, no control is pushed off screen and the
page does not scroll horizontally.

**Contrast (1.4.3).** No text falls below 4.5:1 in the default theme. Seven
built-in themes ship, and a custom theme editor allows any palette — a theme
of your own making is your own to check.

**Reduced motion (2.3.3).** Animation is suppressed under
`prefers-reduced-motion`.

### Keeping this current

A conformance statement is a measurement, and the thing measured keeps
growing: the account pages above did not exist when the first audit ran, and
each arrived without a `lang` attribute or a landmark. So the checks that can
be automated are, and they enumerate the templates on disk rather than a list
of names — a list only covers the pages somebody remembered to add to it. A
new page that ships without them fails the build.

### Dialog behaviour

A static scan reads the markup of a dialog; it cannot press Tab. The
properties that matter for a dialog are all behavioural, so there is a browser
check for them:

```bash
ALLOW_SAMPLE_APPS=true go run ./cmd/3270Web    # another terminal
node scripts/check-modals.mjs
```

It opens every dialog on the terminal screen in turn and asserts that opening
one moves focus into it (2.4.3), that Tab stays inside it (2.4.3), that the
page behind the backdrop does not scroll, that Escape closes the topmost
dialog and only that one, and that closing everything releases the scroll lock
again.

These are properties of one shared helper — `web/static/modal-utils.js` — and
that is the point. Each of them had been got wrong at least once by a dialog
that arranged its own: a focus trap listening on a dialog that focus was never
moved into, so Tab walked into the page behind it; a scroll lock on `<body>`
on a page where `<html>` is the element that scrolls; and several private
Escape listeners that all fired on one keypress, closing a dialog and the one
it had been opened from together. A dialog joins the shared stack by calling
`pushModal` when it opens and `popModal` when it closes, and gets all five.

## Known issues

Stated because a conformance claim that lists nothing is not a conformance
claim.

**A residual horizontal scroll of about 9 pixels at 320 CSS pixels wide
(1.4.10).** At the narrowest supported width the document reports a scrollable
area a few pixels wider than the viewport. No visible content sits in that
region — every element renders inside the viewport — so nothing is cut off or
unreachable, but a horizontal scrollbar can appear. Being tracked; it is a
layout artifact rather than content that requires scrolling to read.

**Custom themes are unverified.** The default and built-in themes were
checked. The theme editor lets an operator choose any foreground and
background, including combinations well under 4.5:1. Nothing validates a
custom palette's contrast.

**The AI chat panel's responses are third-party content.** Text returned by a
model provider is rendered as it comes. Its structure — headings, lists,
tables — is whatever the model produced.

## Design decisions

Two choices that look like failures and are not.

**The terminal grid scrolls horizontally on a narrow screen, and does not
reflow.** WCAG 1.4.10 exempts "parts of the content which require
two-dimensional layout for usage or meaning". A 3270 screen is the textbook
case: which column a character occupies *is* information — column 40 of row 6
sits under column 40 of row 5, applications draw boxes and columns out of
that, and a line that wrapped would be a line that lied. So below tablet width
the grid scrolls inside its own region rather than reflowing, and the page
around it does not scroll. Use the zoom control in the terminal tools for a
size that suits the device: it scales the grid, which keeps it a grid.

**There is no skip link.** A link at the top of the document could not be
reached in practice. `Tab` and `Shift+Tab` inside the terminal are 3270 field
navigation, and leaving through the `Ctrl+Tab` escape hatch puts focus *after*
the terminal, not before it — so the link would sit somewhere a keyboard user
never arrives. An unreachable skip link is a worse answer than none, so 2.4.1
is met the other two ways WCAG accepts: a `main` landmark, and focus starting
on the main content.

## What 3270Web cannot fix

3270Web is a terminal. What it displays is drawn by the host application, and
that content's accessibility is the host's, not the terminal's:

- **Colour used alone to convey meaning** — if an application marks errors
  only by drawing a field red, the terminal renders red.
- **Field captions that are not adjacent** — label derivation reads the
  protected text next to a field. An application that puts its caption
  elsewhere gives nothing to derive from.
- **Timed screens** — a host that clears a screen on its own timer is doing
  so beneath the terminal.

Where a host is the barrier, [Guided Business Tasks](business-tasks.md) are
often the better answer than making the green screen accessible: a task
presents a form with named inputs and a named answer, and never shows the
screen at all.

## Reporting a problem

Accessibility problems are bugs. Please report them at
[github.com/3270io/3270Web/issues](https://github.com/3270io/3270Web/issues),
with the surface (connect page, terminal, which dialog), what you were using
to navigate, and what happened.

## Related

- [Keyboard and Controls](keyboard-and-controls.md) — every key, the escape
  hatch, and remapping
- [Terminal Capabilities](terminal-capabilities.md) — the capability summary
