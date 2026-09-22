# Design Review Checklist

Walk every area against the snapshot; each suspected problem needs a reproduction before it becomes a proposal.

## 1. Module Boundaries and Dependency Direction

- [ ] `internal/mcp` does not import domain packages; tools depend on `mcp`, not the reverse.
- [ ] `internal/storage` is a facade; backend clients live in `internal/objectstore` and `internal/metadata`.
- [ ] No package imports `cmd/*`; no cyclic imports.
- [ ] A tool can be unit-tested without Docker, Postgres, MinIO or the Codex CLI.
- Evidence: `go list -deps ./internal/...`, `go test ./internal/<pkg>`.

## 2. MCP Tool Contracts

- [ ] One tool per capability, not per infrastructure call.
- [ ] Argument names and envelope fields are consistent across tools.
- [ ] Every tool returns the shared envelope (`status`, `data`, `sources`, `warnings`, `metrics`, `trace_id`).
- [ ] Path rules are uniform: inputs relative to `data/input`, artifacts relative to `data/artifacts`.
- Evidence: `go run ./cmd/hackathonkit-mcp --list-tools`, `system.context_budget` item sizes.

## 3. State Ownership

- [ ] Each mutable file has one writer: `routing.yaml`, `configs/*`, `data/index/*`, `data/control/*`.
- [ ] Config, runtime state and caches are separated; caches can be rebuilt.
- [ ] No process holds routing mode or evaluation config in memory without a re-read.
- Evidence: grep for writers; toggle routing and read `system.context` without restart.

## 4. Failure Isolation

- [ ] Postgres/MinIO down -> artifacts still land on the filesystem with a warning.
- [ ] Missing `OPENAI_API_KEY` -> audio/vision fail clearly; embeddings fall back with a warning.
- [ ] One tool panic does not kill the server; the JSON-RPC error path is contained.
- [ ] Malformed input returns `status=error` or `partial`, never a hang.
- Evidence: stop a backend, call the tool, read the envelope warnings.

## 5. Retry and Idempotency

- [ ] Artifact writes are atomic or idempotent; repeated calls do not duplicate ids.
- [ ] Index rebuilds replace, not append.
- [ ] Control-plane ingest is idempotent per byte cursor.
- Evidence: call twice, compare artifact lists and row counts.

## 6. Storage Contracts

- [ ] The local filesystem is authoritative; MinIO is a mirror with read fallback and cache rehydration.
- [ ] A missing local artifact is fetched from the backend or fails with a clear warning.
- [ ] Schema creation on connect is safe to repeat.
- Evidence: delete a local artifact, read it again, inspect the warning.

## 7. Observability

- [ ] One `trace_id` flows through envelope, logs and stored records.
- [ ] `logs/mcp-calls.jsonl` has `ts`, `tool`, `duration_ms`, `status`, `trace_id` per call.
- [ ] Metrics counters exist for new code paths (`eval_*`, `fly_*`, `mcp_*`).
- [ ] Warnings are logged and returned, not swallowed.
- Evidence: tail the JSONL files and match a trace id end to end.

## 8. Security Boundaries

- [ ] Input paths reject absolute paths and `..`.
- [ ] Secrets live in `.env` only; bundles exclude `.env*`, `logs/`, `data/`, `evals/`.
- [ ] Subprocess arguments are not built from untrusted input.
- Evidence: call a tool with `../etc/passwd`; inspect a bundle's `excluded[]`.

## 9. Routing and Evaluation Separation

- [ ] `configs/routing.yaml` is re-read on every `system.context`; writes are atomic.
- [ ] Evaluation artifacts stay under `evals/` and are excluded from runtime bundles.
- [ ] Fly is a recommender; a failed `routing.fly_recommend` falls back to normal mode silently.
- Evidence: toggle Normal/Fly, check `routing_config_version`.

## 10. Docker and Clean-Room

- [ ] `docker compose build` succeeds from a fresh clone.
- [ ] Containers run as the host UID/GID recorded in `.env`.
- [ ] `scripts/smoke.sh` passes and the mandatory scenario runs inside the clean room.
- Evidence: clean-room report with commands and results.

## Proposal Quality Bar

- One boundary per proposal; no bundled refactors.
- `benefit` is measurable ("tool tests run without fakes"), not aesthetic.
- `risk` is honest: a schema, migration or behaviour change is at least medium.
- `verification` is an exact command a reviewer can run.
