---
name: output-self-check
description: "Use when a completed output must be self-evaluated before handoff: rate accuracy, completeness, clarity, actionability and conciseness 1-5 with evidence, fix sub-30-second gaps and store a scorecard via reports.create. Explicit-only: never invoke implicitly. Do not use for judge scoring ($judge), technical cost ($project-cost) or defect hunting ($roast-project)."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: quality
tools:
  - reports.create
  - learning.record
---

# Output Self-Check

Rate the agent's own finished work on five axes, show the gap behind every point lost, and fix only what costs less than 30 seconds.

## When To Use

- A task's deliverable is complete and the agent is about to hand it to the user or another skill.
- The user asks "rate your work", "self-check", "what is missing?" or "how good is this output?".
- A prior self-check flagged rework and the gap was fixed; re-score the affected axes.
- Do not use for judge scoring (`$judge`), technical readiness cost (`$project-cost`) or adversarial defect finding (`$roast-project`); those are separate explicit-only evaluations.
- Do not use mid-task as a progress gate; it scores a finished output, not a plan.

## Core Principles

1. **Reflection, not a gate.** There is no pass/fail line. The check exists to catch omissions before the user does; it never blocks the handoff.
2. **Show the gap, don't name it.** Every score below 5 carries evidence: the exact missing section, unsupported number, ambiguous sentence or unrequested extra - with a pointer.
3. **Axes stay independent.** A perfect accuracy score never lifts a weak completeness score; score each axis against its own anchors.
4. **Fix small, flag large.** An axis at 3 or below with a fix under 30 seconds is fixed in place; anything larger becomes explicit rework.
5. **Audit, do not re-perform.** Read the original request and the existing envelopes; never rerun the analysis, re-fetch sources or redo the task to score it.
6. **One durable lesson.** Record at most one reusable gap per check with `learning.record`; the scorecard records the rest.

## Workflow

1. **Collect the audit inputs.**
   - The original request verbatim (task text, user message or plan stage goal).
   - The final output exactly as delivered (report, diff, answer, artifact).
   - The verifying evidence: tool envelopes, artifact ids, test or lint output, `status` and `warnings` from the task.
   - If no tool results exist (pure conversation), mark evidence as `unverified` and say so in the scorecard.
2. **Score the five axes independently**, 1-5, against `references/scorecard-rubric.md`: accuracy, completeness, clarity, actionability, conciseness. For every score below 5, write evidence that shows the gap - quote the sentence, name the missing section, cite the warning or the artifact that should have been referenced.
3. **Compute the overall**: the arithmetic mean of the five scores, rounded to one decimal: `overall = round(sum(scores) / 5, 1)`. Never weight axes and never blend scores from different tasks.
4. **Apply the fix rule** to every axis at 3 or below:
   - Fixable in under 30 seconds (delete an unsourced sentence, add a missing warning, label a unit, add one next step): fix it in the delivered output, mark `fixed in place`, and rescore that axis with the new evidence.
   - Not quick-fixable: leave the output untouched and add a rework entry to `top_improvements` with the gap and the expected effort.
   - Never fix a number, quote or claim without re-verifying it against the tool result that produced it.
5. **Write the scorecard artifact**: call `reports.create` with `title: "Self-check - <task>"`, `format: "md"` (default) and `sections` in this order: Overall, one section per axis, Top Improvements. Every section body must be non-empty. Keep the returned `artifact_id`.
6. **Record one durable lesson** when an axis revealed a reusable gap: call `learning.record` with `trigger`, `insight`, `outcome` (`success`/`failure`/`neutral`), `scope: "project"` and `evidence: [artifact_id, gap pointer]`. Skip the call when nothing is reusable; do not record five lessons.
7. **Hand off**: give the user the `overall`, the five scores, the top improvements and the scorecard `artifact_id`. If `reports.create` failed, return the scorecard inline and quote the exact error.

## Decision Matrix

| Signal | Action |
|---|---|
| Axis 5, evidence confirms it | Keep the output; one evidence line in the scorecard |
| Axis 4, minor gap | Score 4, cite the gap; add an improvement only when its impact is high |
| Axis <= 3, fix < 30 seconds | Fix in place, mark `fixed in place`, rescore with the new evidence |
| Axis <= 3, fix > 30 seconds | Do not touch the output; flag explicit rework with expected effort |
| Unsupported claim found (accuracy <= 3) | Fix only if deleting or re-sourcing takes < 30 seconds; otherwise flag it and say the claim must not ship |
| A required part of the request is missing | Score completeness down with the request sentence quoted; flag rework |
| Unrequested extra content in the output | Score conciseness down and name the section; never reward the extra |
| No tool results available | Mark evidence `unverified`, cap accuracy and completeness at 3, state why |
| `reports.create` returns `error` | Keep the drafted sections, return them inline, quote the exact error |
| The same gap recurs across two checks | `learning.record` with the recurrence count; recall it before the next task of the same class |
| The user only wants the number | Still store the artifact, then answer with `overall` and one line per axis |
| User request and output quality trade off | The request wins; record the trade-off in the scorecard instead of lowering the request |

## Output Contract

The scorecard artifact body (and the inline fallback) contains:

