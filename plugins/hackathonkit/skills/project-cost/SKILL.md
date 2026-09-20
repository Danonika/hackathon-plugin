---
name: project-cost
description: "Use when an evidence-only technical readiness score (0-100) is needed for a candidate snapshot, with criterion scores, blocking issues and an eval.compare decision. Explicit-only: never invoke implicitly. Do not use for Demo Day judging ($judge) or critic findings ($roast-project)."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: evaluation
tools:
  - eval.compare
---

# Project Cost

A technical readiness score, not a price: every point must trace to an artifact and the loop decision comes from `eval.compare`.

## When To Use

- After an improvement pass, to decide whether the candidate is kept or rolled back.
- Before a judge round, as a cheap deterministic technical gate.
- After a clean-room run, to record the final readiness score.
- Do not use for the official Demo Day rubric (`$judge`), for adversarial criticism (`$roast-project`), or to price work; this is an internal gate only.

## Evidence Collection

| Criterion | Read | Minimum acceptable evidence |
|---|---|---|
| case_and_functionality | `TASK.md`, demo script, end-to-end output | One scenario runnable start to finish |
| technical_implementation | test logs, contract tests, handler map | Test output with no skipped critical cases |
| readme_and_docs | `README.md`, setup flow | Commands that run, plus a labeled limitation |
| reproducibility | clean-room report, docker build log, setup/doctor output | Fresh checkout reaches a working smoke |
| reliability_and_security | fallback paths, malformed-input tests, secret scan | No key in the tree; path traversal rejected |

Read-only rule: inspect the candidate snapshot; never create, fix or regenerate an artifact while scoring. An artifact generated during scoring is evidence of nothing.

## Loop Budget

From `configs/evaluation.yaml`:

| Setting | Value | Effect here |
|---|---|---|
| `technical_target` | 95 | Used when `target` is omitted from `eval.compare` |
| `min_delta` | 0.5 | Default noise floor for the technical loop |
| `max_technical_iterations` | 2 | Stop scoring after the cap even if improvement continues |
| `max_judge_rounds` | 2 | Judge rounds available once the technical gate passes |

The loop walks candidate -> score -> `eval.compare` -> accept and continue, or rollback/stop. When the cap is reached, freeze the best snapshot and move to `$judge`.

## Core Principles

1. Evidence-only: a criterion with no artifact behind it scores zero for the unproven part; claimed functionality earns nothing.
2. Fixed weights: the five criteria and their maxima in `references/rubric.md` never change between runs or candidates.
3. Working code beats documentation; documentation beats claims.
4. Blocking issues and deductions are separate: a blocking issue prevents a demo, a deduction merely loses points.
5. The clean-room gate is part of reproducibility; without it that criterion cannot earn full points.
6. `eval.compare` owns accept, rollback and stop; never state a loop decision in prose.
7. This score never reaches a judge; it gates the builder loop only.

## Workflow

1. Fix the candidate: record the snapshot id (`git_sha` from the freeze, or `bundle_hash` when the sha is unresolved) and the previous accepted score (`best`). Never compare across different snapshots.
2. Collect evidence read-only; do not edit the tree during scoring:
   - test output (`go test ./...` log), `test-results.json`;
   - `smoke-results.json`, `docker-build.json`, clean-room report;
   - `README.md` sections, setup/doctor output, known limitations;
   - demo artifacts; `task-router`/MCP handshake output.
3. Score each criterion per `references/rubric.md`: `case_and_functionality` /20, `technical_implementation` /25, `readme_and_docs` /20, `reproducibility` /20, `reliability_and_security` /15. Attach at least one named artifact per awarded block; total = exact sum, 0-100.
4. Classify findings: `blocking_issues` (demo cannot run or data leaks), `deductions` (lost points with cause), `must_fix_next` (ordered, highest points-per-effort first).
5. Write the score record to `evals/project-cost/<sha>.json` with `snapshot_sha`, `rubric_version`, scores, total, evidence, findings.
6. Decide with `eval.compare`:
   - `mode: "technical"`, `candidate: <total>`, `best: <previous accepted or 0>`;
   - omit `target` to use `configs/evaluation.yaml` (95) and omit `noise_floor` to use `min_delta` (0.5), or pass explicit values;
   - set `mandatory_scenario_failed: true` when the plan's mandatory scenario regressed;
   - pass `snapshot_sha` and `persist: true` so the decision lands in `evals/decisions/<sha>/technical-<ts>.json`.
