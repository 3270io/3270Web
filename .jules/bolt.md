## 2024-10-24 - [Avoid Slice Allocation in Hot Loops]
**Learning:** In hot rendering loops, allocating slices for `strings.Join` (even small ones) can be a significant bottleneck.
**Action:** Use `strings.Builder` directly to construct strings piece-by-piece instead of building intermediate slices, especially when the logic is simple.

## 2025-01-27 - [Avoid html.EscapeString in Hot Loops]
**Learning:** `html.EscapeString` and `strings.ReplaceAll` allocate new strings which generates significant garbage in hot loops (e.g., rendering hundreds of fields).
**Action:** Use a custom helper that writes directly to `strings.Builder` (e.g., `writeEscaped(sb, s)`), utilizing `strings.IndexAny` for a fast path to skip processing when no escaping is needed. This reduced allocations by ~90% in worst-case scenarios.
