---
name: context-management
description: "Use when a session grows long, tool output is verbose or the context window is tight: measure it with system.context_budget, interpret per-item tokens and flags, prefer progressive disclosure, offload long results into report artifacts and summarize, and state what was dropped. Do not use for production code cleanup or for scoring the answer itself ($output-self-check)."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: core
tools:
  - system.context_budget
  - system.capabilities
  - reports.create
---

# Context Management

Keep the working context lean without losing evidence: measure, disclose progressively, offload and summarize, and account for every drop.

## When To Use

- The session is long, answers slow down, or earlier tool results crowd out the current task.
- Tool output is verbose (large JSON, full file dumps, long transcripts) and only part will be needed.
- A request needs several skills or references and the budget must be checked before loading them.
- The task must span turns and intermediate findings should be persisted as artifacts.
- A context-budget warning appeared in an envelope or via `system.context_budget`.
- Do not use for trimming production code or repository files - that is a refactor task; do not use to score answer quality - `$output-self-check` is explicit-only.

## Core Principles

1. **Measure, never guess.** `system.context_budget` is the only source of token numbers; prose estimates are not evidence.
2. **Smallest sufficient context.** Load a skill's `SKILL.md` before its references, and a reference only when the current step names it.
3. **Summarize, then offload.** Long results become a condensed artifact with an `artifact_id`; context keeps the id plus the few lines the answer needs.
4. **Dropped context is announced.** Every removal comes with what, why and how to restore it; silent loss is the failure mode.
5. **Evidence outranks budget.** If a drop would remove required evidence, keep it and cut something else; never trim the proof.
6. **The measurement has a boundary.** The tool covers tool schemas, skills, references and manifests - not conversation growth; report that boundary instead of pretending to measure more.

## Workflow

1. Call `system.context_budget` (no arguments) at the start of a heavy task and after two or more large tool results. Keep `total_tokens`, `by_kind`, `items`, `recommendations`, `limits`, `warnings` and the `trace_id`.
2. Read the envelope: `items` is sorted by `tokens` descending; `estimate` is `bytes / 4`; `flag` marks items over a limit. Thresholds and their meaning are in the table below and in `references/context-budget-playbook.md`.
3. Call `system.capabilities` to confirm which capability groups the plan actually uses; do not keep groups that no remaining step calls.
4. Apply `recommendations` in order of savings:
   - avoid loading flagged skills and references;
   - trim overlapping tool schemas or descriptions in the workflow design;
   - replace whole-file reads with targeted reads: headings first (`rg -n "^#" <path>`), then an offset read for the needed section;
   - consolidate repeated tool results into one.
5. Prefer progressive disclosure: load `SKILL.md` first, follow only the reference the current step names, and stop when the answer is found.
6. **Offload long results.** When a result or analysis is larger than the immediate answer needs and must survive later turns, condense it with `reports.create`: `title` `Context offload - <topic>`, `format` `md`, sections Purpose, Condensed content, Source refs, Open items. Keep the `artifact_id` and replace the raw content in context with the id plus a 3-5 line summary containing the exact numbers needed.
7. Raw source documents are not ad-hoc paste: their ingestion belongs to `$rag-research` / `$document-intelligence` (`documents.ingest`), which return an `artifact_id`; request that handoff instead of quoting whole files.
8. State the drops: keep a visible list of item, reason and restore path (`artifact_id`, path or command). Re-read on demand instead of pre-loading.
9. Re-measure after compaction: call `system.context_budget` again and report `total_tokens` before/after and the remaining flags; the delta is a tool number, not an estimate.
10. Answer per the Output Contract; the answer itself should not re-inline what was just offloaded.

### Flag Thresholds

| Item kind | Flag when | What it means | Action |
|---|---|---|---|
| tool | schema+description > 500 tokens | expensive to keep active | use the closest lighter tool or restructure the step |
| skill | `SKILL.md` > 400 lines | oversized workflow | use it without loading references |
| skill description | > 500 chars | trigger cost on every turn | prefer narrower triggers; load on demand |
| manifest | > 4000 tokens | always-on instructions | summarize from headings |
| tools aggregate | > 8000 tokens total | capability sprawl | shrink the active set to the plan |

### Triage Order

| Priority | Target | Action |
|---|---|---|
| 1 | flagged items with a recommendation | apply the suggested fix first |
| 2 | largest `tokens` value per kind | substitute, defer or summarize |
| 3 | aggregate tool cost over 8000 tokens | shrink the active capability set |
| 4 | unflagged items | keep; churn is not a saving |

One `system.context_budget` call settles all four rows; re-run it after the first three are done.

### What To Drop First

