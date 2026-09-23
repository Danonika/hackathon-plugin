# OCR Backlog (full scan)

Source: `ocr scan` - 420 findings across 126 files.

Workflow: fix one at a time, commit each, push, then re-run OCR against the commit. `[ ]` = open, `[x]` = fixed.

Progress: **24 of 359** fixed (all criticals + all highs). Commits v0.16.32..v0.16.56.

- Actionable: **359** | Skipped (fly frontend being replaced): **61**

## CRITICAL

- [x] **OCR-001** `scripts/repair.sh:50` _bug_
  `mktemp` creates files with mode 0600, and when `go build -o <file>` writes into an *existing* file it does not change that file's mode. The three `mv` calls therefore install binaries with mode 0600, i.e. without the execute bit. Consequences: the `[ ! -x ... ]` guard at the top stays true on every subsequent run (permanent rebuild loop), and the MCP/demo runtime cannot be executed. Add an explicit `chmod` before (or after) the move.

- [x] **OCR-002** `scripts/sync-skills.ps1:127` _bug_
  Cleanup in `sync-skills.ps1` deletes artifacts without verifying ownership: the lock is removed unconditionally in `finally` (it may have been evicted and re-acquired by another run), and the orphan sweep deletes every `.tmp.*`/`.old.*`/`.lock.stale.*` directory by name only (the embedded PID is ignored), so a concurrent instance's in-progress temp directory or live-mirror backup can be destroyed. Record an owner token inside the lock and restrict cleanup to artifacts this run owns (or skip names whose PID is a live process).

## HIGH

- [x] **OCR-003** `.codex/agents/evaluator.toml:15` _security_
  `network_access` is not a documented top-level Codex config key — network access is configured inside the `[sandbox_workspace_write]` table. At top level (and with `sandbox_mode = "read-only"`) this key is likely an unknown field that is silently ignored by the parser (or rejected by the schema). If ignored, the header comment's claim that "network access is disabled explicitly" is false and the agent can still reach the network while ingesting attacker-controllable artifacts. Verify against the config schema; if the key is not honoured in read-only mode, remove the false claim and document the residual risk instead of asserting an explicit denial. The same pattern appears in both `evaluator.toml` and `reviewer.toml`.

- [x] **OCR-004** `.codex/config.toml:27` _bug_
  `git rev-parse --show-toplevel` is evaluated in the Codex process cwd, but per this file's own comment Codex loads this config from any subdirectory. The `|| echo .` fallback then resolves to `<cwd>/../../..` — a directory unrelated to the repo root. Because the launcher `exec`s whatever binary it finds at the derived root, an unresolved root is also an arbitrary-code-execution vector (see H31/M10 in docs/REVIEW-BACKLOG.md). Pin an absolute repo root the way the generated `plugins/hackathonkit/.mcp.json` does, or make the launcher fail clearly when the root cannot be resolved.

- [x] **OCR-005** `.dockerignore:4` _security_
  `.env*` is anchored at the build-context root: in `.dockerignore`, `*` does not cross `/` and patterns are matched against the full path relative to the context root (that's why Docker's own docs use `*/temp*` and `*/*/temp*`). So nested secret files such as `plugins/x/.env`, `cmd/api/.env.local` or `web/.env.production` are NOT excluded and will be shipped to the builder through the `COPY . .` step. The presence of `!**/.env.example` right below shows nested env files were intended to be covered. Use `**/.env*` so all depths are matched.

- [x] **OCR-006** `cmd/fly-train/main.go:165` _bug_
  The feedback rebuild wires the router differently from every live call site: `cmd/hackathonkit-mcp/main.go:279` (and `cmd/fly-bench/main.go:111`) use `WithEmbeddings(fly.NewChainEmbedder(oa, fly.NewHashEncoder(512)), …, cfg.OpenAI.EmbeddingModel+"+hash512")`, while this path passes the raw `*openai.Client` and the bare model name. Two consequences:
  
  1. `AddFeedback` stamps the memory file with `r.modelTag` (= `cfg.OpenAI.EmbeddingModel`), but the live router compares `mem.Model != r.modelTag` before using a stored vector (`memoryCategoryScores`) and prunes vectors whose model tag differs. Every vector written by this rebuild is therefore treated as stale/incompatible by the running router — the rebuild reports success but contributes no knn signal (and is wiped on the next live feedback).
  2. The chain/hash fallback embedder built above (lines 46-52) is never used on this path, and `AddFeedback` silently ignores embed errors (`memory.go:197`). Without `OPENAI_API_KEY` every embed fails, so all examples are persisted without vectors while the command still exits 0 with `processed N feedback tasks`.
  
  Pass the same `fly.Embedder` (and the `+hash512` tag) that the live router uses, e.g. change the parameter to the chain embedder built in `run`.

- [x] **OCR-007** `cmd/hackathonkit-ctl/tui.go:64` _bug_
  `gen` is dropped on the Sessions-failure path: the returned `loadedMsg` has `gen == 0`, so the supersession check in `applyLoaded` (`msg.gen != 0 && msg.gen < m.gen`) never discards it. A stale/failing in-flight load can therefore overwrite `m.err` even after a newer load already succeeded (e.g. an old request times out and clobbers the fresh result, blanking the view). Carry `gen` on the error path and compare unconditionally — the initial load uses `gen == 0` and `m.gen == 0`, so `msg.gen < m.gen` still lets it through.

- [x] **OCR-008** `internal/learning/learning.go:430` _bug_
  The unlock callback removes `lockPath` unconditionally, without verifying that this process still owns it. If the lock was reclaimed as stale by another process (which then created its own lock file) while this process was still in its critical section, `unlock()` deletes the *rival's* lock. Two writers can then run load-modify-save concurrently and lose an update. The lock file already records the owner PID on creation — use it to make the removal conditional (e.g. read the file back and only remove when it still contains this PID/token).

- [x] **OCR-009** `internal/learning/learning.go:436` _bug_
  Stale-lock reclamation is based solely on the lock file's mtime, which is never refreshed while the lock is held. Any writer whose load-modify-save takes longer than 30s (large store, slow disk/NFS, suspended process) has its lock deleted by a peer, so two processes can enter the critical section simultaneously. The owner PID written into the lock is available but never consulted — check that the recorded process is actually gone (or refresh the mtime via a heartbeat) before reclaiming.

- [x] **OCR-010** `internal/routing/fly/embed.go:152` _bug_
  When every centroid mismatches the task-vector dimension, the function returns `false` but leaves `r.centroids` populated, so the embedding signal is permanently disabled for the process lifetime and the poisoned cache is never rebuilt.
  
  Reachability: the production `ChainEmbedder` returns 512-dim `HashEncoder` vectors when the primary (e.g. 1536-dim OpenAI) fails, and 1536-dim vectors once it recovers — under a single shared `modelTag`. If the exemplar batch ever falls back, `buildCentroids` persists a self-consistent `Dims: 512` cache that `cacheDimsMatch` happily accepts (both on load and on later restarts). Subsequent task vectors are 1536-dim, every centroid is skipped here, `sims` stays empty, and `ensureCentroids` keeps returning the bad cache forever (it is never cleared), so routing silently loses the embedding signal with no recovery path.
  
  Consider invalidating the cached centroids when all of them are dimension-mismatched (and not re-accepting the on-disk cache) so a rebuild can happen.

- [x] **OCR-011** `internal/routing/fly/hash_encoder.go:101` _bug_
  The fallback result is returned without validating its length, unlike the primary path which explicitly rejects `len(vectors) != len(texts)`. A misbehaving fallback therefore silently yields a vector/label count mismatch that the primary contract forbids; callers such as `embed.go` zip vectors with their inputs, so this can drop or misalign records without any error. Apply the same check here.

- [x] **OCR-012** `internal/tools/data.go:107` _bug_
  The prefix/suffix fallback contradicts its own documented invariant. `strings.HasSuffix(lower, needle)` matches ANY header ending in the needle, so a request for column `id` still silently binds to an unrelated header such as `grid` — exactly the case the comment claims to prevent. Also, when one header matches by prefix and a *different* header matches by suffix, the code returns `prefixMatch` (or falls through to `suffixMatch`) instead of refusing the ambiguity, so a column that is genuinely ambiguous is selected silently. Only matches where prefix and suffix point at the same column should be accepted; a distinct-header prefix/suffix pair should yield -1.

- [x] **OCR-013** `scripts/doctor.ps1:176` _bug_
  Both the `scripts\plugin-check.ps1` invocation and the `scripts\smoke.ps1` MCP-handshake check reset `$global:LASTEXITCODE = 0` immediately before the call and then trust `$LASTEXITCODE` (the plugin check also lacks a `Test-Path` guard, and the smoke `$null -eq $smokeExit` fallback is dead because the pre-reset guarantees a non-null value). A missing script or a script that throws/early-returns without calling `exit` therefore leaves the stale `0` and the check silently passes. Add a `Test-Path` guard (as used for other helpers) and/or capture `$?` right after the invocation (or drop the pre-reset) so failures are detected.

- [x] **OCR-014** `scripts/doctor.sh:35` _bug_
  `set -e` (line 2) makes an unguarded assignment-from-command-substitution fatal: if `python3` exits non-zero (import error, killed, unexpected interpreter failure), the whole doctor script aborts here with python's status and every remaining check is silently skipped. All other substitutions in this file are protected (`|| true`, `|| echo ...`), only this one is not. Guard it so a parse failure degrades to the `unknown`/`missing` state instead of killing the run.

- [x] **OCR-015** `scripts/e2e-fly.ps1:21` _bug_
  `Call`/`Mcp` blindly dereference `.result.content[0].text` and re-parse it, assuming every response is a well-formed success envelope. A JSON-RPC error response (which has `.error` and no `.result`) or a malformed/empty output yields `$null` for `$env`, and all downstream `$ctx.status`/`$ctx.data...` accesses silently evaluate to `$null`, producing confusing "FAIL ... mode=" results instead of a clear diagnostic. Validate the response (check for `.error`, non-empty `.result.content` and `.text`) and emit an explicit error/exit when the transport-level call fails.

- [x] **OCR-016** `scripts/e2e-fly.ps1:69` _test_
  The PAM and PPL1 assertions both use `-ge` (>=) while their labels claim the counter was *incremented*. If the reward/feedback or correction/pain path is broken and the counter never advances, these checks still pass (0 >= 0), and when `fly-brain.json` is absent both values default to 0 so the comparison is trivially true. Use a strict `-gt` comparison (with a meaningful failure detail) for both the 'PAM incremented (reward)' and 'PPL1 incremented (pain)' checks so a non-incrementing counter is actually detected.

- [x] **OCR-017** `scripts/env-set.sh:103` _bug_
  The stale-lock reclamation is not atomic with respect to acquisition, contradicting the comment. `lock_is_stale` is evaluated once, then the lock is claimed with a separate `mv`. Two contenders A and B can both observe the same stale lock: A renames/remove it and successfully `mkdir`s the lock (writes its pid), then B (which already decided it was stale) performs `mv "$LOCK" "$LOCK.stale.$$"`, renaming A's *live* lock away and deleting it, after which B's `mkdir` also succeeds. Both processes then believe they hold the lock and the losing writer's `mv "$TMP" "$FILE"` silently clobbers the winner's output. Additionally, `kill -0 "$holder"` can report a dead lock as live due to PID reuse, causing a false 'another writer holds' abort. Re-verify the lock is still stale immediately before the rename (or make the claim conditional on the pid file still matching), and re-check staleness inside the acquisition loop.

