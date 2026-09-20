# Query Cookbook

Exact behaviour of `data.query` and `files.describe` / `data.inspect` inputs, with
worked filters. All examples use paths relative to `data/input`.

## Argument Surface

| Argument | Required | Default | Notes |
|---|---|---|---|
| `path` | yes | - | Relative to `data/input`; absolute paths and `..` are rejected |
| `sheet` | no | first sheet | XLSX only; read the list from `files.describe.sheets` |
| `columns` | no | all columns | Projection only; does not change `matched_rows` |
| `where` | no | no filter | `"column op value"`, at least three whitespace-separated fields |
| `limit` | no | 20 | `<= 0` or `> 200` falls back to 20 |

Response fields: `matched_rows` (all rows passing the filter), `returned` (rows in
the response after `limit`), `rows` (list of objects keyed by trimmed header).

## Operator Semantics

| Op | Matching rule |
|---|---|
| `=`, `==` | If both cell and value parse as numbers, compare numerically; otherwise case-insensitive trimmed text equality |
| `!=` | Numeric inequality when both parse; otherwise negated case-insensitive equality |
| `>` `>=` `<` `<=` | Numeric only. If either side does not parse, the row does not match |
| `contains` | Case-insensitive substring of the cell |

Numeric parsing accepts `12`, `-3.5`, `1e3` and comma decimals such as `3,14`
(the parser replaces `,` with `.`). A lone `-` is not a number.

Column lookup is case-insensitive and falls back to substring matching, so
`where: "sensor = x"` may bind to a header named `sensor_id`. Prefer full names.

## Worked Filters

Against `sample-telemetry.csv` (72 rows, headers `ts, sensor_id, value,
temperature, pressure`):

| Call | Result |
|---|---|
| `where: "value > 20"`, `limit: 5` | `matched_rows: 1`, `returned: 1`, row `{ts: "2026-01-02 11:00:00", value: "47.5"}` |
| `where: "sensor_id contains SENSOR-01"` | All 72 rows (case-insensitive) |
| `where: "value <= -5"` | 1 row, the -12.3 low extreme |
| `where: "pressure >= 4.3"`, `limit: 200` | Rows at the pressure ceiling; `matched_rows` is the true count |
| `columns: ["ts","value"]`, no `where` | First 20 rows only; `matched_rows: 72`, `returned: 20` |

## Pagination Pattern

`data.query` has no offset argument. To inspect beyond the first page:

1. Tighten `where` (for example by time window or numeric range).
2. Raise `limit` up to 200.
3. If `matched_rows > 200`, report the count and the filter, and describe the
   population instead of enumerating it.

Never present the first 20 rows as "the data" when `matched_rows` is larger.

## Sheet Selection

`files.describe` on XLSX returns `sheets` (all worksheet names) and `first_sheet`.
`data.query` / `data.inspect` / `data.analyze` default to the first sheet. If the
target sheet is not first, pass `sheet` on every call; a mismatch produces
plausible but wrong numbers rather than an error when both sheets exist.

## Error Catalogue

| Message | Cause | Fix |
|---|---|---|
| `empty table` | No header row or no data rows | Report the file as empty |
| `unknown column in where: X` | Header mismatch | Re-read `headers`; use the exact spelling |
| `where must be "column op value", got "..."` | Fewer than three fields | Add the operator and value |
| `unsupported operator ">>"` | Typo or unknown op | Use a documented op |
| `input file not found: X` | Wrong root or typo | Paths are relative to `data/input` |
| `legacy .xls (BIFF) ... not supported` | Old Excel format | Convert to `.xlsx` or `.csv` |

## Evidence Checklist

- Quote `where` exactly as sent, plus `matched_rows` and `returned`.
- Quote `rows` values as strings; do not re-parse and round them.
- State the sheet when the workbook has more than one.
- Carry any `warning` from the envelope into the answer.
