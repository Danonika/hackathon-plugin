# evals/ layout

Evaluation artifacts are written by the `eval.*` MCP tools. Data files are
git-ignored; this README is the committed description of the layout.

| Path | Written by | Content |
|---|---|---|
| `evals/judges/<sha>/judge-*.json` | judge skill | one report per isolated judge run (schema in `judge/references/rubric.md`) |
| `evals/judges/<sha>/aggregate.json` | `eval.aggregate` | validated aggregate: mean, variance, high disagreement, common deductions, likely questions |
| `evals/calibration/<sha>/calibration.json` | `eval.calibrate` | `retest_delta` and `noise_floor` from two runs over one frozen snapshot |
| `evals/decisions/<sha>/<mode>-<ts>.json` | `eval.compare` | deterministic loop decision (`target_reached`, `accept`, `rollback`, `stop`) |
| `evals/plans/<slug>.json` | `plan.create` / `plan.update` | staged build plan (S0-S6) with statuses, evidence and revision |
| `evals/rankings/ranking-<ts>.json` | `eval.rank` | regulation 9.6 ranking and any recorded commission vote |
| `evals/project-cost/<sha>.json` | project-cost skill | technical readiness score with evidence |
| `evals/roast/<sha>/`, `evals/snapshots/` | roast / freeze skills | critic findings and frozen inputs |
| `evals/rehearsal/<sha>/rehearsal-<ts>.json` | `cmd/hackathonkit-rehearse` | clean-checkout end-to-end rehearsal: per-step status, duration and artifacts |

Rules:

- The LLM judges; the tools do the arithmetic. Never compute means, variance or
  noise floors by hand.
- A run with fewer than `min_valid_judges` valid judge reports (default 3,
  regulation 9.5) is inconclusive; substitute passes replace dropped or
  conflicted reports, and a missing report is never counted as zero.
- Every stored result carries the rubric version and the bundle hash of the
  snapshot it judged; scores from different bundles are never compared.
- When Postgres is configured the same records are mirrored to the `eval_runs`
  table for cross-run queries.