- [x] **OCR-018** `scripts/env-set.sh:134` _security_
  `enc()` only wraps a value in quotes when it matches `[ \t#"'$`\\\r\n]`. Values that contain shell metacharacters but none of those characters (e.g. `;`, `&`, `|`, `<`, `>`, `(`, `)`, `*`, `?`, `!`, `{`, `}`, `~`) are emitted completely unquoted. For a value like `hello;malicious` the resulting line `KEY=hello;malicious` executes `malicious` when the .env file is later sourced (`. .env`). Values such as `a&&b` or `a|sh` are equally dangerous. Since the file exists precisely to be sourced, this is a command-injection vector. Use a default-deny test: quote unless the value consists solely of a known-safe character set.

- [x] **OCR-019** `scripts/plugin-check.ps1:100` _bug_
  False-PASS risk in exit-code capture: `$exitCode` is initialised to 0 and is only overwritten when the side file exists *and* contains a non-empty, parseable value. If the job completes but `Set-Content` silently fails (`-ErrorAction SilentlyContinue`), or `$LASTEXITCODE` is `$null` (e.g. the plugin command resolves to a script that never sets it), the file is missing/empty and the check wrongly reports success. Also `[int]$captured` throws a terminating error on any non-numeric content, aborting the script without the intended FAIL message. Treat a missing/empty/unparseable exit code as a failure and use a non-throwing parse.

- [x] **OCR-020** `scripts/plugin-check.sh:128` _security_
  The tree-containment validation only runs for absolute commands. A relative `command` is merely `os.path.join`ed and executed as-is, with no `realpath` resolution and no prefix check. A manifest containing `"command": "../../evil"` (or `./bin/mcp` where `bin` is a symlink out of the tree) therefore resolves outside the plugin tree and is still executed, defeating the guard added for the absolute case. Apply the same realpath + containment check to the relative branch.

- [x] **OCR-021** `scripts/setup-resources.sh:111` _bug_
  Incomplete credentials are still claimed. `resources.defs` (internal/resources/resources.go:31) declares MinIO as `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`, but only the endpoint is ever prompted/stored. Answering "yes" here writes a partial set to .env and records `minio=true`, so `configs/resources.json` advertises a provider that cannot authenticate (it will always report `missing_keys`) — the exact invariant the block comment at the top claims to guarantee. Collect all required keys, or downgrade `MINIO` when any of them is skipped.

- [x] **OCR-022** `scripts/sync-skills.ps1:60` _bug_
  Two problems here: (1) `[int]$holder` performs a conversion that throws on malformed pid content; because it runs inside the `catch` block, the exception is not caught by that catch and propagates under `$ErrorActionPreference = "Stop"`, aborting the script while an unowned/partial lock state is left behind. (2) When the pid file is absent or unreadable (its `Set-Content` uses `SilentlyContinue`, so a live run may legitimately have no pid file) the code falls back to the 60s age check, which *will evict a slow-but-live copy* — the exact scenario the comment above claims to prevent. Also note that a dead holder's PID can be reused by an unrelated process, in which case the lock is never evicted and the acquire loop spins until failure. Parse defensively and avoid age-only eviction of a lock that this process may still hold.

- [x] **OCR-023** `scripts/uninstall.ps1:69` _bug_
  The `& install-prompts.ps1 remove` invocation is unguarded. If the child script is missing or throws, `$ErrorActionPreference = "Stop"` turns it into a terminating error that aborts the whole uninstaller (leaving targets and codex state partially removed). Worse, `install-prompts.ps1` reports failures via `exit 1`, which does NOT throw — it just sets `$LASTEXITCODE`, so a failed prompt removal is silently ignored and never counted in `$Failures`. Wrap the call in a Test-Path + try/catch and fold the result into failure accounting (initialise `$Failures` before this block so it can be incremented here).

- [x] **OCR-024** `scripts/verify-command.test.sh:37` _test_
  `grep -q 'pattern' file1 file2 file3` succeeds if ANY of the three files contains the phrase, so this check passes even when only plan.md has the instruction and research.md/build.md do not — contradicting its own "commands state the agent runs the tools" label. Assert each command independently (loop over the files) so a missing instruction in one file is actually caught.

## MEDIUM

- [ ] **OCR-025** `.claude-plugin/marketplace.json:3` _bug_
  The `owner` object only declares a `name`. The Claude Code marketplace schema expects owner identification to include an `email` alongside `name`; a marketplace manifest without it can be rejected/incompletely registered by `claude plugin marketplace add`. Note also that none of the repo's validators would catch this: `scripts/sync-harness.sh` / `scripts/doctor.sh` only check that the JSON parses, not its schema. Add the owner email (or confirm the field is genuinely optional for the targeted CLI version).

- [ ] **OCR-026** `.claude-plugin/plugin.json:29` _bug_
  The MCP entrypoint hard-codes the POSIX launcher `hackathonkit-mcp-launcher.sh`. The repo explicitly ships a Windows counterpart (`plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.cmd`) and the generated runtime manifest selects it per-platform (`scripts/gen-mcp-manifest.ps1` substitutes `{{MCP_LAUNCHER}}` with the `.cmd`). Because this static plugin manifest cannot do that substitution, a Windows Claude Code install will try to spawn the `.sh` directly and fail to start the `hackathonkit` MCP server even though a working `.cmd` launcher exists. This is the previously logged M1/S3-M1 regression (`docs/REVIEW-BACKLOG.md:66`, `:466`) that is marked fixed but still present. Make the entrypoint platform-neutral (e.g. a small node/sh dispatcher that picks `.cmd` on Windows) or point `mcpServers` at the generated `.mcp.json`, which is produced with the platform-correct launcher.

- [ ] **OCR-027** `.codex/agents/evaluator.toml:14` _security_
  The comments and instructions overstate containment. `sandbox_mode = "read-only"` only blocks filesystem writes; read/process execution is still allowed. `approval_policy = "never"` merely removes the approval/escalation prompt and does not deny command execution. The instruction saying containment "depends on this, not on the sandbox" also contradicts the header claim that command execution and network are denied by configuration. Reconcile the header and instructions to state the residual reliance on prompt compliance (or actually constrain execution). The same issue appears in both `evaluator.toml` and `reviewer.toml`.

- [ ] **OCR-028** `.codex/agents/researcher.toml:9` _security_
  `sandbox_mode = "read-only"` restricts filesystem writes but does not itself deny process execution or network access. Unlike the sibling `reviewer.toml` (which adds `approval_policy = "never"` and `network_access = false` for exactly this reason), this agent ingests attacker-controllable content (fetched pages, documents, MCP snippets), so its anti-injection rule currently rests on prompt compliance alone. An embedded instruction could still drive a shell command or outbound request. Add an explicit approval/network policy here (at minimum `approval_policy = "never"`; decide network explicitly given `web_search` is disabled at repo level).

- [ ] **OCR-029** `.codex/agents/researcher.toml:10` _bug_
  This caveat (MCP runs outside the sandbox, so `reports.create`/`learning.record` may persist state) lives only in a TOML comment, which is never surfaced to the model. Combined with the declared read-only sandbox, the agent may conclude it must not persist anything and skip `reports.create`, silently failing the workflow's persistence step and the required `theme-research-<slug>` artifact id. Move this rule into `developer_instructions` (as `reviewer.toml` does for its read-only semantics), e.g. state that the sandbox is read-only for its own commands while the Hackathon Agent Kit MCP server persists the brief/learning evidence.

- [ ] **OCR-030** `.codex/agents/reviewer.toml:24` _security_
  The untrusted-content rule covers prompt injection but says nothing about secrets, while `what_breaks`/`repro` will quote attacker-controllable evidence (test output, gate reports, artifact text) that can contain credentials. Unlike the researcher and evaluator profiles, there is no instruction to redact secrets. Also state explicitly that embedded instructions must not alter the output contract itself (only the contract above defines the emission format), and that MCP/tool envelope output is included in "untrusted content".

- [ ] **OCR-031** `.codex/config.toml:3` _maintainability_
  The placement of `web_search` and `persistent_instructions` before the first `[table]` header is load-bearing for TOML semantics but is enforced only by this comment. A future edit that moves either key below `[mcp_servers.hackathonkit]` silently folds it into that table and the global setting disappears with no parse error. Consider a CI check that asserts these root-level keys remain table-free, so the invariant is machine-verified rather than relying on developer discipline.

- [ ] **OCR-032** `.codex/config.toml:15` _bug_
  `persistent_instructions` unconditionally tells the agent to call `system.context` and to "never claim a result without a tool result", but the MCP server it depends on cannot start on Windows (no `sh`) without a manual `scripts/setup.ps1` run. On such installs the agent receives instructions to use an unavailable tool with no guidance that it is missing, producing confusing behavior rather than a clear failure. The Windows path should be made automatic (platform-selected `.cmd` launcher) or the instruction should state how to detect the server's absence.

- [ ] **OCR-033** `.codex/config.toml:22` _bug_
  Windows correctness depends entirely on a manual out-of-band step: `sh` does not exist there, so the server fails to register unless the user runs `scripts/setup.ps1` or hand-edits `command`/`args`. Since this checking-in file is static, it bypasses the platform-aware generation already present in `scripts/gen-mcp-manifest.ps1`/`.ps1:36` (which substitutes `{{MCP_LAUNCHER}}` with `hackathonkit-mcp-launcher.cmd`). Prefer generating this entry the same way as `plugins/hackathonkit/.mcp.json`, or make the launcher platform-neutral, so the MCP server is not silently unregistered on Windows.

- [ ] **OCR-034** `.codex/config.toml:26` _bug_
  Nesting `$(git rev-parse --show-toplevel ...)` inside the outer double-quoted `args` value means the resolved path is substituted directly into the inner shell's command word. A repo path containing spaces, `$`, backticks, or quotes will word-split or undergo further expansion inside the inner `sh`, launching the wrong file or failing. Pass the root as a separate positional argument instead of interpolating it into the command string.

- [ ] **OCR-035** `.codex/config.toml:26` _maintainability_
  Redundant double `sh` wrapper: `command = "sh"` + `args = ["-c", "exec sh ..."]` spawns `sh -c` only to spawn another `sh`, while the launcher already carries its own shebang. The extra layer exists solely to resolve the path (finding above), and any failure inside it surfaces only as a raw shell error on the MCP stdio channel. Point `command` at the launcher directly with an absolute path once the root is resolved.

- [ ] **OCR-036** `.dockerignore:9` _security_
  `.dockerignore` only covers the root dirs and `.env*`, missing many patterns that `.gitignore` already excludes: `node_modules/`, `__pycache__/`, `*.pyc`, `.venv/`, `coverage/`, `htmlcov/`, `/tmp/`, `*.log`, plus generated/sensitive install state (`configs/install-state.json`, `configs/resources.json`, `plugins/hackathonkit/.mcp.json`, `.claude/skills/`). None of these are excluded from the build context, so a developer's local state (e.g. after `npm install` or launcher run) is sent to the Docker daemon, bloating the context, risking leakage, and busting the `go mod download` layer cache on every build. The project's own checklist (`plugins/hackathonkit/skills/devops-expert/references/docker-review-checklist.md:11`) requires `.dockerignore` to list `node_modules/`, and backlog item M13 was marked fixed without the pattern being added. Add the missing patterns to `.dockerignore` (with `**/` prefixes to match at any depth).

- [ ] **OCR-037** `.gitattributes:1` _bug_
  The global `eol=lf` forces the working tree to LF for every text file, including formats that are not enumerated below and that Windows-native tooling expects in CRLF (e.g. `.sln`, `.csproj`, `.vcxproj`, `.vcxproj.filters`, `.props`, `.targets`, `.reg`). Because `eol=lf` overrides `core.autocrlf`/`core.eol`, Windows-only tooling can read/rewrite these files and produce spurious or non-applying diffs. If this repo is genuinely Go-only this is fine, but if Windows project files may be added later, either add them to the CRLF block below or drop `eol=lf` from the global default and rely on `text=auto`.

- [ ] **OCR-038** `.gitattributes:24` _bug_
  The binary list omits many common binary artifacts (e.g. `.exe`, `.dll`, `.so`, `.dylib`, `.a`, `.o`, `.jar`, `.class`, `.wasm`, `.pyc`, `.bmp`, `.tiff`, `.avif`, `.7z`, `.tar`). `text=auto` relies on a NUL-byte heuristic that can misfire on small binary files, causing them to be treated as text and line-ending-normalized (data corruption). Enumerate the binary formats actually used by this repo explicitly rather than depending on auto-detection.

- [ ] **OCR-039** `.github/workflows/ci.yml:0` _security_
  This redactor is materially weaker than the one in `scripts/docker-check.sh` (which already emits redacted diagnostics itself):
  
  1. Case-sensitive keywords: only `PASSWORD|PASS|TOKEN|SECRET|DSN|KEY` in upper case match, so lowercase/mixed-case keys (`password=`, `api_key=`, `secret:`) are printed verbatim. The script uses `[Pp][Aa][Ss]...` character classes to stay case-insensitive.
  2. Quoted values are not redacted at all: the value pattern `[^ "]+` cannot match a leading `"`, so `POSTGRES_PASSWORD="s3cr3t"` (and other values that compose/quote) passes through unredacted. The script handles this with `("[^"]*"|[^ " ]+)`.
  3. If `sed -E` is unavailable the sed fails and, because of `|| true`, the step silently continues with no notice; the script explicitly fails closed with a message.
  
  Since the redaction rules are duplicated here, they drift from the tested implementation. Best fix: reuse the shared redactor (or extract it into a sourceable script) instead of maintaining a second copy.

- [ ] **OCR-040** `.github/workflows/ci.yml:42` _security_
  The rehearsal report is dumped verbatim, unlike the docker job which carefully pipes diagnostics through a redactor. `cmd/hackathonkit-rehearse` records step `evidence`/`error` strings and includes MCP-server stderr (`lastLines(stderr.String(), 2)`), which can embed configuration or connection details in the JSON. On failure the whole file is printed to the public build log. Redact the output before printing (reusing the same filter as the docker job) or upload `rehearsal.json` as an artifact instead of echoing it.

- [ ] **OCR-041** `.gitignore:21` _maintainability_
  Both the `data/` and `evals/*/` blocks are hand-enumerated allowlists. Any new runtime subdirectory under `data/` (e.g. `data/quality/`) or new eval artifact kind (`evals/<new-kind>/<sha>/…`) is silently NOT ignored and will be committed on the next `git add .`. For `data/`, the two directories that must stay tracked are `data/input/` (committed sample corpus) and `data/fly/` (`data/fly/dataset.jsonl` is committed), so both need explicit negations. For `evals/`, only the tracked `evals/README.md` is at the top level, so a directory-only catch-all is safe and keeps the README tracked. Adopting the suggested catch-alls is safer.

- [ ] **OCR-042** `Dockerfile:41` _security_
  The `quality` stage runs as root over a host bind mount, so `scripts/quality-gate.sh` creates `evals/quality/...` (and any other file it writes) owned by root in the host working tree, while the `hackathonkit-mcp`/`demo-api` services run as `${HACKATHONKIT_UID}:${HACKATHONKIT_GID}` (1000:1000). A later gate run through those services — or by the host user — then fails with EACCES on the root-owned `evals/quality` directory. Document the supported invocation (`docker compose run --rm --user "$(id -u):$(id -g)" quality`, as already noted for the cache volumes) or have the gate write into a directory that is chowned back to the runtime UID.

- [ ] **OCR-043** `cmd/demo-api/fly_live.go:120` _performance_
  `refresh()` does blocking, potentially heavy work while holding `f.mu`: `os.Stat` + `os.Open` + `file.ReadAt` of up to 4 MiB, plus `json.Unmarshal`/`json.Marshal` of every complete line. Every SSE subscriber owns a 250 ms ticker and calls `snapshot()` -> `refresh()`, and the `?snapshot=1` REST path shares the same lock, so all connections plus the snapshot endpoint serialize behind one disk read. On a slow/stalled filesystem (e.g. NFS) a single read can stall every connected dashboard client. A single shared poller goroutine that owns the file state and publishes an immutable snapshot (e.g. via an atomic pointer), or reading outside the lock and only locking to publish, would remove the head-of-line blocking. Note the mutex is a `sync.Mutex`, so even the cheap fast-path `Stat` comparison serializes all readers unnecessarily.

- [ ] **OCR-044** `cmd/demo-api/main.go:343` _bug_
  The revision guard is skipped whenever the client omits `revision` (zero value) or sends a negative number, so a stale client that never sends `revision` can overwrite an approval taken against a newer plan revision — exactly the lost-update the `decisionMu` + revision check exist to prevent. Require a valid revision instead of silently bypassing the check.

- [ ] **OCR-045** `cmd/demo-api/main.go:451` _bug_
  `readApproval` collapses every failure into `nil`: a missing file, a permission error, and a truncated/corrupt JSON record are all reported as "no approval". `/plan/api/latest` then returns `"approval": null`, so a corrupt approval is indistinguishable from "not yet decided" and the UI silently loses an existing decision. Distinguish `os.IsNotExist` (return nil, nil) from real read/parse failures and surface them (e.g. change the signature to `(*planApproval, error)` and log/return the error) rather than treating them as absence.

- [ ] **OCR-046** `cmd/fly-bench/main.go:231` _bug_
  When `cfg.Routing.TopK <= 0` (a state the config layer does not forbid) this branch silently uses *all* returned recommendations as k, while `report.Summary.TopK` still records the raw config value (0). Meanwhile `fly.NewRouter` defaults `topK` to 5 for the very same settings, so the published "fly recall@k" / "top_k" no longer describe the top-k the router actually applied. Normalize the effective k once (matching `fly.NewRouter`'s default) and use it for both the recall computation and the report.

- [ ] **OCR-047** `cmd/hackathonkit-ctl/daemon.go:134` _bug_
  `store.Close()` is deferred at the top of `daemonForeground`, but nothing waits for the shutdown goroutine below to finish draining in-flight handlers. When `ctx` is cancelled, the goroutine calls `server.Shutdown`, and `ListenAndServe` returns `http.ErrServerClosed` immediately — *not* after handlers complete. The function then returns, the deferred LIFO order runs `ingestWG.Wait()` and `store.Close()` while the server may still be executing handlers, so concurrent requests can fail with `sql: database is closed` and the pid file is removed before the server is really down. Await the shutdown goroutine (e.g. close a `done` channel after `Shutdown` returns) before returning so `store.Close()` only runs once serving has stopped.

- [ ] **OCR-048** `cmd/hackathonkit-ctl/daemon.go:230` _security_
  Returning `true` here means any live PID recorded in the pid file is reported as "the daemon" on non-Linux or restricted-/proc systems, because `readPID` only requires `processAlive` in that case. A stale or recycled PID then makes `daemonStop` send SIGTERM to an unrelated process. Combined with the brittle substring match on the executable path below, `daemon start/stop/status` are unreliable: if the binary is installed/named anything other than `.../hackathonkit-ctl`, the identity check fails and `start` reports a false "did not start" while `stop` reports "not running". Consider persisting the daemon's executable path (or start time/inode) in the pid file and comparing it exactly, and defaulting the unverifiable case to `false` (requiring an explicit force flag for `stop`) instead of assuming ownership.

- [ ] **OCR-049** `cmd/hackathonkit-ctl/daemon.go:313` _security_
  This Origin/Referer heuristic can be bypassed for the one state-changing GET endpoint. A page can send `<img src="http://127.0.0.1:8788/api/risk">` (or a plain link) after setting `<meta name="referrer" content="no-referrer">`; sub-resource/navigation GETs omit Origin, and with the no-referrer policy Referer is omitted too, so both header checks are empty and `RecomputeRisk` (a DB write) executes cross-site. Since `/api/risk` is explicitly listed here but only guarded when a header happens to be present, either drop the endpoint's mutating behavior into a POST method or reject `/api/risk` whenever `token == ""` regardless of Origin/Referer (or gate on `Sec-Fetch-Site`/`Sec-Fetch-Mode`).

- [ ] **OCR-050** `cmd/hackathonkit-ctl/main.go:394` _maintainability_
  `cmdRun` accepts `stdout, stderr io.Writer` but bypasses them for user-facing output: the final summary uses `fmt.Printf` (hard-wired to `os.Stdout`) and two bookkeeping failures use `fmt.Fprintln(os.Stderr, ...)` (lines ~345 and ~350), while other messages in the same function correctly use `stdout`/`stderr`. Callers that pass a buffer (e.g. `run(args, &out, &errOut)`) silently miss this output, which breaks the injectable-`run()` contract this file establishes. Use the injected writers consistently.

- [ ] **OCR-051** `cmd/hackathonkit-ctl/proc_unix.go:17` _maintainability_
  Both platform implementations of the process-liveness check treat a permission failure as not-alive, and readPID (cmd/hackathonkit-ctl/daemon.go:216) turns that false into a stale verdict: daemonStop skips terminating a running daemon and deletes its pid file, and daemonStart can spawn a second copy. On POSIX (proc_unix.go) the EPERM branch is a raw error-identity comparison against syscall.EPERM that silently breaks if the error is ever wrapped; use errors.Is(err, syscall.EPERM) instead. On Windows (proc_windows.go) every OpenProcess failure is collapsed into false, but only ERROR_INVALID_PARAMETER (87) means no such pid while ERROR_ACCESS_DENIED (5) must be treated as alive (matching the POSIX intent). Fix both files so an unprivileged-but-live daemon is reported correctly.

- [ ] **OCR-052** `cmd/hackathonkit-ctl/tui.go:92` _bug_
  An events-only query failure is stored in the shared `m.err`, but `View()` returns as soon as `m.err != nil`, so a transient failure of the events query hides the otherwise-valid session list (and the whole body) — contradicting the "may be an events-only failure" intent here. Keep the two failures distinct (e.g. a separate `eventsErr` field) and render the events error inline next to the events pane instead of discarding the sessions view.
  
  Relatedly, the `if m.err != nil { ...; return b.String() }` branch in `View()` should only bail out when the sessions themselves are unavailable.

- [ ] **OCR-053** `cmd/hackathonkit-mcp/main.go:397` _bug_
  The error from `store.Top` is silently discarded: only `err == nil` is handled, so an unreadable/corrupt learnings store (`Top` returns the error from `s.load()` when the JSON file cannot be read/parsed) produces exactly the same output as "no learnings" — the session hook gets no signal that injection failed. The sibling branches in this function do surface failures ("resources: profile unreadable", the `countFiles` diagnostic), so this is inconsistent as well as lossy. Report the failure to `diag`, and also treat a successful-but-empty result as "no learnings" rather than a failure.

- [ ] **OCR-054** `cmd/hackathonkit-rehearse/main.go:198` _bug_
  `rep.OK` is assigned before `writeReport`, so when the report cannot be written the local `ok` becomes false (exit code 1, non-JSON output prints FAIL) but `rep.OK` stays `true`. In `--json` mode the JSON emitted on stdout therefore claims `"ok": true` while the process exits 1, which lets a machine consumer read a failed run as success. Keep `rep.OK` consistent with the failure.

- [ ] **OCR-055** `configs/resource-matrix.yaml:15` _bug_
  Variant ordering contradicts this file's own contract. The header states the planner "picks the first variant whose provider is ready; variants without a provider ... act as deterministic fallbacks", but here (and identically in `timeseries`, `classification` and `retrieval`) the provider-less `deterministic` entry is listed first, while `reasoning`/`embeddings`/`asr`/`vision`/`tts`/`notifications`/`metadata`/`artifacts` correctly put the fallback last.
  
  Because `resources.Plan` (internal/resources/matrix.go:113-119) picks the first variant whose provider is empty *or* ready, the provider-less entry always wins: the `openai` variant is only ever surfaced under `alternatives`, the capability is never reported as `degraded`, and the `missing[]` upgrade hint (`OPENAI_API_KEY`) is silently lost even when the key is present. It also breaks the fallback assumption in `Plan` (matrix.go:122-131) that the terminal variant is provider-less — for these four capabilities the last entry is provider-backed, so any reorder would select an unready provider as the "fallback".
  
  Prefer listing the provider-backed variant first and `deterministic` last (matching the other capabilities). Note that `resources.DefaultMatrix()` (internal/resources/matrix.go:229-244) and the `TestRepoMatrixMatchesDefaults` regression test (internal/resources/matrix_test.go:26-35, `reflect.DeepEqual`) must be updated in lockstep or the test will fail.

- [ ] **OCR-056** `docker-compose.yml:14` _bug_
  The comment's premise is wrong, so this gating is a real startup race: the MCP opens the object store exactly once at startup (`cmd/hackathonkit-mcp/main.go:298-307`), and if `objectstore.Open` fails (MinIO's process is “started” long before the S3 API on :9000 accepts connections, so `BucketExists`/`MakeBucket` can get connection-refused) the error is only logged as `objectstore_disabled` and `objects` stays nil for the whole process lifetime — there is no “reconnect on first use”. Result: artifact/blob mirroring is silently lost for the entire session after a plain `docker compose up`. Add a healthcheck that probes MinIO's S3 endpoint (e.g. `test: ["CMD", "mc", "ready", "local"]`, or `/minio/health/live`) and gate on `service_healthy`; otherwise the MCP needs retry-on-first-use instead of a one-shot Open.

- [ ] **OCR-057** `internal/atomicfile/atomicfile.go:35` _bug_
  `os.MkdirAll` only applies `dirMode` to directories it actually creates; it never changes the mode of a pre-existing directory. So the guarantee claimed by the comment above ("Least-privilege state files get a private containing directory") does not hold when the parent directory already exists with 0o755 — the 0o600 state file then sits in a world-listable/traversable directory. If the private-directory guarantee is intended, tighten the existing directory explicitly (e.g. `os.Chmod(dir, dirMode)` for the private case), otherwise soften the comment to say it only applies to newly created directories.

- [ ] **OCR-058** `internal/atomicfile/atomicfile.go:41` _bug_
  The stale-temp reclamation deletes *any* file in the target directory whose name matches `<base>.` + `*` + `.tmp` and is older than one hour. That name space is not exclusively owned by this package, so an unrelated file such as `gate.json.old.tmp` (or a temp left by another tool / another process writing the same base name) will be silently deleted. The same heuristic can also remove a *live* temp file of a concurrent writer that has been stalled/suspended for more than an hour, making that writer's subsequent `Sync`/`Rename` fail with ENOENT. Since the reclamation is best-effort, prefer a package-unique marker in both the prefix and the `os.CreateTemp` pattern so only files this package created are ever candidates.

- [ ] **OCR-059** `internal/config/config.go:390` _bug_
  The routing version is read before `m.mu` is acquired, so the read-modify-write is not atomic across `Manager` instances/processes. `./configs` is bind-mounted into both `demo-api` (cmd/demo-api/main.go:376) and `hackathonkit-mcp` (cmd/hackathonkit-mcp/main.go:197), and each holds its own `Manager` with its own mutex. Both can read version N and then write N+1, so the later write silently discards the earlier mode change and the version never reaches N+2. The reconciliation below only compares against the in-process `m.routingCache`, so it cannot detect this. Serialize writers with an OS-level file lock (e.g. flock on a lock file) around the read-modify-write, or explicitly document that only a single writer process is supported.

- [ ] **OCR-060** `internal/config/config.go:398` _performance_
  `yaml.Marshal` and `atomicfile.Write` (which does MkdirAll, ReadDir, CreateTemp, fsync, rename and directory fsync) run while holding `m.mu`, the same mutex used by `Config()`, `Routing()` and `Evaluation()` on request/worker paths. The comment above states the read is deliberately done outside the lock so a slow/bind-mounted filesystem cannot serialize config access, yet the actual disk write still blocks every reader behind it. Serialize writers with a dedicated mutex (separate from the cache mutex) and/or perform the file write outside `m.mu`, taking the lock only to publish `m.routingCache`.

- [ ] **OCR-061** `internal/config/dotenv.go:93` _bug_
  `unquoteEnvValue` treats the first unescaped `"` as the end of the value and silently discards everything after it, so a shell-legal line can load a different value than the file specifies. For `KEY="abc"def` the process gets `abc` (a shell would export `abcdef`), and the single-quoted branch has the same truncation (`KEY='abc'def` -> `abc`). Because the value is applied with no warning, a hand-edited `.env` can quietly load a wrong secret/config value (the same silent-corruption class the escape handling was fixed for). Consider only accepting the closing quote when the remainder is empty, whitespace, or a `#` comment (which is what trailing comments rely on); otherwise keep the raw value or report the malformed line.

- [ ] **OCR-062** `internal/control/handoff.go:35` _bug_
  `plans.LatestErr` deliberately returns a non-nil error alongside `ok=true` when a plan was found but the newest plan file was unreadable/corrupt (plans.go:176-179). Here `planErr` is only inspected in the `else` branch, so a partially-unreadable plan set is silently swallowed: the handoff renders a possibly stale plan with no warning, defeating the documented purpose of `LatestErr`.

- [ ] **OCR-063** `internal/control/handoff.go:63` _performance_
  Both decision counts materialize up to 10000 complete event rows (the same is done again by the scoped `store.Events(ctx, sessionID, 10000)` below) and `scanEvents` JSON-unmarshals every payload, just to derive a single integer — up to ~20k rows and map allocations per handoff. A dedicated `SELECT COUNT(*) ... WHERE type='eval_decision'` would be far cheaper. Note also that the fixed `LIMIT 10000` (store.go:340-343) truncates silently, so a busy store under-reports `PendingDecisions` while the handoff still prints an exact-looking number; a COUNT query also removes that ambiguity.

- [ ] **OCR-064** `internal/control/ingest.go:117` _bug_
  Any error from `s.Cursor(ctx, name+":head")` is silently swallowed by `cerr == nil`. A transient store error therefore skips rotation detection entirely and the reader keeps seeking to a stale offset, potentially skipping the head of a recreated file with no error surfaced. Additionally, treating `stored != 0` as "set" conflates a legitimate FNV hash of 0 with "unset", disabling rotation detection for that file. Surface the read error instead of folding it into the boolean condition.

- [ ] **OCR-065** `internal/control/ingest.go:175` _bug_
  When `handle` fails and `flush(lastGood)` also fails, the original record-processing error `err` is discarded and only `cerr` is returned. Callers lose the real cause (e.g. a bad record vs. a cursor write failure), which makes diagnosis of the failed ingest much harder. Preserve both errors.

- [ ] **OCR-066** `internal/control/ingest.go:182` _bug_
  `AppendEvent` (store.go) is a plain `INSERT` with no dedup key, so it is not idempotent. Because the cursor is only persisted every `cursorFlushEvery` records (and on error/exit), a crash between appends and `SetCursor` will permanently duplicate `session-events`, `mcp-calls` and `fly-feedback` events on the next run, inflating `Events`/risk inputs. This directly contradicts the `Ingest` doc comment ("repeated runs never duplicate records"). Either persist the cursor per record, give events an idempotency key (e.g. source+line hash) so re-inserts are no-ops, or soften the documented guarantee.

- [ ] **OCR-067** `internal/control/ingest.go:191` _bug_
  The head fingerprint is only refreshed when `hasFP` is true, i.e. the current file is >= 256 bytes. If a file rotates (by size or fingerprint) into a new file whose head is still shorter than 256 bytes, the stale head cursor from the previous file is left in place. Once that new file grows past 256 bytes, its head no longer matches the stale value, so a spurious rotation is detected, `offset` is reset to 0 and the already-ingested prefix is re-read — producing duplicate events. Clear `name+":head"` whenever a rotation is detected to avoid this re-read.

- [ ] **OCR-068** `internal/control/ingest.go:291` _bug_
  Two problems here: (1) `os.ReadFile` reads the entire decision file with no `maxIngestBytes`-style bound, so a large/growing file can exhaust memory; (2) a single unreadable file returns immediately, aborting the whole `Ingest` (skipping the remaining decision files and, importantly, `RecomputeRisk`/`Counts`) on every subsequent cycle as long as that file stays unreadable. Consider bounding the read and skipping/continuing (logging) on a per-file read error so one bad file cannot indefinitely block the rest of ingestion.

- [ ] **OCR-069** `internal/control/risk.go:84` _performance_
  `payloadHasPanic` compiles the negation regex on every call, i.e. once per `mcp_call` event, inside the per-event loop that itself runs for every session in `RecomputeRisk`. For a large events table this is a substantial repeated allocation/compile cost. Hoist it to a package-level var next to `panicWord`. (Also, `strings.ToLower` above already lowercases the text, so the `(?i)` flag here is redundant but harmless.)

- [ ] **OCR-070** `internal/control/risk.go:100` _maintainability_
  Truncation is only logged for the session query. `Events` and `EventsBetween` are also limited to `riskScanLimit`, and both clamp their limit to 10000 (falling back to a small default if exceeded). A session with more than `riskScanLimit` events, or a busy time window with that many global events, is scored on only the newest rows and silently under-counts failures with no warning. Log truncation for the event queries too (or check `len(events) == riskScanLimit` / `len(globals) == riskScanLimit`) so the under-scoring is observable.

- [ ] **OCR-071** `internal/control/store.go:190` _bug_
  `UpsertSession`'s conflict branch unconditionally overwrites every metadata column with `excluded`, so a partial/replayed record (e.g. a Session built with only `ID`/`Status`, as `EndSession` and various callers do) silently erases previously stored `root`, `kind`, `task`, `stage`, `plan_slug`, and `worktree`. This contradicts the deliberate preservation logic used elsewhere (`StartSession` guards `root` with `COALESCE(NULLIF(?,''), root)` and the conflict branch already preserves `ended_at`/`status`). Guard the text columns so an empty incoming value does not clobber stored data.

- [ ] **OCR-072** `internal/control/store.go:241` _bug_
  `EndSession` always rewrites `status`, `ended_at`, and `updated_at` for an existing row, so it is not idempotent: a replayed or out-of-order `session_end` (the ingester replays JSONL from a reset cursor / on fingerprint-detected rewrites) moves `ended_at` — and `updated_at`, which drives `Sessions()` ordering — backwards. This is inconsistent with `UpsertSession`, which deliberately keeps the first `ended_at` via `COALESCE(sessions.ended_at, excluded.ended_at)`. Preserve the earliest end time and never move `updated_at` backwards.

- [ ] **OCR-073** `internal/evaluation/bundle.go:269` _bug_
  The previous bundle is deleted before the new one is renamed into place, so there is a window where the committed bundle no longer exists. If the process crashes/ is killed between the two calls, or `os.Rename` fails (concurrent run making `outAbs` non-empty again, permission/IO error), the previous bundle is lost — contradicting the comment above ("a failed run never destroys the previous bundle"). Prefer moving the live bundle aside (`os.Rename(outAbs, outAbs+".bak")`), swapping the new bundle in, and only then removing the backup (rolling the backup back if the swap fails). The `removeOwnedBundle` ownership/symlink validation should still run before touching the live directory.

- [ ] **OCR-074** `internal/evaluation/evaluation.go:172` _bug_
  `Total` is the only numeric field not checked for non-finite values (scores and deduction points are). For `Total = NaN`, `math.Abs(NaN-sum) > 0.01` is false, so the report validates, and `reportTotal` then returns NaN (`NaN != 0` is true) — poisoning `MeanTotal`/`ScoreVariance` and making every `compareEntries` tie-break evaluate false, i.e. all entries look tied. Add an explicit finite check before the sum reconciliation.

- [ ] **OCR-075** `internal/evaluation/evaluation.go:508` _bug_
  Only `NoiseFloor` is guarded against NaN/Inf; `Candidate`, `Best` and `Target` are used unchecked. With a NaN `Candidate` every comparison below evaluates false (`in.Candidate < in.Best-epsilon`, `in.Candidate >= in.Target-epsilon`, `improvement < in.NoiseFloor-epsilon`), so execution falls through to `DecisionAccept`; `+Inf` behaves the same way (`improvement` = +Inf is never `< floor-epsilon`). A corrupt/overflowing score is therefore silently accepted instead of failing closed. Note the exported struct exposes `Comparison.InputError`, which is declared but never populated anywhere in this file. (Todays `eval.compare` call site filters non-finite args via `argFloat`, but this exported function accepts raw floats.)

- [ ] **OCR-076** `internal/learning/learning.go:205` _maintainability_
  A newly created global-scope learning still records `Project: s.project`, whereas Promote explicitly clears `Project` for global entries (and the update path here also clears it). This leaves two different on-disk representations of "global" learnings: one with a stale project tag and one without. Today `inScope` happens to treat both correctly, but any future projection-by-`Project` query or migration will disagree. Set `Project` only for project scope on creation.

- [ ] **OCR-077** `internal/mcp/registry.go:33` _bug_
  `Registry` is exported but `tools` is only initialized by `NewRegistry`. A zero-value `&Registry{}` (which is legitimate to construct externally) makes `Register` write to a nil map and panic with "assignment to entry in nil map". Make the zero value usable by lazily initializing the map here.

- [ ] **OCR-078** `internal/mcp/registry.go:94` _bug_
  Incomplete deep copy: `cloneSlice` only recurses into `map[string]any` and `[]any`. Any other container nested inside a `[]any` (e.g. `[]string`, `map[string]string`, `[]map[string]any`, typed slices/maps) hits `default` and the returned copy still shares the same backing storage as the registered tool. This contradicts the documented "deep-copies the schema" guarantee in `cloneTool`: a caller that mutates the returned schema could then corrupt registry state (and race with concurrent readers). `cloneMap` already handles `[]string`/`map[string]string`, so the two helpers are inconsistent; mirror those cases here or factor a single `cloneValue(v any) any` helper used by both.

- [ ] **OCR-079** `internal/mcp/server.go:78` _bug_
  The reader goroutine's only exit path while blocked inside `bufio.Read` is `closer.Close()`. Because `in` is declared as `io.Reader`, nothing forces callers to pass a Closer (the tests already pass `strings.NewReader`), and for descriptors whose `Close` does not interrupt an in-flight blocking read (e.g. a terminal fd), the goroutine keeps running after `ServeStreams` returns, retaining the `bufio.Scanner` buffer that can grow to 32MB. Consider making the requirement explicit (accept `io.ReadCloser`, or document it on the exported method) so a pass-through reader cannot silently leak a goroutine per session.

- [ ] **OCR-080** `internal/mcp/server.go:265` _bug_
  This error envelope sets `Status`, `Data["error"]` and `TraceID` but leaves the `Envelope.Error` field empty, while `Fail()` (contracts.go) always populates it. Clients that read the top-level `error` field of the envelope (the canonical error attribute) will see an empty string for handler failures, but a populated one for tool-level failures produced via `Fail`. Use the existing helper to keep the wire shape consistent.

- [ ] **OCR-081** `internal/metadata/metadata.go:126` _bug_
  The `migrations` block adds `mode`/`version`/`kind` as nullable, while the `schema` block declares them `NOT NULL` for fresh databases. Because `CREATE TABLE IF NOT EXISTS` is a no-op on an existing deployment, an upgraded database ends up with weaker constraints than a freshly created one, so the same INSERT can succeed on one deployment and be rejected on another. Reconcile the constraints explicitly (backfill + `ALTER COLUMN ... SET NOT NULL`), or consciously relax the `schema` definitions so both paths match.

- [ ] **OCR-082** `internal/metadata/metadata.go:149` _bug_
  `isDuplicateDDL` classifies errors by substring match on the rendered message. Any unrelated DDL failure whose text happens to contain "already exists" (or the literal substrings "42P07"/"42710") is silently treated as a benign duplicate and startup continues on a half-created schema. Classify by the structured `*pgconn.PgError` SQLSTATE instead (`pgconn` is already imported).

- [ ] **OCR-083** `internal/openai/client.go:252` _bug_
  io.LimitReader returns EOF at the limit and gives no truncation signal, so an oversized response body is silently cut mid-JSON and the failure is misreported. internal/openai/client.go reads the transcription body with a bare 32<<20 LimitReader (larger bodies are truncated and the later json.Unmarshal fails with a misleading unexpected-end-of-JSON error). internal/resources/probe.go reads 1<<20 bytes and ignores the subsequent parse error entirely, reporting Reachable: true with 0 models so a genuine empty list is indistinguishable from a truncated body. Read one extra byte and flag/reject oversized or unparsable responses, mirroring the overflow detection already used in doJSON.

- [ ] **OCR-084** `internal/plans/plans.go:34` _bug_
  `DefaultStages` is an exported package-level slice; callers already assign it directly (`plan.Stages = plans.DefaultStages`, `append(stages, plans.DefaultStages...)`). A caller that mutates a stage's `Status`/`Goal` on the returned slice silently corrupts the shared template for every other caller, and `Save`'s `plan.Stages == nil` check cannot repair it. Return a copy (e.g. keep an unexported template and expose `func DefaultStages() []Stage`) or document the slice as immutable.

- [ ] **OCR-085** `internal/plans/plans.go:148` _bug_
  `bestTime` is not always a file mtime: once a plan with a non-zero `UpdatedAt` is chosen, `bestTime` holds that semantic timestamp. Comparing the raw mtime of the next entry against it (`info.ModTime().Before(bestTime)`) therefore skips files whose mtime is older than another plan's `UpdatedAt`, even when that file's own `UpdatedAt` is newer (exactly the bulk-copy/`cp -p` case the comment below anticipates). The result depends on directory iteration order, so `LatestErr` can return the wrong "latest" plan. Compute `candidate` first and compare against it instead of pre-filtering on mtime.

- [ ] **OCR-086** `internal/plans/plans.go:182` _bug_
  `Latest` is a divergent copy of `LatestErr`: it ignores `plan.UpdatedAt` (using only the file mtime) and swallows every read/parse error via `continue`. For the same directory it can therefore return a different plan than `LatestErr` — contradicting the documented preference for the semantic timestamp — and gives callers no signal that the newest plan was corrupt or unreadable. Delegate to `LatestErr` so both helpers agree.

- [ ] **OCR-087** `internal/quality/proc_unix.go:20` _bug_
  `killProcessGroup` sends SIGKILL to the negated PID without checking that the child is still alive. Its only caller is `cmd.Cancel` in `quality.go`, and `os/exec` invokes `Cancel` from its context-watcher goroutine, which can run *after* `Process.Wait()` has already reaped the child (the process is marked done, but `Wait` is still draining the stdout/stderr pipes that `WaitDelay = 10s` bounds). Once the child has been reaped, its PID — and hence the process-group ID — can be recycled to an unrelated process group, which would then receive SIGKILL. Probe liveness before the raw group kill: `os.Process.Signal` is internally synchronized and returns `os.ErrProcessDone` once `Wait` has reaped the child (do not test `cmd.ProcessState` here — `Wait` writes it concurrently with this goroutine, which would itself be a data race).

- [ ] **OCR-088** `internal/quality/quality.go:130` _bug_
  `strings.Fields` is not quote-aware, but `GOFLAGS` is parsed by cmd/go with shell-like quoting (a value may legitimately contain quoted segments with spaces, e.g. `-gcflags='-m -m'` or `-ldflags="-X main.v='a b'"`). Splitting and rejoining with single spaces both drops the original quoting fidelity and produces tokens like `-gcflags='-m` / `-m'`.
  
  Worse, deleting every token with prefix `-buildvcs=` can strip the first half of a quoted value that happens to start with that text (`-gcflags='-buildvcs=x y'`), leaving an unbalanced quote; cmd/go then fails the whole GOFLAGS string with "invalid quoted string in GOFLAGS" and every check in the gate fails for a reason unrelated to the code under test.
  
  Use a quote-aware splitter (matching cmd/internal/quoted semantics) or preserve the raw inherited string rather than re-serialising tokens, and only drop a flag token whose name is exactly `-buildvcs`.

- [ ] **OCR-089** `internal/quality/quality.go:296` _bug_
  `GitSHA` returns unvalidated content, unlike `validSHA` (used by `Path`) and `resolveGitSHA`/`validGitSHA` in internal/evaluation.
  
  - Detached HEAD: any value with `len(value) >= 40` is returned verbatim — no hex check, no length cap — so a junk/multi-line `HEAD` (e.g. `ref: refs/heads/other` padded, or arbitrary ≥40-char text) becomes the reported SHA.
  - Ref file: the file's contents are returned whenever non-empty, so a symbolic-ref file or corrupt ref yields a value that is not an object id at all (the built-in test only covers the happy path).
  
  `Run` puts this value in `Report.SHA` and persists it to `gate.json`; downstream evidence writers embed the SHA in filenames (e.g. `cmd/fly-bench/main.go`: `fmt.Sprintf("%s-%s.json", rep.SHA, ...)`), so non-SHA content — including path separators from a crafted `.git/HEAD` — can steer those paths outside the intended evidence directory. Validate before returning (and consider a containment check after joining the ref path, as the evaluation package does).

- [ ] **OCR-090** `internal/resources/matrix.go:171` _bug_
  When a capability has no variants, `Plan` appends a `Decision` whose `Chosen` is the zero `Variant` (only `Capability`/`Degraded`/`Reason` are set). `DecisionSummary` then renders an empty id — e.g. `reasoning=` — which is indistinguishable from a malformed entry, and this string is surfaced directly to users via `internal/tools/resources.go`. Either skip this case or render an explicit placeholder so the undefined capability is visible.

- [ ] **OCR-091** `internal/resources/probe.go:54` _bug_
  `Live` is set as soon as the transport returns any response, before the status code and body are validated. The non-200 path (`error: "http 401"`) and the body-read-failure path both return with `Live == true` while `Reachable == false` and `Error != ""`. The caller copies `probe.Live` verbatim into the agent-facing payload (internal/tools/resources.go:305), so consumers receive contradictory results such as `live: true, reachable: false, error: "http 500"`. Set `Live` only after a successful 200, or explicitly document that `Live` means "an HTTP response was received (even an error one)" so callers do not read it as a health signal.

- [ ] **OCR-092** `internal/resources/resources.go:159` _bug_
  `Load` never validates `Version`, and `Save` only defaults it when it is zero while always re-serializing with the v1 `Profile` struct. A `configs/resources.json` written by a future (v2) schema is therefore silently rewound to the v1 shape (any v2-only fields are dropped by `Unmarshal`) but keeps `version: 2`, so the downgrade is invisible and the extra data is permanently lost. Reject an unsupported version in `Load`/`Save` so callers can migrate instead of clobbering.

- [ ] **OCR-093** `internal/resources/resources.go:166` _bug_
  `Claimed["codex_pro"]` unconditionally wins over the `CodexPro` field, and this branch always inserts the key, so every profile ever persisted contains it. A caller that sets `profile.CodexPro = true` on a profile loaded from disk (whose `Claimed` already holds `codex_pro: false`) has the value silently reverted to `false` here, even though `Statuses` (which ORs the two) would have reported it as claimed. Keep a single source of truth (derive `CodexPro` from `Claimed`, or make `CodexPro` authoritative) to avoid the two representations diverging.

- [ ] **OCR-094** `internal/routing/fly/hash_encoder.go:30` _performance_
  `Embed` accepts a context but never consults it. On a large batch (or a batch with many long texts) the FNV loop is unbounded CPU work that cannot observe cancellation or a deadline set by the caller, so a cancelled request still burns CPU until the whole batch finishes. Check the context periodically inside the loop.

- [ ] **OCR-095** `internal/routing/fly/llm.go:29` _maintainability_
  The classifier error is collapsed into the same `nil, false` result as benign "no signal" cases (empty response, non-numeric/unknown categories, all-equal scores). A persistently failing LLM is therefore indistinguishable from a router with no classifier configured, so the failure is invisible to `Recommend` and to callers/reports. Other best-effort paths in this package log such failures (`log.Printf` in embed.go:248 and memory.go:132) — this one is inconsistent and drops the error without observability. Please at least log it (or return a distinguishable state) so operators can detect a broken/unreachable LLM.

- [ ] **OCR-096** `internal/routing/fly/mb.go:347` _performance_
  The process-global `brainMu` is held across the entire read-modify-persist section, including `r.saveBrain`, which performs `MkdirAll`, `MarshalIndent`, a temp-file write, `Sync()` and `Rename()`. Because `brainMu` is shared by every Router in the process, a concurrent `Recommend` (`mbCategoryScores`) or `Dopamine()` call in *any* Router blocks for the full duration of this disk write. This directly contradicts `loadBrain`'s own comment that "a slow disk must not serialize every Router in the process" (and `Dopamine()` -> `loadBrainLocked` even does `os.ReadFile` under the same lock on the cold path). Consider separating the write/persist serialization from the read lock (e.g. an RWMutex where readers take RLock and writers hold the write lock only for clone+swap, plus a dedicated mutex serializing the save), so scoring paths are not gated on I/O.

- [ ] **OCR-097** `internal/routing/fly/mb.go:356` _bug_
  `adjust` silently returns when the category is missing from `Readout` or its slice is shorter than the activation code. `applyDopamine` calls it for categories derived from `categoriesForCapabilities(ev.Chosen)`, which are canonical categories (e.g. "timeseries") that need not exist in a brain trained on an arbitrary dataset. In that case no weight is changed, yet `work.Dopamine.PAM++/PPL1++` still runs, the brain is persisted, and `applyDopamine` returns `true` — so callers such as `AddFeedback` report `learned: true` when nothing was actually learned, masking a misconfigured/untrained brain. Have `adjust` report whether it applied an update and only count/return success when at least one category was updated (or log/skip unknown categories explicitly).

- [ ] **OCR-098** `internal/routing/fly/mb.go:432` _bug_
  `SaveTo` overwrites the brain artifact with a plain `os.WriteFile`. This is the same non-atomic write that `saveBrain` already moved away from (temp file + `Sync` + `Rename`). This method is used by `cmd/fly-train` to write the production `data/index/fly-brain.json`; a crash/full disk mid-write leaves truncated JSON, and `loadBrain` silently returns `nil` on `json.Unmarshal` failure, so all MB signal disappears with no error until a full retrain. Reuse the atomic temp-file + rename pattern here.

- [ ] **OCR-099** `internal/routing/fly/memory.go:131` _bug_
  `loadMemory` mutates the filesystem on a JSON parse failure (renames the file to `*.corrupt`) and then returns an empty memory with a nil error. This means the read-only accessor `MemorySize` can rename files as a side effect, and callers cannot distinguish "no data" from "data was corrupt and lost". Worse, if the rename fails (read-only FS, permissions) the failure is only logged and the corrupt file is left in place, so the next `AddFeedback`/`saveMemory` silently overwrites the recoverable data. Confine quarantine to the write path and/or surface an error instead of returning nil

- [ ] **OCR-100** `internal/routing/fly/memory.go:211` _bug_
  Inconsistent model-mismatch guard: `memoryCategoryScores` only treats stored vectors as stale when `r.modelTag != ""`, but here an unset current model tag (embedder configured without a model tag, common when the model name is not supplied) makes `mem.Model != r.modelTag` true and silently deletes every stored vector, then rewrites `mem.Model` to the empty string. Historical semantic memory is destroyed with no error. Align the condition with the scoring path so an empty current tag never prunes.

- [ ] **OCR-101** `internal/skills/validate.go:82` _bug_
  These two patterns capture the raw text between the delimiters, not a clean path. Markdown links with a fragment or title (`[rubric](references/rubric.md#criteria)`, `[rubric](references/rubric.md "Rubric")`) and backticked spans with anchors/trailing punctuation (`` `references/tools.md#fly` ``) are captured verbatim, so the subsequent `filepath.Clean`/`os.Stat` reports a false "referenced file does not exist". Strip the fragment (`#...`) and any title/whitespace suffix before validating.

- [ ] **OCR-102** `internal/skills/validate.go:125` _maintainability_
  Every `os.ReadFile` failure is reported as "missing SKILL.md", including permission errors, I/O errors and symlink resolution failures. That hides the real cause from whoever consumes the report and sends them chasing a non-existent problem. Distinguish not-exist from other errors.

- [ ] **OCR-103** `internal/skills/validate.go:203` _bug_
  The explicit-only detector requires the literal substring `explicit-only:` (colon), but several skills phrase the marker with a semicolon (`Explicit-only; never invoke implicitly.`) — e.g. roast-project, cp-expert-refactoring, self-improvement-controller, system-design-expert. For those skills this branch never fires, so a missing/true `policy.allow_implicit_invocation` is never reported and the standard silently goes unenforced. Accept both separators instead of a fixed colon.

- [ ] **OCR-104** `internal/storage/store.go:20` _performance_
  Backend calls ignore caller cancellation. `WriteArtifact`/`ReadArtifact` are invoked synchronously from request handlers (e.g. `internal/tools/documents.go`), but the metadata record and object-store put/get run on a fresh `context.Background()` with a hardcoded 30s timeout, and `WriteArtifact` may issue two such calls sequentially. A stalled Postgres/MinIO backend therefore blocks the handler for up to 30s (60s) while ignoring any caller deadline/cancellation. Consider threading the caller's `context.Context` through these methods (or at least exposing the timeout as configuration) so request cancellation aborts the backend call.

- [ ] **OCR-105** `internal/storage/store.go:154` _security_
  TOCTOU on the symlink guard: the `Lstat` check, `ensureWithin`, and `os.WriteFile` are three separate syscalls, so a symlink created at the final path component between the `Lstat` and the `WriteFile` is followed and the data is written to the link target (outside the root). The same race applies to the rehydrate path in `ReadArtifact`, which also routes through `safeWriteFile`. Since the function's stated contract is "refuses to follow a symlink at the final path component", consider making the write atomic w.r.t. the check: create the file with `os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)` (fails if something already exists, including a symlink) or write to a temp file in the verified parent directory and `os.Rename` it into place.

- [ ] **OCR-106** `internal/telegram/telegram.go:106` _security_
  `WithHTTPClient` silently discards the security-relevant `CheckRedirect` guard installed by `New`. The bot token is part of the request URL, so a custom client that follows redirects (any plain `&http.Client{}`/`http.DefaultClient`) will re-send `https://<BaseURL>/bot<token>/<method>` to the redirect target, leaking the token cross-host. Currently only tests use this setter, but it is exported API: either preserve the guard (wrap the supplied client's `CheckRedirect` to reject redirects) or document loudly that the caller must keep redirects disabled.

- [ ] **OCR-107** `internal/telegram/telegram.go:556` _bug_
  The `budget < 1` clamp breaks the hard "chunk never exceeds limit" guarantee. When the tags already open at the chunk boundary need more than `limit` runes to re-open plus close (`len(prefix) + closersBudget >= limit`), the code forces `budget = 1` and the shrink loop below exits immediately because `cut == pos+1`, so an oversized chunk is emitted. Telegram then rejects it with 400 instead of the message being sent. This is reachable with deeply nested/never-closed tags and small limits (the file's own tests call `ChunkHTML(text, 100)`; ~14 simultaneously open tags is enough). Consider flushing the pending chunk (closing all open tags, resetting `stack`) and starting the next chunk without a prefix when the budget would be non-positive, rather than emitting a chunk that cannot fit.

- [ ] **OCR-108** `internal/telegram/telegram.go:575` _bug_
  A chunk boundary can fall inside an HTML entity, corrupting the text. `cutOutsideTag` only protects `<`/`>`, so an entity such as `&lt;`, `&amp;` or `&#39;` that straddles the boundary is split into two chunks (e.g. `...&am` + `p;...`). Telegram then renders the literal, broken text (`&am` is not a recognised entity) instead of the intended character, and `scanTags` does not detect it either. In `HTML` mode entities are common (that is exactly how `<`/`&` must be escaped), so this produces silently garbled messages. Back the cut off to the start of an unterminated entity (last `&` in the window that is not followed by `;`) in addition to `cutOutsideTag`.

- [ ] **OCR-109** `internal/tools/context.go:213` _bug_
  File-access failures are silently dropped from the context-budget report in internal/tools/context.go, so total_tokens/by_kind are under-reported with no flag. The manifest loop continues on any os.Stat/os.ReadFile error (files that exist but are unreadable simply vanish), and the WalkDir callback returns nil when its err argument is non-nil (directory-read failures vanish the same way). Emit a flagged item for these failures as skillContextItems does for its own, so the report stays trustworthy.

- [ ] **OCR-110** `internal/tools/data.go:221` _bug_
  Two safety guards currently fail open when a prerequisite syscall errors: the table size guard skips the limit entirely if `os.Stat` fails (`serr == nil`), and the repository-root containment/symlink check is skipped whenever `filepath.EvalSymlinks(root)` fails (nonexistent/permission/transient errors), so a path escaping the root may be accepted and returned/open. The size guard is also TOCTOU-prone and checks `ctx.Err()` only after the whole file is decoded. Fail closed on these errors, enforce limits while streaming, and compare normalized forms consistently.

- [ ] **OCR-111** `internal/tools/data.go:410` _performance_
  Correlation analysis is O(numericColumns^2 * rows): for every pair it rescans all `t.Rows` and reparses both cells. With up to 2,000,000 rows and up to 64+ columns (only a warning above 64) this can consume enormous CPU on a request path, with no cancellation check inside the loop. Consider parsing each numeric column once into a []float64 slice (single pass over rows), capping the number of columns considered for correlations, and checking `ctx.Err()` periodically.

- [ ] **OCR-112** `internal/tools/data.go:412` _bug_
  `profileColumns` resolves each requested column via `columnIndex` and profiles that index, discarding the mapping. data.analyze then re-resolves the same names with `t.columnIndex(numeric[i])`. Because `numeric[i]` is the trimmed header text (p.Name) of an already-resolved index, a second lookup can return a different column (e.g. two headers that are equal after trimming, or a name that is now ambiguous), so correlations may be computed over different data than was profiled, or silently skipped. Return the resolved indices from profileColumns (or a name->index map) and reuse them here instead of re-resolving.

- [ ] **OCR-113** `internal/tools/documents.go:142` _bug_
  Both paths silently discard rows without a machine-readable warning: `tableAsText` drops every row beyond 500 per worksheet (the omission is only inline in the text), so neither the tool result nor `.meta.json` records the truncation; `loadSeries` drops rows whose time/value cell cannot be parsed, while only `loadTable` warnings are propagated. Return the omitted/skipped count and append a warning so callers can detect the data loss.

- [ ] **OCR-114** `internal/tools/extras.go:387` _bug_
  Both chart paths conflate `series[0]` with the set of rendered series: X-axis labels are always taken from `series[0]` even when it is shorter than the longest series (misplacing/compressing labels), and for bar charts only `series[0]` is drawn while the legend (and point count) still includes every series. Base labeling on the longest/rendered series and restrict legend/point reporting to series actually drawn.

- [ ] **OCR-115** `internal/tools/files.go:80` _performance_
  `readXLSXSheets` fully decodes/materialises every worksheet into `out`; the per-sheet caps (`maxXLSXCells`, `maxXLSXSheetBytes`) provide no aggregate budget across sheets, so a crafted multi-sheet workbook can drive very large memory/CPU on the request path. Track a global cells/rows/bytes budget across the loop and abort once exceeded, and/or add a names-only enumeration so callers that only need sheet names plus the first-sheet table do not parse/retain every sheet.

- [ ] **OCR-116** `internal/tools/knowledge.go:43` _bug_
  `tokenize` treats every contiguous letter/digit run as a single token, which only works for whitespace-delimited scripts. For scripts without word spacing (CJK, Thai, ...) a whole chunk collapses into one giant token that will never match a query token, so `knowledge.search` silently returns zero hits. The `>= 2` rune minimum also drops legitimate single-character CJK words. If non-space-delimited languages are in scope, add a fallback that emits per-character (or n-gram) tokens when a run exceeds some length, or document the limitation.

- [ ] **OCR-117** `internal/tools/knowledge.go:231` _performance_
  Every search walks the whole index directory, reads and `json.Unmarshal`s every index file, and keeps all chunks (full text + term maps) resident. Neither the index count nor the per-index size is bounded, and each `knowledge.index` call can persist an arbitrarily large document (chunk count times chunk size is unbounded). As the store grows, a single `knowledge.search` becomes an unbounded CPU/memory operation on the request path. Consider bounding the number of indexes scanned (or requiring an explicit `index_id`), streaming instead of materializing all docs, and/or persisting only the term statistics needed for scoring rather than full chunk text.

- [ ] **OCR-118** `internal/tools/knowledge.go:290` _bug_
  The returned `text`/`snippet` are just the *prefix* of the chunk, not a window centered on the match. With a default chunk of 800 chars (max 20000) and `text_chars` default 300, a term matched at offset >300 produces a hit whose returned text does not contain the matched term at all — the caller cannot verify the grounding, which breaks the tool's stated "grounded chunks" contract. Consider locating the first matched term in `chunk.Text` and returning a window around it. Additionally, `Snippet` hardcodes 300 and ignores `text_chars`, so with `text_chars < 300` the `snippet` field is longer than `text` — the two fields should derive from the same limit.

- [ ] **OCR-119** `internal/tools/learning.go:39` _bug_
  Both paths silently discard an error/diagnostic that callers rely on: `learning.record` ignores `RecordResult.EmbeddingError` (set by the store specifically so the caller can report degraded semantic recall), and the fly routing/feedback handlers ignore `_ =` JSONL append and `RecordFlyPrediction` errors, so telemetry/routing events can vanish with no diagnostic. Surface the failure as a warning/partial status, or at least log/count it, instead of converting it into an unqualified success.

- [ ] **OCR-120** `internal/tools/plan.go:214` _bug_
  Slug validation around the reserved `plan` fallback is inconsistent: `plan.status` skips validation entirely, so a bogus slug like `"!!!"` or `"日本語"` silently resolves to `evals/plans/plan.json` and returns another plan's data with `ok`; meanwhile `validPlanSlug` rejects every raw slug that normalizes to the fallback, including inputs that do contain letters/digits (`"plan!"`, `"plan.json"`, `"日本語"`), with a misleading 'must contain at least one letter or digit' message. Centralize validation so invalid slugs are rejected accurately and the fallback cannot be reached accidentally.

- [ ] **OCR-121** `internal/tools/quality.go:38` _bug_
  Both `quality.LatestReport` and `plans.LatestErr` can return a readable latest item with `ok == true` plus a non-nil diagnostic error caused by a different/unreadable sibling file. The handlers mishandle this: `quality.status` only reports the error in the `!ok` branch, silently presenting possibly stale/partial evidence; `plan.status` returns `{"plan": nil}` whenever `latestErr != nil`, discarding a perfectly readable latest plan. Handle the ok+err case explicitly in both: return the data with a warning, rather than hiding the diagnostic or throwing away the result.

- [ ] **OCR-122** `internal/tools/resources.go:141` _bug_
  In live mode `reachable < configured` marks the whole probe as incomplete, but presence-only providers (postgres, minio, codex_pro) never set `reachable` — `probeProvider`'s default branch returns `reachable=false` with only a "presence-only provider; no live probe" note. So a fully configured Postgres/MinIO (or a claimed codex_pro) makes this condition true and the tool returns `Partial("some providers are not configured or not reachable")`, contradicting the provider's own `configured=true` result. The reachability check should only compare providers that actually support a live probe.

- [ ] **OCR-123** `internal/tools/security.go:134` _security_
  Raw store/OS errors are forwarded to the MCP client from both the artifact and input-path branches, leaking absolute server paths (e.g. `ReadArtifact`'s `*PathError`, the remote fallback wrapping the local path, `InputPath`/`ensureWithin`, and `os.Open` errors). Wrap them consistently in generic messages such as `"artifact %s is not readable"` / `"input %s is not readable"`, as the sibling branches already do.

- [ ] **OCR-124** `internal/tools/system.go:133` _performance_
  `Recommend` already embeds the task internally (for the embedding/knn/mb/llm scorers), and `ActivationSnapshot` embeds the *same* task again via `r.embedTask(ctx, task)` (see internal/routing/fly/events.go:35). With the OpenAI-backed embedder this means a second network round-trip on every `routing.fly_recommend` call whenever a store is configured, roughly doubling the latency/cost of the request path. Consider reusing the vector computed during `Recommend` (e.g. let the router expose the snapshot from the same embedding pass), or make the KC snapshot opt-in (an argument / separate tool) instead of always re-embedding.

- [ ] **OCR-125** `internal/tools/telegram.go:48` _bug_
  `result.Chunks` is not a reliable "something was delivered" signal. In `telegram.SendMessage` the result is initialized as `SendResult{ChatID: chatID, Chunks: len(chunks)}` *before* the send loop (internal/telegram/telegram.go:240), so for any non-empty text `Chunks` is >= 1 on every error that occurs inside the loop — including a failure of the very first chunk where nothing was delivered. As a result this branch fires for essentially all post-validation send failures, returning a `Partial` response whose hint says "some chunks may already be delivered; verify before retrying" and whose `chunks` field reports the *total* chunk count instead of the delivered count, which can mislead callers into believing a message may have been sent (or into not retrying). Use `result.MessageID != 0` as the delivery signal (it is only set after a successful chunk), or make the client track delivered chunks separately.

- [ ] **OCR-126** `internal/tools/timeseries.go:147` _bug_
  Timestamps for the returned series (and the `start`/`end` fields below, plus the anomaly timestamps in `timeseries.detect_anomalies`) are formatted with `time.RFC3339`, which has no fractional-second component. `parseTimeValue` accepts nanosecond/microsecond epochs, so sub-second data is reachable; multiple distinct observations then collapse to the same string, producing visually duplicated/misleading points and making the descending string sort of anomalies ambiguous for ties. Use `time.RFC3339Nano` (or emit the raw value) to preserve the resolution.

- [ ] **OCR-127** `internal/tools/util.go:51` _bug_
  `commaIsThousands` counts a leading sign as part of the first group, so a 3-digit integer part with a sign fails the check: `parseNumber("-123,456")` splits into `["-123","456"]`, sees `len(parts[0]) == 4 > 3`, returns false, and the comma is rewritten as a decimal point, yielding `-123.456` instead of `-123456` (same for `"+123,456"`). Strip the sign before the group-length check so signed thousands values are parsed consistently with their unsigned counterparts.

- [ ] **OCR-128** `internal/tools/xlsx.go:69` _bug_
  Both recovery paths in xlsx.go hide a parse failure rather than marking the sheet degraded: a `readSharedStrings` failure is only a warning, so every `t="s"` cell keeps its raw shared-string index as the value; and `f.Open()` failures for `xl/_rels/workbook.xml.rels`, `xl/workbook.xml`, and styles are swallowed, so sheet names fall back to fabricated `SheetN` values. Record a warning and/or blank/flag the affected cells instead of returning plausible-but-wrong data.

- [ ] **OCR-129** `plugins/hackathonkit/hooks/codex-hooks.json:6` _maintainability_
  `id` (and the sibling `description`) are not part of the hook schema: a matcher group accepts only `matcher` + `hooks`, and a command hook accepts only `type` + `command` + `timeout`. Depending on how strictly the runner validates the manifest, these extra keys are either silently dropped (so `session:start` / `session:tool` / `session:stop` are dead configuration that misleads maintainers into thinking hooks are addressable by id) or cause the whole file to be rejected, which would disable all three lifecycle hooks at once. Either drop the `id` fields (the `description` strings are redundant with the top-level description) or confirm the runner's schema explicitly documents them.

- [ ] **OCR-130** `plugins/hackathonkit/hooks/codex-hooks.json:12` _bug_
  `timeout` is in seconds here, but the internal timeouts of the invoked script are larger: `binaryContext()` in `scripts/hooks.js` allows 15000 ms for the local binary and 30000 ms for the `docker compose ... --session-context` fallback. With a 10 s cap the harness kills `node hooks.js session-start` before its own best-effort logic can finish, so on a machine that falls back to docker (cold image pull/build — the repo's own `.codex/config.toml` budgets 180 s for that path) context injection silently never happens. Align the manifest bound with the script (raise it, or lower the script's internal timeouts to fit inside 10 s) so the documented behaviour is actually reachable.

- [ ] **OCR-131** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:6` _security_
  When `command -v` fails, the literal (slash-less) name is kept. `dirname` then returns `.` and `REPO_ROOT` becomes `$(pwd)/../../..`, i.e. the repository root is silently derived from the caller's current directory. Because the local branch `exec`s whatever it finds at the derived root, running the launcher from an untrusted directory can execute an unrelated/attacker-placed `bin/hackathonkit-mcp` (the arbitrary-execution vector H31 was raised for). Prefer a hard failure with a clear message when the launcher location cannot be resolved.

- [ ] **OCR-132** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:24` _bug_
  A present-but-not-executable `bin/hackathonkit-mcp` is treated as fatal, so the docker fallback a few lines below is never reached even when a working Docker engine is available. This is a realistic install state (artifacts extracted from a zip/tar that lost the execute bit, a Windows checkout consumed through WSL/`/mnt/c`, or a `cp` without `-p`), and the documented policy is "prefer the local binary, otherwise fall back to docker compose", so aborting contradicts it and leaves a recoverable install dead. Warn and fall through; only fail once Docker has also been ruled out.

- [ ] **OCR-133** `plugins/hackathonkit/scripts/hooks.js:222` _bug_
  The session file is written only at `session-start`, so its mtime/`updated_at` never advance while the session is active. A legitimate long-running session (> 12h, or started before a long gap) is therefore treated as expired by `isExpired()` and pruned/removed, after which its `stop` event is rejected as "unknown or foreign session id" — the session is silently leaked and its end never logged. Refresh the session file on each `tool-use` (touch `updated_at`/mtime) so the TTL reflects last activity rather than creation time.

- [ ] **OCR-134** `plugins/hackathonkit/scripts/hooks.js:386` _bug_
  `tool-use` resolves the session id via `lastActiveID()` without any ownership check, unlike the `stop` path which guards with `ownedByRoot(id)`. With two concurrent Codex sessions in the same repo, an event whose payload has no session id is attributed to whichever session file has the newest mtime, so events can be logged against the wrong session (and, if a foreign file ever lands in `sessions/`, against a foreign session). Validate the fallback before using it.

- [ ] **OCR-135** `scripts/coverage-check.sh:25` _bug_
  The EXIT/INT/TERM traps only remove `$PROFILE`, but `$TEST_LOG` and `$COVER_LOG` are created later and are cleaned up only on the explicit success/failure paths. If the script is interrupted (INT/TERM) while `go test` or `go tool cover -func` is running — the longest phases — those temp files are leaked. Since the INT/TERM handlers now `exit` immediately, the EXIT trap runs but still only knows about `$PROFILE`. Register all temp files with the cleanup function (declare them empty up front so the trap can safely run before they are assigned) so every exit path removes them.

- [ ] **OCR-136** `scripts/docker-check.sh:55` _security_
  The key/value redaction only matches an unquoted key directly followed by `[=: ]+`. Structured/JSON log lines quote the key, e.g. `"password":"secret"` or `"access_key": "..."`, so no match occurs (the separator class cannot cross the closing `"`) and the secret is dumped verbatim into diagnostics — the codebase logs structured entries via `observability.Log`, so this is a realistic output shape. Allow optional surrounding quotes on the key (and/or include `"` in the separator class) so quoted keys are covered too; keep the existing fail-closed behavior.

- [ ] **OCR-137** `scripts/docker-check.sh:69` _bug_
  `diagnostics` tears the infrastructure down unconditionally, ignoring `--keep-infra`. The flag's contract (and the header comment) is to leave postgres/redis/minio running, but the EXIT trap calls `stop_infra` on every non-zero exit — i.e. exactly when the user most wants to inspect the stack. As written, `--keep-infra` cannot be used to debug a failing run. Gate the teardown on `KEEP_INFRA`, or document that failures always clean up.

- [ ] **OCR-138** `scripts/docker-check.sh:121` _bug_
  `HACKATHONKIT_MINIO_PORT` is neither sourced from `.env` nor referenced by `docker-compose.yml` (the repo's only occurrence of this name is this line; compose publishes minio as the hardcoded `127.0.0.1:9000:9000`). The probe therefore reads a process-environment variable that has no influence on the actual published port: if a caller ever exports it (e.g. to a remapped port) the host probe hits a port nothing is listening on and the run aborts with "minio did not become ready" even though the stack is healthy; if it is set only in `.env` it is silently ignored. Derive the port from the real published mapping (or probe from inside the compose network) instead of an unrelated environment variable.

- [ ] **OCR-139** `scripts/doctor.ps1:10` _performance_
  On timeout the pending `BeginConnect` is never cancelled/observed, and the `finally` block closes `$async.AsyncWaitHandle` while the connect may still be completing — closing the wait handle of an in-flight async operation can raise `ObjectDisposedException` on the completion thread (and `EndConnect` is never called, leaving the operation unobserved). Abort the socket before returning and avoid manually closing the shared `AsyncWaitHandle`.

- [ ] **OCR-140** `scripts/doctor.ps1:80` _bug_
  Both the `doctor.ps1` skills-mirror check and the `install-prompts.ps1` `status` check compare only relative file names/existence (Compare-Object over path lists; Test-Path), so a file present in both trees but with different content is treated as up to date and content drift is never detected (Compare-Object is also case-insensitive by default). Compare content (e.g. relative path + hash) rather than names alone, and report orphaned/stale files present in the destination but absent from the source.

- [ ] **OCR-141** `scripts/doctor.sh:65` _bug_
  Two problems in the awk fallback: (1) `found` is never assigned, so `if (found) print "enabled"` is dead code and can never fire; (2) the matching is pure substring/regex on brace-split chunks (`parts[i] ~ /"name"[ \t]*:[ \t]*"hackathonkit"/` and `"enabled"[ \t]*:[ \t]*false`), so a nested object whose name/description contains `"name": "hackathonkit"`, or an `enabled:false` belonging to a different sub-object inside the same chunk, will misclassify the plugin and produce a wrong pass/fail. Prefer a real JSON parser (or at minimum match whole objects keyed on `"name":"hackathonkit"` with quoting) and remove the dead `found` guard.

- [ ] **OCR-142** `scripts/doctor.sh:121` _bug_
  The mirror check mixes two inconsistent listings: `find -mindepth 1 -maxdepth 1 -type d` counts hidden directories but ignores symlinked directories, while `ls -1d */` does the opposite (skips dot-dirs, follows symlinks to dirs). `SRC_COUNT`/`DST_SKILLS` are therefore not comparable with `SRC_NAMES`/`DST_NAMES`, so a hidden or symlinked directory in either tree yields a false pass or false failure. Also, comparing only aggregate `SKILL.md` counts plus top-level names cannot detect a nested layout that differs between plugin and fallback, and names containing whitespace/newlines break the `ls | sort` comparison. Use one consistent, NUL-safe listing for both trees.

- [ ] **OCR-143** `scripts/e2e-fly.ps1:22` _maintainability_
  Assigning to `$env` shadows/misreads PowerShell's `env:` provider (`$env:NAME`) and is easy to confuse with environment-variable access. Rename to something explicit such as `$result` (and update the `return` statement accordingly).

- [ ] **OCR-144** `scripts/e2e-fly.ps1:29` _test_
  HTTP checks assume a server is already listening at the hardcoded `http://127.0.0.1:8787`. Without a startup/readiness check (or a configurable base URL), these checks are environment-dependent and flaky, and every request incurs a 20s timeout when nothing is listening. Consider accepting the base URL as a parameter and probing `/` (or a health endpoint) once before running the page/module assertions.

