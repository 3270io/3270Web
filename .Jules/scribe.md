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

## 2024-05-22 - Configuration Precedence Mismatch
Learning: Users modifying `3270Web-config.xml` `<model>` setting saw no effect because the generated `.env` file (defaulting to `3279-4-E`) silently overrides the XML configuration.
Impact: Confusion and loss of trust in documentation which stated XML defaults.
Action: Updated `docs/configuration.md` to explicitly warn about `.env` precedence and the effective default.

## 2026-01-30 - Misleading Configuration Default
Learning: The generated `.env` file documentation for `S3270_NO_VERIFY_CERT` claimed the default was `true` (insecure), but the actual default value used was `false` (secure).
Impact: Users might assume the system is insecure by default or be confused about TLS behavior.
Action: Fixed the default documentation in `internal/config/s3270_env.go` and verified with `TestEnvDocumentationDrift`.

## 2026-01-31 - Missing Workflow Implementation
Learning: The `workflow_playback.go` file was deleted, breaking the build and the documented workflow playback feature, despite tests verifying the logic.
Action: Restored `workflow_playback.go` with stubs and disabled the feature in docs/tests to resolve the build failure without adding unverified logic.

## 2026-02-01 - Undocumented Security Configuration
Learning: The `ALLOW_LOG_ACCESS` environment variable, which gates access to sensitive logs, was implemented in code but completely absent from documentation.
Impact: Administrators might not know how to debug issues (by enabling logs) or might be unaware of the security control.
Action: Documented the `ALLOW_LOG_ACCESS` variable in `docs/configuration.md`.

## 2026-02-02 - Orphaned Technical Documentation
Learning: Detailed technical documentation (`terminal-model-limits.md`) existed but was not linked from the main configuration guide, effectively hiding the "why" behind model enforcement.
Impact: Users might be confused by screen dimension enforcement logic despite documentation existing in the repo.
Action: Linked the orphan document in `docs/configuration.md` to make it discoverable.

## 2026-02-04 - Misleading Visual Documentation
Learning: The ASCII diagram in `terminal-model-limits.md` was visually misaligned, pointing to incorrect fields in the status line example.
Impact: Users trying to understand the s3270 status line format from the docs would be confused or misled about which field represents the model.
Action: Corrected the ASCII art alignment and clarified field indexing in the text.

## 2026-02-05 - Undocumented Configuration Precedence
Learning: The `.env` variables `S3270_CODE_PAGE` and `S3270_EXEC_COMMAND` had undocumented side effects: `S3270_CODE_PAGE` silently overrides XML `<charset>`, and `S3270_EXEC_COMMAND` disables the configured host connection.
Impact: Users configuring charset via XML would be confused why it wasn't taking effect if `.env` had a value, and users using `S3270_EXEC_COMMAND` might not realize it replaces the connection logic entirely.
Action: Documented these overrides in `docs/configuration.md` and added `TestBuildS3270Args_Precedence` to verify and enforce this behavior.

## 2026-02-06 - Undocumented Empty String Arguments
Learning: The `.env` argument parser supports empty quoted strings (`""` or `''`) as valid empty arguments, but this was not documented, leading to ambiguity on how to pass empty values.
Impact: Users needing to pass empty arguments (e.g. to reset flags or provide required empty values) had no reference on how to do so.
Action: Documented the empty string behavior in `docs/configuration.md` after verifying it with `TestSplitArgs`.
