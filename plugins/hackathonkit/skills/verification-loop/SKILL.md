---
name: verification-loop
description: "Use when about to claim work is done, passing or demo-ready: run the pre-claim phases (gofmt/build/vet/test/coverage/selftest/skills via quality.gate, the plan's mandatory scenario, artifact existence, warnings review) and report per-check evidence. Do not use for writing tests or implementing fixes ($tdd-workflow), planning stages ($build-planner), or judge/project-cost scoring; call eval.compare only when a scored decision is genuinely required."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: quality
tools:
  - quality.gate
  - plan.status
  - eval.compare
  - learning.record
---

# Verification Loop

Prove work before claiming it: fresh gate evidence, the mandatory scenario, artifact existence and warnings - phase by phase.

## When To Use

- Before saying "done", "passing", "works", "verified" or "ready to demo" in any answer or report.
- After a build stage, fix or refactor lands and before `plan.update` marks the stage done.
- When a prior claim is questioned: rerun the phases instead of defending the claim.
- Before the final freeze, when the mandatory scenario must be shown end to end.
- After changing generated artifacts, reports or skills that other stages depend on.
- When a resumed session inherits a plan stage marked `done` but no fresh gate evidence.
- Do not use for writing tests or fixing code (`$tdd-workflow` owns RED/GREEN), for planning (`$build-planner`), or for judge/project-cost scoring. Call `eval.compare` only when a scored accept/rollback/stop decision is actually needed.

## Core Principles

1. **No fresh evidence, no claim.** A claim points at a check that ran against the current tree, not at a memory of a check that passed earlier.
2. **Fresh means the gate `sha` matches the tree.** Stale gate evidence is a failure, not a pass; regenerate before reading.
3. **Report per-check, not in aggregate.** "All green" without the seven check names (including coverage) and their statuses is not a report.
4. **The mandatory scenario is the contract.** If it fails, nothing else can make the claim true.
5. **Fix, then rerun the whole loop.** Never waive a failed check, and never retry until green without fixing the cause.
6. **Scored decisions belong to `eval.compare`.** Never write accept, rollback or stop in prose.

## Workflow

1. Call `plan.status` with no `slug` (latest plan) or `slug=<plan>`. Keep `slug`, `revision`, `current_stage.id` and `mandatory_scenario` (`{input, command, expected}`).
   - `status: ok` - the scenario and stage come from the plan.
   - `status: partial` with `plan: null` - state that no plan exists and verify against the scenario the user stated; do not invent one.
2. **Phase 1 - static checks.** Confirm `gofmt -l internal cmd` prints nothing and that `go build ./...` and `go vet ./...` exit 0. These are the gate's first three checks; when the tree changed since the last gate, run `scripts/quality-gate.sh` (or `docker compose run --rm quality`) before reading.
3. **Phase 2 - tests, coverage and selftest.** Run `go test ./...` and `go run ./cmd/hackathonkit-mcp --selftest`. The gate's `test`, `coverage` and `selftest` checks carry the output tails; a focused test is not a substitute for the package-wide run, and a below-target coverage total fails the gate.
4. **Phase 3 - skills.** Run `go run ./cmd/hackathonkit-mcp --validate-skills`; every skill must pass with an empty problems list. This validates the skill catalog you are touching as part of the same claim.
5. **Phase 4 - read the gate.** Call `quality.gate` with no arguments. Keep `sha`, `ok`, `summary`, `failed[]` and `checks[]`.
   - Report every check by name with its `status` and `duration_ms`: `gofmt`, `build`, `vet`, `test`, `coverage`, `selftest`, `skills`.
   - For `coverage`, quote the percentage and target from the check output (`[coverage] total X% (target N%)`). The target defaults to 80 and is overridable with `HACKATHONKIT_COVERAGE_TARGET` (or `/verify --target N`).
   - For a failed check, quote the tail of `checks[].output`.
   - `status: partial` with `available: false` means no evidence exists: run the gate script and re-read; if no runtime is available, mark the claim `unverified` and say why.
