# Hackathon Agent Kit MCP Tool Catalogue

All 31 tools return the shared envelope:
`status` (`ok` | `partial` | `error`), `data`, `sources`, `warnings`, `metrics`, `trace_id`.
Read `warnings` before trusting `data`; keep `sources` for citations.

## Discovery Order

`system.context` -> `learning.recall` -> `system.capabilities` -> `system.context_budget` ->
`files.describe` -> domain tools -> `reports.create` -> `learning.record`.

Call the smallest set that finishes the task. Do not list or call tools "just in case".

## System and routing

| Tool | Arguments | Returns |
|---|---|---|
| `system.context` | - | `routing_mode`, `routing_config_version`, live `capabilities` |
| `system.capabilities` | - | capability groups, experimental `fly_router`, tool list |
| `routing.fly_recommend` | `task` (required), `input_metadata`, `available_capabilities` | `route`, ranked `recommendations`, metrics `max_score`, `threshold`, `encoder`, `category_scores` |
| `routing.fly_feedback` | `task` (required), `chosen` (required), `recommended`, `source` (`accepted`/`codex-correction`/`normal-teacher`/`fallback`), `input_metadata` | `stored`, `learned`, `memory_size` |
| `system.context_budget` | - | per-item token estimates for tools, skills, references and manifests, flags, recommendations |

## Learning

| Tool | Arguments | Returns |
|---|---|---|
| `learning.record` | `trigger` (required), `insight` (required), `outcome` (`success`/`failure`/`neutral`), `scope` (`project`/`global`), `evidence[]` | `id`, `confidence`, `observations`, `created` |
| `learning.recall` | `query` (required), `limit` (5), `min_confidence`, `scope` (`project`/`global`/`all`) | ranked `matches[]` with `score` and the full learning |
| `learning.promote` | `id` (required) | `promoted`, `reason`, `learning` |

## Plans

| Tool | Arguments | Returns |
|---|---|---|
| `plan.create` | `slug` (required), `task`, `mandatory_scenario`, `stages[]` | `slug`, `revision`, `path`, `stages`, `summary` |
| `plan.update` | `slug`, `stage`, `status` (all required), `evidence[]`, `goal` | `revision`, `stage`, `status`, `summary` |
| `plan.status` | `slug` (optional, defaults to the latest plan) | plan with `current_stage`, or `partial` when none exists |

## Quality and security

| Tool | Arguments | Returns |
|---|---|---|
| `quality.gate` | - | latest gate evidence (`sha`, `ok`, `summary`, `failed[]`, checks) |
| `security.review` | `text` or `path` or `artifact_id` | `findings[]` (type, severity, line, redacted excerpt), `counts`, `clean` |

## Telegram

| Tool | Arguments | Returns |
|---|---|---|
| `telegram.send` | `text` (required), `chat_id`, `parse_mode`, `silent`, `reply_to` | `message_id`, `chat_id`, `chunks`; partial + hint when unconfigured |
| `telegram.validate` | `chat_id` (optional) | `bot`, `chat`, `recent_chats` for id discovery |
| `telegram.manage` | `action` (`get_updates`/`set_commands`/`set_webhook`/`delete_webhook`) + per-action args | action result; https-only webhooks |

## Evaluation

