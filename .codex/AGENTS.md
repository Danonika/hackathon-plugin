# Hackathon Agent Kit for Codex CLI

This supplements the root `AGENTS.md` with Codex-specific guidance. The plugin
registers the MCP server, 34 skills and three lifecycle hooks; the repository
fallback path (`.codex/config.toml` + `.agents/skills`) works without the
plugin.

## Session Start

The `SessionStart` hook injects a compact context block: live routing mode,
current plan stage, top learnings and pending eval decisions. Read it before
planning. `PostToolUse` appends tool events to `logs/session-events.jsonl`;
`SessionEnd` closes the session when the main thread ends.

## Subagents

| Agent | Use for | Sandbox |
|---|---|---|
| `researcher` | cited theme/domain research, source verification | read-only |
| `reviewer` | correctness, security and regression review from fresh context | read-only |
| `evaluator` | judge/project-cost scoring with evidence rules | read-only |

Use them when a task needs isolated judgement; the main session keeps planning
and final decisions.

## MCP and Routing

1. Call `system.context` first: it returns `routing_mode`, `routing_config_version` and the live capability list.
2. In fly mode call `routing.fly_recommend` before domain tools; validate, accept, reject or extend. Fly is a hint, never a blocker.
3. Report outcomes with `routing.fly_feedback` and durable lessons with `learning.record`.
4. Track multi-step work with `plan.create` / `plan.update` / `plan.status`.
5. Check `system.context_budget` before adding skills or tools; keep `SKILL.md` under 400 lines.

## Commands

```sh
go build ./... && go vet ./... && go test ./...
go run ./cmd/hackathonkit-mcp --selftest
go run ./cmd/hackathonkit-mcp --session-context
go run ./cmd/hackathonkit-ctl ingest
go run ./cmd/hackathonkit-ctl tui
scripts/doctor.sh
```

Commands: `/plan`, `/research`, `/build`, `/evaluate`, `/verify` (the agent runs the
skills and tools; the user only states the task or runs a command). `scripts/setup.sh`/
`repair.sh` install the prompts into `~/.codex/prompts/` via `scripts/install-prompts.sh`;
otherwise run that script once. Before claiming work is done, run `/verify` (or
`scripts/quality-gate.sh`): seven gate checks including **test coverage** (default 80%).

Windows: use the `.ps1` scripts and the Node MCP launcher.

## Skills

Skills are auto-loaded from the plugin (`plugins/hackathonkit/skills/`) or from the
`.agents/skills` mirror in fallback mode. Skill authoring rules live in
`docs/SKILL-STANDARD.md`.
