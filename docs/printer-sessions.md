---
description: >-
  Bind a 3287 printer LU beside a terminal session. The host prints, the job
  arrives as a file, and you download it — from the panel or over the API.
---

# Printer sessions

Batch output on a mainframe does not come back on the display. The host binds
a separate **printer LU**, sends it an SCS (LU1) or 3270 (LU3) datastream, and
ends the job. The overnight balance report, the picking list, the audit
extract: none of them appear on the screen the operator is looking at.

3270Web binds one of those printer LUs alongside a terminal session. There is
no printer attached to the server and no reason to assume there should be, so
each finished job lands as a **file the session owns**, and whoever is at the
terminal downloads it.

## What you need

A `pr3287` binary. It ships with the x3270 suite alongside `s3270` — the same
project 3270Web already drives, and the same authors (see
[Acknowledgements](acknowledgements.md)). It speaks both printer datastreams,
so 3270Web does not reimplement the protocol.

=== "Docker"

    Already installed. The image carries `pr3287` beside `s3270`.

=== "Debian / Ubuntu"

    ```bash
    sudo apt-get install pr3287
    ```

=== "macOS"

    ```bash
    brew install x3270
    ```

=== "Windows"

    Install the x3270 suite and put `wpr3287.exe` beside `3270Web.exe`, or
    point `PR3287_PATH` at it.

If the binary is somewhere of its own, name it:

```bash
PR3287_PATH=/opt/x3270/bin/pr3287
```

Without it the panel says so and the API answers `501 Not Implemented` rather
than failing at the moment somebody prints something.

## Binding a printer

**Session → Printer session** in the terminal's menu bar.

There are two ways a host offers a printer, and the panel asks which one this
host uses:

| | When to use it |
|---|---|
| **The associated printer** | The host pairs a printer with each display LU. 3270Web asks the terminal what LU the host bound *it* as, and requests that LU's printer. Nothing to type. |
| **A printer LU by name** | The host has a named printer LU — `PRT001`, `LOCAL3287` — that your site allocated. Type the name. |

If association is chosen and the host has not bound this session to a named
LU, the panel says so and asks for a name instead. That is a real answer
rather than an error: a host that binds no LU has no associated printer to
offer.

### Print options

Under **Print options**, and all optional:

| Option | What it does |
|---|---|
| Line length (MPP) | Maximum presentation position for LU3 printing, 40–256. Default 132. |
| Code page | An alternate EBCDIC code page, if the printer LU differs from the display. |
| End-of-job timeout | End a job after this many idle seconds, for hosts that never send an end-of-job. |
| End lines with CR/LF | On by default, so a downloaded file opens correctly on Windows. |
| Keep blank lines | Keep the blank lines LU3 formatted mode otherwise suppresses. |
| Ignore a formfeed at the top of a page | Skip a formfeed that would otherwise produce a blank first page. |
| Pass formfeeds through | Emit the formfeed rather than interpreting it, in SCS mode. |
| Ignore the host's end-of-job | For hosts whose end-of-job arrives in the wrong place, or not at all. |
| Drop ASA carriage control | Strip the carriage-control character from the first column. |

### What is not yours to choose

A printer session always connects to **the same host, port and TLS terms as
the terminal session it belongs to**. You pick the LU and the print options;
nothing else is settable.

That is deliberate. An endpoint that accepted a hostname would be a way to
make the server open connections wherever it can reach, from a request. And
the real case is always the printer LU on the mainframe the operator is
already signed in to. TLS is read from what the terminal actually negotiated,
not from what its profile asked for, so the two halves of one connection
cannot end up on different terms.

## Collecting the output

Each finished job appears in the panel with the time it arrived and its size.
**Download** saves it; **Delete** removes it.

A job is served as a file to save, never rendered in the browser. It is
whatever the host sent, and this origin is not the place to run it as a
document. What you get is the plain SCS or line-printer stream: open it in a
text editor, or send it to a real printer with `lpr`.

