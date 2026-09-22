---
name: data-analysis
description: "Use when a CSV, TSV or XLSX table needs structure discovery, profiling, filtered queries, statistics, correlations or a data-quality assessment. Produces computed numbers with sources, optional charts and an evidence-backed report. Do not use for time-indexed trends or anomalies; use $time-series-analysis. Do not use for PDF, DOCX or free-text extraction; use $document-intelligence."
metadata:
  version: "2.1.0"
  origin: hackathonkit
  category: domain
tools:
  - files.describe
  - data.inspect
  - data.query
  - data.analyze
  - charts.create
  - reports.create
---

# Data Analysis

Profile, filter and quantify CSV/TSV/XLSX tables with deterministic tools; interpret the numbers instead of computing them.

## When To Use

- A CSV, TSV or XLSX file is provided and the user asks for facts, distributions, comparisons, totals or data quality.
- A claim needs a computed number behind it rather than an impression.
- A table must be triaged before time-series work (`$time-series-analysis`) or cited in a deliverable (`$report-builder`).
- Do not use for timestamp-indexed trends, seasonality or anomaly detection; use `$time-series-analysis` instead.
- Do not use for PDF, DOCX or free-text extraction; use `$document-intelligence` instead.

## Core Principles

1. Discover before analysing: `files.describe` then `data.inspect` before any query.
2. Deterministic tools compute; you interpret. Never derive a mean, sum or correlation in prose from returned rows.
3. One number, one source: every figure carries the tool, `path`, `sheet`, filter and `n`.
4. `missing` values and mixed types are findings, not noise; report them before aggregates.
5. Correlation is not causation: report `r`, `n` and direction; never a mechanism.
6. Envelope `warnings` are part of the answer; quote them, never drop them.

## Workflow

1. `files.describe` with `path` (relative to `data/input`). Read `extension`, `size_bytes`, `rows`, `columns`, `headers`, `has_timestamp`; for XLSX also `sheets` and `first_sheet`. If several sheets exist, choose one and pass `sheet` to every later call.
2. `data.inspect` with `path`, `sheet` (default: first sheet), optional `columns`. Read `profile[]` per column: `kind` (`numeric` / `timestamp` / `text`), `non_empty`, `missing`, `min`, `max`, `mean`, `std`, `samples`.
   - `kind: numeric` requires every non-empty value to parse as a number.
   - `kind: timestamp` requires at least 80% of non-empty values to parse as dates.
   - `has_timestamp` from step 1 only matches header keywords (`time`, `date`, `timestamp`, `datetime`, `ts_`, and Cyrillic equivalents); a header such as `ts` with a BOM can report `false` while `data.inspect` still finds `kind: timestamp`.
   - More than 64 columns triggers a warning; pass a `columns` subset for what you report.
3. `data.query` with `path`, `sheet`, `columns`, `where`, `limit` (default 20, max 200; `limit <= 0` or `> 200` falls back to 20) to pull the rows behind a claim.
   - `where` is `"column op value"`; ops are `=`, `==`, `!=`, `>`, `>=`, `<`, `<=`, `contains`.
   - `contains` is a case-insensitive substring match; `=`/`!=` compare numbers when both sides parse, otherwise case-insensitive text.
   - `>`, `>=`, `<`, `<=` require numeric cells on both sides and skip rows that do not parse.
   - `columns` selects the returned projection only; it does not affect `matched_rows`.
4. Compare `matched_rows` (all matches) with `returned` (rows after `limit`). If `matched_rows > returned`, raise `limit` up to 200 or report the count; never extrapolate the unseen rows.
5. `data.analyze` with `path`, `sheet`, optional `columns` for Pearson correlations over complete numeric pairs. It returns `profile` plus up to 5 `correlations` `{columns, r, n}` with `abs(r) >= 0.3` and `n >= 5`; `r` is rounded to 2 decimals.
6. `charts.create` when a visual helps: `title` (default `"Hackathon Agent Kit chart"`), `chart_type` `line` (default) or `bar`, `series: [{label, points: [{x, y}]}]` (points also accept `t`/`v`). Bar renders only the first series; line renders each series in its own colour. Keep the returned `artifact_id`.
7. `reports.create` with `title`, `format` `md` (default) or `html`, `sections: [{heading, body}]` (at least one non-empty section) when a deliverable is requested; keep `artifact_id` and inspect `preview`.

