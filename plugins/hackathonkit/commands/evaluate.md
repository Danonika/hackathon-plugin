---
description: Score the frozen demo (judge, 3+ isolated runs) and technical readiness, then run bounded improvement rounds.
argument-hint: "[scope]"
---

# /evaluate

Evaluate: $ARGUMENTS

The agent runs the evaluation tools; only accept/rollback/stop comes from `eval.compare`.

1. `eval.freeze_bundle` (`profile: demo` for Demo Day, `technical` for readiness) -> keep the
   bundle path and hash.
2. `$judge`: at least 3 valid isolated runs over the frozen bundle, then `eval.aggregate`
   (drops invalid/conflicted/foreign-sha reports), `eval.calibrate` (noise floor) and
   `eval.rank` (regulation 9.6 + commission). A missing judge report is never zero.
3. `$project-cost`: evidence-only technical readiness score.
4. `$self-improvement-controller`: bounded rounds; accept/rollback/stop only from `eval.compare`.
5. Optional hardening, in this order: `$roast-project`, `$qa-expert`,
   `$cp-expert-refactoring` (hot paths), `$system-design-expert` (structure), `$output-self-check`.