!!! warning "Jobs go when the session does"

    Print jobs belong to the terminal session that collected them, and are
    deleted when it ends — a disconnect, a browser closed for long enough to
    be reaped, an administrator ending the session. Download anything you want
    to keep before then.

    Stopping the *printer* does not remove them. That is something you do on
    purpose, often to rebind against a different LU, and losing the output you
    just collected would be a surprise.

### Limits

| Bound | Value | What happens past it |
|---|---|---|
| One job | 8 MB | Kept, capped, and listed as **truncated** — a report that quietly stops two-thirds of the way down is worse than one that says where it stopped. |
| One session's spool | 64 MB | The oldest jobs are dropped. |
| Jobs per session | 200 | The oldest jobs are dropped. |

## Over the API

Every one of these is under `/api/v1/sessions/{id}` and needs a bearer token —
see [REST API](rest-api.md). The case for driving a printer without a browser
is the ordinary one: a job runs overnight, the output goes to a printer LU,
and something collects it in the morning.

```bash
# Bind the printer associated with this session's display LU
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"associate":true,"crlf":true}' \
     http://localhost:3270/api/v1/sessions/$SESSION/printer

# ...or a named LU, with a line length
curl -X POST -H "Authorization: Bearer $API_TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"lu":"PRT001","mpp":132}' \
     http://localhost:3270/api/v1/sessions/$SESSION/printer

# What is bound, and what has arrived
curl -H "Authorization: Bearer $API_TOKEN" \
     http://localhost:3270/api/v1/sessions/$SESSION/printer

# Take one job
curl -H "Authorization: Bearer $API_TOKEN" -OJ \
     "http://localhost:3270/api/v1/sessions/$SESSION/printer/jobs?name=$JOB"

# Let it go
curl -X DELETE -H "Authorization: Bearer $API_TOKEN" \
     http://localhost:3270/api/v1/sessions/$SESSION/printer
```

A status reply:

```json
{
  "available": true,
  "running": true,
  "printer": {
    "session": "4f2c...",
    "host": "mainframe.example",
    "port": 992,
    "lu": "PRT001",
    "tls": true,
    "started_at": "2026-08-10T09:14:02Z",
    "running": true
  },
  "jobs": [
    {
      "name": "job-20260810T091455Z-3fa1c209.prt",
      "bytes": 18422,
      "received_at": "2026-08-10T09:14:55Z",
      "truncated": false
    }
  ]
}
```

## If something goes wrong

**"This installation has no pr3287 binary."** Install it, or set
`PR3287_PATH`. See [What you need](#what-you-need).

**The printer stops immediately, naming the LU.** The host refused the bind.
Usually the LU name is wrong, or somebody else holds it. The panel shows what
`pr3287` said rather than a generic failure, because that sentence is normally
the whole diagnosis.

**The printer stops later, saying nothing.** The host closed the printer
session. 3270Web does not reconnect on its own: an LU that was refused would
otherwise retry forever behind a status that says "running", and a printer
session that stops should say so. Start it again.

**Nothing arrives, but the status says running.** The host has not printed
anything to that LU yet, or it is printing to a different one. Check with the
application; the **Connection details** panel shows what the terminal itself
was bound as, which is the LU an associated printer hangs off.

**A job arrives but ends early.** Look for **truncated** beside it — see
[Limits](#limits).

**A job is one long line, or has a stray character at the start of each
line.** That is carriage-control handling. Try **Drop ASA carriage control**,
or adjust the line length.

## What this is not

Printing to a *physical* printer on the operator's desk. The job arrives in
the browser as a file, and what happens next is the operating system's
business — which is the trade a browser-delivered terminal makes everywhere
else too. A shop that wants paper can send the downloaded file to `lpr`, or
run a `pr3287` of its own beside the printer and let 3270Web handle the
screens.

## Where the pieces are

| | |
|---|---|
| The panel | **Session → Printer session** |
| The API | `/api/v1/sessions/{id}/printer` — see [REST API](rest-api.md) |
| The trail | `printer.started` and `printer.stopped` in the [audit log](multi-user.md#the-audit-trail), with the LU and the host |
| The setting | `PR3287_PATH`, when the binary is not on the path |
