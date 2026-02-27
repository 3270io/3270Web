# Write / Update Tests Prompt

## When to use
- Adding tests for a new handler, middleware, or internal package function.
- Improving coverage for an existing untested code path.
- Fixing a flaky test or a test that spawns a real s3270 process.
- Adding a benchmark for screen parsing or HTML rendering.

## Inputs
- **Goal:** <goal>
- **Scope:** <scope> (e.g., `cmd/3270Web/security_test.go`, `internal/host/s3270_test.go`)
- **Files:** <files>
- **Constraints:** <constraints> (e.g., must not spawn a real s3270 binary, must follow table-driven style)

## Prompt

Follow the conventions in `.github/copilot-instructions.md` and `.github/instructions/backend.instructions.md`.

Write or update tests for: **<goal>**.

Rules:
- Test files live alongside the code they test (`*_test.go`, same package as production code).
- Use `gin.SetMode(gin.TestMode)`, `httptest.NewRecorder()`, and `httptest.NewRequest()` for HTTP handler tests.
- Use `host.NewMockHost("")` to avoid spawning a real s3270 subprocess.
- Prefer table-driven tests: `tests := []struct{ name string; ... }{ {...}, {...} }` with `t.Run(tt.name, ...)`.
- Do not add new third-party test libraries. Use only `testing`, `net/http/httptest`, `encoding/json`, and existing helpers already imported in the package.
- Each test must have a meaningful name that describes the scenario (`TestHandlerName_WhenCondition_ExpectedBehaviour`).
- For security-related tests, also assert the response body contains the expected error message.

Produce a diff adding only the new/updated `*_test.go` content.

## Done looks like
- [ ] `go test ./...` passes with the new tests included
- [ ] No real s3270 binary is spawned (uses `host.NewMockHost` or similar)
- [ ] Table-driven format used where ≥2 cases share the same logic
- [ ] Test names follow `TestHandlerName_WhenCondition_ExpectedBehaviour`
- [ ] No new third-party test libraries introduced
- [ ] `go vet ./...` reports no new issues
- [ ] Coverage for the target code path is improved (verify with `go test -cover`)
