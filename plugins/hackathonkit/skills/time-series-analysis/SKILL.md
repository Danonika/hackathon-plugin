---
name: time-series-analysis
description: Use for telemetry, sensor readings, metrics over time, anomaly detection and trend analysis. Covers timeseries.analyze, timeseries.detect_anomalies and chart/report output.
---

# Time series analysis

1. `data.inspect` to find the timestamp and numeric columns.
2. `timeseries.analyze` for range, statistics, trend slope and a downsampled series.
3. `timeseries.detect_anomalies` with `zscore` (default window 24, threshold 3.0) or `iqr`.
4. `charts.create` with the series from step 2 (points accept `t`/`v` keys).
5. `reports.create` to summarize anomalies with timestamps.

Rules:

- Anomaly detection is deterministic: rolling z-score or IQR. Explain detected points, do not invent new ones.
- State the window, threshold and method used in the answer.
- Mention data quality issues (gaps, duplicates, non-monotonic timestamps) if present.
