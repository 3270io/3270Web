---
seo_title: "3270Web REST API v1: drive a session over JSON HTTP"
description: >-
  The versioned JSON HTTP API for non-browser clients — RPA bots, CI jobs
  and integration scripts — gated by a Bearer token and enabled per
  deployment.
---

# REST API (v1)

3270Web exposes a small JSON HTTP API for non-browser clients (RPA bots,
CI jobs, integration scripts). The API is versioned and gated by a
Bearer token so it can be enabled per-deployment.

## Enabling the API

Set the `API_TOKEN` environment variable to a non-empty secret before
starting 3270Web. The simplest way is to add a line to the `.env` file
that 3270Web reads on startup:

```
API_TOKEN=replace-with-a-long-random-string
```

When `API_TOKEN` is unset or empty, every `/api/v1/*` request returns
`503 Service Unavailable` with `{"error": "API disabled: API_TOKEN not
configured"}`. This is the default so the API can't be accidentally
exposed.

The 3270Web server binds to `127.0.0.1:3270` by default, so the API is
only reachable from the local host. The Bearer token is additional
defense-in-depth for any deployment that changes the bind address via
`WEBUI_BIND` — including the Docker image, which sets `WEBUI_BIND=0.0.0.0`
so that published ports work at all. In a container, what the API is
reachable from is decided by the port mapping, not the bind address.

## Authentication

Every request must include an `Authorization: Bearer <token>` header.
Bad or missing tokens get `401 Unauthorized`.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/sessions
```

### On an instance with accounts

`API_TOKEN` is one shared credential, which is all there is to say about who
is calling when there is one operator. Where [accounts](authentication.md) are
enabled (`AUTH_MODE=local`) it is refused at startup, and clients present a
token issued to an account instead:

```bash
3270Web token add alice "ci pipeline"
```

Such a token reaches exactly what its owner reaches. `GET /api/v1/sessions`
lists that account's sessions, and naming somebody else's session in a path
answers `404` — the same answer as an ID that does not exist. A token issued
`--read-only` may only make `GET`, `HEAD` and `OPTIONS` requests; anything
else answers `403`.

See [API tokens](multi-user.md#api-tokens) for issuing, scoping and
revoking them.

### Calling from a browser

A page on another origin cannot reach this surface unless its origin is named
in `EMBED_ORIGINS`; see [Embedding 3270Web](embedding.md). Credentials are
never allowed on those cross-origin calls — the token in the `Authorization`
header is the whole of the authentication, and a browser's own 3270Web session
cookie is never involved.

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/sessions` | List active sessions |
| `POST` | `/api/v1/sessions` | Start a new host session |
| `DELETE` | `/api/v1/sessions/:id` | Disconnect and remove a session |
| `GET` | `/api/v1/sessions/:id/screen` | Refresh and read the current screen |
| `POST` | `/api/v1/sessions/:id/key` | Send an AID or navigation key |
| `POST` | `/api/v1/sessions/:id/field` | Write text into a field |
| `POST` | `/api/v1/sessions/:id/submit` | Submit modified fields + send Enter (or another AID) |
| `POST` | `/api/v1/sessions/:id/profile` | Run a host compatibility probe and return the `CompatibilityProfile` JSON |
| `GET` | `/api/v1/sessions/:id/profile` | Return the cached `CompatibilityProfile` from the last probe |
| `GET` | `/api/v1/sessions/:id/query` | Ask the terminal about the connection itself |
| `POST` | `/api/v1/sessions/:id/snapshots` | Freeze the screen and keep it under a name |
| `GET` | `/api/v1/sessions/:id/snapshots` | List the snapshots this session holds, or read one with `?name=` |
| `DELETE` | `/api/v1/sessions/:id/snapshots?name=` | Drop one snapshot |
| `POST` | `/api/v1/sessions/:id/snapshots/diff` | Compare two snapshots, or one against the live screen |
| `GET` | `/api/v1/sessions/:id/toggles` | Read the terminal's display toggles |
| `POST` | `/api/v1/sessions/:id/toggles` | Change one display toggle |
| `POST` | `/api/v1/sessions/:id/screen-trace` | Start recording every screen the terminal draws |
| `GET` | `/api/v1/sessions/:id/screen-trace` | Report the trace, or download it with `?download=1` |
| `DELETE` | `/api/v1/sessions/:id/screen-trace` | Stop recording |
| `POST` | `/api/v1/sessions/:id/printer` | Bind a 3287 printer LU beside this session |
| `GET` | `/api/v1/sessions/:id/printer` | Report the printer and the jobs it has collected |
| `DELETE` | `/api/v1/sessions/:id/printer` | Stop the printer, keeping its jobs |
| `GET` | `/api/v1/sessions/:id/printer/jobs` | List the print jobs, or download one with `?name=` |
| `DELETE` | `/api/v1/sessions/:id/printer/jobs?name=` | Drop one print job |
| `POST` | `/api/v1/sessions/:id/hllapi` | Drive the terminal with HLLAPI-shaped calls: function numbers, one-based positions, return codes |
| `GET` | `/api/v1/tasks` | List the Guided Business Task catalogue |
| `POST` | `/api/v1/tasks` | Add or replace a task |
| `POST` | `/api/v1/sessions/:id/tasks/run` | Run a task in a session and return the result |
| `GET` | `/api/v1/library` | Download the tasks and host presets as one portable document |
| `POST` | `/api/v1/library` | Import one, or ask what importing it would do |

