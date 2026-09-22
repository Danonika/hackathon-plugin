# Go Complexity and Performance Patterns

Concrete patterns found in Hackathon Agent Kit code; each row names the symptom, the before/after complexity and the fix.

## Hot Loop and Data Structures

| Pattern | Before | After | Fix |
|---|---|---|---|
| Linear scan of a slice per lookup inside a loop | O(n*m) | O(n+m) | build `map[key]index` once, then look up |
| `append` without preallocation in a counted loop | repeated reallocations | O(n) | `make([]T, 0, n)` when n is known |
| `s += x` per row instead of `strings.Builder` | O(n^2) | O(n) | `strings.Builder` or `bytes.Buffer` |
| Map iteration leaking into output | nondeterministic | deterministic | sort keys or iterate the source slice |
| Sorting only to take the first k | O(n log n) | O(n) | single-pass selection or `container/heap` |
| Re-scanning the same rows for each term | O(rows*terms) | O(rows+matches) | one pass, collect per term or index tokens |
| Slice removal via `append(a[:i], a[i+1:]...)` in a loop | O(n^2) | O(n) | filter into a new slice |
| `defer` inside a hot loop | allocation per iteration | amortized | hoist the defer or close explicitly |

## Parsing and Text

| Pattern | Problem | Fix |
|---|---|---|
| `regexp.MustCompile` inside the per-cell loop | recompiles per cell | compile once at package level |
| `fmt.Sprintf` for simple concatenation | reflection and allocation | `+` or `strings.Builder` |
| `[]byte` <-> `string` conversions per row | copies | keep `[]byte`, convert at the boundary |
| `strconv.Atoi` then `fmt.Sprint` round-trip | double conversion | keep the numeric value |
| Full file read before knowing the schema | memory spike | `bufio.Scanner` with a bounded buffer |
| CSV without `ReuseRecord` when only aggregating | allocation per row | `reader.ReuseRecord = true` |
| Unicode work with `len(s)` / `s[i]` | wrong for non-ASCII | `utf8.RuneCountInString`, `[]rune` |

## Memory

- Allocation per row: reuse a scratch buffer, or aggregate into numbers first.
- Copying large buffers: pass slices; avoid `append([]byte{}, src...)` unless ownership changes.
- Streaming vs full read: prefer streaming when only aggregates are needed.
- Map growth: size the map (`make(map[K]V, n)`) when n is known.
- Interface boxing in hot loops (`any` per value) shows up as allocations in benchmarks.

## Correctness Alongside Speed

- Integer division truncates; check whether a float is required.
- Rolling statistics: recompute periodically to avoid cumulative float drift.
- Overflow: `int` is safe for row counts, not for byte products; check before multiplying.
- NaN/Inf: decide explicitly whether they are dropped or propagated, and test it.
- Boundary cases: empty input, one element, duplicate keys, all-equal values.

## Before/After Sketch

```go
// before: O(rows * terms) - one full scan of rows per term
for _, term := range terms {
    for _, row := range rows {
        if strings.Contains(row.Text, term) {
            hits[term] = append(hits[term], row)
        }
    }
}

// after: O(rows + matches) for exact-token terms - one pass builds the index
index := make(map[string][]int, len(rows))
for i, row := range rows {
    for _, token := range strings.Fields(row.Text) {
        index[token] = append(index[token], i)
    }
}
for _, term := range terms {
    for _, i := range index[term] {
        hits[term] = append(hits[term], rows[i])
    }
}
```

Substring (not token) matching needs a different structure such as Aho-Corasick; do not add that machinery unless the benchmark on the real input proves it is needed.

## What Not to Change

- Public output ordering, artifact formats and tool argument names.
- Tests that document a contract; add new tests instead.
- A readable O(n) implementation for an O(n log n) one that is already fast enough.
