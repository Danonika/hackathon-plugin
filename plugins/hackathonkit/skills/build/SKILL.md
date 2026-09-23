---
name: build
description: "Use when executing the current plan stage end to end: route the task, run the matching domain skill(s), run the code-review pair after code changes, and record evidence with plan.update before verification. Do not use for planning ($plan) or scoring ($evaluate); use $task-router for a one-shot task with no stage to advance."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: core
tools:
  - system.context
  - plan.status
  - plan.update
  - routing.fly_recommend
  - routing.fly_feedback
  - documents.ingest
  - documents.extract
  - documents.get
  - data.inspect
  - data.query
  - timeseries.analyze
  - timeseries.detect_anomalies
  - knowledge.index
  - knowledge.search
  - charts.create
  - reports.create
  - audio.transcribe
  - vision.analyze
  - learning.recall
  - learning.record
---

# Build

Execute one plan stage: pick the minimal capability set, run the domain work with real tools, review any code change, then advance the stage with evidence.

## When To Use

- A stage from an existing plan is ready to execute (S2 onward).
- The user asks to build, implement, produce the artifact or run the task end to end.
- After `$plan` has written the mandatory scenario for this task.
- Do not use for planning (`$plan`), evaluation (`$evaluate`), or a one-shot question with no stage (`$task-router`).

## Core Principles

1. **The plan stage is the contract.** Execute the current stage; do not invent extra scope.
2. **Discover before analysing.** `files.describe` / `data.inspect` before deeper tools.
3. **Deterministic tools compute, you interpret.** Never do arithmetic in prose when a tool can.
4. **Evidence or it did not happen.** Every stage closes with a tool result, artifact id or command output.
5. **Review code when code changed.** The pair runs in a fixed order and only with a measured benefit.
6. **Learn durably.** `learning.recall` before, `learning.record` after.

## Workflow

1. Call `plan.status` and take the current stage's goal and exit test.
2. Call `system.context`; in `fly` mode call `routing.fly_recommend` for the task, validate the recommendation, and continue in normal mode silently if it fails.
3. Run `$task-router` to pick the minimal capability set.
4. Run the matching domain skill(s) automatically - `$document-intelligence`, `$data-analysis`, `$time-series-analysis`, `$rag-research`, `$government-services`, `$kazakh-language`, `$report-builder`, `$telegram-integration`, `$devops-expert` - and execute the MCP tools yourself (`documents.*`, `data.*`, `timeseries.*`, `knowledge.*`, `charts.create`, `reports.create`, `audio.transcribe`, `vision.analyze`).
5. After a stage **writes or changes code**, run the code-review pair **in this order**: `$cp-expert-refactoring` first (hot paths, algorithmic/performance with before/after evidence), then `$system-design-expert` (module boundaries, contracts, failure isolation). Apply only changes with a measured benefit and keep public contracts stable. Skip the pair on pure data/report/document stages.
6. Prove the stage's exit test on the real input, not a toy.
7. Call `plan.update` with the new status and evidence.
8. Call `$verification-loop` before claiming the stage done.
9. Call `routing.fly_feedback` when the session ran in fly mode, and `learning.record` when something worked or failed.

### Domain skill routing

| Task signal | Skill(s) |
|---|---|
| PDFs, scans, extraction, classification | `$document-intelligence` |
| CSV/TSV/XLSX tables, profiling, joins | `$data-analysis` |
| Telemetry, sensor streams, anomaly intervals | `$time-series-analysis` |
| Questions over `data/input` | `$rag-research` |
| Government forms, procedures, citations | `$government-services` |
| Kazakh text, transcription, localization | `$kazakh-language` |
| Final deliverable report/brief | `$report-builder` |
| Notifications or a bot for the built project | `$telegram-integration` |
| Docker/compose stack for the built project | `$devops-expert` |

## Decision Matrix

| Signal | Action |
|---|---|
| Code was written or changed | Run the review pair in order, refactoring first |
| Pure data/report/document stage | Skip the review pair |
| Exit test needs a real input | Use the mandatory scenario input, not a sample |
| A tool is missing | Fall back per the skill's failure table; list the gap |
| Stage blocked | Mark `failed` with the reason, re-cut with `$plan` |
| Fly recommendation below threshold | Continue in normal mode, silently |

## Output Contract

- The stage artifact or result, produced with real tools.
- The exit-test proof: command output, artifact id or test result.
- An updated plan stage (`status`, `evidence`) via `plan.update`.
- For code stages: the review pair's findings and which changes were applied.
- A one-paragraph hand-off naming the next stage.

## Verification

- Re-read `plan.status` and confirm the stage moved with evidence attached.
- Re-run or re-open the artifact the exit test names.
- For code stages, confirm the review pair ran in order and every applied change has a measured benefit.
- Confirm no unhandled envelope warnings changed the interpretation.
- If any check fails, the stage is not done; fix and repeat.

## Failure Modes

| Symptom | Action |
|---|---|
| Tool returns `isError` | Read the message, adjust arguments, retry once; then report the blocker |
| Deterministic tool missing | Use the documented fallback; record the gap in the stage |
| Exit test passes on a toy only | Re-run on the mandatory scenario input |
| Review suggests a rewrite | Apply only measured, contract-preserving changes |
| Stage overruns its timebox | Mark `failed`, cut scope, re-plan |

## Anti-Patterns

- Doing arithmetic or statistics in prose instead of with a tool.
- Marking a stage done from intent rather than a result.
- Running the review pair before the refactoring pass (order is fixed).
- Expanding scope beyond the current stage.
- Skipping `plan.update` so the next session cannot resume.

## References

- `references/domain-routing.md` - which skill to run for which task signal, with examples.
- `references/build-evidence.md` - what counts as evidence per stage type.
