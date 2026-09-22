---
name: judge
description: "Use when a frozen Hackathon Agent Kit snapshot must be scored with the official Demo Day rubric by at least 3 valid non-conflicted judge runs, with substitute passes replacing dropped reports and deterministic ranking/tie-breaks. Produces validated per-judge reports, an eval.aggregate result, a calibrated noise floor and an eval.rank ranking. Explicit-only: never invoke implicitly. Do not use for technical readiness ($project-cost) or critic findings ($roast-project)."
metadata:
  version: "3.0.0"
  origin: hackathonkit
  category: evaluation
tools:
  - eval.freeze_bundle
  - eval.aggregate
  - eval.calibrate
  - eval.rank
---

# Judge

Score a frozen, read-only snapshot with procedurally isolated judge passes; the tools own the arithmetic, the substitute-judge rule, ranking and the stop condition. Demo Day judges evaluate the presented product and demo, never re-check code (regulation 9.4).

## When To Use

- A candidate snapshot is frozen and an official Demo Day score is needed.
- Before final submission, to collect likely jury questions, common deductions and top improvements.
- To calibrate judge variance: score one frozen snapshot twice and derive the noise floor.
- Do not use for technical readiness (`$project-cost`), qualitative criticism (`$roast-project`) or accept/rollback decisions (the controller calls `eval.compare`); judges only produce scores.

## Preconditions

- The candidate passes its mandatory scenario; a broken demo wastes a judge run.
- Demo evidence exists for the frozen snapshot: `demo-artifacts/`, a demo script or recording, `README.md`, `architecture.md`, `known-limitations.md`. Source and test logs are not Demo Day evidence (regulation 9.4).
- `configs/evaluation.yaml` is read once: `judges: 3`, `min_valid_judges: 3`, `demo_day_target: 90`, `min_delta: 0.5`, `max_judge_rounds: 2`.
- One calibration exists for the current model and rubric version; reuse its noise floor until either changes.
- The target score lives with the controller and never enters a judge prompt.

## Core Principles

1. Frozen only: judge a snapshot produced by `eval.freeze_bundle` with `profile: "demo"`, never the live working tree, and never edit code during a pass.
2. At least three valid scores (regulation 9.5): each project needs three valid reports from non-conflicted judges. A dropped or conflicted report is never counted as zero; a substitute pass runs on the same frozen bundle with comparable conditions.
3. Isolated passes: same rubric and weights, distinct perspectives (01 product value/innovation, 02 result quality and demo integrity, 03 adversarial Q&A/scaling/presentation). Isolated procedurally, not statistically independent.
4. Blind judges: a judge never sees the target score, previous judge scores, the aggregate, other reports, `evals/`, logs or the mutable repository.
5. No evidence, no points: every deduction cites a demo artifact, README section, presentation record or observed live demonstration; observed facts and assumptions are separated.
6. `eval.aggregate` owns the arithmetic and `eval.rank` owns ordering; never compute means, variance, noise floors or tie-breaks by hand.
7. `eval.compare` owns accept/rollback/stop; the judge hands over the unrounded `mean_total` and the calibrated noise floor, nothing more.
8. Scores from different snapshots, rubric versions or models are never compared; the rubric version is frozen for the run.

## Workflow

