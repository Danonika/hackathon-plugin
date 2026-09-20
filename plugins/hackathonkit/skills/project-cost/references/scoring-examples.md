# Scoring Examples

Two worked candidates scored with the same rubric version. The points only
illustrate the mapping; always re-derive them from the current artifacts.

## Candidate A - Working Demo, No Clean Room

Snapshot `sha-a1`, previous accepted `best: 0`.

| Criterion | Score | Max | Evidence and reasoning |
|---|---:|---:|---|
| `case_and_functionality` | 14 | 20 | 3 of 4 TASK.md scenarios demoed (`demo-artifacts/run-a1.md`); export scenario absent |
| `technical_implementation` | 16 | 25 | 12 tests pass (`test-results.json`); 2 tool contracts untested; no panic on malformed input |
| `readme_and_docs` | 11 | 20 | setup commands run; no expected-output example; no known-limitations section |
| `reproducibility` | 12 | 20 | docker build log present; clean-room run not performed |
| `reliability_and_security` | 9 | 15 | fallback for missing API key exists; path traversal untested; no secret scan |
| **Total** | **62** | **100** | sum = 62 |

Findings:

```json
{
  "blocking_issues": [],
  "deductions": [
    {"criterion": "case_and_functionality", "points": 6, "reason": "export scenario not demoed"},
    {"criterion": "technical_implementation", "points": 9, "reason": "2 tool contracts untested"},
    {"criterion": "readme_and_docs", "points": 9, "reason": "no expected-output example, no limitations"},
    {"criterion": "reproducibility", "points": 8, "reason": "clean-room not run"},
    {"criterion": "reliability_and_security", "points": 6, "reason": "traversal untested, no secret scan"}
  ],
  "must_fix_next": [
    "Run the clean-room gate on a fresh checkout",
    "Add the export scenario to the demo",
    "Add contract tests for the two untested tools"
  ]
}
```

`eval.compare(mode="technical", candidate=62, best=0, persist=true)` ->
`accept` ("accepted with improvement 62 above noise floor 0.5"). The snapshot
becomes the new best.

## Candidate B - After Fixes and Clean Room

Snapshot `sha-b7`, previous accepted `best: 62`.

| Criterion | Score | Max | Evidence and reasoning |
|---|---:|---:|---|
| `case_and_functionality` | 19 | 20 | all 4 scenarios demoed; one edge case labeled |
| `technical_implementation` | 23 | 25 | 31 tests pass plus contract tests for both tools; one hot path untested |
| `readme_and_docs` | 17 | 20 | expected-output example added; limitations section thin |
| `reproducibility` | 19 | 20 | clean-room report: setup, doctor, compose build, smoke all pass |
| `reliability_and_security` | 14 | 15 | fallbacks covered; traversal test present; secret scan manual |
| **Total** | **92** | **100** | sum = 92 |

`eval.compare(mode="technical", candidate=92, best=62, target=95, persist=true)`
-> improvement 30 >= noise floor 0.5, target not reached -> `accept`. The
candidate becomes best and the loop may continue toward 95; if no further
improvement lands, the next comparison returns `stop`.

## Partial-Credit Patterns

| Situation | Score behavior |
|---|---|
| Feature works but is not in the demo | Score only the demonstrated part; the rest is a deduction |
| Test suite exists but is skipped | No points for those cases; name the skipped files |
| README exists but commands do not run | Setup points only once commands are verified; deduction with the failing command |
| Docker build passes but clean room was skipped | `reproducibility` partial; never full |
| Fallbacks documented but not exercised | Half credit at most for that block; add an exercised test |
| No evidence at all for a criterion | 0 for that criterion; `must_fix_next` entry |

## Evidence-to-Finding Template

```text
Criterion: technical_implementation
Artifact: test-results.json (31 passed, 0 skipped)
Awarded: 23/25
Deduction: 2 points - hot path `data.query` has no performance or failure test
Observed: verified by reading cases in tests/ and tool contracts
Assumption: none
```

## Scoring Checklist

- [ ] Sum of the five scores equals the recorded total.
- [ ] Every score row cites a real artifact from this snapshot.
- [ ] Assumptions labeled, never scored as facts.
- [ ] `best` comes from the previous accepted snapshot, not from memory.
- [ ] No judge rubric keys used.
