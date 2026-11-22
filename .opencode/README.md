# Hackathon Agent Kit on OpenCode

Open `opencode.json` from the repository root; it registers the Hackathon Agent Kit MCP
server and loads `AGENTS.md` as portable instructions.

- The MCP command resolves the Hackathon Agent Kit checkout from the current directory and
  runs the shared Node launcher. It can start from a subdirectory.
- Skills live in `plugins/hackathonkit/skills/`; reference them from your prompt by
  name (`$task-router`, `$build-planner`, ...).
- Regenerate mirrors and verify adapters with `scripts/sync-harness.sh`.