- [ ] **OCR-145** `scripts/e2e-fly.ps1:46` _bug_
  The script persistently mutates the global routing mode to `fly` (and later back to `normal`) but provides no `try/finally`. Any terminating error or early `exit` between these points leaves the process/repo state stuck in `fly` mode, affecting subsequent runs. Wrap the fly-mode section (or the whole test body) in a `try { ... } finally { & $exe --set-routing-mode normal --repo-root $Root | Out-Null }` so the original mode is always restored.

- [ ] **OCR-146** `scripts/env-set.sh:47` _bug_
  Symlink resolution is single-hop and racy. If `$FILE` points to another symlink (a link chain), `FILE` becomes the intermediate link path; the temp file is created next to that intermediate link and `mv "$TMP" "$FILE"` replaces the intermediate symlink with a regular file instead of writing through to the final target, silently breaking the chain and updating the wrong file. If `readlink` fails/returns empty (e.g. the file is replaced concurrently, or is a symlink to a symlink that readlink cannot fully resolve), `FILE` keeps the original symlink path and the final `mv` replaces the symlink itself. Resolve symlinks iteratively (with a hop limit) or use `readlink -f`/`realpath` when available, and refuse to proceed when resolution fails.

- [ ] **OCR-147** `scripts/env-set.sh:64` _bug_
  Mode/owner restoration is best-effort and the failure path still reports overall success. If `chmod "$MODE"` fails, the file keeps the temp file's 0600 mode; if the original was group-readable (e.g. 0640) consumers break, and the promised 'permissions preserved' guarantee is silently violated. There is also a TOCTOU window: the mode/owner are read before the rewrite, so a concurrent chmod/chown between `stat` and `mv` is lost. Consider failing hard (non-zero exit) when mode/owner cannot be restored, or applying the mode to the temp file before the rename.

