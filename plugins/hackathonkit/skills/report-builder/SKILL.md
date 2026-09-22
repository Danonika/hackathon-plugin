---
name: report-builder
description: "Use when tool work is complete and results must become a deliverable: report, findings summary, comparison write-up or presentation-ready text with evidence and limitations. Produces a stored markdown or HTML artifact via reports.create. Do not use to run the analysis itself; use the domain skill first. Do not use for judge scoring or cost estimates; use $judge or $project-cost."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: domain
tools:
  - files.describe
  - reports.create
---

# Report Builder

Assemble finished tool results into one evidence-backed deliverable with a clear method, findings and limitations.

## When To Use

- The user asks for a report, summary, findings document, conclusion or presentation-ready text.
- Several analyses must be combined into one artifact with traceable numbers.
- A prior skill produced results and warnings that now need packaging.
- Do not use to run the analysis, query or anomaly detection itself; call the domain skill first.
- Do not use for judge scoring, evaluation or cost estimates; use `$judge`, `$eval.*` flows or `$project-cost`.

## Core Principles

1. One number, one source: every figure carries the tool that produced it, the parameters used and `n` where applicable.
2. The executive summary is the first section and must stand alone.
3. Warnings and failed sub-analyses are part of the report, not footnotes to hide.
4. Facts and interpretation are labelled separately; the reader must know which is which.
5. `reports.create` stores exactly the sections you pass; it does not invent or reorder content.
6. Keep the report proportional: one to two pages per question unless the user asks for more.

## Workflow

1. Inventory the evidence. For every input file still referenced, call `files.describe` with `path` to confirm `extension`, `size_bytes` and, for tables, `rows`/`columns`; drop anything that cannot be confirmed.
2. Collect from every tool envelope used in the task:
   - numbers with their tool name, `path`/`sheet`, filters and `n`;
   - `warnings` (verbatim);
   - `artifact_id`s for charts, extracted text and prior reports;
   - `status` per call, including any `partial`/`error`.
3. Draft sections in this order: Executive Summary, Inputs and Scope, Method, Findings, Data Quality and Limitations, Next Steps (only if implied), Evidence Appendix.
4. Write each finding as one sentence plus its evidence in parentheses: value, tool, source. Prefer tables for comparisons and lists.
5. `reports.create` with `title`, `format` `md` (default) or `html`, and `sections: [{heading, body}]` (at least one non-empty section; empty heading+body pairs are dropped).
6. Verify the returned `artifact_id`, `sections`, `chars` and `preview`; confirm the preview contains the executive summary and that no section was silently dropped. Hand the `artifact_id` to the user.

## Tool Call Reference

| Tool | Required | Optional (default) | Key fields |
|---|---|---|---|
| `files.describe` | `path` | - | `extension`, `size_bytes`, `rows`, `columns`, `sheets` |
| `reports.create` | `title`, `sections` | `format` (`md`) | `artifact_id`, `sections`, `chars`, `preview` |

## Section Blueprint

| Section | Content | Rule |
|---|---|---|
| Executive Summary | The answer in 3-6 sentences with the headline numbers | Must make sense without the rest |
| Inputs and Scope | Files, periods, artifact ids, what was and was not examined | Name coverage explicitly |
| Method | Tools, parameters (method, window, threshold, sheet, filter, `n`) | Enough for a reviewer to reproduce |
| Findings | Ordered by importance; one sentence + evidence each | Tables for comparisons |
| Data Quality and Limitations | Missing data, dropped rows, truncation, every warning, failed sub-analyses | Never omit a warning |
| Next Steps | Concrete actions, only when the task implies them | No generic advice |
| Evidence Appendix | Source map: claim -> tool -> parameters -> artifact/`trace_id` | Optional for short reports |

## Worked Example

Task: package a table profile and an anomaly scan into one report.

1. `files.describe` on `sample-telemetry.csv` -> 72 rows, 5 columns; confirms the input still exists.
2. From the analysis envelopes: `data.inspect` (`value` min -12.3, max 47.5, missing 0), `timeseries.detect_anomalies` (`zscore`, window 24, threshold 3.0, 2 anomalies: 47.5 @ 2026-01-02T11:00:00Z score 32.12; -12.3 @ 2026-01-03T12:00:00Z score 23.8).
3. `reports.create` with sections: Executive Summary ("two isolated outliers, no missing data"), Inputs and Scope, Method, Findings (anomaly table), Data Quality and Limitations ("no causes assessed; no maintenance log available"), Evidence Appendix.
4. Check `preview`: it must start with the report title and contain the summary; keep the `artifact_id`.

