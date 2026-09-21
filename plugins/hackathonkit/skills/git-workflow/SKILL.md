---
name: git-workflow
description: "Use when preparing commits, branches or worktrees for hackathon work: small checkpoint commits, imperative subject lines, evidence in the commit body (gate result, test output, artifact ids), secret hygiene and branch isolation via hackathonkit-ctl worktree. Do not use to run commits the user did not ask for, to implement changes ($tdd-workflow) or to prove them ($verification-loop)."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: quality
tools:
  - system.context
  - plan.status
  - plan.update
---

# Git Workflow

Prepare clean, reviewable checkpoint commits for hackathon work; never commit secrets and never rewrite shared history.

## When To Use

- A plan stage reached a green checkpoint and the work should be committed.
- The user asks for a commit, branch, worktree or history cleanup.
- Starting an isolated run that must not touch the main checkout (`hackathonkit-ctl worktree create <slug>`).
- A commit message needs evidence (gate result, test output, artifact ids) before handoff.
- The staged diff may contain generated files, logs or secrets and must be screened before a commit.
- A large working tree needs splitting into reviewable checkpoint commits.
- Do not use to run commits, pushes or history rewrites the user did not ask for; this skill prepares and proposes first.
- Do not use for implementing the change (`$tdd-workflow`) or proving it (`$verification-loop`).

## Core Principles

1. **Propose, never auto-commit.** Do not run `git commit`, `git push` or any history rewrite unless the user explicitly asks; otherwise prepare the staged diff and the exact commands for review.
2. **Small checkpoint commits.** One logical step per commit - a green test, a stage slice, a doc fix. A commit that mixes behavior and refactor is rejected.
3. **The subject is imperative and scoped.** `add anomaly chart fallback`, `fix epoch millis parsing`; no "wip", no "fixes", no trailing period, at most 72 characters.
4. **Secrets never enter history.** `.env`, keys, tokens, database files and logs stay out; scan the staged diff before proposing.
5. **Evidence in the body.** The body names the gate sha and result, the test command and result, and the artifact ids; an unevidenced claim does not ship.
6. **Shared branches are immutable.** Never force-push `main` or a shared branch; isolate risky work in a worktree branch (`hackathonkit/<slug>`).
7. **The plan owns the checkpoint.** `plan.status` gives the stage; `plan.update` records the commit sha and evidence after the commit exists.

## Workflow

1. Call `system.context` once. Read `routing_mode` and confirm the repository root; if the work runs in a worktree, the root is the worktree path, not the main checkout.
2. Call `plan.status` with no `slug` (latest plan) or `slug=<plan>`. Keep `slug`, `revision`, `current_stage.id` and the stage goal; the commit message references the stage.
3. Inspect before staging: `git status --short`, `git diff` and `git diff --staged`. Do not stage anything before the diff is understood; unrelated edits are left alone.
4. Secret scan the staged diff: `.env*`, `*.pem`, `*.key`, `*.p12`, tokens, passwords, `data/control/`, `data/worktrees/`, `logs/`, `bin/` and absolute machine paths. If a secret is staged, unstage it, add the path to `.gitignore`, and report it; if it was already pushed, rotate it and propose a follow-up commit instead of a history rewrite.
5. Isolate when the change is risky, long-running or parallel to another session: propose `hackathonkit-ctl worktree create <slug>` (worktrees live under `data/worktrees/<slug>` on branch `hackathonkit/<slug>`) instead of switching branches in the main checkout.
6. Stage explicitly: `git add <paths>` for the logical step; never `git add -A` or `git add .` in a dirty tree.
7. Gate the checkpoint: the last gate evidence must be fresh for this tree. If it is stale or missing, hand off to `$verification-loop`; never invent a gate sha. A commit that touches the mandatory scenario requires a rerun of the scenario first.
8. Compose the message with `references/commit-conventions.md`: imperative subject, blank line, what/why body wrapped at 72, then the evidence block. Propose the exact `git commit -m "<subject>" -m "<body>"` command for the user to run.
9. If the user runs the commit (or explicitly asks you to), keep the new sha and record the checkpoint: call `plan.update` with `slug`, `stage=<current_stage.id>`, `status` (`doing` for a mid-stage checkpoint, `done` when the stage closes) and `evidence` (`[commit sha, gate sha + result, test output, artifact ids]`). Keep the bumped `revision`.
10. If the user declines or does not respond, leave the work staged or unstaged as found and state that the checkpoint is not committed.
11. Answer per the Output Contract; carry warnings from every envelope into the answer.

### Commit Sequence

The exact command sequence proposed to the user, in order:

```sh
git status --short                 # inventory, never stage blind
git diff                           # unstaged changes
git diff --staged                  # what a commit would capture now
git add <paths>                    # explicit paths only
git diff --staged                  # re-read the staged set and scan for secrets
git commit -m "<subject>" -m "<body + evidence block>"
```

- The second `git diff --staged` is the secret gate; never skip it because the first looked fine.
- If a path should never be tracked, add it to `.gitignore` in the same commit that removes it from the index.
- Proposed commands must run unchanged; no placeholders left in the message.

### What Goes in One Commit

| Include | Exclude |
|---|---|
| One behavior change plus its test | Unrelated formatting churn |
| The `.gitignore` entry for a removed secret | The secret itself |
| A plan/skill doc change tied to the stage | Generated `evals/` evidence |
| A golden-file update with its diff read | Golden updates made by rerunning blindly |
| The smallest set of paths that builds and tests green | `data/`, `logs/`, `bin/` artifacts |

A commit is a checkpoint only if it can be reverted alone without breaking the build.

## Decision Matrix

