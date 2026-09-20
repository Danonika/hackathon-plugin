---
name: cp-expert-refactoring
description: "Use when a hot path is slow on realistic inputs or before the final freeze: algorithmic and performance review of Go parsers, loops and matchers with before/after complexity, benchmarks and tests, without code golf. Explicit-only; never invoke implicitly. Do not use for correctness-only review, scoring ($project-cost) or module boundaries ($system-design-expert)."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: quality
tools:
  - plan.status
  - eval.compare
  - learning.record
---

# CP Expert Refactoring

Find algorithmic and performance problems in hot paths; fix them without changing public behaviour or readability.

## When To Use

- Data or document processing is slow on realistic inputs (10^5 rows, 10^3 chunks, 10^2 MB).
- A hot loop, parser or matcher was written for the demo and never reviewed.
- Before the final freeze, as part of a `$self-improvement-controller` fast block.
- Do not use for correctness-only review or bug hunting; use `$roast-project`.
- Do not use for module boundaries and contracts; use `$system-design-expert`.
- Do not use for scoring; `$project-cost` owns the technical score and `$judge` owns the demo score.

## Core Principles

1. **Measure first.** A performance claim needs a before/after number from the same input and machine; no number, no change.
2. **Preserve behaviour and contracts.** Tool argument names, envelope shape, output ordering and artifact formats stay stable.
3. **Complexity over micro-tricks.** Prefer the simpler asymptotic win; a constant-factor change needs a measured bottleneck.
4. **No readability sacrifice** for a three-line miracle.
5. **Every change states** before complexity, after complexity, expected benefit, measured numbers and the protecting tests.
6. **Scored changes go through code.** If the change is part of a scored loop, `eval.compare` accepts or reverts it; never decide in prose.

## Review Checklist

- Asymptotic complexity on realistic sizes: 10^5 rows, 10^3 chunks, 10^2 MB.
- Data structures: maps vs slices, repeated linear scans, `append` in loops, `strings.Builder`.
- Memory: allocations per row, copies of large buffers, streaming vs full read, map sizing.
- Duplicate passes over the same data; sorting that could be a selection.
- Parsing cost: per-cell regexes, repeated `fmt.Sprintf`, unnecessary conversions.
- Boundary conditions: empty input, single element, duplicate keys, NaN/Inf, overflow.
- Numerical correctness: integer division, cumulative error in rolling statistics.
- Determinism: map iteration order leaking into output.
- Go-specific costs: `defer` in hot loops, interface boxing, `[]byte`/`string` conversions.

Patterns with fixes and before/after complexity: `references/go-complexity-patterns.md`.

## Complexity Reference

| Operation | Typical cost | Watch for |
|---|---|---|
| map lookup / insert | O(1) average | bad hash distribution on string keys |
| slice append | amortized O(1) | growth copies; preallocate when n is known |
| `sort.Slice` | O(n log n) | using sort only to take the first k |
| `strings.Contains` per row per term | O(n*m) worst case | nested term loops over long text |
| `regexp` match | O(n*m) worst case | recompiling inside a loop |
| map iteration | O(n), unordered | leaking into output ordering |
| `json.Unmarshal` into `any` | allocation heavy | decode into typed structs |

Use the table to name the before/after class precisely; do not guess a class you cannot argue.

## Workflow

1. **Locate hot paths.** Call `plan.status` (latest plan or `slug=<plan>`) to read the current stage and its evidence, then trace the call path. Candidates: parsers, chunkers, matchers, per-row allocations, repeated scans, duplicate passes.
2. **Measure before.** Run the existing test or benchmark on the largest realistic input from `data/input`; record wall time, allocations and input size. If no benchmark exists, write one first.
3. **Review.** Walk the checklist above; for each candidate record `before_complexity`, the input size where it matters and the protecting test.
4. **Fix.** Apply the smallest change that removes the complexity; keep public behaviour; add or extend a test for the changed path.
5. **Verify locally.** Run `go build ./...`, `go vet ./...` and `go test ./...`; re-run the benchmark on the same input and record after numbers as medians of repeated runs, not a single sample.
6. **Decide through code.** Re-score with `$project-cost` and call `eval.compare` with `mode=technical`, `candidate=<new technical score>`, `best=<accepted score>`, `snapshot_sha=<git_sha>`, `persist=true`; follow the decision matrix. On `rollback`, revert the change to the accepted snapshot.
7. **Record.** Call `learning.record` with `trigger` (for example "when a chunk index is scanned once per query"), `insight`, `outcome` and `evidence` (benchmark output, test names). Rejected ideas stay in the output with the reason.

