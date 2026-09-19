# Hackathon Agent Kit

Local Codex plugin backed by a typed MCP capability layer. Codex is the main reasoning engine, Skills define domain workflows, the local MCP server exposes deterministic and AI-powered capabilities, and an experimental Fly router recommends capability routes while Codex keeps the final decision.

Status: v0.1 skeleton per System Design v7. MCP runtime, plugin package, skills, tooling and evaluation subsystem are in place; Postgres/MinIO metadata integration and the demo frontend are next.

## Requirements

- Git
- Docker (Docker Desktop on Windows; WSL2 backend enabled)
- Codex CLI (`npm install -g @openai/codex` or the official installer)
- Optional: Go 1.27+ for local development without Docker
- Optional: `OPENAI_API_KEY` for `audio.transcribe` and `vision.analyze`

Fallback without Docker: if `bin/hackathonkit-mcp.exe` (or `bin/hackathonkit-mcp`) exists, the launcher runs it directly, so the whole kit works on a machine without a running Docker engine.

## Quickstart

1. `git clone <repo> && cd hackathon-agent-kit`
2. `scripts/setup.ps1` (Windows) or `scripts/setup.sh` (Linux/macOS)
   - syncs skills into `.agents/skills` fallback mirror
   - builds the MCP image (or local binaries when Docker is unavailable)
   - starts postgres, redis, minio
   - creates `.env` from `.env.example`
3. Open Codex from the repository root.
4. Install the Hackathon Agent Kit plugin from the repo marketplace (`.agents/plugins/marketplace.json`), e.g. via the Codex plugin browser.
5. Start a **new** Codex session (plugins load at session start).
6. `scripts/doctor.ps1` (or `scripts/doctor.sh`) — checks runtime, routing config, skills mirror and runs the MCP handshake.
7. Run the demo task, then `scripts/smoke.ps1`.

Fallback/debug path (no plugin needed): the repository ships `.codex/config.toml` with the same MCP server definition and `.agents/skills` for repo-native skills.

## MCP tools

| Tool | Purpose |
|---|---|
| `system.context` | Live routing mode, config version, capability list |
| `system.capabilities` | Capability groups, experimental features |
| `routing.fly_recommend` | Experimental capability router (recommendation only) |
| `files.describe` | Type, size, shape, timestamp presence |
| `documents.ingest` / `documents.extract` / `documents.get` | Text extraction and stored artifacts (txt, md, csv, tsv, json, log, docx, xlsx) |
| `knowledge.index` / `knowledge.search` | Chunking, TF-IDF search over indexed documents |
| `data.inspect` / `data.query` / `data.analyze` | Table profiling, filters, statistics, correlations |
| `timeseries.analyze` / `timeseries.detect_anomalies` | Trend, stats, rolling z-score / IQR anomalies |
| `classification.classify` | Deterministic keyword classification |
| `charts.create` | Line/bar SVG artifacts |
| `reports.create` | Markdown/HTML report artifacts |
| `audio.transcribe` | OpenAI transcription (requires key) |
| `vision.analyze` | OpenAI vision analysis (requires key) |

Every tool returns the shared envelope: `status`, `data`, `sources`, `warnings`, `metrics`, `trace_id`.

## Normal vs Fly

- `configs/routing.yaml` holds `routing.mode` (`normal` or `fly`) and a version.
- `system.context` re-reads the file on every call, so a toggle takes effect without restarting MCP.
- `scripts`/demo API can change it: `POST /routing {"mode": "fly"}` on the demo API (`http://127.0.0.1:8787`), or `hackathonkit-mcp --set-routing-mode fly`.
- Fly instructions live in the `task-router` skill: in fly mode Codex calls `routing.fly_recommend` first, then validates, accepts, rejects or extends the recommendations. On failure or `max_score < threshold` it silently continues in normal mode.

## Development

```sh
go build ./...
go vet ./...
go test ./...
go run ./cmd/hackathonkit-mcp --selftest        # in-process MCP handshake
go run ./cmd/hackathonkit-mcp --list-tools      # tool descriptors JSON
go run ./cmd/hackathonkit-mcp --set-routing-mode fly
go run ./cmd/demo-api                       # routing toggle + file listing API
```

Sample dataset: `data/input/sample-telemetry.csv` (72 hourly readings with two planted anomalies). Try:

> Найди аномалии в телеметрии из data/input/sample-telemetry.csv, построй график и отчёт.

## Evaluation subsystem

Explicit-only skills (never triggered implicitly): `$judge`, `$project-cost`, `$roast-project`, `$cp-expert-refactoring`, `$system-design-expert`, `$self-improvement-controller`. Configuration and limits live in `configs/evaluation.yaml`; artifacts are stored per git sha under `evals/`.

## Repository layout

```
plugins/hackathonkit/          Codex plugin (single source of truth for skills)
  .codex-plugin/plugin.json
  .mcp.json
  skills/
  scripts/                 launcher (prefers bin/, falls back to docker compose)
.agents/plugins/           repo marketplace
.agents/skills/            generated fallback mirror (scripts/sync-skills)
.codex/config.toml         fallback MCP registration
cmd/hackathonkit-mcp/          MCP stdio server
cmd/demo-api/              local demo API (routing toggle, file listing)
internal/mcp/              JSON-RPC, registry, contracts, server
internal/config/           config + live routing state (atomic writes)
internal/routing/fly/      Fly router v1 (deterministic keyword + metadata scoring)
internal/tools/            capability implementations
internal/storage/          filesystem storage with path sanitization
internal/openai/           thin OpenAI adapters (chat, vision, embeddings, audio)
configs/                   config.yaml, routing.yaml, evaluation.yaml
data/input, data/artifacts, data/index, logs/
scripts/                   setup, doctor, smoke, sync-skills (ps1 + sh)
evals/                     calibration, roast, project-cost, judges, aggregate, snapshots
```

## Known limitations (v0.1)

- PDF text extraction is not enabled; convert to docx/txt or add a processor.
- XLSX support covers the first worksheet and shared strings.
- Postgres/Redis/MinIO are started by setup but metadata is still stored on the filesystem; database integration is the next milestone.
- Frontend is not implemented yet; `cmd/demo-api` provides the endpoints a UI will use.
- Fly router v1 is deterministic (keywords + input metadata). The embedding/encoder version is planned.
- Codex plugin installation is manual (repo marketplace) and requires a new session; the `.codex/config.toml` fallback works without it.

## Hackathon day

1. Define input and expected output from the task statement.
2. Reuse capabilities; add a skill and at most one or two domain processors.
3. Add a small eval set and a golden demo scenario.
4. Run `scripts/doctor.ps1` and the main scenario in both Normal and Fly modes.
5. Record real metrics only (routing latency, agreement, recall, correctness).
