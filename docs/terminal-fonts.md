---
seo_title: "IBM 3270 terminal fonts bundled with 3270Web"
description: >-
  Three IBM 3270-style web fonts ship inside the binary, so a browser
  session looks like a real 3270 terminal whatever fonts the machine has
  installed.
---

# Terminal Fonts

3270Web ships three IBM 3270-style web fonts so the browser session
matches the look of a real 3270 terminal regardless of the user's
system fonts. The fonts are bundled in the binary; no install or
network fetch is required.

## Available fonts

| Font | When to use |
|---|---|
| **3270 Regular** | The classic look — closest to a real 3279 display. The default. |
| **3270 Semi-Condensed** | Slightly tighter glyph width. Useful when running a 132-column model on a narrow viewport. |
| **3270 Condensed** | Tightest glyph width. Pairs well with oversize / wide models on small screens. |

The font files live under `web/static/fonts/` and are served from the
same origin as the app (no CDN, no external request). See
`web/static/fonts/LICENSE.txt` for attribution.

## Switching the terminal font

1. Open **Settings** from the account and settings menu.
2. Switch to the **App** (or theme) tab.
3. Pick the desired font from the **Terminal Font** dropdown.
4. Click **Save settings** — the session re-renders immediately.

The choice is persisted with the rest of the theme settings.

## Programmatic note

The font dropdown writes into the same theme settings block stored
alongside palette and contrast preferences. Any deployment that pushes
a pre-baked `3270Web-config.xml` can set the default font there;
otherwise it falls back to **3270 Regular** on first run.
