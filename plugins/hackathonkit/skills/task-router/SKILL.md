---
name: task-router
description: "Entry point for every Hackathon Agent Kit task: reads the live routing mode (normal or fly), selects capabilities and verifies results. Use when a request needs files, documents, tables, telemetry, audio, images or reports. Do not use for pure conversation without tool work."
---

# Task Router

## When To Use

- Any task that needs Hackathon Agent Kit MCP capabilities: files, documents, knowledge search, tables, time series, classification, charts, reports, audio, images.
- Before calling any domain tool.
- Whenever a previous plan failed and the approach must be re-planned.

## Step 0 - Always

1. Call `system.context`. It returns `routing_mode`, `routing_config_version` and the live `capabilities` list.
2. `routing_mode == "fly"` -> follow the Fly Protocol below.
3. `routing_mode == "normal"` -> select capabilities yourself from `references/tools.md`.

## Fly Protocol

1. Call `routing.fly_recommend` with:
   - `task`: one sentence in the user's language.
   - `input_metadata`: `types`, `rows`, `columns`, `has_timestamp` (use `files.describe` first when unsure).
2. Read `recommendations`, `route`, and `metrics.max_score` / `metrics.threshold`.
3. Validate every recommendation against the real task and input: available tool, sensible order, correct arguments expected.
4. Accept, reject, add, remove or reorder freely. Fly is a hint; Codex decides.
5. On error, timeout, empty list or `max_score < threshold`: continue in normal mode silently. Never tell the user the task is blocked by Fly.

## Planning Rules

- Discover before analysing: `files.describe` or `data.inspect` before deeper tools.
- Deterministic tools compute; you interpret. Never invent numbers, timestamps or quotes.
- Keep `trace_id` and `sources` from every envelope; cite them in the final answer.
- One retry on transient errors is fine; on persistent failure, report exactly which tool failed and why.
- Prefer few decisive calls over exploratory spam.

## Output Contract

Final answer contains: result, evidence (sources), parameters used (method, window, threshold, sheet), warnings, and limitations. If the task produced artifacts, list their `artifact_id`s.

## Quality Checks

- Did you follow the mode from `system.context`, not from memory?
- Are all numbers traceable to a tool response?
- Are warnings from tool envelopes included in the answer?

## References

- `references/tools.md` - complete tool catalogue with arguments
- `references/fly-protocol.md` - detailed Fly mode protocol and fallbacks
- `references/evidence-model.md` - how to cite each source type
