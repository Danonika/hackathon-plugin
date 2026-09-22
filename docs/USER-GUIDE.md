# Hackathon Agent Kit user guide

**Short version: state the task, or use a command. The agent runs the skills and MCP
tools for you — you never have to remember tool names.** A skill's `tools:` list is what
the agent executes automatically when the skill triggers.

## Install once

```sh
scripts/setup.sh        # Windows: scripts/setup.ps1
```
Then start a **new** harness session from the repo root and run `scripts/doctor.sh`.

## Commands

| Command | Use it to | The agent runs |
|---|---|---|
| `/plan [task]` | plan the work | `$build-planner` → `plan.create` / `plan.status` / `plan.update` |
| `/research [question]` | research a theme, or answer from provided documents | `$theme-research` / `$rag-research` (+ the `researcher` subagent) |
| `/build [stage]` | execute a stage end to end | `$task-router` + the domain skill(s) + the MCP tools; code stages then run `$cp-expert-refactoring` → `$system-design-expert` |
| `/evaluate` | score the demo and run improvement rounds | `eval.freeze_bundle`, `$judge`, `$project-cost`, `$self-improvement-controller` |
| `/verify` | prove the work before claiming it | `scripts/quality-gate.sh` — 7 checks incl. **coverage** |

`/verify --target N` overrides the coverage threshold. Commands work in Claude Code
(plugin manifest), OpenCode/Claude (generated mirrors) and Codex (`~/.codex/prompts`).

## Who does what

- **You:** state the task, or run one of the commands above.
- **The agent, automatically:** reads `AGENTS.md`; calls `system.context` and
  `system.resources`; triggers the implicit skills (`$task-router` on every tool task,
  `$build-planner` on multi-step work, `$theme-research` on unfamiliar themes, the
  matching domain skill per task); runs every tool the skill lists; updates the plan
  with evidence; records learnings; and runs `/verify` before saying "done".
- **You name a skill only to force it**, e.g. `$judge`, `$project-cost`, `$roast-project`.

## A typical run

```
/plan Build a telemetry anomaly dashboard from data/input/sample-telemetry.csv
/research Common anomaly-detection methods for hourly sensor data
/build                                  # or just describe the task
/verify
/evaluate
```

## Notes

- **Fly** is opt-in (`configs/routing.yaml`, `mode: normal|fly`). In `normal` mode
  `$task-router` makes no Fly call; `go run ./cmd/fly-bench` is the Normal-vs-Fly demo.
- **Keys** go to `.env` via `sh scripts/env-set.sh .env KEY` (value on stdin). Record
  which resources you have with `system.resources_set`; `routing.variant` maps them to
  execution variants (deterministic fallback when a key is missing).
- **Code stages** run the review pair in order: `$cp-expert-refactoring` (hot paths,
  before/after evidence) then `$system-design-expert` (boundaries, contracts, failure
  isolation) before the stage gate.
- **Evidence first:** no number or quote without a tool result, artifact id or test output.
