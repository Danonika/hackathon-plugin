---
name: system-design-expert
description: "Use when the demo works but the structure is fragile: review module boundaries, MCP tool contracts, failure isolation, storage, observability and routing propagation, and propose structural changes only with measurable benefit. Explicit-only; never invoke implicitly. Do not use for hot-path performance ($cp-expert-refactoring), bug hunting ($roast-project) or scoring ($project-cost)."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: quality
tools:
  - plan.status
  - system.context_budget
  - learning.record
---

# System Design Expert

Review the system, not a single function; propose structural changes only with measurable benefit.

## When To Use

- Before the final freeze, when the demo works but the structure is fragile.
- After `$cp-expert-refactoring`: review hot paths first, then module boundaries and contracts.
- When one backend failure or a missing key degrades unrelated tools.
- When tool schemas, skills or manifests dominate the context budget.
- Do not use for algorithmic hot paths; use `$cp-expert-refactoring`.
- Do not use for runtime bug hunting; use `$roast-project`.
- Do not use for scoring; `$project-cost` and `$judge` own scores, and the controller owns `eval.compare` decisions.

## Core Principles

1. **Review the system, not a function.** Every proposal names the boundary, the contract and the failure mode it fixes.
2. **Modular monolith.** No microservices, queues or new abstraction layers without a measured bottleneck and a second implementation.
3. **Contracts before internals.** Tool argument names, envelope shape, path rules and artifact ids are public interfaces.
4. **Failure isolation is a feature.** A backend down, a missing key or malformed input must degrade with a warning or fail clearly, never corrupt state.
5. **Evidence over taste.** A proposal without a measurable benefit, a risk level and a verification command is an open question, not a change.
6. **Code decides.** When a scored loop is active, `eval.compare` (called by `$project-cost` or the controller) accepts or rolls back; this skill never decides in prose.

## Review Areas

- Module boundaries and dependency direction (`mcp`, `tools`, `routing`, `storage`, `metadata`, `objectstore`).
- MCP tool granularity: capability-level, not infrastructure-level; consistent argument naming.
- Coupling: who knows about whom; a tool testable without Docker or the Codex CLI.
- State ownership: config vs runtime state vs caches; who writes `routing.yaml` and when.
- Failure isolation: backend down, missing key, malformed input; degrade with a warning or fail clearly.
- Retry and idempotency: artifact writes, index rebuilds, duplicated tool calls.
- Storage contracts: filesystem authority vs MinIO mirror; read fallback and cache rehydration.
- Observability: `trace_id` through the call path; call and fly event logs; metric names.
- Security boundaries: path sanitization, secret handling, subprocess arguments, tool allowlist.
- Routing-mode propagation: live read, atomic writes, no stale cache.
- Docker topology and clean-room reproducibility; evaluation vs runtime separation.

Concrete questions and evidence commands per area: `references/design-review-checklist.md`.

## Priority Order

correctness -> reproducibility -> clarity -> failure handling -> performance -> future scaling.

A proposal that improves a later item while risking an earlier one is rejected.

## Constraints

- Modular monolith only; no microservices without a measured bottleneck.
- No new abstraction layer for a single implementation.
- Every structural change keeps `scripts/smoke` and the mandatory scenario passing.
- Evaluation artifacts stay under `evals/` and are excluded from runtime bundles.

## Workflow

1. **Read the plan.** Call `plan.status` (no slug -> latest plan); keep `current_stage`, the `mandatory_scenario` and its evidence. Structural changes must not break the scenario.
2. **Measure the context surface.** Call `system.context_budget`; use flagged `items[]` and `recommendations[]` as evidence for tool-granularity, skill-size or manifest-bloat proposals.
3. **Walk the review areas** above against the snapshot: module boundaries, contracts, state ownership, failure isolation, retry/idempotency, storage, observability, security, routing, evaluation/runtime separation and Docker topology.
4. **Reproduce each concern.** For every suspected problem, run a command or a tool and keep the output; a concern without a reproduction stays in `open_questions`.
5. **Propose.** One proposal per concern with `problem`, `change`, `benefit`, `risk` (`low`/`medium`/`high`) and `verification`. Record ideas rejected by the modular-monolith or single-implementation rules in `rejected[]` with the rule they violate.
6. **Apply one at a time.** After each proposal run `go build ./...`, `go vet ./...`, `go test ./...`, the MCP handshake smoke, the mandatory scenario and the Normal/Fly routing toggle. Revert any proposal that fails.
7. **Record.** Call `learning.record` with `trigger`, `insight`, `outcome` and `evidence` (test output, trace ids, command output) for reusable structural lessons.

