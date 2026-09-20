# Scorecard Rubric

Anchors for the five self-check axes. Score against the delivered output and the
original request only; a 5 always needs a one-line basis, and any score below 5
needs evidence that shows the exact gap.

## Accuracy

Does every claim, number and citation trace to a tool result, artifact or source?

| Score | Anchor |
|---:|---|
| 1 | Claims contradict the tool results or are invented; citations point nowhere |
| 2 | Some numbers trace, but key claims have no source; at least one material error |
| 3 | Mostly traceable; one or two unsupported or wrong statements, no critical falsehood |
| 4 | All material claims trace; minor imprecision (unlabelled rounding, stale figure) named |
| 5 | Every claim, number and citation traces to a named source; nothing unsupported |

## Completeness

Does the output answer every part of the request, including warnings and boundaries?

| Score | Anchor |
|---:|---|
| 1 | Most of the request is unanswered; the required output is absent |
| 2 | The main ask is answered, but a required part and all warnings or gaps are missing |
| 3 | Answers the core ask; one requested part, edge case or warning is absent |
| 4 | Covers the ask and warnings; a small sub-question or boundary case is missing |
| 5 | Every part of the request is answered; warnings, gaps and boundaries stated |

## Clarity

Can the reader find the answer in one pass?

| Score | Anchor |
|---:|---|
| 1 | The reader cannot tell what the answer is; no structure |
| 2 | Answer buried; inconsistent terms, units or labels |
| 3 | Understandable with effort; structure exists but the headline is not obvious |
| 4 | Easy to follow; one wording or ordering issue, cited |
| 5 | Headline first; structure, units and labels make one pass enough |

## Actionability

Does the reader know what to do next, when the request implies next steps?

| Score | Anchor |
|---:|---|
| 1 | No next step; the reader cannot act |
| 2 | Vague advice ("review it") that names no target |
| 3 | Some next steps, but no owner, command or completion check |
| 4 | Concrete next steps for the main gap; a secondary action is thin |
| 5 | Each next step names the action, the target and the completion check, or none are needed |

## Conciseness

Is the length proportional to the request, with nothing unrequested?

| Score | Anchor |
|---:|---|
| 1 | Mostly padding, repetition or unrelated content |
| 2 | The core answer is hidden in extra material; large unrequested sections |
| 3 | Longer than needed; repetition or unrequested detail present |
| 4 | Proportional; one redundant section or aside, cited |
| 5 | Every line serves the request; no repetition and no unrequested features |

## Evidence Format

- Show the gap, do not name it: quote the sentence, name the missing section,
  cite the warning or the artifact that should have been referenced.
- Good: `"completeness 4: request asked for both files; report covers sample-a.csv only (Findings, no sample-b.csv reference)"`.
- Bad: `"completeness 4: slightly incomplete"`.
- A 5 still needs a basis: `"accuracy 5: all three claims cite data.inspect envelopes"`.
- Unverified material is labelled `unverified`, never scored as if it were checked.

## Overall

```text
overall = round((accuracy + completeness + clarity + actionability + conciseness) / 5, 1)
```

- Example: `(5 + 4 + 4 + 5 + 3) / 5 = 4.2`.
- Axes are never weighted and scores from different tasks are never blended.
- The overall is descriptive, not a gate: 3.4 does not mean "fail".

## Fix Rule

| Condition | Action |
|---|---|
| Axis <= 3, fix takes < 30 seconds | Fix in place, mark `fixed in place`, rescore with new evidence |
| Axis <= 3, fix takes >= 30 seconds | Leave the output alone; flag rework with expected effort |
| Axis 4 | No fix; an improvement entry only when the impact is high |
| Axis 5 | No action |
| Fix would change a number, quote or claim | Re-verify against the source first; if that is not quick, flag instead of fixing |

Typical sub-30-second fixes: delete an unsourced sentence, add a missing warning,
label a unit, add one next step, fix a broken reference. Typical rework: add a
missing analysis, rebuild a section, re-cite a number that no envelope supports.

## Checklist

- [ ] All five axes scored, integers 1-5.
- [ ] Every score below 5 has gap-showing evidence with a pointer.
- [ ] Overall equals the mean rounded to one decimal.
- [ ] Every axis <= 3 is fixed in place or flagged in `top_improvements`.
- [ ] Warnings from tool envelopes are part of the scorecard.