## Tool Call Reference

| Tool | Required | Optional (default) | Key fields to read |
|---|---|---|---|
| `files.describe` | `path` | - | `rows`, `columns`, `headers`, `has_timestamp`, `sheets` |
| `data.inspect` | `path` | `sheet`, `columns` | `profile[].kind`, `missing`, `min/max/mean/std` |
| `data.query` | `path` | `sheet`, `columns`, `where`, `limit` (20) | `matched_rows`, `returned`, `rows` |
| `data.analyze` | `path` | `sheet`, `columns` | `correlations[].columns/r/n` |
| `charts.create` | `series` | `title`, `chart_type` (line) | `artifact_id`, `points` |
| `reports.create` | `title`, `sections` | `format` (md) | `artifact_id`, `sections`, `chars`, `preview` |

## Worked Example

Request: "Profile `sample-telemetry.csv` and quantify the extremes."

1. `files.describe` -> `rows: 72`, `columns: 5`, `headers: ["\ufeffts","sensor_id","value","temperature","pressure"]`, `has_timestamp: false` (the BOM-prefixed `ts` header does not match a timestamp keyword), `size_bytes: 3350`.
2. `data.inspect` -> `ts` kind `timestamp`, `sensor_id` kind `text`, `value` numeric `min -12.3 / max 47.5 / mean 10.250833... / std 5.281549...`, `temperature` numeric `min 20.2 / max 21.8`, `pressure` numeric `min 3.7 / max 4.3`; all columns `missing: 0` over 72 rows. Report the exact values or state that they are rounded.
3. `data.query` with `where: "value > 20"`, `limit: 5` -> `matched_rows: 1`, `returned: 1`, row `{ts: "2026-01-02 11:00:00", value: "47.5"}`.
4. `data.analyze` -> one correlation: `{columns: ["temperature","pressure"], r: -0.65, n: 72}`.
5. Report: the high extreme is a single point (47.5 at 2026-01-02 11:00); the strongest association is a moderate negative `temperature`/`pressure` correlation; filter for the low extreme separately before quoting it.

## Analysis Cookbook

- Count and share: `files.describe` gives the denominator (`rows`), `data.query` gives `matched_rows` for the numerator. Report both counts; if you state a percentage, label it as derived from those two tool values.
- Extreme rows: use `data.query` with `>` / `<` at the value returned by `data.inspect` (`min`/`max`) to fetch the exact row and its timestamp.
- Correlation follow-up: `data.analyze` finds `{columns, r, n}`; then query the tails of one column to show the rows behind the association. Never claim direction of cause.
- Data-quality audit: `files.describe` + `data.inspect`; report per-column `missing`, `kind: text` where numeric was expected, and every warning. Duplicate-looking rows are a hypothesis: query a candidate key and compare `matched_rows`.
- Filtered statistics limitation: `data.inspect` and `data.analyze` profile the whole sheet; there is no filtered-statistics tool. For a group comparison, report per-group counts from `data.query` and whole-file statistics separately; never compute a group mean by hand.
- Deliverable packaging: `charts.create` from computed points, then `reports.create` with one section per question and a data-quality section; cite both `artifact_id`s.

## Decision Matrix

