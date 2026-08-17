# CLAUDE.md — 3270Web

## Writing for a public repository

This repository is open source. Everything in it is publicly readable —
code comments, docs (which publish to 3270Web.3270.io), commit messages,
PR titles and bodies, and issue comments.

**Outside the one section named below, do not name commercial terminal
emulators, and do not frame work as matching, catching up to, or copying
one.** No "*product* ships this and we don't", no "closes the gap against
*product*". This applies to commit messages and PR descriptions exactly as
much as to code and docs.

Write the same point in terms of the user and the category instead:

- ✅ "Operators expect this as standard terminal behaviour."
- ✅ "A capability an enterprise 3270 terminal is generally expected to have."
- ✅ "Years of muscle memory from another terminal emulator."
- ❌ Naming two rival products and saying both ship the feature.
- ❌ Calling something "the most-cited gap against *vendor*".

**The one exception** is `## The field, and where 3270Web sits in it` in
`docs/feature-roadmap.md`, which compares 3270Web with named products
because a reader deciding between them deserves the comparison in one
place. Rules that section keeps, and that any edit to it must keep:

- **The set of named products is fixed.** Do not add a vendor to that
  table, and do not name one anywhere else on the site, without being asked
  to by name.
- **Only verifiable, stable facts** — delivery model, platform, licence
  model, published automation surface. No feature-by-feature tick grid
  against another vendor: a claim about what a competitor *lacks* is the
  one most likely to be wrong and the one that reads worst.
- **Neutral and current.** Say what each product is for, not what is wrong
  with it. Keep the "checked in *month year*" line honest, and keep the
  "where the field is ahead" subsection — a comparison that only flatters
  the home team is not worth reading.
- Comparison against the *category* ("where the category generally sits")
  is preferred anywhere a specific vendor is not required.

Naming a **file format or protocol** for interoperability is fine and
sometimes unavoidable — a `.KMP` keymap importer has to say which format it
reads, and IND$FILE and TN3270 are protocol names. State the format, never
the vendor whose product writes it.

## Licence

3270Web is **AGPL-3.0-or-later**, with commercial terms available. New Go
files carry `// SPDX-License-Identifier: AGPL-3.0-or-later` as their first
line. `LICENSING.md` is the reference; `docs/licence.md` is the user-facing
version; `CONTRIBUTING.md` sets the terms contributions arrive under (DCO
sign-off plus the additional grant that keeps dual-licensing possible).

**The boundary with 3270Connect is load-bearing.** 3270Connect is MIT and
stays MIT, and the two repositories share
`internal/{authz,authsession,users,apitoken,audit,oidc,reqsec,agent,profiler}`
by hand-copying. Code may be copied **from 3270Connect into here, never from
here into 3270Connect** unless the copyright holder wrote it. Write a shared
change in 3270Connect first and copy it across.

s3270 is unaffected by any of this: separate process, aggregated not combined,
still BSD 3-Clause. Do not describe it as AGPL, and do not fold it into
3270Web's Corresponding Source.

## Project Overview

**3270Web** is a Go web application that provides a browser-based IBM 3270 mainframe terminal interface. It wraps the `s3270` binary and exposes a REST/JSON API alongside a full-featured HTML/JS UI for interactive sessions, workflow recording/replay, automated chaos exploration, and GitHub Copilot integration.

## Tech Stack

- **Backend:** Go 1.22+ (targeting 1.25), Gin 1.11.0, go3270 v0.9.13
- **Frontend:** Vanilla JS (21 modules), custom 3270 fonts, Tippy.js/Popper.js
- **3270 Runtime:** `s3270` binary (embedded on Windows, apt-installed in Docker)
- **Docs:** MkDocs → GitHub Pages at 3270Web.3270.io
- **CI/CD:** GitHub Actions → GHCR Docker, gh-pages

## Build & Run

