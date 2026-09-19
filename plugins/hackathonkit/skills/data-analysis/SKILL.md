---
name: data-analysis
description: "Use for CSV, TSV and XLSX tables: structure discovery, profiling, filtering, statistics, correlations and data-quality reporting. Covers files.describe, data.inspect, data.query, data.analyze and chart/report output."
---

# Data Analysis

## When To Use

- The input contains a table (CSV, TSV, XLSX) and the user asks for facts, statistics, comparisons or data quality.
- Before time-series work, to locate timestamp and numeric columns.
- When a claim must be backed by computed numbers.

## Workflow

1. `files.describe` - shape, headers, `has_timestamp`, XLSX `sheets`. Pick `sheet` when there are several.
2. `data.inspect` - column kinds (numeric / timestamp / text), missing values, min/max/mean/std.
3. `data.query` - filtered subsets. `where` syntax: `column op value`, ops `= == != > >= < <= contains`.
4. `data.analyze` - correlations with `abs(r) >= 0.3` and at least 5 paired points.
5. Deliver: `charts.create` for visuals, `reports.create` for the summary.

## Interpreting Profiles

- `kind: numeric` means every non-empty value parsed as a number; mixed columns stay `text`.
- `kind: timestamp` requires 80% of non-empty values to parse as dates.
- Always report `missing` counts before drawing conclusions; a column that is 40% empty is a data-quality finding, not a fact.
- Correlations are Pearson on complete pairs. Correlation is not causation; state the direction and sample size (`n`), never a cause.

## Output Contract

- Numbers exactly as returned by tools, with the parameter context: sheet name, filter, `n`.
- Separate "computed facts" from "interpretation".
- Include data-quality findings (missing values, mixed types, duplicate-looking rows) in a dedicated section.
- List artifact ids for any chart or report created.

## Failure Modes

| Symptom | Action |
|---|---|
| `empty table` error | Say the file has no data rows; do not retry blindly |
| Unknown column in `where` | Re-check headers via `files.describe` |
| More than 64 columns | Analyse a column subset; note the truncation warning |
| XLSX sheet missing | List `sheets` from `files.describe` and pick again |

## Anti-Patterns

- Computing statistics by hand from `data.query` output instead of `data.inspect`/`data.analyze`.
- Reporting a mean without `n` and missing-value context.
- Charting before profiling: axes built on mixed-type columns mislead.