1. Freeze the candidate: call `eval.freeze_bundle` with `out: "data/judge-bundles"`, `profile: "demo"` (TASK, README, architecture, demo artifacts, limitations; no source or test logs), `read_only: true`, `model: "<model-id>"` and extra `exclude` patterns only when something must be kept out. Keep `bundle_path`, `bundle_hash`, `git_sha`, `profile`, `files` and the `excluded` list.
2. Choose the snapshot id: use `git_sha` when present; if the envelope warns that the git sha was not resolved, use `bundle_hash` instead. Use the chosen value everywhere and never mix the two.
3. Distribute: copy the read-only bundle to separate working directories, one per pass (parallel where the harness allows). Each pass gets one bundle copy and no shared state. Start with `judges` (default 3) passes.
4. Run the passes. Each judge completes `references/isolation-checklist.md`, scores all five criteria from `references/rubric.md` against demo evidence only, and writes `evals/judges/<sha>/judge-01.json` ... `judge-0N.json` with the exact schema in the Output Contract.
5. Pre-validate each report before aggregation: exact criterion keys, scores within max, `total` equal to the criterion sum, at least one evidence entry, every deduction with a known criterion, a reason and non-negative points, `snapshot_sha` equal across reports.
6. Aggregate: call `eval.aggregate` with `reports: ["evals/judges/<sha>/judge-01.json", ...]`, `snapshot_sha: "<sha>"`, `kind: "judges"`, `persist: true`. Read `valid_judges`, `min_valid_judges`, `inconclusive`, `mean_total` (unrounded) plus `mean_total_rounded`, `criterion_means`, `score_variance`, `high_disagreement`, `common_deductions`, `likely_questions`, `top_improvements`, `dropped_invalid`, `dropped_conflict` and `aggregate_path`.
7. Substitute loop (regulation 9.5): when `valid_judges < min_valid_judges` (default 3), run one replacement isolated pass per missing valid report on the same frozen bundle, record which judge was replaced and why in the run notes, and re-aggregate. A missing or conflicted report is never zero. Repeat until `valid_judges >= min_valid_judges` or the time budget is exhausted; a run that stays below the minimum is `inconclusive` and is never a stop condition.
8. Calibrate before any stop condition: run a second independent pass set over the same frozen bundle, then call `eval.calibrate` with `run1` and `run2` report paths, `snapshot_sha`, `min_delta` (omit to use `configs/evaluation.yaml`, currently 0.5) and `persist: true`. Keep `calibration.retest_delta`, `calibration.noise_floor` and `calibration_path`.
9. Rank candidates when more than one snapshot is being compared: call `eval.rank` with the aggregate paths. It applies the regulation 9.6 order (unrounded mean, then `result_quality`, `value`, `presentation_demo_qa`). If it returns `tie_unresolved`, form a commission of three jury members without a conflict of interest for the tied snapshots, collect one vote each, and record the result via the `commission` argument (members, votes, grounds) so the protocol is persisted.
10. Hand off: pass the unrounded `mean_total` as the candidate and `calibration.noise_floor` to `eval.compare` with `mode: "judge"`; the controller follows its `decision`. Record model id, runtime version, rubric version, skill version, bundle hash and any substitute/commission decisions with the run.

## Aggregation And Calibration

`eval.aggregate` applies these deterministic steps in order:

1. Schema validation: every report needs `judge_id`, `snapshot_sha`, all five criterion scores within max, `total` equal to the criterion sum, at least one evidence entry and well-formed deductions. Failures land in `dropped_invalid` with reasons.
2. Snapshot filter: reports whose `snapshot_sha` differs from the expected sha are dropped.
3. Conflict filter: `conflict_of_interest == true` reports are dropped and listed in `dropped_conflict`.
4. Minimum: fewer than `min_valid_judges` (config, default 3) valid reports sets `inconclusive: true` and returns a `partial` envelope; run substitute passes and re-aggregate. Dropped reports are never zeros, and such a run is never a stop condition.
5. Arithmetic: `mean_total` unrounded (six decimals) plus `mean_total_rounded` for publication, `criterion_means` unrounded plus `criterion_means_rounded`, `score_variance` (sample variance, 4 decimals), `high_disagreement` (top quartile of the five per-criterion variances, alphabetical on ties), `common_deductions` (grouped by criterion and normalized reason, with count and summed points), `likely_questions` and `top_improvements` (normalized text counts).

`eval.calibrate` (test-retest over the same frozen snapshot):

1. Both runs need at least `min_valid_judges` valid reports and the same `snapshot_sha`, or the tool errors and asks for substitute judges.
2. `retest_delta = abs(run1.mean_total - run2.mean_total)` on unrounded means.
3. `noise_floor = max(min_delta, 2 * retest_delta)`, persisted at `evals/calibration/<sha>/calibration.json`.
4. The floor is an engineering heuristic; judge runs are procedurally isolated, not statistically independent.

