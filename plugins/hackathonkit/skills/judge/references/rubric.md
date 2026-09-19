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

1. Validate schemas; drop malformed reports.
2. Drop `conflict_of_interest == true`.
3. Require at least 3 valid reports; otherwise the run is inconclusive.
4. `mean = sum(si) / n` over totals.
5. `score_variance = sum((si - mean)^2) / (n - 1)` for `n > 1`.
6. Group deductions by reason; count question frequency across judges.
7. `high_disagreement` = criteria whose per-criterion variance is in the top quartile.

## Calibration (test-retest)

Before using judge scores as a stop condition:

1. Score one frozen snapshot twice with no changes.
2. `retest_delta = abs(mean_run_1 - mean_run_2)`.
3. `noise_floor = max(configured_min_delta, 2 * retest_delta)`.
4. Record model id, runtime version, rubric version, skill version, bundle hash.

The noise floor is an engineering heuristic, not a statistical confidence interval. Judge runs are procedurally isolated, not statistically independent.
