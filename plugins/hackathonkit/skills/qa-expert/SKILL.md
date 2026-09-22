---
name: qa-expert
description: "Use when a plan or task needs acceptance criteria, a risk-based test matrix, exploratory charters, regression selection, bug reports or a release-readiness verdict. Turns requirements into testable checks and evidence; automated checks run through quality.gate. Explicit-only: never invoke implicitly. Do not use for writing implementation tests ($tdd-workflow), pre-claim gating ($verification-loop) or scoring ($judge, $project-cost)."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: quality
tools:
  - quality.gate
  - reports.create
  - plan.status
  - plan.update
  - security.review
  - learning.record
---

# QA Expert

Own the quality strategy around the product: what must be true, how it will be
checked, what was found, and whether the candidate is release-ready. Automated
checks come from `quality.gate`; the QA verdict is evidence, not a feeling.

## When To Use

- A task or plan needs acceptance criteria before implementation.
- A candidate needs a test plan: scope, risks, coverage, environments.
- Manual/exploratory testing must be chartered and reported.
- Bugs need a reproducible report with severity and evidence.
- Before a demo or release, to produce a readiness verdict with known risks.
- Do not use for writing implementation tests; `$tdd-workflow` owns RED-GREEN-REFACTOR.
- Do not use for the final pre-claim gate (`$verification-loop`) or scores (`$judge`, `$project-cost`).

## Core Principles

1. Testable or it does not exist: every acceptance criterion states an input, an action and an observable result.
2. Risk first: effort follows impact x likelihood, not the order requirements were written.
3. Evidence per check: a pass needs a command output, artifact id or recorded observation; otherwise it is "not tested".
4. Independence: the author of a change does not mark it verified from memory; re-run the check against the candidate snapshot.
5. Bugs are reproducible reports, not complaints: exact steps, expected vs actual, severity, evidence, environment.
6. Coverage is reported honestly: what was tested, what was skipped, and why.
7. The QA verdict never overrides `eval.compare`; it informs the controller and the judge bundle.

## Workflow

1. Gather inputs: the task statement, the current plan (`plan.status`), the README's mandatory scenario and known limitations, and any earlier bug reports.
2. Define acceptance criteria: one line per requirement with input -> action -> expected observable result. Mark mandatory admission conditions separately from nice-to-have checks (regulation 7.3).
3. Build the risk matrix: for each criterion estimate impact (1-3) and likelihood of failure (1-3); priority = impact x likelihood. The matrix drives test order and depth.
4. Design checks:
   - functional: happy path, boundary values, invalid input, empty input, large input;
   - integration: MCP tool calls, storage backends, routing modes;
   - regression: previously fixed bugs and previously passing scenarios;
   - non-functional: startup time, error handling, secret hygiene (`security.review`), offline degradation.
5. Execute the automated part: `quality.gate` for gofmt/build/vet/test/selftest/skills, plus the plan's mandatory scenario. Record per-check status and the exact command.
6. Charter exploratory sessions: time-boxed, a specific area, a specific risk, notes on what was varied. Findings become bug reports or new checks.
7. Write bug reports per `references/bug-report-format.md`: id, title, severity (blocker/critical/major/minor), steps, expected, actual, evidence, environment, suspected area.
8. Produce the artifacts: `reports.create` with title `Test plan - <task>` (criteria, matrix, checks) and `QA report - <candidate>` (results, bugs, coverage, verdict). Update `plan.update` with evidence on the relevant stage.
9. Verdict: `release-ready` (all mandatory checks pass, no blocker/critical open), `conditional` (listed risks with owners), or `not ready` (mandatory check failing). Never round up.
10. Record reusable lessons with `learning.record` (for example "empty CSV breaks the anomaly loader; always test empty input").

## Decision Matrix

| Signal | Action |
|---|---|
| Mandatory acceptance criterion fails | Verdict `not ready`; report the exact check and evidence |
| Blocker or critical bug open | Verdict `not ready`; include the repro and suspected area |
| Only minor bugs open | Verdict `conditional`; list each with impact and workaround |
| A check cannot run (no engine, no key) | Mark `not tested`, state the blocker; never mark it passed |
| Quality gate stale or failing | Re-run `scripts/quality-gate.sh`; a stale green is not evidence |
| Regression on a previously fixed bug | Raise severity one level and reference the earlier report |
| Requirement is ambiguous | Return it as a question with two concrete interpretations; do not invent a criterion |
| Test plan grows beyond the time budget | Cut by lowest risk x impact first; state what was cut |
| Judge/project-cost asks for test evidence | Hand the QA report artifact id; do not re-score |

## Output Contract

- `Test plan - <task>`: acceptance criteria, risk matrix, check list, environments, out-of-scope.
- `QA report - <candidate>`: per-check results with evidence, bug list, coverage (tested/skipped), verdict and risks.
- Every bug: severity, steps, expected, actual, evidence reference.
- Verdict line: `release-ready` | `conditional` | `not ready` with the deciding facts.
- Artifact ids from `reports.create`; `quality.gate` summary for the automated part.
- Explicit list of untested areas and the reason.

## Verification

- Every acceptance criterion maps to at least one executed check or is marked untested.
- Every passing check cites fresh evidence (gate output, artifact id, recorded observation) for the same snapshot.
- Every bug has steps that a second person could follow to reproduce it.
- The verdict matches the matrix: no `release-ready` with an open mandatory failure.
- `security.review` ran on any untrusted content used as test input.
- Report warnings from tool envelopes instead of hiding them.

## Failure Modes

| Symptom | Action |
|---|---|
| `quality.gate` returns failing checks | Verdict `not ready`; hand the failing check to the builder |
| Gate evidence is for another revision | Re-run the gate on the candidate; do not reuse the old report |
| Mandatory scenario cannot run | Report the blocker with the exact command; verdict `not ready` |
| Bug not reproducible | Mark `needs-info` with what was tried; ask for the missing input |
| Environment lacks Docker/API key | Mark affected checks untested; verify the filesystem fallback path instead |
| Acceptance criteria keep changing | Freeze them with the plan revision; new criteria go to the next revision |
| Exploratory session finds nothing | Record what was varied; "no finding" is a result, not a failure |
| Coverage claim challenged | Point to the per-check evidence; never argue from memory |

## Anti-Patterns

- Marking a check passed without running it on the candidate.
- Writing acceptance criteria that cannot fail ("the app works well").
- Reporting bugs without steps, severity or evidence.
- Rounding a `conditional` verdict up to `release-ready`.
- Re-scoring the product; that belongs to `$judge` and `$project-cost`.
- Replacing implementation tests with a QA checklist; `$tdd-workflow` owns those.
- Testing only the happy path and calling it coverage.
- Using real secrets or personal data as test input.

## References

- `references/test-plan-template.md` - ready-to-fill test plan with criteria, risk matrix and coverage table.
- `references/bug-report-format.md` - bug schema with severities and a worked example.
- `references/test-design-techniques.md` - boundary, equivalence, decision-table and exploratory techniques with examples.
