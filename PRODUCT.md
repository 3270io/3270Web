# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

- **Mainframe operators** working live TN3270 applications from a browser, often on locked-down or centrally managed machines where installing a desktop emulator is not an option.
- **Business users** who know the question they need answered but not the green-screen navigation, served by Guided Business Tasks rather than by learning the application.
- **IT operations and platform teams** who self-host 3270Web, manage accounts, assigned hosts, TLS and the audit trail for a team or an estate.
- **Automation and integration engineers** who record or generate workflows, call the REST/JSON API, or drive sessions through an MCP-capable assistant.

Assumed from repository documentation; see `## Assumptions`.

## Product Purpose

3270Web puts a full TN3270 terminal in any modern browser tab with no desktop client to install, package or patch. On top of the terminal it adds discovery, business-level operation and recording: explore a host once and get a screen map, named business functions, a way to run the application in natural language, and a load-test workload, rather than four separate projects.

Success means an operator can reach a host and work it from a browser, a team can run one shared instance without a per-desk rollout, and automation can be produced from the same recorded or discovered session that a person just used.

## Positioning

The terminal is the floor, not the claim. The mechanism a neighbouring terminal product could not truthfully copy is that discovery, business understanding and recording are **one pass**: AI exploration maps every screen and transition, annotates what each screen and field is for, and exports the same coverage as a portable workflow JSON. The recording made in the browser is the JSON an API replays, with no second macro language in between. Delivery is self-hosted and open source, with bring-your-own AI provider rather than a vendor-owned AI service.

## Operating Context

- A self-hosted Go server (binary, Docker image or Compose stack) serves the web UI and connects to TN3270 / TN3270E hosts through the bundled or system `s3270` process.
- Sessions run against real mainframe applications (commonly IBM z/OS, also Rocket Enterprise Server stand-ins) and against bundled sample applications for evaluation and demos.
- Instances range from a single operator on a laptop to a shared deployment with local or OIDC accounts, assigned hosts, per-user data separation, rate limits and an always-on audit trail.
- Work products are server-side and portable: connection profiles, recorded `workflow.json` files, chaos mind maps, the task catalogue, compatibility profiles and discovery reports.
- The same capabilities are exposed through the browser, a token-guarded REST/JSON API at `/api/v1/*` (opt-in via `API_TOKEN`) and an MCP server.
- The application is deployed and upgraded by the operator; state lives in files under a configurable data directory and must survive container replacement.

## Capabilities and Constraints

Confirmed capabilities include: interactive browser TN3270 sessions with virtual keyboard, logging and print screen; workflow recording, replay and debug playback; chaos/AI exploration producing a screen mind map and named business functions; natural-language session operation with approval or hands-free modes; Guided Business Tasks with named inputs and outputs; host compatibility profiler; chaos map comparison; connection profiles; session tabs; themes, terminal fonts and a command palette; local accounts, OIDC sign-in, admin pages and an audit log; REST/JSON API; HLLAPI-shaped endpoint; MCP server; printer sessions; embedding.

Durable constraints:

- Backend is Go (see `go.mod`); frontend is plain ES6 JavaScript and CSS served from `web/static/` with **no bundler, no preprocessor and no npm build step**. New UI work follows that pattern.
- 3270 screen geometry is meaningful: the terminal must preserve the monospace character grid, and 3270 field HTML is generated server-side, not in templates.
- Security expectations are part of the product: CSP and security headers, origin/referer checks, CSRF validation, request-body limits, input validation, and origin checks on state-mutating endpoints.
- AGPL-3.0-or-later with commercial terms available; new Go files carry an SPDX header. Contributions arrive under DCO sign-off plus the additional grant in `CONTRIBUTING.md`.
- The protocol surface is TN3270 and TN3270E only; 5250, VT, DBCS and bidirectional scripts are explicitly out of scope today.
- Product copy must not name commercial terminal emulators outside the single comparison section in `docs/feature-roadmap.md`, and must not frame work as matching or copying a rival product.

## Brand Commitments

- Name: **3270Web** (domain `3270Web.3270.io`, repository `3270io/3270Web`).
- Existing brand assets live under `brand/` (mark, lockup, themed and mono variants, icons, OG card) and `web/static/` (logo, favicon, touch icon, OG image).
- Voice in existing docs is direct, evidence-led and explicit about limits; it states exceptions rather than hiding them, and avoids vendor comparison outside the sanctioned section.
- Open-source identity and the tested accessibility statement are public commitments, not optional decoration.

## Evidence on Hand

- `README.md` and the MkDocs site under `docs/` describe features, installation, configuration, API, accessibility and the category comparison.
- Real screenshots in `docs/images/` and `web/static/images/`, plus recorded demonstration videos under `docs/videos/` (including the showcase tour).
- Bundled sample 3270 applications (`internal/sampleapps`, `ALLOW_SAMPLE_APPS`) for running the product without a host.
- A WCAG 2.1 Level AA partial-conformance statement in `docs/accessibility.md`, with named known issues rather than blanket claims.
- No invented customers, testimonials, benchmarks or pricing claims exist in the repository; future work must not fabricate them.

## Product Principles

1. **The terminal is the floor, not the product.** Fidelity and reliable session work come first; automation is what makes the floor worth standing on.
2. **One pass, many outputs.** Exploration, business understanding and recording should produce the map, the vocabulary, the prompt-driven flow and the load-test workload together.
3. **Operators stay in sight of the host.** Automation must not hide what the mainframe is doing; approval, visible terminal state and honest failure reporting build trust.
4. **Self-hosted and bring-your-own.** Deployment, identity and AI provider are the operator's choices, not a vendor's lock-in.
5. **State the limits.** Accessibility exceptions, protocol gaps and unsupported conversions are documented plainly rather than smoothed over.

## Accessibility & Inclusion

The browser interface targets **WCAG 2.1 Level AA**, currently a stated partial conformance. Keyboard operation, visible focus, labelled 3270 fields, live status regions and reduced-motion support are existing commitments. The two-dimensional terminal grid is allowed to scroll rather than reflow because column position is information, and `Alt+Shift+M` is the documented escape from the terminal keyboard region. New work must preserve these behaviours; custom themes remain the operator's responsibility to check.

## Assumptions

These facts were inferred from the repository and product documentation because no structured interview was available in this run. Confirm or correct them before relying on them for design decisions.

- The primary audience is the four groups above; the repository does not rank them.
- "Success" is framed as browser reach, shared-instance operation and automation output, derived from the README and docs rather than from a stated product goal.
- No image-generation tool is available in this environment, so no `buildPath` was recorded in `.impeccable/config.json`.
- No visual direction, palette or typography is asserted here; those belong to later `document` or `new-work` steps.
