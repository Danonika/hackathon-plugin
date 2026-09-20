---
name: self-improvement-controller
description: "Orchestrate bounded improvement rounds: fast block 1, project cost, demo evidence, judge rounds, rollback on regression. Explicit-only."
---

# Self-Improvement Controller

Run improvement rounds with hard limits. Read `configs/evaluation.yaml` for targets, iteration caps and the finalization reserve.

## When To Use

- After the demo is working end-to-end and there is time for hardening.
- Never during the first build of the main scenario.

## Preconditions

1. Main scenario passes; otherwise fix it first. The mandatory scenario and stage gates come from the `$build-planner` plan; use its latest revision.
2. Judge calibration completed once: score one frozen snapshot twice and run `eval.calibrate`; use the returned `calibration.noise_floor` everywhere.
3. Time budget check: stop self-improvement when remaining time <= `finalization_reserve_minutes`.

## Fast Block 1

1. `$roast-project` (3 critics, parallel) -> fix critical and high findings.
2. `$cp-expert-refactoring` -> apply accepted changes.
3. Targeted tests.
4. `$system-design-expert` -> apply accepted proposals.
5. Targeted tests again.
6. Quick smoke: existing MCP image starts, `initialize`, `tools/list`, main happy path.
7. Build demo evidence (demo-artifacts bundle).
8. Freeze candidate snapshot with `eval.freeze_bundle`; keep its `bundle_hash` and `git_sha`.

Full clean-room rebuild is **not** part of each iteration; it runs once on the final candidate.

## Technical Loop

```text
candidate = run_fast_block1(best)
decision = eval.compare(mode=technical, candidate=candidate.score, best=best.score,
                        mandatory_scenario_failed=<bool>)
if decision.decision == rollback: rollback; stop
if decision.decision == target_reached: best = candidate; exit loop
if decision.decision == stop: best = candidate; stop
best = candidate
```

## Judge Loop

```text
for round in 1..max_judge_rounds:
    bundle = eval.freeze_bundle(best)
    judge  = $judge(bundle)                      # 3 isolated runs, parallel
    aggregate = eval.aggregate(reports, snapshot_sha=bundle.git_sha)
    decision = eval.compare(mode=judge, candidate=aggregate.mean_total,
                            best=previous_judge_mean, noise_floor=calibrated_noise_floor)
    rollback -> restore previous judged snapshot, stop
    stop     -> stop
    target_reached -> stop
    fixes = prioritize_common_deductions(aggregate)
    candidate = run_fast_block1(best, focused_fixes=fixes)
    accept candidate only by project_cost, tests and mandatory scenario
```

## Guardrails

- A candidate is **never** compared to judge scores that do not exist yet.
- Judges never see the target score; `eval.compare` owns the decision, Codex performs the rollback.
- Rollback restores the last judged or last clean-room-passing snapshot.
- Every round stores artifacts under `evals/<kind>/<git-sha>/` and records the bundle hash, model id and rubric version.

## Stop Conditions

target reached | iteration limit | improvement below noise floor | candidate worse than best | mandatory scenario regressed | time reserve reached.

## Final Freeze

1. Full clean-room gate on the best snapshot: fresh checkout, setup, compose build, MCP handshake, end-to-end scenario, Normal/Fly toggle.
2. If the gate fails, roll back to the last clean-room-passing snapshot.
3. Freeze: README, demo script, evidence-backed Q&A cards.
