# Chaos Monkey Skill

## Purpose

Run the automated 3270 chaos monkey to fully discover application screens, input variations, and key press paths. Produce a 3270Connect-compatible workflow JSON and a discovery report. Works in full auto mode or guided mode with user decisions at key checkpoints.

## Trigger phrases

Invoke this skill when the user says any of:
- "run chaos monkey"
- "explore the app" / "discover all screens"
- "chaos mode"
- "map out the application"
- "automated exploration"

## Phases

### Phase 1 — Read & Review

1. Call `get_screen` to see the current screen. Note all visible PF-key labels (especially Exit, Logout, Sign Off, Cancel, Quit).
2. Call `chaos_get_hints` to check existing configuration:
   - Are there already transaction codes? Key blacklists? Per-screen hints?
   - If hints are comprehensive, you may skip Phase 2 configuration and go straight to Phase 3.

### Phase 2 — Setup

3. Call `ask_user` to choose the run mode:
   - Question: "How would you like to run the chaos monkey?"
   - Options:
     - "Full Auto — run, monitor, then export automatically"
     - "Guided — ask me at each key decision"

4. If dangerous keys were detected (Exit/Logout labels), call `ask_user`:
   - Question: "I found keys that might exit or log out. How should I handle them?"
   - Options: "Block all exit/logout keys (recommended)", "Let me review each one", "Allow all keys"

5. Apply the key blacklist via `chaos_update_hints` with the agreed keys, or if no global blacklist changes are needed, use `chaos_save_screen_hint` with `blocked_keys` on the current screen hash.

6. If the user wants to configure transaction codes first, call `ask_user`:
   - Question: "Do you have transaction codes to seed? (e.g. MENU, ACCT, INQ)"
   - Options: "Yes, I'll tell you the codes", "No, let chaos discover them", "Use existing hints"
   - If yes, ask the user to type the codes in the next message, then call `chaos_update_hints`.

### Phase 3 — Run

7. Call `chaos_start`. Recommended defaults:
   - Guided mode: `max_steps=100`, `time_budget_sec=300`
   - Full auto: `max_steps=200`, `time_budget_sec=600`
   - `step_delay_sec=0.5` (give the host time to respond)

8. Inform the user: "Chaos monkey is running. I'll check back when it finishes (or you can ask me for a status update at any time)."

9. **In guided mode:** After starting, periodically call `chaos_status` to report progress:
   - Call `chaos_status` once ~10 seconds after start to confirm it's running.
   - While active: summarise screens discovered, transitions found, steps completed.
   - If chaos stops early (saturation), proceed to Phase 4 immediately.

10. **In full auto mode:** Wait for the run to finish. Detect completion by calling `chaos_status` and checking `active: false`. Retry status check with a brief pause between calls — do not spam the endpoint; check every ~15 steps.

### Phase 4 — Adapt

11. When chaos finishes, call `chaos_status(verbose=true)` to examine the full mind map.
    Analyse:
    - Screens with very few visits (under 3)
    - Screens where no transition was made (deadends)
    - Input fields where no successful write was recorded

12. Summarise the findings to the user: screens found, screens that need more exploration, any obvious gaps.

13. **In guided mode**, call `ask_user`:
    - Question: "Chaos has finished. What would you like to do next?"
    - Options:
      - "Update hints and run again (deepen discovery)"
      - "Export workflow JSON and generate report"
      - "Generate report only"
      - "I'm done — just export the workflow"

14. If re-running: update per-screen hints for deadend screens using `chaos_save_screen_hint` with better `known_keys` and `known_data`. Then go back to Phase 3.

### Phase 5 — Export & Report

15. Call `chaos_report` to generate the Markdown discovery report (screen graph, field/key statistics, saturation reason, next-experiment suggestions).

16. Call `chaos_export_workflow` to get the 3270Connect-compatible workflow JSON. Display a short summary: number of steps, unique screens, host/port.

17. If new dangerous keys or transaction codes were discovered during the run, call `chaos_update_hints` and/or `chaos_save_screen_hint` to persist the learnings for future runs.

18. In guided mode, call `ask_user` one final time:
    - Question: "All done! What would you like to save?"
    - Options:
      - "Save all hints (update chaos-hints.json)"
      - "Nothing — I'll review first"
      - "Run another pass with extended limits"

19. Present the final summary: screens discovered, workflow JSON length, report highlights.

## Hints management reference

| Tool | When to use |
|------|-------------|
| `chaos_get_hints` | Before starting — review what's already configured |
| `chaos_update_hints` | Update global transaction codes or add global key blacklist entries |
| `chaos_save_screen_hint` | Set per-screen blocked_keys, known_keys, or known_data after identifying a specific screen hash |
| `chaos_report` | After run completes — get Markdown discovery report |
| `chaos_export_workflow` | After run completes — get 3270Connect workflow JSON |

## Edge cases

- **Already running:** If `chaos_status` returns `active: true`, tell the user and offer: "Stop current run", "Wait for it to finish", "Check status".
- **Not connected:** If `chaos_start` returns "not connected to host", tell the user to connect first.
- **No screens found (0 unique screens):** The first screen may need a hint. Ask the user for the transaction code or ask them to navigate to a working screen before retrying.
- **User types transaction codes:** Parse them as a space- or comma-separated list, then call `chaos_update_hints` with each as a separate hint object.

## Done looks like

- [ ] At least 2 unique screens discovered (for non-trivial apps: many more)
- [ ] Workflow JSON exported with Steps array populated
- [ ] Key blacklist updated with any logout/exit keys found
- [ ] Hints saved for at least the first screen
- [ ] Chaos report generated and shown to user
