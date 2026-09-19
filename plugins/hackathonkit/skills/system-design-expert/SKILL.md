---
name: system-design-expert
description: "Structural review of module boundaries, contracts, failure isolation, storage, observability and runtime/evaluation separation. Explicit-only."
---

# System Design Expert

Review the system, not a single function. Propose structural changes only with measurable benefit.

## Review Areas

- Module boundaries and dependency direction (mcp, tools, routing, storage, metadata, objectstore).
- MCP tool granularity: capability-level, not infrastructure-level; consistent argument naming.
- Coupling: who knows about whom; can a tool be tested without Docker or Codex.
- State ownership: config vs runtime state vs caches; who writes `routing.yaml` and when.
- Failure isolation: backend down, missing key, malformed input; degrade or fail clearly.
- Retry/idempotency: artifact writes, index rebuilds, duplicated tool calls.
- Storage contracts: filesystem mirror vs MinIO source of truth; read fallback correctness.
- Observability: trace ids through the call path; call and fly event logs; metric names.
- Security boundaries: path sanitization, secret handling, subprocess arguments, tool allowlist.
- Routing-mode propagation: live read, atomic writes, no stale cache.
- Docker topology and clean-room reproducibility; evaluation vs runtime separation.

## Priority Order

correctness -> reproducibility -> clarity -> failure handling -> performance -> future scaling.

## Proposal Format

```json
{
  "proposals": [
    {"problem": "storage.Store mixes persistence and backends",
     "change": "keep Store as facade, move MinIO to objectstore (already done)",
     "benefit": "tool tests run without backend fakes",
     "risk": "low", "verification": "go test ./internal/storage"}
  ],
  "rejected": [{"idea": "split MCP into microservices", "reason": "no measurable need in a 5-hour build"}]
}
```

## Hackathon Agent Kit Constraints

- Modular monolith only; no microservices without a measured bottleneck.
- No new abstraction layer for a single implementation.
- Every structural change must keep `scripts/smoke` and the main scenario passing.

## Verification

After structural changes: `go build`, `go vet`, `go test ./...`, MCP handshake smoke, routing toggle check, artifact write/read round-trip.