| Signal | Action |
|---|---|
| `has_timestamp: false` but a `kind: timestamp` column exists | Header-based detection missed it; trust `data.inspect` and pass the column explicitly to later tools |
| Column `kind: text` where numbers were expected | Mixed types: report `missing` and `samples`; compute nothing from it |
| `missing` is large (rule of thumb > 20%) | Treat as a data-quality finding and state the effective `n` before aggregates |
| `matched_rows > returned` | Raise `limit` (<= 200) or report only the count |
| `data.analyze` returns no `correlations` | State the absence and the thresholds (`abs(r) >= 0.3`, `n >= 5`) |
| XLSX has several `sheets` | Select `sheet` explicitly; the wrong sheet yields plausible but wrong numbers |
| More than 64 columns | Pass a `columns` subset for the profile you report |
| Two or more numeric columns, user asks about "the value" | State which column you chose and why; never silently average unrelated columns |
| User asks "why" or "will it happen" | Report association only; label any interpretation separately |
| User asks for KPIs, funnels, cohorts, retention or an experiment readout | Define the metric first (population, numerator, denominator, window), then compute; see `references/analytics-playbook.md` |
| A rate is requested | Report numerator and denominator, never a bare percentage; weight ratios by their base |

## Output Contract

- Facts: value + tool + `path` + `sheet` + `where` + `n`; keep the precision the tool returned, and label any rounding.
- Interpretation clearly separated from facts.
- Data-quality section: `missing` counts, mixed types, suspected duplicates, plus every envelope `warning`.
- Artifact ids for charts (`charts/<slug>-<timestamp>.svg`) and reports (`reports/<slug>-<timestamp>.md` or `.html`).
- Explicit gaps: questions the tools could not answer and why.
- Wording patterns that keep provenance visible:
  - `value` has 72 rows, 0 missing, min -12.3, max 47.5 (`data.inspect`, `sample-telemetry.csv`).
  - 1 of 72 rows matches `value > 20` (`data.query.matched_rows`), first match 47.5 at 2026-01-02 11:00.
  - `temperature` and `pressure` correlate at `r = -0.65` over `n = 72` (`data.analyze`); association, not causation.

## Verification

- Re-check every quoted number against a fresh `data.inspect`, `data.query` or `data.analyze` result.
- Confirm `status` is `ok` for every envelope used; report `partial`/`error` with the exact message otherwise.
- Confirm the `where` string you report is the one you sent, and that `matched_rows` >= the rows quoted.
- Confirm each `artifact_id` exists under `data/artifacts` and was produced for this task.
- Confirm every column name in the answer appears in `headers` or `profile[]`.

## Failure Modes

| Symptom | Action |
|---|---|
| `empty table` | The file has no data rows; report it, do not retry |
| `unknown column in where: X` | Re-read `headers` from `files.describe`; names match case-insensitively and by substring |
| `unsupported operator ">>"` | Use a documented op; re-send the corrected `where` once |
| `where must be "column op value"` | A `where` needs at least three whitespace-separated fields |
| `> >= < <=` matches nothing | Comparisons need numeric cells; check `kind` and use `contains` for text |
| `legacy .xls (BIFF) ... not supported` | Ask for `.xlsx` or `.csv`; no fallback exists |
| `input file not found: X` | Paths are relative to `data/input`; absolute paths and `..` are rejected |
| `limit` appears ignored | Values `<= 0` or `> 200` clamp to 20; use a tighter `where` |
| `data.analyze` has no correlations | Small `n` or weak `r`; report the absence with the thresholds |
| `charts.create` returns an error | `series` needs at least one point with a numeric `y`; rebuild the points from tool output |

## Anti-Patterns

- Computing sums or averages from `data.query` rows instead of `data.inspect` / `data.analyze`.
- Quoting a mean without `missing`, `n` and column `kind`.
- Treating `matched_rows` as the returned row count.
- Charting a `text` column or mixing units in one series.
- Presenting correlation as causation or as a forecast.
- Silently dropping a truncation or >64-column warning.
- Reading only the first 20 rows and calling the result complete.
- Reporting a group mean when no tool produced it; state the limitation instead.
- Using a whole-file statistic to answer a question about a filtered subset.

## References

- `references/query-cookbook.md` - operator semantics, coercion rules, pagination and worked filters against a real file.
- `references/statistics-and-quality.md` - profile rules, correlation thresholds, data-quality checklist and interpretation wording.
- `references/analytics-playbook.md` - metric definitions, KPI trees, funnels, cohorts/retention, experiment readouts, unit economics and metric traps.