6. **Phase 5 - mandatory scenario.** Execute `mandatory_scenario.command` against `mandatory_scenario.input` and compare the observed output to `expected`. Keep the run log or artifact id. A scenario that is only described is not verified.
7. **Phase 6 - artifacts.** For every `artifact_id` in the answer, confirm it resolves under `data/artifacts` and that the producing envelope returned `status != "error"` with a non-empty id. Drop any claim that depends on a missing artifact.
8. **Phase 7 - warnings review.** Collect `warnings` from every envelope used in the task (plan, gate, producing tools) and restate them. A warning that changes interpretation blocks a clean claim.
9. **Decide the claim.**
   - All phases pass -> claim `verified` with the evidence below.
   - Any phase fails -> report exactly which check failed and its output; fix it (hand test work to `$tdd-workflow`), then rerun from Phase 1.
10. **Scored decision branch.** When the task needs an accept/rollback/stop decision, call `eval.compare` with `mode` (`technical`/`judge`), `candidate`, `best`, `noise_floor`, `snapshot_sha` and `persist: true`; follow the returned `decision`. Never derive it from the verification phases.
11. Call `learning.record` at most once when a check caught a real defect or a scenario needed a fixture: `trigger`, `insight`, `outcome` (`success`/`failure`/`neutral`), `scope: "project"`, `evidence` (`[gate sha, check name]`).
12. Answer per the Output Contract.

### Phase Gates

The loop is only as strong as its weakest phase; each phase has one pass condition and one evidence pointer.

| Phase | Pass condition | Evidence pointer |
|---|---|---|
| 1 static | `gofmt -l internal cmd` empty; build and vet exit 0 | command outputs |
| 2 tests | `go test ./...`, coverage >= target and selftest exit 0 | output tails |
| 3 skills | validator summary `failed: 0` | failing skill names if any |
| 4 gate | `quality.gate` `ok: true`, `sha` matches tree | `sha`, `summary`, `checks[]` |
| 5 scenario | observed output equals `mandatory_scenario.expected` | run log or artifact id |
| 6 artifacts | every id resolves and `status != "error"` | id plus producing envelope |
| 7 warnings | all warnings restated, none interpretation-changing | verbatim list |

- A phase that cannot run is reported as `unverified`, never as a pass.
- A failed phase stops the claim; fix the cause and rerun from Phase 1 so the evidence shares one tree.
- Record durations from `checks[]` when the failure looks environmental (timeouts, slow builds).

### Evidence Ledger

Keep one ledger for the claim; every line must resolve to a command output or an envelope.

| Entry | Source | Value |
|---|---|---|
| tree | current working tree | branch/worktree, dirty or clean |
| gate | `quality.gate` | `sha`, `ok`, `summary` |
| checks | `quality.gate` | seven names with `status` and `duration_ms` |
| scenario | scenario run | input, command, expected, observed |
| artifacts | producing envelopes | id, `status` |
| warnings | all envelopes | verbatim list |
| decision | `eval.compare` (only when needed) | `decision`, `reason`, `decision_path` |

## Decision Matrix

| Signal | Action |
|---|---|
| Gate `ok: true` and `sha` matches the tree | Claim `verified` with the per-check list and scenario evidence |
| Gate `ok: false` | Name `failed[]`, quote the output, fix the check, rerun from Phase 1; never claim done |
| Gate `sha` differs from the current tree | Stale evidence: run `scripts/quality-gate.sh`, then call `quality.gate` again |
| `quality.gate` returns `partial` (`available: false`) | Run the gate script; if unavailable, mark the claim `unverified` and state the gap |
| Mandatory scenario fails | Stop; report input/command/expected vs observed; fix before any claim |
| Mandatory scenario cannot run (missing input) | Report the missing input; do not substitute a different scenario |
| Artifact id missing or empty | Re-create it or drop the dependent claim; check the producing envelope |
| Warnings found in any envelope | Restate them; a warning that changes interpretation blocks the clean claim |
| A check is flaky (pass then fail) | Fix the nondeterminism and rerun the full loop; never average runs |
| Scored decision needed | `eval.compare` with `persist: true`; follow its `decision`, no prose verdict |
| No plan exists | Verify against the user's stated scenario and say plan evidence is missing |
| User asks to "just say it works" | Refuse the unverified claim; offer the fastest phase that can produce evidence |
| The tree changes after Phase 7 | Rerun the loop; evidence binds to the tree, not to the session |

## Output Contract

