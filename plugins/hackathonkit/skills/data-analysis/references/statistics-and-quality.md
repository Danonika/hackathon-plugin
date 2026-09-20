# Statistics and Data Quality

How `data.inspect` and `data.analyze` classify columns and what their numbers do
and do not mean. Use this file before writing any statistics into an answer.

## Profile Semantics

Each `profile[]` entry has `name`, `kind`, `non_empty`, `missing`, optional
`min/max/mean/std` and up to 3 `samples`.

| Field | Meaning |
|---|---|
| `kind: numeric` | Every non-empty value parsed as a number; `min/max/mean/std` are present |
| `kind: timestamp` | At least 80% of non-empty values parsed as dates; no statistics |
| `kind: text` | Anything else, including mixed numeric/text columns |
| `non_empty` | Cells that are not empty after trimming |
| `missing` | Empty cells; `non_empty + missing = rows` |
| `samples` | First 3 non-empty values, useful for spotting units and formats |

`mean` and `std` use parsed numeric values. `std` is the sample standard
deviation (denominator `n-1`). Values are not rounded inside the profile:
`mean: 10.250833333333336` is reported exactly as computed; round only in prose
and say so.

Classification traps:

- A column with one stray `n/a` becomes `text`, even if 99% of cells are
  numbers. Check `samples` and `missing` before trusting a surprising `text`.
- `has_timestamp` in `files.describe` is header-name based (`time`, `date`,
  `timestamp`, `datetime`, `ts_`, and Cyrillic equivalents). A BOM or a custom
  header yields `false` while `data.inspect` reports `kind: timestamp`.
- `data.inspect` profiles every header but warns above 64 columns. Pass
  `columns` to keep the output focused.

## Correlations

`data.analyze` returns `profile` plus up to 5 `correlations`, each
`{columns: [A, B], r, n}`.

- Pearson correlation over complete pairs only: a row contributes only when both
  cells parse as numbers.
- Pairs are dropped when `n < 5` or `abs(r) < 0.3`; the list is sorted by
  `abs(r)` descending and capped at 5.
- `r` is rounded to 2 decimals. `n` is the number of complete pairs, which can
  be smaller than `rows`.

Interpretation rules:

1. Report `r`, `n` and direction (positive/negative). Example wording:
   "`temperature` and `pressure` move together negatively (`r = -0.65`, `n = 72`)".
2. Never write "causes", "drives", "because of" or a prediction from a
   correlation.
3. A missing pair can change `r`; mention `n` when it is below `rows`.
4. Weak or absent correlations are findings too: "no pair reached `abs(r) >= 0.3`
   with `n >= 5`".

## Data-Quality Checklist

Work through this list before reporting aggregates:

- [ ] `missing` per column; flag any column above roughly 20% empty.
- [ ] `kind: text` where numbers were expected (mixed units, `n/a`, thousands
      separators).
- [ ] Suspected duplicates: query a candidate key and compare `matched_rows`
      with distinct expectations; report as a hypothesis unless verified.
- [ ] Suspicious `min`/`max` (negative values for counts, extreme outliers) and
      whether a query can locate the exact rows.
- [ ] All envelope `warnings`, including the >64-column notice.
- [ ] `rows` vs `matched_rows` denominators stated for every share.

## Worked Example

`sample-telemetry.csv` (`data.inspect`; values below rounded to 2 decimals for
readability, the tool returns full precision):

| Column | kind | missing | min | max | mean | std |
|---|---|---|---|---|---|---|
| `ts` | timestamp | 0 | - | - | - | - |
| `sensor_id` | text | 0 | - | - | - | - |
| `value` | numeric | 0 | -12.3 | 47.5 | 10.25 | 5.28 |
| `temperature` | numeric | 0 | 20.2 | 21.8 | 21.11 | 0.56 |
| `pressure` | numeric | 0 | 3.7 | 4.3 | 4.00 | 0.22 |

`data.analyze` on the same file returns one pair:
`{columns: ["temperature","pressure"], r: -0.65, n: 72}`.

Reading: no missing data; the `value` range is widened by two isolated extremes
(-12.3 and 47.5) while `std` (5.28) reflects them; the only association above
threshold is moderate and negative. The extremes are candidates for
`$time-series-analysis`, not evidence of a sensor fault.

## Reporting Template

```text
Facts
- <column> over <rows> rows, <missing> missing: min <min>, max <max>, mean <mean>, std <std> (data.inspect, <path>, sheet <sheet>).
- <matched_rows>/<rows> rows match "<where>" (data.query).
- <A> and <B> correlate at r = <r> over n = <n> (data.analyze).

Data quality
- <missing/mixed-type/duplicate findings, warnings>

Interpretation
- <association wording only; open questions>

Gaps
- <what no tool answered>
```
