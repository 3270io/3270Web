---
seo_title: "3270Web demonstration videos — watch it work before you install"
description: >-
  Short screen recordings with step-by-step captions: a tour of the
  terminal, connecting to a host, and recording a workflow and playing
  it back against a live session.
---

# Demonstration Videos

Short screen recordings of 3270Web doing real work against the bundled
[Pet Store sample application](sample-apps.md), with captions baked in.
Every video is recorded from a live session — nothing is mocked.

## 3270Web in Under a Minute

A tour of the essentials: connect, sign on, work the screens, the command
palette, and where the automation lives.

![type:video](videos/showcase-tour.webm)

## How To: Connect to a Host

The first session, end to end — enter a host address, connect, read the
operator information area, sign on.

![type:video](videos/howto-connect.webm)

## How To: Record and Replay a Workflow

Capture a flow once (sign on, jump to the stock catalogue, sign off), save
it as a JSON recording, load it back and watch 3270Web replay it against
the live host. The same recordings drive [guided business
tasks](business-tasks.md) and the [REST API](rest-api.md).

![type:video](videos/howto-workflow.webm)

## How To: Explore an Application with Chaos

Start a chaos run, watch it drive the screens on its own, open the live
map of what it has discovered, and stop it — see [Chaos
Mode](chaos-mode.md) for everything the run can do afterwards.

![type:video](videos/howto-chaos.webm)

## How To: Themes and Terminal Fonts

Settings → Theme: from the default green phosphor to amber and back, with
the bundled 3270 glyph fonts — details in [Terminal
Fonts](terminal-fonts.md).

![type:video](videos/howto-themes.webm)

## How To: Keyboard, PF Keys and the Virtual Keypad

Function keys mapping to PF keys, the on-screen keypad for touch screens,
and the command palette — the full reference is [Keyboard and
Controls](keyboard-and-controls.md).

![type:video](videos/howto-keyboard.webm)

## How To: Run Several Hosts at Once

Each session is a tab: open a second host from the + tab, switch with a
click, and every session keeps its place — more in [The Session
Manager](session-manager.md).

![type:video](videos/howto-sessions.webm)

## How To: Turn a Flow into a Business Task

The full life of a [guided business task](business-tasks.md): record a
flow, let the wizard name its inputs, check the guard on each step, mark
the answer on the final screen, review what the server reads back, then run
it from a form — no terminal knowledge needed.

![type:video](videos/howto-business-tasks.webm)

## The Bundled Sample Apps

What ships in the box: the connect-page picker, the Pet Store retail
system, and Snake on a second tab — the full list is in [Bundled Sample
Applications](sample-apps.md).

![type:video](videos/howto-sample-apps.webm)

---

The videos are generated with `scripts/record-demo-videos.mjs`, which
drives a local 3270Web with Playwright and injects the captions as page
overlays while recording. To refresh them after a UI change:

```bash
go run ./cmd/3270Web            # in one terminal
node scripts/record-demo-videos.mjs   # in another
```
