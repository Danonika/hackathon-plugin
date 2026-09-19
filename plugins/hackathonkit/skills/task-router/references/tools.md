# Hackathon Agent Kit MCP Tool Catalogue

Every tool returns the shared envelope:
`status` (`ok` | `partial` | `error`), `data`, `sources`, `warnings`, `metrics`, `trace_id`.
Read `warnings` before trusting `data`; keep `sources` for citations.

## System

| Tool | Arguments | Returns |
|---|---|---|
| `system.context` | - | `routing_mode`, `routing_config_version`, `capabilities` |
| `system.capabilities` | - | capability groups, experimental flags, tool list |
| `routing.fly_recommend` | `task` (required), `input_metadata`, `available_capabilities` | `route`, `recommendations`, `metrics.max_score`, `metrics.threshold`, `metrics.encoder` |

## Files and documents

| Tool | Arguments | Returns |
|---|---|---|
| `files.describe` | `path` | extension, size, rows, columns, headers, `has_timestamp`, `sheets` for XLSX |
| `documents.ingest` | `path` | `artifact_id`, chars, preview |
| `documents.extract` | `path`, `max_chars` | `text`, chars, truncated |
| `documents.get` | `artifact_id`, `max_chars`, `offset` | stored text slice |

Supported input formats: txt, md, csv, tsv, json, log, yaml, xml, html, docx, xlsx (all worksheets), pdf (text layer only).

## Knowledge

| Tool | Arguments | Returns |
|---|---|---|
| `knowledge.index` | `path` or `artifact_id`, `corpus`, `chunk_chars` (800), `overlap` (100) | `index_id`, `chunk_count` |
| `knowledge.search` | `query`, `limit` (5), `corpus`, `index_id`, `index_ids`, `text_chars` (300) | ranked hits with `source`, `index_id`, `chunk_id`, `score`, `snippet`, `text` |

## Data

| Tool | Arguments | Returns |
|---|---|---|
| `data.inspect` | `path`, `sheet`, `columns` | shape, `profile[]` (`kind`: numeric/timestamp/text, missing, min/max/mean/std) |
| `data.query` | `path`, `sheet`, `columns`, `where` (`column op value`), `limit` (20) | `matched_rows`, rows |
| `data.analyze` | `path`, `sheet`, `columns` | profile + top correlations (abs(r) >= 0.3, n >= 5) |

## Time series

| Tool | Arguments | Returns |
|---|---|---|
| `timeseries.analyze` | `path`, `sheet`, `time_column`, `value_column`, `max_points` (120) | range, min/max/mean/std, `slope_per_hour`, downsampled `series` |
| `timeseries.detect_anomalies` | `path`, `sheet`, `time_column`, `value_column`, `method` (`zscore`/`iqr`), `window` (24), `threshold` (3.0), `limit` (100) | `anomalies[]` with timestamp, value, score, method |

Time column accepts RFC3339, `YYYY-MM-DD[ HH:MM[:SS]]`, `DD.MM.YYYY`, epoch seconds/milliseconds.

## Classification, charts, reports

| Tool | Arguments | Returns |
|---|---|---|
| `classification.classify` | `text`, `categories` | `label`, `scores`, `matched_keywords` |
| `charts.create` | `title`, `chart_type` (`line`/`bar`), `series:[{label, points:[{x,y} or {t,v}]}]` | SVG `artifact_id` |
| `reports.create` | `title`, `format` (`md`/`html`), `sections:[{heading, body}]` | report `artifact_id`, preview |

## Media (requires OPENAI_API_KEY)

| Tool | Arguments | Returns |
|---|---|---|
| `audio.transcribe` | `path`, `language` (e.g. `kk`, `ru`) | transcript text + `artifact_id` |
| `vision.analyze` | `path`, `prompt` | analysis text + `artifact_id` |

## Argument Conventions

- Paths are relative to `data/input`; absolute paths and `..` are rejected.
- `artifact_id` values are relative to `data/artifacts`.
- Numeric arguments may be passed as numbers or numeric strings.
