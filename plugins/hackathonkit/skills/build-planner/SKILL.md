---
name: build-planner
description: "Use when starting a multi-step build (3+ stages): produce a staged S0-S6 plan with entry/exit gates, timeboxes and exit evidence before the first tool call, then keep it current with plan.create/plan.update/plan.status. Do not use for single-tool questions or for research itself; use $task-router for one-shot tasks and $theme-research for the S1 brief."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: core
tools:
  - system.context
  - system.capabilities
  - system.context_budget
  - plan.create
  - plan.update
  - plan.status
  - reports.create
  - learning.recall
  - learning.record
---

# Build Planner

Plan the build in stages before writing anything. The plan under `evals/plans/<slug>.json` is the contract the improvement loops, gates and clean-room freeze run against.

## When To Use

- A new build or feature that spans 3+ stages, more than one tool call, or more than one work session.
- Before the first tool call of a build; re-plan after two failed gates, a scope change or an invalidated assumption.
- When resuming a session: `plan.status` returns `current_stage` for the handoff block.
- When the mandatory demo scenario changes or a time budget must be re-cut.
- Do not use for one-shot questions (`$task-router` alone is enough), for producing research (`$theme-research`), or for evaluation scoring (`$project-cost` and `$judge` are explicit-only).

## Core Principles

1. **Plan by outcome, not by tool list.** Every stage names the artifact or check that proves it is done.
2. **The mandatory scenario is the contract.** Write it as input -> command -> expected output before stage work starts; the improvement loops, `$project-cost` and the clean-room gate verify the same scenario.
3. **Every stage has an exit test that can fail.** "Do the frontend" is not a stage; "the mandatory scenario returns the expected artifact" is.
4. **Gates require evidence** - a tool result, test output, `artifact_id` or trace id. A claim is not evidence.
5. **The plan file is the source of truth.** `plan.update` bumps the revision and preserves history; never edit `evals/plans/*.json` by hand.
6. **Protect the reserve.** `finalization_reserve_minutes` (default 45) is untouchable; the last 45 minutes belong to the freeze and demo prep.

## Workflow

1. Call `system.context` and read `routing_mode`. Call `learning.recall` with `query` = the build task, `limit` 5, `scope` `project`; apply plan-level lessons before designing stages.
2. Call `system.capabilities` to inventory capability groups the build can reuse. Before adding context-heavy components (large skills, many references), call `system.context_budget` and follow its `recommendations`.
3. Write S0 before anything else:
   - task statement: input -> expected output;
   - mandatory scenario: `{input, command, expected}` - the exact request a judge will run;
   - constraints: time budget, required formats, secrets/keys, known limitations;
   - targets from `configs/evaluation.yaml`: `technical_target` 95, `demo_day_target` 90, `max_technical_iterations` 2, `max_judge_rounds` 2, `judges` 3, `finalization_reserve_minutes` 45, `min_delta` 0.5.

   Example S0 for `telemetry-anomalies`:
   - Task: input hourly telemetry CSV -> output anomaly list, chart and report.
   - Mandatory scenario: `{input: "data/input/sample-telemetry.csv", command: "Find anomalies and produce a chart and report", expected: "anomaly timestamps + chart artifact + report artifact"}`.
   - Constraints: single session, no external APIs, report in Russian and English.
   - Time: 6 hours total, 45-minute reserve, so 5h15m of build time.
4. Call `plan.create` with `slug` (short kebab-case, e.g. `telemetry-anomalies`), `task`, `mandatory_scenario` and, when customizing, `stages`:
   ```json
   {"id":"S3","goal":"vertical slice: mandatory scenario passes","status":"todo","timebox_minutes":90}
   ```
   Omit `stages` to use the default S0-S6 with timeboxes 20/30/45/90/60/60/30. Keep the returned `path`, `revision` and `summary`.
5. Call `reports.create` with `title: "Build plan - <task>"`, `format: md` and one section per stage (goal, entry, exit evidence, timebox, rollback point). Keep the `build-plan-<slug>` `artifact_id`; the judge evidence bundle uses it.
6. Per stage: `plan.update` with `status: doing` when work starts; when the gate passes, `plan.update` with `status: done` and `evidence` entries (artifact ids, test output, trace ids). Valid statuses: `todo`, `doing`, `done`, `failed`, `cut`.
7. Gate check before marking done (full checklist in `references/stage-gates.md`):
   - at S4, when the stage changed code, run `$cp-expert-refactoring` first (hot paths,
     before/after evidence) and then `$system-design-expert` (boundaries, contracts, failure
     isolation); record their findings as stage evidence;
   - exit evidence exists as a tool result, test output or `artifact_id`;
   - the mandatory scenario still passes, or the stage did not touch it;
   - no secret, token or absolute machine path leaked into an artifact;
   - new warnings from tool envelopes are recorded in the stage evidence.
8. Re-plan when: two consecutive gate failures in one stage; the user changes the mandatory scenario; research invalidates a core assumption; remaining time <= `finalization_reserve_minutes`. Use `plan.update` to change goals or mark dropped stages `cut` - never grow scope silently.
9. Resume with `plan.status` (`slug` optional; defaults to the most recently updated plan). It returns `current_stage` and `summary`; the SessionStart hook reads the same file for the handoff block.
10. At S5, hand scoring to the explicit-only skills: `$project-cost` for technical readiness, `$judge` for the Demo Day rubric. Gate on `eval.compare` decisions, not prose:
    - technical mode rolls back on any score decrease;
    - judge mode rolls back only on a regression above the calibrated noise floor;
    - `target_reached` accepts and exits; an improvement below the floor is `stop` on that axis;
    - at least three valid non-conflicted judge runs (substitute passes replace dropped reports), `eval.calibrate` gives `noise_floor = max(min_delta, 2 * retest_delta)`; ranking follows regulation 9.6 via `eval.rank`.
