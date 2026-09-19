---
name: roast-project
description: "Run three isolated critics (bug hunter, product skeptic, reproducibility reviewer) over a read-only snapshot. Findings only: no scoring, no code edits. Explicit-only."
---

# Roast Project

Attack the snapshot from three independent angles. Critics never edit code and never score.

## When To Use

- After a milestone, before an improvement pass.
- When the demo must survive hostile questions.
- As the first step of a self-improvement block.

## Critics (run in parallel, isolated read-only copies)

**A - Bug Hunter**
Runtime bugs, broken error paths, race/state issues, invalid assumptions, unhandled edge cases (empty files, missing columns, wrong encodings, divisive zero, huge inputs).

**B - Logic / Product Skeptic**
Illogical flows, dead features, contradictions with the task statement, fake or weak demo paths, unclear user value, capabilities that exist but are never reachable.

**C - Reproducibility / Adversarial Reviewer**
Hidden local dependencies, missing environment variables, broken Docker setup, hardcoded paths, cold-start failures, malformed input behaviour, secret leakage, missing permissions.

## Severity Definitions

| Level | Meaning |
|---|---|
| critical | Demo or core scenario fails |
| high | A main scenario degrades or data is wrong |
| medium | Real defect with a workaround |
| low | Cosmetic, documentation, polish |

## Output Contract

```json
{
  "critical": [], "high": [], "medium": [], "low": [],
  "recommended_fixes": [
    {"finding": "fragile parsing of empty CSV", "severity": "high",
     "evidence": "internal/tools/data.go:readCSVTable", "fix": "return explicit error envelope"}
  ]
}
```

Each finding needs evidence: file and line, command output, test name or artifact id.

## Rules

- No finding without evidence; no vague advice.
- Critics do not fix, do not score and do not read each other's output.
- After fixes are applied, the same critic re-checks only its own findings.