- [ ] **OCR-148** `scripts/env-set.test.sh:7` _bug_
  The INT/TERM trap only removes the temp dir and does not exit. In POSIX sh a trapped INT/TERM runs the handler and then resumes the script, so Ctrl-C or a CI timeout would clean up `$TMP` but keep running the remaining assertions, and the already-fired trap state means the run no longer aborts as intended. Note the tested helper gets this right (`on_signal_env_set` calls `exit 130`); the test suite should mirror it. Split the EXIT and signal traps and exit from the signal handler.

- [ ] **OCR-149** `scripts/env-set.test.sh:92` _security_
  The injection test uses a fixed, world-writable marker path (`/tmp/hackathonkit-env-inject-marker`). Any other process/user on the host (or a parallel run of this suite) can pre-create that file, turning the `[ -f ... ]` assertion into a spurious failure; the shared path is also a symlink/pre-creation hazard even though it lives in the sandbox. Since a `$TMP` scratch dir already exists, use a marker inside it. Additionally, placing `rm -f` between the helper call and the sourcing check means a payload executed during the helper's *write* path would be silently masked — clear the marker before invoking the helper if you want to cover both phases.

- [ ] **OCR-150** `scripts/gen-mcp-manifest.ps1:19` _bug_
  A caller-supplied `$Root` is accepted verbatim (only the default branch resolves an absolute path in `gen-mcp-manifest.ps1`), and `write-install-state.ps1` validates only a weak marker without canonicalising. A relative value (`-Root .`, `./foo`) or a path containing `..`/symlinks passes `Test-Path` and is spliced into the manifest/state, redirecting the write outside the intended repository. Canonicalize `$Root` to an absolute real path after validation (and strengthen the repository marker check) in both scripts.

- [ ] **OCR-151** `scripts/gen-mcp-manifest.ps1:43` _maintainability_
  The post-render guard only knows the two placeholders that exist today, so a template that introduces a new token (e.g. `{{SOMETHING_ELSE}}`) is not detected and ships an unrendered manifest (the JSON check cannot catch it, it is a valid string). Match placeholders generically.

- [ ] **OCR-152** `scripts/gen-mcp-manifest.ps1:52` _bug_
  `Test-Path` followed by `File.Replace`/`File.Move` is a check-then-act race: another process can create the destination before the `Move` (throws, destination exists) or remove it before the `Replace` (throws, destination missing). The whole update is then lost even though the previous manifest is gone. Re-check and retry once instead of relying on a single probe.

- [ ] **OCR-153** `scripts/gen-mcp-manifest.sh:4` _bug_
  Root derivation/validation issues: (1) when the script is invoked via PATH with a slashless `$0` (e.g. `gen-mcp-manifest.sh`), `dirname "$0"` returns `.`, so the default resolves to the parent of the *current working directory* rather than the directory containing the script; (2) a caller-supplied `$1` is used verbatim, so a relative path passes the `[ ! -d ]` check and is written into the manifest as a relative path, breaking the launcher that expects an absolute root; (3) `[ -z "$ROOT" ]` is effectively dead — `${1:-...}` already substitutes the default for an empty `$1`, and with `set -e` a failing `cd` in the command substitution terminates the script before the diagnostic on the next line can run.

- [ ] **OCR-154** `scripts/gen-mcp-manifest.sh:21` _bug_
  The escaping pipeline does not do what it says. `tr '\r\n\t\f\v' '    '` maps TAB to a space (the replacement set has 4 spaces, so the 5th and following input chars are padded with the last char = space), which means the trailing `sed -e 's/<TAB>/\\t/g'` can never match — it is dead code and never emits the `\t` escape implied by the comment above. Worse, any other C0 control byte (e.g. `\b`, or bytes < 0x20 not in the five listed) is passed through unescaped and produces invalid JSON, silently corrupting the manifest on systems where the optional python3 validation is missing. Escape/reject every control character, or at minimum drop the unreachable stage.

- [ ] **OCR-155** `scripts/gen-mcp-manifest.sh:54` _bug_
  JSON validation is silently skipped when python3 is unavailable, so on such systems only the placeholder grep guards the output and a malformed rendered manifest (e.g. produced by the escaping gap above) is installed as `.mcp.json`, breaking every MCP client that reads it. Consider warning loudly when no validator is present, or providing a portable fallback so invalid output is never published.

- [ ] **OCR-156** `scripts/install-prompts.ps1:20` _bug_
  `$env:CODEX_HOME` is consumed verbatim and `$HOME` is assumed non-null. On environments where `$HOME` is unset (e.g. some service/containers or non-interactive Windows contexts), `Join-Path $HOME ".codex"` throws a null-binding error; because `$ErrorActionPreference = 'Stop'` this aborts the script with an opaque stack trace instead of a clear message. A relative or unexpected `CODEX_HOME` also silently redirects writes to an unintended tree. Validate both inputs and fail explicitly.

- [ ] **OCR-157** `scripts/install-prompts.ps1:24` _bug_
  `$Sources` is never validated. If the directory exists but contains no `*.md` files (e.g. a sparse/filtered checkout), `install` prints `installed 0 prompt(s)` and `status` prints `installed: ...`, both exiting 0 and masking a broken installation. Additionally, `Get-ChildItem -Filter *.md` without `-File` can return directories whose name matches the filter, which `Copy-Item` would then recurse into. Filter to files and fail when nothing is found.

- [ ] **OCR-158** `scripts/install-prompts.ps1:38` _bug_
  TOCTOU race in the remove loop: the destination is only checked with `Test-Path`, then hashed. If the file is deleted, replaced, or becomes locked between `Test-Path`, `Get-FileHash` and `Remove-Item`, `Get-FileHash`/`Remove-Item` raise a terminating error (due to `$ErrorActionPreference = 'Stop'`), aborting the loop mid-run and leaving a partially-removed, unclear state. Handle the race locally instead of letting it terminate the whole run.

- [ ] **OCR-159** `scripts/install-prompts.ps1:39` _bug_
  Locally modified or outdated prompt files are skipped silently, yet the script still prints `removed unmodified prompts from ...` and exits 0. Users are told removal succeeded while stale/divergent files are left behind in the prompts directory. Emit a warning (and/or a skipped counter) for every file that is kept because it differs from the source.

- [ ] **OCR-160** `scripts/install-prompts.sh:26` _bug_
  `install` overwrites any pre-existing file in `$DEST_DIR` unconditionally, so a rerun via `setup.sh`/`repair.sh` silently destroys local edits a user made to their Codex prompts (the `remove` branch deliberately protects such edits with `cmp`, so the two paths are inconsistent). Consider detecting a diverged destination and warning (or refusing) before clobbering it, so the destructive action is at least visible. Note also that if the destination name is unexpectedly a directory/symlink, `cp` will write *into* it or follow the link.

- [ ] **OCR-161** `scripts/install-prompts.sh:45` _bug_
  `status` only checks that a file exists, never that it matches the source (even though the `remove` branch already shows `cmp -s` is the intended comparison). After the plugin updates a command, `status` still prints `installed` and exits 0, and `scripts/doctor.sh` will report the prompts as healthy while the installed copies are stale. Compare content here too (the wording/message should then say "out of date" rather than "missing").

