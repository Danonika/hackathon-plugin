---
name: devops-expert
description: "Use when Docker, docker-compose, the Dockerfile, .env, ports, volumes, profiles, healthchecks, image pulls, local stack bring-up or scripts/setup.sh / doctor.sh / repair.sh need configuration, diagnosis or verification. Checks for errors and proves the stack works from command output. Do not use for CI/release pipelines (out of scope) or application-code bugs ($roast-project)."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: ops
tools:
  - quality.gate
  - system.context
  - security.review
  - reports.create
  - plan.update
  - learning.record
---

# DevOps Expert

Own the local container stack: Dockerfile, compose, environment and scripts. Every
claim of "it works" must be backed by command output.

## When To Use

- Dockerfile or `docker-compose.yml` must be reviewed, changed or validated.
- The stack fails to build, start, become healthy or answer the MCP handshake.
- `.env`, ports, volumes, profiles, healthchecks, image pulls or permissions need a diagnosis.
- `scripts/setup.sh`, `scripts/doctor.sh`, `scripts/repair.sh`, `scripts/smoke.sh` or the launcher misbehave.
- Before a demo or clean-room run, to prove the stack works end to end.
- Do not use for CI/release pipelines, registries or production orchestration (out of scope in this repository).
- Do not use for application-code defects; use `$roast-project` or `$cp-expert-refactoring`.

## Core Principles

1. Evidence or silence: a stack claim requires the exact command, exit status and relevant output line. Never say "should work".
2. Read before write: inspect the Dockerfile, compose, `.env.example`, scripts and launcher before changing anything.
3. The MCP server runs inside a container without the Docker CLI: run Docker commands in the host shell, never through an MCP tool.
4. Secrets stay in `.env`: never print, copy or commit values; report only the key name and whether it is set.
5. Loopback by default: published ports bind `127.0.0.1`; wider binds are a security decision, not a convenience.
6. Non-destructive by default: no `down -v`, no volume deletion, no `prune`, no image pushes without an explicit user request.
7. Degrade honestly: without a Docker engine the supported fallback is `bin/` binaries; state the gap instead of pretending.

## Workflow

1. Inventory the stack (read-only):
   - Dockerfile stages and targets: `build`, `runtime-base`, `hackathonkit-mcp`, `demo-api`, `quality`.
   - `docker-compose.yml` services: `hackathonkit-mcp`, `demo-api`, `quality` (profile `quality`), `postgres`, `redis`, `minio`; named volumes `pgdata`, `miniodata`, `go-mod-cache`, `go-build-cache`.
   - `.env.example` keys and which compose variables use `${VAR:?}` (currently `POSTGRES_PASSWORD`, `MINIO_ROOT_PASSWORD`).
   - Scripts: `scripts/setup.sh`, `doctor.sh`, `repair.sh`, `smoke.sh`, `gen-mcp-manifest.sh`, `sync-harness.sh`, `quality-gate.sh`.
   - Launchers: `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh` / `.cmd`.
2. Static validation before building:
   - `docker compose config` parses the file and interpolates variables (catches missing `.env` keys, bad YAML, undeclared volumes).
   - Cross-check every `build.target` against Dockerfile stages and every named volume against the `volumes:` block.
   - Confirm published ports bind `127.0.0.1` and that the demo API keeps its token/origin guards.
3. Security pass on the stack:
   - `security.review` on changed YAML/env/scripts (secrets, injection, suspicious content).
   - Containers run non-root (`HACKATHONKIT_UID`/`HACKATHONKIT_GID`); no secret is baked into an image; `.dockerignore` excludes `.env`, `data/`, `logs/`, `evals/`.
4. Build and bring up, one step at a time:
   - Run `docker compose build hackathonkit-mcp demo-api minio`. Build the `quality` target when needed.
   - `docker compose up -d postgres redis minio`.
   - `docker compose run --rm quality` for the Go-enabled gate image.
5. Prove the stack works:
   - `sh scripts/doctor.sh` (runtime, plugin, hooks, adapters, skills, handshake).
   - `sh scripts/smoke.sh` (initialize / tools/list / system.context).
   - `curl -fsS http://127.0.0.1:8787/healthz` and `docker compose logs --tail=50 <service>` for anything unhealthy.
6. Diagnose failures with the matrix below; fix one cause at a time and re-run the failing command.
7. Record evidence: `quality.gate` for the build/test state, `reports.create` with title `DevOps report - <stack>` containing commands, outputs, fixes and remaining risks; `plan.update` evidence when a plan stage depends on the stack; `learning.record` when a fix is reusable (for example "compose passwords must be in .env before up").
8. Hand back: changed files, commands run, current health, what is still unverified (for example a registry that is unreachable).

