---
name: verify
description: "Use when claiming work is done or before a freeze: run the seven-check build/verify gate including test coverage (default 80%), then complete the verification-loop phases (mandatory scenario, artifacts, warnings) and report every check. Do not use to justify a result that is already failing, and do not substitute a focused go test for the gate."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: quality
tools:
  - system.context
  - quality.gate
  - plan.status
  - learning.record
---

# Verify

Run the build/verify gate against the current tree and report every check, including test coverage, before claiming the work is done. This is the operator-invoked, gate-first form of the `$verification-loop` skill.

## When To Use

- Before claiming any build, fix or refactor is complete.
- Before a Demo Day freeze or a clean-room gate.
- When the user asks to verify, re-check, or confirm the tree is green.
- Do not use to rationalize a failing result, and do not replace the gate's `test` check with a focused `go test ./pkg/...` run.

## Core Principles

1. **The gate is the authority.** Seven checks, each reported with its own status and duration.
2. **Coverage is enforced.** Default target 80%; override with `--target N` (export `HACKATHONKIT_COVERAGE_TARGET`).
3. **Stale evidence is not evidence.** If the gate `sha` is not the current tree, rerun.
4. **Never waive a failing check.** Fix the cause and rerun; do not retry until green.
5. **A warning that changes interpretation blocks a clean claim.**
6. **Report per check, never collapse to "all green".**

## Workflow

1. Resolve the coverage target: if the argument contains `--target N` (integer 0-100), export `HACKATHONKIT_COVERAGE_TARGET=N`. Otherwise the gate uses 80 or an already-exported value.
2. Run the gate from the repository root:
   - Default: `sh scripts/quality-gate.sh`
   - With an override: `HACKATHONKIT_COVERAGE_TARGET=N sh scripts/quality-gate.sh`
   - No local Go toolchain, Docker available: `docker compose run --rm quality` (add `-e HACKATHONKIT_COVERAGE_TARGET=N` for an override)
   The gate runs seven checks and writes `evals/quality/<sha>/gate.json`: `gofmt`, `build`, `vet`, `test`, `coverage`, `selftest`, `skills`.
3. Read the evidence with the MCP tool `quality.gate` (no arguments) or read `evals/quality/<sha>/gate.json`. Keep `sha`, `ok`, `summary`, `failed[]` and `checks[]`.
4. If the gate `sha` differs from the current tree, the evidence is stale: rerun step 2 and read again.
5. Report every check: print its `status` and `duration_ms`. For `coverage`, quote the percentage and target from the check output (`[coverage] total X% (target N%)`).
6. Decide:
   - All seven `ok` -> claim `verified` with the per-check table, the gate `sha` and the coverage number.
   - Any `failed`/`timeout` -> quote the tail of its `output`, hand test work to `$tdd-workflow`, fix, rerun from step 1.
7. Complete the `$verification-loop` phases the gate does not cover: the plan's mandatory scenario, artifact existence, and a review of all envelope warnings.
8. Record the verified state (sha + coverage) with `learning.record` when it settles a recurring question.

### Gate checks

| Check | What it proves |
|---|---|
| gofmt | Formatting is canonical |
| build | Every package compiles |
| vet | No vet diagnostics |
| test | The Go test suite passes |
| coverage | Repo-wide total >= target |
| selftest | `hackathonkit-mcp --selftest` passes |
| skills | The skill catalog meets the standard |

### Evidence staleness

The gate stamps the evidence with the tree's sha. If the working tree changes after a gate run, that run's evidence describes a tree that no longer exists. Always compare the gate `sha` to the current tree before quoting the result; when they differ, rerun rather than reconcile.

### What the gate does not cover

The gate proves the build and test surface, not the product. After it is green, still check the plan's mandatory scenario, confirm the artifacts exist, and read every envelope warning. These are the `$verification-loop` phases.

## Decision Matrix

| Signal | Action |
|---|---|
| All seven ok | Claim verified with the per-check table and the gate sha |
| A check failed | Quote its output tail, fix the cause, rerun from step 1 |
| Gate sha != tree | Stale; rerun the gate |
| Coverage below target | Add meaningful tests; do not lower the target |
| Envelope warning changes meaning | Treat as blocking; resolve before claiming |
| No Go toolchain and no Docker | Report the gate as unavailable; do not claim green |

## Output Contract

- The per-check table: name, status, duration_ms for all seven.
- The gate `sha` and, for coverage, the percentage and the target it was compared against.
- Any failing check with the tail of its output.
- The outcome of the `$verification-loop` phases (mandatory scenario, artifacts, warnings).
- One explicit verdict: `verified` or `not verified (reason)`.

## Verification

- Confirm the gate `sha` equals the current tree's sha.
- Confirm all seven checks are present in the report, none skipped.
- Confirm the coverage percentage is reported with its target.
- Re-open one artifact the mandatory scenario names and confirm it exists.
- If any fails, the claim is `not verified`.

## Failure Modes

| Symptom | Action |
|---|---|
| `sha` mismatch | Rerun the gate; the evidence was stale |
| Test check timeout | Report the timeout and the last output; fix the hang |
| Coverage just below target | Add a meaningful test; never lower the target |
| Skills check fails | Fix the offending `SKILL.md`; the validator names the problem |
| Docker path unhealthy | Fall back to the local gate or report unavailable |

## Anti-Patterns

- Claiming done on a failed, stale or missing gate.
- Reporting "all green" without the per-check table.
- Replacing the gate's `test` check with a focused package test.
- Reporting coverage without the target it was compared against.
- Retrying until green instead of fixing the cause.

## References

- `references/gate-checks.md` - what each of the seven checks runs and how to read its output.
- `references/coverage-policy.md` - target, override and what a coverage failure means.
