## 2024-05-22 - Extracted Hostname Validation Logic
Learning: Large files like `main.go` often accumulate unrelated helper functions (like validation), making the core logic harder to follow.
Risk: Security-critical validation logic buried in a massive file can be overlooked or accidentally modified.
Action: Extract cohesive logic (like hostname validation) into separate files within the same package to improve readability and separation of concerns.

## 2024-05-24 - Declarative Error Handling
Learning: Long chains of boolean ORs for string matching (e.g., error classification) are hard to read and maintain.
Risk: High cognitive load and increased risk of syntax errors when adding new conditions.
Action: Use declarative data structures (slices/maps) for classification logic to separate data from control flow.
