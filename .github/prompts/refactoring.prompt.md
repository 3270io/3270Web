# Refactoring Prompt

## When to use
- Extracting repeated handler logic into a shared helper on `*App`.
- Splitting an overgrown file in `cmd/3270Web/main.go` into focused files in the same package.
- Moving business logic that has leaked into a handler back into the appropriate `internal/` package.
- Improving naming or reducing cyclomatic complexity without changing behaviour.

## Inputs
- **Goal:** <goal>
- **Scope:** <scope> (e.g., `cmd/3270Web/main.go`, `internal/session/session.go`)
- **Files:** <files>
- **Constraints:** <constraints> (e.g., must not change any public API, must not break existing tests)

## Prompt

Follow the conventions in `.github/copilot-instructions.md` and `.github/instructions/backend.instructions.md`.

Refactor the code in <files> to achieve: **<goal>**.

Rules:
- **No behaviour changes.** All existing tests must pass unchanged after the refactor.
- **No new dependencies.** Use only packages already in `go.mod`.
- Keep all session-locking patterns intact (`withSessionLock` / `sess.Lock/Unlock`).
- New helpers must be methods on `*App` if they need app state, or free functions in the same package if they are pure.
- If splitting `main.go`, new files must be in `package main` under `cmd/3270Web/` and named `<concern>.go` (e.g., `workflow.go`, `logs.go`).
- Do not move types or functions across package boundaries unless the goal explicitly requires it.
- If an internal package is the target, ensure its exported surface is unchanged (no removed/renamed exports).

Produce a minimal diff. If the refactor touches > 3 files, include a one-sentence summary of each change.

## Done looks like
- [ ] `go test ./...` passes with no test modifications
- [ ] No public API signatures changed (exports unchanged)
- [ ] No new libraries or packages introduced
- [ ] Session locking patterns preserved
- [ ] `go vet ./...` reports no new issues
- [ ] Diff size is proportionate — no unrelated cosmetic changes
- [ ] Extracted helper has its own test or is covered by existing tests