- **Claim status** - `verified` / `failed` / `unverified`, with the phase that produced it.
- **Per-check table** - check name, `status`, `duration_ms`; failed checks carry the output tail.
- **Gate identity** - `sha`, `ok`, `summary`.
- **Mandatory scenario** - input, command, expected, observed, evidence pointer.
- **Artifacts** - every `artifact_id` and the producing envelope's `status`.
- **Warnings** - all envelope warnings, verbatim.
- **Scored decision** - when `eval.compare` ran: `decision`, `reason`, `improvement`, `noise_floor`, `decision_path`.
- **Learning** - the `learning.record` id when one was stored, or why none was.
- **Gaps** - phases that could not run, with the reason.

## Worked Loop

Task: a fix landed in `internal/tools`; the answer wants to say "done".

1. `plan.status` -> `slug: demo-plan`, `revision: 8`, `current_stage.id: S3`,
   `mandatory_scenario: {input: data/input/sample.csv, command: "...", expected: "..."}`.
2. Static: `gofmt -l internal cmd` prints nothing; `go build ./...` and `go vet ./...` exit 0.
3. Tests: `go test ./... -count=1` -> `ok`; `--selftest` -> handshake ok.
4. Skills: `--validate-skills` -> summary `failed: 0`.
5. `quality.gate` -> `sha: 9f2c41a`, `ok: true`, all seven checks `ok` (durations kept), coverage `83% (target 80%)`.
6. Scenario run -> observed matches `expected`; artifact `reports/telemetry.md` resolves and
   the report envelope returned `status: ok`.
7. Warnings: `documents.extract` warned about truncation; restated in the answer.
8. Claim `verified` with the ledger; no `eval.compare` call because no score is compared.

If step 5 had returned `ok: false` with `test (failed)`, the answer would stop at
`failed`, quote the test output tail, fix the test, and rerun from Phase 1.

## Verification

- Every phase result comes from the current tree; no phase is skipped silently.
- The gate `sha` matches the working tree, or the evidence was regenerated, not explained away.
- `checks[]` contains all seven names (including `coverage`) and each status is reported; none is merged into "all green".
- The mandatory scenario ran and its observed output is quoted against `expected`.
- Every artifact id resolves and every producing envelope returned `status != "error"`.
- Warnings from all envelopes appear in the answer.
- Any scored decision came from `eval.compare`, not prose.
- The claim status matches the weakest phase: one failed phase makes the claim `failed`.

## Failure Modes

| Symptom | Action |
|---|---|
| Stale gate evidence | Regenerate with `scripts/quality-gate.sh`, re-read `quality.gate`; never claim on an old `sha` |
| Gate has a failing check | Name it from `failed[]`, read `checks[].output`, fix, rerun all phases |
| `quality.gate` returns `partial` | No evidence found: run the script or mark the claim `unverified` |
| Mandatory scenario passes ad hoc but not from the plan | Copy `mandatory_scenario` verbatim; never paraphrase the command |
| Selftest passes but the skills check fails | Fix the skill-standard problem first; the gate is not green until all seven pass |
| Artifact missing | Check the producing envelope and `data/artifacts`; re-create or drop the claim |
| Warning hidden in a `partial` envelope | Surface it; warnings are part of the output contract |
| Check `timeout` | Report `timeout` with its duration, rerun focused; do not call it a pass |
| Verification rerun until green | Fix the nondeterminism; a retry-until-green loop is not verification |
| Tree changed after verification | Rerun the loop; the evidence no longer describes the tree |
| `eval.compare` returns `rollback` | Report the decision and reason; restore the last accepted snapshot; do not argue |
| No gate runtime (no Go, no binary) | Mark the claim `unverified`, name the missing runtime, and give the command to install it |

## Anti-Patterns

- Saying "all tests pass" without the gate `sha` and per-check statuses.
- Treating a previous session's green gate as current evidence.
- Running only the focused test and calling the build verified.
- Paraphrasing the mandatory scenario command or substituting a friendlier one.
- Claiming "done" with warnings suppressed or artifacts unverified.
- Calling `eval.compare` when no scored decision is needed.
- Rerunning the gate until it passes without fixing the flake.
- Marking artifacts verified without checking the producing envelope's `status`.
- Reporting `verified` while one phase is `failed` or `unverified`.

## References

- `references/verification-checklist.md` - phase-by-phase commands, pass criteria and the evidence to keep for each phase.
- `/verify` (plugin command) - runs the gate and reports the seven checks including coverage; `/verify --target N` overrides the threshold.
