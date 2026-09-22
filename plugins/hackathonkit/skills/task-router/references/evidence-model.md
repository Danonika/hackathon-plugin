# Evidence Model

Goal: every claim in the final answer traces to a tool response and its source.
One number, one source. No source, no claim.

## Source Types

| Source | Addressable as | Cite as |
|---|---|---|
| Extracted document text | `artifact_id` | `artifact_id` + chunk or page when available |
| Knowledge chunk | `artifact_id` (or `index_id` for path-indexed text) + `chunk_id` | "found in <artifact_id>, chunk N (index <index_id>)" |
| Table cell / row | `path` + `row`, `column` | "row 17, column `value`" |
| Time-series point | timestamp + value column | "anomaly at 2026-01-01 15:00, sensor value 47.5" |
| Audio segment | `artifact_id` + start/end time | "01:23-01:31 of <artifact_id>" |
| Image region | `artifact_id` + description | "<artifact_id>, region described" |
| Chart / report | `artifact_id` | "chart artifact <id>" |
| Tool envelope | `trace_id` | "trace kit-..., tool `data.inspect`" |

## Rules

1. Numbers, timestamps and quotes must come from a tool response; never reconstruct them from memory.
2. Keep `warnings` visible in the answer: they mark truncation, missing sheets, unsupported formats and API failures.
3. Distinguish fact from interpretation: "the tool detected 3 anomalies" vs "this likely indicates wear".
4. When two sources disagree, show both and say which is primary (raw data over derived summaries; newer authoritative source over older).
5. If no evidence exists for a requested claim, say so explicitly instead of filling the gap.
6. Cite the parameter that produced the number: method, window, threshold, sheet, filter, limit.

## Evidence Ledger

Keep this table while working; it becomes the evidence section of the answer.

| # | Claim | Tool | Parameters | Source | Warning | Confidence |
|---|---|---|---|---|---|---|
| 1 | 3 anomalies found | `timeseries.detect_anomalies` | `zscore`, `window` 24, `threshold` 3.0 | trace kit-..., rows at 14:00/15:00/03:00 | none | high |
| 2 | Sensor 7 drifted 12% | `data.analyze` | all columns | trace kit-..., correlation table | 2 rows missing | medium |

## Worked Example

Request: "How many failed payments in `data/input/payments.csv`?"

1. `files.describe path=data/input/payments.csv` -> 4,210 rows, columns include `status`, `amount`, `ts`.
2. `data.query path=data/input/payments.csv where="status = failed" limit=200` -> `matched_rows: 137`.
3. `reports.create title="Failed payments" sections=[{heading:"Result", body:"137 failed rows (status = failed), from 4,210 total; source data/input/payments.csv."}]` -> `artifact_id`.

Answer: "137 failed payments (4,210 rows total), counted by `data.query` with `where: status = failed`; report `artifact_id`."

Never write "around 140" or "roughly 3%" unless a tool returned that exact figure.

## Warning Handling

| Warning | Meaning | What to do |
|---|---|---|
| `truncated` / chars cap | Extraction or search text was cut | Raise `max_chars`/`text_chars` or read with `documents.get` offset |
| `no text layer` | PDF is scanned | Report the OCR limitation; do not index empty text |
| missing sheet | XLSX sheet name not found | Re-run `files.describe` to list `sheets` |
| API unavailable | Media tool needs `OPENAI_API_KEY` | Report the limitation; offer the local alternative |
| below threshold | Fly confidence is low | Continue in normal mode; do not mention it in the answer |

## Anti-Patterns

- Writing a number first and looking for a source afterwards.
- Citing the report instead of the underlying data for a numeric claim.
- Dropping warnings because the headline result looked correct.
- Treating a `partial` envelope as a complete result without saying what was missing.
- Using `trace_id` as a substitute for a source path; both are needed.
