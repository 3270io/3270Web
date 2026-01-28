## 2024-10-24 - Untested Configuration Logic
Learning: Critical configuration logic (`.env` loading) was documented but lacked verification tests.
Impact: Users relying on documented behavior might encounter regressions without warning, as CI would not catch breaks in this logic.
Action: Implemented "Tests as Spec" for `s3270_env.go` to enforce documented behavior.

## 2024-10-25 - Undocumented .env Argument Parsing
Learning: Users configuring `s3270` options via `.env` (like `S3270_SET`) faced undocumented quoting rules, leading to potential misconfiguration.
Impact: Complex arguments with spaces required specific quoting (nested quotes) that wasn't explained, causing frustration or errors.
Action: Documented the split/quote behavior in `docs/configuration.md` with examples.

## 2026-01-28 - Untested Workflow Keys
Learning: The `docs/workflow.md` claimed support for keys like `PressPA<n>` and `PressClear` (via manual editing), but no tests verified this capability, creating a risk of silent regression if the underlying mapping logic changed.
Impact: Users relying on advanced workflow features might find them broken despite documentation assurances, eroding trust.
Action: Added `TestWorkflowSpecialKeys` to explicitly verify that all documented key types are correctly processed and sent to the host.
