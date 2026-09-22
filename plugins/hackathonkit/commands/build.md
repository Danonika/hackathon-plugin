---
description: Execute the current plan stage end to end: route the task, run the domain skill(s), record evidence.
argument-hint: "[task or stage]"
---

# /build

Build: $ARGUMENTS

The agent selects and runs the tools; the user never has to name them.

1. `$task-router` picks the minimal capability set for the task (it calls
   `routing.fly_recommend` only when `system.context` reports `routing_mode: fly`).
2. Run the matching domain skill(s) automatically: `$document-intelligence`, `$data-analysis`,
   `$time-series-analysis`, `$rag-research`, `$government-services`, `$kazakh-language`,
   `$report-builder`, `$telegram-integration`, `$devops-expert`.
3. Execute the MCP tools yourself (`documents.*`, `data.*`, `timeseries.*`, `knowledge.*`,
   `charts.create`, `reports.create`, `audio.transcribe`, `vision.analyze`, ...).
4. After a stage **writes or changes code**, run the code review pair **in this order**:
   `$cp-expert-refactoring` first (hot paths, algorithmic/performance with before/after
   evidence), then `$system-design-expert` (module boundaries, contracts, failure isolation).
   Apply only changes with a measured benefit and keep public contracts stable. Skip this on
   pure data/report/document stages.
5. `plan.update` the stage with evidence, then `$verification-loop` before claiming it done.
6. `learning.record` when something worked or failed.
