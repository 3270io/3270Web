---
applyTo: "cmd/**/*.go,internal/**/*.go"
---

# Backend (Go) Conventions

## Package boundaries

- `cmd/3270Web` — HTTP handlers and route wiring only. Business logic belongs in `internal/`.
- `internal/host` — all communication with the s3270 subprocess. Never import `host` from `render` or `session`.
- `internal/session` — session data model and workflow structs. Import from handlers; do not import `render` here.
- `internal/render` — pure function: takes session/screen data, returns HTML string. No I/O side-effects.
- `internal/config` — loaded once at startup. Pass `*config.Config` down; do not re-read the file inside handlers.

## Session locking rules

```go
// Correct – always use the helper or explicit lock/unlock
withSessionLock(sess, func() {
    sess.SomeField = value
})

// Also acceptable for longer critical sections
sess.Lock()
defer sess.Unlock()
```

Never hold the lock across a subprocess call or network I/O.

## HTTP handler skeleton

```go
func (a *App) MyHandler(c *gin.Context) {
    sess, ok := a.getSession(c)
    if !ok {
        return // getSession already wrote 401
    }
    // ... mutate via withSessionLock
    c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
```

## Error responses

- Use `c.JSON(http.StatusBadRequest, gin.H{"error": "descriptive message"})` — no HTML for JSON endpoints.
- Wrap subprocess/IO errors: `fmt.Errorf("context: %w", err)`.
- Log unexpected errors with `log.Printf`; do not swallow them silently.

## Adding a new endpoint

1. Add the route in the router block in `cmd/3270Web/main.go`.
2. For state-mutating endpoints, ensure the `originRefererCheck` middleware is applied.
3. Guard sensitive endpoints the same way `/logs*` routes check `ALLOW_LOG_ACCESS`.
4. Add a corresponding `*_test.go` file using `httptest` and `host.NewMockHost`.

## Testing

- Package: same package as tested code (`package main` for cmd, `package host` for internal/host, etc.).
- Mock the host: `host.NewMockHost("")` to avoid spawning real s3270.
- Assert HTTP status codes and JSON response bodies; use `encoding/json` to decode responses.
- Run: `go test ./...`
