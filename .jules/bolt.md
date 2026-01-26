## 2024-10-24 - [Avoid Slice Allocation in Hot Loops]
**Learning:** In hot rendering loops, allocating slices for `strings.Join` (even small ones) can be a significant bottleneck.
**Action:** Use `strings.Builder` directly to construct strings piece-by-piece instead of building intermediate slices, especially when the logic is simple.
