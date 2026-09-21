# Analytics Playbook

Business and product analytics on top of the deterministic tools: define the
metric first, then compute it with `data.*`, then interpret with an explicit
uncertainty statement. This is the analytic-expert layer inside `$data-analysis`
(tables) and `$time-series-analysis` (time-indexed metrics).

## 1. Metric Definition Discipline

Every metric answers: who, what, window, unit, direction.

| Field | Example | Why it matters |
|---|---|---|
| Population | active customers (>= 1 order in 30 days) | the same number means different things per population |
| Numerator | orders with a successful payment | the event that counts |
| Denominator | all checkout attempts | base rate; never mix with a different base |
| Window | last 28 days | comparisons need equal windows |
| Unit | orders per customer per week | avoids "total" hiding growth or decline |
| Direction | higher is better | catches accidental inversions |

Rules:

- One metric, one definition; write it next to the number in the report.
- Never average a ratio; compute numerator and denominator first, then divide.
- Currency and time zones are stated once and used consistently.

## 2. KPI Tree

Link the top metric to the drivers a team can change:

```text
Revenue = active customers x orders per customer x average order value
Growth  = acquisition + activation + retention - churn
```

Map each leaf to a column or query in the dataset. A leaf with no column is a
measurement gap, not an estimate opportunity.

## 3. Funnel Analysis

Build the funnel as counts per step over the same population and window:

```text
data.query(path, where="step = 'checkout_started'")   -> count
data.query(path, where="step = 'payment_attempted'")  -> count
data.query(path, where="step = 'payment_succeeded'")  -> count
```

- Report each step count and the step-to-step conversion; do not report only the end-to-end rate.
- State where users are lost (largest absolute drop), then the relative drop.
- If a step is missing from the data, say so; never interpolate a step.

## 4. Cohorts and Retention

1. Assign each user a cohort by first activity (`data.query` with the first date per user).
2. Count activity per cohort per period (`data.query`, then pivot in the report).
3. Retention at period N = active users of the cohort in period N / cohort size.
4. Report cohort sizes; small cohorts (< 30) get a wide-uncertainty label.

`data.analyze` correlations can suggest which behaviors track with retention, but
association is not causation; label it.

## 5. Experiment Readouts

- State the hypothesis, the unit of randomization, the primary metric and the guardrail metric before looking at results.
- Compare equal windows and the same population; report absolute and relative lift.
- Report sample sizes per arm and the raw counts behind each rate.
- Do not call a winner when the difference is inside the noise; say "inconclusive with this sample".
- No p-values from these tools: they compute descriptive statistics only. State that limitation explicitly.

## 6. Unit Economics

```text
Contribution margin = average order value - variable cost per order
Payback (months)    = customer acquisition cost / monthly contribution per customer
```

- Costs come from the dataset, never from memory; missing cost columns are a gap.
- Report per-cohort where acquisition cost differs.
- Label any assumption (for example an average cost) as an assumption.

## 7. Metric Traps

| Trap | Symptom | Fix |
|---|---|---|
| Simpson's paradox | aggregate trend flips inside every segment | report per-segment before the aggregate |
| Survivorship bias | only active users in the dataset | state the population and who is missing |
| Denominator drift | rate rises while counts fall | show numerator and denominator |
| Mixed windows | 7-day vs 28-day rates compared | equalize windows |
| Double counting | joins multiply rows | count distinct ids, verify totals |
| Averages of ratios | unweighted mean of percentages | weight by the denominator |
| Time-zone boundaries | daily totals shift | fix one time zone and state it |

## 8. Report Wording

- Number, definition, window, source; then interpretation on a separate line.
- "Correlation" for association; "causes" only with an experiment.
- Effective `n` before any percentage; wide uncertainty for small samples.
- Gaps are stated as gaps, with the query that would close them.
