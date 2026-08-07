# Keyboard and Controls

This page explains toolbar controls, keyboard mappings, and the virtual keypad.

## Toolbar Controls

Main toolbar actions include:

- Disconnect session
- View logs
- Print screen
- Start/stop recording
- Load recording
- Play/debug/view/remove recording
- Playback pause/resume/stop controls (when active)
- Chaos exploration controls
- Command palette, Copilot chat, and settings

![Toolbar screenshot](images/toolbar-real.png){: .doc-medal }
{: .doc-medal-wrap }

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

While the palette is open it takes over the keyboard completely, so
nothing you type leaks through to the 3270 screen underneath.

## Terminal Status Bar

The bar directly beneath the screen is the operator information area.

![Terminal status bar](images/terminal-status-bar.png)

| Field | Meaning |
|---|---|
| `KB` | Keyboard state — green `UNLOCKED` when you can type, amber `LOCKED` while the host is inhibiting input, red on `ERROR` |
| `MODEL` | Negotiated 3270 model number |
| `SIZE` | Screen geometry in rows × columns |
| `CURSOR` | Current cursor row and column |

If the keyboard shows `LOCKED`, the host has not finished processing the
last submission — wait, or press `Reset` from the keypad.

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

- `Enter` -> Enter
- `Tab` -> Tab
- `Shift+Tab` -> BackTab
- `Esc` -> Clear (**press twice** — see below)
- `Arrow keys` -> Cursor movement
- `Home` -> Home
- `Backspace` -> BackSpace
- `Delete` -> Delete
- `Insert` -> Insert
- `F1..F12` -> `PF1..PF12`
- `Shift+F1..Shift+F12` -> `PF13..PF24`
- `Alt+F1` -> `PA1`
- `Alt+F2` -> `PA2`
- `Alt+F3` -> `PA3`

Additional 3270 actions available in keypad/full mappings include Reset, EraseEOF, EraseInput, Dup, FieldMark, SysReq, Attn, and NewLine.

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

## Focus and Input Behavior

When you press mapped keys, 3270Web sends the action to the host and refreshes the terminal content. Cursor-aware behavior is preserved for field input where possible.

## Tips for Reliable Use

- Keep browser focus on the terminal area while typing.
- Use the keypad for less common 3270 keys if your keyboard layout does not expose them.
- If commands appear out of sync, pause briefly and retry in debug playback mode.
