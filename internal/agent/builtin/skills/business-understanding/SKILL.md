---
name: business-understanding
description: Turn discovered screens into business meaning — annotate what each screen and field is for, then catalogue the named operations the application supports.
invocation: [understand-app, annotate-screens, map-business-functions]
tools: [chaos_list_screens, chaos_annotate_screen, business_save_function, business_list_functions]
instructions: [untrusted-host-data, sensitive-fields]
---

# Business understanding

Exploration discovers *what works* — which inputs are accepted, which keys
move where. This skill adds *what it means*.

## When to use

After a chaos run finishes, or whenever the user asks you to understand the
application, map its business functions, or explain what a screen is for.

## Phase A — review

1. `chaos_list_screens`. For each screen read `previewText`, `fieldMetadata`,
   `knownWorkingValues` and the destinations under `keyPresses`.
2. Infer each screen's business purpose from its own text — "customer account
   enquiry: enter an account number to see balances" — and each input field's
   meaning from the labels printed near its row and column.

## Phase B — annotate

3. `chaos_annotate_screen` for every screen you understand: a short
   `business_purpose`, plus `field_semantics` keyed by the field key from
   `fieldMetadata`, for example `"R5C20L8": {"name": "account_number",
   "example": "1234"}`.
4. Mark hidden and password fields as sensitive. See
   `sensitive-fields.instructions.md` — a field marked sensitive keeps its
   value out of every recorded artifact.

Annotations persist in the run's mind map, so this is worth doing even if
the conversation ends here.

## Phase C — catalogue business functions

5. Identify complete operations by following `keyPresses` destinations across
   screens: menu, then entry form, then confirmation.
6. Save each with `business_save_function`: concrete steps
   (`screen_hash`, inputs, `aid_key`, `expect_hash`), and a *parameter* for
   every value a user would supply.

Known working values become parameter **examples**, not defaults. Hard-code a
literal only when it is genuinely constant — a menu selection, a transaction
code. An account number that happened to work during exploration is somebody's
account, and burning it into a function makes every later run query it.

## Anti-patterns

- Annotating a screen from its hash or coordinates instead of its text.
- Cataloguing a function whose every input is a literal. That is a recording,
  not a function; it cannot answer a different question.
- Guessing a field's meaning when the screen does not say. Leave it
  unannotated and report the gap — `business_app_overview` lists gaps for
  exactly this reason.
