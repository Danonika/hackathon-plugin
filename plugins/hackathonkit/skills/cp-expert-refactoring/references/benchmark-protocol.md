# Benchmark Protocol

A performance claim is only as good as the measurement behind it. Use the same input, the same machine and repeated runs.

## 1. Pick a Realistic Input

- Largest file in `data/input` that the mandatory scenario uses, or a documented synthetic file with the same shape.
- Record `path`, `rows`/`chunks`/`bytes` with `files.describe` before measuring.
- Never benchmark on a toy fixture and claim production impact.

## 2. Write or Find a Benchmark

```go
func BenchmarkSearch(b *testing.B) {
    data := loadFixture(b, "data/input/<file>")
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = search(data, "term")
    }
}
```

```sh
go test ./internal/tools -run '^$' -bench BenchmarkSearch -benchmem -count 5
```

`-count 5` gives spread; `-benchmem` gives allocations; `-run '^$'` skips the unit tests.

## 3. Capture Before and After

| Run | Time/op | Allocs/op | Bytes/op |
|---|---|---|---|
| before (median of 5) | ... | ... | ... |
| after (median of 5) | ... | ... | ... |

- Compare medians, not single samples; report the spread when it is wide.
- If the delta is smaller than the spread, the change is not measured - revert or repeat with a pinned input.
- Keep the benchmark in the repository; a deleted benchmark is not evidence.

## 4. Report

- `location`, `input`, before/after complexity, before/after numbers and protecting tests.
- State what was not measured (memory under concurrency, cold start, network).
- A regression in another dimension (memory, readability) is reported, not hidden.

## 5. Sanity Checks

- [ ] The benchmark runs the same code path the tool calls (not a copy).
- [ ] The input is the largest realistic one; its size is stated.
- [ ] `go test ./...` still passes.
- [ ] The output of the tool is unchanged (same ordering, same values).
- [ ] Numbers are medians of repeated runs on one machine, and the machine is noted.

## Worked Example

```text
input: data/input/sample-telemetry.csv (72 rows)  # illustrative
before: 1.8 ms/op, 412 allocs/op   (median of 5)
after:  0.4 ms/op, 96 allocs/op    (median of 5)
change: preallocated result slice + single-pass aggregation
tests:  TestAnalyzeTelemetry, TestAnalyzeEmpty
```

At demo sizes the absolute gain is small; the pattern matters because the same code path runs on the larger judge input.
