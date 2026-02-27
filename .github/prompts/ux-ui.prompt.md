# UX / UI Change Prompt

## When to use
- Adding or modifying a modal, button, or control in `web/templates/` or `web/static/`.
- Changing the terminal screen layout or 3270 field styles.
- Updating the log viewer, workflow controls, or keyboard-shortcut panel.
- Fixing a visual regression reported against the rendered screen or a modal.

## Inputs
- **Goal:** <goal>
- **Scope:** <scope> (e.g., `web/templates/screen.html`, `web/static/keys.js`)
- **Files:** <files>
- **Constraints:** <constraints> (e.g., must work without a JS bundler, must not break the terminal grid layout)

## Prompt

Follow the conventions in `.github/copilot-instructions.md` and `.github/instructions/frontend.instructions.md`.

Implement the UI change described by: **<goal>**.

Rules:
- Write plain ES6. No npm dependencies, no bundler, no TypeScript.
- Do not move 3270 field `<input>` generation out of `internal/render/html_renderer.go` into templates.
- New state-mutating actions must call a JSON endpoint (not a form POST) and handle errors in JS.
- All `fetch` POST calls rely on the browser sending the `Origin` header automatically; do not add a custom CSRF token mechanism.
- Preserve the monospace grid layout so 3270 field positions remain accurate. Do not change the `font-family` or cell dimensions without an explicit requirement.
- Keep existing endpoint paths (`/logs`, `/screen`, `/key`, etc.) unchanged.

Produce a minimal diff covering only <files>.

## Done looks like
- [ ] `go test ./...` passes (Go handler side unchanged or tested)
- [ ] No new npm/node dependencies introduced
- [ ] Existing JS files edited in-place (no new files unless clearly necessary)
- [ ] Terminal grid layout visually intact (monospace, correct cell sizing)
- [ ] New JS functions follow the existing naming and `fetch`-based pattern
- [ ] `go vet ./...` reports no new issues
- [ ] Diff is scoped to `web/` only (unless a new JSON endpoint was required)
