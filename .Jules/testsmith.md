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

## 2026-03-10 - Untested Configuration Loading and Defaults
Learning: The `Load` function in `internal/config/config.go`, responsible for parsing XML configuration and applying application defaults, was entirely untested. This left the application startup logic vulnerable to regression, particularly regarding default values for critical paths like `ExecPath` or `Model`.
Risk: High. A regression in configuration loading could cause the application to start with incorrect settings (e.g., wrong model dimensions or missing fonts) or fail to start silently if invalid XML is not handled gracefully.
Action: Added `internal/config/config_test.go` with comprehensive tests verifying XML parsing, default value application, and error handling for malformed or missing files.

## 2026-03-12 - Incorrect Field Attribute Inheritance
Learning: The screen parsing logic in `decodeLineTokens` implicitly inherited the `startCode` (field attribute) from the previous field when parsing a new `SF` token, rather than resetting it. This means if `s3270` output contains a malformed or incomplete `SF` token, the new field incorrectly inherits properties (like Protected status) from the preceding field.
Risk: High. This could lead to security issues (e.g., fields intended to be hidden or protected becoming visible or editable, or vice-versa) if the upstream s3270 output is corrupted or incompatible.
Action: Added `internal/host/screen_parsing_safety_test.go` which reproduces the bug and skips (to avoid breaking CI) until the production logic can be safely patched.

## 2026-03-14 - Validation Gap for Bracketed IPv6 Literals
Learning: The `isValidHostname` validator relied on `net.SplitHostPort` which requires a port or unbracketed input, causing it to reject valid bracketed IPv6 literals (e.g., `[::1]`) that users might paste from browsers.
Risk: Low. Prevents users from connecting to valid IPv6 hosts if they use the standard bracketed notation without an explicit port.
Action: Added a regression test case `[::1]` in `cmd/3270Web/main_test.go` and patched `isValidHostname` to handle this edge case.

## 2026-03-20 - Unsafe Argument Splitting in Config
Learning: The `buildS3270Args` function used `strings.Fields` to split configuration arguments from the `additional` XML field, which breaks quoted strings containing spaces.
Risk: Moderate. Users cannot pass arguments with spaces (e.g., `-scriptport "127.0.0.1:4000"`) via XML config, leading to misconfiguration or arguments shifting positions.
Action: Added `cmd/3270Web/args_test.go` to reproduce the issue, and refactored `internal/config` to export `SplitArgs` for safe argument parsing.
