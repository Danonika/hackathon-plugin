# Anomaly Interpretation

How to read `timeseries.detect_anomalies` output and turn it into an honest,
evidence-backed answer.

## What the Score Means

| Method | Score | Reading |
|---|---|---|
| `zscore` | Absolute z-score of the point against its preceding window | 3.0 means three sample standard deviations from the local mean; bigger is rarer under a roughly normal, stable window |
| `zscore` (flat window) | Exactly `threshold` | The window had zero variance and the point differed; the score carries no magnitude information |
| `iqr` | Distance beyond the nearest fence divided by `IQR` | 1.0 means one IQR beyond the fence; it is a robust relative distance, not a probability |

Scores are comparable within one method and one run. Do not compare a z-score
with an IQR score, and do not average scores across methods.

## Worked Reading

`sample-telemetry.csv`, `value`, `zscore` / `window 24` / `threshold 3.0`:

- `anomaly_count: 2` over `count: 72` valid points.
- `47.5 @ 2026-01-02T11:00:00Z`, score `32.12`: an isolated upward spike; the
  surrounding series moves between roughly 8.5 and 11.5.
- `-12.3 @ 2026-01-03T12:00:00Z`, score `23.8`: an isolated downward spike.
- `iqr` returns the same two timestamps with scores `15.02` and `8.13`, which
  raises confidence that both are genuine distribution outliers, not window
  artefacts.

What can be said: two points deviate sharply from the rest of the series on
2026-01-02 11:00 and 2026-01-03 12:00 UTC. What cannot be said: why. Causes
require a second source (maintenance log, deploy record, upstream sensor data).

## Shapes and What They Imply

| Shape | Typical output | Interpretation |
|---|---|---|
| Single spike | One anomaly, very high score | Isolated event; check data entry and upstream systems |
| Two-point spike | Two adjacent anomalies | A transient excursion; window method may also flag the recovery point |
| Step change | Cluster at the step edge; flat-window scores equal to `threshold` | Level shift; report the new level and the change time, not each edge as a separate event |
| Slow drift | No anomalies until a bend | `zscore` tracks drift; a drift is a trend, report `slope_per_hour` |
| Periodic peaks | Anomalies at a regular cadence | Seasonality; compare same-phase points and state the limitation |
| Many scattered anomalies | `anomaly_count` near `limit` | Baseline is unstable or window is too small; adjust once and report the parameter change |

## False-Positive Checklist

- [ ] `window` matches the sampling interval and the expected event duration.
- [ ] The series is not seasonal at a period shorter than `window`.
- [ ] `count` is close to `rows`; dropped rows can distort the window.
- [ ] The flagged value is not a unit change or a known sentinel (for example
      `-1`, `9999`).
- [ ] `iqr` agrees on the timestamps; disagreement usually means a drift or a
      heavy tail.
- [ ] No duplicate timestamps near the anomaly that would double-count a point.

## Reporting Template

```text
Method
- timeseries.detect_anomalies, method zscore, window 24, threshold 3.0, count 72 (2026-01-01T05:00:00Z .. 2026-01-04T04:00:00Z).

Anomalies (2)
| Timestamp (UTC) | Value | Score |
|---|---|---|
| 2026-01-02T11:00:00Z | 47.5 | 32.12 |
| 2026-01-03T12:00:00Z | -12.3 | 23.80 |

Trend
- slope_per_hour -0.04 units/hour over the window (fitted trend, not a forecast).

Data quality
- 72 of 72 rows parsed; no missing values; no warnings.

Interpretation
- Two isolated outliers. No cause can be assigned from this data alone.

Gaps
- No maintenance or deploy log was available; causation not assessed.
```

## Escalation Rules

- Escalate to a second method (`iqr`) when `zscore` scores cluster at exactly
  `threshold` or when the series has heavy tails.
- Escalate to `$data-analysis` when the question is really about another column
  or a group comparison.
- Escalate to a human/source check when the user needs a cause; tools only
  provide the timing and magnitude.
- Never lower `threshold` below 2.0 to manufacture findings; report "no
  anomalies at the chosen parameters" instead.
