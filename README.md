# Hackathon Agent Kit

Local Codex plugin backed by a typed MCP capability layer. Codex is the main reasoning engine, Skills define domain workflows, the local MCP server exposes deterministic and AI-powered capabilities, and the experimental Fly router recommends capability routes while Codex keeps the final decision.

Status: v0.16.74. MCP runtime (41 tools), 29 standard-compliant skills (validator-enforced), Go test coverage enforced at an 80% gate, session hooks, durable learnings, Codex subagents, four harness adapters, a full control plane (SQLite store, daemon, TUI, worktrees, agent runs, risk scoring, session handoff), staged build plans, PDF/XLSX processing, Fly v2 (keyword + embeddings + kNN memory + mushroom-body learning + LLM classifier), Postgres/MinIO backends, demo dashboard, install profiles/repair, a quality gate with a Go-enabled Docker target, a Fly A/B benchmark, plan review, security review, a clean-checkout rehearsal command, a resource-aware planner (API resource questionnaire, provider probes, per-track variants), a Telegram integration capability for built projects and the evaluation subsystem (deterministic aggregation, calibration, loop decisions and bundle freezing) are in place.

## Contents

- [Architecture](#architecture)
- [Requirements](#requirements)
- [Quickstart](#quickstart)
- [Build flow](#build-flow)
- [Skills](#skills)
- [Session hooks](#session-hooks)
- [Learnings](#learnings)
- [Control plane](#control-plane)
- [MCP tools](#mcp-tools)
- [Telegram integration](#telegram-integration)
- [Routing modes](#routing-modes)
- [Evaluation subsystem](#evaluation-subsystem)
- [Demo UI](#demo-ui)
- [Storage backends](#storage-backends)
- [Configuration](#configuration)
- [Observability](#observability)
- [User guide](docs/USER-GUIDE.md)
- [Plugin packaging](#plugin-packaging)
- [Harness adapters](#harness-adapters)
- [Scripts](#scripts)
- [Repository layout](#repository-layout)
- [Development](#development)
- [Testing](#testing)
- [Known limitations](#known-limitations)
- [Troubleshooting](#troubleshooting)
- [Hackathon day](#hackathonkit-day)

## Architecture

```text
Codex (reasoning, final decisions)
  |
  |-- Skills (29): workflow prompts, progressive disclosure, evidence contracts
  |     task-router, build-planner, theme-research, rag-research, document-intelligence,
  |     data-analysis, time-series-analysis, government-services, kazakh-language,
  |     report-builder, judge, project-cost, roast-project, cp-expert-refactoring,
  |     system-design-expert, self-improvement-controller, devops-expert, qa-expert,
  |     telegram-integration, output-self-check, verification-loop, skill-authoring,
  |     search-first, codebase-onboarding, context-management, git-workflow, tdd-workflow,
  |     learning-loop, security-review
  |
  |-- MCP server (stdio JSON-RPC, 41 tools, one shared envelope per call)
        system.*            context, resources, capabilities, context budget
        routing.fly_*       capability recommendation + learning feedback
        learning.*          durable trigger/insight learnings with confidence
        eval.*              deterministic evaluation, calibration, decisions, bundles
        files/documents/*   describe, ingest, extract, get
        knowledge.*         chunk, index, TF-IDF search
        data.*              inspect, query, analyze tables
        timeseries.*        analyze, detect anomalies
        classification.*    deterministic keyword rules
        telegram.*          send, validate and manage a project Telegram bot
        charts/reports.*    SVG and markdown/HTML artifacts
        audio/vision.*      OpenAI transcription and vision (need OPENAI_API_KEY)
  |
  |-- Hooks (Node 18+): SessionStart context injection, PostToolUse events, Stop handoff
  |
  |-- Control plane (hackathonkit-ctl): JSONL -> SQLite ingest, daemon API, TUI, worktrees, risk, handoff
  |
  |-- Storage: data/ (authoritative local files)
        data/index/learnings.json   durable learnings
        data/control/sessions/<id>.json     per-session state files
        data/control/control.db     control plane database (generated)
        data/worktrees/             per-session git worktrees
        Postgres  optional: artifacts, routing_runs, fly_predictions, eval_runs
        MinIO     optional: artifact blob mirror with local read fallback
```

## Requirements

- Git
- Docker (Docker Desktop on Windows; WSL2 backend enabled)
- Codex CLI (`npm install -g @openai/codex` or the official installer)
- Optional: Go 1.27+ for local development without Docker
- Optional: `OPENAI_API_KEY` for embeddings, `audio.transcribe`, `vision.analyze` and the LLM classifier in Fly

Fallback without Docker: if `bin/hackathonkit-mcp.exe` (or `bin/hackathonkit-mcp`) exists, the launcher runs it directly, so the whole kit works on a machine without a running Docker engine.

## Quickstart

1. `git clone <repo> && cd hackathon-agent-kit`
2. `scripts/setup.ps1` (Windows) or `scripts/setup.sh` (Linux/macOS)
   - syncs skills into the `.agents/skills` fallback mirror
   - builds the MCP image (or local binaries when Docker is unavailable)
   - starts postgres, redis, minio
   - creates `.env` from `.env.example`
   - registers the repo marketplace and installs the plugin: `codex plugin marketplace add <root>` + `codex plugin add hackathonkit@hackathonkit-local`
3. Open Codex from the repository root.
4. Start a **new** Codex session (plugins load at session start).
5. `scripts/doctor.ps1` (or `scripts/doctor.sh`) - checks runtime, routing config, plugin, skills mirror and runs the MCP handshake.
6. Run the demo task, then `scripts/smoke.ps1`.

Fallback/debug path (no plugin needed): the repository ships `.codex/config.toml` with the same MCP server definition and `.agents/skills` for repo-native skills.

## Build flow

```text
$task-router            mode + capability selection for every task
   -> $build-planner    staged plan: gates, timeboxes, exit evidence, rollback points
   -> $theme-research   cited brief on the theme, domain, APIs, regulations
   -> domain skills     document-intelligence, data-analysis, time-series-analysis, ...
   -> $self-improvement-controller   bounded improvement rounds against the plan
   -> $project-cost     technical readiness score (gate)
   -> $judge            Demo Day score from at least 3 valid isolated runs + regulation 9.6 ranking
   -> freeze            clean-room gate + read-only bundle
```

At S4 (hardening), code stages run `$cp-expert-refactoring` (hot paths, before/after evidence)
and then `$system-design-expert` (boundaries, contracts, failure isolation) before the stage gate.

Plans (`build-plan-<slug>`) and research briefs (`theme-research-<slug>`) are stored as artifacts; their ids feed the judge evidence bundle. The planner's mandatory scenario is the same scenario the improvement loops and the clean-room gate verify.

## Skills

Skills are prompt packages (`plugins/hackathonkit/skills/`). Implicit skills trigger from their description; explicit-only skills must be invoked by name.

| Skill | Trigger | Purpose | MCP tools |
|---|---|---|---|
| `task-router` | implicit, entry for every task | Reads live routing mode, selects capabilities, enforces the Fly protocol and evidence rules | `system.context`, `system.capabilities`, `routing.fly_recommend`, `routing.fly_feedback` |
| `build-planner` | implicit, multi-step builds | Staged plan S0 outcome -> S1 discovery -> S2 skeleton -> S3 vertical slice -> S4 hardening -> S5 evaluation -> S6 freeze, each with exit evidence and rollback point | `reports.create` (plan artifact) |
| `theme-research` | implicit, unfamiliar theme | Scoped research questions, isolated passes, primary-source evidence table with dates and confidence | host web search/fetch when available; `knowledge.*`, `documents.*`, `reports.create` |
| `rag-research` | implicit, corpus Q&A | Ingest, index, search and cite provided documents | `files.describe`, `documents.ingest/extract/get`, `knowledge.index/search` |
| `document-intelligence` | implicit | Extraction, field/structure extraction, classification, summaries, comparisons | `files.describe`, `documents.extract/ingest/get`, `classification.classify`, `reports.create` |
| `data-analysis` | implicit | Table profiling, filters, statistics, correlations, data quality | `files.describe`, `data.inspect/query/analyze`, `charts.create`, `reports.create` |
| `time-series-analysis` | implicit | Trends, rolling z-score / IQR anomalies, charts | `data.inspect`, `timeseries.analyze/detect_anomalies`, `charts.create`, `reports.create` |
| `government-services` | implicit | Procedures, requirements and deadlines grounded in an indexed regulation corpus | `documents.ingest`, `knowledge.index/search`, `reports.create` |
| `kazakh-language` | implicit | Kazakh transcription, normalization, subtitles, language quality | `audio.transcribe`, `reports.create` |
| `report-builder` | implicit | Deliverable reports, summaries and presentation text with evidence | `reports.create` |
| `tdd-workflow` | implicit | RED -> GREEN -> REFACTOR with gate evidence and plan updates | `quality.gate`, `plan.status`, `plan.update`, `learning.record` |
| `verification-loop` | implicit | Pre-claim verification: gofmt/build/vet/test/coverage/selftest/skills, mandatory scenario, artifacts (run explicitly with `/verify`) | `quality.gate`, `plan.status`, `eval.compare`, `learning.record` |
| `git-workflow` | implicit | Checkpoint commits, no secrets, worktree isolation, evidence in commit bodies | `plan.status`, `plan.update`, `system.context` |
| `devops-expert` | implicit | Dockerfile/compose configuration and diagnosis, static validation, build/up/health, stack troubleshooting with command evidence | `quality.gate`, `system.context`, `security.review`, `reports.create`, `plan.update`, `learning.record` |
| `telegram-integration` | implicit | Integrate Telegram notifications and a read-only management bot into the project being built (BotFather, alerts/digests, dedupe, allowlisted commands, Go/Node recipes) | `telegram.send`, `telegram.validate`, `telegram.manage`, `security.review`, `reports.create`, `learning.record` |
| `search-first` | implicit | Reuse-before-build: repo, catalog, registries, existing artifacts; adopt/extend/compose/build | `system.capabilities`, `system.context_budget`, `knowledge.search`, `reports.create` |
| `codebase-onboarding` | implicit | Map an unfamiliar repo into an architecture artifact with commands, modules, risks | `system.context`, `system.capabilities`, `system.context_budget`, `reports.create`, `learning.record` |
| `context-management` | implicit | Keep the context window lean: budget audit, progressive disclosure, artifact offloading | `system.context_budget`, `system.capabilities`, `reports.create` |
| `security-review` | implicit | Prompt-injection, secret, traversal and unsafe-default review with redacted findings | `security.review`, `system.context`, `learning.record` |
| `learning-loop` | implicit | Recall before repeating, record after outcomes, promote proven learnings | `learning.record`, `learning.recall`, `learning.promote`, `plan.status` |
| `skill-authoring` | explicit-only | Add or update a skill to the standard, validate and refresh mirrors | `system.context_budget`, `system.capabilities` |
| `judge` | explicit-only | At least 3 valid isolated judge runs over a frozen demo bundle, official Demo Day rubric, substitute passes and 9.6 ranking | `eval.freeze_bundle`, `eval.aggregate`, `eval.calibrate`, `eval.rank` |
| `project-cost` | explicit-only | Internal technical readiness score 0-100, evidence-only | `eval.compare` |
| `roast-project` | explicit-only | Three isolated critics (bug hunter, product skeptic, reproducibility), findings only | reads the snapshot, no tools |
| `cp-expert-refactoring` | explicit-only | Algorithmic and performance review of hot paths | `go test`, smoke handshake |
| `system-design-expert` | explicit-only | Module boundaries, contracts, failure isolation, storage, observability | smoke, routing toggle |
| `self-improvement-controller` | explicit-only | Bounded improvement rounds, rollback on regression, stop conditions | `eval.calibrate/compare/freeze_bundle` + other skills |
| `output-self-check` | explicit-only | 5-axis self-evaluation (accuracy, completeness, clarity, actionability, conciseness) with an evidence-backed scorecard artifact | `reports.create`, `learning.record` |
| `qa-expert` | explicit-only | Acceptance criteria, risk-based test matrix, exploratory charters, bug reports and release-readiness verdicts | `quality.gate`, `reports.create`, `plan.status`, `plan.update`, `security.review`, `learning.record` |

Each skill ships `agents/openai.yaml` (display name, short description, default prompt) and 1-3 references under `references/` for depth. Authoring rules live in `docs/SKILL-STANDARD.md` (150-250 line SKILL.md, required sections, trigger-rich descriptions, untrusted-content block for document-facing skills).

The standard is enforced mechanically: `hackathonkit-mcp --validate-skills` (or `scripts/validate-skills.sh`) checks frontmatter, sections, line bounds, reference links and interface metadata, exits non-zero on violations, and `go test ./internal/skills` keeps the real catalog compliant in CI.

## Session hooks

`plugins/hackathonkit/hooks/codex-hooks.json` registers three best-effort hooks (Node 18+, never blocks a session):

| Hook | What it does |
|---|---|
| `SessionStart` | Injects a compact context block: routing mode, current plan stage, top learnings, pending eval decisions |
| `PostToolUse` | Appends `{ts, type, session_id, tool}` to `logs/session-events.jsonl` for the control plane |
| `Stop` | Appends `session_end` to `logs/sessions.jsonl` and removes only that session's file under `data/control/sessions/` |

The context block comes from `hackathonkit-mcp --session-context` (local binary, then `docker compose`, then a shell fallback). Active sessions own one file each under `data/control/sessions/` (gitignored, no shared read-modify-write), so overlapping start/stop operations cannot erase each other. Events use the session id from the hook payload when present, otherwise the most recently started active session. The project root is resolved from validated candidates (explicit override, then the working directory tree, then the in-repo plugin), so a cached plugin install never writes into `~/.codex`. `node plugins/hackathonkit/scripts/hooks.test.js` verifies overlap, cached roots, unsafe ids and the Windows `.exe` lookup; `scripts/doctor.sh` runs it.

## Learnings

Durable, confidence-scored trigger/insight pairs stored in `data/index/learnings.json` (cap 500, deduplicated by trigger+insight):

- `learning.record` - record a lesson with `outcome` (`success`/`failure`/`neutral`), `scope` (`project`/`global`) and evidence. Confidence grows with successes: `0.3 + 0.6 * successes / (successes + failures + 1)`, clamped to 0.3-0.9.
- `learning.recall` - semantic recall when embeddings are available (keyword overlap otherwise), weighted `0.6 * similarity + 0.4 * confidence`; project scope includes global learnings.
- `learning.promote` - project to global once observed at least `promote_after` times (default 3) and confident enough.
- `SessionStart` injects the top `max_inject` (default 6) learnings with confidence >= `inject_min_confidence` (default 0.7). Configure in `configs/config.yaml` under `learning:`.

## Control plane

`cmd/hackathonkit-ctl` is the layer above individual agent sessions. JSONL logs stay the cross-process source of truth; the daemon replays them into SQLite (`data/control/control.db`) with per-file byte cursors, so repeated ingests never duplicate records.

```sh
hackathonkit-ctl ingest                          # one-shot replay into SQLite
hackathonkit-ctl sessions list [--limit N]       # status, risk, updated, task
hackathonkit-ctl sessions show <id>              # session + events + outputs (JSON)
hackathonkit-ctl sessions resume <id>            # paste-ready handoff block
hackathonkit-ctl daemon start|stop|status        # background ingest + loopback API
hackathonkit-ctl tui                             # live dashboard (bubbletea)
hackathonkit-ctl worktree create|list|remove     # git worktree per session
hackathonkit-ctl run --task "..." [--worktree]   # codex exec in a recorded session
```

- **Daemon API** on `127.0.0.1:8788` (env `HACKATHONKIT_CONTROL_PORT`, optional `HACKATHONKIT_CONTROL_TOKEN` via `X-HackathonKit-Token`): `/healthz`, `/api/sessions`, `/api/sessions/{id}`, `/api/events`, `/api/risk`. Logs to `data/control/daemon.log`, PID in `data/control/daemon.pid`.
- **Risk scoring** is deterministic: failed tool calls (+8, cap 40), tool panics (+15, cap 30), eval rollbacks (+25, cap 50), stalled active sessions (+10), clamped to 100, with reasons stored per session.
- **Handoff** combines the session, the latest plan stage, top learnings, recent events, outputs and pending eval decisions.
- **Worktrees** live under `data/worktrees/<slug>` on branch `hackathonkit/<slug>`; `run` spawns `codex exec --cd <dir>` when the Codex CLI is available.
- **Web view**: the demo API serves `/control` with a live sessions/risk table (reads SQLite read-only; shows a hint when the database does not exist yet).

Plans (`plan.create` / `plan.update` / `plan.status`) are stored under `evals/plans/<slug>.json` with stages S0-S6, statuses `todo|doing|done|failed|cut` and evidence references; `plan.update` bumps the revision. The demo API serves `/plan` for human review: `POST /plan/decision {"slug","decision":"approve"|"request_changes","note"}` records `evals/plans/<slug>.approval.json` (loopback origin check + optional `X-HackathonKit-Token`, same as `/routing`).

## Quality gate

`scripts/docker-check.sh` verifies the container stack end to end (config, builds, readiness, selftest, quality image, handshake) and aligns `HACKATHONKIT_UID`/`HACKATHONKIT_GID` with the current user first; the handshake is forced through Compose (`smoke.sh --compose`) so a local `bin/` binary cannot mask a broken container.

`scripts/quality-gate.sh` runs gofmt, `go build ./...`, `go vet ./...`, `go test ./...`, `scripts/coverage-check.sh` (total coverage >= `HACKATHONKIT_COVERAGE_TARGET`, default 80%), the MCP selftest and the skill validator, then stores a report. Go VCS stamping is disabled (`-buildvcs=false`) so the gate also works in containers running as root over a bind-mounted repository, then stores a report at `evals/quality/<git-sha>/gate.json` (per-check status, duration and bounded output). It exits non-zero when any check fails.

- In the container path, use the Go-enabled image: `docker compose run --rm quality` (Dockerfile target `quality`, compose profile `quality`, Go module/build caches as named volumes).
- `quality.gate` reads the latest evidence; `project-cost` and `judge` consume it instead of re-running checks.
- The gate is read-only with respect to source: it never edits files.

## Fly benchmark

`cmd/fly-bench` measures the Fly router against the deterministic keyword baseline on `configs/fly-tasks.jsonl` (10 tasks with expected capabilities) and stores `evals/fly-ab/<sha>.json`:

```sh
go run ./cmd/fly-bench            # summary
go run ./cmd/fly-bench --json     # full report
```

Metrics: recall@k against expected capabilities, top-1 agreement between baseline and full pipeline, determinism across two runs, and mean latency. With `OPENAI_API_KEY` the full pipeline uses real embeddings and the LLM classifier; without it, the hash encoder fallback is measured.

## Resource-aware planning

The harness asks once which external API resources exist, stores booleans (never secrets) in git-ignored `configs/resources.json` and picks the execution variant per task: deterministic tools when no keys are configured, provider-backed variants when they are.

```sh
go run ./cmd/hackathonkit-mcp --resources             # detected providers + saved profile as JSON
go run ./cmd/hackathonkit-mcp --resources-set 'openai_api=true,codex_pro=true,track=08'
sh scripts/setup-resources.sh                     # interactive questionnaire; keys go to .env via env-set.sh
```

- Providers: `openai_api`, `nvidia_api`, `telegram`, `postgres`, `minio` (detected from `.env`) and `codex_pro` (manual subscription flag). `system.resources` reports `detected`, `claimed` and `ready` per provider.
- `system.resources` is the agent entry point: when the profile is missing it returns `partial` with an ask-the-user warning; `system.resources_set` records the answers; claimed-but-missing keys come back as `missing[]` plus exact `scripts/env-set.sh` commands. Keys never transit chat or the MCP layer.
- `system.resources_probe` checks reachability: presence-only by default; `live: true` makes 5s `GET /models` calls (OpenAI, NVIDIA) and a Telegram `getMe`. Errors are redacted and keys never appear in the output.
- `routing.variant` maps a track's capabilities (from `configs/resource-matrix.yaml`) to the best ready variant: deterministic tools when no keys exist, OpenAI/NVIDIA-backed variants when ready. Degraded capabilities and the keys that would upgrade them come back as `degraded[]` and `missing[]`.
- `--session-context` and the SessionStart hook print a `resources:` line, so every session starts with the known profile or an explicit ask-the-user marker.

## MCP tools

Every tool returns the shared envelope: `status` (`ok` | `partial` | `error`), `data`, `sources`, `warnings`, `metrics`, `trace_id`. Read `warnings` before trusting `data`; keep `sources` for citations. The JSON-RPC response wraps the envelope in `content[0].text` and mirrors `status == "error"` in `isError`.

### System and routing

| Tool | Arguments | Returns |
|---|---|---|
| `system.context` | - | `routing_mode`, `routing_config_version`, live `capabilities` |
| `system.resources` | `task`, `track` (optional) | `profile` (booleans only), `providers[]` with `detected`/`claimed`/`ready`/`missing_keys`, `summary`, `missing[]`, `next_steps[]`, `tracks[]`; `partial` with an ask-the-user warning when the profile is missing |
| `system.resources_set` | provider booleans (`openai_api`, `nvidia_api`, `telegram`, `postgres`, `minio`, `codex_pro`), `track`, `note` | saved `profile`, refreshed `providers`, `next_steps[]` (exact `scripts/env-set.sh` commands); never accepts raw keys |
| `system.resources_probe` | `provider` (optional), `live` (false) | `results[]` with `configured`, `reachable`, `status_code`, `latency_ms`, `models`; live `GET /models` (OpenAI, NVIDIA) and `getMe` (Telegram) only when `live: true` |
| `system.capabilities` | - | capability groups (documents, rag, speech, vision, data_analysis, timeseries, classification, charts, reports, evaluation), experimental `fly_router`, tool list |
| `routing.fly_recommend` | `task` (required), `input_metadata`, `available_capabilities` | `route`, ranked `recommendations`, metrics `max_score`, `threshold`, `encoder`, `category_scores` |
| `routing.fly_feedback` | `task`, `chosen` (both required), `recommended`, `source` (`accepted`/`codex-correction`/`normal-teacher`/`fallback`), `input_metadata` | `stored`, `learned`, `memory_size` |
| `routing.variant` | `task`, `track` (defaults to the profile track), `capabilities[]` (optional override) | `track` playbook, `decisions[]` (chosen variant, reason, degraded flag, alternatives, missing keys), `summary`, `degraded[]`, `missing[]`; `partial` when capabilities fall back to deterministic variants |
| `system.context_budget` | - | per-item token estimates for tool schemas, skills, references and manifests, flags and recommendations |

### Evaluation

| Tool | Arguments | Returns |
|---|---|---|
| `eval.aggregate` | `reports[]` (repo-relative paths) or `reports_json[]`, `snapshot_sha`, `kind` (default `judges`), `persist` (default true) | `valid_judges`, `min_valid_judges`, `inconclusive`, unrounded `mean_total` + `mean_total_rounded`, `criterion_means`, `score_variance`, `high_disagreement[]`, `common_deductions[]`, `likely_questions[]`, `top_improvements[]`, `dropped_invalid[]`, `dropped_conflict[]`, `aggregate_path` |
| `eval.calibrate` | `run1[]`/`run2[]` or `run1_json[]`/`run2_json[]`, `snapshot_sha`, `min_delta`, `persist` (default true) | `calibration.retest_delta`, `calibration.noise_floor`, `calibration_path` |
| `eval.rank` | `aggregates[]` or `aggregates_json[]`, optional `commission {members, votes, grounds}`, `persist` (default true) | ordered `ranking` (9.6 tie-breaks, `tie_unresolved`, `commission`), `ranking_path` |
| `eval.compare` | `mode` (`technical`/`judge`), `candidate` (required), `best`, `target`, `noise_floor`, `mandatory_scenario_failed`, `snapshot_sha`, `persist` (default false) | `decision` (`target_reached`/`accept`/`rollback`/`stop`), `reason`, `improvement` |
| `eval.freeze_bundle` | `out` (default `data/judge-bundles`), `profile` (`full`/`technical`/`demo`), `exclude[]`, `read_only` (default true), `model` | `bundle_path`, `bundle_hash`, `git_sha`, `profile`, `files`, `bytes`, `excluded[]` |

### Learning

| Tool | Arguments | Returns |
|---|---|---|
| `learning.record` | `trigger` (required), `insight` (required), `outcome` (`success`/`failure`/`neutral`), `scope` (`project`/`global`), `evidence[]` | `id`, `confidence`, `observations`, `scope`, `created` |
| `learning.recall` | `query` (required), `limit` (5), `min_confidence`, `scope` (`project`/`global`/`all`) | ranked `matches[]` with `score` and the full learning |
| `learning.promote` | `id` (required) | `promoted`, `reason`, `learning` |

### Plans

| Tool | Arguments | Returns |
|---|---|---|
| `plan.create` | `slug` (required), `task`, `mandatory_scenario`, `stages[]` (defaults S0-S6) | `slug`, `revision`, `path`, `stages`, `summary` |
| `plan.update` | `slug`, `stage`, `status` (all required), `evidence[]`, `goal` | `revision`, `stage`, `status`, `summary` |
| `plan.status` | `slug` (optional, defaults to latest) | `plan` with `current_stage` or partial when none exists |

### Quality and security

| Tool | Arguments | Returns |
|---|---|---|
| `quality.gate` | - | latest gate evidence: `sha`, `ok`, `summary`, `failed[]`, per-check status/duration/output |
| `security.review` | `text` or `path` or `artifact_id` | `findings[]` (type, severity, line, redacted excerpt), `counts`, `clean` |

### Telegram

| Tool | Arguments | Returns |
|---|---|---|
| `telegram.send` | `text` (required), `chat_id`, `parse_mode`, `silent`, `reply_to` | `message_id`, `chat_id`, `chunks`; partial + hint when `TELEGRAM_BOT_TOKEN` is unset |
| `telegram.validate` | `chat_id` (optional) | `bot` (getMe), `chat` (getChat), `recent_chats` (getUpdates) for chat/topic id discovery |
| `telegram.manage` | `action` (`get_updates`/`set_commands`/`set_webhook`/`delete_webhook`) + per-action args | action result; https-only webhooks; no arbitrary API passthrough |

### Files and documents

| Tool | Arguments | Returns |
|---|---|---|
| `files.describe` | `path` | extension, size, rows, columns, headers, `has_timestamp`, XLSX `sheets` |
| `documents.ingest` | `path` | extracted-text `artifact_id`, chars, preview |
| `documents.extract` | `path`, `max_chars` | `text`, chars, truncated |
| `documents.get` | `artifact_id`, `max_chars`, `offset` | stored text slice |

Supported formats: txt, md, csv, tsv, json, log, yaml, xml, html, docx, xlsx (all worksheets), pdf (text layer only).

### Knowledge

| Tool | Arguments | Returns |
|---|---|---|
| `knowledge.index` | `path` or `artifact_id`, `corpus`, `chunk_chars` (800), `overlap` (100) | `index_id`, `chunk_count` |
| `knowledge.search` | `query`, `limit` (5), `corpus`, `index_id`, `index_ids`, `text_chars` (300) | ranked hits with `source`, `index_id`, `chunk_id`, `score`, `snippet`, `text` |

### Data and time series

| Tool | Arguments | Returns |
|---|---|---|
| `data.inspect` | `path`, `sheet`, `columns` | shape and `profile[]` (kind numeric/timestamp/text, missing, min/max/mean/std) |
| `data.query` | `path`, `sheet`, `columns`, `where` (`column op value`; ops `= == != > >= < <= contains`), `limit` (20, max 200) | `matched_rows`, rows |
| `data.analyze` | `path`, `sheet`, `columns` | profiles plus top correlations (abs(r) >= 0.3, n >= 5) |
| `timeseries.analyze` | `path`, `sheet`, `time_column`, `value_column`, `max_points` (120) | range, min/max/mean/std, `slope_per_hour`, downsampled `series` |
| `timeseries.detect_anomalies` | `path`, `sheet`, `time_column`, `value_column`, `method` (`zscore`/`iqr`), `window` (24), `threshold` (3.0), `limit` (100) | `anomalies[]` with timestamp, value, score, method |

Time columns accept RFC3339, `YYYY-MM-DD[ HH:MM[:SS]]`, `DD.MM.YYYY` and epoch seconds/milliseconds.

### Output and media

| Tool | Arguments | Returns |
|---|---|---|
| `classification.classify` | `text`, `categories` | `label`, `scores`, `matched_keywords` |
| `charts.create` | `title`, `chart_type` (`line`/`bar`), `series:[{label, points:[{x,y} or {t,v}]}]` | SVG `artifact_id` |
| `reports.create` | `title`, `format` (`md`/`html`), `sections:[{heading, body}]` | report `artifact_id`, preview |
| `audio.transcribe` | `path`, `language` (e.g. `kk`, `ru`) | transcript text + `artifact_id` (requires `OPENAI_API_KEY`) |
| `vision.analyze` | `path`, `prompt` | analysis text + `artifact_id` (requires `OPENAI_API_KEY`) |

Input paths are relative to `data/input`; `artifact_id` values are relative to `data/artifacts`; `eval.*` report paths are relative to the repository root. Absolute paths and `..` are rejected.

## Telegram integration

A capability for the **project being built**: product alerts, digests and a small management bot, set up and tested during the build with the `telegram.*` MCP tools. It is not a channel for Hackathon Agent Kit harness alerts.

- Configure `.env`: `TELEGRAM_BOT_TOKEN` from BotFather, `TELEGRAM_CHAT_ID`, and `TELEGRAM_ALLOWED_CHAT_IDS` for chat overrides. The token is read from the environment only and never appears in tool output, logs or judge bundles (`.env*` is excluded).
- Discover ids: send a message to the bot, then call `telegram.validate` (getMe, getChat, recent getUpdates).
- Send: `telegram.send` chunks text at 4096 characters, supports HTML parse mode and silent delivery; a `chat_id` override must be allowlisted.
- Manage: `telegram.manage` reads updates, sets the bot command list and sets/deletes an https webhook.
- Implement in the project with the `$telegram-integration` skill (implicit): BotFather setup, alert/digest patterns with dedupe and rate limits, read-only inbound commands (`/status`, `/subscribe`, `/unsubscribe`, `/help`) with an allowlist, and Go/Node recipes. The skill also covers documenting the integration in the project README, which judges score.

## Routing modes

`configs/routing.yaml` holds `routing.mode` (`normal` or `fly`) and a monotonically increasing `version`.

- `system.context` re-reads the file on every call, so a toggle takes effect without restarting MCP.
- Toggle: `POST /routing {"mode":"fly"}` on the demo API (`http://127.0.0.1:8787`; send `X-HackathonKit-Token` when `HACKATHONKIT_DEMO_TOKEN` is set) or `hackathonkit-mcp --set-routing-mode fly`.
- In fly mode the `task-router` skill calls `routing.fly_recommend` first, then validates, accepts, rejects or extends the recommendations. On error, timeout, empty list or `max_score < threshold` it silently continues in normal mode. Fly is a recommender, never a single point of failure.

Fly pipeline (each later signal attenuates earlier ones):

1. **keyword** - deterministic category keywords, `min(1, 0.3 * hits)`.
2. **embedding** - cosine similarity against category centroids built from an exemplar corpus; encoder chain is OpenAI embeddings, falling back to a deterministic 512-dim FNV hash encoder (cached in `data/index/fly-embeddings.json`).
3. **knn** - similarity-weighted votes from the nearest feedback examples in `data/index/fly-memory.json` (cap 500, deduplicated by task hash).
4. **mb** - a sparse mushroom-body network (4133 Kenyon cells, 96 MBONs) trained by `cmd/fly-train` into `data/index/fly-brain.json`; online dopamine updates from feedback.
5. **llm** - optional chat-model category classification.

Results are top-K (`routing.top_k`, default 5) with a warning when `max_score < routing.fly_threshold` (default 0.55). Feedback sources: `accepted`/`normal-teacher` reward, `codex-correction`/`fallback` punish. Offline training/rebuild:

```sh
go run ./cmd/fly-train -dataset train.jsonl [-eval eval.jsonl] [-model mb] [-kc 4133] [-sparsity 0.06] [-epochs 30]
go run ./cmd/fly-train -model knn        # rebuild memory + dopamine from logs/fly-feedback.jsonl
```

Dataset rows: `{"task": "...", "category": "..."}` (one JSON object per line).

## Evaluation subsystem

The LLM judges the work; code owns the arithmetic. Explicit-only skills: `$judge`, `$project-cost`, `$roast-project`, `$cp-expert-refactoring`, `$system-design-expert`, `$self-improvement-controller`.

Rubrics:

| Demo Day (`judge`) | Max | Technical readiness (`project-cost`) | Max |
|---|---:|---|---:|
| `value` | 25 | `case_and_functionality` | 20 |
| `result_quality` | 20 | `technical_implementation` | 25 |
| `innovation` | 15 | `readme_and_docs` | 20 |
| `scalability` | 20 | `reproducibility` | 20 |
| `presentation_demo_qa` | 20 | `reliability_and_security` | 15 |

Flow:

1. `eval.freeze_bundle` copies a read-only snapshot (`git-sha.txt`, `rubric-version.txt`, `bundle.json`, `bundle-hash.txt`) excluding `logs/`, `data/`, `evals/`, `.env*`, `bin/`, `node_modules/` and the generated skills mirror. `profile: "demo"` keeps only presentation evidence (TASK, README, architecture, demo artifacts, limitations) because Demo Day judges do not re-check code (regulation 9.4).
2. At least three valid isolated judge runs score the frozen demo bundle (no code re-check, regulation 9.4); each report needs judge id, snapshot sha, all five criteria within their max, a matching total, at least one evidence entry and reasons for every deduction.
3. `eval.aggregate` drops invalid/conflicted/foreign-sha reports (a missing report is never zero), requires `min_valid_judges` (default 3) valid reports, and computes the unrounded mean, criterion means, sample variance, top-quartile high disagreement, common deductions and likely questions. Short runs trigger substitute passes. `eval.rank` orders candidates by regulation 9.6 with a recorded three-member commission for unresolved ties.
4. `eval.calibrate` runs the same frozen snapshot twice: `retest_delta = abs(mean1 - mean2)`, `noise_floor = max(min_delta, 2 * retest_delta)`.
5. `eval.compare` applies the loop rules: technical - any decrease rolls back; judge - only a regression above the noise floor rolls back; reaching the target accepts and exits; an improvement below the noise floor stops work on that axis.

Artifacts land under `evals/<kind>/<sha>/` and, when Postgres is configured, are mirrored to `eval_runs`. See `evals/README.md`.

## Demo UI

`go run ./cmd/demo-api` (or `docker compose up -d demo-api`) serves `http://127.0.0.1:8787`:

- `/` dashboard: Normal/Fly toggle, Fly ranking bars, tool-call feed, input files, artifacts
- `/fly-viz` animated KC/MBON neuron activity and fly behaviour driven by feedback
- `/routing` `GET` mode/version, `POST {"mode":"fly"}` with loopback origin check and optional `X-HackathonKit-Token`
- `/calls`, `/fly-events`, `/fly-feedback`, `/fly-neuron-events` tail the JSONL logs (`?limit=N`)
- `/inputs`, `/artifacts` list files; `/healthz` status; `/metrics` routing mode
- `/fly-model-list`, `/fly-model/`, `/fly-brain/` serve vendored 3D assets (optional)

## Storage backends

Artifacts always land in `data/artifacts` (offline-safe). With the environment configured they are also mirrored:

- `POSTGRES_DSN` - artifact registry, routing runs, fly predictions and eval runs (`artifacts`, `routing_runs`, `fly_predictions`, `eval_runs`; schema is created on connect).
- `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, `MINIO_BUCKET`, `MINIO_USE_SSL` - artifact blob mirror; reads fall back to MinIO when a local file is missing and rehydrate the local cache. Compose drives both sides from `MINIO_ROOT_USER`/`MINIO_ROOT_PASSWORD` (mapped to the client's `MINIO_ACCESS_KEY`/`MINIO_SECRET_KEY`), so set those two in `.env`.

`docker compose` passes both to the MCP and demo-api services. Without them everything degrades to the filesystem with a warning in the logs. MinIO is pulled from `quay.io/minio/minio` because the `minio/minio` Docker Hub namespace may be unavailable in some regions.

## Configuration

`configs/config.yaml` (read at startup):

| Key | Default | Meaning |
|---|---|---|
| `openai.chat_model` | `deepseek-v4-flash` | chat/classification model |
| `openai.vision_model` | `gpt-4o-mini` | vision model |
| `openai.transcribe_model` | `whisper-1` | audio model |
| `openai.embedding_model` | `text-embedding-3-small` | Fly embeddings |
| `storage.input_dir` / `artifact_dir` / `index_dir` | `data/input` / `data/artifacts` / `data/index` | storage roots |
| `tools.default_timeout_sec` | `120` | per-tool call timeout |
| `tools.max_text_chars` | `60000` | extraction cap |
| `routing.fly_threshold` | `0.55` | recommendation confidence floor |
| `routing.top_k` | `5` | max recommendations |

`configs/evaluation.yaml` (live re-read):

| Key | Default | Meaning |
|---|---|---|
| `technical_target` / `demo_day_target` | `95` / `90` | loop targets |
| `max_technical_iterations` / `max_judge_rounds` | `2` / `2` | loop caps |
| `judges` / `roast_agents` | `3` / `3` | isolated runs |
| `parallel_judges` / `parallel_roast` | `true` | run isolation in parallel |
| `full_cleanroom_each_iteration` / `full_cleanroom_on_final_freeze` | `false` / `true` | clean-room policy |
| `finalization_reserve_minutes` | `45` | stop self-improvement reserve |
| `min_delta` | `0.5` | configured minimum delta for noise floors |

`configs/routing.yaml`: `routing.mode` + `routing.version` (see [Routing modes](#routing-modes)).

`.env` variables: `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `HACKATHONKIT_DEMO_PORT`, `HACKATHONKIT_DEMO_TOKEN`, `HACKATHONKIT_UID`, `HACKATHONKIT_GID`, `POSTGRES_PASSWORD`, `MINIO_ROOT_PASSWORD`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `TELEGRAM_ALLOWED_CHAT_IDS`; plus `POSTGRES_DSN`, `MINIO_*` and `HACKATHONKIT_REPO_ROOT` for direct runs. `.env` is loaded from the repo root without overwriting existing environment variables.

## Observability

- `logs/mcp-calls.jsonl` - one record per tool call: `ts`, `tool`, `duration_ms`, `status`, `trace_id`.
- `logs/fly-events.jsonl` - route, encoder, max_score, threshold, category scores, recommendations, task snippet.
- `logs/fly-feedback.jsonl` - task, chosen, recommended, source, memory size.
- `logs/fly-neuron-events.jsonl` - KC/MBON activation snapshots and PAM/PPL1 dopamine counters.
- stderr - structured JSON (`server_start`, `tool_call`, `tool_panic`, `metadata_*`, `objectstore_*`, `storage_backend_error`).
- Metrics counters: `mcp_tool_calls_total`, `mcp_tool_errors_total`, `mcp_tool_unknown_total`, `tool_duration_ms_total`, `fly_route_requests_total`, `fly_feedback_total`, `eval_aggregate_total`, `eval_calibrations_total`, `eval_decisions_total`, `eval_bundles_total`, `eval_reports_dropped_total`.

Every call gets a `trace_id` (`kit-...`) that flows through the envelope, logs and stored records.

## Plugin packaging

- `plugins/hackathonkit/.codex-plugin/plugin.json` - name, version, `skills: "./skills/"`, `mcpServers: "./.mcp.json"`.
- `plugins/hackathonkit/.mcp.json` - generated by `scripts/setup` from `.mcp.json.template` (replaces `{{REPO_ROOT}}` with the absolute path, not committed). It invokes the launcher, so a binary-only install works without Docker; `setup.ps1` substitutes the `.cmd` launcher.
- `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh` / `.cmd` - prefer `bin/hackathonkit-mcp[.exe]`, otherwise `docker compose run --rm -T hackathonkit-mcp`.
- `.agents/plugins/marketplace.json` - local marketplace `hackathonkit-local` pointing at `./plugins/hackathonkit`.
- `.codex/config.toml` - plugin-free fallback MCP registration via the launcher.

After editing plugin files, bump the plugin version and re-run `scripts/setup`; Codex loads plugins at session start.

## Harness adapters

The skill directory stays the single source of truth; `scripts/sync-harness.sh` (and `.ps1`) generates mirrors and verifies committed adapter manifests:

| Harness | Files | Notes |
|---|---|---|
| Codex | plugin + `.codex/config.toml`, `.codex/AGENTS.md`, `.codex/agents/{researcher,reviewer,evaluator}.toml` | full support: skills, hooks, subagents, MCP |
| Claude Code | `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`, generated `.claude/skills` | MCP command uses a repository-relative compose file |
| OpenCode | `.opencode/opencode.json`, `.opencode/README.md` | loads `AGENTS.md`, registers the MCP server |
| Cursor | `.cursor/rules/hackathonkit.mdc` | conventions and MCP usage |
| Any harness | `AGENTS.md` | portable instructions |

Install profiles: `scripts/setup.sh --profile core` excludes the explicit-only evaluation skills from the generated mirrors (the plugin itself always ships all 29). `scripts/repair.sh` regenerates `.mcp.json`, mirrors and install state; `scripts/uninstall.sh [--yes]` removes generated artifacts (dry run without `--yes`). `configs/install-state.json` records version, profile, runtime and adapters.

## Scripts

| Script | Purpose |
|---|---|
| `setup.sh` / `setup.ps1` | create `.env`, record host UID/GID, sync skills, hydrate `.mcp.json`, build images/binaries, start postgres/redis/minio, register and install the plugin |
| `doctor.sh` / `doctor.ps1` | check repo/config, Go, Codex plugin state, Docker, MCP runtime, skills mirror; run plugin-check and the MCP handshake |
| `smoke.sh` / `smoke.ps1` | send `initialize`, `tools/list`, `tools/call system.context` and validate the three JSON-RPC responses; `--binary`/`--compose`/`--auto` select the transport explicitly |
| `sync-skills.sh` / `sync-skills.ps1` | mirror `plugins/hackathonkit/skills/` to `.agents/skills/` and verify counts |
| `plugin-check.sh` / `plugin-check.ps1` | spawn the installed plugin's MCP command from a temp cwd and validate the handshake (exit 0 ok, 1 failed, 2 skipped) |
| `validate-skills.sh` | validate the skill catalog against `docs/SKILL-STANDARD.md` (exit 1 on violations) |
| `quality-gate.sh` | run gofmt/build/vet/test/coverage/selftest/skills and store `evals/quality/<sha>/gate.json` (exit 1 on failures) |
| `docker-check.sh` | staged Docker verification: compose config, builds, infra readiness, MCP selftest, quality image, compose handshake; `--full` includes the quality gate; dumps diagnostics on failure (exit 2 when Docker is unavailable) |
| `coverage-check.sh` | run the Go suite with a coverage profile and enforce `HACKATHONKIT_COVERAGE_TARGET` (default 80%) |
| `env-set.sh` | set a key in `.env` while preserving permission bits (new files are 0600) |
| `env-set.test.sh`, `smoke.test.sh` | shell regression tests for permission preservation and smoke transport selection (run in CI) |

## Repository layout

```text
AGENTS.md                  portable instructions for every harness
docs/                      SKILL-STANDARD.md + skill template
plugins/hackathonkit/          Codex plugin (single source of truth for skills)
  .codex-plugin/plugin.json
  .mcp.json               generated by scripts/setup (absolute launcher path; prefers bin/, falls back to docker compose)
  .mcp.json.template      template with {{REPO_ROOT}} placeholder
  hooks/codex-hooks.json  SessionStart / PostToolUse / Stop hook registration
  skills/                 29 skills, each with SKILL.md + agents/openai.yaml (+ references/)
  scripts/                launchers and hooks.js (session lifecycle)
.agents/plugins/           repo marketplace
.agents/skills/            generated fallback mirror (scripts/sync-skills)
.claude-plugin/            Claude Code plugin + marketplace manifests
.claude/skills/            generated Claude Code project mirror
.opencode/                 OpenCode config + README
.cursor/rules/             Cursor rules
.codex/config.toml         fallback MCP registration, web search, persistent instructions
.codex/AGENTS.md           Codex supplement
.codex/agents/             researcher / reviewer / evaluator subagents (read-only)
cmd/hackathonkit-mcp/          MCP stdio server
cmd/hackathonkit-ctl/          control plane CLI (ingest, daemon, sessions, TUI, worktrees, run)
cmd/demo-api/              demo dashboard + API (routing toggle, logs, files, fly-viz, control)
cmd/demo-api/static/       embedded dashboard pages
cmd/fly-train/             offline MB trainer + kNN memory rebuilder
internal/mcp/              JSON-RPC, registry, contracts, server
internal/atomicfile/       atomic temp-file + rename writes for local state files
internal/config/           config + live routing/evaluation state
internal/routing/fly/      Fly router (keyword, embeddings, kNN memory, MB brain, LLM)
internal/learning/         durable confidence-scored learnings (instincts)
internal/plans/            staged build plans under evals/plans/
internal/control/          SQLite store, JSONL ingest, risk scoring, session handoff
internal/telegram/         Telegram Bot API client for the telegram.* tools (token never logged)
internal/evaluation/       judge-report validation/aggregation, calibration, loop decisions, bundle freezer
internal/tools/            capability implementations
internal/storage/          filesystem storage with path sanitization + backend attach
internal/metadata/         Postgres recorder (artifacts, routing runs, fly predictions, eval runs)
internal/objectstore/      MinIO client (artifact mirror, read fallback)
internal/openai/           thin OpenAI adapters (chat, vision, embeddings, audio)
configs/                   config.yaml, routing.yaml, evaluation.yaml, install-state.json (generated)
data/input, data/artifacts, data/index, data/control, logs/
scripts/                   setup, doctor, smoke, sync-skills, sync-harness, repair, uninstall, plugin-check (ps1 + sh)
evals/                     judges, calibration, decisions, project-cost, roast, snapshots (see evals/README.md)
```

## Development

```sh
go build ./...
go vet ./...
go test ./...
go run ./cmd/hackathonkit-mcp --selftest        # in-process MCP handshake
go run ./cmd/hackathonkit-mcp --list-tools      # tool descriptors JSON
go run ./cmd/hackathonkit-mcp --set-routing-mode fly
go run ./cmd/hackathonkit-mcp --session-context # context block used by the SessionStart hook
go run ./cmd/hackathonkit-mcp --validate-skills  # skill catalog standard check
go run ./cmd/demo-api                       # demo dashboard + routing toggle API (http://127.0.0.1:8787)
go run ./cmd/fly-train -model knn           # rebuild Fly memory from feedback logs
go run ./cmd/hackathonkit-ctl ingest            # replay logs into the control database
go run ./cmd/hackathonkit-ctl sessions list
go run ./cmd/hackathonkit-ctl daemon start      # background ingest + loopback API
go run ./cmd/hackathonkit-ctl tui               # live dashboard
go run ./cmd/fly-bench                      # Normal-vs-Fly A/B evidence
go run ./cmd/hackathonkit-rehearse              # clean-checkout end-to-end rehearsal -> evals/rehearsal/
go run ./cmd/hackathonkit-mcp --resources       # detected API resources + saved profile as JSON
sh scripts/setup-resources.sh               # interactive resource questionnaire -> configs/resources.json + .env
sh scripts/quality-gate.sh                  # build/verify gate -> evals/quality/
sh scripts/sync-harness.sh                  # regenerate mirrors, verify adapters
sh scripts/repair.sh                        # regenerate missing generated artifacts
```

Sample dataset: `data/input/sample-telemetry.csv` (72 hourly readings with two planted anomalies). Try:

> Найди аномалии в телеметрии из data/input/sample-telemetry.csv, построй график и отчёт.

## Slash commands

The plugin ships five commands (canonical sources in `plugins/hackathonkit/commands/`). **You
state the task or run a command; the agent runs the skills and MCP tools for you** — a
skill's `tools:` list is executed automatically, you never name tools.

| Command | Use it to | The agent runs |
|---|---|---|
| `/plan [task]` | plan the work | `$build-planner` → `plan.create/status/update` |
| `/research [question]` | research a theme or answer from documents | `$theme-research` / `$rag-research` (+ `researcher` subagent) |
| `/build [stage]` | execute a stage | `$task-router` + the domain skill(s) + the MCP tools |
| `/evaluate` | score the demo and improve | `eval.freeze_bundle`, `$judge`, `$project-cost`, `$self-improvement-controller` |
| `/verify` | prove the work | `scripts/quality-gate.sh` (7 checks incl. coverage) |

Delivery per harness:

- **Claude Code**: declared via `"commands"` in `.claude-plugin/plugin.json`, so they travel with the plugin install.
- **OpenCode / project Claude**: `scripts/setup.sh` (via `scripts/sync-harness.sh`) mirrors them to `.opencode/commands/` and `.claude/commands/`.
- **Codex**: Codex plugins bundle skills, MCP servers and hooks but not slash commands, so `scripts/setup.sh`/`repair.sh` install them into `$CODEX_HOME/prompts/` (default `~/.codex/prompts/`) via `scripts/install-prompts.sh`; `scripts/uninstall.sh` removes them.
- **Cursor**: the rule in `.cursor/rules/hackathonkit.mdc` drives the same flow.

`/verify` runs `scripts/quality-gate.sh` (seven checks: gofmt, build, vet, test, **coverage**, selftest, skills), reads the `quality.gate` evidence and reports every check by name. `/verify --target N` overrides the coverage threshold (`HACKATHONKIT_COVERAGE_TARGET`). A failed, stale or missing gate blocks a `verified` claim.

## Testing

`go test ./...` covers the JSON-RPC handshake, notification suppression, panic containment, live routing and evaluation config re-reads, path sanitization and backend fallback, observability append/tail, judge aggregation/calibration/decisions, the bundle freezer, the eval and plan tool handlers, learning confidence/recall/promotion, the context-budget estimator, control-plane ingest idempotency, deterministic risk scoring, handoff rendering, the daemon HTTP handlers, the quality-gate store/reader, the Fly benchmark metrics, the security scanner, the Telegram client and tools (fake Bot API; no live calls in CI), the rehearsal driver, the resource profile/detection and provider probe tools, the variant planner (matrix parsing, deterministic fallbacks), and the skill-standard validator (including a catalog-wide regression test). Total coverage is 83% of statements (enforced at 80% by `scripts/coverage-check.sh`; override with `HACKATHONKIT_COVERAGE_TARGET`). Run `go run ./cmd/hackathonkit-mcp --selftest` for the end-to-end handshake and `scripts/smoke.sh` for the process-level check.

`go run ./cmd/hackathonkit-rehearse` is the clean-checkout rehearsal: it clones the repository into a temporary directory, builds the MCP server there, drives the mandatory telemetry scenario (describe, analyze, detect anomalies, chart, report, index/search, plan, learning, eval, security, context budget, resource profile/probe, gate and Telegram preconditions) over stdio JSON-RPC and stores a per-step report with durations at `evals/rehearsal/<sha>/rehearsal-<timestamp>.json`. It exits non-zero on the first failed step; `--keep` keeps the checkout, `--json` prints the report only.

## Continuous integration

`.github/workflows/ci.yml` runs on every push and pull request:

- **quality (linux)** - hooks behavior test, shell regression tests (`env-set`, `smoke`, `verify-command`), `scripts/quality-gate.sh` (gofmt, build, vet, tests, coverage, selftest, skill validator) and the clean-checkout rehearsal.
- **docker (linux)** - `docker compose config`, image builds, `--selftest` inside the container, the quality gate in the Go-enabled image, infrastructure bring-up and `scripts/smoke.sh`.
- **windows** - `go test ./...`, the hooks behavior test against the real `bin/hackathonkit-mcp.exe`, selftest and skill validation.

This is the verification path for the Docker stack and native Windows hooks, which cannot be executed in a sandbox without Docker or Windows.

## Known limitations

- Scanned PDFs without a text layer need OCR; extraction returns an explicit warning instead of guessing.
- XLSX handles multiple worksheets, shared and inline strings; formulas appear as their last cached value.
- Postgres/MinIO backends are covered by unit tests through interfaces with fakes; run `docker compose up` for a live check (Docker Desktop needs the WSL2 backend on Windows).
- Containers run as UID/GID 1000 by default; on Linux hosts with a different user ID, `scripts/setup.sh` records `HACKATHONKIT_UID`/`HACKATHONKIT_GID` in `.env` so bind-mounted `data/`, `logs/` and `configs/` stay writable.
- The demo UI is a lightweight embedded dashboard, not the full Next.js frontend from the design.
- Fly v2 blends keyword scoring with OpenAI embedding centroids (cached in `data/index/fly-embeddings.json`); without `OPENAI_API_KEY` it silently falls back to the keyword/hash encoder.
- Judge scoring itself is LLM-driven; only validation, aggregation, calibration and loop decisions are deterministic code.
- The MCP layer has no internet access: `theme-research` uses the host's web search when available and otherwise works from the provided corpus, marking external gaps explicitly.
- Codex plugin install writes to `~/.codex`; after editing plugin files bump the plugin version and re-run setup. The `.codex/config.toml` fallback works without the plugin.
- Session hooks require Node 18+ on the host (Codex CLI already needs Node). Every hook is best-effort and never blocks a session; without Node the hooks are skipped.
- `--profile core` filters only the generated skill mirrors; the Codex plugin always ships all 29 skills.
- Learning recall uses embeddings when `OPENAI_API_KEY` is set and keyword overlap otherwise; learnings never leave the local repository.
- The control plane is optional: hooks and MCP work without it. `demo-api /control` reads SQLite read-only and shows a hint until `hackathonkit-ctl ingest` or the daemon has run.
- `hackathonkit-ctl run` requires the Codex CLI on `PATH` and `git` for `--worktree`; the daemon binds loopback only and never proxies to the network.
- Docker and native Windows paths are verified by CI (`scripts/docker-check.sh --full` and the windows job); sandboxes without Docker or Windows can still run the Go tests, hooks test and quality gate.
- Coverage is measured across all packages (92% at v0.12.1). Long-running paths — daemon start/foreground, the TUI, live OpenAI/Telegram calls — are exercised through their error and offline paths rather than live runs.
- `telegram.*` tools need outbound HTTPS to `api.telegram.org`; all tests use a fake Bot API, so CI never calls Telegram. The Go/Node recipes in the skill are documentation and drift-prone: verify the current library APIs before shipping.
- Telegram is a capability for the project being built; Hackathon Agent Kit itself does not send harness alerts to Telegram.
- The `quality` Docker image carries the Go toolchain (larger than the runtime images) and is only built when the `quality` profile is used.
- Fly benchmark metrics are deterministic signals (recall, agreement, determinism, latency), not a full jury simulation; use them as judge evidence, not as a score.

## Troubleshooting

| Symptom | Fix |
|---|---|
| Plugin tools missing in Codex | Start a new session; re-run `scripts/setup.sh`; verify with `scripts/doctor.sh` |
| `docker: command not found` | Build local binaries: `go build -o bin/hackathonkit-mcp ./cmd/hackathonkit-mcp`; the launcher prefers them |
| MCP handshake fails | `go run ./cmd/hackathonkit-mcp --selftest`; check `plugins/hackathonkit/.mcp.json` exists and points at the right root |
| No embeddings / Fly falls back | Set `OPENAI_API_KEY`; the hash encoder is the intentional fallback |
| Artifacts not visible in Postgres/MinIO | Check `POSTGRES_DSN` / `MINIO_*` and the `storage_backend_error` warnings on stderr |
| Permission denied on `data/` or `logs/` | Re-run `scripts/setup.sh` so `HACKATHONKIT_UID`/`HACKATHONKIT_GID` match the host user |
| Eval run inconclusive | Fewer than 2 valid judge reports: fix the report schema (evidence, criteria, matching total) and re-aggregate |

## Hackathon day

1. Define input and expected output from the task statement.
2. Reuse capabilities; add a skill and at most one or two domain processors.
3. Add a small eval set and a golden demo scenario.
4. Run `scripts/doctor.ps1` and the main scenario in both Normal and Fly modes.
5. Record real metrics only (routing latency, agreement, recall, correctness).
