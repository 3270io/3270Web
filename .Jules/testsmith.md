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

## 2026-03-04 - Broken Shell Argument Splitting for Empty Quotes
Learning: The custom `splitArgs` function logic relied on buffer length to decide whether to append arguments, causing it to silently drop explicitly empty quoted strings (like `""` or `''`) from the parsed argument list. Existing tests only covered "happy paths" with non-empty content.
Risk: Moderate. Configuration overrides using `S3270_SET` or similar env vars could fail unpredictably if an empty value was intended (e.g. clearing a resource), potentially leading to misconfiguration or arguments shifting positions.
Action: Added comprehensive table-driven tests in `internal/config/s3270_env_test.go` covering edge cases like empty quotes and nesting, and patched `splitArgs` to track token state explicitly.
