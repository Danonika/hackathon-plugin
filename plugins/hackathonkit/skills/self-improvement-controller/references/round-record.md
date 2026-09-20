# Round Record

Every improvement round leaves one record that a later session can replay without reading the chat log.

## Required Fields

| Field | Source |
|---|---|
| `round` | 1-based counter within the loop |
| `axis` | `bugs` / `performance` / `structure` |
| `plan_slug`, `plan_revision` | `plan.status` / `plan.update` |
| `best_before` | accepted score before the round |
| `candidate_bundle_hash`, `git_sha` | `eval.freeze_bundle` |
| `model`, `rubric_version` | judge run / bundle manifest |
| `aggregate_path` | `eval.aggregate` (judge loop only) |
| `decision`, `reason`, `improvement`, `noise_floor` | `eval.compare` |
| `decision_path` | `eval.compare` with `persist=true` |
| `status` | `accepted` / `rolled_back` / `stopped` |

## Record Shape

```json
{
  "round": 2,
  "axis": "performance",
  "plan_slug": "telemetry-anomalies",
  "plan_revision": 7,
  "best_before": 88.0,
  "candidate_bundle_hash": "sha256:9f2c...",
  "git_sha": "abc1234",
  "model": "<model id>",
  "rubric_version": "1.0.0",
  "decision": "accept",
  "reason": "accepted with improvement 3.5 above noise floor 0.5",
  "improvement": 3.5,
  "noise_floor": 0.5,
  "decision_path": "evals/decisions/abc1234/technical-20260921-101500.json",
  "status": "accepted"
}
```

## End-of-Round Checklist

- [ ] `eval.compare` returned a decision; the record copies its `decision`, `reason`, `improvement` and `noise_floor` verbatim.
- [ ] `persist=true` was used for scored decisions, so `decision_path` exists.
- [ ] `plan.update` bumped the revision and the stage evidence includes the bundle hash and paths.
- [ ] Warnings from `eval.freeze_bundle`, `eval.aggregate` and `eval.compare` are in the record.
- [ ] A rollback names the restored snapshot and the cause.
- [ ] `learning.record` ran for anything reusable, including failures.
- [ ] The next round fits inside the remaining budget minus `finalization_reserve_minutes`.

## Time Budget

`remaining = deadline - now`. Stop the loop when `remaining <= finalization_reserve_minutes` (default 45). Record the value you used and where it came from (`configs/evaluation.yaml`); never shorten the reserve to squeeze in one more round.

## Replay

A fresh session should rebuild the loop state from: the latest `evals/plans/<slug>.json`, the `evals/decisions/` records, the `evals/judges/<sha>/aggregate.json` files and the learnings store. If it cannot, the record is incomplete.
