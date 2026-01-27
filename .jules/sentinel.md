## 2026-01-26 - Command Injection via S3270 Pipe
**Vulnerability:** The `SendKey` method in `internal/host/s3270.go` accepted raw strings which were sent directly to the `s3270` process pipe. If a key string contained a newline (`\n`), `s3270` would interpret the subsequent line as a new command, allowing injection of arbitrary s3270 commands (like `Quit`, `Script`, or potentially `Exec` if enabled).
**Learning:** Even when wrapping an external process, the communication channel (pipe) can be a vector for injection if the protocol (newline-delimited) is not respected by the wrapper. Defense in depth requires validation at the wrapper level (`S3270` struct) rather than relying solely on upstream callers (`cmd/3270Web`).
**Prevention:** Always validate inputs that are written to an external process's stdin to ensure they do not contain protocol delimiters (newlines, null bytes, etc.).

## 2026-01-26 - Content Security Policy (CSP) and Inline Scripts
**Vulnerability:** The application was missing standard security headers, including CSP. Adding a strict CSP (`script-src 'self'`) broke the application because `web/templates/connect.html` relies on inline scripts (`<script>...</script>`) for initialization.
**Learning:** Retrofitting strict CSP into an existing application often requires refactoring inline scripts into external files or implementing nonces. In constrained environments (time/scope), allowing `'unsafe-inline'` is a necessary trade-off to enable other protections (like `frame-ancestors` or `object-src`) without breaking functionality.
**Prevention:** When designing new views, avoid inline scripts and styles. Use external files to allow for stricter CSP rules later.
