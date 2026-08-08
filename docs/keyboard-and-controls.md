# Keyboard and Controls

This page explains toolbar controls, keyboard mappings, and the virtual keypad.

## Toolbar Controls

### Workspace modes

The toolbar has two surfaces, chosen by the **Business / Engineering**
button in the header. The choice is remembered per browser.

**Business** is the default and shows only what you need to use a
mainframe application:

- Disconnect and Reconnect
- View logs
- Print screen
- Copy screen
- Command palette, Copilot chat, and settings

**Engineering** adds the automation surface on top:

- Start/stop recording
- Load recording; play, debug, view or remove it
- Playback pause/resume/stop controls (when active)
- Chaos exploration controls
- The workflow status widget

![Toolbar screenshot](images/toolbar-real.png){: .doc-medal }
{: .doc-medal-wrap }

Nothing is removed in Business mode — switching modes is one click, and
everything in Engineering mode remains reachable from the command palette
once you are there.

!!! note "Runs are always visible"

    If a recording, playback or chaos run starts while you are in
    Business mode — Copilot can start one — the workflow status widget
    appears anyway, and goes away again when the run ends. You are never
    left watching a terminal move on its own with no way to see why.

The **Recording** and **Chaos** labels are group toggles — click one to
expand its controls, and the choice is remembered. Anything hidden inside
a collapsed group is still reachable from the command palette.

## Command Palette

Press ++ctrl+k++ (++cmd+k++ on macOS) anywhere in the session — including
while the terminal has keyboard focus — to open a searchable list of
every toolbar and modal action.

![Command palette screenshot](images/command-palette.png)

- Type to filter; matches are highlighted and grouped by area.
- ++up++ / ++down++ move through results, ++enter++ runs the highlighted
  one, ++esc++ closes without doing anything.
- Recently used commands appear first when the search box is empty.
- Theme switching is available here too, so you can change the look
  without opening Settings.
- Terminal operations are here: copy the screen, copy a marked block,
  reconnect, toggle insert mode, reset the keyboard, show the keypad,
  switch workspace mode.
- Every host key is searchable but hidden from the default list, since
  twenty-four PF keys would crowd out everything else. Type `pf3`,
  `attn`, `erase` or `pa1` to reach them.
- Commands whose control is hidden are hidden too, so in Business mode
  the palette does not offer recording or chaos actions.

While the palette is open it takes over the keyboard completely, so
nothing you type leaks through to the 3270 screen underneath.

## Operator Information Area

The bar directly beneath the screen is the OIA, and it carries the same
indicators a 3270 operator has read reflexively for forty years.

![Terminal status bar](images/terminal-status-bar.png)

