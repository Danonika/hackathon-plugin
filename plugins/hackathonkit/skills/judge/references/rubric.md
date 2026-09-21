# Demo Day Rubric, Aggregation and Ranking

Regulation 9.4-9.6 of the Hackathon Agent Kit position. Demo Day judges evaluate the
presented product and demonstration; they do not re-check code or tests
(regulation 9.4: "без повторной технической проверки кода"). Technical
readiness is a separate rubric used by `$project-cost`.

## Criteria

| Criterion | Key | Max | What is evaluated |
|---|---|---:|---|
| Value of the solution | `value` | 25 | How real and understandable the problem is, how significant the benefit is for the target audience, and how obvious that benefit is from the presentation and demo. |
| Result and quality of the solution | `result_quality` | 20 | Whether the product or prototype actually performs the declared scenario: integrity of the solution and quality of the user-facing result, without a repeated technical code check. |
| Innovation | `innovation` | 15 | Whether the solution offers a new or non-standard approach, differs from the obvious alternatives and has its own advantage. |
| Development and scaling potential | `scalability` | 20 | Whether the project can be used after the hackathon and expand to new users, organizations, industries or scenarios. |
| Presentation, demo and Q&A | `presentation_demo_qa` | 20 | How clearly the team explained the problem, solution and advantages; how convincingly the result was demonstrated and how well the jury's questions were answered. |
| **Total** | | **100** | |

Weights are fixed. Perspective changes the questions, never the weights.

## What Evidence Looks Like

| Criterion | Acceptable evidence |
|---|---|
| value | TASK.md mapping, user-facing scenario in README, demo artifact, live demonstration |
| result_quality | end-to-end demo output, demo recording or script, user-visible artifacts |
| innovation | measurable difference vs a baseline (for example A/B Fly metrics), novel mechanism described in architecture.md |
| scalability | config surface, storage/backends section, post-hackathon usage path in README |
| presentation_demo_qa | demo recording or script, evidence-backed answer cards, observed Q&A |

Forbidden as scoring evidence: source code, test logs, smoke JSON and build logs.
A judge may watch the product run (regulation 9.3) and cite what was observed,
but never award or deduct points for code internals.

## Per-Judge Output

```json
{
  "judge_id": "judge-04",
  "snapshot_sha": "abc123",
  "conflict_of_interest": false,
  "scores": {"value": 22, "result_quality": 18, "innovation": 11, "scalability": 17, "presentation_demo_qa": 16},
  "total": 84,
  "evidence": [{"criterion": "result_quality", "finding": "the demo runs the full scenario on real input", "source": "demo-artifacts/run.json"}],
  "deductions": [{"criterion": "innovation", "points": 4, "reason": "Fly advantage not quantified"}],
  "questions": ["What happens if the local MCP fails?", "Why Fly instead of plain tool selection?"],
  "top_improvements": ["Add Normal vs Fly A/B metrics", "Show fallback during the demo"]
}
```

## Aggregation

Run `eval.aggregate` with the report paths (or inline `reports_json`) and the
bundle sha. The tool implements exactly:

1. Validate every report: required fields, known criteria, scores within their max, `total` matching the criterion sum, at least one evidence entry, deductions with a known criterion and a reason. Malformed reports are dropped and listed in `dropped_invalid`.
2. Drop `conflict_of_interest == true` and reports whose `snapshot_sha` differs from the bundle.
3. Require `min_valid_judges` valid reports (config `configs/evaluation.yaml`, default 3, regulation 9.5); otherwise `inconclusive` is true. A missing report is never counted as zero: run a substitute pass on the same frozen bundle with comparable conditions and re-aggregate.
4. `mean_total` is the unrounded mean of report totals (six decimals); `mean_total_rounded` (two decimals) is for the published ranking only.
5. `score_variance` = sample variance `sum((si - mean)^2) / (n - 1)` for `n > 1`.
6. `common_deductions` grouped by criterion and normalized reason (count, summed points); `likely_questions` and `top_improvements` counted by normalized text.
7. `high_disagreement` = the top quartile of the five criteria by per-criterion sample variance (zero-variance criteria are never listed).

The aggregate is stored at `evals/judges/<sha>/aggregate.json`; the envelope
returns `aggregate_path`, `rubric_version` and `min_valid_judges`.

## Ranking and Tie-Breaks (Regulation 9.6)

`eval.rank` orders aggregates from the same rubric version:

1. Higher unrounded `mean_total` first; ordering never uses the rounded value.
2. Equal totals: higher `result_quality` average, then `value`, then `presentation_demo_qa`.
3. Still equal: the tie is `tie_unresolved`. A commission of three jury members without a conflict of interest for the tied snapshots votes once each, compares only the published criteria, and the outcome is recorded through `eval.rank`'s `commission` argument (members, votes, grounds). Composition, votes and grounds are persisted under `evals/rankings/`.

## Calibration (test-retest)

Before using judge scores as a stop condition, run `eval.calibrate` with two report sets over the same frozen snapshot:

1. `retest_delta = abs(mean_run_1 - mean_run_2)` on unrounded means.
2. `noise_floor = max(configured_min_delta, 2 * retest_delta)`.
3. Both runs need at least `min_valid_judges` valid reports; otherwise the tool errors and asks for substitute judges.
4. The result is stored at `evals/calibration/<sha>/calibration.json` and returned as `calibration.noise_floor`; pass that value to `eval.compare` for every judge decision.
5. Record model id, runtime version, rubric version, skill version, bundle hash, and any substitute or commission decisions.

The noise floor is an engineering heuristic, not a statistical confidence interval. Judge runs are procedurally isolated, not statistically independent.
