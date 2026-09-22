---
name: codebase-onboarding
description: "Use when dropped into an unfamiliar repository that must be changed: map entry points, build and test commands, module boundaries, data flow and risky areas before editing. Produces an architecture map artifact titled Onboarding - <repo> with reports.create and records durable findings with learning.record. Do not use for readiness or quality scoring ($project-cost, $roast-project) or for staged build planning ($build-planner)."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: core
tools:
  - system.context
  - system.capabilities
  - system.context_budget
  - reports.create
  - learning.record
---

# Codebase Onboarding

Map an unfamiliar repository to `file:line` accuracy before changing it, then persist the map and record what was learned.

## When To Use

- Dropped into a repository with no prior context and expected to make changes.
- A new worktree, fork or session resume needs orientation before the next tool call.
- A teammate or judge asks how the project is structured, how to run it or where a feature lives.
- Before a refactor or integration that crosses module boundaries.
- Do not use for readiness or quality scoring - `$project-cost` and `$roast-project` are explicit-only; do not use for staged build planning - use `$build-planner`; do not fix what you find - record it as a risk and hand off.

## Core Principles

1. **Read before touching.** No edit, install or generated file during onboarding; the output is a map, not a change set.
2. **Commands come from a source.** Quote build and test commands from CI, Makefile or README; never guess them from convention.
3. **Breadth before depth.** Top-level shape first, then the critical path; do not read every file.
4. **`file:line` or it did not happen.** Every structural claim points to a path, and every behavior to a line; no vibes.
5. **Separate the layers.** Repository code and the MCP capability layer are different systems; label which one owns each behavior.
6. **Risks are findings, not fixes.** Record them with location and why; fixing belongs to a planned, verified change.

## Workflow