```bash
# Run locally
go run ./cmd/3270Web
# → http://localhost:3270

# Run tests
go test ./...
go test -v -cover ./...

# Docker
docker build -t 3270web .
docker-compose up          # dev: maps to localhost:3270

# The lab: the bundled sample apps as a TN3270 host, this terminal, and
# 3270Connect's operations console, on one network
docker compose -f docker-compose.lab.yml up -d

# The sample apps served as an ordinary host, for anything outside this
# process (cmd/3270Web/sampleapphost.go)
go run ./cmd/3270Web sampleapp --app petstore:3271 --app app1:3272

# Windows .exe (PowerShell)
.\scripts\build-windows.ps1
```

## Repository Layout

```
cmd/3270Web/          # Main package: HTTP handlers, routing, chaos, copilot
internal/
  assets/             # Embedded web assets (bindata.go)
  chaos/              # Chaos engine, config, mindmap, persistence, reports
  config/             # XML config loading, s3270 env vars
  copilot/            # GitHub Copilot OAuth + API client + handlers
  host/               # s3270 process lifecycle, screen parsing, status
  render/             # Renderer interface (HTML output abstraction)
  session/            # Session state, workflow replay structures
  sampleapps/         # Local Go-based 3270 test servers
web/
  static/             # JS modules, fonts, images, tooltip libs
  templates/          # connect.html, screen.html (main UI), error.html
webapp/WEB-INF/       # Optional 3270Web-config.xml loaded at startup
docs/                 # MkDocs source (chaos-mode, config, rest-api, workflow)
scripts/              # build-windows.ps1, capture-doc-screenshots.mjs
.github/workflows/    # docker-publish.yml, docs-gh-pages.yml
```

## Core Architecture

**App struct** (cmd/3270Web/main.go):
```go
type App struct {
    SessionManager *session.Manager
    Renderer       render.Renderer
    Config         *config.Config
    chaosEngines   *chaosEngineStore
    // ...
}
```

**Request flow:**
```
Browser (web/static/*.js + web/templates/)
  → Gin router (cmd/3270Web/main.go)
  → App handlers (Connect, Screen, Submit, Chaos, Copilot…)
  → internal/session.Manager
  → internal/host.Host (s3270 subprocess)
  → 3270 mainframe host
```

**Key files:**
- `cmd/3270Web/main.go` — routing, all HTTP handlers
- `cmd/3270Web/api_v1.go` — REST API (`/api/v1/*`)
- `internal/host/s3270.go` — s3270 binary protocol
- `internal/chaos/engine.go` — chaos exploration logic
- `web/templates/screen.html` — main terminal UI (66 KB)
- `internal/config/config.go` — XML config + env var loading

**Middleware:**
- `SecurityHeadersMiddleware()` — CSP, X-Frame-Options, X-Content-Type-Options
- `OriginRefererCheckMiddleware()` — validates request origin
- CSRF token validation (`csrf.go`)
- Input validation (`validator.go`)

## API

REST endpoints live under `/api/v1/`. See `docs/rest-api.md` for full reference.

## Testing

30 test files across packages. No Makefile; use `go test` directly:

```bash
go test ./...                      # all packages
go test ./cmd/3270Web/...          # cmd package only
go test ./internal/chaos/...       # chaos package
```

Notable test files: `chaos_test.go` (68 KB), `resiliency_test.go`, `security_test.go`, `workflow_playback_test.go`.

## Skills & Validation

- Use `playwright` skill for browser/UI flow validation (web/, webapp/)
- Use `screenshot` skill for desktop app (3270Web.exe) captures → output/ dir
- Use `security-review` skill only for explicit security review requests
- Do **not** use the `doc` skill for MkDocs files (docs/, mkdocs.yml, site/)

## Environment Notes (Windows)

- `python`, `node`, `npm` available
- `gh` CLI not installed — install with: `winget install --id GitHub.cli`
- `go test ./...` is the canonical test command
- Frontend changes: start the server, validate in browser before marking done

## CI Workflows

- `docker-publish.yml` — builds Docker image, pushes to GHCR on push to main
- `docs-gh-pages.yml` — deploys MkDocs site to gh-pages branch
- Both run on self-hosted runner; docker-publish requires `PAT_TOKEN` secret
