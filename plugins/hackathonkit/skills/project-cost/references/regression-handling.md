# Regression Handling

`eval.compare(mode="technical", ...)` is the only source of an accept, rollback
or stop decision for the technical loop. This file shows what it returns and
what the operator must do next.

## Preconditions

1. `candidate` is the total of the freshly scored candidate snapshot.
2. `best` is the total of the previous accepted snapshot, scored with the same
   rubric version; `0` for the first candidate.
3. `target` defaults to `configs/evaluation.yaml` (`technical_target: 95`);
   `noise_floor` defaults to `min_delta: 0.5`.
4. `mandatory_scenario_failed` reflects the current plan stage, not a stale run.
5. Pass `snapshot_sha` and `persist: true` so the decision is stored under
   `evals/decisions/<sha>/technical-<ts>.json`.

## Decision Rules

Applied in this order by the tool:

| # | Condition | Decision |
|---|---|---|
| 1 | `mandatory_scenario_failed == true` | `rollback` |
| 2 | `candidate < best` | `rollback` |
| 3 | `candidate >= target` | `target_reached` |
| 4 | `improvement < noise_floor` | `stop` |
| 5 | otherwise | `accept` |

## Worked Cases

| Candidate | Best | Target | Noise floor | Mandatory failed | Decision | Reason (tool output) |
|---:|---:|---:|---:|---|---|---|
| 60 | 62 | 95 | 0.5 | false | `rollback` | score decreased from 62 to 60; rollback |
| 64 | 64 | 95 | 0.5 | false | `stop` | improvement 0 below noise floor 0.5; stop iterating on this axis |
| 64.3 | 64 | 95 | 0.5 | false | `stop` | improvement 0.3 below noise floor 0.5; stop iterating on this axis |
| 65 | 64 | 95 | 0.5 | false | `accept` | accepted with improvement 1 above noise floor 0.5 |
| 95.2 | 92 | 95 | 0.5 | false | `target_reached` | target 95 reached with 95.2 |
| 99 | 62 | 95 | 0.5 | true | `rollback` | mandatory scenario failed; rollback and stop |

Notes:

- Scores are rounded to 2 decimals; improvements are computed from the rounded
  values, so a 0.004 gain is never an accept.
- An unchanged score is a `stop`, not an `accept`: the loop must not keep
  iterating an axis that produces no measurable improvement.
- `target_reached` is evaluated before the improvement check, so a candidate at
  or above the target exits even when the step was small.

## Operator Actions

| Decision | Action |
|---|---|
| `rollback` | Restore the previous accepted snapshot; fix the cause before the next attempt; stop if the mandatory scenario failed |
| `stop` | Keep the current best, record the decision path, and stop iterating this axis (move to judge rounds or finalization) |
| `accept` | Promote the candidate to `best`, continue the loop within the iteration cap |
| `target_reached` | Promote, then freeze the snapshot for `$judge` or finalization |

Rollback restores files; it never deletes the score record or the decision
record, both of which stay as evidence for the next comparison.

## Per-Criterion Regressions

`eval.compare` compares totals, so a regressed criterion can hide inside a
higher total. When that happens:

1. Keep the candidate if the tool accepted it.
2. Add a `must_fix_next` entry naming the criterion, the lost points and the
   artifact that regressed.
3. If the regressed criterion is part of the mandatory scenario, set
   `mandatory_scenario_failed: true` on the next call instead of papering over it.

## Failure Handling

| Symptom | Action |
|---|---|
| `eval.compare` `persist_error` | Keep the inline decision; fix the path or repository root; state the missing artifact |
| Previous score came from another rubric version | Re-score the previous snapshot under the current rubric before comparing |
| `best` unknown or from a different snapshot | Do not compare; re-establish the baseline first |
| Candidate scored while the tree was edited | Re-freeze and re-score; a moving tree invalidates evidence |
| Decision disagrees with the reviewer's expectation | Trust the tool; quote the `reason` and fix the evidence, not the arithmetic |

## Checklist

- [ ] Same rubric version for candidate and best.
- [ ] `snapshot_sha` set and persisted.
- [ ] `mandatory_scenario_failed` verified against the plan.
- [ ] Decision and reason recorded verbatim, with the decision path.
- [ ] Rollback performed by restoring the previous accepted snapshot.
- [ ] `stop` respected: no further iteration on the same axis.
