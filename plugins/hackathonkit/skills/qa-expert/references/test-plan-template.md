# Test Plan Template

Copy this into `reports.create` as `Test plan - <task>` and fill every section.
Empty sections are findings, not omissions.

## Scope

- Task / plan revision: `<slug rN>`
- Candidate snapshot: `<git_sha or bundle_hash>`
- Mandatory scenario: `<input -> action -> expected result>`
- In scope: `<areas>`
- Out of scope (with reason): `<areas>`

## Acceptance Criteria

| ID | Criterion (input -> action -> observable result) | Kind | Source |
|---|---|---|---|
| AC-1 | upload `data/input/sample-telemetry.csv` -> run anomaly detection -> anomaly table + chart artifact | mandatory | TASK.md |
| AC-2 | empty CSV -> analysis -> explicit error, no crash | functional | risk matrix |
| AC-3 | unknown artifact id -> documents.get -> error envelope | functional | API contract |

Kind: `mandatory` (admission condition, regulation 7.3) or `check`.

## Risk Matrix

Priority = impact (1-3) x likelihood (1-3). Test order follows priority.

| Area | Impact | Likelihood | Priority | Why |
|---|---|---|---|---|
| MCP handshake | 3 | 1 | 3 | blocks every tool call |
| Storage fallback | 3 | 2 | 6 | missing Postgres/MinIO must degrade |
| Path sanitization | 3 | 2 | 6 | security |
| XLSX multi-sheet | 2 | 2 | 4 | common input |

## Checks

| ID | Check | Type | Command / steps | Result | Evidence |
|---|---|---|---|---|---|
| CH-1 | gate | automated | `scripts/quality-gate.sh` | pass/fail | `evals/quality/<sha>/gate.json` |
| CH-2 | handshake | automated | `scripts/smoke.sh` | pass/fail | three JSON-RPC responses |
| CH-3 | mandatory scenario | manual | run AC-1 end to end | pass/fail | artifact ids |
| CH-4 | empty input | manual | AC-2 | pass/fail | error envelope |
| CH-5 | secrets | automated | `security.review` on changed files | pass/fail | findings[] |

Result values: `pass`, `fail`, `not tested` (with blocker), `skipped` (with reason).

## Environments

- OS / engine: `<linux + docker 27 | windows + wsl2 | no docker>`
- Runtime: `<bin/ binaries | docker compose>`
- Keys present: `<OPENAI_API_KEY yes/no>` (never print values)

## Coverage Summary

- Tested: `<n checks>`
- Skipped / not tested: `<list with reasons>`
- Bugs found: `<ids>`
- Residual risk: `<what could still break and why that is acceptable or not>`

## Verdict

`release-ready` | `conditional` | `not ready` - deciding facts in one sentence.