- [ ] **OCR-162** `scripts/plugin-check.ps1:57` _security_
  The containment guard is weaker than it claims: (1) `GetFullPath` only normalises the string, it does not resolve symlinks/junctions, so a link placed inside `$sourcePath` that points outside it still passes; (2) the comparison is `OrdinalIgnoreCase`, which on case-sensitive file systems (Linux/macOS PowerShell Core) lets `/opt/Hackathon Agent Kit/...` pass a `/opt/hackathonkit/` prefix and execute a binary outside the plugin; (3) only `command` is validated, the unbounded `$server.args` are passed straight through, so containment can be bypassed by arguments even when the binary path is legit. Resolve the real target path (e.g. `GetFinalPathNameByHandle`/`Resolve-Path -Resolve` plus re-check after resolving links) and use `Ordinal` on case-sensitive platforms; consider validating args too.

- [ ] **OCR-163** `scripts/plugin-check.ps1:68` _bug_
  `New-Item` runs under `$ErrorActionPreference = "Continue"`, so if the temp directory cannot be created the failure is silent; the script then starts the job anyway, the job's `Set-Location`/`Set-Content` fail, no exit-code file is produced, and the check can be reported as a PASS (see the `$exitCode` default of 0). Assert the creation succeeded before continuing. Note the same `Continue` preference means other non-terminating cmdlet failures (e.g. `Get-Content`, `Set-Content`) don't abort, so state can silently be missing later.

- [ ] **OCR-164** `scripts/plugin-check.ps1:124` _bug_
  The stream framing is fragile: any non-JSON line written to stdout (progress/log noise) is prepended to `$buffer` and, because the buffer is only cleared when the whole thing parses or exceeds 1 MB, every subsequent valid response becomes unparseable and the run reports a false "missing JSON-RPC response". Likewise two JSON values on a single line can never be parsed by a plain `ConvertFrom-Json`, and hitting the 1 MB cap silently discards buffered data. Consider framing strictly by line (`ConvertFrom-Json` per line, tolerating partial trailing JSON) and dropping non-JSON lines instead of accumulating them forever.

- [ ] **OCR-165** `scripts/plugin-check.sh:143` _maintainability_
  `proc.returncode` is never checked, and the captured `stderr_tail` is only surfaced in the missing-ids branch. A server that emits all three responses and then exits non-zero (or reports `isError` on the tool call, or exits before writing anything) produces a failure/diagnostic without the server's stderr, making remote CI failures hard to triage. Verify `proc.returncode` (including negative/signal values) and append `stderr_tail` to the error/isError/exit-code failure messages, not just the missing-id message.

- [ ] **OCR-166** `scripts/quality-gate.sh:23` _bug_
  Non-POSIX reliance on `test -nt` (and `find -quit` on line 18) under a `#!/usr/bin/env sh` shebang. On a strict `test` implementation `[ "$extra" -nt "$BIN" ]` is an unknown binary operator: it returns an error status, which is silently evaluated as "not newer", so an updated go.mod/go.sum would *not* force a rebuild and the gate would run evidence against a stale binary — exactly the failure this check exists to prevent. Using `find` for the comparison keeps the check portable (the script already depends on `find`).

- [ ] **OCR-167** `scripts/quality-gate.sh:60` _bug_
  An executable `$BIN` exists but `go` is absent: USE_BIN was forced to 0 on line 33, so control never reaches the `exec` fast path and the script fails here with exit 2 and the message "no runtime available" — even though a runnable binary is present. The diagnostic is misleading and the prebuilt-binary workflow is unusable in toolchain-less environments (e.g. release tarballs/CI images without Go). If freshness cannot be verified, prefer running the existing binary with an explicit warning, or at least correct the message.

- [ ] **OCR-168** `scripts/repair.ps1:33` _maintainability_
  `$profile` is a PowerShell automatic variable (`$PROFILE`, the path to the profile script). Assigning to it here silently shadows/overwrites that built-in variable inside the script scope, which is a classic PowerShell pitfall and confusing for maintainers. Rename to `$installProfile` (and update the `-Profile $profile` call and the reads below).

- [ ] **OCR-169** `scripts/repair.sh:28` _bug_
  Two problems with this fallback: (1) on hosts where `timeout` is unavailable (e.g. macOS) `docker info` runs without any time limit and can hang the repair indefinitely; consider a portable bounded wait or skipping the probe when it cannot be bounded. (2) Daemon liveness alone does not prove the required images exist — with `RUNTIME=docker` the Go rebuild is skipped entirely, so a missing image still yields a successful repair and an unavailable MCP runtime. Verify the images (e.g. `docker image inspect`) or the actual runtime path before declaring success.

- [ ] **OCR-170** `scripts/repair.sh:33` _bug_
  The whole decision (probe binaries → rebuild → move into `bin/`) is not protected against concurrent runs: two parallel `repair.sh` invocations can both take the build branch and race on `mktemp`/`mv` into `bin/`, and `write-install-state.sh` is invoked afterward against the same file, so state can be torn or conflict. Additionally, replacing binaries in place while an MCP/demo process is running causes version skew until it is restarted. Consider a lock (e.g. `mkdir "$ROOT/.repair.lock"` with cleanup, or `flock`) and note that running processes must be restarted.

- [ ] **OCR-171** `scripts/repair.sh:60` _bug_
  Install state is persisted before the Codex plugin section, which can still `exit 1` afterwards. A failed run therefore leaves `configs/install-state.json` claiming a completed repair with the resolved profile/runtime. Move this call after the Codex verification (or make that section non-fatal and surface the failure at the end) so persisted state only reflects a successful repair.

- [ ] **OCR-172** `scripts/setup.ps1:46` _security_
  The detection pattern only requires *any* non-empty value after `HACKATHONKIT_DEMO_TOKEN=`, so a placeholder shipped in `.env.example` (e.g. `changeme`/`your-token-here`) is treated as a real token and no strong token is generated. Since this value is what protects the demo API, validate against known placeholders (or regenerate whenever the value looks non-random) instead of accepting it blindly.

- [ ] **OCR-173** `scripts/setup.ps1:60` _bug_
  Both `.env` rewriting in `setup.ps1` and `install-state.json` writing in `write-install-state.ps1` use `Set-Content` in place: it truncates the target first (an interrupted/failed write corrupts or destroys the existing file) and, on Windows PowerShell 5.1, uses ANSI or UTF8-with-BOM encoding (mangling existing UTF-8, and strict JSON parsers reject a leading BOM). Write with an explicit BOM-less encoding to a temp file in the same directory, atomically replace/move it into place, and verify the result.

- [ ] **OCR-174** `scripts/setup.ps1:68` _bug_
  Windows-only separators are passed to `Join-Path` (here and for `install-prompts.ps1`, `gen-mcp-manifest.ps1`, `write-install-state.ps1`, as well as `.agents\skills` / `.claude\skills`). On PowerShell for Linux/macOS a backslash is not a path separator, so these paths will not resolve — yet the script explicitly checks `$env:OS -eq "Windows_NT"` above, implying cross-platform intent. Use forward slashes or per-segment `Join-Path`. Related: judging helper scripts purely by `$LASTEXITCODE` is unreliable if a helper is ever a shim/function that throws instead of calling `exit`.

- [ ] **OCR-175** `scripts/setup.ps1:92` _bug_
  Recursive deletes that lack a reparse-point guard can follow junctions/symlinks (Windows PowerShell 5.1 `Remove-Item -Recurse` descends into directory links), destroying content outside the repository. `setup.ps1`'s skills cleanup guards only the skill directory itself (not links inside it), and `uninstall.ps1`'s generic `bin`/`data\control`/`plugins\hackathonkit\.mcp.json` branch has no guard at all (unlike its skills branch). Additionally, `-ErrorAction SilentlyContinue` masks permission-denied/locked failures while still printing the success message. Apply the reparse-point guard consistently and report failures.

- [ ] **OCR-176** `scripts/setup.ps1:107` _bug_
  `docker info` only proves the daemon is reachable; it does not prove the `compose` plugin exists or that the version supports `--wait` (Compose v2.17+). A host with a working `docker` CLI but a missing/old compose will take the docker branch and hard-fail in `Fail` instead of falling back to the Go build. Probe `docker compose version` (and `--wait` support) before committing to `$runtime = "docker"`.

- [ ] **OCR-177** `scripts/setup.ps1:145` _bug_
  When `marketplace add` fails the script still runs `codex plugin add hackathonkit@hackathonkit-local`, which is guaranteed to fail against an unregistered marketplace, leaving a half-configured plugin state that is only surfaced via the aggregated warning text. Skip the `plugin add` step (or fail) when the marketplace registration did not succeed.

- [ ] **OCR-178** `scripts/setup.sh:55` _security_
  The stat-failure branch is a no-op security hole. When neither `stat -c` nor `stat -f` works, `ENV_MODE` is set to `600`, which then matches the `*00` glob in the `case` below, so the `chmod 600` remediation is **skipped** — the exact opposite of the emitted "forcing 600" message. Since `.env` holds `OPENAI_API_KEY`/`POSTGRES_PASSWORD`/`TELEGRAM_BOT_TOKEN`, an unreadable mode must never be treated as already-safe. Force the `chmod` in that branch instead of feeding a fake mode into the `case`.

- [ ] **OCR-179** `scripts/setup.sh:113` _bug_
  On a partial Docker failure the script only prints `docker compose ps` and flips `RUNTIME` to `binary`, leaving whatever containers were already started running. Those containers keep binding the compose ports (postgres 5432, redis 6379, minio 9000), so the subsequent local-binary path can fail to start or silently talk to the half-started stack, and `write-install-state.sh` records `binary` while containers are in fact up — an inconsistent state that the next run cannot reason about. Tear the partial stack down (or at least stop it) before falling back, e.g. `docker compose -f "$ROOT/docker-compose.yml" down` (without `-v`, so data volumes are preserved).

- [ ] **OCR-180** `scripts/smoke.ps1:13` _bug_
  The request payload is LF-joined and written with `-NoNewline`, so the final `tools/call` message has no terminating line break. A line-framed server that only dispatches a request once it sees a newline (or a server expecting CRLF) may never answer request id 3, causing a false failure. Append a trailing line terminator to the payload.

- [ ] **OCR-181** `scripts/smoke.ps1:25` _bug_
  `Start-Process -ArgumentList` joins the array elements with spaces but does not add quoting, so a `$root` (derived from the script path) containing spaces yields truncated arguments. This affects both the local-binary invocation (`--repo-root $root`) and the docker invocation (`compose -f (Join-Path $root 'docker-compose.yml')`). Quote the path values (and ideally validate the path) before passing them.

- [ ] **OCR-182** `scripts/smoke.ps1:127` _bug_
  `$ids` is populated in arrival order and compared verbatim against "1,2,3", but JSON-RPC does not guarantee responses are emitted in request order (and the id set, not its ordering, is what matters here). Additionally, duplicate ids silently overwrite `$byId`. Validate the id set instead of the sequence.

- [ ] **OCR-183** `scripts/smoke.sh:30` _bug_
  `run_binary` captures stdout synchronously with no timeout. If the MCP server hangs (e.g. waiting on stdin/blocking init), CI will stall indefinitely. Wrap the invocation in a timeout so the smoke test fails fast.

- [ ] **OCR-184** `scripts/smoke.sh:130` _bug_
  The fallback id parser only recognises unquoted non-negative integer ids, and `sort -u` merges duplicates. A response with a string id, or duplicate/misordered responses, can pass this check even though the handshake is invalid. Since this branch runs precisely when python3 is unavailable, the assertion should at least verify the raw response count and reject duplicate ids rather than de-duplicating them.

- [ ] **OCR-185** `scripts/smoke.test.sh:30` _test_
  The fake repo has no `cmd/` or `internal/` directories, but smoke.sh's `--auto` freshness probe is `find "$ROOT/cmd" "$ROOT/internal" -name '*.go' -newer "$ROOT/bin/hackathonkit-mcp" 2>/dev/null | head -n 1`. With those paths missing, `find` fails (silently swallowed by `2>/dev/null`) and the pipeline still exits 0 because of `head`, so `NEWEST` is always empty and the "stale binary -> compose" branch is never exercised. The `--auto prefers the local binary` assertion therefore passes even if the freshness comparison were broken. Consider creating `$REPO/cmd`/`$REPO/internal` with a `.go` file newer than the stub binary and asserting that `--auto` then selects the docker stub.

- [ ] **OCR-186** `scripts/smoke.test.sh:37` _maintainability_
  The heredoc is unquoted, so every `$`/backtick in the generated stub is expanded by this shell unless escaped. Only `$*`, `$(...)` and `$MARKER` are escaped today; the JSON payload happens to contain no `$`, so this works by accident. If the payload is ever edited to include `$` or a backtick (or if a helper variable is referenced), the stub is silently corrupted and the tests may pass for the wrong reason. Either quote the delimiter (`<<'EOF'`) and substitute the two variables explicitly, or add a comment warning that all `$` in the body must be escaped.

- [ ] **OCR-187** `scripts/smoke.test.sh:63` _maintainability_
  These assertions are tightly coupled to smoke.sh's exact command line and to the hard-coded request count (`stdin=3`, and `run --rm -T hackathonkit-mcp` ordering). Any legitimate change to smoke.sh (extra flag, reordered args, an added/removed JSON-RPC request) makes several tests fail with a confusing message rather than a real regression. Prefer deriving the expected stdin count from smoke.sh's `REQUESTS` (e.g. `grep -c . ` on the heredoc block) and matching only the stable transport tokens instead of the full argument string.

- [ ] **OCR-188** `scripts/smoke.test.sh:93` _test_
  Coverage gap: there is no case for `--auto` when *neither* the local binary nor the docker CLI is available, which is the third branch of smoke.sh's auto path (exit 1 with the "no MCP runtime available" diagnostic). Adding it would protect the most user-visible failure mode.

- [ ] **OCR-189** `scripts/sync-harness.ps1:49` _bug_
  The `.claude/skills` mirror swap is not serialized. `sync-harness.sh` acquires a `CLAUDE_LOCK` (and installs interrupt traps) around exactly this swap, and the sibling `sync-skills.ps1` does the same for `.agents/skills`, but this script takes no lock. Two concurrent runs (e.g. CI plus a manual invocation) can interleave the move-aside/move-in sequence: one process can move the live mirror to its `.old.<pid>` while the other is mid-swap, so a `Move-Item` fails and the rollback/backup-delete logic in `finally` can run against a mirror that was concurrently replaced. Add the same PID-file lock used by `sync-skills.ps1`, or delegate to a shared helper.
  
  Separately, the header comment claims the mirror is swapped "atomically", but the live `.claude/skills` is moved aside before the temp directory is moved in, so a reader can observe the directory as missing between the two renames. The comment overstates the guarantee.

- [ ] **OCR-190** `scripts/sync-harness.ps1:54` _bug_
  Both `sync-harness.ps1` and `sync-skills.ps1` verify the mirror too shallowly: one compares only the count of top-level directories, the other only their names, so a same-count-but-different set of skills, a renamed/added+removed skill, or a copy that drops/corrupts files inside a skill directory (or top-level files) passes verification and a partial mirror is reported as success. Compare the full relative file set (and ideally hashes/sizes) rather than counts or top-level names.

- [ ] **OCR-191** `scripts/sync-harness.sh:47` _bug_
  The pid file write failure is silently ignored (`|| true`). An empty/missing `pid` makes `claude_lock_stale` fall back to the mtime heuristic, so any run longer than 30s (large skills tree, slow disk) is declared stale and its lock is stolen by a concurrent run — the exact race the lock exists to prevent. Fail hard if the pid cannot be written, or at least keep the mtime threshold above the realistic worst-case copy time.

- [ ] **OCR-192** `scripts/sync-harness.sh:111` _bug_
  Unlike the skills swap above, the command mirrors are copied directly into the destination directories with no lock, no temp-dir staging and no rollback, and the lock has already been released (and traps cleared) at this point. Two concurrent runs can interleave `cp` and expose partially written files, and a failure mid-loop leaves some commands updated while others are stale. Files deleted from the plugin `commands/` source are also never pruned, so the mirrors drift. Stage into a temp dir and swap (or prune obsolete files) under the same lock.

- [ ] **OCR-193** `scripts/sync-harness.sh:160` _bug_
  `json_valid` is invoked for every entry of `JSON_MANIFESTS` without checking existence, so a *missing* JSON manifest is counted in `MISSING` (by the ADAPTERS loop) and again in `INVALID` (here), producing misleading diagnostics ("1 missing, 1 invalid" for a single absent file). Skip files that do not exist so each failure mode is reported once.

- [ ] **OCR-194** `scripts/sync-skills.ps1:70` _maintainability_
  The empty `catch {}` hides why the stale-lock eviction failed, and `continue` re-enters the loop immediately without any sleep, producing a tight busy loop for the remaining iterations (up to ~100 rapid retries). Log the failure reason (e.g. `Write-Verbose $_.Exception.Message`) and sleep before retrying so a transient Move/Remove failure does not spin the CPU and immediately exhaust the attempts.

- [ ] **OCR-195** `scripts/sync-skills.sh:84` _bug_
  These handlers are installed only *after* the lock has been acquired (`LOCKED=1` above), and they replace the earlier `trap cleanup_tmp EXIT` / `on_signal`. A signal arriving in that window runs the old handler, which removes `$TMP` but leaves `$LOCK` behind — the permanent-lock situation the reclamation code is supposed to handle. Define a single cleanup routine that releases both `$TMP` and `$LOCK` (guarded by `LOCKED`) and install its traps once, before the acquisition loop, instead of redefining handlers mid-script.

- [ ] **OCR-196** `scripts/sync-skills.sh:91` _bug_
  The rollback here is best-effort and unchecked. If it fails (permissions, cross-device move, AV interference), control falls through to `exit 1`, which triggers the EXIT trap `finish`: it retries the same `mv`, and on failure proceeds to `rm -rf "$OLD" "$TMP"`, destroying the only remaining copy of the previous mirror and leaving `$DST` absent. Verify the restore and abort loudly (keeping `$OLD`) when it cannot be performed; do not delete `$OLD` until the swap has been confirmed.

- [ ] **OCR-197** `scripts/uninstall.ps1:36` _bug_
  `$missingMarkers` is evaluated before the `-not $root` guard. If `$root` ends up null/empty (e.g. `Split-Path` yields nothing), `Join-Path $root $_` throws a terminating parameter-binding error and, because `$ErrorActionPreference = "Stop"`, the script aborts with an unfriendly error instead of the intended `Fail` message. Guard `$root` before using it in `Join-Path`.

- [ ] **OCR-198** `scripts/uninstall.ps1:52` _bug_
  The uninstall summary messages do not reflect failures: the success message (`removed hackathonkit plugin and marketplace`) is printed even when one or both codex commands failed (`$codexFail = 1`), and the final `Fail` summary mentions only path failures even though `$codexFail` is part of the condition. Report both counts/actual outcome instead of unconditionally claiming success.

- [ ] **OCR-199** `scripts/uninstall.sh:53` _bug_
  Dry-run overstates the effect of `--yes`. In dry-run mode every shipped skill name is reported as "would remove" as long as the mirror directory exists, but the apply path removes a mirror entry only when `[ -d "$mirror/$name" ]` is true *and* `diff -r` shows it is byte-identical; modified copies are explicitly preserved (see the `keeping modified ...` branch). So a preview can advertise removals that will never happen, and the user gets no hint about which entries will be kept. Mirror the apply-path conditions in the dry-run branch.

- [ ] **OCR-200** `scripts/uninstall.sh:65` _bug_
  `codex plugin remove` / `marketplace remove` failing is treated as a hard failure that forces exit 1, but these commands also fail when the plugin/marketplace was simply never registered — a normal state for a partial/uninstalled setup. That turns a benign no-op into a reported failure. Consider distinguishing "not installed" from a real removal error (e.g. check registration first, or only warn), so the exit status reflects an actual problem.

- [ ] **OCR-201** `scripts/uninstall.sh:86` _bug_
  Prompt removal failures are silently swallowed: output is discarded and `|| true` guarantees success, so a missing/crashing `install-prompts.sh` leaves stale `/verify` prompts behind while the script still exits 0 and prints the success message. Count the failure into `FAILURES` (and let the diagnostic through) so a partial uninstall is reported.

- [ ] **OCR-202** `scripts/verify-command.test.sh:67` _test_
  The no-python3 fallback only greps for `commands/plan.md`, so a manifest that omits research/build/evaluate/verify still passes, while the python3 branch would correctly fail it. Manifest coverage silently degrades on minimal environments — check all five command paths in the fallback too.

- [ ] **OCR-203** `scripts/verify-command.test.sh:98` _test_
  After appending 'custom edit' to verify.md the test only asserts the file still exists; it never verifies the user's edit survived, so a `remove` that truncates or rewrites the file would still pass and the data-integrity behavior of the installer is left unproven. Assert the modified content is retained (e.g. grep for the appended text).

- [ ] **OCR-204** `scripts/write-install-state.ps1:30` _bug_
  Version extraction degrades silently. If `plugin.json` parses but has no (or a falsy/empty) `version`, `$Version` quietly stays `unknown` with no warning, so the recorded version can be misleading without any signal. The `.sh` sibling emits a warning in this case. Emit a warning on the missing-version path as well (and be aware the `[string]` cast will coerce a non-string value such as a number/array rather than rejecting it).

- [ ] **OCR-205** `scripts/write-install-state.ps1:42` _bug_
  Silent zero for skills. When the `.agents\skills` mirror is absent, `$Skills` stays 0 and no diagnostic is emitted, so a broken/partial install is recorded as a valid state (the `.sh` sibling warns in this case). Also `Test-Path $mirror` uses the positional `-Path` parameter, so wildcard characters (`[`, `]`) in `$Root` are interpreted as patterns — every other probe here uses `-LiteralPath`. Warn when the mirror is missing and use `-LiteralPath` for consistency.

- [ ] **OCR-206** `scripts/write-install-state.ps1:53` _bug_
  The component flags mix two different notions of "installed". `hooks`/`learning` only probe the shipped plugin tree (`plugins\hackathonkit\...`), `agents` probes the user's `.codex\agents`, while `skills` counts the `.agents\skills` mirror. As a result the emitted state can be internally contradictory — e.g. `hooks = true` and `learning = true` while the mirror is empty (`skills = 0`) — and because the hooks/learning probes only test files that the plugin always ships, those flags are effectively still hardcoded `true`, which is exactly what the "derive components from what is actually present" comment intended to avoid. Align the probes on a single definition of "installed" (user-visible targets), or document what each field means.

- [ ] **OCR-207** `scripts/write-install-state.sh:65` _bug_
  The `components` block is hardcoded and completely ignores the validated `$PROFILE`. A `--profile core` install therefore writes exactly the same manifest (`hooks/agents/learning = true`, all four adapters) as a `full` install, so downstream consumers (`doctor.sh`, `repair.sh`, resource-aware planning) cannot tell what was actually installed. Either derive these flags/adapters from real filesystem evidence (or from a caller-supplied parameter as the PowerShell sibling was changed to do), or stop advertising a component set that is not verified.

## LOW

- [ ] **OCR-208** `.claude-plugin/marketplace.json:14` _maintainability_
  The plugin version is hardcoded here (0.16.31) and duplicates the version in `.claude-plugin/plugin.json` (also 0.16.31). Nothing in the toolchain keeps the two in sync — `scripts/sync-harness.sh` only validates that this file parses, and the README only instructs bumping the plugin version. They match today by luck; once they drift, marketplace resolution/upgrade detection will advertise a stale version. Consider dropping the per-plugin `version` (letting the plugin manifest be the single source of truth) or adding a check that compares both files.

