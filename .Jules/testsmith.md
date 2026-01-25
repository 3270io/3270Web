## 2025-02-18 - Missing Concurrency Tests in Session Manager
Learning: The session manager uses locks but had no tests verifying thread safety, leaving it vulnerable to subtle race conditions during refactors.
Risk: High. Race conditions in session management can lead to cross-session data leaks or panics in a concurrent web environment.
Action: Added `internal/session/session_test.go` with strict concurrency tests using `go test -race`.

## 2026-01-25 - Command Injection via Key Input
Learning: The `normalizeKey` function allowed inputs with newlines and other control characters to pass through, creating a command injection vulnerability where malicious users could execute arbitrary s3270 commands.
Risk: High. An attacker could bypass application logic or potentially gain control over the s3270 session.
Action: Added `cmd/3270Web/security_test.go` to test for injection, and patched `normalizeKey` in `main.go` to strictly reject keys containing control characters.
