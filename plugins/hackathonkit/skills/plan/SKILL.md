---
name: plan
description: "Use when asked for a plan, roadmap, stages, milestones or to update plan progress on a multi-step task: create or update the staged S0-S6 build plan with an explicit mandatory scenario via plan.create/plan.update/plan.status. Do not use for single-tool questions or for research itself; use $build-planner directly when you need the full planning protocol."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: core
tools:
  - system.context
  - system.resources
  - system.capabilities
  - plan.create
  - plan.update
  - plan.status
  - learning.recall
  - learning.record
---

# Plan

Turn a multi-step task into a staged S0-S6 contract under `evals/plans/<slug>.json`, with a mandatory scenario that later loops, gates and the judge all verify.

## When To Use

- The user asks for a plan, roadmap, stages, milestones or a timeboxed breakdown.
- Work spans 3+ stages, more than one tool call, or more than one session.
- Resuming a build: `plan.status` returns the current stage for the handoff block.
- The mandatory scenario or time budget changes and the plan must be re-cut.
- Do not use for one-shot questions (`$task-router` alone), for producing research (`$theme-research`), or for scoring (`$project-cost`, `$judge`).

## Core Principles

1. **Plan by outcome, not by tool list.** Every stage names the artifact or check that proves it is done.
2. **The mandatory scenario is the contract.** Write it as input -> action -> expected output before stage work starts.
3. **Every stage has an exit test that can fail.** "Do the frontend" is not a stage; "the scenario returns the expected artifact" is.
4. **Gates require evidence** - a tool result, test output, `artifact_id` or trace id. A claim is not evidence.
5. **The plan file is the source of truth.** `plan.update` bumps the revision; never hand-edit `evals/plans/*.json`.
6. **Protect the reserve.** The finalization reserve belongs to freeze and demo prep and is never spent on build work.

## Workflow

1. Call `system.context` and `system.resources`; if the resource profile is missing, ask once (OpenAI key, NVIDIA key, Telegram bot, Codex Pro, hackathon track) and record it with `system.resources_set`. Never echo a key.
2. Call `plan.status` for the slug.
   - No plan: call `plan.create` with `slug`, `task`, `mandatory_scenario` and the default stages S0-S6 (outcome, discovery, skeleton, vertical slice, hardening, evaluation, freeze).
   - Existing plan: keep the slug and update only the current stage.
3. Write `mandatory_scenario` explicitly: the input file or request, the command or tool call, and the expected observable output. It must be checkable without judgement.
4. Set one timebox per remaining stage and reserve the final 45 minutes for finalization.
5. As work lands, call `plan.update` for the current stage with `status` (todo/doing/done/failed/cut) and `evidence` (artifact id, command output, test result).
6. When a stage is blocked or an assumption is invalidated, mark it `failed` with the reason, then re-cut the remaining stages instead of silently continuing.
7. Call `learning.recall` before re-planning a class of task you have seen; apply a matching lesson instead of repeating a failed shape.
8. Call `learning.record` when a stage reveals a reusable lesson: a `trigger`, an `insight`, an `outcome` and evidence.

### Stage defaults

| Stage | Goal | Exit evidence |
|---|---|---|
| S0 | Outcome, mandatory scenario, constraints | scenario string + track recorded |
| S1 | Inputs inventory and research brief | artifact id from `reports.create` |
| S2 | Skeleton: build + one MCP tool handshake | `system.context` succeeds |
| S3 | Vertical slice: mandatory scenario on real input | command output or artifact |
| S4 | Hardening: error paths, tests, docs | gate output / test result |
| S5 | Evaluation: project-cost gate and judge round | `eval.aggregate` + compare decision |
| S6 | Freeze: clean-room gate and final bundle | bundle hash |

### Timeboxing

- Give each remaining stage a single timebox in minutes; the sum plus the finalization reserve must fit the deadline.
- A timebox is a trigger to cut scope, not a target to overrun. When a stage hits its box unfinished, mark it `failed` and re-cut.
- The finalization reserve (default 45 minutes) is untouchable: freeze, bundle and demo prep only.

### Evidence rules

- Every `plan.update` carries at least one evidence string: an `artifact_id`, a command and its output, a test name, or a trace id.
- Quote the smallest runnable proof; do not paste whole logs.
- A stage cannot be `done` while any of its exit tests is unproven.

## Decision Matrix

| Signal | Action |
|---|---|
| No plan for the slug | `plan.create` with the S0-S6 default stages |
| Plan exists, stage in progress | `plan.status`, then `plan.update` the current stage |
| Mandatory scenario still vague | Stop; sharpen input/action/output before stage work |
| Two consecutive failed gates | Re-plan the stage (smaller step) and record why |
| Scope grew beyond the track | Cut or defer stages explicitly; never extend silently |
| Session resuming | `plan.status` first, then continue the current stage |

## Output Contract

- A plan file at `evals/plans/<slug>.json` with `slug`, `task`, `mandatory_scenario`, stages S0-S6 and timeboxes.
- A short summary naming the current stage, its exit test and the remaining budget.
- For an update: the changed stage, its new status and the evidence string.
- The `revision` returned by `plan.update`.

## Verification

- Re-read with `plan.status` and confirm the revision bumped and the stage status matches what you reported.
- Confirm the mandatory scenario is present and checkable (input, action, expected output).
- Confirm every `done` stage carries evidence, not intent.
- If any of the above fails, the plan is not updated; fix and repeat.

## Failure Modes

| Symptom | Action |
|---|---|
| `plan.create` rejects the slug | Use a shorter kebab-case slug; retry once |
| `plan.update` returns not-found | Re-check the slug with `plan.status`; recreate only if truly absent |
| Stage slips its timebox | Mark `failed`, re-cut into smaller stages, record the reason |
| Evidence is an opinion | Replace with a tool result, artifact id or command output |
| Plan file edited by hand | Recreate via `plan.update`; the file is tool-owned |

## Anti-Patterns

- Planning by tool list instead of by outcome.
- A mandatory scenario that cannot fail (e.g. "the app works").
- Marking a stage `done` with no evidence attached.
- Extending timeboxes to hide a slip instead of cutting scope.
- Keeping stale stages after the task changed.

## References

- `references/stage-gates.md` - what each S0-S6 stage must produce before the next starts.
- `references/plan-tooling.md` - `plan.*` argument shapes and the revision model.
