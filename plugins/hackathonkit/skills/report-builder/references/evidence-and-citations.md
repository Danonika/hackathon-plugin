# Evidence and Citations

How to keep every number in a report traceable and how to present provenance
without turning the report into a log file.

## One Number, One Source

Every figure in a report must satisfy:

1. A tool returned it (envelope `data`), or it is explicitly labelled as derived
   arithmetic with the operands shown.
2. The parameters that produced it are known (`path`, `sheet`, `where`, `method`,
   `window`, `threshold`, `n`).
3. It can be re-checked by re-running the same call.

If a number fails any of these, remove it or mark it "unverified" in
Limitations.

## Source Map

Keep a working map while drafting; include it as the Evidence Appendix when the
report has more than a handful of numbers.

| Claim | Tool | Parameters | Result field | Artifact |
|---|---|---|---|---|
| `<statement>` | `data.inspect` | `path`, `sheet` | `profile[].mean` | - |
| `<statement>` | `data.query` | `where`, `limit` | `matched_rows`, `rows` | - |
| `<statement>` | `timeseries.detect_anomalies` | `method`, `window`, `threshold` | `anomalies[]` | - |
| `<statement>` | `charts.create` | `series`, `chart_type` | `artifact_id` | `charts/...svg` |
| `<statement>` | `documents.extract` | `max_chars` | `text`, `truncated` | - |
| `<statement>` | `reports.create` | `title`, `format` | `artifact_id` | `reports/...md` |

## Citation Syntax

- Inline, compact: `(data.inspect, sample-telemetry.csv)`.
- With parameters when they matter: `(data.query, value > 20, matched_rows 1 of 72)`.
- Anomalies: `(timeseries.detect_anomalies, zscore, window 24, threshold 3.0)`.
- Documents: `(documents.get, extracted/report-...txt, offset 1200, chars 400)`.
- Indexed passages: `(knowledge.search, index_id idx-..., chunk_id 7, score 0.81)`.
- Artifacts: cite the `artifact_id` as the evidence handle, for example
  `charts/value-spikes-20260101-120000.svg`.

Allowed in prose: tool names, file names relative to `data/input`, artifact ids.
Not allowed in prose: absolute paths, raw JSON blobs, trace ids, internal
identifiers that mean nothing to a reader. A `trace_id` may appear in the
Evidence Appendix for debugging.

## Warnings Are Findings

- Quote envelope `warnings` verbatim in Data Quality and Limitations.
- Never paraphrase a warning into something weaker ("minor issue").
- A `partial` or `error` status is a finding about coverage; include the exact
  message and what was still usable.
- If a warning invalidates a finding (for example `truncated: true` on the only
  source), say the finding is provisional or drop it.

## Conflicting Results

When two tools or runs disagree:

1. List both results with their parameters.
2. Identify the difference in inputs (filter, period, method, `n`).
3. State which one answers the user's question and why.
4. Never average conflicting numbers to force a single answer.

## Pre-Publish Checklist

- [ ] Executive summary stands alone and matches the Findings section.
- [ ] Every number traces to an envelope; no invented or remembered values.
- [ ] Units, periods and `n` stated on first use.
- [ ] Rounding labelled ("about", "rounded to 2 decimals").
- [ ] All warnings, dropped rows and failed calls in Limitations.
- [ ] Differences and gaps are not blended into one figure.
- [ ] Artifact ids listed for charts, extracted text and prior reports.
- [ ] `reports.create` returned `status: "ok"` with an `artifact_id`, and the
      `preview` shows the intended title and summary.
- [ ] `sections` count matches the non-empty sections sent.
- [ ] No absolute paths, raw JSON, tool tokens or trace ids in prose.

## Worked Provenance Example

Finding: "`value` peaked at 47.5 on 2026-01-02T11:00:00Z, 32.1 standard
deviations above its rolling baseline."

Evidence chain:

- Peak value and time: `timeseries.detect_anomalies`, `zscore`, `window 24`,
  `threshold 3.0`, `anomalies[0]` -> `{value: 47.5, score: 32.12, timestamp:
  2026-01-02T11:00:00Z}`.
- Range context: `timeseries.analyze` -> `min -12.3`, `max 47.5`, `count 72`.
- Missing data: `data.inspect` -> `missing: 0` for `value`.

Reported inline as: "one point reached 47.5 at 2026-01-02T11:00:00Z
(timeseries.detect_anomalies, zscore, window 24, threshold 3.0, n 72); the series
has no missing values (data.inspect)."
