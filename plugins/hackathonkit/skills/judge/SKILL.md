---
name: judge
description: "Use when a frozen Hackathon Agent Kit snapshot must be scored with the official Demo Day rubric by 3 isolated judge runs. Produces validated per-judge reports, a deterministic eval.aggregate result and a calibrated noise floor. Explicit-only: never invoke implicitly. Do not use for technical readiness ($project-cost) or critic findings ($roast-project)."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: evaluation
tools:
  - eval.freeze_bundle
  - eval.aggregate
  - eval.calibrate
---

# Judge

Score a frozen, read-only snapshot with three procedurally isolated judge passes; the tools own the arithmetic and the stop condition.

## When To Use

- A candidate snapshot is frozen and an official Demo Day score is needed.
- Before final submission, to collect likely jury questions, common deductions and top improvements.
- To calibrate judge variance: score one frozen snapshot twice and derive the noise floor.
- Do not use for technical readiness (`$project-cost`), qualitative criticism (`$roast-project`) or accept/rollback decisions (the controller calls `eval.compare`); judges only produce scores.

## Preconditions

- The candidate passes its mandatory scenario; a broken demo wastes a judge run.
- `demo-artifacts/`, `test-results.json` and `smoke-results.json` exist for the frozen snapshot.
- `configs/evaluation.yaml` is read once: `judges: 3`, `demo_day_target: 90`, `min_delta: 0.5`, `max_judge_rounds: 2`.
- One calibration exists for the current model and rubric version; reuse its noise floor until either changes.
- The target score lives with the controller and never enters a judge prompt.

## Core Principles

1. Frozen only: judge a snapshot produced by `eval.freeze_bundle`, never the live working tree, and never edit code during a pass.
2. Three isolated passes: same rubric and weights, three perspectives (01 product value/innovation, 02 result quality/technical depth, 03 adversarial Q&A/scaling/presentation). Isolated procedurally, not statistically independent.
3. Blind judges: a judge never sees the target score, previous judge scores, the aggregate, other reports, `evals/`, logs or the mutable repository.
4. No evidence, no points: every deduction cites a file, artifact id, test result or clean-room report; observed facts and assumptions are separated.
5. `eval.aggregate` owns the arithmetic; a run is usable only with a majority of valid reports (at least 2 of 3). Never compute means, variance or noise floors by hand.
6. `eval.compare` owns accept/rollback/stop; the judge hands over `mean_total` and the calibrated noise floor, nothing more.
7. Scores from different snapshots, rubric versions or models are never compared.

## Workflow

1. Freeze the candidate: call `eval.freeze_bundle` with `out: "judge-bundle"`, `read_only: true`, `model: "<model-id>"` and extra `exclude` patterns only when something must be kept out. Keep `bundle_path`, `bundle_hash`, `git_sha`, `files` and the `excluded` list. The default excludes already drop `.env*`, `bin`, `logs`, `evals`, `data/artifacts`, `data/index`, `judge-bundle`, `node_modules` and generated mirrors.
2. Choose the snapshot id: use `git_sha` when present; if the envelope warns that the git sha was not resolved, use `bundle_hash` instead. Use the chosen value everywhere and never mix the two.
3. Distribute: copy the read-only bundle to three separate working directories (parallel where the harness allows). Each pass gets one bundle copy and no shared state.
4. Run the three passes. Each judge completes `references/isolation-checklist.md`, scores all five criteria from `references/rubric.md`, and writes `evals/judges/<sha>/judge-01.json` ... `judge-03.json` with the exact schema in the Output Contract.
5. Pre-validate each report before aggregation: exact criterion keys, scores within max, `total` equal to the criterion sum, at least one evidence entry, every deduction with a known criterion, a reason and non-negative points, `snapshot_sha` equal across reports.
6. Aggregate: call `eval.aggregate` with `reports: ["evals/judges/<sha>/judge-01.json", ...]`, `snapshot_sha: "<sha>"`, `kind: "judges"`, `persist: true`. Read `valid_judges`, `inconclusive`, `mean_total`, `criterion_means`, `score_variance`, `high_disagreement`, `common_deductions`, `likely_questions`, `top_improvements`, `dropped_invalid`, `dropped_conflict` and `aggregate_path`.
7. Calibrate before any stop condition: run a second independent set of three passes over the same frozen bundle, then call `eval.calibrate` with `run1` and `run2` report paths, `snapshot_sha`, `min_delta` (omit to use `configs/evaluation.yaml`, currently 0.5) and `persist: true`. Keep `calibration.retest_delta`, `calibration.noise_floor` and `calibration_path`.
8. Hand off: pass `mean_total` as the candidate and `calibration.noise_floor` to `eval.compare` with `mode: "judge"`; the controller follows its `decision`. Record model id, runtime version, rubric version, skill version and bundle hash with the run.

## Aggregation And Calibration

`eval.aggregate` applies these deterministic steps in order:

1. Schema validation: every report needs `judge_id`, `snapshot_sha`, all five criterion scores within max, `total` equal to the criterion sum, at least one evidence entry and well-formed deductions. Failures land in `dropped_invalid` with reasons.
2. Snapshot filter: reports whose `snapshot_sha` differs from the expected sha are dropped.
3. Conflict filter: `conflict_of_interest == true` reports are dropped and listed in `dropped_conflict`.
4. Majority: fewer than 2 valid reports sets `inconclusive: true` and returns a `partial` envelope; such a run is never a stop condition.
5. Arithmetic: `mean_total` (2 decimals), `criterion_means`, `score_variance` (sample variance, 4 decimals), `high_disagreement` (top quartile of the five per-criterion variances, alphabetical on ties), `common_deductions` (grouped by criterion and normalized reason, with count and summed points), `likely_questions` and `top_improvements` (normalized text counts).

