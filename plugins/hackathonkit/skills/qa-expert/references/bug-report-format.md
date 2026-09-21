# Bug Report Format

One bug per report. A second person must be able to reproduce it without asking
questions.

## Schema

```json
{
  "id": "BUG-003",
  "title": "Empty CSV crashes the anomaly loader",
  "severity": "major",
  "status": "open",
  "candidate": "abc123",
  "steps": [
    "create data/input/empty.csv with a header row only",
    "call timeseries.detect_anomalies with path=empty.csv",
    "observe the envelope"
  ],
  "expected": "status error with a clear message and no panic",
  "actual": "status ok with zero anomalies; warning missing",
  "evidence": "trace kit-..., envelope JSON, gate check CH-4",
  "environment": "linux, bin/hackathonkit-mcp, no OPENAI_API_KEY",
  "suspected_area": "internal/tools/timeseries.go loadSeries",
  "workaround": "validate row count before loading"
}
```

## Severities

| Severity | Meaning | Verdict impact |
|---|---|---|
| blocker | demo cannot run or data leaks | `not ready` |
| critical | mandatory criterion fails, no workaround | `not ready` |
| major | important path broken, workaround exists | `conditional` |
| minor | cosmetic, edge case, low impact | `conditional` |

## Rules

- Steps are exact: file, tool call, arguments, expected observable.
- Expected vs actual are separate lines; "does not work" is not an actual.
- Evidence is a trace id, artifact id, command output or envelope; never a memory.
- Never paste secret values, personal data or full `.env` content into a report.
- One fix attempt per bug before re-testing; a fixed bug gets `status: fixed` and a re-run line.
- Regression: reference the original report id and raise severity one level.
- Bugs that cannot be reproduced get `status: needs-info` with what was tried.
