---
name: build-planner
description: "Use when starting a multi-step build (3+ stages): produce a staged plan with entry/exit gates, timeboxes and evidence before the first tool call. Not for single-tool questions. Every build stage must end with a verifiable artifact."
---

# Build Planner

Plan the build in stages before writing anything. The plan is the contract the improvement loops and gates run against.

## When To Use

- A new build, or a feature that spans more than one tool call and more than one work session.
- Before the first tool call of a build; re-plan after two failed gates or a scope change.
- Not for one-shot questions - `$task-router` alone is enough for those.

## Inputs

1. Task statement: input -> expected output -> demo scenario (the mandatory scenario).
2. Time budget: `configs/evaluation.yaml` (targets, iteration caps, `finalization_reserve_minutes`).
3. Research brief from `$theme-research` when the domain is unfamiliar.
4. Available capabilities: `system.context` + `system.capabilities`.

## Stage Template

For every stage define: goal, entry criteria, work, exit criteria (evidence), timebox, rollback point.

| Stage | Goal | Exit evidence |
|---|---|---|
| S0 Outcome | task statement, mandatory scenario, constraints | one-paragraph spec + demo scenario |
| S1 Discovery | inputs inventory, research brief, capability check | `files.describe` output, research `artifact_id` |
| S2 Skeleton | build + MCP handshake works end to end with one tool | smoke output (`initialize`, `tools/list`, one call) |
| S3 Vertical slice | mandatory scenario passes on real input | run log + produced artifacts |
| S4 Hardening | error paths, fallbacks, tests, docs | test output, README sections |
| S5 Evaluation | project-cost gate + judge round | `evals/...` artifacts + `eval.compare` decision |
| S6 Freeze | clean-room gate + final bundle | clean-room report + bundle hash |

Small builds may collapse S1-S4, but S0 and the vertical slice are never skipped.

## Gates

- A stage is done only when its exit evidence exists as a tool result, test output or `artifact_id`.
- Gate failed: fix inside the stage; two consecutive failures -> re-plan or cut scope explicitly.
- Never advance with a failing mandatory scenario; `$self-improvement-controller` rolls back instead.
- Record stage status and evidence in the plan artifact after each stage.

## Persist The Plan

- `reports.create` with one section per stage; artifact `build-plan-<slug>`.
- Keep the `artifact_id`; the judge bundle evidence list includes it.
- Update by creating a new revision (`-r2`, `-r3`) - never rewrite history silently.

## Integration

- `$task-router` selects capabilities per stage; `$theme-research` fills S1.
- `$project-cost` scores the snapshot at S5; `$judge` scores the frozen bundle.
- `$self-improvement-controller` runs bounded rounds against the plan's mandatory scenario and gates.

## Quality Checks

- Does every stage have an exit test that can fail?
- Is the mandatory scenario written as input -> command -> expected output?
- Does the plan fit the time budget including the finalization reserve?

## Anti-Patterns

- Planning by tool list instead of by outcome.
- Stages without gates ("do the frontend").
- Silent scope growth; re-plan explicitly instead.

## References

- `references/stage-gates.md` - gate checklist and plan artifact format