| Signal | Action |
|---|---|
| User explicitly asked for a commit | Run it after the secret scan and message review |
| User did not ask | Prepare the diff and message; do not commit or push |
| Risky, long or parallel run | Propose `hackathonkit-ctl worktree create <slug>`; one worktree per run |
| Staged diff contains `.env`, keys or tokens | Unstage, add to `.gitignore`, rotate if pushed; never commit |
| Change mixes behavior and refactor | Split into two commits, one logical step each |
| Gate evidence is stale or missing | Do not propose the checkpoint; run `$verification-loop` first |
| Commit touches the mandatory scenario | Rerun the scenario, cite the result in the body, then propose |
| Change belongs on a shared branch | Use a worktree branch and propose a normal merge; never force-push `main` |
| History rewrite requested | Confirm the branch is local and unshared; otherwise refuse and explain |
| Commit already pushed but evidence changed | New commit; never amend or rebase a pushed commit |
| Stage id unknown to the plan | `plan.status` for valid ids; never invent a stage |
| No plan exists | Apply commit hygiene, but state that no stage evidence was attached |

## Output Contract

- **Proposed commit** - branch or worktree, staged file list, full message (subject, body, evidence block).
- **Secret scan** - `clean`, or the finding and the action taken (unstaged, ignored, rotated).
- **Evidence block** - gate `sha` and result, test command and result, artifact ids.
- **Isolation** - the worktree/branch used, or "main checkout" with the reason.
- **Plan record** - `plan.update` result: `slug`, `revision`, `stage`, `status`, evidence; or why it was not called.
- **Commit state** - the commit sha if it was created, otherwise `not committed` plus the exact command to run.
- **Warnings** - from every envelope used.

## Worked Checkpoint

Task: stage S3 of `demo-plan` is green; the user asked for a commit.

1. `system.context` -> root is the main checkout, `routing_mode: normal`.
2. `plan.status` -> `slug: demo-plan`, `revision: 8`, `current_stage.id: S3`.
3. `git status --short` shows `internal/plans/plans.go`, `internal/plans/plans_test.go`
   and an unexpected `logs/session-events.jsonl`; the log file is ignored, not staged.
4. `git add internal/plans/plans.go internal/plans/plans_test.go`; `git diff --staged`
   is scanned: no `.env`, keys, tokens or absolute paths.
5. The gate result from `$verification-loop` is fresh (`sha 9f2c41a`, `ok`); the scenario
   was rerun because the parser touches the demo path.
6. Proposed message:

   ```text
   fix epoch millis parsing

   Normalize millisecond epochs before RFC3339 parsing so telemetry rows
   keep their timestamps in the anomaly report.

   Evidence:
   - gate: 9f2c41a ok (gofmt/build/vet/test/selftest/skills)
   - tests: go test ./internal/plans/... -count=1 ok
   - artifacts: reports/telemetry-anomalies.md
   - stage: S3 vertical slice (plan demo-plan, revision 8)
   ```

7. The user runs the commit; the new sha is `a1b2c3d`.
8. `plan.update` `{slug: demo-plan, stage: S3, status: done, evidence: [a1b2c3d, gate 9f2c41a ok]}`
   -> revision 9.

If the user had not asked, steps 7-8 would stop at the proposal and the answer would say
`not committed`.

## Verification

- No `git commit`, `git push`, amend, rebase or force-push ran without an explicit user request.
- The staged file list matches the reviewed diff; no `git add -A` swept a dirty tree.
- The secret scan covered the staged diff and found nothing, or the finding was handled and reported.
- The subject is imperative, at most 72 characters, and the body carries a real gate sha and test result.
- The commit does not mix behavior with refactor or unrelated files.
- `plan.update` returned a bumped `revision` with the commit sha in the stage evidence.
- Shared branches were not rewritten; risky work used worktree isolation.

## Failure Modes

| Symptom | Action |
|---|---|
| `git status` shows unexpected files | Stop and inspect before staging; never `git add -A` to move on |
| A secret appears in the staged diff | Unstage, add to `.gitignore`, rotate if pushed; report the finding |
| `.env` was already committed earlier | Do not rewrite shared history; report it, rotate the secret, propose a follow-up commit |
| User asks to force-push `main` | Refuse; propose a worktree branch and a normal merge |
| Gate evidence is stale | Hand off to `$verification-loop`; do not commit on stale evidence |
| Commit is too large to review | Split by logical step; a commit spanning unrelated areas is not a checkpoint |
| Message has no evidence block | Rewrite before proposing; an unevidenced commit is rejected |
| `plan.update` returns `unknown stage` | `plan.status` to list ids; update an existing stage only |
| No plan exists | Apply commit hygiene, but state that no stage evidence was attached |
| `hackathonkit-ctl` is unavailable | Fall back to a local branch and say so; do not improvise worktrees by hand |
| Pre-commit hook fails | Fix the cause and restage; never bypass with `--no-verify` without explicit user approval |
| Two sessions edit one checkout | Stop; move one session to a worktree before committing |

## Anti-Patterns

- Committing, pushing or rewriting history without an explicit request.
- `git add -A` in a dirty tree, sweeping in logs, data, build output or secrets.
- "wip", "fixes" or "changes" subjects with no scope and no evidence.
- Force-pushing a shared branch to tidy history, or amending a pushed commit.
- Mixing a refactor with a behavior change in one commit.
- Citing a gate sha or test result that was never produced for this tree.
- Committing while a verification loop is failing or inside the finalization reserve.
- Editing `.gitignore` to hide a secret instead of removing and rotating it.
- Recording a commit in `plan.update` before the commit exists.

## References

- `references/commit-conventions.md` - message shapes, checkpoint patterns, the evidence block format and the never-commit list.
