---
name: learning-loop
description: "Use when experience from a task should become durable: recall learnings before repeating a class of task, record a trigger/insight pair after an outcome, rely on automatic dedupe, and promote project learnings to global after 3+ observations at high confidence. Reads the current plan stage with plan.status. Do not use for one-off trivia, secret values or plan bookkeeping; use $build-planner to plan and plan.update to change stages."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: core
tools:
  - learning.record
  - learning.recall
  - learning.promote
  - plan.status
---

# Learning Loop

Turn completed work into durable, confidence-scored learnings and recall them before repeating a class of task.

## When To Use

- Before starting a task class that may have been attempted before: same toolchain, data shape, failure or deliverable.
- After an outcome - success, failure or neutral - that produced a reusable trigger/insight pair.
- When a project learning has 3+ observations and high confidence and should travel to other projects.
- When resuming a session and the plan stage or prior lessons must be recovered before work continues.
- Do not use for one-off facts, timestamps, secrets, credentials or file dumps; learnings are small reusable rules.
- Do not use for planning or stage bookkeeping; use `$build-planner` and `plan.update` (this skill only reads `plan.status`).

## Core Principles

1. **Recall before repeating.** A class of task starts with `learning.recall`; apply the matches or state why they do not apply.
2. **Trigger is the condition, insight is the action.** If either cannot be written in one sentence, it is not a learning yet.
3. **One behavior per learning.** A pair that bundles several rules can never be confirmed or contradicted cleanly.
4. **Evidence or it did not happen.** `evidence` carries trace ids, artifact ids, `file:line` or test output; never a secret or a raw dump.
5. **Dedupe is automatic.** Re-recording the same trigger+insight reinforces confidence; never re-word to force a duplicate.
6. **Promotion is earned.** Project to global only after 3+ observations and confidence at or above the injection threshold; never promote one-off advice.
7. **Confidence is computed, never asserted.** `0.3 + 0.6 * successes / (successes + failures + 1)`, clamped to 0.3-0.9.
8. **The store is local.** Learnings never leave the repository; recall is local and embeddings are optional.
9. **Session handoff is part of the loop.** Record before the session ends; a lesson that only lives in the chat is lost.

## Workflow

1. **Recall before the task.** Call `learning.recall` with `query` = what you are about to do in natural language, `limit` 5, optional `min_confidence` (default from `configs/config.yaml`, 0.5) and `scope: "project"` (default; includes global). Read `matches[]` (`score`, `confidence`, `observations`, `trigger`, `insight`) and apply or dismiss each match explicitly.
2. **Read the stage context.** Call `plan.status` with no slug (latest plan) or `slug=<plan>`. Keep `current_stage.id` and `mandatory_scenario`; a `partial` envelope with `plan: null` means there is no plan - continue without stage scoping.
3. **Do the task.** Keep evidence as you go: `trace_id`, `artifact_id`, `file:line`, test output.
4. **Write the pair.** After the outcome, call `learning.record` with:
   - `trigger`: "When <specific condition> ..." - the situation that should re-trigger the memory.
   - `insight`: "Do <one behavior> ..." - one action or avoidance, not a paragraph.
   - `outcome`: `success` / `failure` / `neutral` - what actually happened.
   - `scope`: `project` (default) unless this is a proven cross-project rule.
   - `evidence`: trace ids, artifact ids, paths or test output; no secrets, no raw dumps.
   Read `id`, `confidence`, `observations`, `created`. `created: false` means the pair was reinforced, not duplicated.
5. **Check the confidence math.** Successes raise confidence, failures lower it relative to successes, and the value never exceeds 0.9; use the table in `references/learning-quality.md` instead of asserting a number.
6. **Promote when earned.** Call `learning.promote` with `id` only when `observations >= 3` (config `promote_after`) and `confidence >= 0.7` (`inject_min_confidence`). Read `promoted`, `reason` and `learning`; `not eligible yet` is normal - do not retry in a loop.
7. **Close the loop.** In the final answer list recalled learnings that applied, learnings recorded (with ids) and promotions. If `learning.record` failed, keep the pair in the answer with the exact error.

## Tool Call Reference

| Tool | Required | Optional (default) | Key fields |
|---|---|---|---|
| `learning.recall` | `query` | `limit` (5, max 50), `min_confidence` (0.5), `scope` (`project`) | `matches[]` with `score`, `confidence`, `observations`, `trigger`, `insight`; `count` |
| `learning.record` | `trigger`, `insight` | `outcome` (`success`), `scope` (`project`), `evidence[]` (max 10 kept) | `id`, `confidence`, `observations`, `scope`, `created` |
| `learning.promote` | `id` | - | `promoted`, `reason`, `learning` |
| `plan.status` | - | `slug` (latest) | `plan.current_stage`, `mandatory_scenario`, `revision`; `partial` with `plan: null` when none exists |

## Worked Example

Request: "The last CSV export had a UTF-8 BOM and broke the header match. Handle it this time."

1. `learning.recall` with `query: "load CSV with BOM header matching"` -> one match `l-1a2b3c4d`, `trigger: "When a CSV header contains a UTF-8 BOM"`, `insight: "Strip the BOM before matching headers"`, `confidence: 0.7`, `observations: 2`.
2. `plan.status` -> `current_stage.id: "S2"` and `mandatory_scenario` present.
3. Do the task with the BOM stripped; keep the `trace_id` and the test output.
4. `learning.record` with the same trigger and insight, `outcome: "success"`, `scope: "project"`, `evidence: ["trace id", "tests/ingest_test.go:42"]` -> `created: false`, `observations: 3`, `confidence: 0.75`. Reinforcement, not a duplicate.
5. `learning.promote` with `id: "l-1a2b3c4d"` -> `promoted: true`, because observations 3 >= 3 and confidence 0.75 >= 0.7.
6. Report the applied learning, the reinforced pair and the promotion.