```json
{
  "scores": [
    {"axis": "accuracy", "score": 5, "evidence": "all 9 numbers cite data.inspect envelopes; no unsupported claim", "improvement": "none"},
    {"axis": "completeness", "score": 4, "evidence": "request asked for both files; report cites sample-a.csv only", "improvement": "add the sample-b.csv rows"},
    {"axis": "clarity", "score": 5, "evidence": "headline first; units labelled at first use", "improvement": "none"},
    {"axis": "actionability", "score": 4, "evidence": "one next step names the command but not the owner", "improvement": "name the owner"},
    {"axis": "conciseness", "score": 5, "evidence": "all sections serve the request; no repetition", "improvement": "none"}
  ],
  "overall": 4.6,
  "top_improvements": [
    {"rank": 1, "improvement": "add the sample-b.csv row count and its source", "impact": "high", "effort": "rework"}
  ],
  "artifact_id": "reports/self-check-<task>.md"
}
```

- All five axes present, integer scores 1-5, no axis skipped.
- Every score below 5 has evidence that shows the gap with a pointer (section, line, artifact id or warning).
- `overall` equals the mean rounded to one decimal.
- 1-3 `top_improvements`, ranked by impact, each naming the action, the target and an effort (`<30s` or `rework`); entries fixed in place are marked `fixed in place`.
- The `reports.create` `artifact_id`, or the exact error plus the inline scorecard.
- Warnings from every envelope are carried into the handoff, not hidden.

## Worked Examples

**Good.** Request: compare two CSVs and report the row-count difference. Output: a table with both counts, the delta, tool and filter per number, and a limitation naming the skipped sheet.

| Axis | Score | Evidence |
|---|---:|---|
| accuracy | 5 | both counts cite `data.inspect` envelopes; delta matches the arithmetic |
| completeness | 4 | request named two files; `sample-b.csv` date range not stated (Findings misses it) |
| clarity | 5 | table before prose; row counts labelled with units |
| actionability | 4 | "re-run with the same filter" lacks the exact command (Next Steps) |
| conciseness | 5 | no section is longer than its question needs |

Overall 4.6. Top improvement: add the missing date range (rework).

**Weak.** Request: summarize the incident window in `logs/app.log`. Output: a confident narrative with no timestamps, a hidden "412 lines truncated" warning and a deployment-advice section nobody requested.

| Axis | Score | Evidence |
|---|---:|---|
| accuracy | 3 | "root cause was a database timeout" appears in no envelope; the log shows only a restart |
| completeness | 2 | the truncation warning is not disclosed; no timeline of the window |
| clarity | 4 | headline is early, but "it" in paragraph 2 has no referent |
| actionability | 2 | no owner, check or next command |
| conciseness | 3 | deployment advice is out of scope and takes a third of the text |

Fix pass: deleting the unsourced sentence takes under 30 seconds, so it is fixed in place and accuracy becomes 4 with the new evidence; overall is now 3.0. Completeness and actionability are flagged as rework. One lesson is recorded: "log summaries must surface truncation warnings". Full scorecards live in `references/examples.md`.

## Verification

- Confirm every score below 5 has evidence naming the exact gap, not a label such as "slightly incomplete".
- Confirm the `reports.create` envelope has `status != "error"` and a non-empty `artifact_id`; read the `preview` and check the overall and the five axes are visible.
- Confirm `overall` equals `round(mean(scores), 1)` and that no axis score was adjusted to move the overall.
- Confirm `top_improvements` has 1-3 entries ranked by impact, each with a target and an effort, and that every axis <= 3 is either fixed in place or flagged there.
- Confirm at most one `learning.record` call was made for this check and its `evidence` cites the artifact id or the gap pointer.
- Report every warning from the `reports.create` and `learning.record` envelopes instead of hiding it.

## Failure Modes

| Symptom | Action |
|---|---|
| `reports.create` returns `error` | Return the scorecard inline, quote the exact error; still hand over the scores |
| `reports.create` drops sections | Fill empty heading+body pairs and re-create once; artifacts are immutable, never edit them |
| No tool results or artifact ids to cite | Mark evidence `unverified`, cap accuracy and completeness at 3, name the missing verification |
| The original request cannot be recovered | State it; score only what the delivered output claims to cover, and note the limitation |
| A quick fix would change a number or claim | Do not fix silently; flag rework and hand the original output to the user |
| `learning.record` returns `error` | Keep the lesson in the reply with the exact error; the scorecard still ships |
| All axes score 5 | Confirm each 5 has a one-line basis; lower any axis that does not |
| The check finds a critical blocker | Flag it as impact `high` in `top_improvements`; the check is still not a gate |
| The task was never completed | Stop; report the missing deliverable instead of scoring it |

## Anti-Patterns

- Praising mediocre output ("solid work overall") to look competent; name the gap instead.
- Scoring without evidence, or using "seems complete" or "looks good" as the evidence field.
- Re-performing the task during the check: rerunning analyses, re-reading every input or rewriting the answer.
- Penalizing unrequested features; score against the original request, not a wish list.
- Fixing more than the 3-or-below rule allows, or silently editing the delivered output.
- Inflating a score because the work was hard or was produced by the same agent.
- Recording five learnings instead of the one reusable lesson.
- Treating the scorecard as a pass/fail gate and blocking the handoff.
- Skipping the artifact and returning only prose.

## References

- `references/scorecard-rubric.md` - 1-5 anchors for the five axes, evidence format and the overall/fix rules.
- `references/examples.md` - full good and weak scorecards with the fix-in-place walkthrough.
