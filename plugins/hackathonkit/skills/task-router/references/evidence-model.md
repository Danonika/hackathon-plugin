# Evidence Model

Goal: every claim in the final answer can be traced to a tool response and its source.

## Source Types

| Source | Addressable as | Cite as |
|---|---|---|
| Extracted document text | `artifact_id` | `artifact_id` + chunk or page when available |
| Knowledge chunk | `artifact_id` (or `index_id` for path-indexed text) + `chunk:<id>` | "found in <artifact_id>, chunk N (index <index_id>)" |
| Table cell / row | `path` + `row`, `column` | "row 17, column value" |
| Time series point | timestamp + column | "anomaly at 2026-01-01 15:00, sensor value 47.5" |
| Audio segment | start/end time | "01:23-01:31 of <artifact_id>" |
| Image region | artifact + description | "<artifact_id>, region described" |
| Chart / report | `artifact_id` | "chart artifact <id>" |

## Rules

1. Numbers, timestamps and quotes must come from a tool response; never reconstruct them from memory.
2. Keep `warnings` visible in the answer: they mark truncation, missing sheets, unsupported formats, API failures.
3. Distinguish fact from interpretation: "the tool detected 3 anomalies" vs "this likely indicates wear".
4. When two sources disagree, show both and say which is primary (raw data over derived summaries).
5. If no evidence exists for a requested claim, say so explicitly instead of filling gaps.
