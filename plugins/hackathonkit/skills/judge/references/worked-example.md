# Worked Example: Aggregate, Calibrate and Rank

One complete run: three isolated passes, one conflicted report, a substitute
pass, aggregation, calibration, ranking and the possible `eval.compare`
outcomes. The schema and rules stay authoritative in `references/rubric.md`.

## Setup

`eval.freeze_bundle(out="judge-bundle", profile="demo", read_only=true,
model="gpt-5-codex")` returned `git_sha: "abc123"`,
`bundle_hash: "9f2c...e1"`. The snapshot id is `abc123` (`git_sha` wins when
present). Four pass copies were prepared and the reports were written to
`evals/judges/abc123/`.

## Reports

judge-01:

```json
{
  "judge_id": "judge-01", "snapshot_sha": "abc123", "conflict_of_interest": false,
  "scores": {"value": 22, "result_quality": 17, "innovation": 11, "scalability": 16, "presentation_demo_qa": 15},
  "total": 81,
  "evidence": [
    {"criterion": "value", "finding": "TASK.md scenario is demoed end-to-end", "source": "demo-artifacts/run.md"},
    {"criterion": "result_quality", "finding": "the demo completes on real input", "source": "demo-artifacts/run.md"}
  ],
  "deductions": [
    {"criterion": "innovation", "points": 4, "reason": "Fly advantage not quantified"},
    {"criterion": "result_quality", "points": 3, "reason": "No negative-input demonstration"}
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
    {"criterion": "scalability", "finding": "post-hackathon usage path documented", "source": "README.md"},
    {"criterion": "presentation_demo_qa", "finding": "Q&A cards exist", "source": "README.md"}
  ],
  "deductions": [
    {"criterion": "innovation", "points": 3, "reason": "Fly advantage not quantified"}
  ],
  "questions": ["What happens if the local MCP fails?", "Why not use a cloud model?"],
  "top_improvements": ["Add Normal vs Fly A/B metrics", "Show fallback during the demo"]
}
```

judge-03 declared `"conflict_of_interest": true`; its scores are never used and
it must not vote on ties involving this snapshot.

## Substitute Pass (Regulation 9.5)

With only two valid reports, the first aggregation is inconclusive:

```text
valid_judges: 2 < min_valid_judges: 3  ->  inconclusive: true, partial envelope
```

A substitute pass `judge-04` runs on the same frozen bundle with comparable
conditions (scores: value 21, result_quality 17, innovation 11, scalability 16,
presentation_demo_qa 15 = 80). The run notes record: replaced `judge-03`,
reason "conflict of interest". Re-aggregating with three valid reports:

```text
eval.aggregate(
  reports=["evals/judges/abc123/judge-01.json",
           "evals/judges/abc123/judge-02.json",
           "evals/judges/abc123/judge-04.json"],
  snapshot_sha="abc123", kind="judges", persist=true)
```

Tool-computed result (mirrored from `evals/judges/abc123/aggregate.json`):

| Field | Value |
|---|---|
| `rubric_version` | `2.0.0` |
| `min_valid_judges` | 3 |
| `valid_judges` | 3 (usable; `inconclusive: false`) |
| `dropped_conflict` | `["judge-03"]` |
| `mean_total` | 81 = (81 + 82 + 80) / 3, unrounded |
| `mean_total_rounded` | 81 (published ranking value) |
| `criterion_means` | value 21.33, result_quality 17.33, innovation 10.67, scalability 16.33, presentation_demo_qa 15.33 |
| `score_variance` | 1 = (0^2 + 1^2 + (-1)^2) / 2 |
| `common_deductions` | innovation "Fly advantage not quantified" count 2 points 7; result_quality "No negative-input demonstration" count 1 points 3 |
| `likely_questions` | "What happens if the local MCP fails?" count 2; "Why not use a cloud model?" count 1 |
| `aggregate_path` | `evals/judges/abc123/aggregate.json` |

A dropped report is never zero: the mean uses the three valid scores only.

## Calibration

A second independent pass set over the same frozen bundle produced valid totals
79, 80 and 78. Each run needs at least `min_valid_judges` valid reports and the
same `snapshot_sha`:

```text
eval.calibrate(
  run1=[".../judge-01.json", ".../judge-02.json", ".../judge-04.json"],
  run2=[".../judge-05.json", ".../judge-06.json", ".../judge-07.json"],
  snapshot_sha="abc123", min_delta=0.5, persist=true)
```

| Field | Value |
|---|---|
| `run1_mean` | 81 |
| `run2_mean` | 79 |
| `retest_delta` | 2 = abs(81 - 79) |
| `noise_floor` | 4 = max(0.5, 2 * 2) |
| `calibration_path` | `evals/calibration/abc123/calibration.json` |

Every judge stop condition now uses `noise_floor = 4`, not a hand-picked delta.

## Ranking (Regulation 9.6)

Two candidate snapshots with equal totals `81` and equal `result_quality` (both
17.33) are ranked by `value`: snapshot `abc123` (21.33) beats `def456` (20.67),
so `abc123` is first and `def456` carries `tie_break: "value"`. If every
published criterion were equal, `eval.rank` returns `tie_unresolved` and a
commission of three non-conflicted jury members votes once each; the result is
recorded via the `commission` argument and persisted under `evals/rankings/`.

## The Three Outcomes

`eval.compare(mode="judge", candidate=<unrounded mean>, best=<previous mean>,
target=90, noise_floor=4)`:

| Scenario | Tool decision | Reason string |
|---|---|---|
| candidate 81, best 80 | `stop` | "improvement 1 below noise floor 4; stop iterating on this axis" |
| candidate 81, best 76 | `accept` | "accepted with improvement 5 above noise floor 4" |
| candidate 81, best 86 | `rollback` | "judge regression 5 exceeds noise floor 4; rollback" |
| candidate 91, target 90 | `target_reached` | "target 90 reached with 91" |
| any candidate, `mandatory_scenario_failed: true` | `rollback` | "mandatory scenario failed; rollback and stop" |

The judge skill reports the aggregate, the calibration and the raw numbers;
`eval.compare` returns the decision and the controller performs the rollback.
