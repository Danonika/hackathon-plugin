# Procedure Patterns

Reusable answer shapes for common government-service questions. Fill every
field only from retrieved chunks; a field without a hit stays as the literal
string `NOT FOUND IN CORPUS`, never as a guess.

## Pattern A - Document List

Use when the question is "what do I need".

| Column | Rule |
|---|---|
| Document name | Official name from the act, not a colloquial name |
| Form | Status of copies (original/copy/notarized) exactly as written |
| Issued by | Body name from the act, full official form |
| Validity | Period, or `unlimited` if the act says so |
| Source | Citation to the requirement chunk |

Deliver as a numbered list plus one table when more than four documents. If the
act defines an exhaustive list, say "the act lists N documents"; do not add
"commonly required" extras.

## Pattern B - Step Sequence

Use when the question is "how do I do it". Each step needs an actor, an action
and a time limit:

```text
1. Applicant submits form T-3 via the portal (art_9f12, chunk:6).
2. Licensing body registers the application within 1 working day (chunk:7).
3. Applicant pays the fee within 10 calendar days (chunk:7).
4. Licensing body issues the decision within 5 working days (chunk:8).
```

- Keep the state machine explicit: who acts, what triggers the next step.
- Preserve working days vs calendar days exactly.
- If the corpus describes an electronic and a paper path, give both and mark
  which is default.

## Pattern C - Fee and Deadline Table

| Field | Value (verbatim) | Source |
|---|---|---|
| Fee | 5 MCI | art_9f12, chunk:7 |
| Payment window | 10 calendar days from registration | art_9f12, chunk:7 |
| Decision deadline | 5 working days | art_9f12, chunk:8 |
| Validity | 3 years | art_9f12, chunk:9 |

- Copy units as written (tenge, MCI, percent). Never convert MCI to tenge
  unless the corpus states the conversion and the year.
- When a value changed across amendments, show the current one first and the
  previous one in a footnote with its validity dates.

## Pattern D - Multi-Agency Flow

Use when more than one body appears:

```text
Stage 1 - Local executive body: accepts and registers the application.
Stage 2 - Sanitary service: issues the opinion (parallel, 3 working days).
Stage 3 - Licensing committee: issues the permit (5 working days after
          stage 2 completes).
```

- Name dependencies explicitly (parallel vs sequential).
- Citation per stage, not per paragraph.
- State the single point of contact the citizen uses, if the corpus names one.

## Pattern E - Grounds for Refusal or Suspension

- Number the grounds exactly as the act does; do not merge similar grounds.
- Keep appeal routes separate from the grounds list, with the appeal deadline
  and the body that hears it.
- Mark silent gaps: if appeal deadlines are missing, say so.

## Report Skeleton

Use this skeleton with `reports.create` (see SKILL.md step 7):

```markdown
## Question 1: <restated question>
**Answer.** <two to four sentences with citations>
**Requirements.** <numbered list>
**Steps.** <numbered list>
**Fees and deadlines.** <verbatim values with citations>
**Sources.** <artifact/chunk list>
**Gaps.** <uncovered files, unanswered points, UNRESOLVED items>
```

Repeat per question, then add the coverage statement. Never merge two
questions into one section when their requirement lists differ.

## Quality Checklist

- [ ] Field-level values copied verbatim, units intact.
- [ ] Working days and calendar days never interchanged.
- [ ] Parallel and sequential stages labeled.
- [ ] Refusal grounds numbered as in the act.
- [ ] No pattern field invented to make the table look complete.
- [ ] `NOT FOUND IN CORPUS` used wherever a hit is missing.
