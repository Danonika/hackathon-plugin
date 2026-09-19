# Hackathon Agent Kit

Local Codex plugin backed by a typed MCP capability layer. Codex is the main reasoning engine, Skills define domain workflows, the local MCP server exposes deterministic and AI-powered capabilities, and an experimental Fly router recommends capability routes while Codex keeps the final decision.

Status: v0.2 per System Design v7. MCP runtime (19 tools), plugin package with scripted install, skills, PDF/XLSX processing, Fly v2 (keyword + OpenAI embeddings), Postgres/MinIO backends, demo dashboard and the evaluation subsystem are in place.

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
   - registers the repo marketplace and installs the plugin: `codex plugin marketplace add <root>` + `codex plugin add hackathonkit@hackathonkit-local`
3. Open Codex from the repository root.
4. Start a **new** Codex session (plugins load at session start).
5. `scripts/doctor.ps1` (or `scripts/doctor.sh`) — checks runtime, routing config, plugin, skills mirror and runs the MCP handshake.
6. Run the demo task, then `scripts/smoke.ps1`.

Fallback/debug path (no plugin needed): the repository ships `.codex/config.toml` with the same MCP server definition and `.agents/skills` for repo-native skills.

## MCP tools

| Tool | Purpose |
|---|---|
| `system.context` | Live routing mode, config version, capability list |
| `system.capabilities` | Capability groups, experimental features |
| `routing.fly_recommend` | Experimental capability router, keyword + embedding encoder (recommendation only) |
| `files.describe` | Type, size, shape, sheets, timestamp presence |
| `documents.ingest` / `documents.extract` / `documents.get` | Text extraction and stored artifacts (txt, md, csv, tsv, json, log, docx, xlsx with all worksheets, pdf) |
| `knowledge.index` / `knowledge.search` | Chunking, TF-IDF search over indexed documents |
| `data.inspect` / `data.query` / `data.analyze` | Table profiling, filters, statistics, correlations (XLSX sheet selection) |
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

## Demo UI

`go run ./cmd/demo-api` (or `docker compose up -d demo-api`) serves http://127.0.0.1:8787:

- Normal/Fly toggle that writes `configs/routing.yaml` atomically (MCP re-reads it live)
- Fly ranking bars, encoder, max_score and threshold from `logs/fly-events.jsonl`
- MCP tool call feed from `logs/mcp-calls.jsonl`
- Input files and generated artifacts

## Storage backends

Artifacts always land in `data/artifacts` (offline-safe). With the environment configured they are also mirrored:

- `POSTGRES_DSN` — artifact registry, routing runs and fly predictions (`artifacts`, `routing_runs`, `fly_predictions`; schema is created on connect)
- `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`, `MINIO_USE_SSL` — artifact blob mirror; reads fall back to MinIO when a local file is missing

`docker compose` passes both to the MCP and demo-api services. Without them everything degrades to the filesystem with a warning in the logs.

MinIO is pulled from `quay.io/minio/minio`: the `minio/minio` Docker Hub namespace may be unavailable (pull access denied) in some regions. The demo-api binds `HACKATHONKIT_DEMO_HOST` (default `127.0.0.1`, `0.0.0.0` inside compose).

## Development

```sh
go build ./...
go vet ./...
go test ./...
go run ./cmd/hackathonkit-mcp --selftest        # in-process MCP handshake
go run ./cmd/hackathonkit-mcp --list-tools      # tool descriptors JSON
go run ./cmd/hackathonkit-mcp --set-routing-mode fly
go run ./cmd/demo-api                       # demo dashboard + routing toggle API (http://127.0.0.1:8787)
```

Sample dataset: `data/input/sample-telemetry.csv` (72 hourly readings with two planted anomalies). Try:

> Найди аномалии в телеметрии из data/input/sample-telemetry.csv, построй график и отчёт.

## Evaluation subsystem

Explicit-only skills (never triggered implicitly): `$judge`, `$project-cost`, `$roast-project`, `$cp-expert-refactoring`, `$system-design-expert`, `$self-improvement-controller`. Configuration and limits live in `configs/evaluation.yaml`; artifacts are stored per git sha under `evals/`.

## Repository layout

```
plugins/hackathonkit/          Codex plugin (single source of truth for skills)
  .codex-plugin/plugin.json
  .mcp.json               portable MCP command: docker compose run (Windows/Linux/macOS)
  skills/
  scripts/                optional dev launchers (prefer bin/, fall back to docker compose)
.agents/plugins/           repo marketplace
.agents/skills/            generated fallback mirror (scripts/sync-skills)
.codex/config.toml         fallback MCP registration
cmd/hackathonkit-mcp/          MCP stdio server
cmd/demo-api/              demo dashboard + API (routing toggle, logs, files)
cmd/demo-api/static/       embedded dashboard page
internal/mcp/              JSON-RPC, registry, contracts, server
internal/config/           config + live routing state (atomic writes)
internal/routing/fly/      Fly router (keyword + embedding encoder, centroid cache)
internal/tools/            capability implementations
internal/storage/          filesystem storage with path sanitization + backend attach
internal/metadata/         Postgres recorder (artifacts, routing runs, fly predictions)
internal/objectstore/      MinIO client (artifact mirror, read fallback)
internal/openai/           thin OpenAI adapters (chat, vision, embeddings, audio)
configs/                   config.yaml, routing.yaml, evaluation.yaml
data/input, data/artifacts, data/index, logs/
scripts/                   setup, doctor, smoke, sync-skills (ps1 + sh)
evals/                     calibration, roast, project-cost, judges, aggregate, snapshots
```

## Known limitations (v0.2)

- Scanned PDFs without a text layer need OCR; extraction returns an explicit warning instead of guessing.
- XLSX handles multiple worksheets, shared and inline strings; formulas appear as their last cached value.
- Postgres/MinIO backends are covered by unit tests through interfaces with fakes; run `docker compose up` for a live check (Docker Desktop needs the WSL2 backend on Windows).
- The demo UI is a lightweight embedded dashboard, not the full Next.js frontend from the design.
- Fly v2 blends keyword scoring with OpenAI embedding centroids (cached in `data/index/fly-embeddings.json`); without `OPENAI_API_KEY` it silently falls back to the keyword encoder.
- Codex plugin install writes to `~/.codex`; after editing plugin files bump the plugin version and re-run setup. The `.codex/config.toml` fallback works without the plugin.

## Hackathon day

1. Define input and expected output from the task statement.
2. Reuse capabilities; add a skill and at most one or two domain processors.
3. Add a small eval set and a golden demo scenario.
4. Run `scripts/doctor.ps1` and the main scenario in both Normal and Fly modes.
5. Record real metrics only (routing latency, agreement, recall, correctness).
