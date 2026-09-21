---
name: tdd-workflow
description: "Use when implementing a feature, bugfix or refactor in the hackathon codebase: drive one failing test first (RED), the minimal passing change (GREEN), a behavior-preserving cleanup (REFACTOR), then attach fresh gate evidence to the plan stage. Do not use for pure planning ($build-planner), pre-claim verification of finished work ($verification-loop) or scoring ($judge, $project-cost)."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: core
tools:
  - system.context
  - quality.gate
  - plan.status
  - plan.update
  - learning.record
---

# TDD Workflow

Turn one requirement into a red test, a minimal green change and a refactor, then prove the stage with fresh gate evidence.

## When To Use

- A plan stage needs a feature, bugfix or refactor implemented in the Go codebase (`internal/`, `cmd/`).
- A plan stage's exit criteria name a behavior that must be demonstrated by a test.
- A regression needs a reproducing test before the fix is written.
- A behavior needs a second test case and the first one already exists.
- A flaky or order-dependent test needs a deterministic replacement.
- A new CLI flag, tool handler or parser branch needs its first test.
- A test pattern (table-driven, fixture, golden file) proves reusable and should be captured.
- Do not use for planning stages (`$build-planner`), for verifying finished work before a claim (`$verification-loop`), or for accept/rollback scoring (`$judge`, `$project-cost`).

## Core Principles

1. **RED first, for the right reason.** A test that passed before the change proves nothing. The failure must be the expected assertion, not a compile error, panic or missing fixture.
2. **One failing test at a time.** Write the smallest test that expresses the next behavior; no speculative tests for code that does not exist yet.
3. **GREEN is minimal.** Make the focused test pass with the smallest change. Do not refactor, rename or expand scope while red.
4. **REFACTOR keeps the suite green.** Cleanup is behavior-preserving; rerun the focused and package tests after every move and revert any move that changes behavior.
5. **Evidence, not assertion.** `quality.gate` output is the only proof of build health; "tests pass" in prose is not evidence.
6. **The plan stage owns the work.** `plan.update` carries the RED/GREEN evidence and `learning.record` captures a test pattern only once it proved reusable.

## Workflow

1. Call `system.context` (no arguments) once. Read `routing_mode` and confirm the repository root; if the task runs in a worktree, all commands below run from that root.
2. Call `plan.status` with no `slug` (latest plan) or `slug=<plan>`. Keep `slug`, `revision`, `current_stage.id` and `mandatory_scenario`.
   - `status: ok` - use `current_stage.id` as the stage for every `plan.update` call.
   - `status: partial` with `plan: null` - stop and route to `$build-planner`; this skill never creates plans.
3. Detect the runner from the repo, not from memory. The hackathon gate is `gofmt -l internal cmd`, `go build ./...`, `go vet ./...`, `go test ./...`, `go run ./cmd/hackathonkit-mcp --selftest` and `go run ./cmd/hackathonkit-mcp --validate-skills` (see `scripts/quality-gate.sh`). Record the focused command for the package, for example `go test ./internal/plans/... -run TestLoad -v -count=1`.
4. **RED.** Write one failing test that encodes the next requirement. Run the focused command. The failure must name the assertion you expected to fail; keep the output as the RED evidence. If it compiles and passes, the test is wrong - tighten it until it fails for the intended reason.
5. **GREEN.** Implement the minimal change. Rerun the focused test until green, then run the package tests to catch neighbors. Keep the passing output. Do not proceed while any test in the package fails.
6. **REFACTOR.** Remove duplication, rename, extract or reorder while the suite stays green. Rerun the focused and package tests after each move. Stop when the code is clean; do not add behavior here.
7. Produce fresh gate evidence: run `scripts/quality-gate.sh` (or `docker compose run --rm quality`) so `evals/quality/<sha>/gate.json` matches the current tree, then call `quality.gate` with no arguments.
   - Keep `sha`, `ok`, `summary`, `failed[]` and `checks[]`.
   - Report each check by name: `gofmt`, `build`, `vet`, `test`, `selftest`, `skills`. Never summarize as "all good" without the list.
   - If `ok` is false, read the failing `checks[].output` and fix it before step 8.
