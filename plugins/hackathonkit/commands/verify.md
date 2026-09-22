---
description: Run the full verification loop and report every gate check, including test coverage (default 80%; override with --target N).
argument-hint: "[--target N]"
---

# /verify

Run the build/verify gate against the current tree and report every check, including
**test coverage**, before claiming the work is done. This command is the explicit,
operator-invoked form of the `$verification-loop` skill; follow that skill's phases
for the mandatory scenario, artifact and warnings review.

Arguments: `$ARGUMENTS` (optional `--target N` to override the coverage threshold).

## Steps

1. **Resolve the coverage target.** If `$ARGUMENTS` contains `--target N` (an integer
   0-100), export `HACKATHONKIT_COVERAGE_TARGET=N` for the gate run. Otherwise the gate
   uses its default (80, or an already-exported `HACKATHONKIT_COVERAGE_TARGET`).

2. **Run the gate from the repository root.**
   - Default: `sh scripts/quality-gate.sh`
   - With an override: `HACKATHONKIT_COVERAGE_TARGET=N sh scripts/quality-gate.sh`
   - No local Go toolchain, Docker available:
     `docker compose run --rm quality` (add `-e HACKATHONKIT_COVERAGE_TARGET=N` for an override).
   The gate runs seven checks and writes `evals/quality/<sha>/gate.json`:
   `gofmt`, `build`, `vet`, `test`, **`coverage`**, `selftest`, `skills`.

3. **Read the evidence.** Call the MCP tool `quality.gate` with no arguments (or read
   `evals/quality/<sha>/gate.json`). Keep `sha`, `ok`, `summary`, `failed[]` and
   `checks[]`. If the gate `sha` does not match the current tree, the evidence is
   stale: rerun step 2 and read again.

4. **Report every check, per-check.** For each of the seven names print its `status`
   and `duration_ms`. For `coverage`, quote the percentage and target from the check
   output (`[coverage] total X% (target N%)`). Never collapse the result into
   "all green".

5. **Decide.**
   - All seven `ok` -> claim `verified` with the per-check table, the gate `sha`, and
     the coverage number.
   - Any check `failed`/`timeout` -> report the failing check and quote the tail of
     its `output`; hand test work to `$tdd-workflow`, fix the cause, then rerun from
     step 1. Never waive a failing check and never retry until green.

6. **Finish the loop.** Continue with the `$verification-loop` phases that the gate
   does not cover: the plan's mandatory scenario, artifact existence, and a review of
   all envelope warnings. A warning that changes interpretation blocks a clean claim.

## Coverage policy

- Enforced by `scripts/coverage-check.sh` as part of the gate; default target 80%,
  configurable with `HACKATHONKIT_COVERAGE_TARGET` or `/verify --target N`.
- The check fails the gate when the repo-wide total is below the target. Coverage is
  a floor, not a goal: prefer meaningful tests over hitting the number.

## Do not

- Do not claim done on a failed, stale or missing gate.
- Do not substitute a focused `go test ./pkg/...` run for the gate's `test` check.
- Do not report coverage without the target it was compared against.
