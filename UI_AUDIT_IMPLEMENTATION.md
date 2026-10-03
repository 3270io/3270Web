# UI audit implementation

All 22 findings from the 3 October 2026 audit have corresponding changes. This is a self-hosted terminal application: activation means a useful terminal session and workflow recording/replay. There is no product trial or subscription paywall to add.

| Finding | Implementation |
|---|---|
| C1 | One-click Petstore sample, automatically bound shared port; other samples/ports remain optional. Automatic targets survive export, replay and reconnect. |
| C2 | Inline first-session goals apply the selected workspace mode; Business exposes Show automation. |
| C3 | Configured target is visibly enforced and read-only; incompatible raw/sample selectors are hidden. Existing profile policy remains intact. |
| C4 | Narrow phones default to readable 16px terminal text with confined horizontal panning, active-field visibility, column position and an explicit overview alternative. |
| C5 | Login access/recovery guidance, safe configurable administrator help link, organisation-managed account explanation and learning links. |
| H1 | One Saved connections workflow, personal/shared scope, account-backed profiles, explicit legacy import with originals retained. |
| H2 | Connect follows the address immediately and is the primary action; secondary management and sample actions follow. |
| H3 | Docs hero focuses on operator/automation outcomes and one primary sample activation CTA; sample path appears before the feature catalogue. |
| H4 | First session and installation explain account setup and locating the setup code in platform logs. |
| H5 | Connect-now advanced options use profile validation for TLS/LU/model/code page; saving is a separate optional action. |
| H6 | Address validation, explicit persisted-save confirmation, failure feedback and import/export replace silent browser-only saving. |
| H7 | AI entry point explains subscriptions/keys, external provider charges, local models and a useful starter prompt. Terminal/recording need no AI. |
| H8 | Structured login/session/permission errors route to different recovery paths; permission and sign-in failures never trigger host reconnect loops. |
| H9 | Error pages preserve sessions and offer Return to terminal, with expandable diagnostics and shared styling. |
| H10 | DNS, TLS, timeout, refusal and missing-emulator errors have specific guidance; submitted options persist and diagnostics can be copied. |
| H11 | Once-only, optional inline first achievement, sample exercise and keyboard guidance; no blocking tour. |
| H12 | Workspace, quick-start dismissal and terminal view persist per authenticated account; local installs retain browser fallback. |
| N1 | Working session backgrounds default off, with an explicit View toggle; flatter surfaces in dark/light themes. |
| N2 | Admin Logs has a page with shared navigation, active state, loading/error feedback and downloads, independent of a terminal session. |
| N3 | Mainframe-first connection wording, Business/Engineering mode labels and provider-neutral Stop AI response. |
| N4 | Saved-connection editor groups address/port/TLS, discloses terminal options with presets/custom values and retains authorised sharing controls. |
| N5 | Touch collapse control has a 44px minimum target. |

## Validation

- `go test ./...` passed after the final sample replay fix.
- `go test -race ./...` passed before the final replay fix; the affected package was rerun with the race detector afterward.
- `go vet ./...`, application build, JavaScript syntax checks and `mkdocs build --strict` passed.
- Headless Chromium inspection at 1440px and 375px: connect, saved connections, first sample session, workspace choice and terminal view; no page overflow or JavaScript exceptions in exercised paths.
- Browser checks: TLS profile persistence; legacy import retaining originals; account preferences across independent browsers; first-admin setup; admin Logs; enforced target; light theme; real 132-column terminal; permission/login handling without reconnect requests.
- Actual sample interaction: recorded ADMIN login, exported a five-step workflow, uploaded/replayed it and reconnected using the automatic-port target.
- Readable font remains stable on 375×450 and 900×375 viewport changes, simulating keyboard height and landscape.

## Remaining environment limits

Physical mobile keyboard/thumb interaction was simulated by viewport changes, not tested on a device. Live production mainframes and external AI-provider OAuth/paid requests were not exercised; existing policy and provider flows were retained. The local browser harnesses are temporary QA scripts, not repository tests.
