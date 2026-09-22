# Commit Conventions

Commit messages are the audit trail for a hackathon build. A reader must be able to tell what changed, why, and which evidence backed it, without opening the chat log.

## Message Shape

```text
<imperative subject, <= 72 chars>

<what changed and why, wrapped at 72 chars; name the stage goal it serves>

Evidence:
- gate: <sha> ok (gofmt/build/vet/test/selftest/skills)
- tests: go test ./... ok
- artifacts: <artifact ids, if any>
- stage: S3 <stage goal>
```

- Subject is imperative present tense: `add`, `fix`, `refactor`, `docs`, `test`, `chore`. Not `added`, not `adds`.
- Optional scope after the verb reads naturally: `add(router): validate fly recommendations`.
- No period at the end; no emoji; no ticket numbers that do not exist.
- Body explains why, not just what; the diff already shows what.
- Evidence lines are copied from real envelopes, never paraphrased.

## Checkpoint Patterns

| Pattern | When | Subject example |
|---|---|---|
| Test checkpoint | A RED/GREEN cycle landed | `test: reproduce epoch millis parsing bug` |
| Implementation checkpoint | A stage slice is green | `add anomaly chart fallback` |
| Refactor checkpoint | Behavior-preserving cleanup | `refactor: extract gate check runner` |
| Docs checkpoint | README/plan/skill text only | `docs: document verification phases` |
| Stage close | The stage gate passed | `complete S3 vertical slice with gate evidence` |

Keep each checkpoint reversible on its own. If a revert of one commit would break the build, the split is wrong.

## Evidence Block

```text
Evidence:
- gate: 9f2c41a ok (gofmt/build/vet/test/selftest/skills)
- tests: go test ./internal/plans/... -count=1 ok
- artifacts: reports/telemetry-anomalies.md
- stage: S3 vertical slice (plan telemetry-anomalies, revision 7)
```

- The gate `sha` must match the tree being committed; a stale sha is worse than no evidence.
- Failed-then-fixed checks are not listed; only the final green result with its sha.
- Artifact ids are optional but required when the commit adds or changes a generated deliverable.

## Never Commit

| Path / pattern | Why |
|---|---|
| `.env`, `.env.*` | secrets; the freeze excludes them too |
| `*.pem`, `*.key`, `*.p12`, `id_rsa*` | keys and certificates |
| tokens, passwords, connection strings | credentials in any form |
| `data/control/`, `data/worktrees/`, `data/artifacts/`, `data/index/` | generated runtime state |
| `logs/`, `bin/`, `*.exe` | logs and build output |
| `evals/quality/`, `evals/decisions/`, `evals/judges/`, `evals/plans/` | generated evidence; the freeze owns them |
| editor and OS junk (`.DS_Store`, `*.swp`) | noise |
| absolute machine paths in text or config | breaks every other checkout |

If one of these is already tracked, removing it from the index and adding it to `.gitignore` is a normal commit; rotating the secret is a separate action.

## Branch and Worktree Isolation

- `hackathonkit-ctl worktree create <slug>` creates `data/worktrees/<slug>` on branch `hackathonkit/<slug>`; one run per worktree.
- Long, risky or parallel work never shares the main checkout.
- Merges back to the shared branch happen as normal merges after the mandatory scenario passes.
- Never force-push a shared branch; never rewrite history that another session may have based work on.

## Proposing a Commit

State the exact commands so the user can run them unchanged:

```sh
git add internal/plans/plans.go internal/plans/plans_test.go
git commit -m "fix epoch millis parsing" -m "Normalize millisecond epochs before RFC3339 parsing.

Evidence:
- gate: 9f2c41a ok (gofmt/build/vet/test/selftest/skills)
- tests: go test ./internal/plans/... -count=1 ok
- stage: S3 vertical slice"
```

After the commit exists, record it with `plan.update`: stage `S3`, status `doing` or `done`, evidence `[commit sha, gate sha, test command]`. Never record a commit sha before the commit is created.
