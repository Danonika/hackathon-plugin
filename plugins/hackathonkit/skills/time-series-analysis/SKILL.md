---
name: time-series-analysis
description: "Use when telemetry, sensor readings or metrics carry a timestamp and the task needs trends, step changes, rolling z-score or IQR anomaly detection, or a downsampled series for a chart. Produces anomaly tables with timestamps, values, scores and method parameters. Do not use for static table profiling or group comparisons; use $data-analysis. Do not use for forecasting future values."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: domain
tools:
  - files.describe
  - data.inspect
  - timeseries.analyze
  - timeseries.detect_anomalies
  - charts.create
  - reports.create
---

# Time Series Analysis

Quantify trends and detect anomalies in timestamped tables with deterministic tools; explain the method and the shape of each finding.

## When To Use

- A table has a timestamp column plus at least one numeric measurement (telemetry, metrics, logs with rates, financial ticks).
- The user asks about trends, spikes, drops, degradation, step changes or "unusual" periods.
- A prior `data-analysis` pass found extreme values that need temporal context.
- Do not use for static profiling, filtered group statistics or correlations; use `$data-analysis` instead.
- Do not use for forecasting, capacity planning or prediction; no tool fits a model.

## Core Principles

1. Confirm the time and value columns before analysing; auto-detection picks the first timestamp column and the first numeric column that is not the time column.
2. The tools compute; you interpret. Never recompute a mean, z-score or slope in prose.
3. Method parameters (`method`, `window`, `threshold`, `limit`) are part of every result; report them with the findings.
4. An anomaly is a statistical outlier, not a cause. Never name a cause (sensor failure, fraud, attack) without a second source.
5. `slope_per_hour` is a fitted trend over the observed window, not a forecast.
6. Envelope `warnings` are part of the answer; quote them, never drop them.

## Workflow

1. `files.describe` with `path`. Read `rows`, `has_timestamp`, `headers`, and `sheets` for XLSX. `has_timestamp` only matches header keywords (`time`, `date`, `timestamp`, `datetime`, `ts_`, and Cyrillic equivalents), so a BOM-prefixed or custom header can report `false` while values parse as dates.
2. `data.inspect` with `path` and optional `sheet`. Read `profile[]` and confirm which column has `kind: timestamp` and which numeric column is the measurement. If several numeric columns exist, choose one and pass it explicitly in every later call.
3. `timeseries.analyze` with `path`, `sheet`, `time_column`, `value_column`, `max_points` (default 120; `<= 0` or `> 5000` falls back to 120).
   - The tool sorts rows by time and drops rows where either column fails to parse; fewer than 3 valid pairs returns `not enough valid (time, value) rows: N`.
   - Returns `count`, `start`, `end`, `min`, `max`, `mean`, `std`, `slope_per_hour` and `series: [{t, v}]` (RFC3339 UTC, values rounded to 2 decimals) for charting.
   - `slope_per_hour` is the ordinary-least-squares slope converted to units per hour; sign and magnitude matter, extrapolation does not.
4. `timeseries.detect_anomalies` with `path`, `sheet`, `time_column`, `value_column`, `method`, `window`, `threshold`, `limit`.
   - `method: zscore` (default): compares each point with the mean and standard deviation of up to `window` (default 24) preceding points; needs at least 3 preceding points, so the first point is never evaluated.
     - `|z| >= threshold` (default 3.0) flags an anomaly with `score = |z|`.
     - If the window standard deviation is 0 and the point differs from the window mean, the point is flagged with `score = threshold`.
   - `method: iqr`: global quartiles, fences `Q1 - 1.5*IQR` and `Q3 + 1.5*IQR`; `score` is the distance beyond the nearest fence divided by `IQR` (0 when `IQR` is 0).
   - Any method string other than `iqr` runs `zscore`; always send a deliberate value.
   - Anomalies are sorted by timestamp and truncated to `limit` (default 100; `<= 0` or `> 10000` falls back to 100). If `anomaly_count` equals `limit`, raise `limit` and re-run before concluding.
5. `charts.create` with `title`, `chart_type` `line` (default) or `bar`, and `series: [{label: "<value_column>", points: <timeseries.analyze.data.series>}]`. The `{t, v}` points are accepted directly.
6. `reports.create` with `title`, `format` `md` (default) or `html`, `sections: [{heading, body}]`; include a method section (tool, method, window, threshold, count), an anomaly table and a limitations section.

## Tool Call Reference

| Tool | Required | Optional (default) | Key fields to read |
|---|---|---|---|
| `files.describe` | `path` | - | `rows`, `has_timestamp`, `sheets` |
| `data.inspect` | `path` | `sheet`, `columns` | `profile[].kind`, `missing`, `min/max` |
| `timeseries.analyze` | `path` | `time_column`, `value_column`, `max_points` (120) | `count`, `start`, `end`, `slope_per_hour`, `series` |
| `timeseries.detect_anomalies` | `path` | `method` (zscore), `window` (24), `threshold` (3.0), `limit` (100) | `anomaly_count`, `anomalies[].timestamp/value/score` |
| `charts.create` | `series` | `title`, `chart_type` (line) | `artifact_id`, `points` |
| `reports.create` | `title`, `sections` | `format` (md) | `artifact_id`, `preview` |

## Worked Example

Request: "Find unusual `value` readings in `sample-telemetry.csv`."

