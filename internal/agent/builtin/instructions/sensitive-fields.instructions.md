# Sensitive fields

A 3270 field marked hidden suppresses local echo on a real terminal. It does
not stop the typed characters being present in the buffer the emulator reads,
so anything that renders that buffer verbatim publishes them.

3270Web redacts on the way out: a hidden field's value is empty in every
screen serialization, and its characters are replaced with `*` in the screen
text. You will therefore never see a password in a `get_screen` result, and
should not ask for one another way. The same holds for what exploration
learned: a hidden field's working values and examples are left out of
`chaos_get_screen`, the session context and every other screen result.

## When a screen needs a secret

Do not ask the user to give you a password, PIN or passphrase in the chat, and
do not type one for them: anything said in the chat is sent to the model
provider and kept in the conversation. Ask the user, with `ask_user`, to type
it into the hidden field on the terminal themselves and to tell you when they
have. Then continue from the next step. `write_field` into a hidden field
works and is not logged, but the value it was given has already been through
the conversation — which is the thing to avoid.

## When you are annotating

Mark a field sensitive whenever it holds a credential, a token, a PIN, or
anything the screen itself hides. A parameter marked sensitive:

- cannot carry a default value — a default would be a credential stored in
  the catalogue in clear text;
- is kept out of every result, event and log line. The host still receives
  it, because it has to; nothing else retains it.

## When you are reporting

Do not echo a value the user supplied for a sensitive parameter back into your
reply, a summary, or a generated workflow file. Say that the field was filled,
not what it was filled with.

If a host screen displays something that is plainly a credential in a field
that was *not* marked hidden — an application printing a password back, which
happens — treat it as sensitive anyway, tell the user it is exposed, and do
not repeat the value.
