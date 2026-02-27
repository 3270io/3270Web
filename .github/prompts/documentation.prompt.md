# Documentation Update Prompt

## When to use
- Adding a new config key, env var, or `S3270_*` variable (update `docs/configuration.md`).
- Changing the workflow JSON schema or step types (update `docs/workflow.md`).
- Adding a new run/build command or changing an existing one (update `README.md`).
- Updating the MkDocs site structure (`mkdocs.yml`, `docs/index.md`).

## Inputs
- **Goal:** <goal>
- **Scope:** <scope> (e.g., `docs/configuration.md`, `README.md`)
- **Files:** <files>
- **Constraints:** <constraints> (e.g., must match the format of surrounding entries, must not remove existing content)

## Prompt

Follow the conventions in `.github/copilot-instructions.md`.

Update the documentation for: **<goal>**.

Rules:
- Match the existing Markdown style, heading levels, and table format of the file being edited.
- For `docs/configuration.md`: every new env var entry must include the variable name, default value, and a one-sentence description. Format must match existing rows in the config table.
- For `docs/workflow.md`: every new step type must be documented with its JSON key names and an example snippet.
- For `README.md`: commands must be inside fenced code blocks; keep the Quick Start section accurate.
- Do not remove or rewrite existing content — append or update only the relevant section.
- Do not introduce MkDocs plugins or theme changes not already in `mkdocs.yml`.

Produce a diff limited to the documentation files.

## Done looks like
- [ ] New env var / config key appears in `docs/configuration.md` with correct format
- [ ] New workflow step type appears in `docs/workflow.md` with JSON example
- [ ] `README.md` Quick Start commands still accurate after change
- [ ] `mkdocs.yml` nav updated if a new page was added
- [ ] No existing documentation content removed
- [ ] Diff is limited to `docs/`, `README.md`, and `mkdocs.yml`
- [ ] `go test ./...` unaffected (documentation-only change)