## Decision Matrix

| Signal | Action |
|---|---|
| `docker compose config` fails | Fix YAML or the missing `${VAR:?}` value in `.env`; never commit the value |
| `build.target` has no Dockerfile stage | Add the stage or fix the target; re-run `docker compose build <service>` |
| `permission denied` on `data/` or `logs/` | Re-run `scripts/setup.sh` so `HACKATHONKIT_UID`/`HACKATHONKIT_GID` match the host user |
| Image pull denied | Check base-image access. For MinIO, run `docker compose build minio` and inspect the source download and checksum. |
| Port already in use | Find the owner (`ss -ltnp` / `netstat`), stop it or change the loopback port; never bind `0.0.0.0` to dodge a conflict |
| Healthcheck failing | Read `docker compose logs <service>`, then the service's own startup log; fix the cause, do not remove the healthcheck |
| MCP handshake fails in Docker but works from `bin/` | Compare `.mcp.json` and the launcher; regenerate with `scripts/gen-mcp-manifest.sh` |
| Docker engine unavailable | Use the documented fallback: build `bin/hackathonkit-mcp` / `bin/hackathonkit-demo-api` / `bin/hackathonkit-ctl` and state the gap |
| Postgres/MinIO unreachable | Check the DSN/endpoint env wiring; the MCP degrades to the filesystem with a warning, verify that warning |
| WSL2 errors on Windows | Confirm the WSL2 backend is enabled in Docker Desktop; re-run `scripts/setup.ps1` |

## Output Contract

- Stack inventory: files inspected, services, profiles, volumes, ports.
- Commands run with exit status and the decisive output lines.
- Changes: file, what changed, why.
- Health: build, services, handshake, `/healthz`, quality gate.
- Risks and gaps: what is unverified, which registry/engine limits apply, which secrets are still unset.
- Artifact: `DevOps report - <stack>` via `reports.create` with the above sections.
- No secret values anywhere in the answer or artifacts.

## Verification

- `docker compose config` exits 0 and every `build.target` resolves.
- `docker compose build` for the touched targets exits 0.
- `scripts/doctor.sh` passes every check that does not depend on an absent external tool.
- `scripts/smoke.sh` returns three valid JSON-RPC responses and `isError: false`.
- `quality.gate` evidence is fresh for the current revision (no stale gate).
- `reports.create` returned an `artifact_id`; `security.review` found no high-severity issue in changed stack files.
- Every remaining failure is named with its exact command and error line.

## Failure Modes

| Symptom | Action |
|---|---|
| `docker: command not found` | Fall back to `bin/` binaries, state that the container path is unverified |
| `docker compose` v1 error | Use `docker compose` (v2) syntax; the launcher requires v2 |
| `error while interpolating` in compose | Add the missing variable to `.env`; keep `.env.example` updated with an empty placeholder |
| Build runs out of context | Fix `.dockerignore`; never send `.env`, `data/`, `logs/`, `evals/` to the daemon |
| Container exits immediately | `docker compose logs <service>`; check the entrypoint binary exists and the env is complete |
| Bind mount writes fail | UID/GID mismatch; re-run setup, verify `HACKATHONKIT_UID`/`HACKATHONKIT_GID` |
| Gate image build is slow | Go module/build caches are named volumes; keep them, do not prune |
| Two services claim one port | Change the host port mapping, keep loopback, re-run `docker compose config` |
| Healthcheck passes but the feature fails | Reproduce with `docker compose run --rm -T hackathonkit-mcp` and the same request; compare with the `bin/` binary |

## Anti-Patterns

- Claiming the stack works without showing command output.
- Running Docker commands through MCP tools that cannot access the Docker socket.
- Committing `.env`, printing secret values or pasting them into reports.
- `docker compose down -v`, `docker system prune` or volume deletion without an explicit request.
- Binding services to `0.0.0.0` to avoid a port conflict.
- Deleting a healthcheck because it fails.
- Editing the runtime image to include build tools instead of using the `quality` target.
- Ignoring a stale `quality.gate` artifact and reporting an old green state.

## References

- `references/docker-review-checklist.md` - line-by-line Dockerfile and compose review checklist with the exact commands.
- `references/compose-troubleshooting.md` - symptom, cause and fix matrix for build, bring-up, health and permissions.