11. At S6 the freeze flow owns `eval.freeze_bundle` (read-only bundle + hash) and the clean-room gate; the final `plan.update` records the bundle hash as evidence.
12. Close with `learning.record`: trigger "when planning a multi-step build", insight, outcome, evidence (plan path, artifact ids, decisions).

### Time Budgeting

| Build size | Stages to keep | Build time | Reserve |
|---|---|---|---|
| Demo (<= 3h) | S0, S3, S5, S6 | ~2h15m | 45 min |
| Half day (4-6h) | S0-S4 + S5/S6 | ~4h15m-5h15m | 45 min |
| Full day (8h+) | all S0-S6 | ~6h45m+ | 45 min |

Cut from the middle (S2/S4), never from S0, the vertical slice, or the reserve.
If a stage exceeds its timebox by more than 50%, re-plan before continuing.

## Decision Matrix

| Signal | Action |
|---|---|
| 3+ stages, multi-session or multi-tool build | Create a full plan with `plan.create` |
| 1-2 tool calls, single question | Skip planning; `$task-router` is enough |
| Domain is unfamiliar | Run `$theme-research` before S1; store its `artifact_id` as S1 evidence |
| Mandatory scenario fails at any stage | Do not advance; roll back to that stage's rollback point |
| Two consecutive gate failures | Re-plan the remaining stages; cut scope explicitly if needed |
| Research contradicts a core assumption | Re-run S0/S1 and update the plan revision |
| Remaining time <= finalization reserve | Stop building; move to S5/S6 |
| `system.context_budget` flags a skill or reference | Trim before adding; keep workflows lean |
| `plan.status` returns `partial` (no plan) | Create one with `plan.create`; do not improvise stages |
| `plan.update` rejects an unknown stage | Read `plan.status` and use an existing stage id |
| Judge run inconclusive (< 2 valid reports) | Fix the report schema and re-aggregate; never treat as a pass |
| Technical score decreases | `eval.compare` returns `rollback`; revert instead of arguing |

## Output Contract

- Plan file `evals/plans/<slug>.json` with `slug`, `revision`, `path`, stages, `current_stage`, `summary`.
- Mandatory scenario written as `{input, command, expected}`.
- Every stage: goal, timebox, status, evidence list, rollback point.
- Human-readable artifact `build-plan-<slug>` with one section per stage.
- Final answer states: plan path, current stage, gate evidence, open risks, and the next stage's entry criteria.

## Verification

- `plan.status` shows the revision and statuses you reported; every `done` stage has non-empty `evidence`.
- The mandatory scenario was executed and its output matches `expected` (test output or run log).
- The plan artifact exists and came back with `status` != `error`; its id is listed in the answer.
- Timeboxes fit the budget minus `finalization_reserve_minutes`.
- Warnings from every gate envelope are recorded, not dropped.
- The plan's `current_stage` is the stage you claim to be working on.
- Every `cut` stage has a reason in its evidence, and no `done` stage depends on it.

## Failure Modes

| Symptom | Action |
|---|---|
| `plan.create` returns `error` (empty slug) | Pass a non-empty kebab-case `slug`; retry once |
| `plan.update` returns `error` (`unknown stage`) | `plan.status` to list stage ids; update an existing one |
| Invalid status passed | Use only `todo`, `doing`, `done`, `failed`, `cut` |
| `plan.status` returns `partial` | No plan exists; create one before continuing |
| `reports.create` fails | The plan JSON is still the source of truth; retry with shorter sections |
| Gate passes but the evidence is a claim | Do not mark `done`; produce the tool result or artifact first |
| Scope grew silently | Re-plan explicitly: update goals or mark dropped stages `cut` |
| Timebox exceeded | Re-plan remaining stages; protect the finalization reserve |
| Judge round inconclusive | Fix the report schema (evidence, criteria, matching total) and re-aggregate |
| Mandatory scenario regressed | Roll back to the last `done` stage's rollback point; update the plan |
| `plan.status` shows a revision lower than expected | Another session updated the plan; re-read it before editing |
| Plan depends on a missing capability | Mark the stage `failed`, re-plan with the nearest available capability |

## Anti-Patterns

- Planning by tool list instead of by outcome and evidence.
- Stages without gates ("do the frontend") or goals that cannot fail.
- Marking a stage `done` from memory instead of `plan.update` with evidence.
- Editing `evals/plans/*.json` by hand instead of using `plan.update`.
- Silent scope growth; adding features after S0 without a re-plan.
- Treating a `partial` or inconclusive judge aggregate as a pass.
- Building into the finalization reserve.
- Inventing plan revisions or evidence that do not exist.

## References

- `references/stage-gates.md` - the S0-S6 stage table, gate checklist, evidence examples and re-planning triggers.
- `references/plan-tooling.md` - exact `plan.*` arguments, status semantics, a create/update/status cookbook and the artifact handoff to judge and project-cost.