- [ ] **OCR-209** `.codex/agents/evaluator.toml:20` _maintainability_
  The criterion maxima are hard-coded here (25/20/15/20/20) and described as "a convenience copy" of `rubric.md`. If `rubric.md` changes, these numbers silently drift and the model will emit scores that violate the source-of-truth maxima, which `eval.aggregate` then rejects. Either derive the maxima from the rubric at load time, or add an explicit final cross-check step against `rubric.md` before emitting the report so drift surfaces deterministically rather than as an aggregate failure.

- [ ] **OCR-210** `.codex/agents/researcher.toml:7` _maintainability_
  The comment says `model` is left to the harness default by design and in the same sentence says to pin it here to freeze behavior, but no `model` key exists to pin. A maintainer cannot tell whether the default is intentional or a TODO, and could add or remove `model` thinking they are restoring intended behavior. Either add the `model` key or rewrite the comment so it does not promise freezing that the config does not provide. The same issue appears in both `researcher.toml` and `reviewer.toml`.

- [ ] **OCR-211** `.codex/agents/reviewer.toml:21` _maintainability_
  These two bullets restate the same rule ("do not run tests or builds") with slightly different forbidden lists and two different unverified labels ("an unverified claim" vs "unverified - read-only"). Duplicated, partially-overlapping constraints make the actual forbidden set ambiguous (bullet 1 forbids only tests/builds/network fetches and explicitly permits everything else that is "read", which sits awkwardly next to "You are read-only"). Merge into a single canonical rule with one label.

- [ ] **OCR-212** `.codex/agents/reviewer.toml:25` _maintainability_
  The contract asks for "structured fields" but never names a concrete format, unlike the sibling evaluator profile which pins the exact JSON shape. As written, each review can emit a different structure, so the contract is not actually checkable despite the stated goal. Name the encoding (e.g. a single JSON array of finding objects) and say how a finding with no single line (`line`) is represented, so downstream tooling can validate it.

- [ ] **OCR-213** `.codex/config.toml:1` _maintainability_
  `#:schema` is written as an ordinary TOML comment. It only takes effect if the consuming tooling recognizes this specific directive form; otherwise this file receives no schema validation at all, and TOML mistakes (e.g. root-level keys captured by a table) go entirely unchecked. Verify the directive is honored by your editor/CI, and add an explicit parse/validate step for this file so validation does not silently fall away.

- [ ] **OCR-214** `.codex/config.toml:12` _maintainability_
  Linking `$theme-research` behavior to a `web_search` switch that is hardcoded to "disabled" means external verification is off by default across every environment; the agent is instructed to treat external facts as gaps rather than verify them. If this file is the committed default for all users, consider whether "live" (or per-environment override) is the intended baseline, since a disabled default can silently degrade research quality without any signal.

- [ ] **OCR-215** `.dockerignore:5` _maintainability_
  `!/.env.example` is redundant: `!**/.env.example` on the next line already matches the root file because `**` can expand to zero directories. The leading-slash form also differs from the rest of the file's convention, so keeping only the canonical `!**/.env.example` reduces confusion.

- [ ] **OCR-216** `.dockerignore:12` _maintainability_
  `**/*.md` excludes every markdown file in the context and only the root `README.md` is re-included. That is fine for plain docs, but any nested markdown that tooling depends on (e.g. package-level READMEs referenced from build/packaging metadata, or files pulled in by `go:embed`) would silently disappear and break the build. Confirm this breadth is intentional; if only documentation is meant to be dropped, consider `**/*.md` + explicit negations for the paths that must remain.

- [ ] **OCR-217** `.gitattributes:18` _maintainability_
  `.obj` (Wavefront), `.stl` (ASCII variant) and `.gltf` (JSON-based glTF) are frequently text formats. Marking them `binary` sets `-diff -merge -text`, which disables diffs, blame and merge resolution for revieweable assets. If these are intended to be binary-only, please confirm; otherwise keep them as text (or use `-diff` alone) so that text-based assets remain reviewable/mergeable.

- [ ] **OCR-218** `.gitattributes:26` _maintainability_
  These explicit `text eol=lf` entries duplicate the global `* text=auto eol=lf` default and add no effect (same value is applied), so they are pure maintenance burden — every new text extension must be kept in sync in two places. Only the CRLF overrides below actually change behaviour; consider removing the redundant LF lines and keeping just the CRLF block.

- [ ] **OCR-219** `.gitignore:7` _bug_
  The re-inclusion of the example env file is root-anchored (`!/.env.example`), so it only un-ignores the top-level file. A future nested `.env.example` (e.g. under a sample/example directory) would stay ignored by `.env.*` and be silently lost. Use an unanchored negation (or add a matching one per nested location) if nested examples are expected.