`eval.calibrate` (test-retest over the same frozen snapshot):

1. Both runs need at least 2 valid reports and the same `snapshot_sha`, or the tool errors.
2. `retest_delta = abs(run1.mean_total - run2.mean_total)`.
3. `noise_floor = max(min_delta, 2 * retest_delta)`, persisted at `evals/calibration/<sha>/calibration.json`.
4. The floor is an engineering heuristic; judge runs are procedurally isolated, not statistically independent.

## Decision Matrix

| Signal | Action |
|---|---|
| `valid_judges >= 2` and `inconclusive == false` | Run is usable; proceed to calibration and compare |
| `inconclusive == true` or `valid_judges < 2` | Repair or rerun the failing passes; never use as a stop condition |
| `dropped_invalid` not empty | Quote the reasons; fix those reports and re-aggregate |
| `dropped_conflict` not empty | Keep them dropped; add a replacement pass only if validity falls below 2 |
| `high_disagreement` not empty | Report the criteria; never hand-adjust scores to reduce variance |
| No calibration for this snapshot/model | Run the second pass set first; scores are not comparable yet |
| `eval.calibrate` errors on validity | Both runs need at least 2 valid reports; rerun the deficient set |
| `git_sha` empty (bundle warning) | Use `bundle_hash` as `snapshot_sha`; state the substitution |
| `eval.compare` returns `target_reached` | Controller stops the loop; the judge adds nothing |
| `eval.compare` returns `rollback` or `stop` | Controller acts; the judge stays read-only |
| `known-limitations.md` looks stale | Judge what the frozen file says; never fetch the live tree to update it |
| Two reports cite one artifact in different words | Normalize the citation text; aggregation groups by text, not paths |

## Output Contract

Per judge report (`evals/judges/<sha>/judge-0N.json`):

```json
{
  "judge_id": "judge-01",
  "snapshot_sha": "abc123",
  "conflict_of_interest": false,
  "scores": {"value": 22, "result_quality": 18, "innovation": 11, "scalability": 17, "presentation_demo_qa": 16},
  "total": 84,
  "evidence": [{"criterion": "result_quality", "finding": "end-to-end flow works", "source": "smoke-results.json"}],
  "deductions": [{"criterion": "innovation", "points": 4, "reason": "Fly advantage not quantified"}],
  "questions": ["What happens if the local MCP fails?"],
  "top_improvements": ["Add Normal vs Fly A/B metrics"]
}
```

- Aggregate: `valid_judges`, `inconclusive`, `mean_total`, `criterion_means`, `score_variance`, `high_disagreement`, `common_deductions`, `likely_questions`, `top_improvements`, `dropped_invalid`, `dropped_conflict`, `aggregate_path`.
- Calibration: `retest_delta`, `noise_floor` (max of `min_delta` and `2 * retest_delta`), `calibration_path`.
- Provenance: model id, runtime version, rubric version, skill version, bundle hash, snapshot sha.
- The `eval.compare` decision and reason, quoted verbatim; never a decision invented in prose.

## Verification

- Confirm the bundle files are read-only, `bundle.json`, `git-sha.txt` and `rubric-version.txt` exist, and the `excluded` list contains secrets, logs, data and `evals/`.
- Confirm each report matches the schema and validates against `references/rubric.md`; totals equal the criterion sums.
- Confirm `eval.aggregate` returned `status == "ok"` (only an inconclusive run is `partial`) and that `aggregate_path` is present.
- Confirm `eval.calibrate` ran on the same snapshot sha with both runs at least 2 valid judges, and that `noise_floor >= min_delta`.
- Confirm no judge input contained the target score, previous results or another report.
- Report every envelope warning instead of hiding it.

## Failure Modes

| Symptom | Action |
|---|---|
| `eval.freeze_bundle` warns the git sha is unresolved | Use `bundle_hash` as the snapshot id and state it |
| `eval.aggregate` returns `partial` with `inconclusive` | Repair the dropped reports and re-aggregate; do not use the mean |
| `snapshot_sha` mismatch in a report | Re-issue that report against the correct frozen sha; rerun the pass |
| `eval.calibrate` error "calibration needs at least 2 valid reports per run" | Rerun the deficient pass set |
| `eval.calibrate` error "runs must score the same snapshot" | Re-freeze once and redo both pass sets |
| `conflict_of_interest == true` | Drop the report; add a replacement only if validity drops below 2 |
| `persist_error` in an envelope | Keep the inline data, fix the repository root or path, and state the missing artifact |
| Judges disagree strongly (`high_disagreement`) | Report the criteria and rely on the calibrated noise floor, not on adjusted scores |
| Model or rubric changed mid-run | Discard the mixed run and rerun every pass under one model and rubric version |

## Anti-Patterns

- Computing mean, sample variance or the noise floor by hand.
- Letting any judge see the target score, previous scores, `evals/` or the mutable tree.
- Changing rubric weights for one perspective; a perspective changes questions, never weights.
- Using a third report that was dropped for schema or conflict reasons.
- Comparing `mean_total` values from different snapshots, models or rubric versions.
- Calling the noise floor a confidence interval: it is an engineering heuristic.
- Making accept, rollback or stop statements in prose instead of quoting `eval.compare`.
- Editing code or re-freezing to make a score look better.

## References

- `references/rubric.md` - authoritative criteria, evidence expectations, report schema, aggregation and calibration rules.
- `references/worked-example.md` - a full three-report run with aggregation and calibration arithmetic.
- `references/isolation-checklist.md` - allowed inputs, clean-room setup and the per-judge procedure.
