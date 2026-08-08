---
name: chaos-monkey
description: Explore an unknown 3270 application automatically, mapping its screens and transitions, then report what was found and what to try next.
invocation: [chaos, explore-app, discover-screens]
tools: [get_screen, chaos_get_hints, chaos_update_hints, chaos_save_screen_hint, chaos_start, chaos_status, chaos_insights, chaos_report, chaos_export_workflow]
instructions: [untrusted-host-data, aid-key-safety]
---

# Chaos exploration

Drive an application you have not seen before until you have a map of it:
which screens exist, which keys move between them, and which fields accept
what.

## When to use

The user asks you to run chaos monkey, explore the application, discover
screens, or map a host they cannot describe.

## Phase 1 — read and review

1. `get_screen` to see where the session currently is.
2. `chaos_get_hints` for what previous runs already learned: transaction
   codes, known-good values, the key blacklist, per-screen hints.
3. Read the screen for keys that would end the session — labels like *Exit*,
   *Logout*, *Sign Off*, *Return to CICS* — and note their PF numbers.

## Phase 2 — set up

4. Ask the user which mode they want: full auto (run, monitor and export
   without stopping), or guided (check in at each decision).
5. If you found session-ending keys, ask whether to block them. Offer the
   list you found, plus "block all" and "block none".
6. Apply the answer with `chaos_update_hints` for whole-run settings or
   `chaos_save_screen_hint` for a single screen.

Do not skip step 6. Exploration presses keys it has not been told the meaning
of, and the one on a menu that reads *PF3 — Exit* will end the session and
end the run with it. `chaos_start` refuses to begin without a blacklist for
this reason; see `aid-key-safety.instructions.md`.

## Phase 3 — run

7. `chaos_start`. In guided mode, poll `chaos_status` every 20 steps or so and
   tell the user what is happening. In full auto, set `max_steps` to 200 and
   let it finish, then check status.

## Phase 4 — adapt

8. When it stops, call `chaos_insights` for ranked next experiments and the
   termination diagnostics, then `chaos_status` with `verbose=true` if you
   need the whole mind map. `terminationReason` says why it stopped:

   - `max_steps` or `time_budget` — it ran out of budget. Offer to resume
     with more if coverage looks thin.
   - `saturated` — it stopped finding new screens. If `saturatedNoProgress`
     is also true it found *no* transitions at all: resuming will only
     re-saturate. Add hints, or navigate manually to somewhere more
     interesting, then resume.
   - `blocked` — every usable key was blacklisted for a screen. Relax the
     blacklist or add a per-screen hint naming the right key.
   - `error` — a host failure stopped it. Report the error; resuming may
     work if it was transient.

9. Choose hints from `suggestedExperiments`, `deadKeys` and
   `unproductiveFields` rather than guessing. Look for screens with low visit
   counts or no productive transitions.
10. In guided mode, ask what to do next with options that match the
    termination reason. Never resume the same run more than twice without
    changing hints — if nothing new is being found, say so and stop.

## Phase 5 — export and report

11. `chaos_report` for the discovery report.
12. `chaos_export_workflow` for workflow JSON that replays what was found.
13. Persist anything learned — new transaction codes, newly discovered
    dangerous keys — with `chaos_update_hints` and `chaos_save_screen_hint`,
    so the next run starts where this one finished.
14. Run the `business-understanding` skill, so the discoveries are recorded
    with their meaning and not just their coordinates.

## Anti-patterns

- Starting a run without reviewing the key blacklist.
- Resuming a saturated run unchanged, repeatedly.
- Reporting screen counts as progress. A run that found 40 screens and no
  business function found less than one that found 6 and a working account
  enquiry.
