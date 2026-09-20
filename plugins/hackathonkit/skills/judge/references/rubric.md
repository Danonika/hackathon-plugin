# Demo Day Rubric and Aggregation

## Criteria

| Criterion | Key | Max |
|---|---|---:|
| Value of the solution | `value` | 25 |
| Result and quality of the solution | `result_quality` | 20 |
| Innovation | `innovation` | 15 |
| Development and scaling potential | `scalability` | 20 |
| Presentation, demo and Q&A | `presentation_demo_qa` | 20 |
| **Total** | | **100** |

Weights are fixed. Perspective changes the questions, never the weights.

## What Evidence Looks Like

| Criterion | Acceptable evidence |
|---|---|
| value | TASK.md mapping, user-facing scenario in README, demo artifact |
| result_quality | test results, smoke results, end-to-end scenario output |
| innovation | measurable difference vs a baseline (for example A/B Fly metrics), novel mechanism described in architecture.md |
| scalability | clean-room result, config surface, storage/backends section |
| presentation_demo_qa | demo recording or script, evidence-backed answer cards |

## Per-Judge Output

```json
{
  "judge_id": "judge-04",
  "snapshot_sha": "abc123",
  "conflict_of_interest": false,
  "scores": {"value": 22, "result_quality": 18, "innovation": 11, "scalability": 17, "presentation_demo_qa": 16},
  "total": 84,
  "evidence": [{"criterion": "result_quality", "finding": "end-to-end flow works", "source": "smoke-results.json"}],
  "deductions": [{"criterion": "innovation", "points": 4, "reason": "Fly advantage not quantified"}],
  "questions": ["What happens if the local MCP fails?", "Why Fly instead of plain tool selection?"],
  "top_improvements": ["Add Normal vs Fly A/B metrics", "Show fallback during the demo"]
}
```

## Aggregation

Run `eval.aggregate` with the report paths (or inline `reports_json`) and the bundle sha. The tool implements exactly:

1. Validate every report: required fields, known criteria, scores within their max, `total` matching the criterion sum, at least one evidence entry, deductions with a known criterion and a reason. Malformed reports are dropped and listed in `dropped_invalid`.
2. Drop `conflict_of_interest == true` and reports whose `snapshot_sha` differs from the bundle.
3. Require a majority of the 3 judge runs to be valid (at least 2, `valid_judges`); otherwise `inconclusive` is true and the run cannot be a stop condition.
4. `mean_total` = mean of report totals.
5. `score_variance` = sample variance `sum((si - mean)^2) / (n - 1)` for `n > 1`.
6. `common_deductions` grouped by criterion and normalized reason (count, summed points); `likely_questions` and `top_improvements` counted by normalized text.
7. `high_disagreement` = the top quartile of the five criteria by per-criterion sample variance (zero-variance criteria are never listed).

The aggregate is stored at `evals/judges/<sha>/aggregate.json`; the envelope returns `aggregate_path`.

## Calibration (test-retest)

Before using judge scores as a stop condition, run `eval.calibrate` with two report sets over the same frozen snapshot:

1. `retest_delta = abs(mean_run_1 - mean_run_2)`.
2. `noise_floor = max(configured_min_delta, 2 * retest_delta)`.
3. The result is stored at `evals/calibration/<sha>/calibration.json` and returned as `calibration.noise_floor`; pass that value to `eval.compare` for every judge decision.
4. Record model id, runtime version, rubric version, skill version, bundle hash.

The noise floor is an engineering heuristic, not a statistical confidence interval. Judge runs are procedurally isolated, not statistically independent.
