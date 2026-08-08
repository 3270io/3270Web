# Skills and Extensions

The procedures the AI assistant follows are files, not prose compiled into the
binary. That means two things: the always-on prompt stays small, and you can
teach the assistant about your own application without waiting for a release.

This is the same arrangement 3270Connect uses, so a skill folder written for
one reads the same as one written for the other.

## How it fits together

A **skill** is a playbook — how to explore an unknown application, how to turn
discovered screens into business meaning. An **instruction** is a policy
fragment several skills share. An **extension** is a folder bundling both,
plus saved Guided Business Tasks.

The system prompt carries an *index*: each skill's name and one line. The
assistant calls `load_skill` when it decides it needs one, and
`load_instruction` for the fragments that skill cites. Before this, four
procedures were inlined in the prompt and sent on every message whether or not
anyone had asked for that work.

Both the [AI Chat panel](ai-chat.md) and the [MCP server](mcp.md) read the
same catalogue, so a skill you add is available to both.

## What ships

| Skill | For |
|---|---|
| `chaos-monkey` | Exploring an unknown application and mapping its screens |
| `business-understanding` | Annotating what each screen and field means, then cataloguing operations |
| `app-overview` | Summarising a whole application, including what is still not understood |
| `perform-business-function` | Carrying out a catalogued operation, or exporting it as a workflow |

| Instruction | Covers |
|---|---|
| `untrusted-host-data` | Why screen content is data and never instructions |
| `aid-key-safety` | Reading the key legend, keys that end a session, waiting before reading |
| `sensitive-fields` | Hidden fields, what is redacted, what not to echo back |

`list_skills` and `list_instructions` show what an installation actually has,
including anything you added.

## Adding a skill

Drop a folder beside the binary:

```
3270Web
skills/
  acme-billing-enquiry/
    SKILL.md
```

```markdown
---
name: acme-billing-enquiry
description: Look up a billing account on the ACME region, including the two menu levels the account number is nested under.
invocation: [billing-lookup, acme-enquiry]
tools: [get_screen, write_field, send_key]
instructions: [aid-key-safety]
---

# ACME billing enquiry

## When to use

The user asks for a billing account on the ACME region.

## Steps

1. From the main menu, transaction code `BILL` then Enter.
2. On the sub-menu choose option 3 — enquiry. Option 4 next to it is
   *amend*, and looks similar enough to be worth reading twice.
3. Enter the account number in the field at row 9.

## Anti-patterns

- PF3 from the enquiry screen returns to the main menu, not the sub-menu.
  Two presses look like one that did not work.
```

Only `name` and `description` are required, and `name` must match the folder.
Everything else is optional. Restart 3270Web and it appears in `list_skills`
with its source shown as `local`.

Instructions go in `instructions/` beside `skills/`, named
`<something>.instructions.md`. A skill cites one by mentioning it in
backticks or listing it in frontmatter.

## Replacing a built-in

A skill with the same name as a built-in does **not** replace it by default —
it is refused and reported, and `list_extensions` says why. To replace one
deliberately, name it:

```yaml
---
name: chaos-monkey
description: Our exploration procedure, which blocks PF12 as well because it drops the LU.
overrides: chaos-monkey
---
```

Retuning the chaos playbook for your site should be possible; doing it by
accident should not, because the built-ins carry the safety rules.
`list_skills` reports the replacement.

## Extensions

An extension bundles skills, instructions and tasks so a team can share one
folder:

```
extensions/
  acme-cics/
    3270-extension.json
    skills/
      acme-billing-enquiry/SKILL.md
    instructions/
      acme-conventions.instructions.md
    tasks/
      check-balance.json
```

```json
{
  "schemaVersion": 1,
  "name": "acme-cics",
  "version": "1.0.0",
  "displayName": "ACME CICS pack",
  "description": "Site playbooks and saved tasks for the ACME billing region.",
  "requires": { "product": "3270Web", "minVersion": "0.3.0" },
  "contributes": {
    "skills":       [{ "dir": "skills/acme-billing-enquiry", "name": "acme-billing-enquiry" }],
    "instructions": [{ "file": "instructions/acme-conventions.instructions.md" }],
    "tasks":        [{ "file": "tasks/check-balance.json" }]
  }
}
```

Contributed tasks are [Guided Business Tasks](business-tasks.md) and go
through the same validation as one recorded in the browser.

Install by unzipping into `extensions/` and restarting. Disable one without
deleting it by listing its name in `extensions/.disabled`, one per line.

`list_extensions` shows every pack found, whether it is enabled, and **why one
failed to load** — which is the answer when a skill someone expects is missing
from `list_skills`.

## What an extension may contribute

Content only: skills, instructions and task documents.

There is deliberately no way for an extension to register a command, a script,
or an executable tool. 3270Web holds an authenticated session against
someone's mainframe, and "drop a folder in and it runs" is not a security
model an operator can reason about. Everything an extension needs is already a
built-in tool it can direct the assistant to use.

Treat an extension as you would any other trusted local content: there is no
signing, so install ones you or your team wrote.

## Rules the loader applies

- Every contributed path must resolve **inside** the extension folder.
- A skill's frontmatter `name` must match what the manifest declares.
- `schemaVersion` must be `1`; `requires.minVersion` must not be newer than
  the running version.
- A pack that fails any of these is **skipped and reported**, never
  half-loaded. One malformed manifest does not stop 3270Web starting.