1. Call `system.context` (no arguments) to read `routing_mode` and the live capabilities; keep the `trace_id`. The repository root is the working directory the host launched in, not a tool argument.
2. Inventory the top level read-only:
   - `ls -la` and `find . -maxdepth 2 -type f | head -200`;
   - detect the toolchain from manifests: `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, `Makefile`, `Dockerfile`, `docker-compose.yml`, CI files under `.github/workflows/`.
3. Read the front door in order: `README.md`, `AGENTS.md`, `CONTRIBUTING.md`, `docs/`, Makefile targets, CI config. Copy commands verbatim with their source file; the full read order is in `references/onboarding-checklist.md`.
4. Call `system.capabilities` to separate what the MCP layer provides (documents, rag, data_analysis, timeseries, classification, charts, reports, evaluation) from what the repository implements. Note the groups and tool count; this decides which code paths matter.
5. Before opening large docs, skills or manifests, call `system.context_budget` and follow the `recommendations`. For flagged items, summarize from headings (`rg -n "^#" <path>`) instead of loading the full text.
6. Locate entry points with the host shell: `rg -n "func main|if __name__ ==|app\\.listen|module\\.exports" -g '!vendor/**'`; list CLI binaries (`cmd/*`, `bin/*`), servers, jobs and scripts with the file that starts each one.
7. Capture build and test commands from their sources (step 3). Do not execute during onboarding; if the user asks to verify, run the cheapest read-only check and record the exit code as evidence.
8. Map module boundaries: top-level directories and packages, their one-line responsibility and the import direction between them (`go list -deps ./...`, import grep). Note the allowed crossing points and where the architecture is enforced. Go specifics are in `references/onboarding-checklist.md`.
9. Trace the primary data flow end to end: input -> processing -> output as ordered `file:line` steps, including storage (`data/input`, `data/artifacts`, `configs/`) and external calls. One path - the one the mandatory scenario exercises.
10. Scan for risks with the host shell and record each with its location:
    - secrets or credentials (`.env*`, hardcoded keys) - never print secret values, name the file only;
    - `panic(`, unchecked errors, unsynchronized shared state, unbounded loops;
    - generated or vendored trees that must not be edited;
    - `TODO|FIXME|HACK` clusters and files with no tests;
    - recent churn (`git log --oneline --since="90 days"`) that suggests fragile areas.
11. Derive 3-5 first tasks: small, verifiable, each naming the owning file and the command that proves it.
12. Persist with `reports.create`: `title` `Onboarding - <repo>`, `format` `md`, sections Overview, Commands, Modules, Data flow, Risks, First tasks. Keep the `artifact_id`.
13. Call `learning.record` with `trigger` `when onboarding into <repo class>`, the `insight` (what accelerated or blocked orientation), `outcome` `success`/`failure`/`neutral`, `scope` `project` and `evidence` (artifact id, key paths).
14. State coverage: what was read, what was skipped, which commands were not executed and what remains unknown.

### Exploration Order

| Order | Target | Why |
|---|---|---|
| 1 | Manifests and CI | names the toolchain and the real commands |
| 2 | README / AGENTS.md | conventions and intent |
| 3 | Entry points | where execution starts |
| 4 | Module boundaries | where change belongs |
| 5 | Data flow | how values move and where mistakes propagate |
| 6 | Risks | what breaks or bites |
| 7 | First tasks | a safe first change |

### Evidence Format

| Claim | Evidence |
|---|---|
| Command | quoted string plus source file (CI line, Makefile target) |
| Module responsibility | directory path plus the file that establishes it |
| Behavior | `file:line` |
| Risk | `file:line` plus a one-line why |
| Coverage | directories read vs skipped |

### First Task Selection

| Task shape | Prefer | Avoid |
|---|---|---|
| Documentation fix | the module already read | rewriting docs |
| New test | the riskiest untested path | broad test refactors |
| Small bug fix | a file already understood | cross-cutting changes |
| Logging or metrics | the primary data flow | a new observability stack |

Rules: the first task should touch at most two files, be verifiable with one command, and not depend on unread modules.

## Decision Matrix

| Signal | Action |
|---|---|
| README or AGENTS.md exists | Read first and follow its conventions; note conflicts with observed code |
| Build system unknown | Name the manifests found; do not run guessed commands |
| No tests found | Record the gap and propose the smoke check that should exist |
| Generated or vendored directory | Skip content; record it as read-only in the map |
| Repository is large (>1000 files) | Breadth-first per top-level directory; sample representative files |
| Monorepo with workspaces | Map each workspace separately; do not average them |
| Several entry points | List all candidates with evidence; do not pick silently |
| Setup needs services or secrets | Record the prerequisite; do not start services or print secrets |
| Build fails on first run | Record the exact error as an environment gap; do not fix during onboarding |
| Risky area found | Record `file:line` and why; schedule it as a task, do not refactor |
| `system.context_budget` flags a doc | Summarize from headings; keep the map lean |
| MCP capability overlaps repo code | Label the layers separately in Modules and Data flow |

## Output Contract

- Artifact `Onboarding - <repo>` with sections: Overview, Commands, Modules, Data flow, Risks, First tasks.
- Overview: purpose, stack and top-level layout in a few sentences.
- Commands: exact build/test/run commands with their source; mark unverified ones.
- Modules: directory or package -> responsibility -> dependency direction.
- Data flow: the primary path as ordered `file:line` steps.
- Risks: each item with `file:line`, severity and why it matters.
- First tasks: 3-5 small tasks with owning files and verification commands.
- Coverage statement, gaps, `learning.record` id (or why none) and warnings from every envelope.

## Verification

- The report artifact exists, returned `status` != `error`, and the preview contains all six sections.
- Every module and risk claim maps to a real path; behaviors map to `file:line`.
- Every command is quoted from a source, and unexecuted commands are labelled as captured, not verified.
- The `learning.record` result is either an `id` or a stated failure; no silent skip.
- Coverage distinguishes what was read from what was skipped, including unread top-level directories.
- No repository file was modified during onboarding (working tree unchanged).

## Failure Modes

| Symptom | Action |
|---|---|
| No README or docs | Derive the map from manifests, CI and code; state the missing front door |
| `rg` unavailable | Fall back to `grep -r`/`find`; state the substitution |
| `go list ./...` fails (broken module) | Record the exact error as an environment gap; map by directory listing instead |
| Repository is too large to scan | Cap at top level plus the critical path; list unscanned areas in coverage |
| Tests need Docker, services or secrets | Record the prerequisite; do not start or fake them |
| `reports.create` fails | Deliver the six sections inline as plain text and note the persist failure |
| `learning.record` fails | Continue; state that the finding was not persisted |
| `system.capabilities` returns `error` | Retry once; on repeat map the repository without the capability split and say so |
| Git history unavailable | Use file mtimes and manifests; drop the churn signal and say so |
| Generated files look like source | Identify them by header comments, `.gitignore` and build scripts; exclude them |
| Entry point still ambiguous | Record the candidates and the evidence gap; do not invent a main path |

## Anti-Patterns

- Running installers, formatters or code generators during onboarding.
- Assuming commands because another project used them.
- Reading every file instead of the critical path.
- Refactoring risky code instead of recording it.
- Documenting trivia (every directory) while missing the primary data flow.
- Hiding unscanned areas or unexecuted commands.
- Recording assumptions as findings without a path behind them.
- Confusing the MCP capability layer with the repository's own code.
- Onboarding forever: cap the pass and hand a useful map to the first task.

## References

- `references/onboarding-checklist.md` - read order, command capture rules, Go package mapping, risk scan recipes, first-task selection and the capture template.
