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

## Baseline Expectations

- Reproducibility: `scripts/setup` + `scripts/doctor` + clean-room run on a fresh checkout.
- Reliability: graceful degradation for missing API keys, unavailable backends and malformed input; no unhandled panics in tool handlers.
- Security basics: no secrets in the repository, path traversal rejected, subprocess arguments sanitized.

## Regression Handling

Compare with the previous accepted snapshot using the same rubric version:

- Mandatory scenario failure -> rollback and stop the loop.
- Any score decrease -> rollback.
- Increase below the noise floor -> record and stop iterating on this axis.
