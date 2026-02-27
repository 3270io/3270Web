---
applyTo: "web/**"
---

# Frontend (JS / HTML) Conventions

## Template rendering

- Go templates live in `web/templates/`. They receive data via `c.HTML(...)` from `cmd/3270Web/main.go`.
- 3270 field HTML is generated server-side in `internal/render/html_renderer.go`, not in templates. Do not move field `<input>` generation into templates — it would lose coordinate and sizing information.
- Use `{{ .SomeField }}` for simple values; use `{{ template "name" . }}` for partials defined in the same template set.

## Client JavaScript

- All JS lives in `web/static/*.js`; there is no bundler (no webpack/vite/esbuild). Write plain ES6 compatible with evergreen browsers.
- Runtime behaviour (key sending, workflow controls, log viewer) is driven by `fetch()` calls to JSON endpoints. New features follow the same pattern: add a JSON endpoint in Go, call it from JS.
- `web/static/logs.js` owns the log viewer modal and expects these endpoints: `/logs`, `/logs/toggle`, `/logs/clear`, `/logs/download`, `/logs/access`. Do not rename or move them.
- CSRF: all `POST`/`PUT`/`DELETE` `fetch` calls must include the `Origin` header (or rely on the browser sending it automatically for same-origin requests). The Go `originRefererCheck` middleware will reject requests without a matching origin.

## Styling

- CSS lives in `web/static/`. No preprocessor (no Sass/Less). Add styles to the appropriate existing `.css` file.
- The terminal screen must preserve the monospace grid layout so 3270 field positions remain accurate.

## Adding a new UI feature

1. Add the JSON endpoint in `cmd/3270Web/main.go`.
2. Add or update the relevant `.js` file in `web/static/`.
3. Update the relevant template in `web/templates/` if new markup is needed.
4. Do not introduce npm dependencies or a build step for the frontend.
