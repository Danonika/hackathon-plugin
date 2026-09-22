---
name: self-improvement-controller
description: "Use when the mandatory scenario passes and a candidate needs bounded improvement rounds: fast block, technical loop, judge loop, rollback on regression, stop conditions and the final clean-room freeze. Explicit-only; never invoke implicitly. Do not use for the first build of the scenario, for scoring ($judge, $project-cost) or for finding defects ($roast-project, $cp-expert-refactoring, $system-design-expert)."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: quality
tools:
  - eval.aggregate
  - eval.calibrate
  - eval.compare
  - eval.freeze_bundle
  - plan.status
  - plan.update
  - learning.record
  - system.context_budget
---

# Self-Improvement Controller

Run bounded improvement rounds against the staged plan: code decides accept, rollback or stop; you execute.

## When To Use

- The mandatory scenario from the plan passes end to end and time remains for hardening (stages S4-S5).
- A milestone produced a prioritized fix list from `$roast-project` or a judge round.
- A candidate snapshot needs an accept/rollback decision, or the best snapshot needs the final clean-room freeze.
- Do not use for the first build of the main scenario; finish `$build-planner` stages first.
- Do not use to produce scores (`$judge`, `$project-cost`) or to find defects (`$roast-project`, `$cp-expert-refactoring`, `$system-design-expert`).

## Core Principles

1. **Code decides.** `eval.compare` returns `target_reached`, `accept`, `rollback` or `stop` and is the only decision maker. Never accept, roll back or stop in prose.
2. **One axis per round.** A round changes one class of problem (bugs, performance, structure) so the decision stays attributable.
3. **Rollback is mandatory and cheap.** Any technical decrease, a judge regression above the calibrated noise floor, or a failed mandatory scenario restores the last judged or clean-room-passing snapshot.
4. **Noise floor before judging.** Judge decisions use `calibration.noise_floor` from `eval.calibrate`; the configured `min_delta` (default 0.5) is only the floor of that number.
5. **Bounded by config.** `configs/evaluation.yaml` owns `max_technical_iterations`, `max_judge_rounds`, `finalization_reserve_minutes` (defaults 2, 2, 45) and the targets; the plan's latest revision owns stages and the mandatory scenario.
6. **Every round leaves a record.** Bundle hash, git sha, model id, rubric version, aggregate path and decision path are part of the result, not optional.

## Preconditions

1. `plan.status` returns a plan with a `mandatory_scenario`; if it returns `partial` with `plan: null`, stop and create one with `$build-planner`.
2. The mandatory scenario passes on the current working tree; otherwise fix it before any scoring.
3. `eval.calibrate` has produced a `noise_floor` for the snapshot. If not, freeze the snapshot with `eval.freeze_bundle`, score it twice with `$judge`, then call `eval.calibrate` with `run1`, `run2` and `snapshot_sha=<git_sha>`, and keep `calibration.noise_floor`.
4. Remaining time exceeds `finalization_reserve_minutes`; if not, skip to the final freeze.

## Round Types

| Axis | Owner skills | Exit evidence before scoring |
|---|---|---|
| `bugs` | `$roast-project` | critical and high findings closed, targeted tests green |
| `performance` | `$cp-expert-refactoring` | benchmark before/after on the same input, `go test ./...` green |
| `structure` | `$system-design-expert` | accepted proposals verified, smoke and mandatory scenario green |
| `evidence` | `$judge` + `$project-cost` | frozen bundle, aggregate and decision paths |

Never mix axes in one round: a score drop must be attributable to one change class.

## Workflow

1. **Read the plan.** Call `plan.status` with no slug (latest plan) or `slug=<plan>`; keep `slug`, `revision`, `current_stage` and `mandatory_scenario`.
2. **Budget the context.** Call `system.context_budget`; drop non-essential skills and references from the round before launching sub-agents, and note flagged items in the round record.
3. **Run the fast block** (one axis per round, targeted tests after each change):
   - `$roast-project` against the frozen snapshot -> fix `critical` and `high` findings.
   - `$cp-expert-refactoring` -> apply accepted changes only (hot paths first).
   - `$system-design-expert` -> apply accepted proposals only (structure after performance).
   - Quick smoke: MCP starts, `initialize`, `tools/list`, mandatory happy path.
4. **Freeze the candidate.** Call `eval.freeze_bundle` with `out` (default `data/judge-bundles`), `read_only=true` and the candidate `model`; keep `bundle_path`, `bundle_hash` and `git_sha`. A full clean-room rebuild is not part of an iteration.
5. **Technical decision.** Re-score the candidate with `$project-cost`, then call `eval.compare` with `mode=technical`, `candidate=<new technical score>`, `best=<accepted score>`, `mandatory_scenario_failed=<bool>`, `snapshot_sha=<git_sha>`, `persist=true` and follow the decision matrix.
6. **Judge loop** (at most `max_judge_rounds`, default 2): `eval.freeze_bundle` the best candidate with `profile="demo"`; run `$judge` passes on read-only copies; `eval.aggregate` with `reports[]` and `snapshot_sha=<git_sha>`, and when `valid_judges < min_valid_judges` (default 3) run substitute passes until the minimum is met; rank candidates with `eval.rank` when comparing snapshots (regulation 9.6); then `eval.compare` with `mode=judge`, `candidate=<aggregate.mean_total>` (unrounded), `best=<previous judge mean>`, `noise_floor=<calibrated>`, `snapshot_sha=<git_sha>`, `persist=true`. Feed `common_deductions` and `top_improvements` into the next fast block.
7. **Persist stage evidence.** Call `plan.update` with `slug`, `stage=<current_stage.id>`, `status` (`doing` while a round runs, `done` or `failed` after the decision) and `evidence=[bundle_hash, aggregate_path, decision_path]`. `plan.update` bumps `revision`; keep the previous revision for comparison.
8. **Record the learning.** Call `learning.record` with `trigger`, `insight`, `outcome` (`success`/`failure`/`neutral`) and `evidence` for anything reusable - including why a rollback happened.
9. **Final freeze.** Run the full clean-room gate once on the best snapshot: fresh checkout, setup, compose build, MCP handshake, mandatory scenario, Normal/Fly toggle. If it fails, restore the last clean-room-passing snapshot. Freeze README, demo script and evidence-backed Q&A cards from the passing snapshot.