### `POST /api/v1/sessions`

Create and start a host session. Sample-app pseudo-hostnames (`mock`,
`demo`, `sampleapp:appN`) are rejected by the API — those are reserved
for the browser UI.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"host":"mainframe.example.com:23"}' \
  http://127.0.0.1:3270/api/v1/sessions
```

Response:

```json
{ "id": "f1c5...", "host": "mainframe.example.com", "port": 23 }
```

### `GET /api/v1/sessions/:id/screen`

Refreshes the screen and returns its full structure.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/sessions/$ID/screen
```

Response:

```json
{
  "width": 80,
  "height": 24,
  "text": "...screen contents...\n...\n",
  "formatted": true,
  "kbd_lock": "U",
  "cursor": { "row": 5, "col": 12 },
  "fields": [
    {
      "start_row": 1, "start_col": 0,
      "end_row": 1, "end_col": 19,
      "value": "USERID",
      "protected": true, "numeric": false, "hidden": false,
      "length": 20
    }
  ],
  "status": "U F P C(mainframe.example.com) I 4 24 80 5 12 0x0 0.000"
}
```

`kbd_lock` is `"U"` (unlocked), `"L"` (locked), or `"E"` (error). The
field is omitted when 3270Web could not parse the status line.

### `POST /api/v1/sessions/:id/key`

Send a single key. The key name follows the same vocabulary the Copilot
side panel uses: `Enter`, `PF1`..`PF24`, `PA1`..`PA3`, `Tab`, `BackTab`,
`Clear`, `Reset`, `EraseEOF`, `EraseInput`, `Home`, `Up`, `Down`,
`Left`, `Right`.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"key":"PF3"}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/key
```

### `POST /api/v1/sessions/:id/field`

Write text into the input field that contains `(row, col)`. Coordinates
are 0-indexed. Text containing CR, LF, or TAB is rejected.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"row":3,"col":10,"text":"USER01"}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/field
```

### `POST /api/v1/sessions/:id/submit`

Submit any modified fields and send an AID key. The default AID is
`Enter`; pass `{"aid": "PF3"}` to use a different key. The response
includes the updated screen.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"aid":"Enter"}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/submit
```

### `DELETE /api/v1/sessions/:id`

Disconnect from the host and remove the session.

```sh
curl -X DELETE \
  -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/sessions/$ID
```

### `POST /api/v1/sessions/:id/profile`

Probe the session's connected host and return a `CompatibilityProfile`
JSON document. The schema is shared byte-for-byte with `3270Connect
-profile` output, so profiles from either tool can be diffed against
each other. Body is optional.

```sh
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"collect_raw": true}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/profile
```

Supported body fields: `ind_file_probe`, `collect_raw`,
`per_action_timeout_ms`. See the
[Host Compatibility Profiler](host-profiler.md) page for the full
walkthrough and the
[Compatibility Profile Schema](compatibility-profile-schema.md) for the
response shape.

### `GET /api/v1/sessions/:id/profile`

Return the cached `CompatibilityProfile` from the last probe in this
session. `404 Not Found` if no probe has run.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/sessions/$ID/profile
```

