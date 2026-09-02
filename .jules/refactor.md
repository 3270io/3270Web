## 2024-05-22 - Extracted Hostname Validation Logic
Learning: Large files like `main.go` often accumulate unrelated helper functions (like validation), making the core logic harder to follow.
Risk: Security-critical validation logic buried in a massive file can be overlooked or accidentally modified.
Action: Extract cohesive logic (like hostname validation) into separate files within the same package to improve readability and separation of concerns.

## 2024-05-24 - Declarative Error Handling
Learning: Long chains of boolean ORs for string matching (e.g., error classification) are hard to read and maintain.
Risk: High cognitive load and increased risk of syntax errors when adding new conditions.
Action: Use declarative data structures (slices/maps) for classification logic to separate data from control flow.

## 2025-05-25 - Extracted Key Normalization Logic
Learning: Helper functions for specific domains (like key parsing/normalization) tend to accumulate in `main.go`, obscuring the application flow.
Risk: Logic buried in `main.go` is often untested and hard to reuse or reason about in isolation.
Action: Extract such logic into domain-specific files (e.g., `keys.go`) within the same package to improve readability and testability without complex dependency changes.

## 2025-05-25 - Extracted Status Parsing Logic
Learning: Complex struct implementations (like emulator hosts) often mix process management with protocol parsing.
Risk: Logic coupling makes it hard to test parsing in isolation and obscures the core process lifecycle flow.
Action: Extract protocol parsing and helper functions into separate files (e.g., `*_helpers.go` or `status.go`) within the same package.

## 2025-05-25 - Extracted Key Command Execution Logic
Learning: Repetitive error handling and state checks (like connection status or keyboard locking) in sequential command attempts (try-fallback) duplicate code and obscure the core logic.
Risk: Inconsistent error handling updates (e.g., fixing a reconnection bug in one block but missing the other) and reduced readability.
Action: Encapsulate the common execution and check logic into a helper method that returns a "done/continue" signal, allowing the main flow to express the high-level strategy clearly.

## 2025-05-25 - Standardized Status Line Parsing
Learning: Implicit dependency on array indices (magic numbers) for parsing fixed-format strings (like status lines) makes code brittle and hard to read.
Risk: Incorrect index usage or changes in status format can silently break logic in multiple places.
Action: Define named constants for protocol field indices to document the data structure and ensure consistency across the codebase.

## 2026-02-02 - Extracted Complex Token Parsing Logic
Learning: Parsing loops that handle multiple token types inline (e.g., data bytes vs. attribute commands) create deep nesting and high cognitive load.
Risk: Logic for different token types becomes entangled, making it harder to verify or modify the handling of one type without affecting others.
Action: Extract the handling of complex, state-changing tokens (like "Start Field" attributes) into dedicated helper functions to keep the main parsing loop clean and focused on flow.

## 2024-05-25 - Magic Strings and Manual Parsing
Learning: Magic strings for protocol attributes (e.g., "c0", "41") and manual low-level parsing (like hex conversion) inline obscure intent and clutter logic.
Risk: Typos in magic strings are hard to catch, and verbose inline parsing distracts from the higher-level data processing flow.
Action: Define named constants for protocol keys and use focused helper functions for low-level parsing tasks.

## 2026-02-05 - Extracted Process Launcher Logic
Learning: Mixing application startup wiring with low-level process configuration (like binary path resolution and argument building) in `main.go` creates a "god object" anti-pattern.
Risk: Logic for critical external dependencies (s3270 binary) becomes entangled with web server logic, making it harder to test, configure, or swap out the backend.
Action: Extract cohesive process management logic into dedicated files (e.g., `s3270_launcher.go`) to clarify the boundary between the web application and the underlying emulator process.
