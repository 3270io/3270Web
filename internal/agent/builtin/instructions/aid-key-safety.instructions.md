# Choosing which key to press

An AID key is the only thing that submits a 3270 screen: Enter, PF1-PF24,
PA1-PA3 and Clear. Pressing one is a decision to run whatever the host has
bound to it, and it cannot be taken back. A field write is different — it goes
into the terminal's own buffer and the host hears nothing until an AID key
transmits it. Tab, BackTab, Home, the arrow keys, EraseEOF, EraseInput and
Reset are not AID keys: they move the cursor or edit that local buffer.

## Read the screen first

The screen usually says what its keys do — a legend line such as
`PF3=Exit  PF7=Prev  PF8=Next`. Cite that line when you propose a key. If the
screen says nothing about a key, you do not know what it does.

## Keys that end a session

`PF3` and `Clear` end or reset the session in most applications, and losing
the session mid-task loses everything not yet committed. Both are excluded
from chaos exploration's default key weights for that reason. Neither runs
unattended: the server refuses them, in any spelling (`PF3`, `F3`, `pf(3)`),
until a person has confirmed the press, and that holds with tool calls
auto-approved. Say what you are about to press and why before asking.

## Blacklists are a precondition, not advice

Before starting an exploration run, record the keys it must not press.
`chaos_start` refuses to begin without a blacklist unless it is explicitly
overridden, because an exploration that logs itself out on step four has told
you nothing and has to be started again from a screen you may no longer be
able to reach.

## Unknown keys fail, they do not fall back

A key name the server does not recognise is rejected. It is never quietly
treated as Enter. If a key press returns an error, read the error rather than
retrying with a different spelling — the vocabulary is `Enter`, `PF1`-`PF24`,
`PA1`-`PA3`, `Tab`, `BackTab`, `Clear`, `Reset`, `EraseEOF`, `EraseInput`,
`Home`, and the four arrow keys.

## Read the result

`send_key` and `submit_screen` wait for the host and return the screen that
came back, so there is nothing to wait for and no need to read the screen
again. Look at what was returned before deciding the next step: whether the
screen changed, whether the host put up a message, and the keyboard state.

- `unlocked` — carry on.
- `locked` — the host had not finished when the call returned. Call
  `wait_for_unlock`; this is the only time it is needed.
- `error` — the terminal rejected a keystroke and will refuse everything until
  it is released. Send `Reset`, read the screen, and try again.

A screen that did not change and shows a message is the host refusing what was
sent. Read the message and correct the input; pressing the key again sends the
same thing.
