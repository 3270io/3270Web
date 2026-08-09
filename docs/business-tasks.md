---
description: >-
  Turn a recorded screen flow into a guided task with named inputs and a
  named answer, so someone can get a result without navigating a green
  screen.
---

# Guided Business Tasks

A task is a recorded screen flow with **named inputs** and a **named
answer**. Someone who knows the business question — but not the application
— fills in a short form and reads the result, without navigating a green
screen.

```
Account balance enquiry
Retrieves the current cleared balance for a customer account.

  Account number  [ 40218855 ]   required, 8 digits
  As-at date      [ 2026-08-08 ] optional

                          [ Back ]  [ Run ]
```

3270Web drives the application, then shows a result card:

```
Account balance enquiry                          2.4 s
  Account            40218855
  Name               J MARGOLIS
  Cleared balance    1,240.55
  Available          1,190.55

  [ Copy ]  [ CSV ]  [ Run again ]  [ Close ]
```

## Running a task

Open **Tasks** from the toolbar (the checklist icon) or the command palette.
Tasks are business-user surface, so the control stays visible in Business
mode.

Pick a task, fill in the form, press **Run**. The dialog closes and a card
appears in the corner — **the terminal stays visible the whole time.** That
is deliberate: operators need to see what the host is actually doing, and it
is how trust in the automation gets built. The card never covers the
[Operator Information Area](keyboard-and-controls.md#operator-information-area),
so `X SYSTEM` and the operator-error indicator stay readable.

**Cancel** stops the run. The terminal is left exactly where it stopped —
the request was to stop the automation, not to undo it, and a 3270
transaction cannot be rolled back.

A run lives on the server, so reloading the page does not lose it; the card
reappears and carries on reporting.

## When a task does not finish

A task stops at the first step whose screen is not what it expected, and
reports:

- **which step** it stopped at, and why
- **what it expected** against **what it found** at that position
- **the screen it stopped on**, behind a disclosure

The terminal is left on that screen so you can take over by hand.

This is the opposite of what
[recording playback](workflow.md) does, on purpose. Playback logs a failed
step and carries on, which suits a scripted regression. For a business
transaction, carrying on means typing an account number into whatever field
happens to be under the cursor on a screen nobody expected. An output that
cannot be read counts as a failure too, unless it is marked optional — a
result card showing a blank balance is worse than an honest error.

## Defining a task

### From a recording (the wizard)

Record the flow once, then press **Save recording as a task** on the
recording controls — or find it in the command palette.

1. **Record.** Start recording, work through the screens exactly as the task
   should run, then stop.
2. **Confirm the inputs.** Everything typed during the recording arrives as
   an input the operator will be asked for, labelled with the text the screen
   itself uses. Switch any that should always be the same to **Always this
   value**; that removes the input from the form and bakes the value into the
   step.
3. **Mark the answer.** Drag across the screen the flow ended on to mark each
   value the task should report back. This is the one thing a recording
   cannot work out for itself.
4. **Name it and save.**

Above the form is **What was assumed** — every guess the draft made, listed
rather than buried. Read it: a guard that could not be derived, a field with
no label, or a value that was cleared rather than typed all show up there.

!!! note "Marked regions extend to the next text on the row"

    If you mark `ADA` you get a region as wide as the *slot* the value sits
    in, not as wide as those three characters. That is deliberate — a region
    sized to the recorded value would silently truncate a longer one, so
    `GRACE` would come back as `GRA`. Trailing spaces are trimmed from the
    result.

### From a chaos run

A [chaos exploration](chaos-mode.md) maps an application's screens and the
fields that drive them, and a **business function** names one path through
that map. `GET /chaos/business/task-draft?name=...` converts one into a task
draft — or every function at once if you omit the name.

This is the join between exploration and use: the mind-map is the asset, the
task is the product.

The conversion has one real difficulty, and it is worth knowing about. Chaos
identifies a screen by **hash**, which is exact and useless to a person: when
it stops matching, all it can say is "a different screen". A task guards with
**positional text** from the screen's own labels. The bridge is the screen text
the run captured, so a function whose screens were recorded converts with real
guards — and one whose screens were not says so, per step, rather than
producing a task that would act on any screen at all.

The draft also reports anything it dropped: a key a task cannot press, a field
key it could not read, or a parameter no step fills. As with the wizard, it
proposes no outputs.

### By hand

The catalogue is **server-side**, so one person defines a task and everyone
else picks it off the menu. A task can also be written directly and posted to
`/tasks/save`:

```json
{
  "name": "Account balance enquiry",
  "description": "Retrieves the current cleared balance for a customer account.",
  "parameters": [
    {
      "name": "account_number",
      "label": "Account number",
      "required": true,
      "maxLength": 8,
      "pattern": "\\d{8}",
      "example": "40218855"
    }
  ],
  "steps": [
    {
      "description": "Enter the account number",
      "expect": [{ "row": 1, "column": 29, "text": "ACCOUNT ENQUIRY" }],
      "inputs": [
        { "row": 5, "column": 21, "length": 8, "parameter": "account_number" }
      ],
      "aidKey": "Enter"
    },
    {
      "description": "Confirm the account was found",
      "expect": [
        { "row": 22, "column": 2, "text": "INVALID ACCOUNT", "absent": true }
      ]
    }
  ],
  "outputs": [
    {
      "name": "cleared_balance",
      "label": "Cleared balance",
      "row": 8, "column": 21, "length": 14,
      "pattern": "([\\d,]+\\.\\d{2})"
    }
  ]
}
```

All rows and columns are **1-based**, matching the `CURSOR` readout in the
OIA and the coordinates in a recording.

### Parameters

| Field | Meaning |
|---|---|
| `name` | Machine identifier that steps refer to. Letters, digits, underscore. |
| `label` | What the form shows. Defaults to `name`. |
| `description` | Hint shown under the field. |
| `example` | Becomes the field's placeholder. |
| `default` | Pre-filled value. Not allowed on a sensitive parameter. |
| `required` | An empty value is refused before the run starts. |
| `maxLength` | Enforced in the form and again on the server. |
| `pattern` | RE2 expression. **Anchored at both ends**, so `\d{8}` means the whole value is eight digits, not "contains eight digits". |
| `sensitive` | Masked in the form and kept out of every result and log line. The host still receives it. |

Validation runs on the server on every run, not only in the browser. The
form is a convenience, not the gate.

### Steps

A step checks the screen, fills fields, then presses a key.

- `expect` guards the step. Every entry must match before anything is
  typed. Set `absent: true` to require that text is **not** there — that is
  how a step refuses to read an error line as an answer.
- `inputs` set exactly one of `value` (a literal, such as a menu selection)
  or `parameter`. An optional parameter left blank means *leave the field
  alone*, which on a 3270 is not the same as typing nothing.
- `aidKey` defaults to `Enter`. `PF1`–`PF24`, `PA1`–`PA3`, `Clear`, `Attn`
  and `SysReq` are accepted. Leave it empty on a final step that only needs
  to confirm where the flow landed.

Guards are **positional text, not a screen hash**. A hash changes the moment
the host paints a different date in the corner, and when it mismatches it
can only report "a different screen". An anchor on the application's own
label text survives cosmetic change and can say exactly what it wanted and
what was there instead.

### Outputs

An output names a region of the final screen.

| Field | Meaning |
|---|---|
| `name` | Machine identifier. |
| `label` | What the result card shows. |
| `row`, `column`, `length` | The region, 1-based, on a single line. |
| `pattern` | Optional RE2. With one capture group, the group is the value; without, the whole match is. |
| `optional` | Allows the value to be missing without failing the run. |

`pattern` is what turns `Cleared balance:      1,240.55 CR` into
`1,240.55`.

A region is a span on one line. An answer that wraps is two outputs —
letting one run onto the next line would capture the label of whatever sits
below it.

## API

These endpoints use the browser session cookie.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/tasks` | List the catalogue |
| `POST` | `/tasks/save` | Add or replace a task, validated |
| `POST` | `/tasks/delete` | Remove a task by name |
| `POST` | `/tasks/run` | Start a run in this session |
| `GET` | `/tasks/status` | Progress, then the result |
| `POST` | `/tasks/cancel` | Stop a run |

`/tasks/run` returns `202` immediately and the run proceeds in the
background; poll `/tasks/status` for progress and the result. One run per
session — a task drives the single terminal that session owns, and two at
once would interleave keystrokes into the same screen buffer.

### For bots and CI

The same capability is on the token-authenticated
[REST API](rest-api.md), which needs `API_TOKEN` set.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v1/tasks` | The catalogue — this is also **export** |
| `POST` | `/api/v1/tasks` | Add or replace a task — this is also **import** |
| `POST` | `/api/v1/sessions/{id}/tasks/run` | Run a task in a session and return the result |

```bash
curl -H "Authorization: Bearer $API_TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"name":"Account balance enquiry","parameters":{"account_number":"40218855"}}' \
     https://3270web.example/api/v1/sessions/$SID/tasks/run
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

The API run is **synchronous**, unlike the browser's. The two callers want
opposite things: a browser has to show progress and offer Cancel while a
transaction takes its seconds, so it polls; a bot wants the answer in the
response and would otherwise have to implement a poll loop to get it. Both are
bounded by the same five-minute ceiling, and both register in the same
per-session slot — so an API run and a browser run cannot overlap on one
terminal.

A task that stopped early returns **200 with `completed: false`** and the
failure detail, not an HTTP error. The request succeeded; the body says what
the host did. An HTTP status cannot express "step 3 saw the wrong screen", and
collapsing it into a 500 would discard the only useful part of the answer.

### Export and import

`GET /api/v1/tasks` returns exactly the documents `POST /api/v1/tasks`
accepts, so moving a catalogue between deployments — or keeping it in version
control — needs no separate format:

```bash
# Export
curl -H "Authorization: Bearer $API_TOKEN" \
     https://source.example/api/v1/tasks | jq '.tasks' > tasks.json

# Import, one task at a time
jq -c '.[]' tasks.json | while read -r t; do
  curl -H "Authorization: Bearer $API_TOKEN" -H 'Content-Type: application/json' \
       -d "$t" https://target.example/api/v1/tasks
done
```

Imported tasks go through the same validation as everything else, so a task
edited by hand in version control cannot reach the runner malformed.

## Limits

- 200 tasks in the catalogue, 100 steps in a task.
- A run is abandoned after five minutes.
- Parameter values are capped at 160 characters and may not contain CR, LF
  or TAB, which the 3270 data stream would read as actions rather than text.

## Sharing tasks

A saved task can be shipped to other installations inside an extension,
alongside the skills and instructions that explain when to use it. See
[Skills and Extensions](skills.md).
