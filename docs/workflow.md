---
seo_title: "Record a 3270 session in 3270Web and play it back later"
description: >-
  Capture what you do at the terminal as a JSON recording, then load, play,
  step through and debug it later — with every recording and playback
  control explained.
---

# Recordings and Playback

3270Web can capture terminal actions as JSON recordings and run them later.

## Recording and Playback Callouts

![Recording and playback screenshot](images/workflow-controls-real.png){: .doc-medal }
{: .doc-medal-wrap }

1. Start recording
2. Play recording
3. Debug recording
4. View recording JSON
5. Remove loaded recording
6. Save the recording as a business task

While a run is live, its progress shows in the strip under the menu bar and
in the Workflow Status widget.

## Record a Session

1. Connect to the target host.
2. Click **Start recording**.
3. Perform your terminal actions.
4. Click **Stop recording**.
5. Optional: click **Download** to save the generated JSON file.

## Load a Recording

1. Click **Load recording**.
2. Choose a `.json` file.
3. Confirm the filename appears as loaded in the Automation menu.
4. Click **View recording** to inspect the full JSON.

## Play a Recording

1. Load a recording.
2. Click **Play recording**.
3. Watch playback status in the strip under the menu bar and the Workflow Status widget.
4. Use **Pause/Resume** in play mode if needed.
5. Click **Stop playback** to end execution.

## Debug a Recording (Step-by-step)

1. Load a recording.
2. Click **Debug recording**.
3. Use **Step** to execute one action at a time.
4. Watch current step number/type in the status indicators.
5. Click **Stop playback** when done.

Debug mode is recommended for new or edited recordings.

## Importing a Macro File

A shop that has been driving a green screen from an installed terminal
emulator usually has a shelf of macros — and a shelf of macros is a reason not
to move anywhere. **Automation → Import a macro file…** reads one and turns it
into a recording, then tells you, line by line, what it would not translate.

The report is the point. A partial conversion that names the twelve lines
needing a person is worth more than a confident-looking one that quietly drops
four of them; the second kind is discovered in production, by a flow doing
something nobody asked it to.

1. Click **Automation → Import a macro file…** and choose the file.
2. Read the report: how many steps it produced, and every line it left out.
3. Close it. The recording is loaded, named after the macro.
4. Run it with **Debug recording** first. A translated flow has never met this
   host.

