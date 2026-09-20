---
name: project-cost
description: "Internal technical readiness score 0-100 for the current snapshot, evidence-only. Explicit-only: never invoke implicitly."
---

# Project Cost

Technical readiness score, not a price. Every point needs an artifact behind it.

## When To Use

- After each improvement pass, to decide whether the candidate is accepted or rolled back.
- Before a judge round, as a cheap technical gate.

## Procedure

1. Collect evidence: tests, smoke results, README sections, build output, demo artifacts, clean-room report.
2. Score the five criteria; a criterion with no evidence scores zero for the unproven part.
3. List `blocking_issues` (things that prevent a demo) separately from `deductions` (lost points).
4. Compare with the previous accepted snapshot using `eval.compare` with `mode=technical` and follow its `decision`:
   - `rollback` -> restore the previous accepted snapshot, stop.
   - `stop` -> improvement below the configured noise floor; do not iterate further on this axis.
   - `accept` / `target_reached` -> record the score and continue or exit.

## Output Contract

```json
{
  "technical_score": 91,
  "scores": {"case_and_functionality": 19, "technical_implementation": 23, "readme_and_docs": 17, "reproducibility": 18, "reliability_and_security": 14},
  "blocking_issues": [],
  "deductions": ["README has no expected-output example"],
  "must_fix_next": ["Add a clean-room README scenario", "Add a negative input test"]
}
```

Rubric details and evidence rules: `references/rubric.md`.

## Rules

- Never award points for claimed functionality without an artifact.
- Reproducibility cannot receive full score unless the clean-room gate passed.
- This score is never shown to judges as input; it gates the builder loop only.
