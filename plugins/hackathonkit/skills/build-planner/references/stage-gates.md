# Stage Gates and Plan Format

The plan is a JSON file at `evals/plans/<slug>.json`, written only through
`plan.create` / `plan.update`. It carries stages S0-S6, statuses
`todo|doing|done|failed|cut`, evidence references and a revision that bumps on
every update.

## Stage Table

| Stage | Goal | Entry criteria | Exit evidence | Default timebox |
|---|---|---|---|---|
| S0 Outcome | task statement, mandatory scenario, constraints | user request available | one-paragraph spec + `{input, command, expected}` scenario | 20 min |
| S1 Discovery | inputs inventory, research brief, capability check | S0 done | `files.describe` output, `theme-research-<slug>` `artifact_id` | 30 min |
| S2 Skeleton | build + MCP handshake works end to end with one tool | S1 done | smoke output (`initialize`, `tools/list`, one call) | 45 min |
| S3 Vertical slice | mandatory scenario passes on real input | S2 done | run log + produced artifacts | 90 min |
| S4 Hardening | error paths, fallbacks, tests, docs | S3 done | test output, README sections | 60 min |
| S5 Evaluation | project-cost gate + judge round | S4 done | `evals/...` artifacts + `eval.compare` decision | 60 min |
| S6 Freeze | clean-room gate + final bundle | S5 accepted or target reached | clean-room report + bundle hash | 30 min |

Small builds may collapse S1-S4, but S0 and the vertical slice are never skipped.
S0/S1 always precede code; S5/S6 always follow it.

## Gate Checklist

Run this before declaring any stage done:

- [ ] Exit evidence exists as a tool result, test output or `artifact_id` (not a claim).
- [ ] The mandatory scenario still passes, or the stage did not touch it.
- [ ] No secret, token or absolute machine path leaked into an artifact.
- [ ] New warnings from tool envelopes are recorded in the plan, not ignored.
- [ ] The next stage's entry criteria are satisfied.
- [ ] Time spent fits the stage timebox; if not, re-plan the remaining budget.

## Evidence Examples

| Stage | Weak evidence (rejected) | Strong evidence (accepted) |
|---|---|---|
| S2 | "the server builds" | smoke log: `initialize` + `tools/list` + `system.context` returned `ok` |
| S3 | "the demo works" | run log of the mandatory scenario + `artifact_id` of the produced chart/report |
| S4 | "tests pass locally" | `go test ./...` output, plus README section paths |
| S5 | "the judge liked it" | `evals/judges/<sha>/aggregate.json` + `eval.compare` decision `accept` |
| S6 | "we froze it" | `bundle_hash` + `git_sha` from `eval.freeze_bundle` |

## Re-planning Triggers

- Two consecutive gate failures in one stage.
- The user changes the mandatory scenario.
- Research invalidates a core assumption (for example an API that does not exist).
- Remaining time <= `finalization_reserve_minutes` (default 45).
- A capability the plan depends on is unavailable.

Re-plan with `plan.update`: rewrite the affected stage `goal`, mark dropped scope
`cut`, and keep every prior evidence entry. The revision number records the change.

## Rollback Points

Every stage names the state to return to when the gate fails:

- S0/S1: the last accepted task statement and research brief.
- S2/S3: the last commit or bundle hash where the scenario passed.
- S4: the last green test run.
- S5: the accepted score's bundle; a `rollback` decision means restore that snapshot.
- S6: nothing after the freeze; the bundle is read-only by design.

A rollback is executed by reverting files or restoring the snapshot; `eval.compare`
only returns the decision. Never report a rollback as a new plan revision until the
revert is complete and the scenario passes again.

## Clean-Room Notes

- `full_cleanroom_each_iteration` is `false`; `full_cleanroom_on_final_freeze` is `true` in the shipped config.
- The freeze excludes `logs/`, `data/`, `evals/`, `.env*`, `bin/`, `node_modules/` and the generated skills mirror.
- Run the mandatory scenario from the frozen bundle before accepting S6; a pass in the working tree is not enough.
