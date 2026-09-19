---
name: report-builder
description: "Use when the user needs a deliverable: a report, findings document, summary or presentation text assembled from tool results. Covers reports.create and artifact organization."
---

# Report Builder

## When To Use

- The user asks for a report, summary, conclusion document or presentation-ready text.
- Tool work is finished and results must be packaged with evidence.
- Multiple analyses must be combined into one deliverable.

## Structure

1. Task and inputs: file names, artifact ids, period covered.
2. Method: tools used, parameters (method, window, threshold, sheet, filter, `n`).
3. Findings: numbers with sources, ordered by importance.
4. Limitations: warnings from tool envelopes, data-quality issues, coverage gaps.
5. Next steps: concrete, short, only if the task implies them.

## Workflow

1. Collect results and sources from every tool envelope used.
2. Draft sections; keep each finding in one sentence plus its evidence.
3. `reports.create` with `sections:[{heading, body}]`, `format` `md` (default) or `html`.
4. Verify the returned `preview` and `artifact_id`; hand the id to the user.

## Writing Rules

- One number, one source. No number without a tool behind it.
- Prefer tables for comparisons and lists of anomalies.
- The executive summary is the first section and stands alone.
- No tool tokens, trace ids or file system paths in prose; artifact ids are allowed as evidence references.
- Keep language plain; the report may be read by non-technical judges.

## Quality Checks

- Does every claim survive the question "which tool returned this?"
- Are all warnings from the analysis phase present in Limitations?
- Is the first section understandable without reading the rest?

## Anti-Patterns

- A report longer than the analysis deserves; 1-2 pages per question.
- Hiding a failed sub-analysis; report it in Limitations.
- Copying raw JSON into the report instead of a readable summary.