- [ ] **OCR-220** `.gitignore:11` _bug_
  These patterns are unanchored, so they match at *any* depth, not just the repository root. `*.log` in particular can silently hide a legitimate fixture (e.g. a sample log under a skill's `references/` or a test data directory), and `coverage/` / `htmlcov/` would swallow a same-named fixture directory anywhere in the tree. If only root-level build/test artifacts are meant to be excluded, anchor them with a leading `/`; otherwise confirm no tracked file/directory at a nested depth matches.

- [ ] **OCR-221** `Dockerfile:6` _performance_
  Two independent `go build` invocations in separate layers, without BuildKit cache mounts for `/go/pkg/mod` and `/root/.cache/go-build`, so every rebuild after a source change re-downloads modules and recompiles all shared packages twice (the `quality` stage already uses cache volumes, so the pattern exists in this project). Combine the builds into one layer and mount the caches.

- [ ] **OCR-222** `Dockerfile:19` _security_
  The UID/GID build args are not validated, so `--build-arg HACKATHONKIT_UID=0` (or `HACKATHONKIT_GID=0`) silently produces an image whose `USER` is root — contrary to the checklist rule "containers run as ${HACKATHONKIT_UID:-1000}:${HACKATHONKIT_GID:-1000}, never root". Guard the values (fail the build) before switching user.

- [ ] **OCR-223** `Dockerfile:27` _security_
  `HACKATHONKIT_DEMO_HOST=0.0.0.0` is needed for the compose port mapping to work, but the comment above advertises `docker run -p 8787:8787`, which publishes the unauthenticated dashboard (and its read-only data/metrics endpoints) on every host interface — the project's checklist requires published ports to bind `127.0.0.1`. Keep the container-side bind and update the guidance to `-p 127.0.0.1:8787:8787`.

- [ ] **OCR-224** `cmd/demo-api/fly_live.go:135` _maintainability_
  Stat/Open/Read failures are mapped to `setAvailable(false)` with no logging or error surfaced to the caller. Because `setAvailable(false)` also bumps `revision`, a transient filesystem error (or a momentarily missing path) is indistinguishable from a deliberate feed outage: clients just observe `feed_available: false` frames with stale recommendation/feedback and have no way to tell the feed is actually broken. Consider logging the underlying error (with the path) at least once, or exposing a reason/error field so operators can diagnose silent failures; the best-effort contract in the doc comment should also state that errors are intentionally swallowed here.

- [ ] **OCR-225** `cmd/demo-api/main.go:92` _bug_
  `meta.Close()` is called here explicitly, but `main` also registers `defer meta.Close()`, so on the normal shutdown path (signal received) the recorder is closed twice. It happens to be harmless today because `metadata.Disabled.Close()` is a no-op and `(*Postgres).Close()` nils the pool under a lock, but the redundant close is fragile — a `Recorder` implementation whose `Close` is not idempotent (e.g. an `os.File`/transaction-backed one) would misbehave. Keep the defer and drop this trailing call (the explicit call before `os.Exit(1)` must stay, since `os.Exit` skips deferred functions).

- [ ] **OCR-226** `cmd/demo-api/main.go:230` _security_
  The mutation token is interpolated into the served HTML/JS verbatim. A token containing `"`, `<`, `&` or `</script>` breaks the page or injects markup, and because `/fly-viz` is an unauthenticated GET, the secret is also handed to every client that can reach the port. Use an escaped/JSON-encoded substitution (e.g. `template.JSEscapeString`) or keep the token out of the page and have the client supply it via the `X-HackathonKit-Token` header.

- [ ] **OCR-227** `cmd/demo-api/main.go:442` _security_
  `slug` is joined into a filesystem path with no normalization. The slug used by `/plan/api/latest` and `/plan/decision` comes straight from the plan JSON body (`plans.Load` only normalizes it when the JSON `slug` field is empty), so a plan file containing `"slug": "../../x"` makes this build a path outside `evals/plans` (a read in `readApproval`, and a write in `writeApproval`). Normalize/validate the slug before constructing the path.

- [ ] **OCR-228** `cmd/fly-bench/main.go:103` _bug_
  The benchmark runs on a bare `context.Background()` with no deadline or cancellation. The embedder/LLM calls go through `openai.Client` (180s HTTP timeout each), but there is no bound on the total run, so a stalled endpoint makes every row (plus the warmup) take up to minutes with no way to abort early and no way for callers to bound the run. Use a bounded context and cancel it when `run` returns.

- [ ] **OCR-229** `cmd/fly-bench/main.go:115` _maintainability_
  The warmup result is discarded, so nothing verifies that the intended pipeline (embedding/LLM) actually engaged before the timed run starts. If the embedder or classifier fails at runtime the router silently falls back to the hash encoder and the run still publishes A/B evidence as if the full pipeline were measured; the only guard is the later `engaged == 0` check on a non-empty top-1. Capture the warmup `Result` and fail (or warn on stderr) when the embedding path never engaged, e.g. by asserting `Encoder` contains "embedding".

- [ ] **OCR-230** `cmd/fly-bench/main.go:138` _maintainability_
  Determinism is asserted from a single repeated call on the *same* already-warmed router instance and only compares the top-1 string; changes in the full ranked order, and any cold-start/process-level nondeterminism (persistent embedding cache, map-iteration tie-breaks), are invisible, so `determinism_rate` can overstate stability of the evidence. Consider comparing the full ordered recommendation lists (and/or repeating on a freshly constructed router) and documenting the measured scope in the report field.

- [ ] **OCR-231** `cmd/fly-bench/main.go:172` _bug_
  The output-stream encode error is discarded (cmd/fly-bench/main.go: `_ = enc.Encode(rep)`; internal/mcp/server.go: `_ = enc.Encode(...)` for the oversize-frame response), while the success path in server.go does return it. A broken pipe or full disk silently truncates the emitted JSON/message while the process or session still reports success. Propagate the write error in both places.

- [ ] **OCR-232** `cmd/fly-train/main.go:71` _bug_
  `--epochs` is validated before the `--model knn` early return, although it is never used on that path. `fly-train --model knn --epochs 0` is rejected with exit code 2 even though a valid rebuild was requested. Move this check below the knn branch (or only apply it when `*model == "mb"`).

- [ ] **OCR-233** `cmd/fly-train/main.go:125` _bug_
  `--kc` and `--sparsity` are never validated, and `fly.NewMBrain`/`activate` silently substitute defaults for out-of-range values (`kc <= 0` -> 4133; `sparsity <= 0 || sparsity >= 1` -> `MBDefaultSparse`). The command therefore trains with values the user did not ask for while the summary below echoes the raw flags (`KC=%d sparsity=%.2f`), e.g. `--sparsity 1` or `--sparsity 0` reports `sparsity=1.00`/`0.00` but actually trains dense/default sparsity. Validate these flags together with `--model`/`--epochs` (require `*kc > 0` and `0 < *sparsity < 1`) so failures are explicit instead of silently sanitized.

- [ ] **OCR-234** `cmd/hackathonkit-ctl/daemon.go:243` _security_
  Several handlers (`/healthz`, `/api/sessions`, `/api/events`, `/api/risk`, and the aggregated `events:`/`outputs:` string in `/api/sessions/`) echo the raw `err.Error()` of the underlying SQLite layer into the JSON response. Even on a loopback control API this leaks internal details (schema/table names, file paths, driver errors) that a caller cannot otherwise observe. Prefer logging the detailed error server-side and returning a stable generic message to the client.

- [ ] **OCR-235** `cmd/hackathonkit-ctl/detach_windows.go:12` _documentation_
  The comment claims the child keeps "its own process group", but with `DETACHED_PROCESS` the child inherits no console, and Windows process groups are meaningful only for processes attached to a console (`CREATE_NEW_PROCESS_GROUP` is documented as being ignored when it cannot apply). Combined with DETACHED_PROCESS the flag is effectively inert, so the stated rationale is misleading. Either drop the redundant `CREATE_NEW_PROCESS_GROUP` or reword the comment to describe what actually happens (no console inherited, thus no console control events reach the daemon). Low severity / doc-only.

- [ ] **OCR-236** `cmd/hackathonkit-ctl/main.go:147` _bug_
  `len(task)` counts bytes while `truncate(task, 40)` limits runes, so for tasks containing multi-byte characters the check can trigger while `truncate` returns the string unchanged, appending a spurious `...`. Compare rune counts instead.

- [ ] **OCR-237** `cmd/hackathonkit-ctl/main.go:251` _maintainability_
  Error causes are flattened with %v so callers cannot unwrap them. In cmd/hackathonkit-ctl/main.go the retry branch wraps only the second git failure while firstErr (the actual reason the first attempt failed) is flattened with %v. In internal/routing/fly/hash_encoder.go the fallback error is formatted with %v while only the primary cause uses %w. Use %w for every cause (or errors.Join, since errors is already imported) so errors.Is/As can match all of them.

- [ ] **OCR-238** `cmd/hackathonkit-ctl/main.go:306` _bug_
  `fs.Parse` returns `flag.ErrHelp` for `-h/--help`, and no subcommand handles it: `hackathonkit-ctl run --help` (same for `ingest`, `sessions list`) prints the flag usage to the process `os.Stderr` (the FlagSet default output, again ignoring the injected writer) and then `run()` reports `error: flag: help requested` with exit code 1, whereas the top-level `help` path returns 0. Treat `errors.Is(err, flag.ErrHelp)` as a successful help request (e.g. return nil) and/or set `fs.SetOutput(stderr)` so flag diagnostics go to the provided writer.

- [ ] **OCR-239** `cmd/hackathonkit-ctl/proc_windows.go:19` _bug_
  The `pid int` argument is truncated to `uint32` without a range check. `readPID` (daemon.go:207-215) only rejects `pid <= 0`, so a corrupt or hand-edited pid file containing a value above math.MaxUint32 (e.g. 4294967297) is silently folded onto an unrelated, potentially live PID here, and `os.FindProcess` in the caller truncates the same way before killing it. Validate the upper bound (e.g. reject `pid > math.MaxUint32`) before narrowing the value.

- [ ] **OCR-240** `cmd/hackathonkit-ctl/tui.go:47` _bug_
  `Ingest` is invoked with an unbounded `context.Background()` on the startup path, before `program.Run()`. A slow or stalled ingest (large JSONL corpus, lock contention) blocks the TUI from ever starting, with no deadline and no cancellation. Give it a bounded context so startup cannot hang indefinitely.

- [ ] **OCR-241** `cmd/hackathonkit-mcp/main.go:44` _security_
  Coverage gap in the credential filter: `redactMarkerValues` only matches `key=value` markers, so the equally common `key: value` / `key value` renderings produced by middleware, config dumps and HTTP-error echo (e.g. `password: hunter2`, `Secret Key: abc`, `Authorization: Bearer eyJ...`) pass through unreleased into the logs. Add colon/space-separated variants of the sensitive keys (the same idea the repo's shell-side redaction uses). Note the delimiter is consumed by the marker, so a double space would need the `"key:"` form as well if you want full coverage.

- [ ] **OCR-242** `cmd/hackathonkit-mcp/main.go:366` _security_
  Raw backend errors are logged without going through the repository's redaction helper. In cmd/hackathonkit-mcp/main.go the Serve error is logged verbatim (the only backend/error log in the file that bypasses redactBackendError, which redacts every sibling path such as metadata_disabled/objectstore_disabled/storage_backend_error). In internal/objectstore/minio.go the raw MinIO MakeBucket error is emitted with %v even though the same redaction helper documents that MinIO errors can echo DSNs/credentials, and Open still returns success so the caller never gets a chance to redact it. Route both through redaction (or log without the raw error text).

- [ ] **OCR-243** `cmd/hackathonkit-rehearse/main.go:169` _bug_
  Returning `nil` for an error on the walk root silently converts an inaccessible/missing `data/artifacts` directory into a successful, empty listing (`out` empty, `err` nil). `run` only records a note when `listArtifacts` returns an error, so this case is reported as a clean "0 artifact(s)" PASS-style run instead of surfacing the filesystem problem. Propagate the error (or explicitly handle `os.IsNotExist` and add a note) so a missing artifacts directory is visible. Errors on non-root paths are already propagated.

- [ ] **OCR-244** `docker-compose.yml:51` _bug_
  `HACKATHONKIT_DEMO_TOKEN` defaults to empty here, and `authorizeMutation` now fails closed on an empty token (cmd/demo-api/main.go:544-549), so every state-changing endpoint (plan approvals, `POST /routing`) returns 401 whenever `.env` has no token — e.g. a fresh `.env.example` copy. Prefer requiring it like the other secrets so the misconfiguration surfaces at `docker compose up` instead of at the first dashboard click.

- [ ] **OCR-245** `internal/atomicfile/atomicfile.go:84` _bug_
  Both directory-sync errors (`Open`, `Sync`) are discarded and the function still returns nil. On a crash/power loss the rename may be durable while the directory entry is not, so the caller is told the write succeeded even though the new name can disappear — precisely the durability guarantee this package exists to provide. Unlike the temp-file cleanup (which is documented as best-effort), nothing here documents this as intentionally best-effort. Consider surfacing the sync failure (or at least documenting the deliberate best-effort behavior) so callers relying on crash safety are not misled.

- [ ] **OCR-246** `internal/config/config.go:377` _bug_
  The pre-lock read and parse errors are silently discarded (`if data, err := os.ReadFile(...); err == nil`). If routing.yaml exists but is transiently unreadable, `current` stays at `{normal, 1}`; when `routingInit` is false (a freshly constructed Manager that has never read the file) this then writes version 2, regressing the on-disk version below an already higher value. `Routing()` handles the same failure by logging and keeping the last known state; handle/report the error here consistently and avoid defaulting the version to 1 on a real read error.

- [ ] **OCR-247** `internal/config/dotenv.go:21` _bug_
  Only the exact lowercase `export ` (single ASCII space) prefix is stripped. A shell-legal line such as `export\tKEY=value` (tab) or `EXPORT KEY=value` is left intact, so the parsed key becomes `export\tKEY` / `EXPORT KEY`, fails `validEnvKey`, and the assignment is dropped with only a log line - the variable silently stays unset. Suggest consuming the `export` keyword followed by any whitespace while still refusing to match a real key that merely starts with `export` (e.g. `exportFOO=1`).

- [ ] **OCR-248** `internal/control/handoff.go:105` _maintainability_
  `stage.Goal` (and `plans.Summary(*h.Plan)` on the line above) is interpolated without the `line()` normalizer that task/risk-reasons/learnings go through, so a goal containing a newline or stray whitespace breaks the intended single-line, paste-ready bullet format. Apply `line()` here for consistency.

- [ ] **OCR-249** `internal/control/risk.go:85` _bug_
  The panic classifier is fragile: only the exact negation form `no [errors or ]panic` is stripped, so other negated or quantified forms in tool payloads (e.g. "without panic", "panic-free", "0 panics", "panics=0") still match `panicWord` and are reported as real panic events, inflating the panic reason/score. Conversely a genuine panic expressed as, say, "recovered without crash: panic" could be masked if it shares a line with a stripped negation. Consider matching on bounded values/keys or an explicit allowlist of positive signals rather than free-text word matching.

- [ ] **OCR-250** `internal/control/store.go:168` _bug_
  `parseTime` collapses every unparseable/foreign-format timestamp to `time.Time{}` with no error signal, and `formatTime` maps zero back to `""`. Callers cannot distinguish "corrupt ts" from "never set": `handleSessionRecord` passes the result to `EndSession`, which substitutes `time.Now()` for a zero ts, and `AppendEvent` does the same — so a malformed `ts` in the JSONL is silently recorded as ingest time rather than surfaced. The same ambiguity breaks the lexicographic window range in `EventsBetween` and cursor-rewrite detection. Return an error (or a validity flag) so callers can reject/locate corrupt records instead of fabricating timestamps.

- [ ] **OCR-251** `internal/evaluation/bundle.go:108` _maintainability_
  The `os.Stat` error cause is discarded: if `os.Stat` fails for a reason other than the path not being a directory (e.g. permission denied on `root`), the caller sees the misleading "bundle root is not a directory". Split the two cases and wrap the original error with `%w`.

- [ ] **OCR-252** `internal/evaluation/bundle.go:209` _bug_
  Ownership-marker/order bug: `.hackathonkit-bundle` is written into the temp bundle *before* `filepath.WalkDir` runs, and it is not part of `DefaultBundleExcludes`, so a repository file named `.hackathonkit-bundle` at the root is copied over the marker. The resulting bundle then contains a marker whose content != `outAbs`, so the next run refuses to overwrite it with "not a bundle created by this tool" and the output directory becomes permanently unusable without manual cleanup. The same ordering silently discards a repo file named `git-sha.txt`/`rubric-version.txt`, and `bundle.json`/`bundle-hash.txt` are copied into the bundle yet excluded from the hash. Reserve these names when copying (or write the marker after the walk).

- [ ] **OCR-253** `internal/evaluation/bundle.go:365` _security_
  Glob patterns that contain a `/` silently match nothing: `path.Match(rule.pattern, rel)` uses `*` which never crosses `/`, and the second check matches the same pattern against `base`, which never contains `/` (and is only reached if the full-path match failed). So user excludes like `**/secrets`, `data/*/cache` or `*/generated` are accepted by `validateExcludes` (they are syntactically valid) but exclude nothing — exactly the leak the comment on `validateExcludes` claims to prevent. Consider matching the pattern against every ancestor prefix of `rel`, or rejecting patterns that combine `/` with glob metacharacters.

- [ ] **OCR-254** `internal/evaluation/evaluation.go:152` _bug_
  When a score is missing (or out of range) the loop `continue`s, so `sum` no longer reflects the criteria the report actually declared. The subsequent total/sum check therefore emits a second, misleading problem such as `total 75 does not match criterion sum 50` for a report that only omitted one criterion, obscuring the real cause. Track whether all criterion scores were usable and only reconcile `total` against a complete `sum` (the report is already rejected either way).

- [ ] **OCR-255** `internal/evaluation/evaluation.go:427` _bug_
  `minDelta` rejects NaN but not ±Inf. A non-finite threshold reaches `Calibration.NoiseFloor` (`math.Max(minDelta, 2*delta)`), which then cannot be marshalled (`json: unsupported value: +Inf`) and, when fed back into `Compare`, is silently clamped to 0 — losing the operator's intent. This is reachable through configuration: `internal/config/config.go` accepts any `min_delta >= 0`, including a YAML `.inf`. Reject non-finite thresholds here.

- [ ] **OCR-256** `internal/evaluation/evaluation.go:430` _bug_
  `Calibrate` compares the means of two runs but, unlike `Rank`, never verifies that both aggregates were produced under the same rubric version. Both runs are allowed to carry an empty or differing `RubricVersion`, which contradicts the package contract that scores from different rubric versions are never compared. Add the same version guard `Rank` applies.

- [ ] **OCR-257** `internal/learning/learning.go:94` _bug_
  `WithEmbedder` (and `WithClock`) mutate struct fields that `Record`/`Recall` read on request paths with no synchronization. Current wiring happens before the store is served, but nothing prevents a caller from invoking these after the store is in use, which would be a data race. Either document that these must be called before first use or guard the fields.

- [ ] **OCR-258** `internal/learning/learning.go:157` _performance_
  `s.mu` is held across the cross-process lock acquisition (up to ~1s of sleeping on contention) and the subsequent file read/write. Because Recall/Top/Count only need `s.mu` to read the store, they can be stalled for the whole duration of a peer process's write. Consider using a separate mutex for the file-lock sequence, or reading the store without blocking on the writer mutex.

- [ ] **OCR-259** `internal/learning/learning.go:388` _bug_
  `Count` converts any load failure (corrupt JSON, permission error, I/O error) into a successful `0`, silently masking data loss and making a broken store indistinguishable from an empty one. Since this is a public method, consider returning `(int, error)` or at least logging the failure so operators can observe it.

- [ ] **OCR-260** `internal/mcp/registry.go:30` _maintainability_
  Duplicate tool names are silently overwritten: an existing entry's `Description`, `InputSchema`, and `Handler` are replaced in place while the original `order` entry is kept. A duplicate registration (easy to introduce when many `register*` functions feed the same registry) is thus masked instead of surfaced. Consider returning a bool/error for duplicates or at least logging/panicking on conflict so callers can detect the mistake.

- [ ] **OCR-261** `internal/mcp/server.go:59` _bug_
  Truncation is byte-based, so a multi-byte UTF-8 rune can be cut in half and the client receives a torn sequence (rendered as U+FFFD) in the error message. Truncate on a rune boundary instead. Note also that this sanitizer is only applied to `err.Error()` strings; the client-supplied tool name (`"unknown tool: " + p.Name`) and method (`"method not found: " + req.Method`) are echoed back unbounded and unsanitized at the same RPC boundary.

- [ ] **OCR-262** `internal/mcp/server.go:119` _bug_
  When the discarded oversized frame ends at EOF without a trailing newline, the drain loop returns before the oversize sentinel is ever pushed onto `lines`, so no error response is emitted and `ServeStreams` returns nil (apparent success). The client cannot distinguish this from a clean shutdown, even though the same oversized frame is reported as an error when it is newline-terminated. Deliver the oversize response before reporting EOF.

- [ ] **OCR-263** `internal/metadata/metadata.go:272` _bug_
  `poolRef` hands out the pool reference and releases the read lock before the caller calls `Exec`, while `Close` nils the field and then closes the pool outside the write lock. A `Record*` call that obtained the reference just before shutdown can therefore execute against a pool that is being closed; pgx returns a "closed pool" error, and callers discard it (`_ =`), so the record is dropped silently. Since `Close()` is reached via `defer meta.Close()` in `main` while request goroutines may still be recording, consider coordinating shutdown with in-flight writes (e.g. reference counting / WaitGroup) rather than relying on the nil check alone.

- [ ] **OCR-264** `internal/objectstore/minio.go:56` _bug_
  Whitespace is trimmed only for the validation check, while the raw untrimmed value is what actually gets used, so a value with surrounding whitespace (common when read from a .env/CRLF file) passes the guard and then fails later with an opaque error. In internal/objectstore/minio.go the check trims AccessKey/SecretKey but credentials.NewStaticV4 receives the untrimmed values, yielding a confusing SignatureDoesNotMatch at request time. In internal/resources/probe.go Configured is computed from the trimmed key but the raw apiKey is sent as the header, producing a spurious 401/403 for a key that was reported as configured. Normalize once and use the trimmed value both for the check and for the request.

- [ ] **OCR-265** `internal/observability/obs.go:167` _performance_
  Prepending each chunk with `append(tmp[:n], buf...)` re-copies the entire accumulated buffer on every iteration, and the subsequent `bytes.Count(buf, ...)` re-scans it as well, making the backward read O(n^2) in the tail size (up to ~4 MiB → tens of MiB of copying plus scanning for a file with few newlines in its last 4 MiB, e.g. one very long line). Accumulate the chunks in a slice and concatenate once at the end (or read into a preallocated buffer at the right offsets) and count only the freshly read chunk.

- [ ] **OCR-266** `internal/observability/obs.go:170` _bug_
  The backward read stops as soon as `limit+1` **newlines** are buffered, but lines that are not valid JSON (blank lines, `undefined\n` written by `plugins/hackathonkit/scripts/hooks.js`, a record truncated by a crashed writer, or any non-JSONL writer sharing `logs/*.jsonl`) are only discarded later by the `json.Valid` filter. A tail containing such lines therefore makes `TailJSONL` return fewer than `limit` records even though more valid records exist earlier in the file — silently under-returning on the user-facing caller (`cmd/demo-api/main.go` `tail()` passes the raw `?limit=` value). Count the lines that actually pass validation, or keep reading until `limit` valid records are collected or `offset` reaches 0. The same silent truncation happens on the `maxTailBytes` path for a file whose tail exceeds 4 MiB.

- [ ] **OCR-267** `internal/openai/client.go:50` _maintainability_
  A single hardcoded 180s `Timeout` is applied to every request, including large audio uploads (files up to 25MB) and long chat completions. Because `http.Client.Timeout` caps the whole round trip and callers can only shorten it via their context, a slow-but-legitimate upload can never succeed even if the caller would allow more time. Consider making the timeout configurable (e.g. via `config.OpenAISettings`) or relying on the caller's context deadline for long-running endpoints.

- [ ] **OCR-268** `internal/openai/client.go:174` _bug_
  The "missing vector" check only catches a nil slice; a provider that returns an empty array (`"embedding": []`) yields a non-nil zero-length vector that passes this guard and is then returned to callers as a silently invalid embedding. Check the length instead of nil in both the unindexed branch and in the final validation loop.

- [ ] **OCR-269** `internal/openai/client.go:190` _bug_
  The indexed branch assigns `result[index] = d.Embedding` without detecting duplicate indices. A provider returning the same index twice silently overwrites the earlier vector, leaving another slot nil (surfacing later as a confusing "missing a vector" error) or, worse, mapping vectors to the wrong inputs. Consider rejecting duplicate indices explicitly.

- [ ] **OCR-270** `internal/plans/plans.go:161` _maintainability_
  The synthetic corrupt-plan error discards the underlying `json.Unmarshal` cause, so callers (e.g. `internal/tools/plan.go`, which surfaces `latestErr.Error()`) cannot tell why a file was rejected. Preserve the cause so diagnostics are actionable.

- [ ] **OCR-271** `internal/quality/proc_unix.go:1` _maintainability_
  The build constraint excludes `plan9`, `js` and `wasip1`, but the only other implementation in the package (`proc_windows.go`) is gated on `windows`. On those three GOOS neither file matches, so `configureProcessGroup`/`killProcessGroup` are undefined and the whole `quality` package fails to compile (e.g. `GOOS=js GOARCH=wasm go build ./...`), rather than silently losing group-kill semantics. Add a no-op fallback stub constrained to `!unix && !windows` (or `plan9 || js || wasip1`) so every target that the package can be built for has the functions defined — the sibling `cmd/hackathonkit-ctl` detach/proc helpers hit the same asymmetry.

- [ ] **OCR-272** `internal/quality/proc_windows.go:12` _bug_
  On the Windows build the process-group contract exposed to callers is not actually honoured: `configureProcessGroup` is a no-op and `killProcessGroup` terminates only the direct child, so grandchildren spawned by gate commands (e.g. `go test ./...` test binaries, `sh scripts/*.sh` children) keep running after `quality.go` reports `timeout`. `WaitDelay` only bounds the pipe drain, it does not reap those descendants, which can hold source/output files, ports or locks long after the gate completed. This is documented here, but if hermetic teardown is required, start the child with a Job Object (`CREATE_SUSPENDED` + `AssignProcessToJobObject` + `TerminateJobObject`) or at minimum `syscall.CREATE_NEW_PROCESS_GROUP` plus `taskkill /T`, and record the intended limitation where callers can see it.

- [ ] **OCR-273** `internal/quality/quality.go:104` _bug_
  `exec.ErrWaitDelay` is documented as being returned by `Cmd.Wait` "if the process exits with a successful status code but its output pipes are not closed before the command's WaitDelay expires" — exactly the lingering-grandchild scenario this WaitDelay is meant to tolerate. With this switch such a check is reported as `failed`, and `Check` has no field carrying the cause, so a successful `go test`/`go build` whose output pipes stayed open is indistinguishable from a real failure and the gate is blocked with no actionable reason. Consider distinguishing `errors.Is(err, exec.ErrWaitDelay)` (own status, or at least record the cause/truncated output) from a genuine non-zero exit.

- [ ] **OCR-274** `internal/resources/matrix.go:61` _maintainability_
  `yaml.Unmarshal` ignores unknown/misspelled fields by default. Since this file is documented as authoritative at runtime, a typo such as `capabilites:` inside a track silently yields an empty playbook (no decisions) instead of an error. Prefer a decoder with `KnownFields(true)` to fail fast on config typos.

- [ ] **OCR-275** `internal/resources/matrix.go:81` _maintainability_
  `Validate` never inspects the variant lists: empty or duplicate variant `id`s pass validation even though `Plan` relies on the id (plus provider) to exclude the chosen variant from `Alternatives`. An empty id also collides with the zero `Variant` used for "no variants defined", making the two cases indistinguishable in output. Add checks that each capability has at least one variant and that ids are non-empty and unique.

- [ ] **OCR-276** `internal/resources/matrix.go:113` _maintainability_
  A providerless variant is treated as always-ready and returned as soon as it is encountered. When it is listed before a ready provider-backed variant (e.g. `tabular: [deterministic, openai]` with `openai_api` ready), the ready provider is silently skipped and the decision is not marked `Degraded`. The `Variant` doc comment calls providerless variants the deterministic *fallback*, which suggests ready providers should be preferred; if the ordering is instead authoritative, please document that contract explicitly so config authors do not misread it.

- [ ] **OCR-277** `internal/resources/matrix.go:198` _maintainability_
  `providerMissing` returns `nil` for a provider that has no matching `Status` entry (e.g. a typo'd or unknown provider id) as well as for a `Status` with both `MissingKeys` and `EnvKeys` empty. In those cases a degraded decision reports no missing keys, so a misconfigured provider id stays invisible to the user. Consider surfacing the unresolved provider id in `Missing`.

- [ ] **OCR-278** `internal/resources/resources.go:112` _bug_
  `t.ID[:2]` assumes every id is at least two bytes with `-` at index 2, so a malformed/renamed id panics here, and the numeric branch (`number == t.ID[:2]`) matches only the two-digit prefix while the slug is compared with full equality only. Consequently a reference such as `1-management` resolves to `01-energy` instead of the track the user named. Split the id with `strings.Cut(t.ID, "-")` and validate the parts rather than slicing at fixed offsets.

- [ ] **OCR-279** `internal/resources/resources.go:154` _maintainability_
  `Save` copies and persists every `Claimed` entry verbatim, and `Load` accepts any key, whereas `SetAnswer` rejects unknown providers. Hand-edited or legacy ids thus round-trip through the file forever: they are never surfaced by `Statuses`/`View` and never cleared. Filter `Claimed` against `defs` when loading or saving so the persisted profile only contains known provider ids.

- [ ] **OCR-280** `internal/resources/resources.go:264` _maintainability_
  This agent prompt enumerates only OpenAI, NVIDIA, Telegram, Codex Pro and the track, while `defs` also contains `postgres` and `minio`. In the `!present` flow the agent will never be told to ask about those two providers, so the profile can end up incomplete relative to the configured provider set. Derive the list from `Defs()` (or `Statuses`) instead of hardcoding a subset.

- [ ] **OCR-281** `internal/routing/fly/embed.go:136` _bug_
  The one-entry cache stores failed embeddings too (`ok == false`), so a transient embedder error for a given task string is replayed indefinitely until a *different* task evicts the entry. This differs from `ensureCentroids`, which deliberately bounds its negative cache to 30s. A long-lived caller that re-requests the same task (or a benchmark loop over one task) will never retry the embedder even after it recovers. Consider caching only successful results, or giving the negative entry a short TTL like the centroid cache.

- [ ] **OCR-282** `internal/routing/fly/hash_encoder.go:61` _performance_
  `current.String()` is evaluated twice per flush, allocating a copy each time (once for the rune count, once for `ToLower`). In this per-token/per-text hot loop, build the string once and reuse it.

- [ ] **OCR-283** `internal/routing/fly/llm.go:20` _bug_
  The typed-nil guard only covers `reflect.Ptr`. Any other nilable dynamic type that can implement `LLMClassifier` (named map/slice/func/chan type with value receivers, or an interface value) passes this check and `ClassifyTask` is then invoked on the nil value, which may panic inside the implementation. `reflect.ValueOf(r.llm)` is also evaluated three times. Consider a single reflect lookup that tests all nil-capable kinds (keeping the explicit `r.llm == nil` check, since `reflect.ValueOf(nil)` is an invalid Kind).

- [ ] **OCR-284** `internal/routing/fly/llm.go:55` _maintainability_
  `first` is taken from the first element of a random map-iteration order, which is a fragile way to express the "all scores equal" check: it is only correct because the comparison is order-independent, and it silently changes meaning if anyone later assumes `first` is the max or a specific category. Deriving a deterministic baseline (min/max) makes the intent explicit and removes the subtlety.

- [ ] **OCR-285** `internal/routing/fly/mb.go:289` _performance_
  The cached `*MBrain` is never mutated in place — `applyDopamine` builds a `clone` and publishes it via `r.brain = work`, and `Train` is not invoked on a Router-owned brain by any production call site. Holding the process-global `brainMu` across the purely CPU-bound `brain.scores` (O(KC) allocation plus an O(KC log KC) sort of 4133 elements and map writes) therefore adds no safety but serializes every concurrent `Recommend` across all Routers, and makes scoring block behind `applyDopamine`'s disk write. Take a local reference under the lock (or drop the lock here entirely) and score outside it.

- [ ] **OCR-286** `internal/routing/fly/mb.go:404` _bug_
  Weights are clamped to +/-`mbWeightLimit`, but `Bias[category]` grows by `eta*0.1` on every `Train` call with no upper bound. Over repeated epochs/examples the bias can grow arbitrarily large while the readout weights saturate, letting the bias dominate the softmax logits and skew `Rank`/`scores` away from the learned weights. Clamp the bias to the same limit (or decay it) so it cannot overwhelm the weighted sum.

- [ ] **OCR-287** `internal/routing/fly/memory.go:197` _maintainability_
  Embedding failure or an unexpected vector count is silently swallowed: the example is stored keyword-only with no log or metric. Callers cannot tell "no embedder configured" from "transient embedding outage", so a persistent embedder failure silently degrades semantic scoring to keyword-only. At minimum log the error for observability.

- [ ] **OCR-288** `internal/routing/fly/memory.go:202` _performance_
  `memoryMu` is a package-level mutex shared by every Router, and it is held across `loadMemory` (file read + potential rename) and `saveMemory` (temp-file create/write/fsync/rename). All routers in the process therefore serialize on disk I/O even when they target different `memoryPath` values, and a slow filesystem stalls unrelated feedback writes. Prefer a per-Router mutex (or a path-keyed lock) so contention is scoped to the memory file actually being touched.

- [ ] **OCR-289** `internal/routing/fly/memory.go:238` _maintainability_
  On a failed save the function returns `len(kept)` — the number of examples that would have been persisted — alongside the error, which is a success-shaped count for a write that never reached disk. Callers that ignore the error (or log the count before checking it) will report a memory size that does not exist. Return 0 on failure so the count only ever reflects persisted state.

- [ ] **OCR-290** `internal/routing/fly/router.go:46` _bug_
  `embedTask` (internal/routing/fly/embed.go) returns this cached slice directly to its callers without copying (`return vec, ok`), so the same backing array is handed out by every call. Today all callers (`embeddingCategoryScores`, `mbCategoryScores`, and the `taskVector` passed to `memoryCategoryScores`) only read it, so there is no live corruption, but the ownership contract of `embedCacheVec` is undocumented and a single future in-place mutation (e.g. normalizing/zeroing an element) would silently corrupt the one-entry cache for subsequent requests. Either return a copy from `embedTask` or document on this field that the returned vector must be treated as read-only.

- [ ] **OCR-291** `internal/routing/fly/router.go:150` _maintainability_
  Fusion is asymmetric between encoders, which makes the weight of earlier evidence depend on which downstream encoders happen to answer. Here the embedding step only reweights categories that `embeddingScores` returns (`combined = 0.5*prior + 0.5*emb`), so a keyword-only category keeps its full weight, while the kNN/MB/LLM steps below multiply *every* existing category by 0.6/0.4 and then add their own categories undecayed. Consequence: a task's category ranking (and its relation to the 0.55 threshold) changes depending on whether embeddings/LLM happen to be available, even though the task text is identical. This mirrors the open backlog item L67 — please confirm the intent and document it, or unify the combine step so the effective weight of each encoder is independent of the others.

- [ ] **OCR-292** `internal/storage/store.go:80` _security_
  Fail-open fallback in the containment check. Walking up the ancestor chain, if no ancestor can be resolved by `EvalSymlinks` the loop reaches the filesystem root and `parent == p` returns `nil` (path accepted) instead of failing closed. This is the opposite of the fail-closed behaviour used for the root resolution above. (It is currently hard to reach because `sanitize` guarantees `full` is lexically under `s.root`, but a concurrent delete/rename of the root or an ancestor turns it into an accepted out-of-root path.) Return an error here so an unverifiable path is rejected.

- [ ] **OCR-293** `internal/telegram/telegram.go:235` _bug_
  Only `HTML` gets entity-aware chunking; every other parse mode (notably `MarkdownV2`, which the `telegram.send` tool accepts) is chunked by `ChunkText`, which cuts at arbitrary rune/newline boundaries. A `MarkdownV2` entity or escape sequence that spans the 4096-rune boundary (e.g. `*bold*`, `__underline__`, `\*`) leaves the chunk unparsable and Telegram answers 400, so long formatted messages fail to send. Either add minimal MarkdownV2-balanced chunking, or reject/document unsupported parse modes rather than emitting chunks the API will refuse.

- [ ] **OCR-294** `internal/telegram/telegram.go:240` _maintainability_
  On a mid-chunk failure this returns the zero-ish `SendResult` with `Chunks` set to the *total planned* chunk count, not the number actually delivered, and `MessageID` holding the last successfully sent chunk (or 0). The caller in `internal/tools/telegram.go` surfaces this on partial failure as `"chunks": result.Chunks`, so a partial send is reported as if every chunk had been planned/sent, which can mislead retry logic. Consider tracking and returning the number of chunks actually sent (e.g. a distinct `Sent`/`Delivered` field or setting `Chunks` to the sent count) so the partial result is unambiguous.

- [ ] **OCR-295** `internal/tools/context.go:169` _performance_
  Every file is read fully into memory and then copied once more to a string just to count newlines (`string(data)` allocates a full copy per file, including large/binary assets). Since `data` is already a byte slice, use `bytes.Count(data, []byte("\n"))` to avoid the copy.

- [ ] **OCR-296** `internal/tools/context.go:210` _maintainability_
  `total` is accumulated in this loop but never read afterwards (the function returns only `items`), so this is dead code that misleads readers into thinking a manifest subtotal is reported somewhere. Remove it or actually surface it.

- [ ] **OCR-297** `internal/tools/context.go:295` _bug_
  `frontmatterDescription` hand-rolls a YAML subset, so it can mis-measure the description and produce wrong `description_warn` flags: block-scalar indicators other than `|`/`>`/`|-`/`>-` (e.g. `|2-`, `>-2`) are returned as literal text; the inline-`#` stripping is skipped when the value is quoted, so `description: "abc" # note` keeps the comment in the length; and `strings.Trim(value, "\"'")` strips unbalanced/escaped quotes. Prefer a real frontmatter/YAML parser, or normalize these forms explicitly.

- [ ] **OCR-298** `internal/tools/data.go:69` _bug_
  `readCSVTable` mutates the first record's header slice in place (`headers[0] = ...`). `headers` aliases `records[0]` and `rows` aliases the same `records` backing array, which is fine here, but the BOM-strip only handles the first cell. More importantly, the ragged-row warning tells users short rows are padded, yet nothing pads them — downstream consumers that index by header position get an empty cell via the `idx < len(row)` guard, but the message is misleading. Either pad rows to header width or reword the warning.

- [ ] **OCR-299** `internal/tools/deps.go:54` _maintainability_
  The helper accepts a nil `props` and would then emit `{"type":"object","properties":null}`; `properties` must be a JSON Schema object, and strict MCP clients/validators reject `null`. Every current caller passes a map literal (empty `map[string]any{}` when a tool takes no arguments), so this is latent, but normalizing nil keeps the schema valid for future callers. A guard that every `required` name exists in `props` would likewise catch schema/name drift early.

- [ ] **OCR-300** `internal/tools/deps.go:115` _bug_
  Argument normalization silently turns unusable input into a default/absent value: `argInt` returns `def` for out-of-range values (indistinguishable from an omitted argument) and truncates fractions; `argStringSlice` drops non-string elements and collapses to `nil` if all are non-string, which callers treat as 'argument absent'; `resources.record` iterates only known `resources.Defs()` ids, so unknown/misspelled resource keys are dropped while a recognized key can still make `changed` true. Reject or warn on invalid/unknown arguments so callers do not operate on an unintended set.

- [ ] **OCR-301** `internal/tools/deps.go:165` _maintainability_
  `argMap` hands back the caller's own map. Handlers store the result into persisted state (`plan.MandatoryScenario`) or pass it into `Router.Recommend(...)`, so any later in-place mutation of the returned map also mutates the request argument map and anything that aliases it. Returning a shallow copy (or documenting read-only ownership) avoids the aliasing surprise.

- [ ] **OCR-302** `internal/tools/documents.go:297` _maintainability_
  This `max_chars` clamping block is duplicated verbatim in `documents.extract` and `documents.get`, but with a different statement order (extract clamps `>10_000_000` before the `<=0` fallback; get does the reverse), which makes the two handlers easy to diverge on a future edit. Also, the trailing `if maxChars <= 0 { maxChars = 60000 }` is unreachable: `config.Manager` always yields `Tools.MaxTextChars > 0` (default 60000, and non-positive values are ignored on load), so after the first fallback the value can never be <= 0. Extract this into a shared helper (e.g. `clampMaxChars(args, cfg)`) and drop the dead branch.

- [ ] **OCR-303** `internal/tools/evaluation.go:369` _bug_
  Both `int(...)` conversions accept any finite float64 (up to ~1.8e308) even though Go's conversion of an out-of-range float to int is implementation-dependent (e.g. MinInt64 on amd64/arm64): the votes guard lets `1e300` pass as integral and reach `int(fv)`, and the xlsx date/elapsed serial lets a crafted numeric cell reach `int(math.Round(serial * 86400))`. Add explicit finiteness/range validation before converting (bound votes to a sane integer range; reject/clamp Excel serials outside the valid date range).

- [ ] **OCR-304** `internal/tools/evaluation.go:491` _maintainability_
  `InvalidReport.Index` is populated from two independent index spaces: the inline-JSON loop (`for i, item := range raw`) and this file loop (`for i, rel := ...`). Both entries land in the same `DroppedInvalid` slice after the caller prepends `loadInvalids`, so receivers cannot tell whether an index refers to the inline array or the file list, and indices can collide without any discriminator. Either namespace the inline/file indices (e.g. an offset or a `source` field) or document and normalize them after merging.

- [ ] **OCR-305** `internal/tools/evaluation.go:618` _bug_
  Truncating to 64 chars without any uniqueness suffix means two distinct values that share the first 64 characters (e.g. long hashes/ids differing only in the tail) map to the same directory segment, silently overwriting persisted aggregates/rankings. Preserve uniqueness by appending a short hash of the full value instead of a plain prefix.

- [ ] **OCR-306** `internal/tools/files.go:118` _performance_
  `len([]rune(string(b)))` allocates a full rune slice (up to ~8M runes ≈ 32MB) just to count characters, on top of the `string(b)` copies already made for the line count. Use `utf8.RuneCount(b)` (add `unicode/utf8` to the imports) to count without the extra allocation; consider computing `s := string(b)` once and reusing it for the line count.

- [ ] **OCR-307** `internal/tools/knowledge.go:113` _performance_
  Both `knowledge.index` and the `security` artifact fallback call `Store.ReadArtifact`, which materialises the entire stored artifact into memory (`os.ReadFile` / unbounded remote `objects.Get`) before any cap or scan limit is applied. For `security` this re-opens the OOM vector `readCapped` was added to close, despite the comment saying the read is capped; for `knowledge` it makes indexing an unbounded-memory operation. Expose/use a size-limited read (or rehydrate and then apply `readCapped`) instead of `ReadArtifact`.

- [ ] **OCR-308** `internal/tools/knowledge.go:302` _maintainability_
  `sort.Slice` compares only `Score`, so equal-scored hits have no defined relative order (and the subsequent `hits[:limit]` truncation may keep/drop different hits for what should be the same result). Sparse TF-IDF scores produce many exact ties, so add stable tiebreakers (e.g. IndexID then ChunkID) to make the result deterministic and reproducible.

- [ ] **OCR-309** `internal/tools/plan.go:166` _bug_
  `plans.Load` keeps the `slug` stored *inside* the file (it only falls back to the requested slug when that field is empty), and `plans.Save` derives the target path from `plan.Slug` (plans.go:107-118, 213-231). So if a plan file's `slug` field and its filename stem diverge (renamed/hand-copied files), this update is written to a different file than the one just read, creating or clobbering an unrelated plan. Force the requested slug back onto the loaded plan before saving.

- [ ] **OCR-310** `internal/tools/timeseries.go:251` _performance_
  For every point after the first, the whole rolling window is copied into a new slice and `mean`/`stddev` are recomputed from scratch, giving O(n*window) work. `window` is user-controlled up to 10000, so a large table combined with a large window can make this tool very slow. Consider maintaining rolling sums (or prefix sums) and computing the mean/variance incrementally.

- [ ] **OCR-311** `internal/tools/timeseries.go:319` _maintainability_
  Both helpers lack the safe edge-case guard their siblings have: `minOf`/`maxOf` index `values[0]` without an empty check (they will panic if reused on an empty slice), and `snippet` has no non-positive `limit` guard (`r[:limit]` panics for `limit < 0`), unlike `truncateRunes`. Add length/limit checks so these generic helpers fail safe.

- [ ] **OCR-312** `internal/tools/util.go:233` _bug_
  `stddev` accumulates squared deviations in a naive `sum`, so for large-magnitude input (|v| above ~1e154) `d*d` overflows to +Inf and the result becomes Inf; `mean`'s naive `sum += v` likewise overflows for values near `math.MaxFloat64`. `parseNumber` accepts such finite values (it only rejects NaN/Inf), and callers serialize the result (`data.go` min/max/mean/std, `timeseries.go` `"std": round2(stddev(values))`) with `json.MarshalIndent`, which fails on non-finite floats and destroys the whole tool result. Consider a numerically stable accumulation (e.g. Welford for the variance) or guarding non-finite results.

- [ ] **OCR-313** `internal/tools/xlsx.go:444` _bug_
  The guard rejects any read once `remaining` reaches 0, so an entry whose decompressed size is exactly the configured limit fails with "exceeds the configured size limit" instead of ending with `io.EOF` (the XML decoder reads past the root element to confirm EOF). Probe one extra byte to distinguish a clean EOF from a real overrun.

- [ ] **OCR-314** `overlay.json:3` _maintainability_
  Spelling inconsistency in the path key: the source uses `Dokument`, while the corresponding target path and the standard Windows user folder are `Documents`. Since these keys are exact-match paths used for substitution, a misspelled segment such as `Dokument` would make the `Replace` mapping silently fail to match the real source file. All five keys share this segment, so please verify the actual folder name (e.g. `Dokument` vs `Documents`) before relying on this overlay.

- [ ] **OCR-315** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:10` _bug_
  `readlink` without `-f` and without a loop resolves exactly one hop. If the launcher is reached through a chain (link -> link -> real file) or the link target lives in another symlinked directory, `SELF` ends up as an intermediate path, so `SCRIPT_DIR`/`REPO_ROOT` (`../../..`) are computed from the wrong tree and the script can exec the wrong `bin/hackathonkit-mcp` or report a bogus missing-compose path (residual of the S3-L128 class of bug, now only partially covered). Note also that line 16 uses plain `pwd`; a symlinked parent directory keeps the logical path, so the three `..` hops stay on the literal path even though `bin/` resolves elsewhere. Loop until `[ -L ]` is false (or use `readlink -f`/`realpath` when available) and `pwd -P` for the physical directory, or fail clearly when the derived root does not contain `docker-compose.yml`.

- [ ] **OCR-316** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:21` _maintainability_
  `--repo-root "$REPO_ROOT"` is prepended before `"$@"` with no validation, so a caller-supplied `--repo-root` is accepted silently and wins (the Go flag package keeps the last occurrence). That is at best a surprising precedence and at worst lets an argument override the derived root. It is also asymmetric with the docker branch below, which never injects `--repo-root`, so the two transports can end up operating on different roots. Validate/reject a duplicate flag or document the precedence explicitly.

- [ ] **OCR-317** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:46` _test_
  No automated test exercises this launcher: `scripts/smoke.test.sh`/`smoke.sh` cover smoke.sh's transport selection, and `hooks.test.js` covers hooks.js, but nothing asserts the launcher's own behavior. The paths added here (local binary vs docker fallback, the non-executable guard, `"$@"` passthrough and the exit-status propagation) are therefore unverified and can regress unnoticed. A `.test.sh` with stubbed `docker`/binary (same pattern as smoke.test.sh) would pin the branch selection and the propagated status.

- [ ] **OCR-318** `plugins/hackathonkit/scripts/hooks.js:150` _bug_
  The stdin payload is truncated to 1MB before parsing. A larger payload is cut mid-JSON, `parsePayload` then returns `{}`, and the session id (and tool name) are silently lost, which degrades to wrong/unknown session attribution in the `tool-use` path. At minimum log/flag truncation, or parse incrementally / raise the limit so valid payloads are not mangled.

- [ ] **OCR-319** `plugins/hackathonkit/scripts/hooks.js:303` _style_
  Formatting is inconsistent here (statement on the same line as the function opening brace), which is easy to miss in review and diverges from the rest of the file. Also note the comment above `readActive()` claims the tail is the "most recently started" session, but the sort key is file mtime; if that ever diverges from `started_at` the comment is misleading.

- [ ] **OCR-320** `plugins/hackathonkit/scripts/hooks.js:328` _security_
  `safeBinary` stats the candidate and then a later `execFileSync` re-resolves the path, leaving a TOCTOU window in which a group/world-writable or foreign-owned file can be swapped in between the check and the exec. Consider re-checking immediately before execution (or opening the file and exec'ing via the held fd) so the validated inode is the one executed.

- [ ] **OCR-321** `scripts/coverage-check.sh:34` _bug_
  The anchor `(^|[[:space:]])` is not a real token boundary: whitespace inside a quoted GOFLAGS value also matches, so `GOFLAGS='-ldflags="-X a -buildvcs=b"'` has `-buildvcs=b` silently stripped out of the `-ldflags` value, corrupting the build flags. The bare `-buildvcs` form is likewise not removed and ends up duplicated with the appended flag. Since Go flag parsing lets the last occurrence win, you can drop the fragile string surgery entirely and just append the disabling flag.

- [ ] **OCR-322** `scripts/coverage-check.sh:58` _performance_
  `$COVER_LOG` is discarded here and then, on the below-target branch, `go tool cover -func="$PROFILE"` is executed a second time to build the diagnostics (the same double invocation flagged in the backlog). Keep the file until after the comparison and feed it to `sort`, so the (potentially large) profile is only processed once.

- [ ] **OCR-323** `scripts/coverage-check.sh:64` _bug_
  Correction to the previous suggestion: the multi-dot guard should be expressed purely with globs. `*.*.*` matches any value containing two or more literal dots, so combine it with the existing character-class arm (the at-least-one-digit requirement is already implicit for values produced by `go tool cover`, and can be added with a separate `*[0-9]*` check mirroring the TARGET validation).

- [ ] **OCR-324** `scripts/docker-check.sh:25` _documentation_
  The header comment documents only exit codes 0 (ok), 1 (failed) and 2 (docker unavailable), but the argument parser also exits 64 for an unknown flag; `--fast` is likewise accepted yet absent from the usage string. Update the header/usage so callers (and CI) can interpret every exit code and flag the script can produce.

- [ ] **OCR-325** `scripts/doctor.ps1:45` _maintainability_
  This check hard-codes `$true`, so it can never fail and its result is meaningless as a "check" (the `Check` helper exists to detect failures). Either turn it into an informational `Write-Host`/`[info]` line like the telegram postgres entries, or pass `($null -ne $go)` and adjust the detail to state that Go is optional.

- [ ] **OCR-326** `scripts/doctor.ps1:117` _maintainability_
  The `-ne "unknown"` comparison can never be true: `$stateVersion` is only ever assigned from `$state.version`, `""`, or `"unreadable"`, so `"unknown"` is never produced. Dead condition that obscures intent — remove it or make the sentinel consistent with the values actually assigned.

- [ ] **OCR-327** `scripts/doctor.sh:168` _maintainability_
  The version is scraped with a hand-rolled `sed` pattern that only tolerates spaces after the colon (not tabs) and silently yields an empty string (`[ok] install state version `) on any JSON formatting drift, hiding a corrupt/unreadable install state behind a passing check. Consider validating the extracted value (or using `jq`/`python3` like the rest of the script) and failing the check when the version cannot be parsed.

- [ ] **OCR-328** `scripts/doctor.sh:234` _maintainability_
  `smoke.sh` (and `plugin-check.sh` above) are executed with no timeout and only the *last* output line is surfaced on failure, so a hanging child blocks doctor indefinitely and a multi-line/non-final-line root cause is discarded, making failures hard to diagnose. Consider bounding the invocation (e.g. `timeout`) and reporting more than `tail -n 1` (or pointing the user at the full log).

- [ ] **OCR-329** `scripts/e2e-fly.ps1:16` _maintainability_
  `2>$null` on the MCP invocation discards stderr entirely, which hides the binary's own error output and makes failures hard to diagnose. At minimum capture stderr (or only suppress it when a debug/verbose flag is off) so failing checks can show the underlying cause.

- [ ] **OCR-330** `scripts/e2e-fly.ps1:81` _test_
  Only the last 6 log lines are read (`-Tail 6`), so if additional events (e.g. from prior runs or other tools) push the `recommend`/`feedback` entries out of that window, the checks for neuron events fail spuriously. Consider reading more lines or filtering by a unique task marker to make the assertion robust.

- [ ] **OCR-331** `scripts/env-set.test.sh:13` _maintainability_
  `check` reads `$3` unconditionally, while several call sites pass only two arguments (e.g. "append keeps existing keys", "literal key is replaced"). This happens to work because the script does not set `-u`, but it is fragile: enabling `set -u` later, or copying the helper into a stricter script, would break. Default the detail parameter explicitly.

- [ ] **OCR-332** `scripts/env-set.test.sh:64` _maintainability_
  Several helper invocations are not guarded, unlike the ones in the earlier sections. Under `set -e` a failure here aborts the whole suite with the helper's exit status and no `[fail]` line, making the cause hard to diagnose (and skipping the remaining assertions). Make the guard consistent across all invocations.

- [ ] **OCR-333** `scripts/env-set.test.sh:74` _test_
  The comment states "a single trailing newline is a terminator", but the helper reads stdin via `VALUE=$(cat)`, so command substitution strips *all* trailing newlines. As written the test only covers exactly one trailing newline; multi-newline input (where semantics diverge from the documented behavior) is untested. Add a case such as `printf 'v\n\n'` and assert the actual expected result so the intended contract is pinned down.

- [ ] **OCR-334** `scripts/env-set.test.sh:79` _test_
  Coverage gap: the quoting path in the helper (`enc`) triggers on space, tab, `#`, `"`, `'`, `$`, backtick, `\`, CR and LF, but only space/backslash/newline/`$` are exercised here. Boundary values such as an empty value, a value with a leading/trailing space, one containing `#`, and single quotes are not tested, so regressions in those branches would go unnoticed. Consider adding these cases.

- [ ] **OCR-335** `scripts/gen-mcp-manifest.ps1:27` _bug_
  `$null -eq $template` only catches a zero-byte template, and even that is version dependent (`Get-Content -Raw` returns `$null` on Windows PowerShell 5.1 but `''` on PowerShell 7). A whitespace-only template falls through and is only rejected indirectly by the JSON validation. Check for empty/whitespace explicitly.

- [ ] **OCR-336** `scripts/install-prompts.ps1:14` _maintainability_
  Diagnostics/failures are written with `Write-Host`, which targets the host/information stream rather than stderr (the shell counterparts redirect errors to `>&2`). Callers and CI logs classify them as ordinary output. Emit failure messages via `Write-Error` / `[Console]::Error.WriteLine($Message)` (keep the explicit exit) in both `install-prompts.ps1` and `gen-mcp-manifest.ps1`.

- [ ] **OCR-337** `scripts/install-prompts.sh:12` _bug_
  If `HOME` is unset/empty (e.g. `env -i sh scripts/install-prompts.sh`), this silently resolves to `/.codex/prompts`; the install then creates files in the filesystem root (succeeding when run as root) instead of failing fast. Guard against an unset `HOME` (or empty `CODEX_HOME`) before computing `DEST_DIR`.

- [ ] **OCR-338** `scripts/install-prompts.sh:39` _maintainability_
  The success message is printed unconditionally: it claims prompts were removed even when nothing matched, when `$DEST_DIR` does not exist, or when every file was skipped because it was modified. Additionally, destination prompt files that no longer exist in the source (renamed/removed commands) are never cleaned up and remain installed forever. Track and report the actual number of removals (and consider deleting stale non-source `.md` files that are byte-identical to a previously shipped prompt).

- [ ] **OCR-339** `scripts/plugin-check.ps1:1` _bug_
  Two robustness gaps around the job itself: `Start-Job` can return `$null` (e.g. when the job subsystem is unavailable), in which case `Wait-Job $null` emits a non-terminating error under the `Continue` preference and the script falls into the timeout branch, printing the misleading "timed out after 120s" verdict. Also, `$ErrorActionPreference = "Continue"` combined with the unchecked `[System.IO.Path]::GetFullPath()` calls above means a malformed `source.path` leaves `$cmdFull`/`$sourceFull` `$null` and the subsequent `.StartsWith()` call cascades. Guard the job object after `Start-Job` and validate the resolved paths before using them.

- [ ] **OCR-340** `scripts/plugin-check.ps1:20` _maintainability_
  The shape assumptions here are not validated. An empty `installed` array is falsy, so it produces the misleading message "unexpected ... shape (no installed array)" instead of "plugin not installed"; and if the CLI renames the field or nests `source.path` differently, the script silently falls through to "hackathonkit plugin is not installed" / "has no source.path" and tells the user to re-run setup, masking a real parser/shape mismatch. Validate explicitly (e.g. `$parsed.PSObject.Properties.Name -contains 'installed'` and an array check) and distinguish "shape mismatch" from "not installed".

- [ ] **OCR-341** `scripts/plugin-check.sh:17` _maintainability_
  The EXIT trap is installed while `ERR_FILE` is still unset: if `mktemp` for ERR_FILE fails (or an interrupt fires between the two `mktemp` calls), cleanup runs `rm -f "$JSON_FILE" ""`. This currently relies on `rm -f` silently tolerating an empty operand, which is implementation-dependent. Initialize `ERR_FILE=""` (or assign it before installing the traps) so cleanup is unambiguous.

- [ ] **OCR-342** `scripts/repair.ps1:20` _bug_
  In `repair.ps1`, `$global:LASTEXITCODE` is not reset to 0 before the `install-prompts.ps1 install` call or before `docker info`, unlike the other helper invocations. If the child fails to launch, throws before reaching its `exit`, or resolves to a function/alias wrapper that does not set an exit code, a stale `0` is read and the failure is silently reported as success (the docker branch then wrongly selects the docker runtime). Reset `$global:LASTEXITCODE = 0` before each call (or base the check on `$?`/script existence) and use `Get-Command -CommandType Application` for tool lookups.

- [ ] **OCR-343** `scripts/repair.ps1:57` _bug_
  Several `Test-Path`/`Get-ChildItem` calls omit `-LiteralPath` (e.g. `$bin`/`$demo`/`$ctl` in `repair.ps1`, and the adapter-file probe plus the final `Get-ChildItem -Directory $ClaudeSkills` in `sync-harness.ps1`), unlike the rest of each file. `-Path` interprets wildcard characters `[`/`]`/`*`/`?`, so a repository root containing them can match the wrong path or fail. Use `-LiteralPath` consistently.

- [ ] **OCR-344** `scripts/repair.ps1:61` _maintainability_
  Build outputs and package paths are relative (`bin/...`, `./cmd/...`), so they silently depend on the earlier `Set-Location -LiteralPath $root` having succeeded (it is called without `-ErrorAction Stop`). If the working directory is not what you expect, artifacts can be written outside the repo root. Prefer root-qualified paths, e.g. `go build -o (Join-Path $root 'bin\hackathonkit-mcp.exe') ./cmd/hackathonkit-mcp`, and only build into `bin/` after confirming the directory creation succeeded.

- [ ] **OCR-345** `scripts/setup-resources.sh:34` _bug_
  `read` with the default IFS strips leading/trailing blanks from the line, so a secret that legitimately starts or ends with a space/tab (e.g. a password or a DSN fragment) is silently corrupted before it is handed to `set_key`. Use `IFS=` to preserve the value verbatim.

- [ ] **OCR-346** `scripts/setup-resources.sh:54` _bug_
  Only INT and TERM are trapped. If the shell is terminated by an untrapped signal while `read_secret` has the terminal in `-echo` (SIGHUP when the terminal window closes, SIGQUIT via Ctrl-\), the EXIT trap does not run and the user is left with echo disabled. Trap HUP and QUIT as well.

- [ ] **OCR-347** `scripts/setup.sh:94` _maintainability_
  The `rm -rf` loop is unguarded and silent: it deletes `$dir/$skill` for every name in the hardcoded `EXPLICIT_ONLY_SKILLS` list (which is duplicated again in the echo below), so a renamed/removed skill turns the intent into a silent no-op and stale mirrors are never detected. A mis-resolved `$dir` (e.g. an empty `$ROOT` or a symlinked `.agents/skills`) would also allow deletion outside the repository, and any `rm` failure is swallowed by `set -e`'s exit without a message. Check the directory exists, keep the deletion contained to the repo, and report failures/missing entries so drift is visible.

- [ ] **OCR-348** `scripts/smoke.ps1:16` _maintainability_
  The docker/compose branch is only reached when `bin\hackathonkit-mcp.exe` is absent. A stale or broken local binary therefore silently bypasses the container smoke path, hiding docker/compose regressions. Consider an explicit switch (e.g. `-Runtime local|docker|auto`) and/or verifying the binary's freshness.

- [ ] **OCR-349** `scripts/smoke.ps1:83` _bug_
  `docker compose run --rm` creates a one-off container whose name/labels differ from the service container, and `docker compose kill hackathonkit-mcp` may not match the timed-out one-off run container, leaving an orphaned server process behind. Prefer killing by project/service with an explicit `--project-name`, or capture the container id (`docker compose run -d` / `docker ps --filter`) and `docker rm -f` it. Note also that after `Kill()` the redirected temp files may still be locked on Windows, so the `Remove-Item ... -ErrorAction SilentlyContinue` in `finally` can leak them.

- [ ] **OCR-350** `scripts/smoke.sh:67` _maintainability_
  Auto mode silently switches the runtime based on a `find | head` staleness probe. `find` output order is not deterministic, so `NEWEST` is an arbitrary newer file (the warning may name a different file each run), and a single newer .go file is treated as "the binary is stale" — masking a broken/not-rebuilt local binary and running the test against different code than the developer expects. Consider making the transport explicit/overridable (e.g. honour a SKIP_STALE env var or emit a clear, deterministic decision) and sort the candidate list before picking the newest.

- [ ] **OCR-351** `scripts/smoke.test.sh:111` _test_
  This case is documented as "--compose without docker fails loudly", but `$TMP/bin` (which still contains the fake `docker` stub) is on `PATH`, so the test only verifies that a bogus `HACKATHONKIT_SMOKE_DOCKER` name is rejected — it never exercises the "docker CLI absent" situation it claims to cover. Drop `$TMP/bin` from `PATH` (or remove the stub) for this invocation, or rename/reword the test so the intent matches what is asserted.

- [ ] **OCR-352** `scripts/sync-harness.ps1:77` _maintainability_
  `$ClaudeOld` is only assigned inside the `try`, but it is dereferenced unconditionally in `finally`. This relies on loose variable semantics (it will throw under `Set-StrictMode -Version Latest`, which a user profile could enable) and the deletion logic duplicates the identical check already performed inside the `try`. Initialise `$ClaudeOld = $null` before the `try` and drop the redundant in-try cleanup so the finally block is the single owner of backup deletion.

- [ ] **OCR-353** `scripts/sync-harness.ps1:122` _maintainability_
  The `$Missing` counter is reused for JSON-parse failures, which are a different failure category than "missing adapter file". `sync-harness.sh` deliberately keeps two separate counters (`MISSING` / `INVALID`) and prints a combined summary. Reusing one counter conflates the two categories and, because of the `exit 1` after the first loop, the intended combined summary is never produced. Use a dedicated `$Invalid` counter (and report both).

- [ ] **OCR-354** `scripts/sync-harness.sh:77` _bug_
  The INT/TERM handlers call `finish_harness` and then `exit`, which fires the EXIT trap and runs `finish_harness` a second time. Besides duplicated diagnostics it re-executes the restore/`rm -rf` logic on state that the first invocation may have already modified. Let the signal handlers just exit (or re-raise the signal) and leave cleanup to the EXIT trap.

- [ ] **OCR-355** `scripts/sync-harness.sh:134` _maintainability_
  `ADAPTERS` verifies the plugin-side sources (including `plugins/hackathonkit/commands/*.md`) but not the mirrors this script just wrote into `.opencode/commands` and `.claude/commands`. A silent failure of the mirroring loop above would still be reported as "adapters ready", and only `CMDS_COUNT` (a bare count) covers it. Add the mirrored command paths (or a source/mirror count comparison) so the verification phase actually covers what was generated.

- [ ] **OCR-356** `scripts/sync-skills.sh:44` _bug_
  Content verification is optional: on minimal systems without `diff` the mirror is published based only on directory/file/`SKILL.md` counts, so a truncated or corrupted file (same entry count) passes as a valid mirror. `diff -r` also does not compare file modes or symlink targets. Either require `diff` (fail with a clear diagnostic when it is missing) or fall back to a portable content check (sorted file list compared with `cmp`, or checksums) before swapping.

- [ ] **OCR-357** `scripts/uninstall.ps1:28` _bug_
  `Test-SameTree` only compares file path+hash sets, so two directories that differ solely in empty subdirectories (or two empty directories) compare equal. If the shipped skill happens to consist only of empty directories, a user's own empty skill sharing that name would be treated as unmodified and deleted. Consider comparing the directory structure (e.g. include relative directory entries) rather than only files.

- [ ] **OCR-358** `scripts/uninstall.ps1:88` _bug_
  `Get-ChildItem -Directory` is called without `-Force`, so hidden shipped skill directories would never be enumerated or removed, while `Test-SameTree` itself uses `-Force` when hashing trees. This inconsistency can leave installed skills behind; add `-Force` for parity.

- [ ] **OCR-359** `scripts/uninstall.sh:60` _maintainability_
  `codex` availability is probed twice with two separate `if` blocks. Consolidate into a single `command -v` check with an `else` branch to avoid a redundant lookup and the risk of the two branches drifting apart.
