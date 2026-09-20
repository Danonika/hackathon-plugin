# Report Templates

Section patterns for the most common Hackathon Agent Kit deliverables. Fill the skeletons;
do not add sections the reader does not need.

Section rules: Executive Summary, Inputs and Scope, Method, Findings and Data
Quality/Limitations are always present; Next Steps only when implied; the
Evidence Appendix appears when the report carries more than about five numbers.

## Template 1: Analysis Report

```markdown
# <Question as a title>

## Executive Summary
<The answer in 3-6 sentences. Include the headline number and its scope.>

## Inputs and Scope
- <file> (<rows> rows, <period>) - <artifact_id if any>; not examined: <exclusions>

## Method
- `<tool>` with `<parameters>` over `<n>` records.

## Findings
1. <Finding sentence> (`<tool>`, <path>, <n>).
2. <Finding sentence> (`<tool>`, <filter>, `matched_rows` <k> of <rows>).

## Data Quality and Limitations
- <missing counts, dropped rows, truncation, warnings>

## Next Steps
- <Only if the task implies them.>
```

## Template 2: Comparison Report

```markdown
# <A> vs <B>

## Executive Summary
<What differs, by how much, and what is the same.>

## Inputs and Scope
- <A>: <file/artifact, period>
- <B>: <file/artifact, period>

## Method
- `<tool>` with `<parameters>`; fields aligned on <key>.

## Differences
| Field | <A> | <B> | Delta | Source |
|---|---|---|---|---|

## Gaps
- <Field present in only one side.>

## Matches
- <Fields identical after normalisation; state the normalisation.>

## Data Quality and Limitations
- <Unavailable fields, differing periods, warnings>
```

## Template 3: Anomaly / Incident Report

```markdown
# <What was observed> in <system> (<period>)

## Executive Summary
<How many anomalies, when, how large, and what is not known.>

## Inputs and Scope
- <file> (<count> valid points, <start>..<end>)

## Method
- `timeseries.detect_anomalies`, method `<zscore|iqr>`, window `<w>`, threshold `<t>`, limit `<l>`, count `<n>`.

## Findings
| Timestamp (UTC) | Value | Score | Source |
|---|---|---|---|

## Trend
- `slope_per_hour` <value> <units>/hour over the observed window (fitted trend, not a forecast).

## Data Quality and Limitations
- <dropped rows, gaps, duplicates, warnings; no cause assigned>

## Next Steps
- <Check upstream logs/maintenance records; re-run with parameters.>
```

## Template 4: Research Summary

```markdown
# <Topic>

## Executive Summary
<The takeaway and confidence level.>

## Scope and Questions
- Q1: <question>
- Q2: <question>

## Evidence
| Claim | Source | Date | Confidence |
|---|---|---|---|

## Counter-evidence and Open Questions
- <Contradictions found, unanswered questions; note sources and coverage limits>
```

## Template 5: Hackathon Demo Report

```markdown
# <Project> - <task>

## Executive Summary
<What was built/answered, in two sentences a judge can repeat.>

## What Was Done
- <Step -> tool -> result -> artifact id>

## Demo Script
1. <Command/action and the expected visible result>
2. <...>

## Limitations and Next Steps
- <Known gaps, timeboxed follow-ups>
```

## Formatting Rules

- `format: "md"` for text deliverables; `format: "html"` only when a
  browser-ready file is needed. HTML escapes the body and wraps blank-line
  paragraphs; raw HTML tags inside a body are not interpreted.
- Markdown tables are safe in `md`; in `html` they render as plain text, so
  convert small tables to sentences or keep the `md` format.
- Every `body` is plain text or markdown; headings belong in `heading`; one
  message per section, split instead of nesting.

## Choosing a Template

| Request | Template |
|---|---|
| "What does the data say?" | 1 Analysis |
| "Compare A and B" | 2 Comparison |
| "What happened at 11:00?" | 3 Anomaly/Incident |
| "Summarise what you found about X" | 4 Research |
| "Make a demo write-up" | 5 Hackathon Demo |
| Mixed request | Combine: summary from 1, comparison table from 2 |