| Priority | Drop | Keep |
|---|---|---|
| 1 | duplicate or superseded results | the latest result |
| 2 | raw output already offloaded | the artifact id and summary |
| 3 | unused capability groups and their schemas | the groups the plan calls |
| 4 | reference detail behind a link | the `SKILL.md` step and the link |
| 5 | exploratory dead ends | the gap statement (searched X, found none) |

Evidence, warnings and error messages are never dropped; they move into the answer or an artifact.

### Worked Example

A telemetry scan returned a 6,000-token anomaly dump plus a chart; the answer needs two rows and the chart id.

1. `system.context_budget` before: `total_tokens` 12,400; the analysis sits in the conversation.
2. `reports.create` with `title` `Context offload - telemetry anomalies`, sections Purpose, Condensed content (anomaly table), Source refs (`trace_id`, input path), Open items (causes not assessed).
3. Keep the `artifact_id` plus a three-line summary (two anomalies, timestamps and values, chart id); drop the raw dump and state the drop and the restore path.
4. `system.context_budget` after: `total_tokens` 7,100; remaining flags: one skill reference, deferred.
5. Report: 12,400 -> 7,100 tokens, the kept evidence, and one dropped item with its restore path.

## Decision Matrix

| Signal | Action |
|---|---|
| A needed skill is flagged | Load its `SKILL.md` only; defer references until a step names one |
| A tool schema exceeds 500 tokens | Substitute the closest lighter capability and note the substitution |
| A manifest exceeds 4000 tokens | Summarize from headings; do not re-read the whole file |
| Tool schemas total > 8000 tokens | Shrink the active capability set to the plan's steps |
| A long result is needed later | Offload with `reports.create`; keep the `artifact_id` |
| A long result is not needed | Drop it and state the drop with the reason |
| The answer is one line in a large file | Targeted read (`rg`, then offset) instead of a full load |
| Repeated similar results | Keep the latest; state that earlier ones were superseded |
| A drop would remove required evidence | Keep the evidence; cut lower-priority context instead |
| The offload artifact errored or does not exist | Keep a compact inline summary; do not lose the numbers |
| A raw source document must persist | Hand off to `$rag-research` / `$document-intelligence` for `documents.ingest` |
| A session resumes | Re-run `system.context_budget`; load the offload artifacts named by the handoff |

## Output Contract

- `total_tokens` before and after, plus `by_kind` from the tool (both numbers, not one).
- Flagged items with their exact `flag` text and the flags that remain after trimming.
- Actions taken: loads avoided, results offloaded (`artifact_id`), items dropped with restore paths.
- What was kept and why (the evidence the answer requires).
- The measurement boundary: static schemas, skills, references and manifests - not conversation growth.
- Warnings from every `system.context_budget` envelope.

## Verification

- Every token number came from `system.context_budget`; no number is estimated in prose.
- Each offload artifact exists, returned `status` != `error`, and contains the exact numbers removed from context.
- The dropped list is complete: item, reason, restore path.
- No required evidence or warning was dropped; if one was, the answer says so explicitly.
- The before/after delta matches the two tool results.
- Recommendations were followed or consciously deferred with a stated reason.

## Failure Modes

| Symptom | Action |
|---|---|
| `system.context_budget` returns `error` | Retry once; on repeat continue conservatively and state that no measurement was available |
| `reports.create` fails | Keep a compact inline summary and note that offload was not persisted |
| Nothing is flagged but the session is heavy | State the measurement boundary: it covers static context, not conversation growth |
| An item is over the flag but still required | Load the minimum (`SKILL.md`, one section) and record the accepted cost |
| A result is too large to read before offload | Read headings or offsets first; never paste unbounded output |
| Offloading loses fidelity | Store exact values in the artifact; keep units and parameters verbatim |
| Two summaries cover the same data | Keep the artifact version; state which is authoritative |
| A dropped item turns out to be needed | Restore from the recorded path or artifact; do not guess its content |
| Budget warnings persist after trimming | Report the remaining flags and their cost; do not hide them |
| The task needs a document ingested | Hand off to `$rag-research` / `$document-intelligence`; do not inline the file |

## Anti-Patterns

- Quoting token counts from memory instead of the tool envelope.
- Loading every reference "just in case".
- Pasting the same long result a second time after it was summarized.
- Dropping context silently and answering as if nothing was removed.
- Using the budget as an excuse to skip verification or required evidence.
- Summarizing away warnings, errors or coverage gaps.
- Offloading content without keeping the `artifact_id` and a restore path.
- Treating `system.context_budget` as a model-performance benchmark.

## References

- `references/context-budget-playbook.md` - envelope fields, token estimation, flag thresholds, compaction strategy, artifact offloading recipe and restore protocol.
