# Worked Scorecards

Two full self-checks against the rubric in `scorecard-rubric.md`. The scores
illustrate the mapping; always re-derive them from the current output and the
current request.

## Good: Row-Count Comparison

Request: "Compare `sample-a.csv` and `sample-b.csv` and report the row-count
difference."

Delivered: a table with both counts, the delta, tool and filter per number, and
a limitation naming a skipped sheet.

| Axis | Score | Evidence | Improvement |
|---|---:|---|---|
| accuracy | 5 | both counts cite `data.inspect` envelopes; the delta equals `b - a` in the envelope | none |
| completeness | 4 | request named two files; the `sample-b.csv` date range is not stated (Findings) | add both date ranges |
| clarity | 5 | table before prose; row counts labelled with units at first use | none |
| actionability | 4 | "re-run with the same filter" omits the exact command (Next Steps) | name the command |
| conciseness | 5 | no section is longer than its question needs | none |

`overall = (5 + 4 + 5 + 4 + 5) / 5 = 4.6`.

Actions: no axis is at 3 or below, so the output ships unchanged. Top
improvement ranked by impact: add the missing date ranges (rework, low urgency).

## Weak: Incident-Log Summary

Request: "Summarize the incident window in `logs/app.log`."

Delivered: a confident narrative with no timestamps, a "412 lines truncated"
warning that was not disclosed, and a deployment-advice section nobody asked
for.

| Axis | Score | Evidence | Improvement |
|---|---:|---|---|
| accuracy | 3 | "root cause was a database timeout" appears in no envelope; the log shows only a restart at 02:14 | remove or source the sentence |
| completeness | 2 | the "412 lines truncated" warning is absent; no timeline of the window | rebuild the summary with the timeline |
| clarity | 4 | headline is early, but "it" in paragraph 2 has no referent | name the referent |
| actionability | 2 | no owner, check or next command for the incident | add owner and follow-up check |
| conciseness | 3 | deployment advice is out of scope and takes a third of the text | cut the deployment section |

`overall = (3 + 2 + 4 + 2 + 3) / 5 = 2.8`.

Fix pass, axis by axis:

| Axis | Rule applied | Result |
|---|---|---|
| accuracy | deleting the unsourced sentence takes < 30 s -> fix in place | rescored 4; evidence: "the sentence is gone; remaining claims cite the log lines" |
| completeness | rebuilding the timeline takes rework | flagged: "add the incident timeline and the truncation warning" (high impact) |
| actionability | adding owner + check takes rework | flagged: "name the incident owner and the completion check" (medium impact) |
| conciseness | cutting one out-of-scope section takes < 30 s -> fix in place | rescored 5; evidence: "deployment section removed; no padding remains" |
| clarity | one referent fix takes < 30 s -> fix in place | rescored 5; evidence: "`it` replaced with 'the restart event'" |

Post-fix `overall = (4 + 2 + 4 + 2 + 5) / 5 = 3.4`. The artifact ships with the
two rework items as `top_improvements`; the check did not block the handoff.

Recorded lesson (one): trigger "incident log summary", insight "log summaries
must surface truncation warnings and a timestamped timeline", outcome
`failure`, evidence `[artifact_id, "logs/app.log warning: 412 lines truncated"]`.

## Quick Contrasts

| Situation | Score it | Do not |
|---|---|---|
| User asked for a chart; output adds a long causal essay | conciseness 2-3, name the essay | subtract accuracy points |
| Every number is right but one tool warning is hidden | accuracy 3, quote the warning | keep accuracy 5 because the numbers trace |
| Output is short and correct but has no next step | actionability 3, flag it | inflate clarity to compensate |
| Extra features were shipped but never requested | conciseness down, name them | count them as completeness |

## Anti-Pattern Walkthrough

A self-check that returns "overall 4.8, looks great" with `"evidence": "well
written"` fails this rubric twice: the score has no basis, and praising the
output replaces showing the gap. A second failure mode is re-performing the
work - rerunning `data.inspect` to "verify" the row counts instead of reading
the delivered envelope - which duplicates effort and still proves nothing about
the output as shipped.

Ship only after: every below-5 score shows a gap, the artifact exists with
`status != "error"`, and the improvements name a target and an effort.