| Position | Meaning |
|---|---|
| Left block | `4` online, `4-A` online and owned by an application, `–` disconnected, `?` status not yet readable |
| Input inhibit | Empty when ready. `X SYSTEM` while the host is processing. `X -f` on an operator error |
| `^` | Insert mode is on (see [Insert and overtype](#insert-and-overtype)) |
| `MODEL` | Negotiated 3270 model number |
| `SIZE` | Screen geometry in rows × columns |
| `CURSOR` | Current cursor row and column |

The two indicators on the left are the ones that matter:

- **`X SYSTEM`** means the host has the keyboard and is thinking. Wait.
  Pressing Enter again will not help. It appears the moment an AID key
  goes out, so there is no window in which the terminal looks idle while
  the host is busy.
- **`X -f`** means input is inhibited by an operator error — most often a
  letter typed into a numeric field, or a full field in insert mode. It
  will not clear on its own: press ++esc++ or `Reset`.

The terminal bezel also tints while input is inhibited, so a locked
keyboard is noticeable without reading the bar.

!!! info "Screen readers"

    Only the inhibit explanation is announced, not the whole bar. The bar
    also carries the cursor position, and announcing that on every
    keystroke made the terminal unusable with a screen reader.

## Virtual Keyboard (Keypad)

Use the keyboard icon to show or hide the virtual keypad.

Keypad features:

- Compact and full modes
- PF keys (`PF1` to `PF24`)
- PA keys (`PA1` to `PA3`)
- Common editing/navigation keys
- Dedicated Hide button

The keypad visibility preference can be saved in Settings (`Use keypad`).

![Virtual keyboard screenshot](images/keypad-real.png){: .doc-medal }
{: .doc-medal-wrap }

1. Keypad title area
2. Compact/full mode toggle
3. Hide keypad
4. PF key groups
5. PA key group
6. Common 3270 action keys

## Physical Keyboard Mappings

Common mappings used by 3270Web:

| Key | Action | Where it runs |
|---|---|---|
| `Enter` | Enter | Host |
| `F1`..`F12` | `PF1`..`PF12` | Host |
| `Shift+F1`..`Shift+F12` | `PF13`..`PF24` | Host |
| `Alt+F1` / `Alt+F2` / `Alt+F3` | `PA1` / `PA2` / `PA3` | Host |
| `Esc` | Clear (**press twice**) — or Reset while input is inhibited | Host |
| `Tab` / `Shift+Tab` | Next / previous field | **Terminal** |
| `Arrow keys` | Cursor movement | **Terminal** |
| `Home` | First input field | **Terminal** |
| `Insert` | Toggle insert / overtype | **Terminal** |
| `Backspace` / `Delete` | Edit within the field | **Terminal** |

Additional 3270 actions available from the keypad and the command palette
include Reset, EraseEOF, EraseInput, Dup, FieldMark, SysReq, Attn, and
NewLine.

### Cursor movement is local

Tab, Back-Tab, the arrows and Home move the cursor inside the browser and
do not contact the host. On a 3270 the cursor belongs to the terminal; the
host is told where it is exactly once, in the inbound data stream, when an
AID key is pressed.

This matters on a real network. Each of those keys used to cost a full
round-trip — submit, s3270 action, screen re-read, re-render — so tabbing
through a twelve-field screen over a WAN meant a dozen waits for movement
the host never sees. They are now instant.

Two deviations from a hardware terminal are worth knowing:

- A field's caret range is bounded by the text currently in it, not its
  full width, so arrowing right past the end of a short value moves to the
  next field rather than walking the field's blank tail. Typing still
  fills the field normally.
- Up and down move between input fields. Rows made entirely of protected
  text are skipped, because there is nowhere in this rendering to park a
  cursor that is not in a field.

### Insert and overtype

The terminal starts in overtype, the 3270 default: what you type replaces
the character under the cursor. Press ++ins++ to switch to insert, which
pushes the rest of the field right; the OIA shows `^` while it is on.
Typing into a field that is already full in insert mode raises an
operator error rather than silently dropping the character.

Insert is a terminal function — pressing it never contacts the host.

### Numeric fields

Fields the host marks numeric accept only digits, `.`, `,`, `-` and `+`.
Typing anything else is refused and raises `X -f` in the OIA; press
++esc++ or `Reset` to continue. This is what a real terminal does, and it
is a round-trip faster than letting the host reject the value.

Pasting into a numeric field is treated more leniently: non-numeric
characters are stripped and the digits land. Copying an account number
that arrived with spaces or dashes in it is routine, and refusing the
whole paste over one stray character would be the wrong answer.

### Type-ahead

Keystrokes typed while the host holds the keyboard are buffered and
replayed once the screen comes back, instead of being dropped. AID keys
are deliberately *not* buffered — replaying a transaction fired against a
screen you had not yet seen is a hazard, not a convenience.

!!! warning "Escape needs a second press"

    On a 3270 terminal `Clear` wipes the entire screen, but most people
    press ++esc++ reflexively to dismiss things. 3270Web therefore arms
    `Clear` on the first ++esc++ and only sends it if you press ++esc++
    again within two seconds. Any other key — or letting the window
    elapse — cancels it.

### Moving focus out of the terminal

While the terminal has focus it captures ++tab++ so field navigation
reaches the host instead of moving browser focus. To get out to the rest
of the page, press ++ctrl+tab++ (or ++ctrl+shift+tab++).

## Hotspots

Most mainframe screens print a key legend — `F1=Help  F3=Exit  F7=Bkwd`.
With hotspots on (the default), those are clickable: click `F3` on the
screen and 3270Web sends PF3. URLs printed on a screen are clickable too
and open in a new tab.

Toggle them from the toolbar or the command palette; the choice is
remembered per browser.

Two rules keep hotspots from doing anything surprising:

- **Never over an input field.** `PF3` sitting inside a value someone
  typed is data, not a control, so it does not become clickable.
- **Only real keys.** `PF1`–`PF24` and `PA1`–`PA3` are recognised;
  anything outside those ranges (`F30`, `PA9`) is ignored, so a screen
  full of numbers does not sprout dead hotspots.

## Find on screen

++ctrl+f++ (or the toolbar's magnifier) opens a find bar over the
terminal. ++enter++ moves to the next match, ++shift+enter++ the
previous, ++esc++ closes.

This searches the screen, not the page. The browser's own ++ctrl+f++
cannot see values inside input fields — which is where the account
numbers and names actually are — and cannot match text that straddles a
field boundary. Both work here.

Stepping onto a match that sits in an input field also moves the 3270
cursor there, since someone who searched for a field is usually about to
type in it.

Outside the terminal — in the Copilot panel, in a settings field —
++ctrl+f++ is left alone and the browser's own find still works.

## Screen history

A 3270 screen is repainted in place, so once the host moves on, whatever
was there is gone. The **Screen history** toolbar button (also in the
command palette) opens a read-only view of recent screens.

- ++left++ / ++right++ or the Older / Newer buttons move between screens.
- **Copy** puts the displayed screen on the clipboard.
- ++esc++ closes.

The last 50 screens are kept per session, as plain text. Consecutive
identical screens are recorded once, so the history is a record of what
you saw rather than of how often the browser polled.

It is deliberately read-only: you cannot type into a screen from five
minutes ago, and pretending otherwise would be worse than not offering
it. History is per session and does not survive a disconnect.

## Copying from the screen

The screen is rendered as text with input fields spliced into it, so
dragging across it with the mouse produces mangled output — the input
values drop out of the selection. Use these instead:

- **Copy screen** (toolbar, or the command palette) copies the whole
  screen as text, including anything you have typed but not yet
  submitted. Values in hidden password fields are never copied, since
  they were never displayed.
- **Rectangular block copy**: hold ++alt++ and drag over the screen to
  mark a rectangle, then press ++ctrl+c++. ++esc++ clears the mark. This
  is how you get a column of values into a spreadsheet without the
  surrounding text coming with it.

## Reconnecting

If the host drops the connection, 3270Web notices, shows
`X DISCONNECTED` in the OIA, and retries automatically with a backoff
(1s, 2s, 4s, 8s, 15s). If it comes back, the page reloads on the new
connection. If it does not, a banner offers a manual **Reconnect**, which
is also on the toolbar and in the command palette.

Reconnecting starts a new host session, so recording and chaos state from
the dead connection does not survive — the host-side conversation those
features were tracking ended when the connection did.

## Focus and Input Behavior

Host keys are sent to the host and the terminal content is refreshed.
Cursor movement, insert mode and field validation are handled locally
(see above), so only keys the host actually needs cause a round-trip.

## Tips for Reliable Use

- Keep browser focus on the terminal area while typing.
- Use the keypad for less common 3270 keys if your keyboard layout does not expose them.
- If commands appear out of sync, pause briefly and retry in debug playback mode.
