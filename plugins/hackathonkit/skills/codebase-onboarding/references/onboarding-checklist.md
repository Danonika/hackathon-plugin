# Onboarding Checklist

What to read first, how to map Go packages, and what to capture in the architecture map. Read this at the start of an onboarding pass.

## Read Order

| Order | File or directory | Capture |
|---|---|---|
| 1 | `go.mod`, `package.json`, `pyproject.toml`, lockfiles | toolchain, module name, dependencies |
| 2 | `README.md` | purpose, quickstart, stated commands |
| 3 | `AGENTS.md` / `CONTRIBUTING.md` / `docs/` | conventions, rules, architecture notes |
| 4 | `Makefile`, `Taskfile`, `justfile` | target names and what each really runs |
| 5 | CI config (`.github/workflows/`, `.gitlab-ci.yml`) | the commands that must pass |
| 6 | `Dockerfile`, `docker-compose.yml` | services, ports, environment variables |
| 7 | entry points (`cmd/`, `main.go`, `src/index.*`) | where execution starts |
| 8 | test files next to the code | testing style and what is covered |

If a file is missing, that absence is itself a finding: record it under Risks.

## Command Capture Rules

- Quote the command exactly as the source writes it, and name the source: `go test ./... (Makefile:test)`.
- Mark every command as `captured` or `verified` (you ran it); never blur the two.
- Do not run installers, formatters, generators or anything that writes to the working tree.
- A failing command is a finding: record the exact error, do not debug it during onboarding.
- Prefer target names over raw commands when a Makefile wraps them; the target is the interface.

## Repository Shape Detection

| Evidence | Shape | Implication |
|---|---|---|
| `cmd/*/main.go` + `internal/` | Go service or CLI | map `cmd` entry points and `internal` boundaries |
| `src/`, `package.json` | JS/TS app | map routes, components, build output |
| `pyproject.toml`, `src/` | Python package | map modules, entry points, virtualenv needs |
| `Dockerfile` + `docker-compose.yml` | containerized | services and ports are part of the map |
| multiple manifests per directory | monorepo | map each workspace separately |
| `vendor/`, `node_modules/`, `dist/` | vendored or generated | exclude from reading, record as read-only |

## Mapping Go Packages

- List packages: `go list ./...`; if it fails, use `find . -name '*.go' -not -path './vendor/*'`.
- Identify the main packages: `go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./...`.
- Boundary heuristics:
  - `cmd/<name>` - one main per binary, thin wiring only;
  - `internal/<domain>` - implementation, not importable outside the module;
  - `pkg/` - intentionally public packages;
  - `internal/tools`, `internal/mcp` - capability layer versus protocol layer.
- Import direction: `go list -f '{{.ImportPath}} {{join .Imports " "}}' ./...` shows who depends on whom; a cycle or a reverse import is a finding.
- Tests: `go test ./...` layout shows package-level vs integration tests; `_test.go` next to source means white-box tests.
- Generated code: files with `// Code generated ... DO NOT EDIT.` or a `.go` name matching `*.pb.go`, `*_gen.go`, `mock_*.go` are read-only.
- Config and data roots: `configs/`, `data/input`, `data/artifacts`, `logs/` - capture which code reads and writes each.

## Data Flow Capture

Trace one primary path, not every path:

1. **Input** - where data arrives (file, request, queue, CLI flag), and who validates it.
2. **Transform** - the functions in order, as `file:line` -> `file:line`.
3. **State** - what is stored, where (`data/`, database, memory), and for how long.
4. **Output** - artifact, response or side effect, and who consumes it.
5. **Failure path** - what happens on bad input, missing config or a downstream error.

Include external calls and their failure behavior; mark unresolved branches as gaps.

## Risk Scan Recipes

| Risk | Command | Record |
|---|---|---|
| Secrets | `rg -n "api[_-]?key|secret|password|token" -g '!vendor/**'` | file only, never values |
| Panics / escapes | `rg -n "panic\\(|log\\.Fatal|os\\.Exit"` | `file:line` and trigger |
| Concurrency | `rg -n "go func|sync\\.|chan "` | shared-state hotspots |
| Generated code | `rg -n "Code generated .* DO NOT EDIT"` | read-only trees |
| Debt clusters | `rg -c "TODO|FIXME|HACK" | sort -t: -k2 -n -r | head` | hottest files |
| Untested paths | `rg -L "_test\\.go$" --files` vs source dirs | packages without tests |
| Churn | `git log --oneline --since="90 days" --name-only | sort | uniq -c | sort -rn | head` | fragile areas |

Severity guide: exposed secret or data loss (high), crash on the critical path (high), missing tests around the critical path (medium), debt clusters elsewhere (low). Keep one line of why per risk.

## First Task Selection

Pick tasks that answer a real question in the map and can be verified quickly:

- add a missing test around a medium/high risk on the critical path;
- fix a small bug in a file already understood;
- add a log or metric at a failure boundary;
- improve a command or document the exact command that was missing.

Avoid: refactors, dependency upgrades, cross-cutting changes and anything that touches generated code.

## Capture Template

The report sections map to the pass as follows:

| Section | Source in this checklist |
|---|---|
| Overview | Shape Detection + entry points |
| Commands | Read Order (CI, Makefile) + Command Capture Rules |
| Modules | Mapping Go Packages (or the language equivalent) |
| Data flow | Data Flow Capture |
| Risks | Risk Scan Recipes |
| First tasks | First Task Selection |

End the capture with coverage: directories read, directories skipped, commands not run.

## Anti-Patterns

- Reading the whole tree instead of the critical path.
- Trusting a README command that CI does not run.
- Recording a risk without a location.
- Running package installs or generators "to understand the build".
- Leaving coverage implicit: say what was not examined.
