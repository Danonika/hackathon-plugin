# Method Selection

Choosing between `zscore` and `iqr` in `timeseries.detect_anomalies`, with the
exact mechanics the tool implements.

## Z-Score (default)

For each point `i` (starting from the second point):

1. Take up to `window` preceding points (`i-window` .. `i-1`), default `window: 24`.
2. Skip the point when fewer than 3 preceding points exist.
3. Compute `mean` and sample `std` (denominator `n-1`) over that window.
4. If `std == 0`: flag when the point differs from the window mean, with
   `score = threshold` (default 3.0). This is the constant-baseline rule.
5. Otherwise flag when `|value - mean| / std >= threshold`; `score` is the
   absolute z-score.

Consequences:

- Local baseline: slow drift does not inflate the score, because each point is
  compared with its recent past.
- The first point is never evaluated, and early points use shorter windows.
- A single spike enters the next window and can mask or shift later scores.
- `window` counts points, not time. Confirm the sampling interval with
  `timeseries.analyze` (`start`/`end`/`count`) before interpreting it.

## IQR

1. Sort all values; `Q1` and `Q3` use linear interpolation on the sorted array.
2. `IQR = Q3 - Q1`; fences are `Q1 - 1.5*IQR` and `Q3 + 1.5*IQR`.
3. Flag every point outside the fences; `score = distance to nearest fence / IQR`
   (0 when `IQR == 0`).
4. `window` and `threshold` are ignored.

Consequences:

- Global baseline: robust to heavy tails and works with sparse series, but a
  sustained shift leaves old values inside the fences while new ones flag.
- No local context: a drifting baseline can push ordinary recent values out of
  the fences.
- Constant series (`IQR == 0`) flags any value that is not exactly at the
  constant.

## Comparison

| Situation | Prefer | Why |
|---|---|---|
| Smooth series, local anomalies | `zscore` | Recent baseline; score has a familiar scale |
| Sparse series (< 50 points) | `iqr` | No warm-up; global quartiles |
| Heavy-tailed or skewed values | `iqr` | Quartiles resist extreme values |
| Slow drift plus small anomalies | `zscore`, short `window` | Local baseline tracks the drift |
| Step change in a flat series | `iqr`, or `zscore` with an explanation | `zscore` scores flat-window steps at exactly `threshold` |
| Seasonality with short cycles | Neither cleanly | Compare the same phase across cycles and say the limitation |

## Parameter Scenarios

| Scenario | Call |
|---|---|
| Default triage | `method: "zscore"`, `window: 24`, `threshold: 3.0` |
| Noisy sensor, too many flags | `threshold: 3.5` or `4.0`; keep `window: 24` |
| Expected short excursions in a spiky series | `window: 6` with `threshold: 3.5` or higher; verify against `iqr` |
| Smooth series, local baseline | Keep `window: 24`; a short window flags ordinary variation |
| Long slow shift, want sensitivity | `window: 48`, `threshold: 3.0` |
| Fewer than ~50 points | `method: "iqr"` |
| Truncated output | raise `limit` (default 100, max effective 10000) |

## Worked Calls

`sample-telemetry.csv` (72 hourly points, `value`):

| Call | Result |
|---|---|
| `zscore`, `window: 24`, `threshold: 3.0` | 2 anomalies: 47.5 @ 2026-01-02T11:00:00Z (score 32.12), -12.3 @ 2026-01-03T12:00:00Z (score 23.8) |
| `iqr` | Same 2 points, scores 15.02 and 8.13 |
| `zscore`, `window: 6`, `threshold: 3.0` | 8 anomalies: the two spikes (scores 271.83 and 76.16) plus six ordinary points scored 3.2-3.8; the short window makes smooth variation look anomalous |
| `zscore`, `window: 6`, `threshold: 3.5` | 4 anomalies: the two spikes plus two ordinary points scored 3.8 |

## Method Checklist

- [ ] Both columns confirmed via `data.inspect` (`timestamp` + `numeric`).
- [ ] Method chosen from the table above, not by habit.
- [ ] `window` interpreted in points after checking the sampling interval.
- [ ] `anomaly_count < limit` before concluding.
- [ ] Method, window, threshold and `count` reported with the findings.
