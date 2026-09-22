# Code Deletion Log

## [2026-09-22] Refactor Session

Baseline: `prior revision` (v0.13.1), Go 1.27.1. Analysis only; no production behavior
was intended to change except the documented atomic-write consolidation.

Tools and signals used:

- `go mod tidy -diff` - clean: no unused or missing dependencies.
- `deadcode -test ./...` - 0 unreachable functions before and after.
- `staticcheck ./...` - 3 findings, 2 remain (both intentional determinism
  assertions in tests, see "Retained").
- `golangci-lint run --enable=dupl,unparam,unused,ineffassign,misspell,gocritic`
  - 12 findings, 5 remain (all intentional, see "Retained"); `dupl` found no
  duplicate blocks above threshold.
- Custom `go/types` AST scanners for fields and package-level objects that
  staticcheck cannot flag (exported members of `internal/` packages).
- `shellcheck` on all shell scripts - only false positives (`CDPATH= cd` is a
  valid POSIX command prefix; `rm -rf "$dir/$skill"` guarded by a directory
  check) - no dead shell code.
- `eslint` on the Node hooks - 0 findings.
- Repository-wide search for `TODO`/`FIXME`/commented-out code - none.

### Unused Fields Removed

- `internal/tools/deps.go` - `Deps.Version` (write-only): set by
  `cmd/hackathonkit-mcp` and the tools test helper, read by no tool. The server
  version is already reported through `mcp.ServerOptions.Version` and the
  handshake (`serverInfo.version`).
- `cmd/hackathonkit-ctl/tui.go` - `tuiModel.root` (write-only): stored by
  `newTUIModel`, never read by any method. The `root` parameter was removed
  from `newTUIModel` and its 4 call sites updated; `cmdTUI` still needs
  `repoRoot()` for `openStore`.
- `cmd/hackathonkit-rehearse/main.go` - `mcpClient.stderr` (write-only): assigned
  from the process buffer, never read; the local buffer is still used for the
  `initialize` failure diagnostic.
- `internal/plans/plans.go` - `Plan.Path` field (`json:"-"`): set by `Load`
  and `Latest`, read only by a test assertion. Tool responses already compute
  the path with `plans.Path(root, slug)`.

### Unused Functions Removed

- `internal/plans/plans.go` - `SortStages()`: exported helper with zero
  production callers; the unit test was its only consumer. Removed with the
  `sort` import.
- `internal/telegram/telegram.go` - `EscapeHTML()`: exported helper with zero
  production callers; `telegram.send` passes text through with the caller's
  `parse_mode`, so the test was the only consumer.
- `internal/tools/system.go` - `toolNameIsExperimental()`: leftover predicate
  superseded by the explicit capability map in `system.capabilities`; its test
  was circular. Removed with the now-unused `strings` import.

### Unused Data Removed

- `internal/evaluation/evaluation.go` - `TechnicalCriteria`: unreferenced
  rubric slice. The technical rubric consumed by `project-cost` lives in the
  skill and `configs/evaluation.yaml`; `eval.compare` does not validate
  criterion keys against it. `JudgeCriteria` (used) was kept.

### Consolidation

- **Atomic state writes**: 5 near-identical implementations replaced by
  `internal/atomicfile.Write` (temp file in the same directory + rename):
  - `internal/plans/plans.go` (`Save`)
  - `internal/quality/quality.go` (`Save`)
  - `internal/config/config.go` (`SetRoutingMode`)
  - `internal/routing/fly/embed.go` (kept as a thin best-effort wrapper)
  - `cmd/demo-api/main.go` (`writeApproval`)
  Behavioral notes: the shared helper takes the destination mode as a
  parameter, so the consolidation preserves the previous permissions -
  `atomicfile.ModeState` (`0o600`) for plans, gate reports and plan approvals
  (least-privilege state, per the security-review checklist) and
  `atomicfile.ModeContent` (`0o644`) for `configs/routing.yaml` and the
  embeddings cache, which already set `0o644` explicitly. Cleanup on rename
  failure now also removes the temp file in `plans.Save`. New tests:
  `internal/atomicfile/atomicfile_test.go` (roundtrip, parent creation,
  overwrite, per-mode permissions, blocked parent, destination-is-directory
  cleanup, no temp leftovers) plus per-caller mode assertions in the plans,
  quality, config and demo-api test suites.
