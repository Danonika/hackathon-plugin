# Hackathon Agent Kit on OpenCode

Open `opencode.json` from the repository root; it registers the Hackathon Agent Kit MCP
server and loads `AGENTS.md` as portable instructions.

- The MCP command uses `docker compose` with a repository-relative compose file,
  so run OpenCode from the repository root (or edit the command to an absolute
  path).
- Skills live in `plugins/hackathonkit/skills/`; reference them from your prompt by
  name (`$task-router`, `$build-planner`, ...).
- Regenerate mirrors and verify adapters with `scripts/sync-harness.sh`.
