# Worked Example: Aggregate and Calibrate

One complete run: three isolated passes, one conflicted report, aggregation,
calibration and the three possible `eval.compare` outcomes. The schema and rules
stay authoritative in `references/rubric.md`.

## Setup

`eval.freeze_bundle(out="judge-bundle", read_only=true, model="gpt-5-codex")`
returned `git_sha: "abc123"`, `bundle_hash: "9f2c...e1"`. The snapshot id is
`abc123` (`git_sha` wins when present). Three pass copies were prepared and the
reports were written to `evals/judges/abc123/`.

## Reports

judge-01:

```json
{
  "judge_id": "judge-01", "snapshot_sha": "abc123", "conflict_of_interest": false,
  "scores": {"value": 22, "result_quality": 17, "innovation": 11, "scalability": 16, "presentation_demo_qa": 15},
  "total": 81,
  "evidence": [
    {"criterion": "value", "finding": "TASK.md scenario is demoed end-to-end", "source": "demo-artifacts/run.md"},
    {"criterion": "result_quality", "finding": "smoke flow passes", "source": "smoke-results.json"}
  ],
  "deductions": [
    {"criterion": "innovation", "points": 4, "reason": "Fly advantage not quantified"},
    {"criterion": "result_quality", "points": 2, "reason": "No negative-input test"}
  ],
  "questions": ["What happens if the local MCP fails?"],
  "top_improvements": ["Add Normal vs Fly A/B metrics"]
}
```

judge-02 (same sha):

```json
{
  "judge_id": "judge-02", "snapshot_sha": "abc123", "conflict_of_interest": false,
  "scores": {"value": 21, "result_quality": 18, "innovation": 10, "scalability": 17, "presentation_demo_qa": 16},
  "total": 82,
  "evidence": [
    {"criterion": "scalability", "finding": "clean-room run documented", "source": "known-limitations.md"},
    {"criterion": "presentation_demo_qa", "finding": "Q&A cards exist", "source": "README.md"}
  ],
  "deductions": [
    {"criterion": "innovation", "points": 3, "reason": "Fly advantage not quantified"}
  ],
  "questions": ["What happens if the local MCP fails?", "Why not use a cloud model?"],
  "top_improvements": ["Add Normal vs Fly A/B metrics", "Show fallback during the demo"]
}
```

judge-03 declared `"conflict_of_interest": true`; its scores are never used.

## Aggregation

```text
eval.aggregate(
  reports=["evals/judges/abc123/judge-01.json",
           "evals/judges/abc123/judge-02.json",
           "evals/judges/abc123/judge-03.json"],
  snapshot_sha="abc123", kind="judges", persist=true)
```

Tool-computed result (mirrored from `evals/judges/abc123/aggregate.json`):

| Field | Value |
|---|---|
| `valid_judges` | 2 (`>= 2`, so the run is usable; `inconclusive: false`) |
| `dropped_conflict` | `["judge-03"]` |
| `mean_total` | 81.5 = (81 + 82) / 2 |
| `criterion_means` | value 21.5, result_quality 17.5, innovation 10.5, scalability 16.5, presentation_demo_qa 15.5 |
| `score_variance` | 0.5 = ((81-81.5)^2 + (82-81.5)^2) / (2-1) |
| `high_disagreement` | `["innovation", "presentation_demo_qa"]` (tie in variance 0.5; top quartile = 2 criteria, alphabetical tie-break) |
| `common_deductions` | innovation "Fly advantage not quantified" count 2 points 7; result_quality "No negative-input test" count 1 points 2 |
| `likely_questions` | "What happens if the local MCP fails?" count 2; "Why not use a cloud model?" count 1 |
| `top_improvements` | "Add Normal vs Fly A/B metrics" count 2; "Show fallback during the demo" count 1 |
| `aggregate_path` | `evals/judges/abc123/aggregate.json` |

A third invalid or conflicted report does not void the run; a second one would
(`valid_judges < 2` -> `inconclusive: true`, `partial` envelope, no stop
condition).

## Calibration

A second independent pass set over the same frozen bundle produced valid totals
79 and 80 (one conflicted report again). Two runs are required; each must have
at least 2 valid reports and the same `snapshot_sha`.

```text
eval.calibrate(
  run1=["evals/judges/abc123/judge-01.json", "evals/judges/abc123/judge-02.json"],
  run2=["evals/judges/abc123/judge-04.json", "evals/judges/abc123/judge-05.json"],
  snapshot_sha="abc123", min_delta=0.5, persist=true)
```

| Field | Value |
|---|---|
| `run1_mean` | 81.5 |
| `run2_mean` | 79.5 |
| `retest_delta` | 2 = abs(81.5 - 79.5) |
| `noise_floor` | 4 = max(0.5, 2 * 2) |
| `calibration_path` | `evals/calibration/abc123/calibration.json` |

Every judge stop condition now uses `noise_floor = 4`, not a hand-picked delta.
The floor is an engineering heuristic, not a confidence interval.

## The Three Outcomes

`eval.compare(mode="judge", candidate=<mean_total>, best=<previous mean>,
target=90, noise_floor=4)`:

| Scenario | Tool decision | Reason string |
|---|---|---|
| candidate 81.5, best 80 | `stop` | "improvement 1.5 below noise floor 4; stop iterating on this axis" |
| candidate 81.5, best 76 | `accept` | "accepted with improvement 5.5 above noise floor 4" |
| candidate 81.5, best 86 | `rollback` | "judge regression 4.5 exceeds noise floor 4; rollback" |
| candidate 91, target 90 | `target_reached` | "target 90 reached with 91" |
| any candidate, `mandatory_scenario_failed: true` | `rollback` | "mandatory scenario failed; rollback and stop" |

The judge skill stops at the first two columns: it reports the aggregate, the
calibration and the raw numbers. `eval.compare` returns the decision, and the
controller performs the rollback.
