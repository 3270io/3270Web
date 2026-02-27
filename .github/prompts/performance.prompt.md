# Performance Review Prompt

## When to use
- A handler or host command is noticeably slow or makes repeated subprocess calls.
- Benchmarks in `internal/render/benchmark_test.go` regress.
- Screen-parsing or field-rendering takes measurable wall time on large 3270 screens.
- The chaos engine or workflow playback loop consumes excessive CPU.

## Inputs
- **Goal:** <goal>
- **Scope:** <scope> (e.g., `internal/render`, `internal/host`, a specific handler in `cmd/3270Web/main.go`)
- **Files:** <files>
- **Constraints:** <constraints> (e.g., must not change the public API, must stay within existing dependencies)

## Prompt

Follow the conventions in `.github/copilot-instructions.md` and `.github/instructions/backend.instructions.md`.

Analyse the code in <files> for performance issues in the context of: **<goal>**.

Rules:
- Do NOT introduce new libraries. Use only packages already in `go.mod`.
- Do NOT change public function signatures unless the scope explicitly allows it.
- Preserve all session-locking patterns (`withSessionLock` / `sess.Lock/Unlock`).
- Prefer pre-allocating slices/maps with known capacity over repeated `append` in hot paths.
- Avoid holding the session lock across s3270 subprocess I/O.
- For `internal/render`, measure with the existing benchmark in `internal/render/benchmark_test.go` before and after.

Produce:
1. A brief diagnosis of the bottleneck (2–4 sentences).
2. A minimal diff that addresses it.
3. Any benchmark command to verify the improvement.

## Done looks like
- [ ] `go test ./...` passes with no regressions
- [ ] Benchmark command shows measurable improvement (`go test -bench=. ./internal/render/...`)
- [ ] No new third-party packages introduced
- [ ] Session locking rules unchanged
- [ ] Diff is minimal — only lines directly relevant to the performance fix
- [ ] `go vet ./...` reports no new issues
