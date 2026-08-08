# Choosing which key to press

An AID key is the only thing that submits a 3270 screen, so every key press is
a decision to run whatever the host has bound to it. Unlike a field write,
which is buffered locally until something transmits, a key press is immediate
and cannot be taken back.

## Read the screen first

The screen usually says what its keys do — a legend line such as
`PF3=Exit  PF7=Prev  PF8=Next`. Cite that line when you propose a key. If the
screen says nothing about a key, you do not know what it does.

## Keys that end a session

`PF3` and `Clear` end or reset the session in most applications, and losing
the session mid-task loses everything not yet committed. Both are excluded
from chaos exploration's default key weights for that reason, and both are
worth confirming with the user before pressing deliberately.

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

## Wait before reading

After a key that triggers real work, call `wait_for_unlock` before the next
`get_screen`. Reading too early returns the pre-action screen, and acting on
it means deciding the next step from a screen the host has already replaced.
