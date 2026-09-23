---
name: evaluate
description: "Use when scoring a frozen demo or assessing technical readiness: freeze a bundle, run at least 3 isolated judge passes, aggregate/calibrate/rank, score project cost, then run bounded improvement rounds where only eval.compare decides accept/rollback/stop. Do not use mid-build or without a frozen artifact; use $verify for the gate and $judge for a single scoring rubric."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: evaluation
tools:
  - system.context
  - eval.freeze_bundle
  - eval.aggregate
  - eval.calibrate
  - eval.rank
  - eval.compare
  - quality.gate
  - reports.create
  - learning.recall
  - learning.record
---

# Evaluate

Score what was built against the regulation rubric, with the arithmetic done by tools, and let only `eval.compare` decide whether to keep, roll back or stop.

## When To Use

- A build has reached S5 and a frozen artifact exists to score.
- The user asks to evaluate, score, judge, rank or assess readiness.
- Before a Demo Day freeze (demo profile) or a technical readiness check (technical profile).
- Do not use mid-build on an unfrozen tree, and do not use it as a substitute for the gate (`$verify`).

## Core Principles

1. **Freeze first, then score.** Judges score a bundle with a stable hash, never a moving tree.
2. **Deterministic tools own the arithmetic.** `eval.aggregate`, `eval.calibrate`, `eval.rank`, `eval.compare` compute; you interpret.
3. **A missing judge report is never zero.** Fewer than `min_valid_judges` (default 3) valid scores means the round is inconclusive, not failed.
4. **Judges never see the target score.** The target stays out of judge prompts to avoid anchoring.
5. **Only `eval.compare` decides.** Accept, rollback and stop come from the comparison, never from prose.
6. **Bound the rounds.** The improvement loop has a round budget and stops on a decision or the budget.

## Workflow

1. Call `system.context` and confirm the plan stage is S5.
2. Freeze the artifact: `eval.freeze_bundle` with `profile: demo` for Demo Day or `technical` for readiness. Keep the bundle path and hash.
3. Score the frozen bundle with `$judge`: run at least 3 isolated passes (the `evaluator` subagent), each returning the canonical per-judge JSON. Judges receive the bundle, never the target.
4. Aggregate: `eval.aggregate` drops invalid, conflicted and foreign-sha reports. If `valid < min_valid_judges`, report inconclusive and stop the round.
5. Calibrate: `eval.calibrate` over two runs to get the noise floor.
6. Rank: `eval.rank` applies regulation 9.6; ties go to the commission (`eval.rank` with votes).
7. Score technical readiness with `$project-cost` (evidence-only).
8. Run bounded improvement rounds with `$self-improvement-controller`; after each candidate, `eval.compare` returns accept/rollback/stop.
9. Optional hardening, in this order: `$roast-project`, `$qa-expert`, `$cp-expert-refactoring` (hot paths), `$system-design-expert` (structure), `$output-self-check`.
10. Record the decision and the lesson with `learning.record`; persist the report with `reports.create`.

### Profiles

| Profile | Purpose | Contents |
|---|---|---|
| demo | Demo Day judging | presentation evidence only (regulation 9.4) |
| technical | Readiness review | repository minus runtime state |
| full | Internal full snapshot | repository minus secrets/logs/data |

### Profile selection

- `demo` keeps only presentation evidence for Demo Day judges; use it when the audience is non-technical.
- `technical` keeps the repository minus runtime state; use it for an engineering readiness review.
- `full` is the internal snapshot; use it when both audiences may read the result.

### Round protocol

Each improvement round is atomic: propose one change, rebuild, re-freeze, re-score, compare. Never widen a round mid-flight. Record the change, the new score and the decision so the history survives the session.

## Decision Matrix

| Signal | Action |
|---|---|
| `valid_judges < min_valid_judges` | Inconclusive; report and stop; do not treat as zero |
| Candidate within noise floor | Accept (the delta is not real) |
| Regression above noise floor | Rollback |
| Candidate meets target | Stop (reached) |
| Conflicted or foreign-sha report | Dropped by `eval.aggregate`; note the count |
| Tie after 9.6 | Commission vote via `eval.rank`; majority per tie tier |

## Output Contract

- The bundle path and hash that was scored.
- Per-judge reports count and the valid count used.
- `eval.aggregate` summary, calibrated noise floor and the 9.6 ranking with any commission result.
- The `eval.compare` decision (accept/rollback/stop) with its inputs.
- `$project-cost` readiness score and its evidence.
- The improvement-round log and the next action.

## Verification

- Confirm the bundle hash matches the frozen artifact before and after scoring.
- Confirm `eval.aggregate` received at least `min_valid_judges` valid reports; otherwise the round is inconclusive.
- Confirm the target score never appeared in a judge prompt.
- Confirm the decision came from `eval.compare`, not from prose.
- If any fails, the evaluation is not valid; fix and repeat.

## Failure Modes

| Symptom | Action |
|---|---|
| Too few valid judges | Inconclusive; report and stop the round |
| Judge reports a foreign sha | `eval.aggregate` drops it; re-freeze if systematic |
| Noise floor missing | Run `eval.calibrate` on two runs before deciding |
| Tie with no majority | Commission vote; if still no majority, no ranking |
| Bundle changed after freeze | Re-freeze; do not mix hashes |

## Anti-Patterns

- Scoring an unfrozen or moving tree.
- Treating a missing judge report as zero.
- Letting a prose summary override `eval.compare`.
- Telling the judge the target score.
- Running improvement rounds past the budget without a decision.

## References

- `references/eval-tools.md` - `eval.*` argument shapes, profiles and regulation 9.6.
- `references/round-budget.md` - how the bounded improvement loop stops.
