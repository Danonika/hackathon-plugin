# Session handoff (review-fix campaign)

Durable state for resuming after `/compact` or in a new session. Read this and
`docs/REVIEW-BACKLOG.md` (the full findings register) before continuing.

## Where we are

- Latest release: **v0.16.15**, pushed. Previous: v0.13.0–v0.16.14.
- Scan 2 register: **408/416 fixed** (8 lows annotated reviewed-intentional).
- Scan 3 (final verification, 421 findings): all 5 criticals + 29 highs fixed
  (v0.16.14); 29 highest-value mediums fixed (v0.16.15). **Current backlog:
  174 S3 mediums + 184 S3 lows** (S3-M*/S3-L* in `docs/REVIEW-BACKLOG.md`).
- Regenerate historical findings with `ocr scan`.
- Every release passed: gofmt/build/vet, `go test ./...`, coverage ≥80%,
  quality gate 7/7, hooks + env-set + smoke tests, Windows cross-vet, and
  `go run ./cmd/hackathonkit-rehearse` (clean-checkout rehearsal) PASS 24/24.

## Verification environment

Use the Go toolchain in `go.mod`. Provider tests use isolated environment variables.

## Workflow that has been used per batch

1. Fix findings, update `docs/REVIEW-BACKLOG.md` checkboxes (`- [x] **Lnn**`).
2. `gofmt -w internal cmd`; `go build ./... && go vet ./...`; `go test ./...`.
3. `sh scripts/coverage-check.sh`; `sh scripts/quality-gate.sh` (7/7).
4. `node plugins/hackathonkit/scripts/hooks.test.js`; `sh scripts/env-set.test.sh`;
   `sh scripts/smoke.test.sh`; Windows cross build/vet.
5. Bump the four version files (`cmd/hackathonkit-mcp/main.go`,
   `plugins/hackathonkit/.codex-plugin/plugin.json`, `.claude-plugin/plugin.json`,
   `.claude-plugin/marketplace.json`), README status line, then
   `sh scripts/write-install-state.sh . full binary`.
6. Commit the changes, run `go run ./cmd/hackathonkit-rehearse` on the commit,
   then `git push origin main`.

## Remaining work

### Low batch 3 — 88 open lows (IDs from the register)

`L1-L10, L15-L18, L21-L25, L28-L29, L37-L38, L45-L46, L52-L54, L59-L61, L64,
L66-L67, L73-L82, L86-L89, L102, L104-L142`

Highlights by area:
- `cmd/`: fly-train embedding batching + deadline (L16), dead `seen` guard
  (L17), daemon ingest goroutine join (L18), TUI async queries/width floors
  (L23-L24), mcp diagnostics before external clients (L25), sessions show
  writer (L21), fly-bench task-field diagnostics (L15).
- `internal/`: plans.Latest error surfacing (L59), quality status/cancel
  (L60), activation snapshot reasons (L64), loadBrain lock scope (L66),
  router score fusion asymmetry (L67), storage ctx (L74), telegram body-limit
  detection (L75), tools context-budget ctx/truncation (L76-L77), data
  extension handling (L78-L79), argInt/argSlice semantics (L80-L81),
  capability denylist (L82), evaluation roots (L86), second-resolution
  artifact names (L87-L89), parseNumber edge (L102), slug trailing dash
  (L104), xlsx inline-string/format fixes (L105-L106).
- `scripts/` + configs: `.dockerignore`/`.gitattributes`/Dockerfile polish
  (L4-L8), plugin manifest skill count (L107), hooks manifest description
  (L108), launcher directory check + docker fallback (L109-L110), hooks stop
  cleanup (L111), coverage/docker-check POSIX details (L112-L115), doctor
  perf/parsing (L116-L120), env-set empty-file (L121), test detail args
  (L122), manifest absolute root + loop (L123-L124), repair message (L125),
  setup-resources stale keys (L126), setup profile/skills/go checks
  (L127-L129), smoke PS/sh checks (L130-L132), sync-harness context/counters
  (L133-L134), sync-skills reparse/POSIX (L135-L136), uninstall messages
  (L137-L138), validate-skills path/exec (L139-L140), write-install-state
  validation (L141-L142), compose MinIO/redis (L28-L29), metadata migrations
  (L53), MinIO bucket default (L54), codex agent cost note (L1), sandbox
  guarantees (L2), web-search mode value (L3).
- Deliberately reviewed and NOT changed (false positives / design decisions):
  - `M61` was reverted: `providerMissing` returning `EnvKeys` for unconfigured
    providers is the actionable hint the variant planner is meant to give.
  - `M53`/`M54`: compose comments explain the MinIO `service_started`
    dependency and the shared read-write volumes.
  - `L102`: the `1,5` decimal vs `1,234` thousands heuristic is documented
    behaviour covered by tests.

### After the lows

Run the final verification scan (V5): `ocr scan` full-repo as before
(excludes: `.agents/**,.claude/**,bin/**,data/**,evals/**,logs/**`), parse the
JSON, confirm the fixed set, and report any residuals by severity.

## Command cheat sheet

```sh
go build ./... && go vet ./... && go test ./... -count=1
sh scripts/quality-gate.sh
go run ./cmd/hackathonkit-rehearse
node plugins/hackathonkit/scripts/hooks.test.js && sh scripts/smoke.test.sh
```
