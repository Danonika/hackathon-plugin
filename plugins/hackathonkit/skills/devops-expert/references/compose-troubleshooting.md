# Compose Troubleshooting Matrix

One cause at a time: reproduce, read the exact error, fix, re-run the failing
command. Never "fix" by deleting data, volumes or healthchecks.

## Build

| Symptom | Likely cause | Fix |
|---|---|---|
| `failed to solve: target stage "X" could not be found` | compose `build.target` typo or missing Dockerfile stage | align target with stages `build`, `runtime-base`, `hackathonkit-mcp`, `demo-api`, `quality` |
| `go: module lookup disabled` / checksum error | `go.sum` not copied before `go mod download`, or stale cache | keep `COPY go.mod go.sum ./` before the source copy; rebuild without cache |
| Build context is huge / slow | `.dockerignore` missing or too narrow | exclude `.env`, `data/`, `logs/`, `evals/`, `bin/`, `.git/`, `node_modules/` |
| `permission denied` copying files | bind-mounted source owned by another UID | build as the host user or fix ownership; never chmod 777 |
| `CGO`/compiler error in the runtime stage | build tool leaked into a runtime stage | build binaries in the `build` stage and copy them |

## Configuration

| Symptom | Likely cause | Fix |
|---|---|---|
| `error while interpolating ... required variable is missing a value` | `${VAR:?}` variable absent from `.env` | add it to `.env` and `.env.example`; never commit the real value |
| `Invalid interpolation format` | unescaped `$` in a compose command | escape as `$$` or quote correctly |
| Service cannot reach another | wrong hostname (use the compose service name) or wrong network | use `postgres:5432`, `minio:9000` inside compose; loopback only for host access |
| `no such file or directory: docker-compose.yml` | command run outside the repository root | `cd` to the root or pass `-f /abs/path/docker-compose.yml` |

## Runtime

| Symptom | Likely cause | Fix |
|---|---|---|
| `port is already allocated` | another process owns 8787/5432/9000 | find it (`ss -ltnp`), stop it or change the host port; keep `127.0.0.1` |
| Container exits with code 1 immediately | missing entrypoint binary or incomplete env | `docker compose logs <service>`; verify the binary path and required env |
| Writes to `data/` or `logs/` fail | UID/GID mismatch on bind mounts | re-run `scripts/setup.sh` to record `HACKATHONKIT_UID`/`HACKATHONKIT_GID` |
| Healthcheck flapping | service starts slower than `start-period`, or dependency not ready | raise `start-period`, add a dependency check; keep the healthcheck |
| MinIO source build fails | Base image, upstream archive or Go dependency download failed | Run `docker compose build minio`; check the first failing download and its checksum. |
| Docker Desktop on Windows errors | WSL2 backend disabled | enable WSL2, restart Docker Desktop, re-run `scripts/setup.ps1` |
| MCP handshake works from `bin/` but not via compose | stale `plugins/hackathonkit/.mcp.json` | `sh scripts/gen-mcp-manifest.sh` and re-run the plugin check |

## Data and Recovery

| Symptom | Likely cause | Fix |
|---|---|---|
| Artifacts missing after a restart | data lives in named volumes, not the bind mount | check `docker volume ls`; do not delete `pgdata`/`miniodata` casually |
| Need a clean slate | test data polluted | ask the user first; prefer targeted deletes over `down -v` |
| Postgres auth failure | password changed in `.env` after the volume was initialized | either restore the old password or recreate the volume with explicit user consent |

## Escalation Rules

- Stop and ask before: deleting volumes, `down -v`, `prune`, pushing images, changing a published port beyond loopback, or adding a new service.
- Report as a gap, not a failure: no Docker engine, no network access to a registry, no Windows/WSL2 available.
- Record the fix with `learning.record` when the same cause is likely to recur.
