# Maintain Copilot Library Prompt

## When to use
- Periodically (e.g., after a significant feature merge) to keep Copilot guidance in sync.
- When build commands, directory structure, or framework versions change.
- When a new architectural pattern is introduced (e.g., a new internal package, a new middleware).
- When prompted by a CI failure caused by a stale instruction referencing a renamed file or command.

## Inputs
- **Goal:** <goal> (default: "Bring all Copilot instruction and prompt files up to date with the current codebase")
- **Scope:** <scope> (default: `.github/copilot-instructions.md`, `.github/instructions/`, `.github/prompts/`)
- **Files:** <files> (default: all files listed in scope)
- **Constraints:** <constraints> (e.g., preserve deliberate human customisations, do not remove content without a note)

## Prompt

You are performing a maintenance pass on the Copilot library for the `3270io/3270Web` repository.

### Step 1 — Re-run discovery

Inspect the current repository to verify these facts (compare against what is currently written in `.github/copilot-instructions.md`):

1. Go module name and version (`go.mod`).
2. Direct dependencies (Gin version, go3270 version, etc.).
3. Directory structure: `cmd/`, `internal/`, `web/`, `docs/`, `scripts/`, `webapp/`.
4. Build commands: `go run ./cmd/3270Web`, `scripts/build-windows.ps1`, `docker build -t 3270web .`.
5. Test command: `go test ./...` (no Makefile, no golangci-lint config).
6. CI workflows in `.github/workflows/` — names and trigger conditions.
7. Session-locking helper name (`withSessionLock`).
8. CSRF middleware name (`originRefererCheck`).
9. Log-access env var (`ALLOW_LOG_ACCESS`).
10. Mock host constructor (`host.NewMockHost`).

### Step 2 — Detect drift

For each item in Step 1, check whether the current `.github/copilot-instructions.md`, `.github/instructions/*.instructions.md`, and `.github/prompts/*.prompt.md` accurately reflect reality.

Flag as **drift** if:
- A command, file path, function name, or env var has been renamed or removed.
- A new architectural boundary (e.g., new `internal/` package) is not mentioned.
- A referenced file no longer exists.

Flag as **breaking** if the drift would cause a developer following the instructions to break the build or tests.

### Step 3 — Propose and apply updates

For each drift item, produce a minimal diff for the affected file.

Rules:
- Preserve deliberate human customisations (e.g., project-specific bullet points, curated examples). Note why if you must change them.
- Do not remove content without explaining why it is no longer accurate.
- Do not introduce new conventions not already present in the codebase.
- Do not add new third-party tools or libraries to the instructions.

### Step 4 — Finish with this checklist

- [ ] Commands in instructions match current `go.mod`, `scripts/`, and `Dockerfile`
- [ ] No outdated tool names referenced (golangci-lint, webpack, etc.)
- [ ] No dependencies assumed that are not in `go.mod`
- [ ] All `internal/` package names and file paths verified against the actual directory tree
- [ ] Prompt files reference correct test helpers (`host.NewMockHost`, `gin.TestMode`, `httptest`)
- [ ] CI workflow names and trigger conditions match `.github/workflows/`
- [ ] Breaking drift items explicitly flagged and fixed
- [ ] Human customisations preserved or change rationale noted

## Done looks like
- [ ] `go test ./...` passes (no behaviour changed by this maintenance pass)
- [ ] All drift items resolved with minimal diffs
- [ ] Breaking changes called out separately with clear explanation
- [ ] Checklist above completed and all items checked
