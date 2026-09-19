---
name: time-series-analysis
description: "Use for telemetry, sensor readings, metrics over time, anomaly detection and trend analysis. Covers timeseries.analyze, timeseries.detect_anomalies (z-score/IQR), chart and report output."
---

# Time Series Analysis

## When To Use

- Data has a timestamp column and at least one numeric measurement.
- The user asks about anomalies, trends, spikes, drops, degradation or seasonality.
- Monitoring, mining, fintech and IoT scenarios.

## Workflow

1. `data.inspect` - confirm the timestamp column kind and candidate value columns.
2. `timeseries.analyze` - range, statistics, `slope_per_hour`, downsampled series (default 120 points).
3. `timeseries.detect_anomalies` - choose the method deliberately:
   - `zscore` (default): rolling window 24, threshold 3.0. Good for smooth series with local context.
   - `iqr`: global quartiles, 1.5*IQR fences. Good for heavy-tailed series or when no local baseline exists.
4. `charts.create` - feed the `series` from step 2 directly (points accept `t`/`v` keys).
5. `reports.create` - summarize anomalies with timestamps, values and scores, plus method parameters.

## Parameter Guidance

| Situation | Adjustment |
|---|---|
| Baseline drifts over time | Keep `zscore`, reduce `window` (6-12) |
| Sparse data (< 50 points) | Use `iqr`; rolling z-score needs history |
| Many false positives | Raise `threshold` to 3.5-4.0 or increase `window` |
| One sharp step change | `zscore` flags the step edges; explain this in the answer |

## Output Contract

- State method, window, threshold and the number of analysed points.
- For each anomaly: timestamp, value, score. Top 5 in prose, the rest in a table when numerous.
- Trend: report `slope_per_hour` with units of the value column; call it a trend, not a forecast.
- Mention data-quality issues: gaps, duplicate timestamps, non-monotonic ordering (the tool sorts internally).

## Failure Modes

| Symptom | Action |
|---|---|
| `not enough valid (time, value) rows` | Check column choice; report the count instead of retrying |
| Timestamp column not detected | Pass `time_column` explicitly; check the date format |
| All anomalies score ~equal | Likely a constant baseline with a step; switch to `iqr` |

## Anti-Patterns

- Presenting an anomaly as a cause ("sensor failure") without a second source.
- Running both methods and reporting only the more dramatic result; report the chosen method once.
- Skipping the chart when the user asked to "see" the data.
