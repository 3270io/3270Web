## 2026-10-09 - Atoi is not a strict number check
**Finding:** `normalizeKey` passed the digits after PF/PA/F through `strconv.Atoi`, which accepts a leading sign, so "PF+3" and "PF(+3)" were pressed as PF(3) despite the rule that unrecognised keys are rejected.
**Learning:** The codebase's pure parsers are well tested for range but not for lenient stdlib parsing; `Atoi`/`ParseInt` accept "+" (and `Sscanf` accepts more still).
**Prevention:** Check that a numeric body is digits only before converting when the input names something (keys, ids), and add a signed case to the table test.

## 2026-10-23 - The same lenient parse had a second copy
**Finding:** The signed-number fix for `normalizeKey` did not cover `workflowKeyForStepType`, which parses "PressPF+3" / "PressPA+1" with `Atoi` and played them back as PF(3) / PA(1).
**Learning:** A parser fix lands in one function while its inverse or sibling keeps the old shape; the pair (key to step, step to key) drifts apart.
**Prevention:** When fixing a lenient parse, grep for every `Atoi`/`ParseInt` on the same kind of name and fix or test them together.

## 2026-11-13 - A third copy of the signed-number key parse
**Finding:** `normalizeChaosKeyName` in internal/chaos passed the digits after PF/PA/F through `strconv.Atoi`, so "PF+3", "PF(+3)" and "Press PF+3" were canonicalised to PF(3), the same defect fixed earlier in cmd/3270Web/keys.go.
**Learning:** The earlier fixes were scoped to one package; internal/chaos has its own key normaliser with the same lenient shape.
**Prevention:** When fixing a lenient parse, grep the whole module (`internal/` as well as `cmd/`) for `Atoi` on key names; `aidKeyToStepType` still trusts PF(...) inner text unvalidated.
