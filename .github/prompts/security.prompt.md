# Security Review / Fix Prompt

## When to use
- Adding a new HTTP endpoint that accepts user input or performs state mutation.
- Modifying input-handling code in `internal/host/s3270.go` (key commands, string injection).
- Reviewing CSRF middleware or Origin/Referer validation logic.
- Auditing log-access gating or any new "gated by env var" pattern.

## Inputs
- **Goal:** <goal>
- **Scope:** <scope> (e.g., a new handler in `cmd/3270Web/main.go`, `internal/host/s3270.go`)
- **Files:** <files>
- **Constraints:** <constraints> (e.g., must not break existing tests, must not change the public API)

## Prompt

Follow the conventions in `.github/copilot-instructions.md` and `.github/instructions/backend.instructions.md`.

Perform a security review / implement the security fix for: **<goal>**.

Checklist to verify before producing a diff:

1. **Injection prevention** — does any new code pass user-supplied strings to s3270 without going through the control-character rejection in `internal/host/s3270.go`? If so, add the check.
2. **CSRF** — do all new state-mutating endpoints pass through the `originRefererCheck` middleware? Verify the route is registered inside the middleware group.
3. **Log access** — do any new sensitive endpoints check `ALLOW_LOG_ACCESS` (or an equivalent env-var gate)?
4. **Secrets** — are credentials, tokens, or session IDs ever logged or returned in error responses?
5. **Path traversal** — if any new endpoint reads/writes files, is the path validated against the expected base directory?
6. **Input validation** — are all query/body parameters validated (length, character set) before use?

Produce:
1. A summary of findings (one bullet per issue found).
2. A minimal diff that fixes any confirmed issues.
3. Any new test cases that should be added to `*_test.go` to prevent regression.

## Done looks like
- [ ] No user input reaches s3270 without control-character validation
- [ ] All new POST/PUT/DELETE routes are inside the CSRF middleware group
- [ ] Sensitive endpoints return `403` when the gate env var is unset
- [ ] No secrets appear in log output or JSON error bodies
- [ ] File-path operations use `filepath.Clean` / base-directory checks
- [ ] `go test ./...` passes including any new security tests
- [ ] `go vet ./...` reports no new issues