## Decision Matrix

| Signal | Action |
|---|---|
| repeated benchmark runs beat the before numbers on the same input | keep, record medians and protecting tests |
| asymptotic class improves but wall clock is flat at demo size | keep only if the input can realistically grow; report the class win |
| no measurable difference across repeated runs | revert; speculative change |
| change alters output ordering or formatting | reject unless the contract explicitly allows it |
| concurrency proposed without a proven bottleneck | reject; keep the sequential version |
| a test fails after the change | revert immediately; never weaken the test |
| `eval.compare` returns `rollback` | revert to the accepted snapshot and record why |
| benchmark spread is wider than the delta | increase repetitions and pin the input; do not claim a win |

## Worked Example

```text
location: internal/tools/knowledge.go:search
before:   for each term, scan every chunk        -> O(chunks * terms)
after:    one pass builds term -> chunk ids       -> O(chunks + matches)
input:    data/input/<largest>.csv, n rows / 10^3 chunks
before:   1.8 ms/op, 412 allocs/op  (median of 5)
after:    0.4 ms/op,  96 allocs/op  (median of 5)
tests:    TestKnowledgeIndexAndSearch, TestKnowledgeSearchEmpty
decision: eval.compare(mode=technical) -> accept, improvement 3.5 above noise floor 0.5
```

The numbers are illustrative; replace them with the measured ones. A change without a benchmark table does not ship.

## Output Contract

```json
{
  "changes": [
    {"location": "internal/tools/knowledge.go:search",
     "before_complexity": "O(chunks * terms)", "after_complexity": "O(chunks + matches)",
     "input": "data/input/<largest>.csv (n rows)",
     "measured_before_ms": 0, "measured_after_ms": 0,
     "expected_benefit": "one pass instead of one scan per term",
     "tests": ["TestKnowledgeIndexAndSearch"]}
  ],
  "rejected_ideas": [{"idea": "cache the whole index in memory", "reason": "breaks multi-process freshness"}],
  "checks_run": ["go build ./...", "go vet ./...", "go test ./...", "benchmark re-run"],
  "decision": "eval.compare decision when the change was scored"
}
```

Numbers are medians from the same machine and input; a claim without them is not a result.

## Verification

- `go build ./...`, `go vet ./...` and `go test ./...` pass.
- The benchmark ran before and after on the same input; medians and spread are recorded.
- Public behaviour is unchanged: same tool contracts, output ordering and artifacts.
- Every scored change has an `eval.compare` decision; no prose accept/rollback.
- Pre-existing failures, warnings and regression risks are stated instead of hidden.
- The benchmark file stays in the repository with the change.

## Failure Modes

| Symptom | Action |
|---|---|
| benchmark is flaky (wide spread) | increase repetitions, pin the input, report medians and spread; a single sample is not evidence |
| `go test ./...` fails in an unrelated package | record it as pre-existing with the test name; do not mask it |
| the change is faster only on synthetic data | re-measure on the largest real input; revert otherwise |
| complexity improves but memory grows sharply | report both numbers; keep only if memory stays within the environment limits |
| profiling shows the bottleneck is outside the code | report the finding and revert speculative changes |
| the change is correct but unreadable | prefer the readable version; readability is a contract with the next session |
| the benchmark disappears with the change | restore it; a deleted benchmark is not evidence |

## Anti-Patterns

- Premature optimisation without a measured target.
- Code golf, clever one-liners or dropped error handling for speed.
- Adding concurrency without a proven bottleneck.
- Caching that breaks freshness (for example an index cached across processes).
- Weakening or deleting tests to make a change pass.
- Changing output formatting or ordering that clients depend on.
- Benchmarking a copy of the code instead of the real call path.

## References

- `references/go-complexity-patterns.md` - concrete Go patterns with before/after complexity and fixes.
- `references/benchmark-protocol.md` - how to measure, repeat and report before/after numbers.