1. `data.inspect` -> `ts` kind `timestamp`, `value` kind `numeric`, `missing: 0`, `rows: 72`.
2. `timeseries.analyze` with `time_column: "ts"`, `value_column: "value"` -> `count: 72`, `start: 2026-01-01T05:00:00Z`, `end: 2026-01-04T04:00:00Z`, `min: -12.3`, `max: 47.5`, `mean: 10.25`, `std: 5.28`, `slope_per_hour: -0.04`.
3. `timeseries.detect_anomalies` with defaults (`zscore`, `window: 24`, `threshold: 3.0`) -> `anomaly_count: 2`: `{timestamp: 2026-01-02T11:00:00Z, value: 47.5, score: 32.12}` and `{timestamp: 2026-01-03T12:00:00Z, value: -12.3, score: 23.8}`.
4. Same call with `method: "iqr"` -> the same two points with scores `15.02` and `8.13`.
5. `charts.create` with the `series` from step 2 and a title; `reports.create` with method, anomaly table and limitations.

## Parameter Cookbook

| Parameter | Meaning | Guidance |
|---|---|---|
| `method` | `zscore` (default) or `iqr`; any other string runs `zscore` | Choose deliberately; state it in the answer |
| `window` | Number of preceding **points**, not hours or days | Scale it to the expected anomaly duration; 24 points on hourly data is 24 hours, on minute data it is 24 minutes |
| `threshold` | Absolute z-score cut-off (default 3.0) | Lower (2.5) for sensitivity, raise (3.5-4.0) to reduce noise; never tune it just to produce findings |
| `limit` | Maximum anomalies returned (default 100) | Raise before concluding when `anomaly_count == limit` |
| `max_points` | Points in the returned `series` (default 120) | Affects chart size only; statistics always use every valid point |

`iqr` ignores `window` and `threshold`: its fences are global and depend on the
whole series. The `zscore` method is local, so it adapts to slow drift but is
slower to flag the first point of a sustained shift.

## Decision Matrix

| Signal | Action |
|---|---|
| Several numeric columns, user says "the metric" | Pass `value_column` explicitly and say which column you chose |
| Smooth series with a local baseline | `zscore`, `window: 24`, `threshold: 3.0` |
| Baseline drifts over time | `zscore` with a shorter `window` (6-12); raise `threshold` if flags cluster |
| Sparse series (< 50 points) or heavy tails | `iqr`; rolling z-score has too little history |
| Many false positives | Raise `threshold` to 3.5-4.0 or increase `window`; re-run once |
| One sharp step change | `zscore` flags step edges and, when the prior window is flat, scores them at `threshold`; explain the shape, consider `iqr` |
| `anomaly_count == limit` | Raise `limit` (up to 10000) and re-run |
| Multiple sensors/entities in one file | `timeseries.*` has no `where` filter; analyse separate files or report the limitation |
| User asks "what will happen next" | Report the fitted trend only; state that no forecast is available |

## Output Contract

- Method block: tool, `method`, `window`, `threshold`, `limit`, `count`, `start`, `end`.
- Anomaly table: `timestamp`, `value`, `score`, `method`; every row traces to the tool result.
- Trend: `slope_per_hour` with the units of `value_column`; label it a fitted trend.
- Data-quality notes: invalid rows dropped (compare `rows` from `files.describe` with `count`), gaps, duplicate timestamps, and all envelope warnings.
- Artifact ids for charts and reports.
- Explicit gaps: what the method cannot see (causes, forecasts, unparsed timestamps).

## Verification

- Confirm `status` is `ok` for every envelope used; report `partial`/`error` with the exact message otherwise.
- Confirm the reported anomaly count matches `anomaly_count` and that timestamps come from `anomalies[]`, not from memory.
- Re-check that `count` <= the file's `rows` and explain any difference (unparseable pairs).
- Confirm the chart's points come from `timeseries.analyze.data.series` and the report's numbers from the envelopes.
- Re-run the anomaly call with the parameters you report if any value was copied manually.

## Failure Modes

| Symptom | Action |
|---|---|
| `not enough valid (time, value) rows: N` | Fewer than 3 pairs parsed; check column choice and timestamp formats; report N instead of retrying |
| `could not detect time/value columns; pass time_column and value_column explicitly` | Pass both explicitly from `data.inspect` |
| Auto-detected column looks wrong | Pass `time_column` / `value_column` explicitly; never trust the default silently |
| `empty table` | No data rows; report it, do not retry |
| Timestamps fail to parse | Accepted layouts: RFC3339, `YYYY-MM-DD[ HH:MM[:SS]]`, `DD.MM.YYYY`, epoch seconds/milliseconds; report the offending samples |
| `anomaly_count: 0` | A valid result; report method and parameters, do not lower the threshold until something appears |
| All anomaly scores are equal | Likely a constant window with a step (`std = 0` rule); explain and cross-check with `iqr` |
| `anomaly_count == limit` | Results are truncated; raise `limit` and re-run |
| XLSX sheet missing | Re-read `sheets` from `files.describe` |

## Anti-Patterns

- Presenting an anomaly as a cause ("sensor failed", "fraud") without an independent source.
- Running both methods and reporting only the more dramatic one; state the chosen method and parameters.
- Calling `slope_per_hour` a forecast or extrapolating beyond `end`.
- Using the auto-detected value column when several numerics exist without saying which one was analysed.
- Ignoring dropped rows: `count` below `rows` is a data-quality finding.
- Skipping the chart when the user asked to "see" the series.
- Re-running with a lower threshold until an anomaly appears.
- Treating `window: 24` as 24 hours without checking the sampling interval.

## References

- `references/method-selection.md` - z-score versus IQR, parameter cookbook and when each method fails.
- `references/anomaly-interpretation.md` - reading scores, common false-positive shapes and a reporting template.
