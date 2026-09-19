---
name: cp-expert-refactoring
description: "Algorithmic and performance review of hot paths with competitive-programming rigour, without code golf. Explicit-only."
---

# CP Expert Refactoring

Find algorithmic and performance problems; fix them without changing public behaviour.

## When To Use

- Data or document processing is slow on realistic inputs.
- A hot loop, parser or matcher was written for the demo and never reviewed.
- Before the final freeze, as part of an improvement block.

## Review Checklist

- Asymptotic complexity on realistic sizes (10^5 rows, 10^3 chunks, 10^2 MB).
- Data structures: maps vs slices, repeated linear scans, string concatenation in loops.
- Memory: allocations per row, copies of large buffers, streaming vs full read.
- Duplicate passes over the same data; sorting that could be selection.
- Parsing cost: per-cell regexes, repeated `fmt.Sprintf`, unnecessary conversions.
- Boundary conditions: empty input, single element, duplicate keys, NaN/Inf, integer overflow.
- Numerical correctness: integer division, cumulative error in rolling statistics.
- Determinism: map iteration order leaking into output.

## Rules

1. Preserve public behaviour and the tool contracts.
2. Preserve tests; add tests for the changed path.
3. Prefer a simpler complexity over a clever trick.
4. No readability sacrifice for a three-line miracle.
5. Every non-trivial change states: before complexity, after complexity, expected benefit, protecting tests.

## Output Contract

```json
{
  "changes": [
    {"location": "internal/tools/knowledge.go:search", "before_complexity": "O(chunks * terms)",
     "after_complexity": "O(chunks + matches)", "expected_benefit": "10x on 10k chunks",
     "tests": ["TestKnowledgeIndexAndSearch"]}
  ],
  "rejected_ideas": [{"idea": "cache whole index in memory", "reason": "breaks multi-process freshness"}]
}
```

After applying changes: run `go test ./...` and a smoke handshake; a performance claim without a passing test is not a result.

## Anti-Patterns

- Premature optimisation without a measurable target.
- Changing output formatting or ordering that clients depend on.
- Introducing concurrency without a proven bottleneck.
