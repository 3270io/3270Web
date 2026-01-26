## 2024-10-24 - Untested Configuration Logic
Learning: Critical configuration logic (`.env` loading) was documented but lacked verification tests.
Impact: Users relying on documented behavior might encounter regressions without warning, as CI would not catch breaks in this logic.
Action: Implemented "Tests as Spec" for `s3270_env.go` to enforce documented behavior.
