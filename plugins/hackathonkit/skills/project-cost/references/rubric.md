# Technical Readiness Rubric

| Criterion | Key | Max | Evidence sources |
|---|---|---:|---|
| Case fit and functionality | `case_and_functionality` | 20 | TASK.md mapping, end-to-end scenario, capability list |
| Technical implementation | `technical_implementation` | 25 | code structure, tests, smoke results, tool contracts |
| README and technical documentation | `readme_and_docs` | 20 | README sections, setup flow, known limitations |
| Reproducibility and deployment readiness | `reproducibility` | 20 | clean-room report, docker build, setup script output |
| Reliability and security basics | `reliability_and_security` | 15 | error handling, fallbacks, path sanitization, secret handling |

## Evidence Rules

1. Working code beats documentation; documentation beats claims.
2. Each point needs a named artifact: test file, smoke JSON, README section, build log, demo artifact.
3. Missing evidence is a finding: put it in `must_fix_next`, not in prose.
4. Partial functionality earns partial credit only when the working part is demonstrated and the broken part is labeled.

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
| technical_implementation | `go test ./...` output, contract tests, handler map | test files with skipped cases only |
| readme_and_docs | setup steps with expected output, known limitations section | a long README without runnable commands |
| reproducibility | fresh-checkout clean-room report, docker build log, setup/doctor output | "works on my machine" |
| reliability_and_security | fallback behavior for missing keys/backends, path traversal rejection test, no secrets committed | a promise to add validation later |

## Baseline Expectations

- Reproducibility: `scripts/setup` + `scripts/doctor` + clean-room run on a fresh checkout.
- Reliability: graceful degradation for missing API keys, unavailable backends and malformed input; no unhandled panics in tool handlers.
- Security basics: no secrets in the repository, path traversal rejected, subprocess arguments sanitized.

## Regression Handling

Compare with the previous accepted snapshot using the same rubric version:

- Mandatory scenario failure -> rollback and stop the loop.
- Any score decrease -> rollback.
- Increase below the noise floor -> record and stop iterating on this axis.

The decision strings and worked cases live in `references/regression-handling.md`;
`eval.compare` (mode `technical`) is the only source of the decision.

## Scoring Checklist

- [ ] Candidate snapshot id recorded and distinct from `best`.
- [ ] All five criteria scored within their maxima; total = sum.
- [ ] Every award backed by a named artifact; missing ones listed.
- [ ] Blocking issues separated from deductions.
- [ ] Same rubric version as the previous accepted score.
- [ ] `eval.compare` called with the mandatory-scenario flag and persisted.