The same translation is on the API at
[`POST /api/v1/macros/translate`](rest-api.md#post-apiv1macrostranslate),
which takes no session and returns the recording itself — that is the one to
point at a directory of three hundred files.

### What it reads

Macro files in this category are written in a BASIC-family scripting language
against a session automation object model. The object name varies between the
products that write them — `Session`, `Sess0`, whatever a variable was called
— so it is ignored; the method decides everything, and parentheses are
optional as they are in the language itself.

| The macro says | The recording gets |
|---|---|
| `PutString "USER01", 5, 20` | `FillString` at row 5, column 20 |
| `MoveTo 5, 20` then `SendKeys "USER01"` | the same, from the position the macro set |
| `SendKeys "<Enter>"`, `<PF3>`, `<Clear>`, `<EraseEOF>`… | `PressEnter`, `PressPF3`, `PressClear`, `PressEraseEOF`… |
| `WaitForString "MAIN MENU", 1, 30` | `CheckValue` — that text, at that position |
| `WaitHostQuiet 2500` | 2.5 seconds of delay on the step that follows |
| `WaitHostQuiet` with no time | nothing: playback waits for the keyboard at every step already |
| `Connect`, `Disconnect` | `Connect`, `Disconnect` |
| a comment, `Dim`, `Sub`, `End Sub`, `Option Explicit` | nothing, and no note — it is scaffolding, not an instruction |

Function keys are read as `<PF3>` or `<F3>`; both mean the same key to the
host.

### What it will not do, and why

Each of these is reported against its line rather than approximated:

- **Branching, loops and subroutine calls.** A recording has
  [decisions of its own](#decisions-variables-and-loops) — `If`, `While`,
  `Stop` — and the importer still will not write one for you. Which lines
  belong inside a branch, and what its condition should compare on which part
  of the screen, is a judgement about the host's screens rather than a
  translation of the file; guessing it would produce a flow that takes the
  wrong path confidently. `If`, `For`, `Do`, `Call` and the rest are reported
  with their line numbers, and the branch is written by hand against the step
  types above.
- **Variables.** A recording has variables too, and the same rule applies: a
  `SetVariable` step names the row, column and length it reads, and where the
  macro's value came from is not knowable from the file.
- **Text typed with no known cursor position.** `SendKeys "USER<Tab>PASS"`
  types `USER` where the macro last put the cursor, and `PASS` into whichever
  field the host moved to — which is not knowable from the file. The first
  half translates; the second is reported. Nothing is ever emitted with the
  coordinates left off, because an absent position reads as row zero to
  everything downstream, and typing into the wrong field is exactly what this
  rule prevents.
- **A wait for text with no position.** `WaitForString "BALANCE"` searches the
  whole screen; a recording checks a named row and column. Add them and it
  translates.
- **Dialogs.** A replayed recording has nobody at the keyboard to answer a
  `MsgBox`.
- **Anything outside the terminal session** — files, other applications, the
  operating system.

### Two differences in meaning worth knowing

The report names both when they apply:

- **A wait for text becomes a check, not a wait.** Playback waits for the
  keyboard to unlock before every step, but it does not poll for text. If the
  screen behind a check is slow to draw, give that step a delay.
- **Wait times are read as milliseconds.** A macro that counted in seconds
  replays faster than it ran, which is the safer way to be wrong; the step
  delays in the recording are where to correct it.

The translated recording is a recording like any other — edit it in **View
recording**, save it as a [Guided Business Task](business-tasks.md), or feed
it to chaos exploration.

!!! warning "Treat an imported recording as a secret"
    A macro that logs in has the password in it, and so does the recording it
    becomes — and so does any report line quoting that statement. The same
    caution applies as to recordings that fill sensitive fields.

## Remove a Loaded Recording

Click **Remove recording** to clear the currently loaded file from the session.

## Workflow Status Widget

The Workflow Status panel shows:

- Current step and total steps
- Current action type
- Delay range and applied delay (when present)
- Recent playback events

When you connect to a host the widget starts minimized so it stays out of the
way until you need it.

You can:

- Open it from the status indicator
- Minimize/maximize it
- Enable or disable tracking

## Recording JSON Basics

A recording includes a `Steps` array of actions.

Example:

```json
{
  "Host": "sampleapp:app1",
  "Steps": [
    { "Type": "Connect" },
    {
      "Type": "FillString",
      "Coordinates": { "Row": 5, "Column": 21 },
      "Text": "User"
    },
    { "Type": "PressEnter" },
    { "Type": "Disconnect" }
  ]
}
```

Common action types:

- `FillString`
- `PressEnter`
- `PressTab`
- `PressPF<n>` (for example `PressPF3`)

## Decisions, Variables and Loops

Everything above describes a straight line: fill this field, press that key,
check that text. That is the whole of what a recorded flow needs when the
answer is the same every time.

It is not the whole of what a flow needs when the host has an opinion. Read
the balance, and take a different path if it is under the limit. Press Enter
until the screen stops saying `MORE`. Skip the confirmation panel on the days
the host does not draw one. Those need a recording that can decide, and the
step types below are that.

They are ordinary steps in the same `Steps` array — nothing nests, and a
recording that makes no decisions is byte-for-byte the file it always was.

| Step | What it does |
|---|---|
| `SetVariable` | Remember a literal, or the text at a place on the screen |
| `If` … `Else` … `EndIf` | Run one group of steps or the other |
| `While` … `EndWhile` | Repeat a group of steps while something stays true |
| `Stop` | End the run here, and say why |

### Reading something off the screen

`SetVariable` with `Coordinates` reads that region of the screen and trims the
padding a field carries; with `Text` instead, it stores that literal.

```json
{
  "Type": "SetVariable",
  "Variable": "balance",
  "Coordinates": { "Row": 12, "Column": 40, "Length": 12 }
}
```

Anywhere a later `FillString` or `CheckValue` writes `${balance}`, the value
goes in:

```json
{
  "Type": "FillString",
  "Coordinates": { "Row": 5, "Column": 21 },
  "Text": "ACCT ${account}"
}
```

A name that has not been set is an error that ends the run, rather than an
empty string quietly typed into a field. To type the two characters `${`
literally, write `$${`.

Add `"Sensitive": true` to a `SetVariable` step that reads a password or
anything else that should not be written down. The run still uses the value —
`${name}` fills the field it was read for — but the run log and the status
endpoint say the variable was set rather than what it was set to.

### Asking a question about the screen

`If` and `While` compare one thing against another. The left-hand side is
either `Coordinates` (a region of the screen, trimmed) or `Variable` (one you
set earlier) — one or the other, never both. `Text` is what it is compared
against, and may itself contain `${…}`.

```json
[
  {
    "Type": "If",
    "Coordinates": { "Row": 24, "Column": 2, "Length": 9 },
    "Operator": "contains",
    "Text": "NOT FOUND"
  },
  { "Type": "Stop", "Text": "the host has no record of that account" },
  { "Type": "EndIf" }
]
```

The comparisons are `equals`, `notEquals`, `contains`, `notContains`,
`startsWith`, `endsWith`, `isEmpty`, `isNotEmpty`, `greaterThan`,
`greaterOrEqual`, `lessThan` and `lessOrEqual`. The symbolic spellings
(`==`, `!=`, `>`, `>=`, `<`, `<=`) mean the same thing. Add
`"IgnoreCase": true` to fold case; comparisons are case-sensitive without it.

The four numeric comparisons read a number the way a green screen writes one:
grouped with commas, and negative by a leading sign, a trailing one, or a pair
of brackets — `1,240.55`, `45.00-`, `(12)` and `1,000CR` all parse. A value
that is not a number ends the run rather than being compared as zero, because
a screen that did not say what the recording expected is worth stopping for.

### Repeating until the host is done

```json
[
  {
    "Type": "While",
    "Coordinates": { "Row": 24, "Column": 70, "Length": 4 },
    "Operator": "equals",
    "Text": "MORE",
    "MaxIterations": 20
  },
  { "Type": "PressPF8" },
  { "Type": "EndWhile" }
]
```

Every `While` is bounded. `MaxIterations` sets the bound, up to 10000; without
it, a loop stops after 100 passes. A loop that reaches its bound ends the run
and says so — a host that never changes its screen would otherwise hold the
session until somebody noticed.

### What stops a run

Everywhere else in playback, a step that fails is logged and the run carries
on to the next one. That is right for a step that acts and wrong for a step
that decides: carrying on past an `If` whose condition could not be evaluated
means guessing which branch was wanted, and the wrong guess types real values
into a real host. So these end the run instead:

- a condition that cannot be evaluated — a region off the screen, a variable
  that was never set, a comparison of a number against text
- a `${name}` in a step's text that names nothing the run has set
- a `While` that reaches its iteration bound
- a `Stop` step, which is the deliberate one; it counts as a completed run,
  and its `Text` is the reason in the log

Structural mistakes are caught earlier still. A recording whose blocks do not
close, or whose comparison names an operator that does not exist, is refused
when it is **loaded** — before it has typed anything anywhere. The message
names the step number.

### Watching it decide

The Workflow Status widget carries each decision as it is made — which branch
an `If` took and on what value, which pass a `While` is on, what a
`SetVariable` read. `GET /workflow/status` returns the same events, plus
`playbackVariables`: what the run knows, which survives the end of the run so
"what did it read?" is answerable afterwards.

!!! note "Variables are per run"
    A run starts knowing nothing. That is what makes a replay a replay — a
    recording that behaved differently on its second run because of something
    left over from its first would be much harder to trust.

### Business metadata fields

Workflows generated from a cataloged business function (see [AI Chat — Business Understanding](ai-chat.md#business-understanding)) carry optional top-level metadata. All fields are ignored by playback, so older workflows and older 3270Web versions remain fully compatible:

```json
{
  "Host": "sampleapp:app1",
  "Port": 3270,
  "Name": "Account inquiry",
  "Description": "Look up an account balance",
  "BusinessFunction": "Account inquiry",
  "Parameters": [
    { "Name": "account_number", "Value": "1234", "Row": 5, "Column": 21, "Length": 8 }
  ],
  "Steps": [ { "Type": "Connect" }, { "Type": "PressEnter" }, { "Type": "Disconnect" } ]
}
```

`Parameters` documents which business input produced which `FillString` value and where it was written, making the file self-describing.

Parameters whose value lands in a hidden (password-style) field, or in a field the AI marked sensitive, are exported with `"Sensitive": true` and an empty `Value` in the metadata. The `FillString` step itself still carries the value — playback needs it — so treat generated workflow files that fill sensitive fields as secrets.

## Troubleshooting Playback

- Confirm host and port are correct.
- Confirm terminal model matches the one used when recording.
- Confirm screen layout and field coordinates still match host screens.
- Add delays for timing-sensitive screens.
- Use Debug mode to find the first failing step.

## Related: Chaos Exploration

Use Chaos mode when you need to discover unknown screen paths or generate new workflow steps automatically.

- Start/stop chaos from the Automation menu
- Review granular attempt status in the Workflow Status widget
- Export the generated JSON and load it back as a standard workflow

See [Chaos Mode](chaos-mode.md) for full details.
