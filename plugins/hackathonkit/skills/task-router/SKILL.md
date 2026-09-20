---
name: task-router
description: "Use when a request needs Hackathon Agent Kit MCP tool work: files, documents, knowledge search, tables, telemetry, audio, images, charts or reports. Reads the live routing mode (normal/fly), selects the minimal capability set, validates Fly recommendations, enforces evidence rules and records a learning when the task ends. Do not use for pure conversation without tools, or for scoring owned by explicit-only skills such as $judge and $project-cost."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: core
tools:
  - system.context
  - system.capabilities
  - system.context_budget
  - routing.fly_recommend
  - routing.fly_feedback
  - files.describe
  - learning.recall
  - learning.record
---

# Task Router

Entry point for every Hackathon Agent Kit task with tool work: read the live routing mode, select the smallest capability set that can finish the task, and verify the result with evidence.

## When To Use

- Any request that needs Hackathon Agent Kit MCP capabilities: files, documents, knowledge search, tables, time series, classification, charts, reports, audio, images.
- Before the first domain tool call of a task, and again after a failed plan, a scope change or a session resume.
- When the routing mode may have changed (`normal` vs `fly`) or capability selection is non-obvious.
- When a task produced artifacts and the answer must list them with sources.
- Do not use for pure conversation with no tool work, and do not use for scoring or evaluation calls owned by explicit-only skills (`$judge`, `$project-cost`, `$self-improvement-controller`).

## Core Principles

1. **Mode comes from the live call, never from memory.** `system.context` re-reads `configs/routing.yaml` on every call, so a toggle can happen mid-session and takes effect without a restart.
2. **Deterministic tools compute; you interpret.** Never do arithmetic, statistics, date parsing or aggregation in prose when a tool returns the number.
3. **Discover before analysing.** `files.describe` / `data.inspect` before extraction, indexing, querying or analysis. Guessed columns, sheets or timestamp formats are the most common source of wrong answers.
4. **Fly is a recommender, never an orchestrator or a failure point.** Codex validates every recommendation, keeps the final decision, and continues in normal mode silently on any Fly problem.
5. **One number, one source.** Every claim traces to a tool result, `artifact_id` or test output; warnings from the envelopes are part of the answer, not noise.
6. **Close the loop.** Recall before, record after: `learning.recall` when the task starts, `learning.record` once the task has a verified outcome.

## Workflow

1. Call `system.context` (no arguments). Read `routing_mode`, `routing_config_version` and the live `capabilities` list. Keep the returned `trace_id`.
2. Call `learning.recall` with `query` = the task in one sentence, `limit` 5 and `scope` `project` (the default; it includes global learnings). Apply relevant matches to tool choice. A `partial` envelope with `count: 0` means no history - continue normally.
3. When the capability map is unclear, call `system.capabilities` for capability groups and the experimental `fly_router` flag. When the task will load context-heavy components (large skills, many references, long reports), call `system.context_budget` first and follow its `recommendations` before adding context.
4. Inventory the inputs with `files.describe` for each candidate path in `data/input`. Capture `types`, `rows`, `columns`, `has_timestamp` and XLSX `sheets`. These values become the `input_metadata` for routing.
5. Branch on the live mode:
   - `routing_mode: normal` - select capabilities yourself from `references/tools.md`.
   - `routing_mode: fly` - run steps 6-7 before selecting tools.
6. Call `routing.fly_recommend` with:
   - `task`: one sentence in the user's language.
   - `input_metadata`: `types`, `rows`, `columns`, `has_timestamp` from step 4.
   - `available_capabilities`: omit to rank the full list; pass it only when the plan is already constrained.
   Read `recommendations`, `route`, `metrics.max_score`, `metrics.threshold` and `metrics.encoder`.
7. Validate every recommendation against the real task and the observed input: the tool exists, the order is sensible, the expected arguments are available. Accept, reject, add, remove or reorder freely. If `max_score < threshold`, or the call errors, times out or returns an empty list, continue in normal mode silently - never surface routing trouble as a task failure. Full protocol: `references/fly-protocol.md`.
8. Execute the selected tools in dependency order:
   - discovery: `files.describe`, `data.inspect`
   - extraction and data: `documents.extract` / `documents.ingest`, `data.query` / `data.analyze`, `timeseries.analyze` / `timeseries.detect_anomalies`
   - retrieval: `knowledge.index`, `knowledge.search`
   - output: `classification.classify`, `charts.create`, `reports.create`, `audio.transcribe`, `vision.analyze`
   Use documented defaults unless the task justifies others (for example `knowledge.search` `limit` 5, `text_chars` 300; `timeseries.detect_anomalies` `method` `zscore`, `window` 24, `threshold` 3.0; `data.query` `limit` 20).
9. Keep from every envelope: `sources`, `warnings`, `trace_id` and any `artifact_id`. One retry on a transient error is acceptable; on a repeat failure report the exact tool, arguments and error.
10. After a fly-mode task, call `routing.fly_feedback` exactly once with `task` (the same text used for routing), `chosen` (tools actually executed), `recommended` (when available) and `source`: `accepted` | `codex-correction` | `normal-teacher` | `fallback`.
11. After any non-trivial task, call `learning.record` once with `trigger` (when it applies), `insight` (what to do or avoid), `outcome` `success` | `failure` | `neutral`, `scope` `project` and `evidence` (artifact ids, trace ids, test output). Never record unverified output as a lesson.
12. Answer per the Output Contract below.

### Selection Order