## Decision Matrix

| `eval.compare` decision | Trigger | Action |
|---|---|---|
| `target_reached` | candidate >= target | adopt as best, exit the loop, run the final freeze |
| `accept` | improvement at or above the noise floor | adopt as best, run the next round |
| `rollback` | technical decrease, judge regression above the noise floor, or mandatory scenario failed | restore the last judged or clean-room-passing snapshot, stop the loop, record the learning with the decision path |
| `stop` | improvement below the noise floor | keep the candidate only if it did not regress; stop iterating on that axis |

## Stop Conditions

| Condition | Source | Action |
|---|---|---|
| target reached | `eval.compare` -> `target_reached` | proceed to the final freeze |
| iteration cap reached | `max_technical_iterations` / `max_judge_rounds` | freeze the current `best` |
| improvement below the noise floor | `eval.compare` -> `stop` | change axis or freeze; do not force a retry |
| candidate worse than best | `eval.compare` -> `rollback` | restore, stop the loop, record the failure |
| mandatory scenario regressed | `mandatory_scenario_failed=true` | restore, fix the scenario, then re-score |
| time reserve reached | `finalization_reserve_minutes` | freeze immediately; no new round |

Stops that are not decisions must still be recorded with the value that fired.

## Output Contract

- Per round: `round`, `axis`, `candidate_bundle_hash`, `git_sha`, `model`, `rubric_version`, score before/after, `decision`, `reason`, `improvement`, `noise_floor`, `aggregate_path`, `decision_path`, `plan_revision`.
- Per loop: `iterations_used` against the cap, the identity of `best`, and every rolled-back candidate with its `reason`.
- Final: the clean-room report (commands and results), the frozen bundle identity, and the remaining gaps and limitations.
- Every number traces to a tool result; warnings from every envelope are carried into the report.

## Round Record

```json
{
  "round": 2, "axis": "performance",
  "candidate_bundle_hash": "sha256:9f2c...", "git_sha": "abc1234",
  "model": "<model id>", "rubric_version": "2.0.0",
  "decision": "accept", "improvement": 3.5, "noise_floor": 0.5,
  "aggregate_path": "evals/judges/abc1234/aggregate.json",
  "decision_path": "evals/decisions/abc1234/technical-20260921-101500.json",
  "plan_revision": 7
}
```

The full schema and the end-of-round checklist live in `references/round-record.md`.

## Verification

- `eval.compare` returned one of the four decisions; no accept/rollback exists in prose.
- `best` equals the last `accept`/`target_reached` candidate, or the last judged/clean-room-passing snapshot after a rollback.
- Every judge decision used a `noise_floor` produced by `eval.calibrate` on the same snapshot.
- `plan.update` returned a bumped `revision` and the current stage carries the round evidence.
- The final clean-room gate ran on the frozen snapshot, not on the working tree.

## Failure Modes

| Symptom | Action |
|---|---|
| `eval.aggregate` returns `inconclusive` (fewer than 2 valid reports) | fix the invalid reports (schema, evidence, snapshot sha), re-aggregate; never compare an inconclusive run |
| `eval.calibrate` errors on mismatched snapshot shas | re-run both judge passes on the same frozen bundle, then retry |
| `eval.compare` returns `rollback` | restore the last judged or clean-room-passing snapshot, record the learning with the decision path, stop the loop |
| mandatory scenario regressed | pass `mandatory_scenario_failed=true`, expect `rollback`, fix before any further scoring |
| remaining time <= `finalization_reserve_minutes` | stop the loop and freeze the best snapshot; do not start a new round |
| `eval.freeze_bundle` warns `git sha not resolved` | commit the candidate and re-freeze; an unattached bundle is not a judged snapshot |
| a judge report carries a foreign `snapshot_sha` | `eval.aggregate` drops it; re-run that judge on the frozen bundle |
| the session is interrupted mid-loop | rebuild state from `evals/plans/<slug>.json`, the `evals/decisions/` records and `hackathonkit-ctl sessions resume <id>` |

## Anti-Patterns

- Comparing a candidate to judge scores that do not exist yet.
- Letting judges see the target score, each other's reports or `evals/`.
- Running a full clean-room rebuild on every iteration (`full_cleanroom_each_iteration` is false by default; it runs once on the final candidate).
- Deciding accept/rollback in prose instead of calling `eval.compare`.
- Editing risk by hand; risk is deterministic (failed tools, panics, eval rollbacks, stalls).
- More than one axis per round, or continuing after a `stop` decision.

## References

- `references/loop-playbook.md` - rollback/stop playbook, loop pseudocode and a worked round.
- `references/round-record.md` - the per-round record schema and end-of-round checklist.