### `GET /api/v1/sessions/:id/query`

The connection's own account of itself: negotiated telnet options, TLS state,
terminal name, cursor, byte counts, the s3270 build actually running, and
everything else the terminal knows about its link to the host.

None of it is on the screen, which is the point. A session that renders
perfectly may still have failed to negotiate TN3270E, or bound a different LU
from the one that was asked for, and this is the only place that shows. It is
the difference between "the application looks fine" and "the connection is
what we specified".

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/sessions/$ID/query
```

```json
{
  "session": "1900afaa255b1f4d29257119f4e8f021",
  "queries": {
    "ConnectionState": "connected-3270",
    "Host": "host mvs01.example.com 992",
    "TerminalName": "IBM-3278-4-E",
    "ScreenSizeCurrent": "rows 24 columns 80",
    "TelnetHostOptions": "BINARY END OF RECORD",
    "Tls": "not secure",
    "LuName": "",
    "Version": "s3270 v4.5ga5"
  },
  "available": ["About", "Actions", "BindPluName", "..."]
}
```

`available` is every field name, in the terminal's own order. A field whose
value is genuinely empty is still reported — "the LU name is blank" and "this
build has no LU name to report" are different facts.

Add `?name=` for one field. The match is case-insensitive and the reply echoes
the canonical spelling:

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  "http://127.0.0.1:3270/api/v1/sessions/$ID/query?name=terminalname"
```

```json
{ "session": "1900afaa…", "name": "TerminalName", "value": "IBM-3278-4-E" }
```

A name that is not in `available` gets `400 Bad Request` **and the list of
names that would have worked**. That refusal is deliberate rather than
pedantic: the underlying terminal action does not reliably reject a keyword it
does not know — it can block instead — and a blocked command costs the session,
because the only way out of it is to restart the terminal process. So names are
only ever taken from the terminal's own answer, never from the request.

`409 Conflict` if the session is not connected.