| Step | Prefer | Then | Only if needed |
|---|---|---|---|
| Understand input | `files.describe` | `data.inspect` | `documents.extract` preview |
| Reuse corpus | existing `index_id` | `knowledge.index` | `documents.ingest` first |
| Compute | `data.query` / `data.analyze` | `timeseries.*` | `classification.classify` |
| Present | `reports.create` | `charts.create` | `vision.analyze` / `audio.transcribe` |

### Routing Examples

| Request | Normal-mode plan | Fly check |
|---|---|---|
| "Find anomalies in `data/input/sample-telemetry.csv` and chart them" | `files.describe` -> `timeseries.detect_anomalies` -> `charts.create` -> `reports.create` | Expect a `timeseries` route; validate the timestamp column exists |
| "Summarise `contract.pdf` and cite the clauses" | `$rag-research`: `files.describe` -> `documents.ingest` -> `knowledge.index` -> `knowledge.search` | Expect a `rag` route; keep `documents.extract` for a quick preview |
| "Transcribe `meeting.m4a` and write a report" | `audio.transcribe` (requires `OPENAI_API_KEY`) -> `reports.create` | Expect a `speech` route; if the key is missing, report the fallback |
| "Score this build for Demo Day" | Do not route implicitly - `$judge` is explicit-only | No Fly call |

## Decision Matrix

| Signal | Action |
|---|---|
| `routing_mode: normal` | Select from `references/tools.md`; no Fly call |
| `routing_mode: fly` | `routing.fly_recommend` first, then validate every item |
| Fly `max_score < threshold`, error, timeout or empty list | Continue in normal mode silently; do not mention the failure |
| Recommendation names an unavailable tool | Drop it; substitute the nearest capability from `references/tools.md` |
| `files.describe` shows no timestamp column but the task is temporal | Do not call `timeseries.*`; inspect columns and state the limitation |
| Multi-step build (3+ stages) | Hand off to `$build-planner`; do not improvise stage order |
| Unfamiliar theme or facts outside the repository | Hand off to `$theme-research` |
| Question over provided documents | Hand off to `$rag-research` |
| `system.context_budget` flags an item | Trim context (move depth to `references/`) before adding more |
| `learning.recall` returns a high-confidence match | Apply it and cite the learning id in the answer |
| Task produced artifacts | List every `artifact_id` in the answer |
| A score or accept/rollback decision is needed | Explicit-only skills own it; never invoke them implicitly |

## Output Contract

- **Result** - the direct answer to the request.
- **Evidence** - `sources` and `trace_id` for every claim; one number, one source.
- **Parameters** - mode (`normal`/`fly`), tools used, filters, limits, windows, sheets.
- **Artifacts** - every `artifact_id` produced and what it contains.
- **Warnings** - copied from the envelopes, including truncation and fallbacks.
- **Gaps** - what could not be established and which queries or tools were tried.
- **Learning** - the `learning.record` id when one was stored, or why none was.

## Verification

- `routing_mode` in the answer came from the live `system.context` call, not memory.
- Every number, timestamp and quote maps to a tool result or `artifact_id`.
- Every artifact named exists and came back with `status` != `error`.
- Warnings from all envelopes are present in the answer.
- If the mode was `fly`, `routing.fly_feedback` was called exactly once with the executed tool list.
- If a learning was recorded, its `id` and `confidence` are reported.
- Coverage is stated for retrieval tasks ("searched N chunks from M documents") and gaps are explicit.
- No claim rests on model memory; anything unverifiable is labelled `unverified`.

## Failure Modes

| Symptom | Action |
|---|---|
| `system.context` returns `error` | Retry once; if it persists, report the failure and stop - never guess the mode |
| Domain tool returns `partial` | Use the covered part, quote the warning, mark the uncovered part as a gap |
| Domain tool returns `error` | Fix arguments once (path, column, sheet); on repeat, report tool + arguments + error |
| Path rejected (absolute or `..`) | Rewrite relative to `data/input`; artifact ids are relative to `data/artifacts` |
| `learning.recall` returns `partial` (`no learnings matched`) | Continue; this is not an error |
| `learning.record` fails or the store is disabled | Continue the task; state that the lesson was not persisted |
| Fly feedback fails | Do not block the answer; keep the trace id and continue |
| Context budget flags a reference or skill | Do not load the flagged item; use the smallest reference that answers the question |
| Unknown tool name suggested anywhere | Verify against `references/tools.md` before calling; never invent a tool |
| Two different tools give conflicting numbers | Prefer raw data over derived summaries; report both and the trace ids |
| Task needs a tool that requires `OPENAI_API_KEY` and it is unset | Report the limitation and offer the local alternative; do not fake the output |

## Anti-Patterns

- Reading the mode from memory or assuming `normal` because Fly is experimental.
- Treating `routing.fly_recommend` output as an execution plan instead of a scored shortlist.
- Calling every available tool "just in case" instead of the minimal decisive set.
- Skipping `files.describe` / `data.inspect` and guessing columns, sheets or timestamp formats.
- Reporting a routing failure, a below-threshold score or an internal fallback as a task failure.
- Recording learnings from unverified output or from Fly's own recommendations.
- Invoking explicit-only evaluation skills implicitly.
- Leaving warnings out of the final answer because the result looked fine.

## References

- `references/tools.md` - complete MCP tool catalogue with exact arguments and defaults; read before selecting capabilities.
- `references/fly-protocol.md` - request/response shapes, validation rules, fallback matrix and feedback sources for fly mode.
- `references/evidence-model.md` - how to cite each source type and keep an evidence ledger for the final answer.
