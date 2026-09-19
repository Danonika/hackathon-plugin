---
name: self-improvement-controller
description: Orchestrate bounded improvement rounds: fast block 1, project cost, demo evidence, judge rounds, rollback on regression. Explicit-only.
---

# Self-improvement controller

Read limits from `configs/evaluation.yaml`. Calibrate the judge noise floor with a test-retest run before using judge scores as a stop condition (`noise_floor = max(min_delta, 2 * retest_delta)`).

## Loop

1. Fast block 1: roast (3 in parallel), fix critical/high, CP refactoring, targeted tests, system design review, quick Docker smoke, build demo evidence, freeze candidate.
2. `$project-cost` on the candidate. Rollback and stop on mandatory-scenario failure or cost regression.
3. Build demo evidence bundle, freeze the judge bundle.
4. `$judge` round N. Compare only with the previous judge round mean.
5. Regression beyond the noise floor -> rollback to the previous judged snapshot. Improvement below the floor -> stop.
6. On target reached or budget end: final clean-room gate, then freeze the best snapshot.

## Rules

- A candidate is never compared with a judge score before that candidate has been judged.
- Stop conditions: target reached, iteration limit, improvement below noise floor, regression, mandatory scenario broken, time reserve reached.
- Keep `finalization_reserve_minutes` untouched for README, demo and Q&A.
- Store all artifacts under `evals/` per git sha.