## Decision Matrix

| Signal | Action |
|---|---|
| backend down or key missing degrades unrelated tools | propose isolation at the boundary; degrade with a warning |
| one tool schema or skill dominates `system.context_budget` | propose splitting, merging or trimming it |
| two components own the same state | propose a single writer; document every reader |
| retry duplicates an artifact or index write | propose idempotent keys or a write guard |
| routing mode is cached in a process | propose a live re-read with atomic writes |
| a change needs a second implementation to be justified | reject: no abstraction for a single implementation |
| a proposal fails smoke or the mandatory scenario | revert immediately and report the failing command |
| the benefit cannot be measured | keep as an open question; do not change code |
| structural churn starts during the final freeze | stop; the freeze accepts only critical fixes |

## Worked Example

```text
problem:  storage.Store mixes persistence with backend clients; tool tests need fakes
evidence: go test ./internal/storage output + system.context_budget shows metadata deps in tools
change:   keep Store as a facade; backend clients stay in objectstore / metadata
benefit:  tool tests run without Docker, Postgres or MinIO
risk:     low - interfaces already exist
verify:   go test ./internal/storage && scripts/smoke.sh
result:   applied; go build/vet/test pass, handshake and mandatory scenario unchanged
```

This mirrors the real repository direction (filesystem authoritative, backends attached); proposals that contradict it are rejected.

## Output Contract

```json
{
  "proposals": [
    {"problem": "storage.Store mixes persistence and backend clients",
     "change": "keep Store as a facade, keep MinIO in objectstore",
     "benefit": "tool tests run without backend fakes",
     "risk": "low",
     "verification": "go test ./internal/storage",
     "evidence": "go test output / trace_id"}
  ],
  "rejected": [{"idea": "split MCP into microservices", "reason": "no measured bottleneck"}],
  "open_questions": ["does the read fallback rehydrate the local cache under concurrent reads?"],
  "checks_run": ["go build ./...", "go vet ./...", "go test ./...", "MCP handshake", "mandatory scenario", "Normal/Fly toggle"]
}
```

Every proposal carries evidence, a measurable benefit, a risk level and a verification command; the full checklist lives in `references/design-review-checklist.md`.

## Verification

- Every proposal has evidence, a benefit, a risk level and a verification command.
- `go build ./...`, `go vet ./...` and `go test ./...` pass after each applied proposal.
- The MCP handshake, the Normal/Fly routing toggle and an artifact write/read round-trip still work.
- The mandatory scenario from `plan.status` still passes.
- Rejected ideas name the rule they violate; no silent scope growth.
- Applied proposals are recorded with `learning.record` and their evidence.

## Failure Modes

| Symptom | Action |
|---|---|
| a proposal breaks the mandatory scenario | revert; the scenario wins over structure |
| tests pass but the MCP handshake fails | treat the handshake as the contract; revert |
| the storage round-trip loses an artifact | critical; fix or revert before any other work |
| `system.context_budget` errors | continue with the checklist and note the missing evidence |
| a proposal expands scope mid-round | split it into a new proposal; one boundary per change |
| the reviewer disagrees with the plan stage | escalate to `$self-improvement-controller`; do not edit the plan here |
| Docker/clean-room topology cannot be reproduced | treat as high risk; propose the smallest change and verify in a fresh checkout |

## Anti-Patterns

- Microservices, queues or new layers in a hackathon build.
- Abstraction for a single implementation.
- Moving code without changing a contract or fixing a failure.
- Hiding a backend failure behind a silent fallback with no warning.
- Editing risk scores or eval decisions by hand.
- Structural churn during the final freeze.
- Rewriting working code to match a personal style.

## References

- `references/design-review-checklist.md` - review areas, concrete failure questions and evidence commands.
