---
name: perform-business-function
description: Carry out a business operation the user asked for in plain language — run it on the live session, or export it as a reusable workflow.
invocation: [run-function, do-business-task, lookup]
tools: [business_list_functions, chaos_list_screens, get_screen, write_field, send_key, submit_screen, business_generate_workflow]
instructions: [untrusted-host-data, aid-key-safety, sensitive-fields]
---

# Performing a business function

The user asks for an outcome — "look up account 1234", "create a new
customer" — rather than for a sequence of keystrokes.

## When to use

Any plain-language request for an operation the application performs.

## Steps

1. `business_list_functions` and match the request against the catalogue. Use
   `chaos_list_screens` if you need more context to choose between two
   candidates.

   If nothing matches, say so. Offer to explore with the `chaos-monkey` skill
   or to navigate manually and record a new function — do not improvise a
   path through an application you have not mapped.

2. **To do it now on the live session**: follow the function's steps with
   `get_screen`, `write_field` and `send_key`, substituting the user's
   values, and verify with `get_screen` before each write.

   Each step's inputs carry a `field_key` such as `R5C10L8`. Pass it straight
   through to `write_field`'s `field_key` parameter. Do not convert it to
   row/column yourself: the key is 1-indexed and `get_screen` coordinates are
   0-indexed, and the server does that conversion correctly.

3. **To produce a reusable file** — the user says save, export, automate, or
   asks for a workflow: collect any missing required parameters from the
   user, then call `business_generate_workflow` and offer the JSON.

## Verify before you type

A function's steps were recorded against particular screens. Check you are on
the screen the step expects before writing to it. Typing an account number
into whatever field happens to be under the cursor on an unexpected screen is
the failure this check exists to prevent, and on a screen that is one field
away from a different transaction it is not a harmless one.

## Anti-patterns

- Running a function without checking the screen matches its first step.
- Substituting a value into a field the function marked sensitive and then
  echoing it back in your reply.
- Inventing a path when no catalogued function matches. Say what you could
  not find.