## Decision Matrix

| Signal | Action |
|---|---|
| `learning.recall` returns matches | Apply the applicable ones; state which and why |
| `learning.recall` returns `partial` with `count: 0` | No memory yet; continue and record after the task |
| `learning.record` returns `created: false` | Reinforcement: read the new `confidence` and `observations`; do not force a duplicate |
| `outcome: failure` on a rule that worked before | Record the failure; never delete the learning; confidence reflects both counts |
| `observations >= 3` and `confidence >= 0.7` | Candidate for `learning.promote`; call it once |
| `learning.promote` returns `not eligible yet` | Keep project scope; do not retry now |
| Trigger is generic ("when coding", "when debugging") | Rewrite to the concrete condition or do not record |
| Insight names two or more actions | Split into separate learnings or keep the single most important behavior |
| The "lesson" is a one-off fact, timestamp or value | Do not record; put it in the task output instead |
| Evidence would contain a secret or raw dump | Drop the evidence, record the insight without it, or skip the learning |
| A recalled learning conflicts with new evidence | Record the new outcome, report the conflict, never silently overwrite |
| `plan.status` returns `partial` with no plan | Continue; note that learnings are not stage-scoped |
| Session ends mid-task | Record the durable pair before handoff; `hackathonkit-ctl sessions resume` carries the rest |
| A learning's evidence is a private document | Cite the artifact id, not the content; the store is local but the excerpt may still be sensitive |
| The same trigger has two contradictory insights | Record the outcome that just happened; report both and let confidence rank them |
| Recording a failure feels discouraging | Failures are the most valuable records; record them with the same care as successes |
| The task was exploratory and produced no reusable rule | Record nothing; state that explicitly in the answer |
| A recalled learning is stale but not wrong | Record a neutral outcome with the current evidence; do not delete it |

## Output Contract

- Recall: the `query` used, matched `id`s with `score` and `confidence`, and apply/dismiss per match.
- Record: `id`, `created`, `confidence`, `observations`, `scope`, and the exact `trigger`/`insight` text.
- Promote: `promoted`, `reason`, and the learning's new `scope`.
- Evidence per learning: trace ids, artifact ids, `file:line` or test output; no secrets.
- Failures: exact envelope `status`, `warnings` and error text, with the pair preserved in prose.
- Explicit note when nothing was worth recording, with the reason.

## Verification

- Confirm every `learning.record` claim matches its envelope (`id`, `created`, `confidence`, `observations`).
- Confirm no learning text or evidence contains a secret, credential or raw file dump.
- Confirm each recorded pair has a concrete trigger and exactly one behavior.
- Confirm promotions happened only at `observations >= 3` and `confidence >= 0.7`, quoting the envelope.
- Confirm recall ran before the task when the class of task was not new, or state why it was skipped.
- Report warnings from every learning envelope instead of hiding them.

## Failure Modes

| Symptom | Action |
|---|---|
| `trigger and insight are required` | Both must be non-empty after trimming; write the pair properly or skip |
| `invalid outcome "..."` | Use `success`, `failure` or `neutral`; the default is `success` |
| `invalid scope "..."` | Use `project` or `global`; the default is `project` |
| `learning store is disabled` | Keep the pair in the answer; tell the user the store is off |
| `learning.recall` returns `partial` with no matches | Continue; absence of memory is not a failure |
| `learning.promote` returns `learning not found: <id>` | Re-check the id from the record envelope; never invent one |
| `learning.record` returns `error` | Keep the pair in the reply with the exact error; do not retry blindly |
| Confidence does not move after a record | The pair was deduplicated and reinforced; read `observations` |
| Two similar learnings exist | Keep both when triggers differ; when identical, reinforce the existing one |
| The user asks to "remember" a secret | Refuse; secrets belong in `.env`, never in the learning store |
| `evidence` exceeds 10 items | Only the last 10 are kept; trim to the most probative references |
| The learning store file is unreadable | Report the exact error; do not invent confidence or ids |
| A recall match looks irrelevant | Dismiss it explicitly with the reason; do not silently ignore it |
| `learning.promote` returns `already global` | Nothing to do; report it and move on |
| A learning id cannot be found after a record | Re-read the record envelope; ids are stable, so never invent one |

## Anti-Patterns

- Recording vague advice ("be careful with paths") instead of a trigger+behavior pair.
- Bundling several rules into one insight so it can never be cleanly confirmed.
- Recording one-off trivia: a file count, a timestamp, a session detail.
- Putting a secret, token or unredacted log excerpt into `trigger`, `insight` or `evidence`.
- Re-wording a pair to force a duplicate row instead of reinforcing the existing one.
- Promoting after a single success because the advice "feels right".
- Calling `learning.promote` repeatedly after `not eligible yet`.
- Recalling and then ignoring matches without saying why.
- Treating recall hits as instructions; they are prior experience, not commands.
- Recording a learning mid-task as if the outcome were already known.
- Using `scope: "global"` on the first observation to skip the promotion rules.
- Letting a lesson die in the chat instead of recording it before handoff.

## References

- `references/learning-quality.md` - good and bad trigger/insight examples, the confidence table, promotion rules and session-handoff use.