## Ranking (Regulation 9.6)

`eval.rank` orders two or more aggregates deterministically:

1. Higher unrounded `mean_total` first. The published ranking may round to two decimals; ordering never uses the rounded value.
2. Equal totals: higher `criterion_means.result_quality`, then `value`, then `presentation_demo_qa`.
3. Still equal: the tie is `tie_unresolved`; a commission of three jury members without a conflict of interest for those snapshots votes once each, compares only the published criteria, and the result is recorded through the `commission` argument (`members`, `votes`, `grounds`) in `evals/rankings/`.
4. Aggregates from another rubric version or an inconclusive run are rejected, not ranked.

## Decision Matrix

| Signal | Action |
|---|---|
| `valid_judges >= min_valid_judges` and `inconclusive == false` | Run is usable; proceed to calibration and compare |
| `inconclusive == true` or `valid_judges < min_valid_judges` | Run substitute passes on the same frozen bundle until the minimum is met or the time budget ends; never use as a stop condition |
| `dropped_invalid` not empty | Quote the reasons; fix those reports and re-aggregate (or replace the pass) |
| `dropped_conflict` not empty | Keep them dropped; run a replacement pass for each conflicted report |
| More than one candidate snapshot to compare | Call `eval.rank`; never order by hand |
| `tie_unresolved` in the ranking | Record a three-member non-conflicted commission vote via `eval.rank`'s `commission` argument |
| `high_disagreement` not empty | Report the criteria; never hand-adjust scores to reduce variance |
| No calibration for this snapshot/model | Run the second pass set first; scores are not comparable yet |
| `eval.calibrate` errors on validity | Both runs need at least `min_valid_judges` valid reports; run substitute passes |
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
- Confirm `valid_judges >= min_valid_judges` and `status == "ok"`; an inconclusive run is `partial` and requires substitute passes.
- Confirm each substitute pass used the same frozen bundle and that the run notes name the replaced judge and the reason.
- Confirm `eval.calibrate` ran on the same snapshot sha with both runs at least `min_valid_judges` valid judges, and that `noise_floor >= min_delta`.
- Confirm any multi-candidate ordering came from `eval.rank`, and that unresolved ties have a recorded commission vote with three non-conflicted members.
- Confirm no judge input contained the target score, previous results, source code or another report.
- Report every envelope warning instead of hiding it.

## Failure Modes

| Symptom | Action |
|---|---|
| `eval.freeze_bundle` warns the git sha is unresolved | Use `bundle_hash` as the snapshot id and state it |
| `eval.aggregate` returns `partial` with `inconclusive` | Run substitute passes until `valid_judges >= min_valid_judges`, then re-aggregate; do not use the mean |
| `snapshot_sha` mismatch in a report | Re-issue that report against the correct frozen sha; rerun the pass |
| `eval.calibrate` error "needs at least N valid reports ... assign substitute judges" | Run substitute passes for the deficient set |
| `eval.calibrate` error "runs must score the same snapshot" | Re-freeze once and redo both pass sets |
| `conflict_of_interest == true` | Drop the report and run a replacement pass; a recused judge never votes on ties involving that snapshot |
| `eval.rank` rejects a mixed rubric version | Re-aggregate every candidate under the current rubric version before ranking |
| `persist_error` in an envelope | Keep the inline data, fix the repository root or path, and state the missing artifact |
| Judges disagree strongly (`high_disagreement`) | Report the criteria and rely on the calibrated noise floor, not on adjusted scores |
| Model or rubric changed mid-run | Discard the mixed run and rerun every pass under one model and rubric version |

## Anti-Patterns

- Computing mean, sample variance, the noise floor or a tie-break by hand.
- Counting a missing, invalid or conflicted report as zero.
- Stopping at two valid reports because "a majority" is reached.
- Letting a Demo Day judge re-check source code or test logs (regulation 9.4).
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
