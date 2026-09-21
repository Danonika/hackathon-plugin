# Context Budget Playbook

How to read `system.context_budget`, what the flags mean, and how to compact context without losing evidence.

## Envelope Fields

| Field | Meaning | Use |
|---|---|---|
| `total_tokens` | estimated tokens for all measured items | the before/after number to report |
| `by_kind` | token totals per kind: `tool`, `skill`, `reference`, `template`, `manifest`, `asset` | see which layer dominates |
| `tool_count` | registered MCP tools (currently 33) | context for the tool-schema total |
| `items[]` | every measured item with `kind`, `name`, `bytes`, `lines`, `tokens`, `flag` | triage list, sorted by tokens descending |
| `recommendations[]` | `item`, `reason`, `suggestion` for the largest problems | the action queue |
| `limits` | `tool_tokens_warn` 500, `skill_lines_warn` 400, `manifest_tokens_warn` 4000, `description_chars` 500 | interpret `flag` values |
| `estimate` | `tokens ~= bytes / 4` | the estimation rule in use |
| `warnings` | present when one or more items are flagged | part of the output contract |

The call takes no arguments and is deterministic; run it twice and the numbers match unless files changed.

## Token Estimation

- The estimator is `ceil(bytes / 4)`; treat it as a directional measure, not an exact tokenizer count.
- Line counts are reported for skills and manifests; a flag on a skill uses lines, a flag on a tool or manifest uses tokens.
- Do not convert tokens back into bytes and quote the result as a measurement; only the tool's fields are evidence.
- Conversation history is not part of the estimate; the tool measures static context an agent carries.

## Flag Thresholds

| Flag | Limit | Interpretation | First action |
|---|---|---|---|
| `schema+description > 500 tokens` | 500 per tool | keeping this tool active is costly | substitute a lighter capability |
| `SKILL.md > 400 lines` | 400 lines | oversized workflow prompt | use it without references |
| `description > 500 chars` | 500 chars | trigger matching cost on every turn | narrow the trigger; load on demand |
| manifest aggregate > 4000 tokens | 4000 total | always-on instructions are heavy | summarize from headings |
| tool schemas aggregate > 8000 tokens | 8000 total | capability sprawl | shrink the active set |

A `warnings` entry appears when at least one item is flagged; the count is not an error, it is the prompt to act.

## Triage Order

1. Items with a `flag` and a direct recommendation.
2. The largest `tokens` value per kind (`tool`, `skill`, `manifest`).
3. Aggregate recommendations (`tools` over 8000 tokens).
4. Everything else stays - the budget is not a reason to churn.

## Progressive Disclosure Patterns

| Situation | Pattern |
|---|---|
| Skill needed | read `SKILL.md`; open a reference only when a step names it |
| Large markdown file | `rg -n "^#" <path>` for the outline, then read the target section |
| Long structured tool output | request the narrow fields (`columns`, `limit`) instead of the full result |
| Document question | `knowledge.search` with `limit` 5 and a focused query; `documents.get` with `offset` for context |
| Repeated lookups | keep one canonical result; drop the duplicates |
| Resumed session | load the artifacts named by the handoff, not the whole transcript |

## Compaction Strategy

| Class | Rule | Keep in context |
|---|---|---|
| Evidence required for the answer | keep verbatim | numbers, units, parameters, source ids |
| Long but reusable | offload to an artifact | `artifact_id` plus a 3-5 line summary |
| Superseded | drop the older version | the newest result |
| Exploratory dead ends | drop | the gap statement and what was searched |
| Warnings and errors | keep or move into an artifact | the exact message |

Order: drop duplicates, then offload long results, then defer references, then shrink the active capability set. Never drop evidence, warnings or errors.

## Artifact Offloading Recipe

1. Identify the part that must survive: exact values, parameters, sources.
2. Condense it into at most 1-2 pages; do not paste raw JSON.
3. `reports.create` with `title: "Context offload - <topic>"`, `format: md`, sections: Purpose, Condensed content, Source refs, Open items.
4. Verify the returned `artifact_id` and `status`.
5. Keep the id plus a short summary in context; state the drop and the restore path.
6. On demand, restore from the artifact (or re-run the source tool) instead of reconstructing from memory.

Raw source text (documents, transcripts) is ingested by the document skills with `documents.ingest`, which also returns an artifact id; do not inline whole files.

## Restore Protocol

| Dropped item | Restore by |
|---|---|
| Offloaded summary | read the `Context offload - <topic>` artifact |
| Raw tool result | re-run the tool with the recorded parameters |
| Skill reference | load `references/<file>.md` when the step needs it |
| Capability schemas | call the tool again; the schema is always available |
| Conversation detail | ask for the missing fact; never invent it |

Every drop in the output must name one of these paths.

## Reporting Before and After

- Record `total_tokens` and `by_kind` before any trimming.
- After trimming, call the tool again and record both totals.
- Report the delta and the remaining flags; the delta is the tool's arithmetic.
- If trimming did not lower the total, say why (for example, the weight sits in manifests that cannot move).

## Worked Example

| Step | Tool | Result |
|---|---|---|
| measure | `system.context_budget` | `total_tokens` 12,400; flagged skill `report-builder/SKILL.md` (over 400 lines); `by_kind.tool` 8,300 |
| capability check | `system.capabilities` | three groups unused by the plan |
| offload | `reports.create` | `Context offload - telemetry anomalies` -> `artifact_id` |
| drop | - | raw anomaly dump (restore: re-run `timeseries.detect_anomalies`) |
| re-measure | `system.context_budget` | `total_tokens` 7,100; one reference deferred |

Report: 12,400 -> 7,100 tokens; kept the anomaly values and chart id; dropped the raw dump with a restore path.

## Anti-Patterns

- Estimating tokens in prose and reporting them as measured.
- Offloading without a `status` check on the artifact call.
- Dropping the warning text because it is "just noise".
- Re-reading an offloaded artifact every turn instead of trusting the summary.
- Treating the before/after number as a quality metric for the model.
