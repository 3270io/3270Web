# 3270Web Copilot Instructions

## Architecture

- **Entry point / HTTP server:** `cmd/3270Web/main.go` — Gin routes and handlers. All new routes go here.
- **Internal packages** (one concern each):
  - `internal/host` — s3270 subprocess lifecycle, command execution, screen parsing (`host.S3270`)
  - `internal/session` — session state, preferences, workflow recording structs (`session.Manager`, `session.Session`)
  - `internal/render` — Go-side HTML renderer that converts 3270 fields into `<input>` elements (`render.Renderer`)
  - `internal/config` — XML config parsing and s3270 CLI-flag defaults
  - `internal/chaos` — chaos-mode engine and persistence
  - `internal/sampleapps` — built-in demo 3270 applications
- **UI layer:** server-rendered Go templates in `web/templates/`; JS/CSS in `web/static/`. Runtime behaviour is driven by client JS calling JSON endpoints. Do **not** move field rendering into templates.
- **Module:** `github.com/jnnngs/3270Web` — Go 1.22+, Gin HTTP framework.

## Commands

```bash
# Run locally
go run ./cmd/3270Web

# Run all tests
go test ./...

# Run a single package's tests verbosely
go test -v ./internal/host/...

# Build Docker image
docker build -t 3270web .

# Build Windows EXE (PowerShell)
./scripts/build-windows.ps1
```

No Makefile exists. No golangci-lint config exists; standard `go vet ./...` is the available static checker.

## Coding Style

- Follow standard Go idioms (`gofmt`, short variable names, table-driven tests).
- Return errors; do not panic outside of `main` startup.
- All handler functions must be methods on `*App` and accept `*gin.Context`.
- Use `c.JSON(statusCode, gin.H{...})` for JSON responses; `c.HTML(...)` for HTML.
- Do not introduce new third-party libraries unless strictly necessary.

## Session State & Concurrency

- **Always** wrap session mutations in `withSessionLock(sess, func() { ... })` or explicit `sess.Lock()` / `sess.Unlock()`.
- Never hold a session lock across a network or subprocess call.
- Use `session.Manager.CreateSession` / `GetSession` — do not instantiate sessions directly.

## Error Handling

- HTTP handlers return structured JSON errors: `c.JSON(http.StatusXxx, gin.H{"error": "..."})`.
- Propagate errors up; log with the standard `log` package (no structured-logging library is used).
- Host/subprocess errors from `internal/host` are wrapped with `fmt.Errorf("context: %w", err)`.

## Security

- **Injection prevention:** `internal/host` rejects any key command containing control characters (`\n`, `\r`, `\t`, `;`). Do not bypass this check.
- **CSRF:** The `originRefererCheck` middleware validates `Origin`/`Referer` headers against the request host for all non-safe HTTP methods. New state-mutating endpoints must pass through this middleware.
- **Log access:** All `/logs*` endpoints check `ALLOW_LOG_ACCESS` env var and return `403` when unset. Follow this pattern for any other sensitive endpoints.
- **Secrets:** Never commit secrets. Use the `.env` file (git-ignored) or environment variables.

## Tests

- Test files live alongside the code they test (`*_test.go`, same package).
- Use `gin.SetMode(gin.TestMode)`, `httptest.NewRecorder()`, and `httptest.NewRequest()` for HTTP handler tests.
- Use `host.NewMockHost("")` to avoid spawning real s3270 in tests.
- Prefer table-driven tests (`tests := []struct{...}{ ... }`).
- Run `go test ./...` before pushing; all tests must pass.

## Configuration

- Runtime config priority: `.env` > `webapp/WEB-INF/3270Web-config.xml` > built-in defaults.
- `S3270_*` env vars map directly to s3270 CLI flags — see `internal/config/s3270_env.go`.
- Do not hard-code host/port values; read from `config.Config`.

## Documentation

- User-facing docs live in `docs/` and are built with MkDocs (`mkdocs.yml`).
- Update `docs/configuration.md` when adding new env vars or config keys.
- Update `docs/workflow.md` when changing the workflow JSON schema.
- Keep `README.md` in sync with any new run/build commands.

## CI / Workflows

- `.github/workflows/docker-publish.yml` — builds and pushes to GHCR on push to `main`.
- `.github/workflows/docs-gh-pages.yml` — publishes MkDocs site to GitHub Pages.
- Both workflows require changes to pass `go test ./...` locally before merging.

## Key Files to Reference

| Change area | File(s) |
|---|---|
| Routes & handlers | `cmd/3270Web/main.go` |
| s3270 lifecycle / commands | `internal/host/s3270.go` |
| Session data model | `internal/session/session.go` |
| Field rendering | `internal/render/html_renderer.go` |
| Config loading | `internal/config/config.go`, `internal/config/s3270_env.go` |
| UI templates | `web/templates/screen.html` |
| Client JS | `web/static/*.js` |
| Docs | `docs/configuration.md`, `docs/workflow.md` |