8. Call `plan.update` with `slug`, `stage=<current_stage.id>`, `status: "doing"` when RED starts and `status: "done"` (or `failed`) after GREEN plus gate:
   - `evidence` strings: test path and name, exact commands, RED failure excerpt, GREEN pass excerpt, gate `sha` plus per-check summary.
   - Keep the returned `revision`; never edit `evals/plans/*.json` by hand.
9. Call `learning.record` at most once when a test pattern proved reusable: `trigger` ("when testing <class of behavior>"), `insight`, `outcome` (`success`/`failure`/`neutral`), `scope: "project"`, `evidence` (`[test path, gate sha]`). Skip it when nothing generalizes.
10. Answer per the Output Contract; if `quality.gate` returned warnings, carry them into the answer verbatim.

### Stage Checkpoints

Tie each phase to the plan so a later session can replay the cycle from `evals/plans/<slug>.json`.

| Phase | `plan.update` status | Evidence to attach |
|---|---|---|
| RED written and failing | `doing` | test path and name, command, expected assertion, failure excerpt |
| GREEN focused and package-wide | `doing` | command, pass excerpt, one line on the minimal change |
| REFACTOR complete | `doing` | commands rerun, still-green result, what moved |
| Gate fresh and green | `done` | gate `sha`, `ok`, per-check summary |
| Gate red or test blocked | `failed` | failing check, output tail, blocking reason |

- Update the stage when the phase changes, not once at the end; the plan revision history is the audit trail.
- `doing` may be written repeatedly; `done` and `failed` are terminal for that stage until a re-plan.
- Never attach a gate `sha` from an earlier revision of the tree; rerun the gate first.

### Focused Test Selection

| Change | Focused command |
|---|---|
| One function or parser branch | `go test ./<pkg>/... -run TestName -v -count=1` |
| One subtest case | `go test ./<pkg>/... -run 'TestName/case_name' -v` |
| Tool handler wiring | `go test ./internal/tools/... -run TestTool -count=1` |
| Skill validator change | `go test ./internal/skills/... -run TestCatalogPassesStandard -count=1` |
| Whole repository before gate | `go test ./... -count=1` |

The focused command is for the RED/GREEN loop; the repository-wide command is the one the gate records.

## Decision Matrix

| Signal | Action |
|---|---|
| Test passes before the change | Rewrite it so it fails first; a test that never failed is not RED |
| Test fails with a compile error or panic | Fix the test setup; RED must fail on the assertion |
| Focused test green, package tests red | The change broke a neighbor; fix before REFACTOR |
| `quality.gate` `sha` differs from the current tree | Run `scripts/quality-gate.sh`, then call `quality.gate` again; stale evidence is not evidence |
| `quality.gate` check `test` failed | Read `checks[].output`, fix the failing test or implementation, regenerate the gate |
| `quality.gate` check `gofmt` failed | Run `gofmt -w internal cmd`, rerun the tests, regenerate the gate |
| `quality.gate` returns `partial` (`available: false`) | Run the gate script; if no runtime exists, state that build health is unverified |
| `plan.status` returns `partial` | Route to `$build-planner`; do not improvise a plan or stage |
| No plan exists for a small task | Proceed, but state that no stage evidence was attached |
| A fixture or golden file is needed | Use `references/test-patterns.md`; keep fixtures under `testdata/` |
| The same pattern works in a second package | Record one learning citing both test paths |
| Refactor changes behavior | Revert the move, keep the last green state, rerun the focused test |

## Output Contract

- **RED evidence** - test path and name, exact command, expected assertion, failing output excerpt.
- **GREEN evidence** - same command, passing output, one line on the minimal change.
- **REFACTOR evidence** - commands rerun and still-green result; what moved and why.
- **Gate** - `sha`, `ok`, `summary`, and every check name with its `status`; failing checks carry their `output` tail.
- **Plan** - `slug`, bumped `revision`, `stage`, `status`, evidence list.
- **Learning** - the `learning.record` id when one was stored, or why none was.
- **Warnings** - from every envelope used, verbatim.
- **Gaps** - behavior not covered by tests, with the reason.

## Worked Cycle

Task: `plan.status` stage S3 wants slug normalization for the plan tool.

