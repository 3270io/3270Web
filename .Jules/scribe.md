## 2024-10-24 - Untested Configuration Logic
Learning: Critical configuration logic (`.env` loading) was documented but lacked verification tests.
Impact: Users relying on documented behavior might encounter regressions without warning, as CI would not catch breaks in this logic.
Action: Implemented "Tests as Spec" for `s3270_env.go` to enforce documented behavior.

## 2024-10-25 - Undocumented .env Argument Parsing
Learning: Users configuring `s3270` options via `.env` (like `S3270_SET`) faced undocumented quoting rules, leading to potential misconfiguration.
Impact: Complex arguments with spaces required specific quoting (nested quotes) that wasn't explained, causing frustration or errors.
Action: Documented the split/quote behavior in `docs/configuration.md` with examples.
