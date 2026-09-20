# evals/ layout

Evaluation artifacts are written by the `eval.*` MCP tools. Data files are
git-ignored; this README is the committed description of the layout.

| Path | Written by | Content |
|---|---|---|
| `evals/judges/<sha>/judge-*.json` | judge skill | one report per isolated judge run (schema in `judge/references/rubric.md`) |
| `evals/judges/<sha>/aggregate.json` | `eval.aggregate` | validated aggregate: mean, variance, high disagreement, common deductions, likely questions |
| `evals/calibration/<sha>/calibration.json` | `eval.calibrate` | `retest_delta` and `noise_floor` from two runs over one frozen snapshot |
| `evals/decisions/<sha>/<mode>-<ts>.json` | `eval.compare` | deterministic loop decision (`target_reached`, `accept`, `rollback`, `stop`) |
| `evals/project-cost/<sha>.json` | project-cost skill | technical readiness score with evidence |
| `evals/roast/<sha>/`, `evals/snapshots/` | roast / freeze skills | critic findings and frozen inputs |

Rules:

- The LLM judges; the tools do the arithmetic. Never compute means, variance or
  noise floors by hand.
- A run without a valid majority (at least 2 of the 3 judge reports) is
  inconclusive and can never be a stop condition.
- Every stored result carries the rubric version and the bundle hash of the
  snapshot it judged; scores from different bundles are never compared.
- When Postgres is configured the same records are mirrored to the `eval_runs`
  table for cross-run queries.