The browser reads the same thing through its session cookie at
`GET /host/query`, which is what the **Connection** panel shows — one
implementation, two doors. See
[Keyboard and Controls](keyboard-and-controls.md#connection-details).

### Screen snapshots

A snapshot is the screen frozen and kept under a name. Freezing matters
because every other read goes to the live buffer: between deciding to capture
a screen and finishing reading it, the host may have written over it, and what
comes back is half of one screen and half of another. One command freezes the
display, and everything read afterwards is of the same instant.

The point of freezing it is comparison. Capture the screen a flow is supposed
to land on, run the flow again tomorrow, and ask what changed — the answer is
a list of rows, which is the difference between a test that tells you it
failed and one that tells you why.

```sh
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" -d '{"name":"before"}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/snapshots
```

```json
{
  "name": "before",
  "taken_at": "2026-03-04T10:15:22.184Z",
  "snapshot": {
    "rows": 24,
    "cols": 80,
    "status": "U F P C(mvs01.example.com) I 4 24 80 5 20 0x0 0.012",
    "text": "                    ACCOUNT ENQUIRY\n..."
  }
}
```

Then run the transaction and ask what moved. Omitting `right` compares against
the screen as it stands now, which is the shape this is usually used in:

```sh
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" -d '{"left":"before"}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/snapshots/diff
```

```json
{
  "left": "before",
  "right": "(live screen)",
  "identical": false,
  "lines": [
    { "row": 6, "left": " LAST NAME . . . .", "right": " LAST NAME . . . .  SMITH" },
    { "row": 11, "left": "", "right": " FIRST NAME FIELD IS REQUIRED." }
  ]
}
```

Rows are numbered from 1, the way an operator counts lines on a screen.
Trailing blanks are ignored, so a one-character change reports one row rather
than every row whose padding differs. A screen that grew or shrank reports its
extra rows against an empty string rather than matching on the common prefix.

Names may contain letters, digits, spaces, dots, dashes and underscores.
Capturing over a name that already exists replaces it — re-capturing `before`
at the top of a loop is the ordinary way to use this. A session holds up to 32
snapshots, and they end when the session does: a snapshot is a working note
taken during a run, not a record.

`409 Conflict` if the session is not connected, or if the ceiling is reached.

### `GET` and `POST /api/v1/sessions/:id/toggles`

The terminal's own display settings — monocase, the crosshair, cursor blink,
the underscore under input fields. They are settings of the *terminal*, so
they are read from and written to it rather than stored here; what you read is
what the terminal will actually do.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/sessions/$ID/toggles
```

```json
{
  "session": "1900afaa…",
  "toggles": [
    { "name": "blankFill", "value": true, "description": "treat trailing blanks in a field as if they were nulls" },
    { "name": "monoCase", "value": false, "description": "display all letters in upper case" }
  ]
}
```

Only what this terminal build actually reports is listed. A toggle that is
absent is absent, not `false` — an older or newer build not having one is not
the same as it being off, and s3270 legitimately has fewer of them than a
windowed terminal does.

```sh
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" -d '{"name":"monoCase","value":true}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/toggles
```

The reply carries the toggle as the terminal reports it *afterwards*, not as
the request asked for it, so a build that silently declines a change is
visible in the answer.

`value` is required and must be `true` or `false`. A body without it would
otherwise be indistinguishable from asking for the toggle to be turned off.

The settable names are an allowlist, and a name outside it gets `400 Bad
Request` together with the names that would have worked. The underlying
terminal action also reaches trace files, printer sessions and the proxy
configuration, none of which belong on an HTTP endpoint — so only display
toggles can be named at all, and every name that reaches the terminal is one
the terminal itself just reported.

The browser reads and writes the same thing through its session cookie at
`GET` and `POST /host/toggles`.

### `/api/v1/sessions/:id/screen-trace`

Records every screen the terminal draws, as it draws it — including the ones
replaced before anybody asked to see them. That is what separates it from
polling: a host that paints an error and immediately paints over it leaves a
poller nothing to find, and that is usually the screen someone needs
afterwards.

**Requires `ALLOW_SCREEN_TRACE=1`** as well as the API token. A trace is a
file on the server holding everything that crossed the display, including
whatever was typed into a field the host did not mark hidden, so it is off
until somebody turns it on. Without the flag, the start call returns `403`.

```sh
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" -d '{"format":"text"}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/screen-trace
```

```json
{
  "session": "1900afaa…",
  "file": "1900afaa…-20260304T101522Z.txt",
  "format": "text",
  "started_at": "2026-03-04T10:15:22.184Z",
  "running": true
}
```

`format` is `text` (the default) or `html`, which preserves colour and
highlighting. Files are written to `screen-traces/` beside the executable,
next to `chaos-runs/`. The destination is chosen by the server — no part of it
comes from the request — and only a file this server started is ever served
back.

`GET` reports the trace; `?download=1` returns the file itself, as an
attachment, up to 16 MB. An HTML trace is a page built out of whatever the
host painted, so it is never served inline. `DELETE` stops the capture;
stopping one that was never started succeeds, so a cleanup path does not have
to know whether it is cleaning up.

One trace per session: the terminal has a single trace destination, so a
second concurrent start would silently redirect the first and is refused with
`409`.

Ending the session drops this server's record of where the trace went. The
file stays on disk for whoever asked for it.

### `/api/v1/sessions/:id/printer`

Binds a 3287 printer LU alongside the terminal session, and collects what the
host prints to it. Batch output does not come back on the display: the host
binds a separate printer LU and sends the job there, and something has to be
listening.

On the token surface as well as the browser's because the case for it is
unattended — a job runs overnight and something collects it in the morning.
[Printer sessions](printer-sessions.md) is the full page; this is the API
shape.

```sh
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" -d '{"lu":"PRT001","mpp":132}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/printer
```

Body fields, all optional: `lu` names a printer LU, or `associate: true`
requests the printer paired with this session's own display LU (the terminal
is asked what that is; the caller does not supply it). Then `code_page`,
`mpp` (40–256), `eoj_timeout`, `crlf`, `blank_lines`, `ff_skip`, `ff_thru`,
`ignore_eoj` and `skip_cc`. A body with neither `lu` nor `associate` binds by
association.

The host, the port and the TLS terms are **not** settable: a printer session
always follows the terminal session it belongs to. An endpoint that took a
hostname would be a way to make the server open connections wherever it can
reach, and the real case is always the printer LU on the mainframe the caller
is already connected to.

`GET` returns `available` (whether this installation has a `pr3287` binary at
all), `running`, the `printer` itself, and the `jobs` collected so far:

```json
{
  "available": true,
  "running": true,
  "printer": {
    "session": "1900afaa…",
    "host": "mainframe.example",
    "port": 992,
    "lu": "PRT001",
    "tls": true,
    "started_at": "2026-08-10T09:14:02Z",
    "running": true
  },
  "jobs": [
    {"name": "job-20260810T091455Z-3fa1c209.prt", "bytes": 18422,
     "received_at": "2026-08-10T09:14:55Z", "truncated": false}
  ]
}
```

`GET /printer/jobs` lists them; the same path with `?name=` returns one job as
an attachment. The name is checked against the shape this server generates, so
nothing else under the spool directory or outside it is reachable.
`DELETE /printer/jobs?name=` drops one.

Without a `pr3287` binary the start call returns `501` naming what to install.
A printer that will not bind — an unknown LU, an LU somebody else holds —
returns `502` carrying what `pr3287` said, because that sentence is usually
the whole diagnosis.

One printer per session; a second start is refused with `409`. Stopping the
printer keeps its jobs. Ending the *session* deletes them, along with the
spool directory: the jobs belong to the session that collected them.

### `POST /api/v1/sessions/:id/hllapi`

A compatibility surface for screen-scrapers written against HLLAPI.

There is a lot of working code that drives a terminal by calling numbered
functions with a presentation-space position and branching on a return code.
Porting it to the endpoints above is not hard, but it is a rewrite of every
call site — and the reason those programs still exist is that nobody has time
to rewrite them. This endpoint lets one be ported by changing *how* it calls
rather than *what* it does.

What it reproduces is the shape, because the shape is what the calling code is
built around:

- **Positions are one-based and linear.** Position 81 on a 24x80 screen is row
  2, column 1. Every HLLAPI program does this arithmetic already.
- **Every call answers with a return code.** The response is always HTTP 200
  with an `rc` in the body — turning "string not found" into an HTTP error
  would make it a different program.
- **`SendKey` takes text with mnemonics embedded**, so `"SMITH@E"` still means
  what it always meant.

```sh
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"function":6,"text":"First Name"}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/hllapi
```

```json
{ "rc": 0, "function": "SearchPresentationSpace", "number": 6, "position": 322 }
```

#### Functions

| # | Name | Reads | Answers |
|---|---|---|---|
| 1 | `ConnectPresentationSpace` | — | `rc` 0. A no-op: the session is named in the path |
| 2 | `DisconnectPresentationSpace` | — | `rc` 0. Also a no-op; `DELETE /sessions/:id` ends a session |
| 3 | `SendKey` | `text` | Types the literal runs and presses the mnemonics, in order |
| 4 | `Wait` | — | `rc` 0 when the keyboard is unlocked, 4 while the host is still working |
| 5 | `CopyPresentationSpace` | — | `data` — the whole screen, plus `rows` and `cols` |
| 6 | `SearchPresentationSpace` | `text`, `position` | `position` of the match, or `rc` 24 |
| 7 | `QueryCursorLocation` | — | `position`, and `row`/`col` one-based |
| 8 | `CopyPresentationSpaceToString` | `position`, `length` | `data` |
| 15 | `CopyStringToPresentationSpace` | `position`, `text` | Writes at that position |
| 31 | `FindFieldPosition` | `position` | `position` and `length` of the field there |
| 32 | `FindFieldLength` | `position` | `length` |
| 33 | `CopyStringToField` | `position`, `text` | Writes from the *start* of the field the position falls in |
| 34 | `CopyFieldToString` | `position` | `data` — the field's contents |
| 40 | `SetCursor` | `position` | Moves the cursor |

`function` accepts the number or the name — the number for a mechanical port,
the name for whoever reads it afterwards.

A function that is not implemented is refused with `rc` 25 and the list of
those that are. Answering 0 for something that did not happen is the one thing
a compatibility layer must never do, so nothing is guessed at.

A hidden field read through function 34 comes back redacted, with
`"hidden": true` — the same treatment it gets everywhere else this application
hands a screen out. This endpoint is not a way around that.

#### Return codes

| `rc` | Meaning |
|---|---|
| 0 | The call did what was asked |
| 1 | There is no presentation space — the session is not connected |
| 2 | A parameter was missing, malformed or untranslatable |
| 4 | The keyboard is locked; the host has not finished |
| 7 | The position is outside the presentation space |
| 9 | The terminal could not carry the call out |
| 24 | The string was not found |
| 25 | This function is not implemented here |

#### Mnemonics

| Mnemonic | Key | | Mnemonic | Key |
|---|---|---|---|---|
| `@E` | Enter | | `@1`–`@9` | `PF1`–`PF9` |
| `@C` | Clear | | `@a` `@b` `@c` | `PF10` `PF11` `PF12` |
| `@T` | Tab | | `@x` `@y` `@z` | `PA1` `PA2` `PA3` |
| `@B` | Back-tab | | `@@` | A literal `@` |
| `@R` | Reset | | | |
| `@F` | Erase EOF | | | |

This is a subset on purpose. The mnemonic tables diverge between vendors past
this common core, and a mnemonic mapped to the *wrong* key is worse than one
that is not mapped at all: the wrong key reaches the host and the program
carries on believing it did what it asked. Anything outside the table is
refused with `rc` 2, and a key it does not cover can be named instead —
`{"function":3,"text":"PF13"}` — since the rest of this application already
understands key names.

#### A worked port

Typing a name into two fields and pressing Enter:

```sh
post() { curl -s -X POST -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" -d "$1" \
  http://127.0.0.1:3270/api/v1/sessions/$ID/hllapi; }

post '{"function":6,"text":"First Name"}'                  # find the prompt
post '{"function":33,"position":341,"text":"GRACE"}'       # fill its field
post '{"function":33,"position":421,"text":"HOPPER"}'
post '{"function":3,"text":"@E"}'                          # press Enter
post '{"function":5}'                                      # read the result
```

### `GET /api/v1/tasks` and `POST /api/v1/tasks`

The [Guided Business Task](business-tasks.md) catalogue. `GET` returns
exactly the documents `POST` accepts, so these two are also export and
import — moving a catalogue between deployments, or keeping one in version
control, needs no separate format. Every task goes through the same
validation the browser and the runner use.

The catalogue is deployment-wide, which is why it is not under
`/sessions/:id`.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://127.0.0.1:3270/api/v1/tasks | jq '.tasks'
```

### `POST /api/v1/sessions/:id/tasks/run`

Run a task and return its result. The task name travels in the body rather
than the path: task names are prose, and one containing a slash would
silently become two path segments.

```sh
curl -H "Authorization: Bearer $API_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Account balance enquiry","parameters":{"account_number":"40218855"}}' \
  http://127.0.0.1:3270/api/v1/sessions/$ID/tasks/run
```

```json
{
  "task": "Account balance enquiry",
  "durationMs": 152,
  "completed": true,
  "steps": [{ "index": 1, "description": "Enter the account number", "status": "ok" }],
  "outputs": [{ "name": "cleared_balance", "label": "Cleared balance",
                "value": "1,240.55", "found": true }]
}
```

**Synchronous**, unlike the browser's `/tasks/run`: a bot wants the answer in
the response rather than a poll loop. Bounded at five minutes. One run per
session, shared with the browser path, so an API run and a browser run cannot
overlap on the same terminal.

A task that stops early returns **`200` with `completed: false`** plus the
failure detail — which step, what it expected, what it found, and the screen
it stopped on. The request succeeded; the body says what the host did. An HTTP
status cannot express "step 3 saw the wrong screen".

| Status | Meaning |
|---|---|
| `200` | The run finished. Check `completed`. |
| `400` | A parameter was rejected. Nothing was sent to the host. |
| `404` | No such session, or no such task. |
| `409` | The session is not connected, or a run is already in progress on it. |

### `GET /api/v1/library` and `POST /api/v1/library`

A **library** is this deployment's Guided Business Tasks and connection
profiles as one document, for carrying a set-up to another deployment. `GET`
returns exactly what `POST` accepts, so the file downloaded from one instance
is the body posted to the next.

```sh
curl -H "Authorization: Bearer $API_TOKEN" \
  http://test.internal:3270/api/v1/library > library.json

curl -H "Authorization: Bearer $API_TOKEN" -H 'Content-Type: application/json' \
  --data-binary @library.json \
  'http://prod.internal:3270/api/v1/library?dryRun=true'
```

The document:

```json
{
  "formatVersion": 1,
  "exportedAt": "2026-04-02T09:14:00Z",
  "instance": "test.internal:3270",
  "tasks": [ … ],
  "profiles": [ … ],
  "notes": ["Host presets were read from the published list every account connects through."]
}
```

**Query parameters**

| Parameter | Where | Meaning |
|---|---|---|
| `include` | `GET` | `tasks`, `profiles`, or both (the default) |
| `download` | `GET` | `1` to be offered as a file rather than shown |
| `onConflict` | `POST` | `skip` (default) leaves an existing name alone; `replace` overwrites it |
| `dryRun` | `POST` | `true` reports what would happen and writes nothing |

**Three rules worth knowing before pointing this at a production instance.**

*Nothing is written until everything validates.* A library holding one entry
this build refuses is refused whole, with that entry named, rather than half
stored. The refusal is a `400` carrying the same report a success carries.

*A library covers the set you administer.* Tasks are always your own
catalogue. Host presets are the published list for an administrator — and for
the single operator of an instance with no accounts — and your own presets for
anybody else. The report says which under `profileScope`.

*It is not a backup.* Audiences that name individual accounts are dropped on
export: those accounts exist only on the deployment the file came from, and a
file meant to be handed to another site should not carry a staff list. Groups
and roles survive, since those are names two deployments plausibly share. A
preset whose only audience was named accounts arrives **not offered**, so
losing the restriction cannot silently turn "these four people" into
"everyone". Tasks contributed by an installed extension are left out too —
install the extension at the far end instead. Every one of these is recorded
in the document's own `notes`.

The report:

```json
{
  "dryRun": true,
  "profileScope": "published",
  "added": 12, "replaced": 0, "skipped": 1, "rejected": 0,
  "entries": [
    { "kind": "profile", "name": "Production TSO", "action": "added" },
    { "kind": "task", "name": "Account balance enquiry", "action": "skipped",
      "reason": "a task with this name is already here" }
  ],
  "notes": ["Host presets go into the published list every account connects through."]
}
```

An administrator who would rather not use curl has the same thing on
**Session screen → Library**: download, choose a file, read what it would
change, import.

## Browser-session endpoints

These endpoints reuse the session cookie set by the connect flow rather
than `API_TOKEN`. They are useful for in-browser callers and for tools
that already drive 3270Web through the cookie.

| Method | Path | Description |
|---|---|---|
| `POST` | `/profile` | Probe the current session and return the `CompatibilityProfile` JSON. |
| `GET` | `/profile` | Return the cached profile for the current session. |
| `GET` | `/printer/status` | The session's printer session and the jobs it has collected. |
| `POST` | `/printer/start` | Bind a printer LU. Same body as the API route; see [Printer sessions](printer-sessions.md). |
| `POST` | `/printer/stop` | Stop the printer, keeping its jobs. |
| `GET` | `/printer/jobs` | List the print jobs. |
| `GET` | `/printer/jobs/download?name=` | Download one job as an attachment. |
| `POST` | `/printer/jobs/delete?name=` | Drop one job. |
| `POST` | `/chaos/report` | Markdown discovery report for the active chaos run (ASCII screen graph, per-screen stats, suggested experiments). |
| `POST` | `/chaos/mindmap/compare` | Diff two previously-exported chaos mind maps. JSON by default; pass `Accept: text/html` (or `?format=html`) for the HTML report. See [Chaos Mind-Map Compare](chaos-compare.md). |
| `GET` | `/chaos/screens` | Every screen discovered by chaos: fields, learned values, key destinations, business annotations, and a truncated preview. `?include_previews=false` omits previews. |
| `POST` | `/chaos/screens/annotate` | Record a screen's business purpose and field semantics. Body: `{"screen_hash", "business_purpose", "notes", "field_semantics": {"R5C20L8": {"name", "description", "example", "sensitive"}}}`. |
| `GET` | `/chaos/business/functions` | List cataloged business functions (name, description, steps, parameters). |
| `POST` | `/chaos/business/functions` | Upsert a business function. Body: `{"name", "description", "entry_screen_hash", "steps": [{"screen_hash", "inputs": [{"field_key", "value", "parameter"}], "aid_key", "expect_hash"}], "parameters": [{"name", "description", "screen_hash", "field_key", "example", "required"}]}`. |
| `POST` | `/chaos/business/generate-workflow` | Generate a business-focused workflow JSON from a cataloged function. Body: `{"name", "parameters": {"param": "value"}, "host", "port"}`. Returns a playback-compatible workflow document with `Name`/`Description`/`BusinessFunction`/`Parameters` metadata. |

## AI provider

These endpoints configure which AI service [AI Chat Mode](ai-chat.md) talks
to. Like the browser-session endpoints above, they use a cookie rather than
`API_TOKEN` — settings are scoped to one browser identity (`3270Web_copilot_id`),
so a shared instance keeps each person's credentials to themselves. See
[AI Providers](ai-providers.md) for the concepts.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/ai/providers` | The provider catalogue (id, label, auth style, default endpoint, fallback model list) plus the caller's saved settings. |
| `GET` | `/api/ai/status` | Whether the selected provider can answer a chat: `{"provider", "providerLabel", "auth", "ready", "needs", "model", "baseUrl"}`. `needs` is `login` (Copilot sign-in required), `config` (key or endpoint missing), or empty. |
| `GET` | `/api/ai/config` | Saved settings, per provider: `{"provider", "providers": {"<id>": {"baseUrl", "model", "hasKey"}}}`. **API keys are never returned** — only `hasKey`. |
| `POST` | `/api/ai/config` | Save settings. Body: `{"provider", "target", "baseUrl", "apiKey", "model"}`. Omitted fields keep their stored value, so a model change does not clear the key; an explicit `"apiKey": ""` clears it. `target` names the provider the other fields apply to (defaults to `provider`). |
| `POST` | `/api/ai/forget` | Clear the credentials for one provider. Body: `{"provider"}` (defaults to the selected one). For Copilot this also performs the OAuth logout. |
| `GET` | `/api/ai/models` | Model IDs available from the selected provider, fetched live where possible and falling back to the built-in list otherwise. |
| `GET` | `/api/ai/tools` | The tool schema and system prompt handed to the model. Identical for every provider — the tools describe the 3270 session, not the backend. |
| `POST` | `/api/ai/chat` | Streaming chat proxy. Takes an OpenAI-shaped `/chat/completions` body and returns OpenAI-shaped SSE chunks, whichever provider is selected — Anthropic's Messages protocol is translated in both directions. |

```bash
# Point AI Chat at a local Ollama and confirm it is usable
curl -X POST http://127.0.0.1:3270/api/ai/config \
  -H 'Content-Type: application/json' \
  -d '{"provider":"ollama","baseUrl":"http://localhost:11434/v1","model":"qwen3"}'

curl http://127.0.0.1:3270/api/ai/status
```

The GitHub-specific `/api/copilot/*` routes (`status`, `login/start`,
`login/poll`, `logout`, `enterprise`) are unchanged and still drive the OAuth
device flow.

## System endpoints

Unauthenticated and not session-scoped — intended for liveness/readiness probes.

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Returns `200 OK` with `{"status":"ok","version":"<app version>"}`. Used by the Docker `HEALTHCHECK` and orchestrators. Performs no session or `s3270` work. See [Install and Run](installation.md). |

## Errors

| Status | Meaning |
|---|---|
| `400 Bad Request` | Bad input — e.g. missing host, CR/LF/TAB in field text |
| `401 Unauthorized` | Missing or bad `Authorization` header |
| `404 Not Found` | Session id does not exist |
| `502 Bad Gateway` | The host or s3270 subprocess returned an error |
| `503 Service Unavailable` | `API_TOKEN` not configured — API is disabled |

## Out of scope (v1)

The following are deliberately not part of v1. Some of them are tracked
in the [Feature Roadmap](feature-roadmap.md).

- WebSocket / SSE streaming of screen changes
- File transfer endpoints (`IND$FILE`)
- OAuth / OIDC / SAML auth
- Multiple tokens, per-token scopes, rate limiting
- API token rotation

## MCP

The [MCP Server](mcp.md) is built on this API. `3270Web mcp` speaks the Model
Context Protocol on stdin and stdout and calls these routes underneath, and
the running server also exposes MCP over HTTP at `POST /api/v1/mcp` behind the
same `API_TOKEN`.

The session-scoped routes above exist largely for it: naming the session in
the path is what lets a client with no browser cookie drive chaos exploration
and business understanding.