- **`internal/tools/data.go` - `loadTable`**: dropped an always-ignored first
  return value (`string`); 5 call sites in `data.go` and `timeseries.go`
  updated.
- **`internal/evaluation/evaluation.go` - `criterionMax`**: returned a
  criterion maximum that both call sites discarded; replaced by
  `criterionKnown()` returning a plain bool.

### Lint-Driven Cleanups

- `internal/tools/plan.go` - replaced an evidence-copy loop with a single
  `append(..., slice...)`.
- `internal/routing/fly/router.go` - `scores[c] = scores[c] + x` ->
  `scores[c] += x` (3 sites).
- `internal/routing/fly/mb.go` - `z = z ^ (z >> 31)` -> `z ^= z >> 31`.
- `cmd/hackathonkit-rehearse/main.go` - `fail` wrapper replaced by `fmt.Errorf`.
- `cmd/demo-api/main.go` - explicit `meta.Close()` before `os.Exit(1)` so the
  deferred recorder close is not skipped on a listen failure.

### Retained (verified, intentionally kept)

- **Test seams**: `learning.Store.WithClock`,
  `telegram.Client.WithHTTPClient` / `WithRateLimit` / `WithRetryDelay`.
  Documented seams that keep tests deterministic and offline (rate-limit,
  429-retry and clock behavior); removing them would force sleeps or fakes.
- **Read-only accessors used by tests**: `learning.Store.Path` / `Count`,
  `telegram.Client.Settings`, `mcp.Server.Registry`,
  `observability.Metrics.Snapshot`.
- **Utility API with security coverage**: `storage.Store.ArtifactPath` /
  `ListInputs` (path-traversal and symlink-escape tests).
- **Protocol/domain constants**: `mcp.CodeInternalError` (JSON-RPC code set
  completeness), `fly.MBPAMCount` / `MBPPL1Count` (FlyWire anatomy reference
  next to the used constants).
- **`fly.FeedbackEvent.InputMetadata`**: written from the documented
  `routing.fly_feedback` argument and not yet consumed by the kNN memory.
  Removing it would change a documented MCP tool argument; flagged for the
  maintainers to either wire it in or drop argument + field together.
- **Uniform handler signatures**: `registerClassification(reg, d)` and
  `cmdTUI(args)` keep unused parameters to match every sibling in their
  dispatch group.
- **`internal/tools/knowledge.go` if/else-if**: clearer than a switch here.
- **`SA4000` in `learning_test.go` / `util_test.go`**: deliberate
  determinism assertions (`ProjectKey(root) != ProjectKey(root)`), not bugs.

### Impact

- Tracked files changed: 22 (+45 / -204 lines).
- New package: `internal/atomicfile` (44 production + 92 test lines).
- Net production code reduction: ~119 lines.
- MCP tool count (37) and skill catalog (28) unchanged.
- Coverage: 88.7% against the 80% gate (baseline 88%).

### Testing

- `gofmt -l internal cmd` clean, `go build ./...`, `go vet ./...`.
- `go test ./...` - all 23 packages pass.
- `sh scripts/coverage-check.sh` - total 88.7% (target 80%).
- `sh scripts/quality-gate.sh` - ok 7/7 (gofmt, build, vet, test, coverage,
  selftest, skills); report at
  `evals/quality/prior revision/gate.json`.
- `hackathonkit-mcp --selftest` - 37 tools, handshake ok.
- `hackathonkit-mcp --validate-skills` - all skills compliant.
- Re-scan after the changes: `deadcode -test ./...` clean,
  `go mod tidy -diff` clean, golangci-lint 12 -> 5 findings, staticcheck
  3 -> 2 (both intentional).
- Not executed in this environment: the Docker stack and the Windows CI job
  (`scripts/docker-check.sh --full`, CI windows job).
