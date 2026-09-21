# Test Design Techniques

Use the cheapest technique that exposes the risk. Combine at least two for
mandatory criteria.

## Equivalence Partitioning

Group inputs that should behave the same; test one from each group.

| Field | Partitions |
|---|---|
| `limit` | 0 / 1 / typical (5) / max (50) / above max (500) |
| `path` | valid input / missing file / directory / traversal (`../x`) / absolute |
| `where` | valid comparison / unknown column / malformed expression / injection attempt |
| `format` | md / html / unsupported value |

## Boundary Values

Test at, just below and just above each boundary:

- `window`: 0, 1, 24, 100000
- `threshold`: negative, 0, typical (3.0), huge
- row counts: 0 rows, 1 row, header only, very wide table (65+ columns)
- text: empty, 1 char, exactly `max_chars`, `max_chars` + 1, multi-byte (Cyrillic)

## Decision Tables

For combinations that interact (for example routing mode x input type):

| Mode | Input | Expected route |
|---|---|---|
| normal | csv + timestamps | timeseries tools |
| fly | csv + timestamps | recommendation, Codex validates |
| fly | unknown | below-threshold warning, normal fallback |

Every row is a check; conflicting rows reveal a missing rule.

## Negative and Adversarial

- malformed JSON arguments, wrong types, unknown keys;
- path traversal and absolute paths;
- secret-like strings (must be redacted, not echoed);
- prompt-injection text inside documents (`security.review` must flag it);
- concurrent writes to the same artifact path;
- missing API key / unreachable backend -> documented degradation, no crash.

## Exploratory Charters

Format: `Explore <area> with <resources> to discover <risk> in <time-box>`.

Examples:

- "Explore the XLSX loader with multi-sheet and date-formatted workbooks to discover silent type coercion in 20 minutes."
- "Explore the demo dashboard with two browser tabs to discover stale routing state in 15 minutes."
- "Explore the MCP server with malformed lines on stdin to discover parser crashes in 10 minutes."

Record what was varied and what was observed; no finding is still a result.

## Choosing Depth

| Priority (impact x likelihood) | Technique depth |
|---|---|
| 9 | equivalence + boundaries + negative + exploratory |
| 4-6 | equivalence + boundaries + one negative |
| 1-3 | one happy-path check; note the residual risk |

Always test the mandatory scenario end to end, regardless of priority.