1. `system.context` -> `routing_mode: normal`, root confirmed.
2. `plan.status` -> `slug: demo-plan`, `revision: 6`, `current_stage.id: S3`.
3. `plan.update` `{slug: demo-plan, stage: S3, status: doing}` -> revision 7.
4. RED: add `TestSlug/keeps_digits` to `internal/plans/plans_test.go`; run
   `go test ./internal/plans/... -run TestSlug -v -count=1`; it fails with
   `Slug("S3 plan") = "s3 plan", want "s3-plan"`.
5. GREEN: normalize punctuation after lowercasing; rerun focused -> pass; run
   `go test ./internal/plans/... -count=1` -> pass.
6. REFACTOR: extract `normalizeToken`; rerun both commands -> pass, no behavior change.
7. Run `scripts/quality-gate.sh`; call `quality.gate` -> `sha: 9f2c41a`, `ok: true`,
   all six checks `ok`.
8. `plan.update` `{slug: demo-plan, stage: S3, status: done, evidence: [red failure,
   green command, gate sha 9f2c41a ok]}` -> revision 8.
9. `learning.record` -> `L-0142` "table-driven slug cases catch punctuation edges".

The answer reports RED, GREEN, REFACTOR, the six gate checks and revision 8. If step 7 had
failed on `gofmt`, the cycle would stop there: format, rerun tests, regenerate the gate,
and only then mark the stage done.

## Verification

- Every test claim maps to command output from this session, not memory.
- The RED failure was observed before the GREEN pass on the same test name.
- The gate `sha` matches the tree that produced the change; `ok` is true or every failed check is named and explained.
- All six gate checks are reported by name; no check is skipped or merged.
- `plan.update` returned a bumped `revision` and the stage `evidence` is non-empty.
- No test was skipped, weakened or deleted to reach green; `t.Skip` and `-run` filters are not hiding failures.
- Warnings from `quality.gate`, `plan.status` and `learning.record` are in the answer.

## Failure Modes

| Symptom | Action |
|---|---|
| Gate evidence is stale (`sha` != current tree) | Run `scripts/quality-gate.sh`, re-read `quality.gate`; never mark done on stale evidence |
| Test passes for the wrong reason (assert-free, wrong fixture, `t.Skip`) | Tighten the assertion, remove the skip, rerun RED -> GREEN; a pass without a prior failure is not evidence |
| Coverage theater (many shallow tests, no behavior asserted) | Replace with one behavior-level test per requirement; report covered behaviors, not test counts |
| `go test ./...` fails only package-wide | Run the full command, isolate with `-run`, fix ordering or shared state |
| `quality.gate` returns `partial` (`no gate evidence found`) | Run the gate script, then re-read; state the fallback if no runtime is available |
| `quality.gate` check `timeout` | Rerun the focused test; if the gate still times out, report the check and duration |
| Flaky test (passes then fails on rerun) | Fix the nondeterminism (clock, map order, ports); never retry until green |
| `plan.update` returns `unknown stage` | `plan.status` to list stage ids; update an existing stage only |
| `plan.status` returns `partial` | No plan; hand off to `$build-planner` and stop |
| A test needs network or an API key | Use a local fixture; if impossible, report the limitation and mark the check unverified |
| `gofmt -l` output is not empty | Format the named files, rerun tests, regenerate the gate |
| Refactor went too far and broke behavior | Revert to the last green checkpoint, rerun the focused test, redo the refactor smaller |

## Anti-Patterns

- Writing the implementation before the failing test exists.
- Marking a stage `done` from memory instead of `plan.update` with evidence.
- Claiming "tests pass" without a `quality.gate` envelope and a matching `sha`.
- Asserting coverage percentages instead of naming covered behaviors.
- Weakening an assertion, adding `t.Skip` or deleting a test to get green.
- Refactoring while red, or mixing a refactor with a behavior change.
- Running only the focused test and calling the build verified.
- Recording a learning per test file instead of one reusable pattern.
- Editing `evals/plans/*.json` by hand instead of calling `plan.update`.

## References

- `references/test-patterns.md` - table-driven Go tests, fixtures, golden files and negative cases used in this codebase.
- `references/red-green-evidence.md` - the RED/GREEN/REFACTOR checkpoint format and what to put into `plan.update` evidence.
