## 2025-02-18 - Missing Concurrency Tests in Session Manager
Learning: The session manager uses locks but had no tests verifying thread safety, leaving it vulnerable to subtle race conditions during refactors.
Risk: High. Race conditions in session management can lead to cross-session data leaks or panics in a concurrent web environment.
Action: Added `internal/session/session_test.go` with strict concurrency tests using `go test -race`.

## 2026-01-25 - Command Injection via Key Input
Learning: The `normalizeKey` function allowed inputs with newlines and other control characters to pass through, creating a command injection vulnerability where malicious users could execute arbitrary s3270 commands.
Risk: High. An attacker could bypass application logic or potentially gain control over the s3270 session.
Action: Added `cmd/3270Web/security_test.go` to test for injection, and patched `normalizeKey` in `main.go` to strictly reject keys containing control characters.

## 2026-02-18 - Untested Retry Logic in S3270 Host
Learning: The `isConnectionError` and `isS3270Error` functions, critical for process recovery and error reporting, were untested and relied on fragile string matching.
Risk: High. Failure to correctly identify connection errors could prevent the application from reconnecting to `s3270`, leading to denial of service for the user.
Action: Added comprehensive table-driven tests in `internal/host/s3270_test.go` to verify error classification logic.

## 2026-02-20 - Fragile S3270 Status Parsing
Learning: The `statusPattern` regex used to parse `s3270` output included a strict end-of-line anchor (`$`), which caused parsing to fail entirely if the status line format changed (e.g., adding new fields).
Risk: Moderate. Future versions of `s3270` or different configurations could inadvertently break screen rendering, leading to unformatted or duplicated screens.
Action: Added `internal/host/screen_parsing_test.go` to enforce parsing resiliency and removed the `$` anchor from the regex in `internal/host/screen_update.go`.

## 2026-02-24 - Untested HTML Escaping and Null Byte Handling
Learning: The `HtmlRenderer` uses a custom `writeEscaped` method for performance that manually handles HTML escaping and null-byte replacement. This logic was not explicitly tested, relying on indirect screen rendering tests that didn't cover all edge cases (like null bytes or mixed content).
Risk: High. Flaws in escaping logic are a primary vector for XSS attacks, and mishandling null bytes can cause rendering issues or undefined browser behavior.
Action: Added `TestWriteEscaped` in `internal/render/html_renderer_test.go` with comprehensive table-driven cases for HTML entities, null bytes, and unicode to ensure correctness and safety.

## 2026-03-05 - Duplicate Business Logic Hiding Bugs
Learning: Critical workflow logic (`applyWorkflowFill`) was duplicated in `main.go` and `workflow_playback.go`, with the `main.go` version containing an off-by-one error (1-based vs 0-based coordinates) that was active in tests but masked by weak test assertions.
Risk: High. Duplicate implementations drift apart, leading to behavior that passes tests but fails in production (or vice versa), and makes bug fixes unreliable.
Action: Removed duplicate code from `main.go` and added `TestApplyWorkflowFillCoordinates` to explicitly verify coordinate translation using a mock host with argument recording.
