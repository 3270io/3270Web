## 2026-10-09 - Atoi is not a strict number check
**Finding:** `normalizeKey` passed the digits after PF/PA/F through `strconv.Atoi`, which accepts a leading sign, so "PF+3" and "PF(+3)" were pressed as PF(3) despite the rule that unrecognised keys are rejected.
**Learning:** The codebase's pure parsers are well tested for range but not for lenient stdlib parsing; `Atoi`/`ParseInt` accept "+" (and `Sscanf` accepts more still).
**Prevention:** Check that a numeric body is digits only before converting when the input names something (keys, ids), and add a signed case to the table test.
