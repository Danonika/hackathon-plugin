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

1. Main scenario passes; otherwise fix it first.
2. Judge calibration completed once: score one frozen snapshot twice, `noise_floor = max(min_delta, 2 * retest_delta)`.
3. Time budget check: stop self-improvement when remaining time <= `finalization_reserve_minutes`.

## Fast Block 1

1. `$roast-project` (3 critics, parallel) -> fix critical and high findings.
2. `$cp-expert-refactoring` -> apply accepted changes.
3. Targeted tests.
4. `$system-design-expert` -> apply accepted proposals.
5. Targeted tests again.
6. Quick smoke: existing MCP image starts, `initialize`, `tools/list`, main happy path.
7. Build demo evidence (demo-artifacts bundle).
8. Freeze candidate snapshot.

Full clean-room rebuild is **not** part of each iteration; it runs once on the final candidate.

## Technical Loop

```text
candidate = run_fast_block1(best)
if mandatory_scenario_failed(candidate): rollback; stop
if project_cost_regressed(candidate, best): rollback; stop
best = candidate; if best.score >= technical_target: exit loop
```

## Judge Loop

```text
for round in 1..max_judge_rounds:
    bundle = freeze_judge_bundle(best)
    judge  = $judge(bundle)                      # 10 isolated runs, parallel
    if previous judge mean exists:
        regression > noise_floor  -> rollback to previous judged snapshot, stop
        improvement < noise_floor -> stop
    if judge.mean >= demo_day_target: stop
    fixes = prioritize_common_deductions(judge)
    candidate = run_fast_block1(best, focused_fixes=fixes)
    accept candidate only by project_cost, tests and mandatory scenario
```

## Guardrails

- A candidate is **never** compared to judge scores that do not exist yet.
- Judges never see the target score; the controller owns acceptance decisions.
- Rollback restores the last judged or last clean-room-passing snapshot.
- Every round stores artifacts under `evals/<kind>/<git-sha>/` and records the bundle hash, model id and rubric version.

## Stop Conditions

target reached | iteration limit | improvement below noise floor | candidate worse than best | mandatory scenario regressed | time reserve reached.

## Final Freeze

1. Full clean-room gate on the best snapshot: fresh checkout, setup, compose build, MCP handshake, end-to-end scenario, Normal/Fly toggle.
2. If the gate fails, roll back to the last clean-room-passing snapshot.
3. Freeze: README, demo script, evidence-backed Q&A cards.
