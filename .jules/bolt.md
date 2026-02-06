## 2024-10-24 - [Avoid Slice Allocation in Hot Loops]
**Learning:** In hot rendering loops, allocating slices for `strings.Join` (even small ones) can be a significant bottleneck.
**Action:** Use `strings.Builder` directly to construct strings piece-by-piece instead of building intermediate slices, especially when the logic is simple.

## 2025-01-27 - [Avoid html.EscapeString in Hot Loops]
**Learning:** `html.EscapeString` and `strings.ReplaceAll` allocate new strings which generates significant garbage in hot loops (e.g., rendering hundreds of fields).
**Action:** Use a custom helper that writes directly to `strings.Builder` (e.g., `writeEscaped(sb, s)`), utilizing `strings.IndexAny` for a fast path to skip processing when no escaping is needed. This reduced allocations by ~90% in worst-case scenarios.

## 2025-02-14 - [Replace Regexp with Manual Parsing in Hot Loops]
**Learning:** `regexp.FindAllString` and `regexp.ReplaceAllString` are significantly slower than manual string parsing (e.g. `strings.Fields` and loops) in hot paths. Replacing regex with manual parsing in `decodeLine` reduced execution time by ~82%.
**Action:** Prefer `strings` package functions or manual parsing over `regexp` when the pattern is simple and the code is in a critical loop.

## 2025-05-23 - [Refactor Screen Parsing to Avoid Allocations]
**Learning:** Reconstructing strings from tokens only to parse them again (split/join/split) is a major waste of allocations in hot loops.
**Action:** Pass tokenized data structures ([][]string) directly between producer and consumer functions instead of serializing to strings in between.

## 2025-05-24 - [String Concatenation and Stack Allocation]
**Learning:** String concatenation (e.g. `s := "a" + "b"`) does not always cause heap allocations in Go if the result does not escape (e.g. passed to `sb.WriteString`). The compiler can stack-allocate the backing array.
**Action:** Do not assume replacing string concatenation with builder writes will reduce *heap* allocations count, but it still improves CPU performance by avoiding buffer copying and construction overhead (observed ~10% speedup).

## 2025-05-24 - [Avoid strconv in Tight Loops]
**Learning:** `strconv.ParseUint` adds significant overhead (error wrapping, base validation) for small, fixed-length strings in tight loops (e.g. 2-byte hex).
**Action:** Unroll fixed-length parsing manually if it is a hot path. Replacing `strconv.ParseUint` with a manual 2-byte hex parser improved execution speed by ~2.7x (10.9ns -> 3.9ns).

## 2025-05-25 - [Use strings.Cut to Avoid Slice Allocation]
**Learning:** `strings.Split` allocates a slice even for a single split. In hot loops where only the first part is needed (or the string is expected to be single-line), this is wasteful.
**Action:** Use `strings.Cut` (Go 1.18+) instead of `strings.Split` when possible. Replacing `GetValueLines()` (which calls `Split`) with `strings.Cut` in a hot rendering loop reduced allocations from 212/op to 20/op (~90% reduction).

## 2025-05-25 - [Optimize String Trimming in Hot Loops]
**Learning:** Generic `strings.Trim` with a cutset (like "\x00 _") incurs overhead from bitset construction or iteration. A specialized loop for small fixed cutsets is significantly faster (observed ~30% faster).
**Action:** Use custom trim functions for known small cutsets in hot paths.

## 2025-05-25 - [Fast Path for Int formatting]
**Learning:** `strconv.AppendInt` is fast but still has function call and logic overhead. For frequently written small integers (e.g., coordinates), a manual fast path (0-999) can be ~30% faster.
**Action:** Inline or use fast-path helpers for formatting small integers in tight loops.

## 2025-05-26 - [Manual Tokenizer vs strings.Fields]
**Learning:** `strings.Fields` allocates a slice of strings and potentially new string headers, which is costly in hot loops. A manual byte-scanning loop can avoid this allocation entirely if the logic is simple.
**Action:** Replace `strings.Fields` with manual iteration when parsing simple delimited strings in critical paths. This reduced allocations by 50% and memory usage by 27% in `extractTokens`.

## 2025-05-26 - [Slice Pre-allocation in Hot Loops]
**Learning:** Appending to a slice without pre-allocation causes multiple re-allocations and copying. If the size is known or can be estimated (e.g. 1-to-1 mapping), pre-allocating using `make([]T, 0, cap)` is a huge win.
**Action:** Always pre-allocate slices in hot loops if the upper bound is known. This reduced execution time by ~40% in `decodeLineTokens`.
