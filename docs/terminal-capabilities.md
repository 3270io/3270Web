# Terminal Capabilities

What an enterprise 3270 terminal is expected to do, and what 3270Web
provides. Every capability below is implemented and shipping — the tables
are a checklist you can take into an evaluation, not a roadmap.

For what may come next, see the [Feature Roadmap](feature-roadmap.md).

---

## Terminal fidelity

Behaviour an experienced 3270 operator expects from the terminal itself.
This is not feature count — it is whether the thing behaves like a
terminal.

| Capability | 3270Web |
|---|:---:|
| TN3270 and TN3270E, with TLS | ✅ |
| Full AID and control key coverage — PF1–24, PA1–3, Attn, SysReq, Clear, Reset, EraseEOF, EraseInput, Dup, FieldMark, NewLine, Home | ✅ |
| Extended colour, blink, reverse video, underscore, intensify | ✅ |
| Operator Information Area — online/application block, `X SYSTEM`, `X -f` operator error, insert indicator | ✅ |
| Local cursor movement — Tab, Back-Tab, arrows and Home resolve in the client, with no host round-trip | ✅ |
| Numeric field enforcement, with a real operator-error lock | ✅ |
| Insert / overtype toggle | ✅ |
| Type-ahead while the host holds the keyboard | ✅ |
| Selectable terminal models and screen sizes | ✅ |
| Code page selection per connection | ✅ |

On a 3270 the cursor belongs to the terminal, and the host learns its
position exactly once — in the inbound data stream, when an AID key is
pressed. 3270Web models it the same way, which is what removes a network
round-trip from every Tab and arrow keypress. See
[Keyboard and Controls](keyboard-and-controls.md).

## Screen tools

| Capability | 3270Web |
|---|:---:|
| Screen-accurate copy, including unsubmitted input and excluding hidden fields | ✅ |
| Rectangular block copy — Alt+drag to mark, Ctrl+C to take | ✅ |
| Find on screen, over the character grid so it matches input values too | ✅ |
| Screen history / scrollback — the last 50 screens per session | ✅ |
| Hotspots — clickable PF/PA legends and URLs printed by the application | ✅ |
| Print screen | ✅ |
| Focus mode — the terminal fills the display, with an auto-hiding toolbar rail | ✅ |
| Business and Engineering workspace modes | ✅ |
| Command palette over every control | ✅ |

Find and copy both run on the character grid rather than the DOM, so they
see what the screen actually shows — including values someone has typed but
not yet submitted, which the browser's own find and selection cannot reach.

## Sessions and connections

| Capability | 3270Web |
|---|:---:|
| Multiple concurrent sessions in one browser, with a tab bar | ✅ |
| Named connection profiles, stored server-side | ✅ |
| Per-connection TLS, certificate verification, LU name, model and code page | ✅ |
| Auto-reconnect on host drop, with a manual Reconnect | ✅ |
| Session timeout handling with a clear prompt | ✅ |

Connection profiles are server-side rather than browser-local, because
connection settings are what an administrator sets up once for everyone.
Most of a profile becomes s3270's own target syntax, which the UI shows
verbatim, so what you check is what gets dialled.

## Keyboard

| Capability | 3270Web |
|---|:---:|
| Customisable keyboard mapping | ✅ |
| Rebind by pressing the key, not by choosing from a list of key names | ✅ |
| Custom bindings layer over the built-ins rather than replacing them | ✅ |
| Export and import a layout as JSON | ✅ |
| Import an existing `.KMP` keyboard file | ✅ |
| Virtual keypad, in compact, full and maximum layouts | ✅ |

## File transfer

| Capability | 3270Web |
|---|:---:|
| IND$FILE send and receive | ✅ |
| Text and binary modes, with line-ending handling | ✅ |
| TSO, VM and CICS host types | ✅ |
| Dataset creation options — record format, record length, block size, space allocation | ✅ |
| PDS member names (`USER.DATA(MEMBER)`) | ✅ |

## Accessibility

| Capability | 3270Web |
|---|:---:|
| Screen-reader field labelling derived from the screen's own label text | ✅ |
| Keyboard-only operation | ✅ |
| Keyboard-trap escape hatch (WCAG 2.1.2) | ✅ |
| High-contrast and themeable rendering, seven built-in themes | ✅ |
| Reduced-motion support | ✅ |

Field labelling is worth calling out. 3270Web derives each input's
`aria-label` from the protected text to its left, so a screen reader
announces "Customer number, edit text" rather than "edit text" — the
difference between a usable screen and an unusable one.

---

## Beyond the category baseline

These are not table stakes anywhere. They are the reason to choose 3270Web
rather than merely tolerate it.

| Capability | 3270Web |
|---|:---:|
| Runs in a browser tab — no emulator install, no thick client | ✅ |
| Public REST/JSON API for RPA and CI integration | ✅ |
| Workflow recording and playback, with pause, step and debug | ✅ |
| Guided Business Tasks — run a recorded flow from a form and get an answer, no green screen | ✅ |
| Task authoring from a recording — record once, mark the answer, save to a shared catalogue | ✅ |
| Task authoring from a chaos run — convert a discovered path into a runnable task | ✅ |
| Chaos exploration — automated discovery of an application's screens and transitions | ✅ |
| Screen mind-map, exportable and diffable between hosts | ✅ |
| Host compatibility profiler | ✅ |
| AI Chat driving the host through a documented tool surface | ✅ |
| Docker image and a one-line installer | ✅ |

Guided Business Tasks are the one that changes who can use the product: a
task has named inputs and a named answer, so the person running it needs to
know the business question rather than the application. See
[Guided Business Tasks](business-tasks.md).

The REST API and the workflow JSON are paired deliberately: a flow recorded
by hand in the browser is the same document an automated job replays, so
there is no separate scripting language to learn and no gap between what a
person did and what a bot repeats. See [REST API](rest-api.md),
[Recordings and Playback](workflow.md) and [Chaos Mode](chaos-mode.md).
