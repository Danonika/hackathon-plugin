# Technical Readiness Rubric (Regulation 7.4)

| Criterion | Key | Max | What is evaluated |
|---|---|---:|---|
| Case fit and functionality | `case_and_functionality` | 20 | Mandatory requirements of the selected case are met. The main scenario works end to end: from input data to result. Only actually working functionality is scored. |
| Technical implementation | `technical_implementation` | 25 | The declared functionality is really implemented in code: components are connected, the structure is understandable, and key functions are not replaced by pre-baked answers or imitation. The 5-hour development limit is taken into account. |
| README and technical documentation | `readme_and_docs` | 20 | The README replaces the team's preliminary presentation: what is implemented, how it is built, technologies and data, how to install and run, how to verify the main scenario, known limitations. Content is scored, not visual design. |
| Reproducibility and deployment readiness | `reproducibility` | 20 | The jury can run the project in a clean environment: dependencies, configuration, environment variables and data are prepared. A deployed version is an advantage but does not replace running from the repository. |
| Reliability and security basics | `reliability_and_security` | 15 | The main scenario does not fail on correct input, and obvious invalid requests are handled. |

## Admission Conditions (Regulation 7.3)

Admission conditions come from the task, are listed separately from deductions,
and are checked before scoring:

- Required input and required output exist and work.
- The mandatory scenario of the selected case runs.
- Required data sources or integrations are present.
- Additional functionality never compensates for a failed admission condition.
- No new admission conditions or disqualification grounds after the competitive part starts.

A failed admission condition sets `admission.passed: false`; the candidate is not
admitted and the loop rolls back regardless of the total.

## Evidence Rules

1. Working code beats documentation; documentation beats claims.
2. Each point needs a named artifact: test file, smoke JSON, README section, build log, demo artifact, `quality.gate` evidence.
3. Missing evidence is a finding: put it in `must_fix_next`, not in prose.
4. Partial functionality earns partial credit only when the working part is demonstrated and the broken part is labeled.
5. Uniform application: domain, popularity or the number of teams solving a case never raises or lowers a score (regulation 7.2).
6. The rubric version is frozen when the competitive part starts; no new criteria, weights or disqualification grounds afterwards (regulation 7.3).

## README Checklist (required items)

- [ ] What is implemented.
- [ ] How the solution is built (architecture).
- [ ] Technologies and data used.
- [ ] How to install and run it.
- [ ] How to verify the main scenario.
- [ ] Known limitations.

## Score Arithmetic

- Score each criterion on its own scale; the total is the exact sum of the five
  criteria, 0-100. No weights, no curve, no rounding to tens.
- A criterion never exceeds its max; a criterion with no evidence scores 0 for
  the unproven part.
- Every awarded block carries at least one artifact path in `evidence[]`.
- Same rubric version + same snapshot + same evidence -> same score. Re-run the
  evidence check instead of averaging two attempts.

## Evidence Examples

| Criterion | Acceptable evidence | Not evidence |
|---|---|---|
| case_and_functionality | TASK.md scenario to demo mapping, runnable end-to-end output | "the feature exists in code" |
| technical_implementation | `go test ./...` output, `quality.gate` evidence, contract tests, handler map, real-input demo run | test files with skipped cases only; canned or stubbed responses |
| readme_and_docs | all six checklist items with runnable commands and expected output | a long README without runnable commands or limitations |
| reproducibility | fresh-checkout clean-room report, docker build log, setup/doctor output | "works on my machine"; only a deployed instance |
| reliability_and_security | fallback behavior for missing keys/backends, malformed-input handling, path traversal rejection, no secrets committed | a promise to add validation later |

## Baseline Expectations

- Reproducibility: `scripts/setup` + `scripts/doctor` + clean-room run on a fresh checkout; env vars and data prepared.
- Reliability: graceful degradation for missing API keys, unavailable backends and malformed input; no unhandled panics in tool handlers.
- Security basics: no secrets in the repository, path traversal rejected, subprocess arguments sanitized.
- Scope: the implementation fits the 5-hour development limit and does not depend on pre-built answers.

## Regression Handling

Compare with the previous accepted snapshot using the same rubric version:

- Mandatory scenario failure or failed admission -> rollback and stop the loop.
- Any score decrease -> rollback.
- Increase below the noise floor -> record and stop iterating on this axis.

The decision strings and worked cases live in `references/regression-handling.md`;
`eval.compare` (mode `technical`) is the only source of the decision.

## Scoring Checklist

- [ ] Admission conditions checked first; `admission.passed` recorded.
- [ ] Candidate snapshot id recorded and distinct from `best`.
- [ ] All five criteria scored within their maxima; total = sum.
- [ ] Every award backed by a named artifact; missing ones listed.
- [ ] Main scenario uses real input; no canned or imitated answers.
- [ ] README covers all six required items.
- [ ] Blocking issues separated from deductions.
- [ ] Same rubric version as the previous accepted score.
- [ ] `eval.compare` called with the mandatory-scenario flag and persisted.
