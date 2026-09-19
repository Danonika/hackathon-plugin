---
name: data-analysis
description: Use when the task involves CSV, TSV or XLSX tables, statistics, correlations or table lookups. Covers files.describe, data.inspect, data.query and data.analyze.
---

# Data analysis

1. `files.describe` to confirm shape, headers and timestamp presence.
2. `data.inspect` for column kinds, missing values and summary statistics.
3. `data.query` for filtered subsets (`where` uses `column op value`).
4. `data.analyze` for correlations and numeric summaries.
5. `charts.create` and `reports.create` for deliverables.

Rules:

- Never claim a statistic that no tool returned.
- Report missing values and their share before conclusions.
- Round numbers consistently and cite the source artifact or file path.
