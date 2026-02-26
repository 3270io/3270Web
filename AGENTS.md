# h3270 Repo Guidance

## Purpose

Repo-specific instructions so installed Codex skills are used consistently in `h3270`.

## Skills To Use In This Repo

- `playwright`: Use for browser/UI flow debugging, regressions in `web/` and `webapp/`, and validating interactions in the running app. Prefer CLI workflows (not Playwright test files) unless explicitly requested.
- `screenshot`: Use for Windows desktop/app screenshots when reviewing the built `3270Web.exe`, comparing UI states, or capturing docs/screenshots outside browser-only flows.
- `gh-fix-ci`: Use when the user asks to inspect or fix failing GitHub Actions checks in `.github/workflows/`. Requires `gh` CLI and authentication.
- `security-best-practices`: Use only for explicit security reviews/reports or secure-by-default changes. This repo is primarily Go backend + browser JS frontend, so check guidance for both stacks.
- `doc`: Use only when the task involves `.docx` files (rare in this repo). Do not trigger for MkDocs docs under `docs/`.

## Repo-Specific Trigger Notes

- For documentation work in `docs/`, `mkdocs.yml`, or `site/`, do not use the `doc` skill unless the user specifically asks about `.docx`.
- For UI checks in browser pages served by `cmd/3270Web`, prefer `playwright` first and `screenshot` second.
- For screenshots used in project artifacts, prefer stable paths under `output/` (for example `output/playwright/` and `output/screenshots/`) unless the user requests another location.
- For CI issues, limit `gh-fix-ci` usage to GitHub Actions workflows in this repo (`docs-gh-pages.yml`, `docker-publish.yml`).

## Local Environment Status (Windows)

- `python`, `node`, `npm`, and `npx` are available.
- `gh` CLI is not currently installed, so `gh-fix-ci` needs setup before use.

## Setup Commands (when needed)

- GitHub CLI for `gh-fix-ci`:
```powershell
winget install --id GitHub.cli
gh auth login
gh auth status
```

- Playwright CLI wrapper path (optional convenience):
```powershell
$env:CODEX_HOME = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { "$HOME/.codex" }
$env:PWCLI = "$env:CODEX_HOME/skills/playwright/scripts/playwright_cli.sh"
```

## Preferred Validation Flow

- UI change in `web/` or `webapp/`: run app, use `playwright` for flow validation, use `screenshot` only for desktop/native captures or when browser-only capture is insufficient.
- Docs/site visual updates: use `screenshot` for local rendered pages if needed.
- Security request: invoke `security-best-practices`, inspect both Go and frontend JS.

