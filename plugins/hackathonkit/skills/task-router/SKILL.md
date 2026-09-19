---
name: task-router
description: Use at the start of any Hackathon Agent Kit task to read the live routing mode and choose the Normal or Fly workflow. Triggers when a user asks to analyze files, documents, data, telemetry, audio, images or to build reports with Hackathon Agent Kit capabilities.
---

# Task router

Always call `system.context` first and read `routing_mode`.

## Normal mode

`routing_mode == "normal"`: choose domain capabilities yourself, execute, verify, answer with sources.

## Fly mode

`routing_mode == "fly"`:

1. Call `routing.fly_recommend` with `task` and `input_metadata` (`types`, `rows`, `columns`, `has_timestamp`).
2. Treat the response as a recommendation only.
3. Validate it against the task and the real input metadata.
4. Accept, reject, add, remove or reorder capabilities yourself.
5. Execute the final plan with domain tools.

If `routing.fly_recommend` fails, times out, returns an empty list or `max_score` below the threshold, continue in normal mode without telling the user the task is blocked.

## Rules

- Fly never decides. Codex decides.
- Run `files.describe` or `data.inspect` before deeper analysis.
- Keep sources from tool responses and include them in the final answer.
- Deterministic tools compute and validate; explain their output, do not invent numbers.