## Decision Matrix

| Signal | Action |
|---|---|
| Audience is non-technical | Plain language, summary first, tables over prose, no tool tokens |
| User asks for a file or "deliverable" | `format: "md"` unless HTML is requested; return the `artifact_id` |
| User asks for a presentation | Short sections, one message per section, no long paragraphs |
| Results conflict across tools | Report both with their parameters; explain the difference; do not average |
| A sub-analysis returned `error` | Include it under Limitations with the exact message |
| Numbers came from different filters or periods | Split the findings by period/filter; never blend them silently |
| Evidence is thin (small `n`, truncated text) | Say so in Limitations and lower the confidence wording |
| Report grows past ~2 pages per question | Cut detail, keep evidence; offer a deeper appendix as a second artifact |

## Output Contract

- A stored report artifact with `artifact_id`, `format` and section count.
- Executive summary that stands alone.
- Method with exact parameters and `n`.
- Findings with per-number provenance; tables where comparisons exist.
- Limitations listing every warning, dropped row and failed sub-analysis.
- Optional evidence appendix mapping claims to tools and artifacts.
- If `reports.create` fails: the exact error and the drafted sections as plain text.

## Writing Rules

- Lead with the answer, then the evidence; never build suspense.
- One idea per sentence; define units and periods on first use.
- Use tables for anything with more than three comparable values.
- Round only for readability and say so ("about 10.3"); keep exact values in the appendix.
- Attribute claims inline: `(data.inspect, sample-telemetry.csv)`.
- Write "associated with", not "caused by"; write "observed from X to Y", not "always".
- Put limitations in the same tone as findings; they are results too.
- No emojis, no exclamation marks, no marketing adjectives.

## Length Budget

| Report | Target | Hard limit |
|---|---|---|
| Single question | 1 page | 2 pages |
| Multi-question analysis | 2-3 pages | 5 pages |
| Incident or anomaly write-up | 1-2 pages | 3 pages |

When the material exceeds the budget, cut repetition first, then detail; move
raw tables into an appendix section or a second artifact.

## Verification

- Confirm `status` is `ok` and `artifact_id` is present before claiming completion.
- Read the `preview`: title, executive summary and the first finding must be visible and correct.
- Check that `sections` equals the number of non-empty sections you sent; investigate a lower number.
- Re-check every quoted number against the originating envelope; fix or drop anything that does not trace.
- Confirm every warning from the analysis phase appears in Limitations.
- Confirm no tool tokens, trace ids or absolute paths appear in prose (artifact ids are allowed).

## Failure Modes

| Symptom | Action |
|---|---|
| `title is required` | Pass a non-empty `title`; re-send once |
| `at least one section is required` | Pass `sections` with at least one `{heading, body}` |
| `no valid sections provided` | Sections were empty or malformed objects; rebuild them as `{heading, body}` strings |
| `sections` count lower than expected | Empty heading+body pairs are dropped; fill or remove them |
| A number cannot be traced | Remove it or mark it as unverified; never keep it as a fact |
| Input file no longer exists | `files.describe` returns `input file not found`; report the input as unavailable |
| Preview does not match intent | Re-create with corrected sections; artifacts are immutable, do not edit them |
| Report is much longer than the analysis | Cut to the blueprint; move depth into an appendix artifact |

## Anti-Patterns

- Writing a number that no tool returned, or rounding it without saying so.
- Hiding a failed sub-analysis or a warning to make the report look clean.
- Copying raw JSON into the body instead of a readable table.
- Putting trace ids, tool tokens or absolute file paths in prose.
- Blending findings from different filters or periods into one figure.
- Producing a report longer than the analysis deserves.
- Presenting interpretation in the Findings section without a label.

## References

- `references/report-templates.md` - ready section patterns for analysis, comparison, incident and demo reports.
- `references/evidence-and-citations.md` - source-map format, citation syntax and the pre-publish checklist.
