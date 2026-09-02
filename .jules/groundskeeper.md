# Groundskeeper journal — critical learnings for this repo

## 2026-09-02 - Duplicate `.jules` / `.Jules` agent-journal directories
**Finding:** The repo carried two directories differing only in case — `.jules/bolt.md` and
`.Jules/{palette,refactor,scribe,sentinel,testsmith}.md` — both created in the same merge commit
(PR #280, "3270web-multiple-sessions"). No filenames overlapped, so no content was lost merging
`.Jules/*` into `.jules/`.
**Learning:** This is the same case-insensitive-filesystem trap documented in the sub12 repo's
CLAUDE.md: two paths differing only by case cannot both be checked out on macOS/Windows default
filesystems, which leaves a contributor with a permanently dirty working tree. `.jules/` (lowercase)
is the canonical name here — it's what this session's own instructions and `.gitignore`
(`.jules/sentinel.md`) already assumed, even though that gitignore line previously pointed at a
file that didn't exist yet at that path (it lived under `.Jules/` before this merge).
**Prevention:** Never create a dot-directory whose name differs from an existing one only by case.
If a new agent journal is needed, add it under the existing `.jules/`.
