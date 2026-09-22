# Hackathon Agent Kit for Codex CLI

This supplements the root `AGENTS.md` with Codex-specific guidance. The plugin
registers the MCP server, 29 skills and three lifecycle hooks; the repository
fallback path (`.codex/config.toml` + `.agents/skills`) works without the
plugin.

## Session Start

The `SessionStart` hook injects a compact context block: live routing mode,
current plan stage, top learnings and pending eval decisions. Read it before
planning. `PostToolUse` appends tool events to `logs/session-events.jsonl`;
`Stop` closes the session.

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

Before claiming work is done, run `/verify` or `scripts/quality-gate.sh`: it runs the
seven gate checks including **test coverage** (default 80%) and reports each one.
`scripts/setup.sh`/`repair.sh` install the prompt to `~/.codex/prompts/`; otherwise
copy `.codex/prompts/verify.md` there once.

Windows: use the `.ps1` scripts and `hackathonkit-mcp-launcher.cmd`.

## Skills

Skills are auto-loaded from the plugin (`plugins/hackathonkit/skills/`) or from the
`.agents/skills` mirror in fallback mode. Skill authoring rules live in
`docs/SKILL-STANDARD.md`.
