# 3270Web AI Instructions

## Big picture
- Entry point and HTTP server live in cmd/3270Web/main.go (Gin routes + handlers).
- Core runtime pieces are split by concern:
  - internal/host: s3270 subprocess wrapper and screen parsing (host.S3270 in internal/host/s3270.go).
  - internal/session: session state, preferences, and workflow recording/playback structs (internal/session/session.go).
  - internal/render: HTML renderer that turns 3270 fields into input elements (internal/render/html_renderer.go).
  - internal/config: XML config parsing + defaults for s3270 settings (internal/config/config.go).
- UI is server-rendered templates in web/templates and JS/CSS in web/static; most runtime behavior is driven by client JS calling JSON endpoints.

## Configuration and runtime data flow
- Startup loads webapp/WEB-INF/3270Web-config.xml; if missing it falls back to defaults (see internal/config/config.go).
- A .env is created on first run and loaded at startup; S3270_* values override XML config and map directly to s3270 CLI flags (docs/configuration.md).
- Logging endpoints are gated by ALLOW_LOG_ACCESS; log content is read from the startup log file at repo root (see cmd/3270Web/main.go Logs* handlers).
- Workflow recording outputs a 3270Connect-compatible JSON (see docs/workflow.md). Playback is currently disabled per README/docs.

## Developer workflows
- Run locally: go run ./cmd/3270Web (README.md).
- Build Windows EXE: scripts/build-windows.ps1 (outputs 3270Web.exe at repo root).
- Docker image: Dockerfile + docker build -t 3270web . (README.md). The image installs s3270 at /usr/bin/s3270.
- Tests are standard Go tests located under cmd/3270Web and internal/* (use go test ./...).

## Project-specific patterns and conventions
- Session state is always guarded by withSessionLock(...) or Session.Lock/Unlock (see cmd/3270Web/main.go and internal/session/session.go).
- Host commands guard against injection by rejecting control characters in key commands (internal/host/s3270.go).
- HTML rendering for 3270 fields is done in Go (not in templates) to preserve field coordinates and input sizing (internal/render/html_renderer.go).
- Client-side log UI lives in web/static/logs.js and expects JSON endpoints (/logs, /logs/toggle, /logs/clear, /logs/download, /logs/access).

## Files to reference when changing behavior
- cmd/3270Web/main.go: routes, handlers, and server setup.
- internal/host/s3270.go: s3270 lifecycle, command execution, screen updates.
- internal/session/session.go: session data model and workflow structs.
- web/templates/screen.html + web/static/*.js: UI wiring and modals.
- docs/configuration.md and docs/workflow.md: configuration and workflow format details.