7. Follow the returned `decision` exactly: `target_reached` -> freeze and hand to `$judge`; `accept` -> new best, continue or exit; `rollback` -> restore the previous accepted snapshot and stop; `stop` -> improvement below the noise floor, stop iterating this axis.
8. Record the score and decision path; never expose either to a judge pass.

## Decision Matrix

| Signal | Action |
|---|---|
| `candidate < best` (any decrease) | `eval.compare` returns `rollback`; restore the previous accepted snapshot |
| `candidate == best` (improvement 0) | `stop`: no measurable gain, do not keep iterating this axis |
| Improvement positive but below the noise floor (0.5) | `stop`; record and move on |
| `candidate >= target` (95) | `target_reached`; freeze for the judge round |
| `mandatory_scenario_failed` | `rollback`; the flag overrides every score |
| One criterion decreased, total rose | Note it in `must_fix_next`; the tool compares the total |
| A required artifact is missing | Score zero for the unproven part; put it in `must_fix_next` |
| Clean-room gate not run | `reproducibility` cannot earn full points; state the gap |
| Secrets, logs or data in the snapshot | Treat as blocking; path traversal or leaked keys are not deductible |

## Output Contract

Score record (`evals/project-cost/<sha>.json`):

```json
{
  "snapshot_sha": "abc123",
  "rubric_version": "1.0.0",
  "technical_score": 91,
  "scores": {"case_and_functionality": 19, "technical_implementation": 23, "readme_and_docs": 17, "reproducibility": 18, "reliability_and_security": 14},
  "evidence": [{"criterion": "reproducibility", "artifact": "clean-room/report.md", "finding": "fresh checkout passed setup and smoke"}],
  "blocking_issues": [],
  "deductions": [{"criterion": "readme_and_docs", "points": 3, "reason": "no expected-output example"}],
  "must_fix_next": ["Add a clean-room README scenario", "Add a negative-input test"]
}
```

- `eval.compare` fields quoted verbatim: `decision`, `reason`, `candidate`, `best`, `target`, `noise_floor`, `improvement`, and `decision_path` when persisted.
- Every score maps to a named artifact; missing artifacts are stated, not implied.
- The score is never shown to judges; it gates the builder loop only.

## Verification

- Recheck the arithmetic: criterion sum equals `technical_score`; every criterion is within its maximum.
- Spot-check each evidence entry: open the artifact and confirm the finding is present; drop entries that fail.
- Confirm `eval.compare` returned `status == "ok"` and a decision, and that `decision_path` exists when `persist: true`.
- Confirm `mandatory_scenario_failed` matches the current plan stage, not a stale flag.
- Confirm the score record names the same snapshot id as the `eval.compare` call.
- Report every envelope warning, including `persist_error`.

## Failure Modes

| Symptom | Action |
|---|---|
| `eval.compare` returns `rollback` | Restore the previous accepted snapshot; do not re-score until the fix lands |
| `eval.compare` returns `stop` with improvement below noise floor | Stop iterating this axis; keep the candidate as best |
| `eval.compare` reports `persist_error` | Keep the inline decision, fix the repository root or path, state the missing artifact |
| Clean-room report missing | Score `reproducibility` partially and put the clean-room run in `must_fix_next` |
| Smoke results stale (different sha) | Treat as missing evidence; rerun the smoke on the candidate |
| `mandatory_scenario_failed` unclear | Verify the plan's mandatory scenario first; a wrong `false` invalidates the decision |
| Candidate is not frozen | Freeze with `eval.freeze_bundle` or record the exact working-tree revision before scoring |
| Score drops only because the rubric changed | Re-score the previous snapshot with the same rubric version before comparing |
| Iteration cap reached with improvement still positive | Freeze the best snapshot and move to `$judge`; do not score a third candidate |
| Evidence artifacts disagree (smoke passed, test failed) | Score the lower truth and put the contradiction in `blocking_issues` |

## Anti-Patterns

- Awarding points for claimed functionality with no artifact.
- Using the judge rubric keys or weights here; the two rubrics never mix.
- Treating the score as a price, an estimate or a percentage of "done".
- Re-scoring the same snapshot twice and choosing the higher number.
- Comparing totals from different rubric versions or snapshots.
- Announcing accept/rollback/stop without the `eval.compare` decision.
- Sharing this score with a judge pass.
- Editing the tree during scoring to make an artifact exist.

## References

- `references/rubric.md` - the five criteria, maxima, evidence rules and baseline expectations.
- `references/scoring-examples.md` - worked criterion scoring for two candidates with artifact citations.
- `references/regression-handling.md` - decision walkthroughs for rollback, stop, accept and target_reached.
