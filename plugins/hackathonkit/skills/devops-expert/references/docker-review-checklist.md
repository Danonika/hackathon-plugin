# Docker Review Checklist

Run top to bottom; each item is either proven by a command or reported as a gap.

## Files and Ownership

- [ ] `Dockerfile` inspected: stages `build`, `runtime-base`, `hackathonkit-mcp`, `demo-api`, `quality`.
- [ ] `docker-compose.yml` inspected: services `hackathonkit-mcp`, `demo-api`, `quality` (profile `quality`), `postgres`, `redis`, `minio`.
- [ ] `hackathonkit-mcp` and `demo-api` mount `./evals:/workspace/evals` so plans, decisions, rankings and gate evidence survive `run --rm`.
- [ ] `./data` is mounted, which persists `data/judge-bundles` (the default `eval.freeze_bundle` output) and `data/control`.
- [ ] `.dockerignore` excludes `.env`, `data/`, `logs/`, `evals/`, `bin/`, `node_modules/`, `.git/`.
- [ ] `.env.example` lists every `${VAR:?}` compose variable with an empty or placeholder value.
- [ ] `plugins/hackathonkit/.mcp.json` invokes `hackathonkit-mcp-launcher.sh` (or `.cmd` on Windows), not `docker` directly, so a binary-only install works.
- [ ] Shell scripts are executable: `git ls-files -s scripts/*.sh` shows mode `100755`.
- [ ] Telegram (optional): `TELEGRAM_*` values are passed only as environment variables, never baked into images; the stack can reach `api.telegram.org` when the built project needs it.

## One-Shot Check

```sh
sh scripts/docker-check.sh --full   # config, builds, readiness, selftest, quality image, smoke
```

Exit codes: 0 ok, 1 failed (diagnostics dumped), 2 Docker unavailable. The script
aligns `HACKATHONKIT_UID`/`HACKATHONKIT_GID` with the current user before starting.

## Static Validation

```sh
docker compose config >/dev/null && echo "compose config: ok"
python3 - <<'EOF'
import yaml, re
compose = yaml.safe_load(open("docker-compose.yml"))
stages = re.findall(r"^FROM\s+\S+\s+AS\s+(\S+)", open("Dockerfile").read(), re.M)
for name, svc in compose["services"].items():
    target = (svc.get("build") or {}).get("target")
    assert not target or target in stages, f"{name}: missing stage {target}"
    for vol in svc.get("volumes", []) or []:
        if isinstance(vol, str) and not vol.startswith((".", "/")):
            assert vol.split(":")[0] in compose.get("volumes", {}), f"{name}: undeclared volume"
print("targets and volumes: ok")
EOF
```

## Security and Isolation

- [ ] Published ports bind `127.0.0.1`, not `0.0.0.0`.
- [ ] Containers run as `${HACKATHONKIT_UID:-1000}:${HACKATHONKIT_GID:-1000}`, never root.
- [ ] No secret is baked into an image or a compose `environment` literal.
- [ ] `security.review` on changed `.yml`, `.env.example` and scripts reports no high-severity finding.
- [ ] Demo API keeps its loopback origin check and optional `X-HackathonKit-Token`.

## Build

```sh
docker compose build hackathonkit-mcp
docker compose build demo-api
docker compose --profile quality build quality
```

- [ ] Each build exits 0; failures show the failing stage and line.
- [ ] Runtime images stay small: no Go toolchain outside the `build` and `quality` stages.
- [ ] The `quality` target carries `git` and the Go toolchain and is used only via the profile.

## Bring-Up and Health

```sh
docker compose up -d postgres redis minio
docker compose run --rm quality
sh scripts/doctor.sh
sh scripts/smoke.sh
curl -fsS http://127.0.0.1:8787/healthz
```

- [ ] Infrastructure containers are `running`/`healthy`: `docker compose ps`.
- [ ] The quality gate exits 0 inside the image (or the reason is recorded).
- [ ] Doctor and smoke pass (`sh scripts/smoke.sh --compose` forces the container transport); three valid JSON-RPC responses, `isError: false`.
- [ ] `/healthz` reports `status: ok` and the live routing mode.
- [ ] `docker compose logs --tail=50 <service>` has no repeated error loop.

## Evidence

- [ ] `quality.gate` fresh for the current revision.
- [ ] `reports.create` artifact `DevOps report - <stack>` written with commands and outputs.
- [ ] Remaining gaps listed with the exact command that could not run.