| Tool | Arguments | Returns |
|---|---|---|
| `eval.aggregate` | `reports[]` or `reports_json[]`, `snapshot_sha`, `kind` (default `judges`), `persist` (default true) | `valid_judges`, `min_valid_judges`, `inconclusive`, unrounded `mean_total` + `mean_total_rounded`, `criterion_means`, `score_variance`, `high_disagreement[]`, `common_deductions[]`, `likely_questions[]`, `dropped_invalid[]`, `dropped_conflict[]`, `aggregate_path` |
| `eval.calibrate` | `run1[]`/`run2[]` or `run1_json[]`/`run2_json[]`, `snapshot_sha`, `min_delta`, `persist` (default true) | `calibration.retest_delta`, `calibration.noise_floor`, `calibration_path` |
| `eval.compare` | `mode` (`technical`/`judge`), `candidate` (required), `best`, `target`, `noise_floor`, `mandatory_scenario_failed`, `persist` (default false) | `decision` (`target_reached`/`accept`/`rollback`/`stop`), `reason`, `improvement` |
| `eval.rank` | `aggregates[]` or `aggregates_json[]`, optional `commission {members, votes, grounds}`, `persist` (default true) | ordered `ranking` (regulation 9.6 tie-breaks, `tie_unresolved`, `commission`), `ranking_path` |
| `eval.freeze_bundle` | `out` (default `data/judge-bundles`), `profile` (`full`/`technical`/`demo`), `exclude[]`, `read_only` (default true), `model` | `bundle_path`, `bundle_hash`, `git_sha`, `profile`, `files`, `excluded[]` |

Evaluation scores stay with the explicit-only judge and project-cost skills; these tools own validation, arithmetic, substitute passes, ranking and stop conditions. Never call them implicitly. A dropped judge report is never zero: run a substitute pass until `min_valid_judges` (default 3) is met.

## Files and documents

| Tool | Arguments | Returns |
|---|---|---|
| `files.describe` | `path` | extension, size, rows, columns, headers, `has_timestamp`, `sheets` for XLSX |
| `documents.ingest` | `path` | `artifact_id`, chars, preview |
| `documents.extract` | `path`, `max_chars` | `text`, chars, truncated |
| `documents.get` | `artifact_id`, `max_chars`, `offset` | stored text slice |

Supported input formats: txt, md, csv, tsv, json, log, yaml, xml, html, docx, xlsx (all worksheets), pdf (text layer only). Scanned PDFs without a text layer return a warning, not text.

## Knowledge

| Tool | Arguments | Returns |
|---|---|---|
| `knowledge.index` | `path` or `artifact_id`, `corpus`, `chunk_chars` (800), `overlap` (100) | `index_id`, `chunk_count` |
| `knowledge.search` | `query`, `limit` (5), `corpus`, `index_id`, `index_ids`, `text_chars` (300) | ranked hits with `source`, `index_id`, `chunk_id`, `score`, `snippet`, `text` |

## Data

| Tool | Arguments | Returns |
|---|---|---|
| `data.inspect` | `path`, `sheet`, `columns` | shape, `profile[]` (`kind`: numeric/timestamp/text, missing, min/max/mean/std) |
| `data.query` | `path`, `sheet`, `columns`, `where` (`column op value`), `limit` (20, max 200) | `matched_rows`, rows |
| `data.analyze` | `path`, `sheet`, `columns` | profile + top correlations (abs(r) >= 0.3, n >= 5) |

`where` operators: `=`, `==`, `!=`, `>`, `>=`, `<`, `<=`, `contains`.

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

## Media (requires `OPENAI_API_KEY`)

| Tool | Arguments | Returns |
|---|---|---|
| `audio.transcribe` | `path`, `language` (e.g. `kk`, `ru`) | transcript text + `artifact_id` |
| `vision.analyze` | `path`, `prompt` | analysis text + `artifact_id` |

## Argument Conventions

- Paths are relative to `data/input`; absolute paths and `..` are rejected by the server.
- `artifact_id` values are relative to `data/artifacts`.
- `eval.*` report paths are relative to the repository root (for example `evals/judges/<sha>/judge-01.json`).
- Numeric arguments may be passed as numbers or numeric strings.

## Citation Map

| Source type | Cite as |
|---|---|
| Extracted document | `artifact_id` + chunk or page |
| Knowledge hit | `artifact_id` (or `index_id` when indexed by path) + `chunk_id` |
| Table row | `path` + row and column |
| Time-series point | timestamp + value column |
| Audio | `artifact_id` + start/end time |
| Image | `artifact_id` + described region |
| Chart or report | `artifact_id` |
