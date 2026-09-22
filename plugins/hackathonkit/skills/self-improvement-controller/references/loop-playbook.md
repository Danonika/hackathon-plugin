# Loop Playbook: Rollback, Stop and Pseudocode

## Rollback Playbook

`eval.compare` returns `rollback` in exactly three cases. The response is the same every time:

1. Stop the round; do not "fix forward" on a regressed candidate.
2. Restore the last judged or clean-room-passing snapshot, which is the accepted `best`, not the candidate.
3. Record the round: candidate bundle hash, git sha, `decision_path`, `reason`, `improvement`, `noise_floor`.
4. `plan.update` the current stage with `status=failed` and the evidence list.
5. `learning.record` with `outcome=failure`, the trigger and the insight ("do not retry X without Y").
6. If the rollback came from a failed mandatory scenario, fix the scenario before any new scoring.

| Rollback cause | What it means | What to do next |
|---|---|---|
| `mode=technical`, `candidate < best` | the change made the technical score worse | inspect the round diff, isolate the offending change, revert it |
| `mode=judge`, regression > noise floor | judges scored the candidate below the previous accepted mean beyond noise | keep the previous judged snapshot; re-check `common_deductions` before another attempt |
| `mandatory_scenario_failed=true` | the demo path broke | treat as critical; fix the scenario and re-run the clean-room gate |

## Stop Playbook

`stop` means further work on this axis has no measurable effect. It is not a failure.

- Keep the candidate only if it did not regress; otherwise restore `best`.
- Record `decision=stop` and the `improvement`/`noise_floor` pair.
- Move to another axis (bugs -> structure -> performance) or exit to the final freeze.
- Never lower the target or the noise floor to force an `accept`.

Stops that are not decisions: iteration cap reached, remaining time <= `finalization_reserve_minutes`, no fixable findings left. Record which one fired.

## Technical Loop Pseudocode

```text
best = project_cost(best_snapshot)
for i in 1..max_technical_iterations:
    candidate = run_fast_block(best)              # one axis, targeted tests
    bundle    = eval.freeze_bundle(out="data/judge-bundles", read_only=true, model=<model>)
    score     = project_cost(candidate)
    decision  = eval.compare(mode="technical", candidate=score, best=best.score,
                             mandatory_scenario_failed=<bool>, snapshot_sha=bundle.git_sha,
                             persist=true)
    if decision == "rollback":       restore(best); record_failure(); stop
    if decision == "target_reached": best = candidate; break
    if decision == "stop":           best = max(best, candidate); stop
    best = candidate                              # accept
```

## Judge Loop Pseudocode

```text
noise    = calibration.noise_floor            # from eval.calibrate on the same snapshot
previous = null
for round in 1..max_judge_rounds:
    bundle    = eval.freeze_bundle(best)
    reports   = judge_three_isolated_copies(bundle)     # $judge, 3 runs, parallel
    aggregate = eval.aggregate(reports=reports, snapshot_sha=bundle.git_sha)
    if aggregate.inconclusive: fix_reports(); re_aggregate()
    decision  = eval.compare(mode="judge", candidate=aggregate.mean_total,
                             best=previous, noise_floor=noise,
                             snapshot_sha=bundle.git_sha, persist=true)
    if decision == "rollback":       restore(last_judged); record_failure(); stop
    if decision == "target_reached": stop
    if decision == "stop":           stop
    previous  = aggregate.mean_total
    best      = run_fast_block(best, fixes=aggregate.common_deductions)
```

## Worked Round (illustrative numbers)

```text
best technical score      = 88.0     (accepted snapshot sha aaa111)
fast block                = fix 2 high roast findings, 1 perf change
candidate bundle hash     = sha256:9f2c...
candidate technical score = 91.5
eval.compare(mode=technical, candidate=91.5, best=88.0, target=95)
  -> decision=accept, improvement=3.5, noise_floor=0.5
plan.update(stage="S4", status="doing", evidence=[...])
next round: judge loop; calibration noise_floor = 1.0 (min_delta 0.5, retest_delta 0.5)
judge mean_total = 84.0, previous judge mean = 84.0
  -> improvement=0.0 < 1.0 -> decision=stop; keep the snapshot, stop judging this axis
```

## Final Freeze Checklist

- [ ] Full clean-room gate on the best snapshot: fresh checkout, `scripts/setup.sh`, compose build.
- [ ] MCP handshake and the mandatory scenario pass in the clean room.
- [ ] Normal/Fly toggle checked on the frozen snapshot.
- [ ] `eval.freeze_bundle` hash and git sha recorded with the final report.
- [ ] README, demo script and Q&A cards written from the passing snapshot only.
- [ ] Remaining gaps and known limitations listed explicitly.
