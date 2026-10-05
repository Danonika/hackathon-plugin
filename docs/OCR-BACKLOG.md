# OCR Backlog (full scan)

Source: `ocr scan` - 420 findings across 126 files.

Workflow: fix one at a time, commit each, push, then re-run OCR against the commit. `[ ]` = open, `[x]` = fixed.

Progress: **48 of 359** fixed and verified against pushed commits (all criticals, all highs, OCR-025..048 mediums). Commits v0.16.32..v0.16.85. Next: OCR-049 (medium/bug).

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

- [x] **OCR-025** `.claude-plugin/marketplace.json:3` _bug_
  The `owner` object only declares a `name`. The Claude Code marketplace schema expects owner identification to include an `email` alongside `name`; a marketplace manifest without it can be rejected/incompletely registered by `claude plugin marketplace add`. Note also that none of the repo's validators would catch this: `scripts/sync-harness.sh` / `scripts/doctor.sh` only check that the JSON parses, not its schema. Add the owner email (or confirm the field is genuinely optional for the targeted CLI version).

- [x] **OCR-026** `.claude-plugin/plugin.json:29` _bug_
  The MCP entrypoint hard-codes the POSIX launcher `hackathonkit-mcp-launcher.sh`. The repo explicitly ships a Windows counterpart (`plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.cmd`) and the generated runtime manifest selects it per-platform (`scripts/gen-mcp-manifest.ps1` substitutes `{{MCP_LAUNCHER}}` with the `.cmd`). Because this static plugin manifest cannot do that substitution, a Windows Claude Code install will try to spawn the `.sh` directly and fail to start the `hackathonkit` MCP server even though a working `.cmd` launcher exists. This is the previously logged M1/S3-M1 regression (`docs/REVIEW-BACKLOG.md:66`, `:466`) that is marked fixed but still present. Make the entrypoint platform-neutral (e.g. a small node/sh dispatcher that picks `.cmd` on Windows) or point `mcpServers` at the generated `.mcp.json`, which is produced with the platform-correct launcher.

- [x] **OCR-027** `.codex/agents/evaluator.toml:14` _security_
  The comments and instructions overstate containment. `sandbox_mode = "read-only"` only blocks filesystem writes; read/process execution is still allowed. `approval_policy = "never"` merely removes the approval/escalation prompt and does not deny command execution. The instruction saying containment "depends on this, not on the sandbox" also contradicts the header claim that command execution and network are denied by configuration. Reconcile the header and instructions to state the residual reliance on prompt compliance (or actually constrain execution). The same issue appears in both `evaluator.toml` and `reviewer.toml`.

- [x] **OCR-028** `.codex/agents/researcher.toml:9` _security_
  `sandbox_mode = "read-only"` restricts filesystem writes but does not itself deny process execution or network access. Unlike the sibling `reviewer.toml` (which adds `approval_policy = "never"` and `network_access = false` for exactly this reason), this agent ingests attacker-controllable content (fetched pages, documents, MCP snippets), so its anti-injection rule currently rests on prompt compliance alone. An embedded instruction could still drive a shell command or outbound request. Add an explicit approval/network policy here (at minimum `approval_policy = "never"`; decide network explicitly given `web_search` is disabled at repo level).

- [x] **OCR-029** `.codex/agents/researcher.toml:10` _bug_
  This caveat (MCP runs outside the sandbox, so `reports.create`/`learning.record` may persist state) lives only in a TOML comment, which is never surfaced to the model. Combined with the declared read-only sandbox, the agent may conclude it must not persist anything and skip `reports.create`, silently failing the workflow's persistence step and the required `theme-research-<slug>` artifact id. Move this rule into `developer_instructions` (as `reviewer.toml` does for its read-only semantics), e.g. state that the sandbox is read-only for its own commands while the Hackathon Agent Kit MCP server persists the brief/learning evidence.

- [x] **OCR-030** `.codex/agents/reviewer.toml:24` _security_
  The untrusted-content rule covers prompt injection but says nothing about secrets, while `what_breaks`/`repro` will quote attacker-controllable evidence (test output, gate reports, artifact text) that can contain credentials. Unlike the researcher and evaluator profiles, there is no instruction to redact secrets. Also state explicitly that embedded instructions must not alter the output contract itself (only the contract above defines the emission format), and that MCP/tool envelope output is included in "untrusted content".

- [x] **OCR-031** `.codex/config.toml:3` _maintainability_
  The placement of `web_search` and `persistent_instructions` before the first `[table]` header is load-bearing for TOML semantics but is enforced only by this comment. A future edit that moves either key below `[mcp_servers.hackathonkit]` silently folds it into that table and the global setting disappears with no parse error. Consider a CI check that asserts these root-level keys remain table-free, so the invariant is machine-verified rather than relying on developer discipline.

- [x] **OCR-032** `.codex/config.toml:15` _bug_
  `persistent_instructions` unconditionally tells the agent to call `system.context` and to "never claim a result without a tool result", but the MCP server it depends on cannot start on Windows (no `sh`) without a manual `scripts/setup.ps1` run. On such installs the agent receives instructions to use an unavailable tool with no guidance that it is missing, producing confusing behavior rather than a clear failure. The Windows path should be made automatic (platform-selected `.cmd` launcher) or the instruction should state how to detect the server's absence.

- [x] **OCR-033** `.codex/config.toml:22` _bug_
  Windows correctness depends entirely on a manual out-of-band step: `sh` does not exist there, so the server fails to register unless the user runs `scripts/setup.ps1` or hand-edits `command`/`args`. Since this checking-in file is static, it bypasses the platform-aware generation already present in `scripts/gen-mcp-manifest.ps1`/`.ps1:36` (which substitutes `{{MCP_LAUNCHER}}` with `hackathonkit-mcp-launcher.cmd`). Prefer generating this entry the same way as `plugins/hackathonkit/.mcp.json`, or make the launcher platform-neutral, so the MCP server is not silently unregistered on Windows.

- [x] **OCR-034** `.codex/config.toml:26` _bug_
  Nesting `$(git rev-parse --show-toplevel ...)` inside the outer double-quoted `args` value means the resolved path is substituted directly into the inner shell's command word. A repo path containing spaces, `$`, backticks, or quotes will word-split or undergo further expansion inside the inner `sh`, launching the wrong file or failing. Pass the root as a separate positional argument instead of interpolating it into the command string.

- [x] **OCR-035** `.codex/config.toml:26` _maintainability_
  Redundant double `sh` wrapper: `command = "sh"` + `args = ["-c", "exec sh ..."]` spawns `sh -c` only to spawn another `sh`, while the launcher already carries its own shebang. The extra layer exists solely to resolve the path (finding above), and any failure inside it surfaces only as a raw shell error on the MCP stdio channel. Point `command` at the launcher directly with an absolute path once the root is resolved.

- [x] **OCR-036** `.dockerignore:9` _security_
  `.dockerignore` only covers the root dirs and `.env*`, missing many patterns that `.gitignore` already excludes: `node_modules/`, `__pycache__/`, `*.pyc`, `.venv/`, `coverage/`, `htmlcov/`, `/tmp/`, `*.log`, plus generated/sensitive install state (`configs/install-state.json`, `configs/resources.json`, `plugins/hackathonkit/.mcp.json`, `.claude/skills/`). None of these are excluded from the build context, so a developer's local state (e.g. after `npm install` or launcher run) is sent to the Docker daemon, bloating the context, risking leakage, and busting the `go mod download` layer cache on every build. The project's own checklist (`plugins/hackathonkit/skills/devops-expert/references/docker-review-checklist.md:11`) requires `.dockerignore` to list `node_modules/`, and backlog item M13 was marked fixed without the pattern being added. Add the missing patterns to `.dockerignore` (with `**/` prefixes to match at any depth).

- [x] **OCR-037** `.gitattributes:1` _bug_
  The global `eol=lf` forces the working tree to LF for every text file, including formats that are not enumerated below and that Windows-native tooling expects in CRLF (e.g. `.sln`, `.csproj`, `.vcxproj`, `.vcxproj.filters`, `.props`, `.targets`, `.reg`). Because `eol=lf` overrides `core.autocrlf`/`core.eol`, Windows-only tooling can read/rewrite these files and produce spurious or non-applying diffs. If this repo is genuinely Go-only this is fine, but if Windows project files may be added later, either add them to the CRLF block below or drop `eol=lf` from the global default and rely on `text=auto`.

- [x] **OCR-038** `.gitattributes:24` _bug_
  The binary list omits many common binary artifacts (e.g. `.exe`, `.dll`, `.so`, `.dylib`, `.a`, `.o`, `.jar`, `.class`, `.wasm`, `.pyc`, `.bmp`, `.tiff`, `.avif`, `.7z`, `.tar`). `text=auto` relies on a NUL-byte heuristic that can misfire on small binary files, causing them to be treated as text and line-ending-normalized (data corruption). Enumerate the binary formats actually used by this repo explicitly rather than depending on auto-detection.

- [x] **OCR-039** `.github/workflows/ci.yml:0` _security_
  This redactor is materially weaker than the one in `scripts/docker-check.sh` (which already emits redacted diagnostics itself):
  
  1. Case-sensitive keywords: only `PASSWORD|PASS|TOKEN|SECRET|DSN|KEY` in upper case match, so lowercase/mixed-case keys (`password=`, `api_key=`, `secret:`) are printed verbatim. The script uses `[Pp][Aa][Ss]...` character classes to stay case-insensitive.
  2. Quoted values are not redacted at all: the value pattern `[^ "]+` cannot match a leading `"`, so `POSTGRES_PASSWORD="s3cr3t"` (and other values that compose/quote) passes through unredacted. The script handles this with `("[^"]*"|[^ " ]+)`.
  3. If `sed -E` is unavailable the sed fails and, because of `|| true`, the step silently continues with no notice; the script explicitly fails closed with a message.
  
  Since the redaction rules are duplicated here, they drift from the tested implementation. Best fix: reuse the shared redactor (or extract it into a sourceable script) instead of maintaining a second copy.

- [x] **OCR-040** `.github/workflows/ci.yml:42` _security_
  The rehearsal report is dumped verbatim, unlike the docker job which carefully pipes diagnostics through a redactor. `cmd/hackathonkit-rehearse` records step `evidence`/`error` strings and includes MCP-server stderr (`lastLines(stderr.String(), 2)`), which can embed configuration or connection details in the JSON. On failure the whole file is printed to the public build log. Redact the output before printing (reusing the same filter as the docker job) or upload `rehearsal.json` as an artifact instead of echoing it.

- [x] **OCR-041** `.gitignore:21` _maintainability_
  Both the `data/` and `evals/*/` blocks are hand-enumerated allowlists. Any new runtime subdirectory under `data/` (e.g. `data/quality/`) or new eval artifact kind (`evals/<new-kind>/<sha>/…`) is silently NOT ignored and will be committed on the next `git add .`. For `data/`, the two directories that must stay tracked are `data/input/` (committed sample corpus) and `data/fly/` (`data/fly/dataset.jsonl` is committed), so both need explicit negations. For `evals/`, only the tracked `evals/README.md` is at the top level, so a directory-only catch-all is safe and keeps the README tracked. Adopting the suggested catch-alls is safer.

- [x] **OCR-042** `Dockerfile:41` _security_
  The `quality` stage runs as root over a host bind mount, so `scripts/quality-gate.sh` creates `evals/quality/...` (and any other file it writes) owned by root in the host working tree, while the `hackathonkit-mcp`/`demo-api` services run as `${HACKATHONKIT_UID}:${HACKATHONKIT_GID}` (1000:1000). A later gate run through those services — or by the host user — then fails with EACCES on the root-owned `evals/quality` directory. Document the supported invocation (`docker compose run --rm --user "$(id -u):$(id -g)" quality`, as already noted for the cache volumes) or have the gate write into a directory that is chowned back to the runtime UID.

- [x] **OCR-043** `cmd/demo-api/fly_live.go:120` _performance_
  `refresh()` does blocking, potentially heavy work while holding `f.mu`: `os.Stat` + `os.Open` + `file.ReadAt` of up to 4 MiB, plus `json.Unmarshal`/`json.Marshal` of every complete line. Every SSE subscriber owns a 250 ms ticker and calls `snapshot()` -> `refresh()`, and the `?snapshot=1` REST path shares the same lock, so all connections plus the snapshot endpoint serialize behind one disk read. On a slow/stalled filesystem (e.g. NFS) a single read can stall every connected dashboard client. A single shared poller goroutine that owns the file state and publishes an immutable snapshot (e.g. via an atomic pointer), or reading outside the lock and only locking to publish, would remove the head-of-line blocking. Note the mutex is a `sync.Mutex`, so even the cheap fast-path `Stat` comparison serializes all readers unnecessarily.

- [x] **OCR-044** `cmd/demo-api/main.go:343` _bug_
  The revision guard is skipped whenever the client omits `revision` (zero value) or sends a negative number, so a stale client that never sends `revision` can overwrite an approval taken against a newer plan revision — exactly the lost-update the `decisionMu` + revision check exist to prevent. Require a valid revision instead of silently bypassing the check.

- [x] **OCR-045** `cmd/demo-api/main.go:451` _bug_
  `readApproval` collapses every failure into `nil`: a missing file, a permission error, and a truncated/corrupt JSON record are all reported as "no approval". `/plan/api/latest` then returns `"approval": null`, so a corrupt approval is indistinguishable from "not yet decided" and the UI silently loses an existing decision. Distinguish `os.IsNotExist` (return nil, nil) from real read/parse failures and surface them (e.g. change the signature to `(*planApproval, error)` and log/return the error) rather than treating them as absence.

- [x] **OCR-046** `cmd/fly-bench/main.go:231` _bug_
  When `cfg.Routing.TopK <= 0` (a state the config layer does not forbid) this branch silently uses *all* returned recommendations as k, while `report.Summary.TopK` still records the raw config value (0). Meanwhile `fly.NewRouter` defaults `topK` to 5 for the very same settings, so the published "fly recall@k" / "top_k" no longer describe the top-k the router actually applied. Normalize the effective k once (matching `fly.NewRouter`'s default) and use it for both the recall computation and the report.

- [x] **OCR-047** `cmd/hackathonkit-ctl/daemon.go:134` _bug_
  `store.Close()` is deferred at the top of `daemonForeground`, but nothing waits for the shutdown goroutine below to finish draining in-flight handlers. When `ctx` is cancelled, the goroutine calls `server.Shutdown`, and `ListenAndServe` returns `http.ErrServerClosed` immediately — *not* after handlers complete. The function then returns, the deferred LIFO order runs `ingestWG.Wait()` and `store.Close()` while the server may still be executing handlers, so concurrent requests can fail with `sql: database is closed` and the pid file is removed before the server is really down. Await the shutdown goroutine (e.g. close a `done` channel after `Shutdown` returns) before returning so `store.Close()` only runs once serving has stopped.

- [x] **OCR-048** `cmd/hackathonkit-ctl/daemon.go:230` _security_
  Returning `true` here means any live PID recorded in the pid file is reported as "the daemon" on non-Linux or restricted-/proc systems, because `readPID` only requires `processAlive` in that case. A stale or recycled PID then makes `daemonStop` send SIGTERM to an unrelated process. Combined with the brittle substring match on the executable path below, `daemon start/stop/status` are unreliable: if the binary is installed/named anything other than `.../hackathonkit-ctl`, the identity check fails and `start` reports a false "did not start" while `stop` reports "not running". Consider persisting the daemon's executable path (or start time/inode) in the pid file and comparing it exactly, and defaulting the unverifiable case to `false` (requiring an explicit force flag for `stop`) instead of assuming ownership.

- [x] **OCR-049** `cmd/hackathonkit-ctl/daemon.go:313` _security_
  This Origin/Referer heuristic can be bypassed for the one state-changing GET endpoint. A page can send `<img src="http://127.0.0.1:8788/api/risk">` (or a plain link) after setting `<meta name="referrer" content="no-referrer">`; sub-resource/navigation GETs omit Origin, and with the no-referrer policy Referer is omitted too, so both header checks are empty and `RecomputeRisk` (a DB write) executes cross-site. Since `/api/risk` is explicitly listed here but only guarded when a header happens to be present, either drop the endpoint's mutating behavior into a POST method or reject `/api/risk` whenever `token == ""` regardless of Origin/Referer (or gate on `Sec-Fetch-Site`/`Sec-Fetch-Mode`).

- [x] **OCR-050** `cmd/hackathonkit-ctl/main.go:394` _maintainability_
  `cmdRun` accepts `stdout, stderr io.Writer` but bypasses them for user-facing output: the final summary uses `fmt.Printf` (hard-wired to `os.Stdout`) and two bookkeeping failures use `fmt.Fprintln(os.Stderr, ...)` (lines ~345 and ~350), while other messages in the same function correctly use `stdout`/`stderr`. Callers that pass a buffer (e.g. `run(args, &out, &errOut)`) silently miss this output, which breaks the injectable-`run()` contract this file establishes. Use the injected writers consistently.

- [x] **OCR-051** `cmd/hackathonkit-ctl/proc_unix.go:17` _maintainability_
  Both platform implementations of the process-liveness check treat a permission failure as not-alive, and readPID (cmd/hackathonkit-ctl/daemon.go:216) turns that false into a stale verdict: daemonStop skips terminating a running daemon and deletes its pid file, and daemonStart can spawn a second copy. On POSIX (proc_unix.go) the EPERM branch is a raw error-identity comparison against syscall.EPERM that silently breaks if the error is ever wrapped; use errors.Is(err, syscall.EPERM) instead. On Windows (proc_windows.go) every OpenProcess failure is collapsed into false, but only ERROR_INVALID_PARAMETER (87) means no such pid while ERROR_ACCESS_DENIED (5) must be treated as alive (matching the POSIX intent). Fix both files so an unprivileged-but-live daemon is reported correctly.

- [x] **OCR-052** `cmd/hackathonkit-ctl/tui.go:92` _bug_
  An events-only query failure is stored in the shared `m.err`, but `View()` returns as soon as `m.err != nil`, so a transient failure of the events query hides the otherwise-valid session list (and the whole body) — contradicting the "may be an events-only failure" intent here. Keep the two failures distinct (e.g. a separate `eventsErr` field) and render the events error inline next to the events pane instead of discarding the sessions view.
  
  Relatedly, the `if m.err != nil { ...; return b.String() }` branch in `View()` should only bail out when the sessions themselves are unavailable.

- [x] **OCR-053** `cmd/hackathonkit-mcp/main.go:397` _bug_
  The error from `store.Top` is silently discarded: only `err == nil` is handled, so an unreadable/corrupt learnings store (`Top` returns the error from `s.load()` when the JSON file cannot be read/parsed) produces exactly the same output as "no learnings" — the session hook gets no signal that injection failed. The sibling branches in this function do surface failures ("resources: profile unreadable", the `countFiles` diagnostic), so this is inconsistent as well as lossy. Report the failure to `diag`, and also treat a successful-but-empty result as "no learnings" rather than a failure.

- [x] **OCR-054** `cmd/hackathonkit-rehearse/main.go:198` _bug_
  `rep.OK` is assigned before `writeReport`, so when the report cannot be written the local `ok` becomes false (exit code 1, non-JSON output prints FAIL) but `rep.OK` stays `true`. In `--json` mode the JSON emitted on stdout therefore claims `"ok": true` while the process exits 1, which lets a machine consumer read a failed run as success. Keep `rep.OK` consistent with the failure.

- [x] **OCR-055** `configs/resource-matrix.yaml:15` _bug_
  Variant ordering contradicts this file's own contract. The header states the planner "picks the first variant whose provider is ready; variants without a provider ... act as deterministic fallbacks", but here (and identically in `timeseries`, `classification` and `retrieval`) the provider-less `deterministic` entry is listed first, while `reasoning`/`embeddings`/`asr`/`vision`/`tts`/`notifications`/`metadata`/`artifacts` correctly put the fallback last.
  
  Because `resources.Plan` (internal/resources/matrix.go:113-119) picks the first variant whose provider is empty *or* ready, the provider-less entry always wins: the `openai` variant is only ever surfaced under `alternatives`, the capability is never reported as `degraded`, and the `missing[]` upgrade hint (`OPENAI_API_KEY`) is silently lost even when the key is present. It also breaks the fallback assumption in `Plan` (matrix.go:122-131) that the terminal variant is provider-less — for these four capabilities the last entry is provider-backed, so any reorder would select an unready provider as the "fallback".
  
  Prefer listing the provider-backed variant first and `deterministic` last (matching the other capabilities). Note that `resources.DefaultMatrix()` (internal/resources/matrix.go:229-244) and the `TestRepoMatrixMatchesDefaults` regression test (internal/resources/matrix_test.go:26-35, `reflect.DeepEqual`) must be updated in lockstep or the test will fail.

- [x] **OCR-056** `docker-compose.yml:14` _bug_
  The comment's premise is wrong, so this gating is a real startup race: the MCP opens the object store exactly once at startup (`cmd/hackathonkit-mcp/main.go:298-307`), and if `objectstore.Open` fails (MinIO's process is “started” long before the S3 API on :9000 accepts connections, so `BucketExists`/`MakeBucket` can get connection-refused) the error is only logged as `objectstore_disabled` and `objects` stays nil for the whole process lifetime — there is no “reconnect on first use”. Result: artifact/blob mirroring is silently lost for the entire session after a plain `docker compose up`. Add a healthcheck that probes MinIO's S3 endpoint (e.g. `test: ["CMD", "mc", "ready", "local"]`, or `/minio/health/live`) and gate on `service_healthy`; otherwise the MCP needs retry-on-first-use instead of a one-shot Open.

- [x] **OCR-057** `internal/atomicfile/atomicfile.go:35` _bug_
  `os.MkdirAll` only applies `dirMode` to directories it actually creates; it never changes the mode of a pre-existing directory. So the guarantee claimed by the comment above ("Least-privilege state files get a private containing directory") does not hold when the parent directory already exists with 0o755 — the 0o600 state file then sits in a world-listable/traversable directory. If the private-directory guarantee is intended, tighten the existing directory explicitly (e.g. `os.Chmod(dir, dirMode)` for the private case), otherwise soften the comment to say it only applies to newly created directories.

- [x] **OCR-058** `internal/atomicfile/atomicfile.go:41` _bug_
  The stale-temp reclamation deletes *any* file in the target directory whose name matches `<base>.` + `*` + `.tmp` and is older than one hour. That name space is not exclusively owned by this package, so an unrelated file such as `gate.json.old.tmp` (or a temp left by another tool / another process writing the same base name) will be silently deleted. The same heuristic can also remove a *live* temp file of a concurrent writer that has been stalled/suspended for more than an hour, making that writer's subsequent `Sync`/`Rename` fail with ENOENT. Since the reclamation is best-effort, prefer a package-unique marker in both the prefix and the `os.CreateTemp` pattern so only files this package created are ever candidates.

- [x] **OCR-059** `internal/config/config.go:390` _bug_
  The routing version is read before `m.mu` is acquired, so the read-modify-write is not atomic across `Manager` instances/processes. `./configs` is bind-mounted into both `demo-api` (cmd/demo-api/main.go:376) and `hackathonkit-mcp` (cmd/hackathonkit-mcp/main.go:197), and each holds its own `Manager` with its own mutex. Both can read version N and then write N+1, so the later write silently discards the earlier mode change and the version never reaches N+2. The reconciliation below only compares against the in-process `m.routingCache`, so it cannot detect this. Serialize writers with an OS-level file lock (e.g. flock on a lock file) around the read-modify-write, or explicitly document that only a single writer process is supported.

- [x] **OCR-060** `internal/config/config.go:398` _performance_
  `yaml.Marshal` and `atomicfile.Write` (which does MkdirAll, ReadDir, CreateTemp, fsync, rename and directory fsync) run while holding `m.mu`, the same mutex used by `Config()`, `Routing()` and `Evaluation()` on request/worker paths. The comment above states the read is deliberately done outside the lock so a slow/bind-mounted filesystem cannot serialize config access, yet the actual disk write still blocks every reader behind it. Serialize writers with a dedicated mutex (separate from the cache mutex) and/or perform the file write outside `m.mu`, taking the lock only to publish `m.routingCache`.

- [x] **OCR-061** `internal/config/dotenv.go:93` _bug_
  `unquoteEnvValue` treats the first unescaped `"` as the end of the value and silently discards everything after it, so a shell-legal line can load a different value than the file specifies. For `KEY="abc"def` the process gets `abc` (a shell would export `abcdef`), and the single-quoted branch has the same truncation (`KEY='abc'def` -> `abc`). Because the value is applied with no warning, a hand-edited `.env` can quietly load a wrong secret/config value (the same silent-corruption class the escape handling was fixed for). Consider only accepting the closing quote when the remainder is empty, whitespace, or a `#` comment (which is what trailing comments rely on); otherwise keep the raw value or report the malformed line.

- [x] **OCR-062** `internal/control/handoff.go:35` _bug_
  `plans.LatestErr` deliberately returns a non-nil error alongside `ok=true` when a plan was found but the newest plan file was unreadable/corrupt (plans.go:176-179). Here `planErr` is only inspected in the `else` branch, so a partially-unreadable plan set is silently swallowed: the handoff renders a possibly stale plan with no warning, defeating the documented purpose of `LatestErr`.

- [x] **OCR-063** `internal/control/handoff.go:63` _performance_
  Both decision counts materialize up to 10000 complete event rows (the same is done again by the scoped `store.Events(ctx, sessionID, 10000)` below) and `scanEvents` JSON-unmarshals every payload, just to derive a single integer — up to ~20k rows and map allocations per handoff. A dedicated `SELECT COUNT(*) ... WHERE type='eval_decision'` would be far cheaper. Note also that the fixed `LIMIT 10000` (store.go:340-343) truncates silently, so a busy store under-reports `PendingDecisions` while the handoff still prints an exact-looking number; a COUNT query also removes that ambiguity.

- [x] **OCR-064** `internal/control/ingest.go:117` _bug_
  Any error from `s.Cursor(ctx, name+":head")` is silently swallowed by `cerr == nil`. A transient store error therefore skips rotation detection entirely and the reader keeps seeking to a stale offset, potentially skipping the head of a recreated file with no error surfaced. Additionally, treating `stored != 0` as "set" conflates a legitimate FNV hash of 0 with "unset", disabling rotation detection for that file. Surface the read error instead of folding it into the boolean condition.

- [x] **OCR-065** `internal/control/ingest.go:175` _bug_
  When `handle` fails and `flush(lastGood)` also fails, the original record-processing error `err` is discarded and only `cerr` is returned. Callers lose the real cause (e.g. a bad record vs. a cursor write failure), which makes diagnosis of the failed ingest much harder. Preserve both errors.

- [x] **OCR-066** `internal/control/ingest.go:182` _bug_
  `AppendEvent` (store.go) is a plain `INSERT` with no dedup key, so it is not idempotent. Because the cursor is only persisted every `cursorFlushEvery` records (and on error/exit), a crash between appends and `SetCursor` will permanently duplicate `session-events`, `mcp-calls` and `fly-feedback` events on the next run, inflating `Events`/risk inputs. This directly contradicts the `Ingest` doc comment ("repeated runs never duplicate records"). Either persist the cursor per record, give events an idempotency key (e.g. source+line hash) so re-inserts are no-ops, or soften the documented guarantee.

- [x] **OCR-067** `internal/control/ingest.go:191` _bug_
  The head fingerprint is only refreshed when `hasFP` is true, i.e. the current file is >= 256 bytes. If a file rotates (by size or fingerprint) into a new file whose head is still shorter than 256 bytes, the stale head cursor from the previous file is left in place. Once that new file grows past 256 bytes, its head no longer matches the stale value, so a spurious rotation is detected, `offset` is reset to 0 and the already-ingested prefix is re-read — producing duplicate events. Clear `name+":head"` whenever a rotation is detected to avoid this re-read.

- [x] **OCR-068** `internal/control/ingest.go:291` _bug_
  Two problems here: (1) `os.ReadFile` reads the entire decision file with no `maxIngestBytes`-style bound, so a large/growing file can exhaust memory; (2) a single unreadable file returns immediately, aborting the whole `Ingest` (skipping the remaining decision files and, importantly, `RecomputeRisk`/`Counts`) on every subsequent cycle as long as that file stays unreadable. Consider bounding the read and skipping/continuing (logging) on a per-file read error so one bad file cannot indefinitely block the rest of ingestion.

- [x] **OCR-069** `internal/control/risk.go:84` _performance_
  `payloadHasPanic` compiles the negation regex on every call, i.e. once per `mcp_call` event, inside the per-event loop that itself runs for every session in `RecomputeRisk`. For a large events table this is a substantial repeated allocation/compile cost. Hoist it to a package-level var next to `panicWord`. (Also, `strings.ToLower` above already lowercases the text, so the `(?i)` flag here is redundant but harmless.)

- [x] **OCR-070** `internal/control/risk.go:100` _maintainability_
  Truncation is only logged for the session query. `Events` and `EventsBetween` are also limited to `riskScanLimit`, and both clamp their limit to 10000 (falling back to a small default if exceeded). A session with more than `riskScanLimit` events, or a busy time window with that many global events, is scored on only the newest rows and silently under-counts failures with no warning. Log truncation for the event queries too (or check `len(events) == riskScanLimit` / `len(globals) == riskScanLimit`) so the under-scoring is observable.

- [x] **OCR-071** `internal/control/store.go:190` _bug_
  `UpsertSession`'s conflict branch unconditionally overwrites every metadata column with `excluded`, so a partial/replayed record (e.g. a Session built with only `ID`/`Status`, as `EndSession` and various callers do) silently erases previously stored `root`, `kind`, `task`, `stage`, `plan_slug`, and `worktree`. This contradicts the deliberate preservation logic used elsewhere (`StartSession` guards `root` with `COALESCE(NULLIF(?,''), root)` and the conflict branch already preserves `ended_at`/`status`). Guard the text columns so an empty incoming value does not clobber stored data.

- [x] **OCR-072** `internal/control/store.go:241` _bug_
  `EndSession` always rewrites `status`, `ended_at`, and `updated_at` for an existing row, so it is not idempotent: a replayed or out-of-order `session_end` (the ingester replays JSONL from a reset cursor / on fingerprint-detected rewrites) moves `ended_at` — and `updated_at`, which drives `Sessions()` ordering — backwards. This is inconsistent with `UpsertSession`, which deliberately keeps the first `ended_at` via `COALESCE(sessions.ended_at, excluded.ended_at)`. Preserve the earliest end time and never move `updated_at` backwards.

- [x] **OCR-073** `internal/evaluation/bundle.go:269` _bug_
  The previous bundle is deleted before the new one is renamed into place, so there is a window where the committed bundle no longer exists. If the process crashes/ is killed between the two calls, or `os.Rename` fails (concurrent run making `outAbs` non-empty again, permission/IO error), the previous bundle is lost — contradicting the comment above ("a failed run never destroys the previous bundle"). Prefer moving the live bundle aside (`os.Rename(outAbs, outAbs+".bak")`), swapping the new bundle in, and only then removing the backup (rolling the backup back if the swap fails). The `removeOwnedBundle` ownership/symlink validation should still run before touching the live directory.

- [x] **OCR-074** `internal/evaluation/evaluation.go:172` _bug_
  `Total` is the only numeric field not checked for non-finite values (scores and deduction points are). For `Total = NaN`, `math.Abs(NaN-sum) > 0.01` is false, so the report validates, and `reportTotal` then returns NaN (`NaN != 0` is true) — poisoning `MeanTotal`/`ScoreVariance` and making every `compareEntries` tie-break evaluate false, i.e. all entries look tied. Add an explicit finite check before the sum reconciliation.

- [x] **OCR-075** `internal/evaluation/evaluation.go:508` _bug_
  Only `NoiseFloor` is guarded against NaN/Inf; `Candidate`, `Best` and `Target` are used unchecked. With a NaN `Candidate` every comparison below evaluates false (`in.Candidate < in.Best-epsilon`, `in.Candidate >= in.Target-epsilon`, `improvement < in.NoiseFloor-epsilon`), so execution falls through to `DecisionAccept`; `+Inf` behaves the same way (`improvement` = +Inf is never `< floor-epsilon`). A corrupt/overflowing score is therefore silently accepted instead of failing closed. Note the exported struct exposes `Comparison.InputError`, which is declared but never populated anywhere in this file. (Todays `eval.compare` call site filters non-finite args via `argFloat`, but this exported function accepts raw floats.)

- [x] **OCR-076** `internal/learning/learning.go:205` _maintainability_
  A newly created global-scope learning still records `Project: s.project`, whereas Promote explicitly clears `Project` for global entries (and the update path here also clears it). This leaves two different on-disk representations of "global" learnings: one with a stale project tag and one without. Today `inScope` happens to treat both correctly, but any future projection-by-`Project` query or migration will disagree. Set `Project` only for project scope on creation.

- [x] **OCR-077** `internal/mcp/registry.go:33` _bug_
  `Registry` is exported but `tools` is only initialized by `NewRegistry`. A zero-value `&Registry{}` (which is legitimate to construct externally) makes `Register` write to a nil map and panic with "assignment to entry in nil map". Make the zero value usable by lazily initializing the map here.

- [x] **OCR-078** `internal/mcp/registry.go:94` _bug_
  Incomplete deep copy: `cloneSlice` only recurses into `map[string]any` and `[]any`. Any other container nested inside a `[]any` (e.g. `[]string`, `map[string]string`, `[]map[string]any`, typed slices/maps) hits `default` and the returned copy still shares the same backing storage as the registered tool. This contradicts the documented "deep-copies the schema" guarantee in `cloneTool`: a caller that mutates the returned schema could then corrupt registry state (and race with concurrent readers). `cloneMap` already handles `[]string`/`map[string]string`, so the two helpers are inconsistent; mirror those cases here or factor a single `cloneValue(v any) any` helper used by both.

- [x] **OCR-079** `internal/mcp/server.go:78` _bug_
  The reader goroutine's only exit path while blocked inside `bufio.Read` is `closer.Close()`. Because `in` is declared as `io.Reader`, nothing forces callers to pass a Closer (the tests already pass `strings.NewReader`), and for descriptors whose `Close` does not interrupt an in-flight blocking read (e.g. a terminal fd), the goroutine keeps running after `ServeStreams` returns, retaining the `bufio.Scanner` buffer that can grow to 32MB. Consider making the requirement explicit (accept `io.ReadCloser`, or document it on the exported method) so a pass-through reader cannot silently leak a goroutine per session.

- [x] **OCR-080** `internal/mcp/server.go:265` _bug_
  This error envelope sets `Status`, `Data["error"]` and `TraceID` but leaves the `Envelope.Error` field empty, while `Fail()` (contracts.go) always populates it. Clients that read the top-level `error` field of the envelope (the canonical error attribute) will see an empty string for handler failures, but a populated one for tool-level failures produced via `Fail`. Use the existing helper to keep the wire shape consistent.

- [x] **OCR-081** `internal/metadata/metadata.go:126` _bug_
  The `migrations` block adds `mode`/`version`/`kind` as nullable, while the `schema` block declares them `NOT NULL` for fresh databases. Because `CREATE TABLE IF NOT EXISTS` is a no-op on an existing deployment, an upgraded database ends up with weaker constraints than a freshly created one, so the same INSERT can succeed on one deployment and be rejected on another. Reconcile the constraints explicitly (backfill + `ALTER COLUMN ... SET NOT NULL`), or consciously relax the `schema` definitions so both paths match.

- [x] **OCR-082** `internal/metadata/metadata.go:149` _bug_
  `isDuplicateDDL` classifies errors by substring match on the rendered message. Any unrelated DDL failure whose text happens to contain "already exists" (or the literal substrings "42P07"/"42710") is silently treated as a benign duplicate and startup continues on a half-created schema. Classify by the structured `*pgconn.PgError` SQLSTATE instead (`pgconn` is already imported).

- [x] **OCR-083** `internal/openai/client.go:252` _bug_
  io.LimitReader returns EOF at the limit and gives no truncation signal, so an oversized response body is silently cut mid-JSON and the failure is misreported. internal/openai/client.go reads the transcription body with a bare 32<<20 LimitReader (larger bodies are truncated and the later json.Unmarshal fails with a misleading unexpected-end-of-JSON error). internal/resources/probe.go reads 1<<20 bytes and ignores the subsequent parse error entirely, reporting Reachable: true with 0 models so a genuine empty list is indistinguishable from a truncated body. Read one extra byte and flag/reject oversized or unparsable responses, mirroring the overflow detection already used in doJSON.

- [x] **OCR-084** `internal/plans/plans.go:34` _bug_
  `DefaultStages` is an exported package-level slice; callers already assign it directly (`plan.Stages = plans.DefaultStages`, `append(stages, plans.DefaultStages...)`). A caller that mutates a stage's `Status`/`Goal` on the returned slice silently corrupts the shared template for every other caller, and `Save`'s `plan.Stages == nil` check cannot repair it. Return a copy (e.g. keep an unexported template and expose `func DefaultStages() []Stage`) or document the slice as immutable.

- [x] **OCR-085** `internal/plans/plans.go:148` _bug_
  `bestTime` is not always a file mtime: once a plan with a non-zero `UpdatedAt` is chosen, `bestTime` holds that semantic timestamp. Comparing the raw mtime of the next entry against it (`info.ModTime().Before(bestTime)`) therefore skips files whose mtime is older than another plan's `UpdatedAt`, even when that file's own `UpdatedAt` is newer (exactly the bulk-copy/`cp -p` case the comment below anticipates). The result depends on directory iteration order, so `LatestErr` can return the wrong "latest" plan. Compute `candidate` first and compare against it instead of pre-filtering on mtime.

- [x] **OCR-086** `internal/plans/plans.go:182` _bug_
  `Latest` is a divergent copy of `LatestErr`: it ignores `plan.UpdatedAt` (using only the file mtime) and swallows every read/parse error via `continue`. For the same directory it can therefore return a different plan than `LatestErr` — contradicting the documented preference for the semantic timestamp — and gives callers no signal that the newest plan was corrupt or unreadable. Delegate to `LatestErr` so both helpers agree.

- [x] **OCR-087** `internal/quality/proc_unix.go:20` _bug_
  `killProcessGroup` sends SIGKILL to the negated PID without checking that the child is still alive. Its only caller is `cmd.Cancel` in `quality.go`, and `os/exec` invokes `Cancel` from its context-watcher goroutine, which can run *after* `Process.Wait()` has already reaped the child (the process is marked done, but `Wait` is still draining the stdout/stderr pipes that `WaitDelay = 10s` bounds). Once the child has been reaped, its PID — and hence the process-group ID — can be recycled to an unrelated process group, which would then receive SIGKILL. Probe liveness before the raw group kill: `os.Process.Signal` is internally synchronized and returns `os.ErrProcessDone` once `Wait` has reaped the child (do not test `cmd.ProcessState` here — `Wait` writes it concurrently with this goroutine, which would itself be a data race).

- [x] **OCR-088** `internal/quality/quality.go:130` _bug_
  `strings.Fields` is not quote-aware, but `GOFLAGS` is parsed by cmd/go with shell-like quoting (a value may legitimately contain quoted segments with spaces, e.g. `-gcflags='-m -m'` or `-ldflags="-X main.v='a b'"`). Splitting and rejoining with single spaces both drops the original quoting fidelity and produces tokens like `-gcflags='-m` / `-m'`.
  
  Worse, deleting every token with prefix `-buildvcs=` can strip the first half of a quoted value that happens to start with that text (`-gcflags='-buildvcs=x y'`), leaving an unbalanced quote; cmd/go then fails the whole GOFLAGS string with "invalid quoted string in GOFLAGS" and every check in the gate fails for a reason unrelated to the code under test.
  
  Use a quote-aware splitter (matching cmd/internal/quoted semantics) or preserve the raw inherited string rather than re-serialising tokens, and only drop a flag token whose name is exactly `-buildvcs`.

- [x] **OCR-089** `internal/quality/quality.go:296` _bug_
  `GitSHA` returns unvalidated content, unlike `validSHA` (used by `Path`) and `resolveGitSHA`/`validGitSHA` in internal/evaluation.
  
  - Detached HEAD: any value with `len(value) >= 40` is returned verbatim — no hex check, no length cap — so a junk/multi-line `HEAD` (e.g. `ref: refs/heads/other` padded, or arbitrary ≥40-char text) becomes the reported SHA.
  - Ref file: the file's contents are returned whenever non-empty, so a symbolic-ref file or corrupt ref yields a value that is not an object id at all (the built-in test only covers the happy path).
  
  `Run` puts this value in `Report.SHA` and persists it to `gate.json`; downstream evidence writers embed the SHA in filenames (e.g. `cmd/fly-bench/main.go`: `fmt.Sprintf("%s-%s.json", rep.SHA, ...)`), so non-SHA content — including path separators from a crafted `.git/HEAD` — can steer those paths outside the intended evidence directory. Validate before returning (and consider a containment check after joining the ref path, as the evaluation package does).

- [x] **OCR-090** `internal/resources/matrix.go:171` _bug_
  When a capability has no variants, `Plan` appends a `Decision` whose `Chosen` is the zero `Variant` (only `Capability`/`Degraded`/`Reason` are set). `DecisionSummary` then renders an empty id — e.g. `reasoning=` — which is indistinguishable from a malformed entry, and this string is surfaced directly to users via `internal/tools/resources.go`. Either skip this case or render an explicit placeholder so the undefined capability is visible.

- [x] **OCR-091** `internal/resources/probe.go:54` _bug_
  `Live` is set as soon as the transport returns any response, before the status code and body are validated. The non-200 path (`error: "http 401"`) and the body-read-failure path both return with `Live == true` while `Reachable == false` and `Error != ""`. The caller copies `probe.Live` verbatim into the agent-facing payload (internal/tools/resources.go:305), so consumers receive contradictory results such as `live: true, reachable: false, error: "http 500"`. Set `Live` only after a successful 200, or explicitly document that `Live` means "an HTTP response was received (even an error one)" so callers do not read it as a health signal.

- [x] **OCR-092** `internal/resources/resources.go:159` _bug_
  `Load` never validates `Version`, and `Save` only defaults it when it is zero while always re-serializing with the v1 `Profile` struct. A `configs/resources.json` written by a future (v2) schema is therefore silently rewound to the v1 shape (any v2-only fields are dropped by `Unmarshal`) but keeps `version: 2`, so the downgrade is invisible and the extra data is permanently lost. Reject an unsupported version in `Load`/`Save` so callers can migrate instead of clobbering.

- [x] **OCR-093** `internal/resources/resources.go:166` _bug_
  `Claimed["codex_pro"]` unconditionally wins over the `CodexPro` field, and this branch always inserts the key, so every profile ever persisted contains it. A caller that sets `profile.CodexPro = true` on a profile loaded from disk (whose `Claimed` already holds `codex_pro: false`) has the value silently reverted to `false` here, even though `Statuses` (which ORs the two) would have reported it as claimed. Keep a single source of truth (derive `CodexPro` from `Claimed`, or make `CodexPro` authoritative) to avoid the two representations diverging.

- [x] **OCR-094** `internal/routing/fly/hash_encoder.go:30` _performance_
  `Embed` accepts a context but never consults it. On a large batch (or a batch with many long texts) the FNV loop is unbounded CPU work that cannot observe cancellation or a deadline set by the caller, so a cancelled request still burns CPU until the whole batch finishes. Check the context periodically inside the loop.

- [x] **OCR-095** `internal/routing/fly/llm.go:29` _maintainability_
  The classifier error is collapsed into the same `nil, false` result as benign "no signal" cases (empty response, non-numeric/unknown categories, all-equal scores). A persistently failing LLM is therefore indistinguishable from a router with no classifier configured, so the failure is invisible to `Recommend` and to callers/reports. Other best-effort paths in this package log such failures (`log.Printf` in embed.go:248 and memory.go:132) — this one is inconsistent and drops the error without observability. Please at least log it (or return a distinguishable state) so operators can detect a broken/unreachable LLM.

- [x] **OCR-096** `internal/routing/fly/mb.go:347` _performance_
  The process-global `brainMu` is held across the entire read-modify-persist section, including `r.saveBrain`, which performs `MkdirAll`, `MarshalIndent`, a temp-file write, `Sync()` and `Rename()`. Because `brainMu` is shared by every Router in the process, a concurrent `Recommend` (`mbCategoryScores`) or `Dopamine()` call in *any* Router blocks for the full duration of this disk write. This directly contradicts `loadBrain`'s own comment that "a slow disk must not serialize every Router in the process" (and `Dopamine()` -> `loadBrainLocked` even does `os.ReadFile` under the same lock on the cold path). Consider separating the write/persist serialization from the read lock (e.g. an RWMutex where readers take RLock and writers hold the write lock only for clone+swap, plus a dedicated mutex serializing the save), so scoring paths are not gated on I/O.

- [x] **OCR-097** `internal/routing/fly/mb.go:356` _bug_
  `adjust` silently returns when the category is missing from `Readout` or its slice is shorter than the activation code. `applyDopamine` calls it for categories derived from `categoriesForCapabilities(ev.Chosen)`, which are canonical categories (e.g. "timeseries") that need not exist in a brain trained on an arbitrary dataset. In that case no weight is changed, yet `work.Dopamine.PAM++/PPL1++` still runs, the brain is persisted, and `applyDopamine` returns `true` — so callers such as `AddFeedback` report `learned: true` when nothing was actually learned, masking a misconfigured/untrained brain. Have `adjust` report whether it applied an update and only count/return success when at least one category was updated (or log/skip unknown categories explicitly).

- [x] **OCR-098** `internal/routing/fly/mb.go:432` _bug_
  `SaveTo` overwrites the brain artifact with a plain `os.WriteFile`. This is the same non-atomic write that `saveBrain` already moved away from (temp file + `Sync` + `Rename`). This method is used by `cmd/fly-train` to write the production `data/index/fly-brain.json`; a crash/full disk mid-write leaves truncated JSON, and `loadBrain` silently returns `nil` on `json.Unmarshal` failure, so all MB signal disappears with no error until a full retrain. Reuse the atomic temp-file + rename pattern here.

- [x] **OCR-099** `internal/routing/fly/memory.go:131` _bug_
  `loadMemory` mutates the filesystem on a JSON parse failure (renames the file to `*.corrupt`) and then returns an empty memory with a nil error. This means the read-only accessor `MemorySize` can rename files as a side effect, and callers cannot distinguish "no data" from "data was corrupt and lost". Worse, if the rename fails (read-only FS, permissions) the failure is only logged and the corrupt file is left in place, so the next `AddFeedback`/`saveMemory` silently overwrites the recoverable data. Confine quarantine to the write path and/or surface an error instead of returning nil

- [x] **OCR-100** `internal/routing/fly/memory.go:211` _bug_
  Inconsistent model-mismatch guard: `memoryCategoryScores` only treats stored vectors as stale when `r.modelTag != ""`, but here an unset current model tag (embedder configured without a model tag, common when the model name is not supplied) makes `mem.Model != r.modelTag` true and silently deletes every stored vector, then rewrites `mem.Model` to the empty string. Historical semantic memory is destroyed with no error. Align the condition with the scoring path so an empty current tag never prunes.

- [x] **OCR-101** `internal/skills/validate.go:82` _bug_
  These two patterns capture the raw text between the delimiters, not a clean path. Markdown links with a fragment or title (`[rubric](references/rubric.md#criteria)`, `[rubric](references/rubric.md "Rubric")`) and backticked spans with anchors/trailing punctuation (`` `references/tools.md#fly` ``) are captured verbatim, so the subsequent `filepath.Clean`/`os.Stat` reports a false "referenced file does not exist". Strip the fragment (`#...`) and any title/whitespace suffix before validating.

- [x] **OCR-102** `internal/skills/validate.go:125` _maintainability_
  Every `os.ReadFile` failure is reported as "missing SKILL.md", including permission errors, I/O errors and symlink resolution failures. That hides the real cause from whoever consumes the report and sends them chasing a non-existent problem. Distinguish not-exist from other errors.

- [x] **OCR-103** `internal/skills/validate.go:203` _bug_
  The explicit-only detector requires the literal substring `explicit-only:` (colon), but several skills phrase the marker with a semicolon (`Explicit-only; never invoke implicitly.`) — e.g. roast-project, cp-expert-refactoring, self-improvement-controller, system-design-expert. For those skills this branch never fires, so a missing/true `policy.allow_implicit_invocation` is never reported and the standard silently goes unenforced. Accept both separators instead of a fixed colon.

- [x] **OCR-104** `internal/storage/store.go:20` _performance_
  Backend calls ignore caller cancellation. `WriteArtifact`/`ReadArtifact` are invoked synchronously from request handlers (e.g. `internal/tools/documents.go`), but the metadata record and object-store put/get run on a fresh `context.Background()` with a hardcoded 30s timeout, and `WriteArtifact` may issue two such calls sequentially. A stalled Postgres/MinIO backend therefore blocks the handler for up to 30s (60s) while ignoring any caller deadline/cancellation. Consider threading the caller's `context.Context` through these methods (or at least exposing the timeout as configuration) so request cancellation aborts the backend call.

- [x] **OCR-105** `internal/storage/store.go:154` _security_
  TOCTOU on the symlink guard: the `Lstat` check, `ensureWithin`, and `os.WriteFile` are three separate syscalls, so a symlink created at the final path component between the `Lstat` and the `WriteFile` is followed and the data is written to the link target (outside the root). The same race applies to the rehydrate path in `ReadArtifact`, which also routes through `safeWriteFile`. Since the function's stated contract is "refuses to follow a symlink at the final path component", consider making the write atomic w.r.t. the check: create the file with `os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)` (fails if something already exists, including a symlink) or write to a temp file in the verified parent directory and `os.Rename` it into place.

- [x] **OCR-106** `internal/telegram/telegram.go:106` _security_
  `WithHTTPClient` silently discards the security-relevant `CheckRedirect` guard installed by `New`. The bot token is part of the request URL, so a custom client that follows redirects (any plain `&http.Client{}`/`http.DefaultClient`) will re-send `https://<BaseURL>/bot<token>/<method>` to the redirect target, leaking the token cross-host. Currently only tests use this setter, but it is exported API: either preserve the guard (wrap the supplied client's `CheckRedirect` to reject redirects) or document loudly that the caller must keep redirects disabled.

- [x] **OCR-107** `internal/telegram/telegram.go:556` _bug_
  The `budget < 1` clamp breaks the hard "chunk never exceeds limit" guarantee. When the tags already open at the chunk boundary need more than `limit` runes to re-open plus close (`len(prefix) + closersBudget >= limit`), the code forces `budget = 1` and the shrink loop below exits immediately because `cut == pos+1`, so an oversized chunk is emitted. Telegram then rejects it with 400 instead of the message being sent. This is reachable with deeply nested/never-closed tags and small limits (the file's own tests call `ChunkHTML(text, 100)`; ~14 simultaneously open tags is enough). Consider flushing the pending chunk (closing all open tags, resetting `stack`) and starting the next chunk without a prefix when the budget would be non-positive, rather than emitting a chunk that cannot fit.

- [x] **OCR-108** `internal/telegram/telegram.go:575` _bug_
  A chunk boundary can fall inside an HTML entity, corrupting the text. `cutOutsideTag` only protects `<`/`>`, so an entity such as `&lt;`, `&amp;` or `&#39;` that straddles the boundary is split into two chunks (e.g. `...&am` + `p;...`). Telegram then renders the literal, broken text (`&am` is not a recognised entity) instead of the intended character, and `scanTags` does not detect it either. In `HTML` mode entities are common (that is exactly how `<`/`&` must be escaped), so this produces silently garbled messages. Back the cut off to the start of an unterminated entity (last `&` in the window that is not followed by `;`) in addition to `cutOutsideTag`.

- [x] **OCR-109** `internal/tools/context.go:213` _bug_
  File-access failures are silently dropped from the context-budget report in internal/tools/context.go, so total_tokens/by_kind are under-reported with no flag. The manifest loop continues on any os.Stat/os.ReadFile error (files that exist but are unreadable simply vanish), and the WalkDir callback returns nil when its err argument is non-nil (directory-read failures vanish the same way). Emit a flagged item for these failures as skillContextItems does for its own, so the report stays trustworthy.

- [x] **OCR-110** `internal/tools/data.go:221` _bug_
  Two safety guards currently fail open when a prerequisite syscall errors: the table size guard skips the limit entirely if `os.Stat` fails (`serr == nil`), and the repository-root containment/symlink check is skipped whenever `filepath.EvalSymlinks(root)` fails (nonexistent/permission/transient errors), so a path escaping the root may be accepted and returned/open. The size guard is also TOCTOU-prone and checks `ctx.Err()` only after the whole file is decoded. Fail closed on these errors, enforce limits while streaming, and compare normalized forms consistently.

- [x] **OCR-111** `internal/tools/data.go:410` _performance_
  Correlation analysis is O(numericColumns^2 * rows): for every pair it rescans all `t.Rows` and reparses both cells. With up to 2,000,000 rows and up to 64+ columns (only a warning above 64) this can consume enormous CPU on a request path, with no cancellation check inside the loop. Consider parsing each numeric column once into a []float64 slice (single pass over rows), capping the number of columns considered for correlations, and checking `ctx.Err()` periodically.

- [x] **OCR-112** `internal/tools/data.go:412` _bug_
  `profileColumns` resolves each requested column via `columnIndex` and profiles that index, discarding the mapping. data.analyze then re-resolves the same names with `t.columnIndex(numeric[i])`. Because `numeric[i]` is the trimmed header text (p.Name) of an already-resolved index, a second lookup can return a different column (e.g. two headers that are equal after trimming, or a name that is now ambiguous), so correlations may be computed over different data than was profiled, or silently skipped. Return the resolved indices from profileColumns (or a name->index map) and reuse them here instead of re-resolving.

- [x] **OCR-113** `internal/tools/documents.go:142` _bug_
  Both paths silently discard rows without a machine-readable warning: `tableAsText` drops every row beyond 500 per worksheet (the omission is only inline in the text), so neither the tool result nor `.meta.json` records the truncation; `loadSeries` drops rows whose time/value cell cannot be parsed, while only `loadTable` warnings are propagated. Return the omitted/skipped count and append a warning so callers can detect the data loss.

- [x] **OCR-114** `internal/tools/extras.go:387` _bug_
  Both chart paths conflate `series[0]` with the set of rendered series: X-axis labels are always taken from `series[0]` even when it is shorter than the longest series (misplacing/compressing labels), and for bar charts only `series[0]` is drawn while the legend (and point count) still includes every series. Base labeling on the longest/rendered series and restrict legend/point reporting to series actually drawn.

- [x] **OCR-115** `internal/tools/files.go:80` _performance_
  `readXLSXSheets` fully decodes/materialises every worksheet into `out`; the per-sheet caps (`maxXLSXCells`, `maxXLSXSheetBytes`) provide no aggregate budget across sheets, so a crafted multi-sheet workbook can drive very large memory/CPU on the request path. Track a global cells/rows/bytes budget across the loop and abort once exceeded, and/or add a names-only enumeration so callers that only need sheet names plus the first-sheet table do not parse/retain every sheet.

- [x] **OCR-116** `internal/tools/knowledge.go:43` _bug_
  `tokenize` treats every contiguous letter/digit run as a single token, which only works for whitespace-delimited scripts. For scripts without word spacing (CJK, Thai, ...) a whole chunk collapses into one giant token that will never match a query token, so `knowledge.search` silently returns zero hits. The `>= 2` rune minimum also drops legitimate single-character CJK words. If non-space-delimited languages are in scope, add a fallback that emits per-character (or n-gram) tokens when a run exceeds some length, or document the limitation.

- [x] **OCR-117** `internal/tools/knowledge.go:231` _performance_
  Every search walks the whole index directory, reads and `json.Unmarshal`s every index file, and keeps all chunks (full text + term maps) resident. Neither the index count nor the per-index size is bounded, and each `knowledge.index` call can persist an arbitrarily large document (chunk count times chunk size is unbounded). As the store grows, a single `knowledge.search` becomes an unbounded CPU/memory operation on the request path. Consider bounding the number of indexes scanned (or requiring an explicit `index_id`), streaming instead of materializing all docs, and/or persisting only the term statistics needed for scoring rather than full chunk text.

- [x] **OCR-118** `internal/tools/knowledge.go:290` _bug_
  The returned `text`/`snippet` are just the *prefix* of the chunk, not a window centered on the match. With a default chunk of 800 chars (max 20000) and `text_chars` default 300, a term matched at offset >300 produces a hit whose returned text does not contain the matched term at all — the caller cannot verify the grounding, which breaks the tool's stated "grounded chunks" contract. Consider locating the first matched term in `chunk.Text` and returning a window around it. Additionally, `Snippet` hardcodes 300 and ignores `text_chars`, so with `text_chars < 300` the `snippet` field is longer than `text` — the two fields should derive from the same limit.

- [x] **OCR-119** `internal/tools/learning.go:39` _bug_
  Both paths silently discard an error/diagnostic that callers rely on: `learning.record` ignores `RecordResult.EmbeddingError` (set by the store specifically so the caller can report degraded semantic recall), and the fly routing/feedback handlers ignore `_ =` JSONL append and `RecordFlyPrediction` errors, so telemetry/routing events can vanish with no diagnostic. Surface the failure as a warning/partial status, or at least log/count it, instead of converting it into an unqualified success.

- [x] **OCR-120** `internal/tools/plan.go:214` _bug_
  Slug validation around the reserved `plan` fallback is inconsistent: `plan.status` skips validation entirely, so a bogus slug like `"!!!"` or `"日本語"` silently resolves to `evals/plans/plan.json` and returns another plan's data with `ok`; meanwhile `validPlanSlug` rejects every raw slug that normalizes to the fallback, including inputs that do contain letters/digits (`"plan!"`, `"plan.json"`, `"日本語"`), with a misleading 'must contain at least one letter or digit' message. Centralize validation so invalid slugs are rejected accurately and the fallback cannot be reached accidentally.

- [x] **OCR-121** `internal/tools/quality.go:38` _bug_
  Both `quality.LatestReport` and `plans.LatestErr` can return a readable latest item with `ok == true` plus a non-nil diagnostic error caused by a different/unreadable sibling file. The handlers mishandle this: `quality.status` only reports the error in the `!ok` branch, silently presenting possibly stale/partial evidence; `plan.status` returns `{"plan": nil}` whenever `latestErr != nil`, discarding a perfectly readable latest plan. Handle the ok+err case explicitly in both: return the data with a warning, rather than hiding the diagnostic or throwing away the result.

- [x] **OCR-122** `internal/tools/resources.go:141` _bug_
  In live mode `reachable < configured` marks the whole probe as incomplete, but presence-only providers (postgres, minio, codex_pro) never set `reachable` — `probeProvider`'s default branch returns `reachable=false` with only a "presence-only provider; no live probe" note. So a fully configured Postgres/MinIO (or a claimed codex_pro) makes this condition true and the tool returns `Partial("some providers are not configured or not reachable")`, contradicting the provider's own `configured=true` result. The reachability check should only compare providers that actually support a live probe.

- [x] **OCR-123** `internal/tools/security.go:134` _security_
  Raw store/OS errors are forwarded to the MCP client from both the artifact and input-path branches, leaking absolute server paths (e.g. `ReadArtifact`'s `*PathError`, the remote fallback wrapping the local path, `InputPath`/`ensureWithin`, and `os.Open` errors). Wrap them consistently in generic messages such as `"artifact %s is not readable"` / `"input %s is not readable"`, as the sibling branches already do.

- [x] **OCR-124** `internal/tools/system.go:133` _performance_
  `Recommend` already embeds the task internally (for the embedding/knn/mb/llm scorers), and `ActivationSnapshot` embeds the *same* task again via `r.embedTask(ctx, task)` (see internal/routing/fly/events.go:35). With the OpenAI-backed embedder this means a second network round-trip on every `routing.fly_recommend` call whenever a store is configured, roughly doubling the latency/cost of the request path. Consider reusing the vector computed during `Recommend` (e.g. let the router expose the snapshot from the same embedding pass), or make the KC snapshot opt-in (an argument / separate tool) instead of always re-embedding.

- [x] **OCR-125** `internal/tools/telegram.go:48` _bug_
  `result.Chunks` is not a reliable "something was delivered" signal. In `telegram.SendMessage` the result is initialized as `SendResult{ChatID: chatID, Chunks: len(chunks)}` *before* the send loop (internal/telegram/telegram.go:240), so for any non-empty text `Chunks` is >= 1 on every error that occurs inside the loop — including a failure of the very first chunk where nothing was delivered. As a result this branch fires for essentially all post-validation send failures, returning a `Partial` response whose hint says "some chunks may already be delivered; verify before retrying" and whose `chunks` field reports the *total* chunk count instead of the delivered count, which can mislead callers into believing a message may have been sent (or into not retrying). Use `result.MessageID != 0` as the delivery signal (it is only set after a successful chunk), or make the client track delivered chunks separately.

- [x] **OCR-126** `internal/tools/timeseries.go:147` _bug_
  Timestamps for the returned series (and the `start`/`end` fields below, plus the anomaly timestamps in `timeseries.detect_anomalies`) are formatted with `time.RFC3339`, which has no fractional-second component. `parseTimeValue` accepts nanosecond/microsecond epochs, so sub-second data is reachable; multiple distinct observations then collapse to the same string, producing visually duplicated/misleading points and making the descending string sort of anomalies ambiguous for ties. Use `time.RFC3339Nano` (or emit the raw value) to preserve the resolution.

- [x] **OCR-127** `internal/tools/util.go:51` _bug_
  `commaIsThousands` counts a leading sign as part of the first group, so a 3-digit integer part with a sign fails the check: `parseNumber("-123,456")` splits into `["-123","456"]`, sees `len(parts[0]) == 4 > 3`, returns false, and the comma is rewritten as a decimal point, yielding `-123.456` instead of `-123456` (same for `"+123,456"`). Strip the sign before the group-length check so signed thousands values are parsed consistently with their unsigned counterparts.

- [x] **OCR-128** `internal/tools/xlsx.go:69` _bug_
  Both recovery paths in xlsx.go hide a parse failure rather than marking the sheet degraded: a `readSharedStrings` failure is only a warning, so every `t="s"` cell keeps its raw shared-string index as the value; and `f.Open()` failures for `xl/_rels/workbook.xml.rels`, `xl/workbook.xml`, and styles are swallowed, so sheet names fall back to fabricated `SheetN` values. Record a warning and/or blank/flag the affected cells instead of returning plausible-but-wrong data.

- [x] **OCR-129** `plugins/hackathonkit/hooks/codex-hooks.json:6` _maintainability_
  `id` (and the sibling `description`) are not part of the hook schema: a matcher group accepts only `matcher` + `hooks`, and a command hook accepts only `type` + `command` + `timeout`. Depending on how strictly the runner validates the manifest, these extra keys are either silently dropped (so `session:start` / `session:tool` / `session:stop` are dead configuration that misleads maintainers into thinking hooks are addressable by id) or cause the whole file to be rejected, which would disable all three lifecycle hooks at once. Either drop the `id` fields (the `description` strings are redundant with the top-level description) or confirm the runner's schema explicitly documents them.

- [x] **OCR-130** `plugins/hackathonkit/hooks/codex-hooks.json:12` _bug_
  `timeout` is in seconds here, but the internal timeouts of the invoked script are larger: `binaryContext()` in `scripts/hooks.js` allows 15000 ms for the local binary and 30000 ms for the `docker compose ... --session-context` fallback. With a 10 s cap the harness kills `node hooks.js session-start` before its own best-effort logic can finish, so on a machine that falls back to docker (cold image pull/build — the repo's own `.codex/config.toml` budgets 180 s for that path) context injection silently never happens. Align the manifest bound with the script (raise it, or lower the script's internal timeouts to fit inside 10 s) so the documented behaviour is actually reachable.

- [x] **OCR-131** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:6` _security_
  When `command -v` fails, the literal (slash-less) name is kept. `dirname` then returns `.` and `REPO_ROOT` becomes `$(pwd)/../../..`, i.e. the repository root is silently derived from the caller's current directory. Because the local branch `exec`s whatever it finds at the derived root, running the launcher from an untrusted directory can execute an unrelated/attacker-placed `bin/hackathonkit-mcp` (the arbitrary-execution vector H31 was raised for). Prefer a hard failure with a clear message when the launcher location cannot be resolved.

- [x] **OCR-132** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:24` _bug_
  A present-but-not-executable `bin/hackathonkit-mcp` is treated as fatal, so the docker fallback a few lines below is never reached even when a working Docker engine is available. This is a realistic install state (artifacts extracted from a zip/tar that lost the execute bit, a Windows checkout consumed through WSL/`/mnt/c`, or a `cp` without `-p`), and the documented policy is "prefer the local binary, otherwise fall back to docker compose", so aborting contradicts it and leaves a recoverable install dead. Warn and fall through; only fail once Docker has also been ruled out.

- [x] **OCR-133** `plugins/hackathonkit/scripts/hooks.js:222` _bug_
  The session file is written only at `session-start`, so its mtime/`updated_at` never advance while the session is active. A legitimate long-running session (> 12h, or started before a long gap) is therefore treated as expired by `isExpired()` and pruned/removed, after which its `stop` event is rejected as "unknown or foreign session id" — the session is silently leaked and its end never logged. Refresh the session file on each `tool-use` (touch `updated_at`/mtime) so the TTL reflects last activity rather than creation time.

- [x] **OCR-134** `plugins/hackathonkit/scripts/hooks.js:386` _bug_
  `tool-use` resolves the session id via `lastActiveID()` without any ownership check, unlike the `stop` path which guards with `ownedByRoot(id)`. With two concurrent Codex sessions in the same repo, an event whose payload has no session id is attributed to whichever session file has the newest mtime, so events can be logged against the wrong session (and, if a foreign file ever lands in `sessions/`, against a foreign session). Validate the fallback before using it.

- [x] **OCR-135** `scripts/coverage-check.sh:25` _bug_
  The EXIT/INT/TERM traps only remove `$PROFILE`, but `$TEST_LOG` and `$COVER_LOG` are created later and are cleaned up only on the explicit success/failure paths. If the script is interrupted (INT/TERM) while `go test` or `go tool cover -func` is running — the longest phases — those temp files are leaked. Since the INT/TERM handlers now `exit` immediately, the EXIT trap runs but still only knows about `$PROFILE`. Register all temp files with the cleanup function (declare them empty up front so the trap can safely run before they are assigned) so every exit path removes them.

- [x] **OCR-136** `scripts/docker-check.sh:55` _security_
  The key/value redaction only matches an unquoted key directly followed by `[=: ]+`. Structured/JSON log lines quote the key, e.g. `"password":"secret"` or `"access_key": "..."`, so no match occurs (the separator class cannot cross the closing `"`) and the secret is dumped verbatim into diagnostics — the codebase logs structured entries via `observability.Log`, so this is a realistic output shape. Allow optional surrounding quotes on the key (and/or include `"` in the separator class) so quoted keys are covered too; keep the existing fail-closed behavior.

- [x] **OCR-137** `scripts/docker-check.sh:69` _bug_
  `diagnostics` tears the infrastructure down unconditionally, ignoring `--keep-infra`. The flag's contract (and the header comment) is to leave postgres/redis/minio running, but the EXIT trap calls `stop_infra` on every non-zero exit — i.e. exactly when the user most wants to inspect the stack. As written, `--keep-infra` cannot be used to debug a failing run. Gate the teardown on `KEEP_INFRA`, or document that failures always clean up.

- [x] **OCR-138** `scripts/docker-check.sh:121` _bug_
  `HACKATHONKIT_MINIO_PORT` is neither sourced from `.env` nor referenced by `docker-compose.yml` (the repo's only occurrence of this name is this line; compose publishes minio as the hardcoded `127.0.0.1:9000:9000`). The probe therefore reads a process-environment variable that has no influence on the actual published port: if a caller ever exports it (e.g. to a remapped port) the host probe hits a port nothing is listening on and the run aborts with "minio did not become ready" even though the stack is healthy; if it is set only in `.env` it is silently ignored. Derive the port from the real published mapping (or probe from inside the compose network) instead of an unrelated environment variable.

- [x] **OCR-139** `scripts/doctor.ps1:10` _performance_
  On timeout the pending `BeginConnect` is never cancelled/observed, and the `finally` block closes `$async.AsyncWaitHandle` while the connect may still be completing — closing the wait handle of an in-flight async operation can raise `ObjectDisposedException` on the completion thread (and `EndConnect` is never called, leaving the operation unobserved). Abort the socket before returning and avoid manually closing the shared `AsyncWaitHandle`.

- [x] **OCR-140** `scripts/doctor.ps1:80` _bug_
  Both the `doctor.ps1` skills-mirror check and the `install-prompts.ps1` `status` check compare only relative file names/existence (Compare-Object over path lists; Test-Path), so a file present in both trees but with different content is treated as up to date and content drift is never detected (Compare-Object is also case-insensitive by default). Compare content (e.g. relative path + hash) rather than names alone, and report orphaned/stale files present in the destination but absent from the source.

- [x] **OCR-141** `scripts/doctor.sh:65` _bug_
  Two problems in the awk fallback: (1) `found` is never assigned, so `if (found) print "enabled"` is dead code and can never fire; (2) the matching is pure substring/regex on brace-split chunks (`parts[i] ~ /"name"[ \t]*:[ \t]*"hackathonkit"/` and `"enabled"[ \t]*:[ \t]*false`), so a nested object whose name/description contains `"name": "hackathonkit"`, or an `enabled:false` belonging to a different sub-object inside the same chunk, will misclassify the plugin and produce a wrong pass/fail. Prefer a real JSON parser (or at minimum match whole objects keyed on `"name":"hackathonkit"` with quoting) and remove the dead `found` guard.

- [x] **OCR-142** `scripts/doctor.sh:121` _bug_
  The mirror check mixes two inconsistent listings: `find -mindepth 1 -maxdepth 1 -type d` counts hidden directories but ignores symlinked directories, while `ls -1d */` does the opposite (skips dot-dirs, follows symlinks to dirs). `SRC_COUNT`/`DST_SKILLS` are therefore not comparable with `SRC_NAMES`/`DST_NAMES`, so a hidden or symlinked directory in either tree yields a false pass or false failure. Also, comparing only aggregate `SKILL.md` counts plus top-level names cannot detect a nested layout that differs between plugin and fallback, and names containing whitespace/newlines break the `ls | sort` comparison. Use one consistent, NUL-safe listing for both trees.

- [x] **OCR-143** `scripts/e2e-fly.ps1:22` _maintainability_
  Assigning to `$env` shadows/misreads PowerShell's `env:` provider (`$env:NAME`) and is easy to confuse with environment-variable access. Rename to something explicit such as `$result` (and update the `return` statement accordingly).

- [x] **OCR-144** `scripts/e2e-fly.ps1:29` _test_
  HTTP checks assume a server is already listening at the hardcoded `http://127.0.0.1:8787`. Without a startup/readiness check (or a configurable base URL), these checks are environment-dependent and flaky, and every request incurs a 20s timeout when nothing is listening. Consider accepting the base URL as a parameter and probing `/` (or a health endpoint) once before running the page/module assertions.

- [x] **OCR-145** `scripts/e2e-fly.ps1:46` _bug_
  The script persistently mutates the global routing mode to `fly` (and later back to `normal`) but provides no `try/finally`. Any terminating error or early `exit` between these points leaves the process/repo state stuck in `fly` mode, affecting subsequent runs. Wrap the fly-mode section (or the whole test body) in a `try { ... } finally { & $exe --set-routing-mode normal --repo-root $Root | Out-Null }` so the original mode is always restored.

- [x] **OCR-146** `scripts/env-set.sh:47` _bug_
  Symlink resolution is single-hop and racy. If `$FILE` points to another symlink (a link chain), `FILE` becomes the intermediate link path; the temp file is created next to that intermediate link and `mv "$TMP" "$FILE"` replaces the intermediate symlink with a regular file instead of writing through to the final target, silently breaking the chain and updating the wrong file. If `readlink` fails/returns empty (e.g. the file is replaced concurrently, or is a symlink to a symlink that readlink cannot fully resolve), `FILE` keeps the original symlink path and the final `mv` replaces the symlink itself. Resolve symlinks iteratively (with a hop limit) or use `readlink -f`/`realpath` when available, and refuse to proceed when resolution fails.

- [x] **OCR-147** `scripts/env-set.sh:64` _bug_
  Mode/owner restoration is best-effort and the failure path still reports overall success. If `chmod "$MODE"` fails, the file keeps the temp file's 0600 mode; if the original was group-readable (e.g. 0640) consumers break, and the promised 'permissions preserved' guarantee is silently violated. There is also a TOCTOU window: the mode/owner are read before the rewrite, so a concurrent chmod/chown between `stat` and `mv` is lost. Consider failing hard (non-zero exit) when mode/owner cannot be restored, or applying the mode to the temp file before the rename.

- [x] **OCR-148** `scripts/env-set.test.sh:7` _bug_
  The INT/TERM trap only removes the temp dir and does not exit. In POSIX sh a trapped INT/TERM runs the handler and then resumes the script, so Ctrl-C or a CI timeout would clean up `$TMP` but keep running the remaining assertions, and the already-fired trap state means the run no longer aborts as intended. Note the tested helper gets this right (`on_signal_env_set` calls `exit 130`); the test suite should mirror it. Split the EXIT and signal traps and exit from the signal handler.

- [x] **OCR-149** `scripts/env-set.test.sh:92` _security_
  The injection test uses a fixed, world-writable marker path (`/tmp/hackathonkit-env-inject-marker`). Any other process/user on the host (or a parallel run of this suite) can pre-create that file, turning the `[ -f ... ]` assertion into a spurious failure; the shared path is also a symlink/pre-creation hazard even though it lives in the sandbox. Since a `$TMP` scratch dir already exists, use a marker inside it. Additionally, placing `rm -f` between the helper call and the sourcing check means a payload executed during the helper's *write* path would be silently masked — clear the marker before invoking the helper if you want to cover both phases.

- [x] **OCR-150** `scripts/gen-mcp-manifest.ps1:19` _bug_
  A caller-supplied `$Root` is accepted verbatim (only the default branch resolves an absolute path in `gen-mcp-manifest.ps1`), and `write-install-state.ps1` validates only a weak marker without canonicalising. A relative value (`-Root .`, `./foo`) or a path containing `..`/symlinks passes `Test-Path` and is spliced into the manifest/state, redirecting the write outside the intended repository. Canonicalize `$Root` to an absolute real path after validation (and strengthen the repository marker check) in both scripts.

- [x] **OCR-151** `scripts/gen-mcp-manifest.ps1:43` _maintainability_
  The post-render guard only knows the two placeholders that exist today, so a template that introduces a new token (e.g. `{{SOMETHING_ELSE}}`) is not detected and ships an unrendered manifest (the JSON check cannot catch it, it is a valid string). Match placeholders generically.

- [x] **OCR-152** `scripts/gen-mcp-manifest.ps1:52` _bug_
  `Test-Path` followed by `File.Replace`/`File.Move` is a check-then-act race: another process can create the destination before the `Move` (throws, destination exists) or remove it before the `Replace` (throws, destination missing). The whole update is then lost even though the previous manifest is gone. Re-check and retry once instead of relying on a single probe.

- [x] **OCR-153** `scripts/gen-mcp-manifest.sh:4` _bug_
  Root derivation/validation issues: (1) when the script is invoked via PATH with a slashless `$0` (e.g. `gen-mcp-manifest.sh`), `dirname "$0"` returns `.`, so the default resolves to the parent of the *current working directory* rather than the directory containing the script; (2) a caller-supplied `$1` is used verbatim, so a relative path passes the `[ ! -d ]` check and is written into the manifest as a relative path, breaking the launcher that expects an absolute root; (3) `[ -z "$ROOT" ]` is effectively dead — `${1:-...}` already substitutes the default for an empty `$1`, and with `set -e` a failing `cd` in the command substitution terminates the script before the diagnostic on the next line can run.

- [x] **OCR-154** `scripts/gen-mcp-manifest.sh:21` _bug_
  The escaping pipeline does not do what it says. `tr '\r\n\t\f\v' '    '` maps TAB to a space (the replacement set has 4 spaces, so the 5th and following input chars are padded with the last char = space), which means the trailing `sed -e 's/<TAB>/\\t/g'` can never match — it is dead code and never emits the `\t` escape implied by the comment above. Worse, any other C0 control byte (e.g. `\b`, or bytes < 0x20 not in the five listed) is passed through unescaped and produces invalid JSON, silently corrupting the manifest on systems where the optional python3 validation is missing. Escape/reject every control character, or at minimum drop the unreachable stage.

- [x] **OCR-155** `scripts/gen-mcp-manifest.sh:54` _bug_
  JSON validation is silently skipped when python3 is unavailable, so on such systems only the placeholder grep guards the output and a malformed rendered manifest (e.g. produced by the escaping gap above) is installed as `.mcp.json`, breaking every MCP client that reads it. Consider warning loudly when no validator is present, or providing a portable fallback so invalid output is never published.

- [x] **OCR-156** `scripts/install-prompts.ps1:20` _bug_
  `$env:CODEX_HOME` is consumed verbatim and `$HOME` is assumed non-null. On environments where `$HOME` is unset (e.g. some service/containers or non-interactive Windows contexts), `Join-Path $HOME ".codex"` throws a null-binding error; because `$ErrorActionPreference = 'Stop'` this aborts the script with an opaque stack trace instead of a clear message. A relative or unexpected `CODEX_HOME` also silently redirects writes to an unintended tree. Validate both inputs and fail explicitly.

- [x] **OCR-157** `scripts/install-prompts.ps1:24` _bug_
  `$Sources` is never validated. If the directory exists but contains no `*.md` files (e.g. a sparse/filtered checkout), `install` prints `installed 0 prompt(s)` and `status` prints `installed: ...`, both exiting 0 and masking a broken installation. Additionally, `Get-ChildItem -Filter *.md` without `-File` can return directories whose name matches the filter, which `Copy-Item` would then recurse into. Filter to files and fail when nothing is found.

- [x] **OCR-158** `scripts/install-prompts.ps1:38` _bug_
  TOCTOU race in the remove loop: the destination is only checked with `Test-Path`, then hashed. If the file is deleted, replaced, or becomes locked between `Test-Path`, `Get-FileHash` and `Remove-Item`, `Get-FileHash`/`Remove-Item` raise a terminating error (due to `$ErrorActionPreference = 'Stop'`), aborting the loop mid-run and leaving a partially-removed, unclear state. Handle the race locally instead of letting it terminate the whole run.

- [x] **OCR-159** `scripts/install-prompts.ps1:39` _bug_
  Locally modified or outdated prompt files are skipped silently, yet the script still prints `removed unmodified prompts from ...` and exits 0. Users are told removal succeeded while stale/divergent files are left behind in the prompts directory. Emit a warning (and/or a skipped counter) for every file that is kept because it differs from the source.

- [x] **OCR-160** `scripts/install-prompts.sh:26` _bug_
  `install` overwrites any pre-existing file in `$DEST_DIR` unconditionally, so a rerun via `setup.sh`/`repair.sh` silently destroys local edits a user made to their Codex prompts (the `remove` branch deliberately protects such edits with `cmp`, so the two paths are inconsistent). Consider detecting a diverged destination and warning (or refusing) before clobbering it, so the destructive action is at least visible. Note also that if the destination name is unexpectedly a directory/symlink, `cp` will write *into* it or follow the link.

- [x] **OCR-161** `scripts/install-prompts.sh:45` _bug_
  `status` only checks that a file exists, never that it matches the source (even though the `remove` branch already shows `cmp -s` is the intended comparison). After the plugin updates a command, `status` still prints `installed` and exits 0, and `scripts/doctor.sh` will report the prompts as healthy while the installed copies are stale. Compare content here too (the wording/message should then say "out of date" rather than "missing").

- [x] **OCR-162** `scripts/plugin-check.ps1:57` _security_
  The containment guard is weaker than it claims: (1) `GetFullPath` only normalises the string, it does not resolve symlinks/junctions, so a link placed inside `$sourcePath` that points outside it still passes; (2) the comparison is `OrdinalIgnoreCase`, which on case-sensitive file systems (Linux/macOS PowerShell Core) lets `/opt/Hackathon Agent Kit/...` pass a `/opt/hackathonkit/` prefix and execute a binary outside the plugin; (3) only `command` is validated, the unbounded `$server.args` are passed straight through, so containment can be bypassed by arguments even when the binary path is legit. Resolve the real target path (e.g. `GetFinalPathNameByHandle`/`Resolve-Path -Resolve` plus re-check after resolving links) and use `Ordinal` on case-sensitive platforms; consider validating args too.

- [x] **OCR-163** `scripts/plugin-check.ps1:68` _bug_
  `New-Item` runs under `$ErrorActionPreference = "Continue"`, so if the temp directory cannot be created the failure is silent; the script then starts the job anyway, the job's `Set-Location`/`Set-Content` fail, no exit-code file is produced, and the check can be reported as a PASS (see the `$exitCode` default of 0). Assert the creation succeeded before continuing. Note the same `Continue` preference means other non-terminating cmdlet failures (e.g. `Get-Content`, `Set-Content`) don't abort, so state can silently be missing later.

- [x] **OCR-164** `scripts/plugin-check.ps1:124` _bug_
  The stream framing is fragile: any non-JSON line written to stdout (progress/log noise) is prepended to `$buffer` and, because the buffer is only cleared when the whole thing parses or exceeds 1 MB, every subsequent valid response becomes unparseable and the run reports a false "missing JSON-RPC response". Likewise two JSON values on a single line can never be parsed by a plain `ConvertFrom-Json`, and hitting the 1 MB cap silently discards buffered data. Consider framing strictly by line (`ConvertFrom-Json` per line, tolerating partial trailing JSON) and dropping non-JSON lines instead of accumulating them forever.

- [x] **OCR-165** `scripts/plugin-check.sh:143` _maintainability_
  `proc.returncode` is never checked, and the captured `stderr_tail` is only surfaced in the missing-ids branch. A server that emits all three responses and then exits non-zero (or reports `isError` on the tool call, or exits before writing anything) produces a failure/diagnostic without the server's stderr, making remote CI failures hard to triage. Verify `proc.returncode` (including negative/signal values) and append `stderr_tail` to the error/isError/exit-code failure messages, not just the missing-id message.

- [x] **OCR-166** `scripts/quality-gate.sh:23` _bug_
  Non-POSIX reliance on `test -nt` (and `find -quit` on line 18) under a `#!/usr/bin/env sh` shebang. On a strict `test` implementation `[ "$extra" -nt "$BIN" ]` is an unknown binary operator: it returns an error status, which is silently evaluated as "not newer", so an updated go.mod/go.sum would *not* force a rebuild and the gate would run evidence against a stale binary — exactly the failure this check exists to prevent. Using `find` for the comparison keeps the check portable (the script already depends on `find`).

- [x] **OCR-167** `scripts/quality-gate.sh:60` _bug_
  An executable `$BIN` exists but `go` is absent: USE_BIN was forced to 0 on line 33, so control never reaches the `exec` fast path and the script fails here with exit 2 and the message "no runtime available" — even though a runnable binary is present. The diagnostic is misleading and the prebuilt-binary workflow is unusable in toolchain-less environments (e.g. release tarballs/CI images without Go). If freshness cannot be verified, prefer running the existing binary with an explicit warning, or at least correct the message.

- [x] **OCR-168** `scripts/repair.ps1:33` _maintainability_
  `$profile` is a PowerShell automatic variable (`$PROFILE`, the path to the profile script). Assigning to it here silently shadows/overwrites that built-in variable inside the script scope, which is a classic PowerShell pitfall and confusing for maintainers. Rename to `$installProfile` (and update the `-Profile $profile` call and the reads below).

- [x] **OCR-169** `scripts/repair.sh:28` _bug_
  Two problems with this fallback: (1) on hosts where `timeout` is unavailable (e.g. macOS) `docker info` runs without any time limit and can hang the repair indefinitely; consider a portable bounded wait or skipping the probe when it cannot be bounded. (2) Daemon liveness alone does not prove the required images exist — with `RUNTIME=docker` the Go rebuild is skipped entirely, so a missing image still yields a successful repair and an unavailable MCP runtime. Verify the images (e.g. `docker image inspect`) or the actual runtime path before declaring success.

- [x] **OCR-170** `scripts/repair.sh:33` _bug_
  The whole decision (probe binaries → rebuild → move into `bin/`) is not protected against concurrent runs: two parallel `repair.sh` invocations can both take the build branch and race on `mktemp`/`mv` into `bin/`, and `write-install-state.sh` is invoked afterward against the same file, so state can be torn or conflict. Additionally, replacing binaries in place while an MCP/demo process is running causes version skew until it is restarted. Consider a lock (e.g. `mkdir "$ROOT/.repair.lock"` with cleanup, or `flock`) and note that running processes must be restarted.

- [x] **OCR-171** `scripts/repair.sh:60` _bug_
  Install state is persisted before the Codex plugin section, which can still `exit 1` afterwards. A failed run therefore leaves `configs/install-state.json` claiming a completed repair with the resolved profile/runtime. Move this call after the Codex verification (or make that section non-fatal and surface the failure at the end) so persisted state only reflects a successful repair.

- [x] **OCR-172** `scripts/setup.ps1:46` _security_
  The detection pattern only requires *any* non-empty value after `HACKATHONKIT_DEMO_TOKEN=`, so a placeholder shipped in `.env.example` (e.g. `changeme`/`your-token-here`) is treated as a real token and no strong token is generated. Since this value is what protects the demo API, validate against known placeholders (or regenerate whenever the value looks non-random) instead of accepting it blindly.

- [x] **OCR-173** `scripts/setup.ps1:60` _bug_
  Both `.env` rewriting in `setup.ps1` and `install-state.json` writing in `write-install-state.ps1` use `Set-Content` in place: it truncates the target first (an interrupted/failed write corrupts or destroys the existing file) and, on Windows PowerShell 5.1, uses ANSI or UTF8-with-BOM encoding (mangling existing UTF-8, and strict JSON parsers reject a leading BOM). Write with an explicit BOM-less encoding to a temp file in the same directory, atomically replace/move it into place, and verify the result.

- [x] **OCR-174** `scripts/setup.ps1:68` _bug_
  Windows-only separators are passed to `Join-Path` (here and for `install-prompts.ps1`, `gen-mcp-manifest.ps1`, `write-install-state.ps1`, as well as `.agents\skills` / `.claude\skills`). On PowerShell for Linux/macOS a backslash is not a path separator, so these paths will not resolve — yet the script explicitly checks `$env:OS -eq "Windows_NT"` above, implying cross-platform intent. Use forward slashes or per-segment `Join-Path`. Related: judging helper scripts purely by `$LASTEXITCODE` is unreliable if a helper is ever a shim/function that throws instead of calling `exit`.

- [x] **OCR-175** `scripts/setup.ps1:92` _bug_
  Recursive deletes that lack a reparse-point guard can follow junctions/symlinks (Windows PowerShell 5.1 `Remove-Item -Recurse` descends into directory links), destroying content outside the repository. `setup.ps1`'s skills cleanup guards only the skill directory itself (not links inside it), and `uninstall.ps1`'s generic `bin`/`data\control`/`plugins\hackathonkit\.mcp.json` branch has no guard at all (unlike its skills branch). Additionally, `-ErrorAction SilentlyContinue` masks permission-denied/locked failures while still printing the success message. Apply the reparse-point guard consistently and report failures.

- [x] **OCR-176** `scripts/setup.ps1:107` _bug_
  `docker info` only proves the daemon is reachable; it does not prove the `compose` plugin exists or that the version supports `--wait` (Compose v2.17+). A host with a working `docker` CLI but a missing/old compose will take the docker branch and hard-fail in `Fail` instead of falling back to the Go build. Probe `docker compose version` (and `--wait` support) before committing to `$runtime = "docker"`.

- [x] **OCR-177** `scripts/setup.ps1:145` _bug_
  When `marketplace add` fails the script still runs `codex plugin add hackathonkit@hackathonkit-local`, which is guaranteed to fail against an unregistered marketplace, leaving a half-configured plugin state that is only surfaced via the aggregated warning text. Skip the `plugin add` step (or fail) when the marketplace registration did not succeed.

- [x] **OCR-178** `scripts/setup.sh:55` _security_
  The stat-failure branch is a no-op security hole. When neither `stat -c` nor `stat -f` works, `ENV_MODE` is set to `600`, which then matches the `*00` glob in the `case` below, so the `chmod 600` remediation is **skipped** — the exact opposite of the emitted "forcing 600" message. Since `.env` holds `OPENAI_API_KEY`/`POSTGRES_PASSWORD`/`TELEGRAM_BOT_TOKEN`, an unreadable mode must never be treated as already-safe. Force the `chmod` in that branch instead of feeding a fake mode into the `case`.

- [x] **OCR-179** `scripts/setup.sh:113` _bug_
  On a partial Docker failure the script only prints `docker compose ps` and flips `RUNTIME` to `binary`, leaving whatever containers were already started running. Those containers keep binding the compose ports (postgres 5432, redis 6379, minio 9000), so the subsequent local-binary path can fail to start or silently talk to the half-started stack, and `write-install-state.sh` records `binary` while containers are in fact up — an inconsistent state that the next run cannot reason about. Tear the partial stack down (or at least stop it) before falling back, e.g. `docker compose -f "$ROOT/docker-compose.yml" down` (without `-v`, so data volumes are preserved).

- [x] **OCR-180** `scripts/smoke.ps1:13` _bug_
  The request payload is LF-joined and written with `-NoNewline`, so the final `tools/call` message has no terminating line break. A line-framed server that only dispatches a request once it sees a newline (or a server expecting CRLF) may never answer request id 3, causing a false failure. Append a trailing line terminator to the payload.

- [x] **OCR-181** `scripts/smoke.ps1:25` _bug_
  `Start-Process -ArgumentList` joins the array elements with spaces but does not add quoting, so a `$root` (derived from the script path) containing spaces yields truncated arguments. This affects both the local-binary invocation (`--repo-root $root`) and the docker invocation (`compose -f (Join-Path $root 'docker-compose.yml')`). Quote the path values (and ideally validate the path) before passing them.

- [x] **OCR-182** `scripts/smoke.ps1:127` _bug_
  `$ids` is populated in arrival order and compared verbatim against "1,2,3", but JSON-RPC does not guarantee responses are emitted in request order (and the id set, not its ordering, is what matters here). Additionally, duplicate ids silently overwrite `$byId`. Validate the id set instead of the sequence.

- [x] **OCR-183** `scripts/smoke.sh:30` _bug_
  `run_binary` captures stdout synchronously with no timeout. If the MCP server hangs (e.g. waiting on stdin/blocking init), CI will stall indefinitely. Wrap the invocation in a timeout so the smoke test fails fast.

- [x] **OCR-184** `scripts/smoke.sh:130` _bug_
  The fallback id parser only recognises unquoted non-negative integer ids, and `sort -u` merges duplicates. A response with a string id, or duplicate/misordered responses, can pass this check even though the handshake is invalid. Since this branch runs precisely when python3 is unavailable, the assertion should at least verify the raw response count and reject duplicate ids rather than de-duplicating them.

- [x] **OCR-185** `scripts/smoke.test.sh:30` _test_
  The fake repo has no `cmd/` or `internal/` directories, but smoke.sh's `--auto` freshness probe is `find "$ROOT/cmd" "$ROOT/internal" -name '*.go' -newer "$ROOT/bin/hackathonkit-mcp" 2>/dev/null | head -n 1`. With those paths missing, `find` fails (silently swallowed by `2>/dev/null`) and the pipeline still exits 0 because of `head`, so `NEWEST` is always empty and the "stale binary -> compose" branch is never exercised. The `--auto prefers the local binary` assertion therefore passes even if the freshness comparison were broken. Consider creating `$REPO/cmd`/`$REPO/internal` with a `.go` file newer than the stub binary and asserting that `--auto` then selects the docker stub.

- [x] **OCR-186** `scripts/smoke.test.sh:37` _maintainability_
  The heredoc is unquoted, so every `$`/backtick in the generated stub is expanded by this shell unless escaped. Only `$*`, `$(...)` and `$MARKER` are escaped today; the JSON payload happens to contain no `$`, so this works by accident. If the payload is ever edited to include `$` or a backtick (or if a helper variable is referenced), the stub is silently corrupted and the tests may pass for the wrong reason. Either quote the delimiter (`<<'EOF'`) and substitute the two variables explicitly, or add a comment warning that all `$` in the body must be escaped.

- [x] **OCR-187** `scripts/smoke.test.sh:63` _maintainability_
  These assertions are tightly coupled to smoke.sh's exact command line and to the hard-coded request count (`stdin=3`, and `run --rm -T hackathonkit-mcp` ordering). Any legitimate change to smoke.sh (extra flag, reordered args, an added/removed JSON-RPC request) makes several tests fail with a confusing message rather than a real regression. Prefer deriving the expected stdin count from smoke.sh's `REQUESTS` (e.g. `grep -c . ` on the heredoc block) and matching only the stable transport tokens instead of the full argument string.

- [x] **OCR-188** `scripts/smoke.test.sh:93` _test_
  Coverage gap: there is no case for `--auto` when *neither* the local binary nor the docker CLI is available, which is the third branch of smoke.sh's auto path (exit 1 with the "no MCP runtime available" diagnostic). Adding it would protect the most user-visible failure mode.

- [x] **OCR-189** `scripts/sync-harness.ps1:49` _bug_
  The `.claude/skills` mirror swap is not serialized. `sync-harness.sh` acquires a `CLAUDE_LOCK` (and installs interrupt traps) around exactly this swap, and the sibling `sync-skills.ps1` does the same for `.agents/skills`, but this script takes no lock. Two concurrent runs (e.g. CI plus a manual invocation) can interleave the move-aside/move-in sequence: one process can move the live mirror to its `.old.<pid>` while the other is mid-swap, so a `Move-Item` fails and the rollback/backup-delete logic in `finally` can run against a mirror that was concurrently replaced. Add the same PID-file lock used by `sync-skills.ps1`, or delegate to a shared helper.
  
  Separately, the header comment claims the mirror is swapped "atomically", but the live `.claude/skills` is moved aside before the temp directory is moved in, so a reader can observe the directory as missing between the two renames. The comment overstates the guarantee.

- [x] **OCR-190** `scripts/sync-harness.ps1:54` _bug_
  Both `sync-harness.ps1` and `sync-skills.ps1` verify the mirror too shallowly: one compares only the count of top-level directories, the other only their names, so a same-count-but-different set of skills, a renamed/added+removed skill, or a copy that drops/corrupts files inside a skill directory (or top-level files) passes verification and a partial mirror is reported as success. Compare the full relative file set (and ideally hashes/sizes) rather than counts or top-level names.

- [x] **OCR-191** `scripts/sync-harness.sh:47` _bug_
  The pid file write failure is silently ignored (`|| true`). An empty/missing `pid` makes `claude_lock_stale` fall back to the mtime heuristic, so any run longer than 30s (large skills tree, slow disk) is declared stale and its lock is stolen by a concurrent run — the exact race the lock exists to prevent. Fail hard if the pid cannot be written, or at least keep the mtime threshold above the realistic worst-case copy time.

- [x] **OCR-192** `scripts/sync-harness.sh:111` _bug_
  Unlike the skills swap above, the command mirrors are copied directly into the destination directories with no lock, no temp-dir staging and no rollback, and the lock has already been released (and traps cleared) at this point. Two concurrent runs can interleave `cp` and expose partially written files, and a failure mid-loop leaves some commands updated while others are stale. Files deleted from the plugin `commands/` source are also never pruned, so the mirrors drift. Stage into a temp dir and swap (or prune obsolete files) under the same lock.

- [x] **OCR-193** `scripts/sync-harness.sh:160` _bug_
  `json_valid` is invoked for every entry of `JSON_MANIFESTS` without checking existence, so a *missing* JSON manifest is counted in `MISSING` (by the ADAPTERS loop) and again in `INVALID` (here), producing misleading diagnostics ("1 missing, 1 invalid" for a single absent file). Skip files that do not exist so each failure mode is reported once.

- [x] **OCR-194** `scripts/sync-skills.ps1:70` _maintainability_
  The empty `catch {}` hides why the stale-lock eviction failed, and `continue` re-enters the loop immediately without any sleep, producing a tight busy loop for the remaining iterations (up to ~100 rapid retries). Log the failure reason (e.g. `Write-Verbose $_.Exception.Message`) and sleep before retrying so a transient Move/Remove failure does not spin the CPU and immediately exhaust the attempts.

- [x] **OCR-195** `scripts/sync-skills.sh:84` _bug_
  These handlers are installed only *after* the lock has been acquired (`LOCKED=1` above), and they replace the earlier `trap cleanup_tmp EXIT` / `on_signal`. A signal arriving in that window runs the old handler, which removes `$TMP` but leaves `$LOCK` behind — the permanent-lock situation the reclamation code is supposed to handle. Define a single cleanup routine that releases both `$TMP` and `$LOCK` (guarded by `LOCKED`) and install its traps once, before the acquisition loop, instead of redefining handlers mid-script.

- [x] **OCR-196** `scripts/sync-skills.sh:91` _bug_
  The rollback here is best-effort and unchecked. If it fails (permissions, cross-device move, AV interference), control falls through to `exit 1`, which triggers the EXIT trap `finish`: it retries the same `mv`, and on failure proceeds to `rm -rf "$OLD" "$TMP"`, destroying the only remaining copy of the previous mirror and leaving `$DST` absent. Verify the restore and abort loudly (keeping `$OLD`) when it cannot be performed; do not delete `$OLD` until the swap has been confirmed.

- [x] **OCR-197** `scripts/uninstall.ps1:36` _bug_
  `$missingMarkers` is evaluated before the `-not $root` guard. If `$root` ends up null/empty (e.g. `Split-Path` yields nothing), `Join-Path $root $_` throws a terminating parameter-binding error and, because `$ErrorActionPreference = "Stop"`, the script aborts with an unfriendly error instead of the intended `Fail` message. Guard `$root` before using it in `Join-Path`.

- [x] **OCR-198** `scripts/uninstall.ps1:52` _bug_
  The uninstall summary messages do not reflect failures: the success message (`removed hackathonkit plugin and marketplace`) is printed even when one or both codex commands failed (`$codexFail = 1`), and the final `Fail` summary mentions only path failures even though `$codexFail` is part of the condition. Report both counts/actual outcome instead of unconditionally claiming success.

- [x] **OCR-199** `scripts/uninstall.sh:53` _bug_
  Dry-run overstates the effect of `--yes`. In dry-run mode every shipped skill name is reported as "would remove" as long as the mirror directory exists, but the apply path removes a mirror entry only when `[ -d "$mirror/$name" ]` is true *and* `diff -r` shows it is byte-identical; modified copies are explicitly preserved (see the `keeping modified ...` branch). So a preview can advertise removals that will never happen, and the user gets no hint about which entries will be kept. Mirror the apply-path conditions in the dry-run branch.

- [x] **OCR-200** `scripts/uninstall.sh:65` _bug_
  `codex plugin remove` / `marketplace remove` failing is treated as a hard failure that forces exit 1, but these commands also fail when the plugin/marketplace was simply never registered — a normal state for a partial/uninstalled setup. That turns a benign no-op into a reported failure. Consider distinguishing "not installed" from a real removal error (e.g. check registration first, or only warn), so the exit status reflects an actual problem.

- [x] **OCR-201** `scripts/uninstall.sh:86` _bug_
  Prompt removal failures are silently swallowed: output is discarded and `|| true` guarantees success, so a missing/crashing `install-prompts.sh` leaves stale `/verify` prompts behind while the script still exits 0 and prints the success message. Count the failure into `FAILURES` (and let the diagnostic through) so a partial uninstall is reported.

- [x] **OCR-202** `scripts/verify-command.test.sh:67` _test_
  The no-python3 fallback only greps for `commands/plan.md`, so a manifest that omits research/build/evaluate/verify still passes, while the python3 branch would correctly fail it. Manifest coverage silently degrades on minimal environments — check all five command paths in the fallback too.

- [x] **OCR-203** `scripts/verify-command.test.sh:98` _test_
  After appending 'custom edit' to verify.md the test only asserts the file still exists; it never verifies the user's edit survived, so a `remove` that truncates or rewrites the file would still pass and the data-integrity behavior of the installer is left unproven. Assert the modified content is retained (e.g. grep for the appended text).

- [x] **OCR-204** `scripts/write-install-state.ps1:30` _bug_
  Version extraction degrades silently. If `plugin.json` parses but has no (or a falsy/empty) `version`, `$Version` quietly stays `unknown` with no warning, so the recorded version can be misleading without any signal. The `.sh` sibling emits a warning in this case. Emit a warning on the missing-version path as well (and be aware the `[string]` cast will coerce a non-string value such as a number/array rather than rejecting it).

- [x] **OCR-205** `scripts/write-install-state.ps1:42` _bug_
  Silent zero for skills. When the `.agents\skills` mirror is absent, `$Skills` stays 0 and no diagnostic is emitted, so a broken/partial install is recorded as a valid state (the `.sh` sibling warns in this case). Also `Test-Path $mirror` uses the positional `-Path` parameter, so wildcard characters (`[`, `]`) in `$Root` are interpreted as patterns — every other probe here uses `-LiteralPath`. Warn when the mirror is missing and use `-LiteralPath` for consistency.

- [x] **OCR-206** `scripts/write-install-state.ps1:53` _bug_
  The component flags mix two different notions of "installed". `hooks`/`learning` only probe the shipped plugin tree (`plugins\hackathonkit\...`), `agents` probes the user's `.codex\agents`, while `skills` counts the `.agents\skills` mirror. As a result the emitted state can be internally contradictory — e.g. `hooks = true` and `learning = true` while the mirror is empty (`skills = 0`) — and because the hooks/learning probes only test files that the plugin always ships, those flags are effectively still hardcoded `true`, which is exactly what the "derive components from what is actually present" comment intended to avoid. Align the probes on a single definition of "installed" (user-visible targets), or document what each field means.

- [x] **OCR-207** `scripts/write-install-state.sh:65` _bug_
  The `components` block is hardcoded and completely ignores the validated `$PROFILE`. A `--profile core` install therefore writes exactly the same manifest (`hooks/agents/learning = true`, all four adapters) as a `full` install, so downstream consumers (`doctor.sh`, `repair.sh`, resource-aware planning) cannot tell what was actually installed. Either derive these flags/adapters from real filesystem evidence (or from a caller-supplied parameter as the PowerShell sibling was changed to do), or stop advertising a component set that is not verified.

## LOW

- [x] **OCR-208** `.claude-plugin/marketplace.json:14` _maintainability_
  The plugin version is hardcoded here (0.16.31) and duplicates the version in `.claude-plugin/plugin.json` (also 0.16.31). Nothing in the toolchain keeps the two in sync — `scripts/sync-harness.sh` only validates that this file parses, and the README only instructs bumping the plugin version. They match today by luck; once they drift, marketplace resolution/upgrade detection will advertise a stale version. Consider dropping the per-plugin `version` (letting the plugin manifest be the single source of truth) or adding a check that compares both files.

- [x] **OCR-209** `.codex/agents/evaluator.toml:20` _maintainability_
  The criterion maxima are hard-coded here (25/20/15/20/20) and described as "a convenience copy" of `rubric.md`. If `rubric.md` changes, these numbers silently drift and the model will emit scores that violate the source-of-truth maxima, which `eval.aggregate` then rejects. Either derive the maxima from the rubric at load time, or add an explicit final cross-check step against `rubric.md` before emitting the report so drift surfaces deterministically rather than as an aggregate failure.

- [x] **OCR-210** `.codex/agents/researcher.toml:7` _maintainability_
  The comment says `model` is left to the harness default by design and in the same sentence says to pin it here to freeze behavior, but no `model` key exists to pin. A maintainer cannot tell whether the default is intentional or a TODO, and could add or remove `model` thinking they are restoring intended behavior. Either add the `model` key or rewrite the comment so it does not promise freezing that the config does not provide. The same issue appears in both `researcher.toml` and `reviewer.toml`.

- [x] **OCR-211** `.codex/agents/reviewer.toml:21` _maintainability_
  These two bullets restate the same rule ("do not run tests or builds") with slightly different forbidden lists and two different unverified labels ("an unverified claim" vs "unverified - read-only"). Duplicated, partially-overlapping constraints make the actual forbidden set ambiguous (bullet 1 forbids only tests/builds/network fetches and explicitly permits everything else that is "read", which sits awkwardly next to "You are read-only"). Merge into a single canonical rule with one label.

- [x] **OCR-212** `.codex/agents/reviewer.toml:25` _maintainability_
  The contract asks for "structured fields" but never names a concrete format, unlike the sibling evaluator profile which pins the exact JSON shape. As written, each review can emit a different structure, so the contract is not actually checkable despite the stated goal. Name the encoding (e.g. a single JSON array of finding objects) and say how a finding with no single line (`line`) is represented, so downstream tooling can validate it.

- [x] **OCR-213** `.codex/config.toml:1` _maintainability_
  `#:schema` is written as an ordinary TOML comment. It only takes effect if the consuming tooling recognizes this specific directive form; otherwise this file receives no schema validation at all, and TOML mistakes (e.g. root-level keys captured by a table) go entirely unchecked. Verify the directive is honored by your editor/CI, and add an explicit parse/validate step for this file so validation does not silently fall away.

- [x] **OCR-214** `.codex/config.toml:12` _maintainability_
  Linking `$theme-research` behavior to a `web_search` switch that is hardcoded to "disabled" means external verification is off by default across every environment; the agent is instructed to treat external facts as gaps rather than verify them. If this file is the committed default for all users, consider whether "live" (or per-environment override) is the intended baseline, since a disabled default can silently degrade research quality without any signal.

- [x] **OCR-215** `.dockerignore:5` _maintainability_
  `!/.env.example` is redundant: `!**/.env.example` on the next line already matches the root file because `**` can expand to zero directories. The leading-slash form also differs from the rest of the file's convention, so keeping only the canonical `!**/.env.example` reduces confusion.

- [x] **OCR-216** `.dockerignore:12` _maintainability_
  `**/*.md` excludes every markdown file in the context and only the root `README.md` is re-included. That is fine for plain docs, but any nested markdown that tooling depends on (e.g. package-level READMEs referenced from build/packaging metadata, or files pulled in by `go:embed`) would silently disappear and break the build. Confirm this breadth is intentional; if only documentation is meant to be dropped, consider `**/*.md` + explicit negations for the paths that must remain.

- [x] **OCR-217** `.gitattributes:18` _maintainability_
  `.obj` (Wavefront), `.stl` (ASCII variant) and `.gltf` (JSON-based glTF) are frequently text formats. Marking them `binary` sets `-diff -merge -text`, which disables diffs, blame and merge resolution for revieweable assets. If these are intended to be binary-only, please confirm; otherwise keep them as text (or use `-diff` alone) so that text-based assets remain reviewable/mergeable.

- [x] **OCR-218** `.gitattributes:26` _maintainability_
  These explicit `text eol=lf` entries duplicate the global `* text=auto eol=lf` default and add no effect (same value is applied), so they are pure maintenance burden — every new text extension must be kept in sync in two places. Only the CRLF overrides below actually change behaviour; consider removing the redundant LF lines and keeping just the CRLF block.

- [x] **OCR-219** `.gitignore:7` _bug_
  The re-inclusion of the example env file is root-anchored (`!/.env.example`), so it only un-ignores the top-level file. A future nested `.env.example` (e.g. under a sample/example directory) would stay ignored by `.env.*` and be silently lost. Use an unanchored negation (or add a matching one per nested location) if nested examples are expected.

- [x] **OCR-220** `.gitignore:11` _bug_
  These patterns are unanchored, so they match at *any* depth, not just the repository root. `*.log` in particular can silently hide a legitimate fixture (e.g. a sample log under a skill's `references/` or a test data directory), and `coverage/` / `htmlcov/` would swallow a same-named fixture directory anywhere in the tree. If only root-level build/test artifacts are meant to be excluded, anchor them with a leading `/`; otherwise confirm no tracked file/directory at a nested depth matches.

- [x] **OCR-221** `Dockerfile:6` _performance_
  Two independent `go build` invocations in separate layers, without BuildKit cache mounts for `/go/pkg/mod` and `/root/.cache/go-build`, so every rebuild after a source change re-downloads modules and recompiles all shared packages twice (the `quality` stage already uses cache volumes, so the pattern exists in this project). Combine the builds into one layer and mount the caches.

- [x] **OCR-222** `Dockerfile:19` _security_
  The UID/GID build args are not validated, so `--build-arg HACKATHONKIT_UID=0` (or `HACKATHONKIT_GID=0`) silently produces an image whose `USER` is root — contrary to the checklist rule "containers run as ${HACKATHONKIT_UID:-1000}:${HACKATHONKIT_GID:-1000}, never root". Guard the values (fail the build) before switching user.

- [x] **OCR-223** `Dockerfile:27` _security_
  `HACKATHONKIT_DEMO_HOST=0.0.0.0` is needed for the compose port mapping to work, but the comment above advertises `docker run -p 8787:8787`, which publishes the unauthenticated dashboard (and its read-only data/metrics endpoints) on every host interface — the project's checklist requires published ports to bind `127.0.0.1`. Keep the container-side bind and update the guidance to `-p 127.0.0.1:8787:8787`.

- [x] **OCR-224** `cmd/demo-api/fly_live.go:135` _maintainability_
  Stat/Open/Read failures are mapped to `setAvailable(false)` with no logging or error surfaced to the caller. Because `setAvailable(false)` also bumps `revision`, a transient filesystem error (or a momentarily missing path) is indistinguishable from a deliberate feed outage: clients just observe `feed_available: false` frames with stale recommendation/feedback and have no way to tell the feed is actually broken. Consider logging the underlying error (with the path) at least once, or exposing a reason/error field so operators can diagnose silent failures; the best-effort contract in the doc comment should also state that errors are intentionally swallowed here.

- [x] **OCR-225** `cmd/demo-api/main.go:92` _bug_
  `meta.Close()` is called here explicitly, but `main` also registers `defer meta.Close()`, so on the normal shutdown path (signal received) the recorder is closed twice. It happens to be harmless today because `metadata.Disabled.Close()` is a no-op and `(*Postgres).Close()` nils the pool under a lock, but the redundant close is fragile — a `Recorder` implementation whose `Close` is not idempotent (e.g. an `os.File`/transaction-backed one) would misbehave. Keep the defer and drop this trailing call (the explicit call before `os.Exit(1)` must stay, since `os.Exit` skips deferred functions).

- [x] **OCR-226** `cmd/demo-api/main.go:230` _security_
  The mutation token is interpolated into the served HTML/JS verbatim. A token containing `"`, `<`, `&` or `</script>` breaks the page or injects markup, and because `/fly-viz` is an unauthenticated GET, the secret is also handed to every client that can reach the port. Use an escaped/JSON-encoded substitution (e.g. `template.JSEscapeString`) or keep the token out of the page and have the client supply it via the `X-HackathonKit-Token` header.

- [x] **OCR-227** `cmd/demo-api/main.go:442` _security_
  `slug` is joined into a filesystem path with no normalization. The slug used by `/plan/api/latest` and `/plan/decision` comes straight from the plan JSON body (`plans.Load` only normalizes it when the JSON `slug` field is empty), so a plan file containing `"slug": "../../x"` makes this build a path outside `evals/plans` (a read in `readApproval`, and a write in `writeApproval`). Normalize/validate the slug before constructing the path.

- [x] **OCR-228** `cmd/fly-bench/main.go:103` _bug_
  The benchmark runs on a bare `context.Background()` with no deadline or cancellation. The embedder/LLM calls go through `openai.Client` (180s HTTP timeout each), but there is no bound on the total run, so a stalled endpoint makes every row (plus the warmup) take up to minutes with no way to abort early and no way for callers to bound the run. Use a bounded context and cancel it when `run` returns.

- [x] **OCR-229** `cmd/fly-bench/main.go:115` _maintainability_
  The warmup result is discarded, so nothing verifies that the intended pipeline (embedding/LLM) actually engaged before the timed run starts. If the embedder or classifier fails at runtime the router silently falls back to the hash encoder and the run still publishes A/B evidence as if the full pipeline were measured; the only guard is the later `engaged == 0` check on a non-empty top-1. Capture the warmup `Result` and fail (or warn on stderr) when the embedding path never engaged, e.g. by asserting `Encoder` contains "embedding".

- [x] **OCR-230** `cmd/fly-bench/main.go:138` _maintainability_
  Determinism is asserted from a single repeated call on the *same* already-warmed router instance and only compares the top-1 string; changes in the full ranked order, and any cold-start/process-level nondeterminism (persistent embedding cache, map-iteration tie-breaks), are invisible, so `determinism_rate` can overstate stability of the evidence. Consider comparing the full ordered recommendation lists (and/or repeating on a freshly constructed router) and documenting the measured scope in the report field.

- [x] **OCR-231** `cmd/fly-bench/main.go:172` _bug_
  The output-stream encode error is discarded (cmd/fly-bench/main.go: `_ = enc.Encode(rep)`; internal/mcp/server.go: `_ = enc.Encode(...)` for the oversize-frame response), while the success path in server.go does return it. A broken pipe or full disk silently truncates the emitted JSON/message while the process or session still reports success. Propagate the write error in both places.

- [x] **OCR-232** `cmd/fly-train/main.go:71` _bug_
  `--epochs` is validated before the `--model knn` early return, although it is never used on that path. `fly-train --model knn --epochs 0` is rejected with exit code 2 even though a valid rebuild was requested. Move this check below the knn branch (or only apply it when `*model == "mb"`).

- [x] **OCR-233** `cmd/fly-train/main.go:125` _bug_
  `--kc` and `--sparsity` are never validated, and `fly.NewMBrain`/`activate` silently substitute defaults for out-of-range values (`kc <= 0` -> 4133; `sparsity <= 0 || sparsity >= 1` -> `MBDefaultSparse`). The command therefore trains with values the user did not ask for while the summary below echoes the raw flags (`KC=%d sparsity=%.2f`), e.g. `--sparsity 1` or `--sparsity 0` reports `sparsity=1.00`/`0.00` but actually trains dense/default sparsity. Validate these flags together with `--model`/`--epochs` (require `*kc > 0` and `0 < *sparsity < 1`) so failures are explicit instead of silently sanitized.

- [x] **OCR-234** `cmd/hackathonkit-ctl/daemon.go:243` _security_
  Several handlers (`/healthz`, `/api/sessions`, `/api/events`, `/api/risk`, and the aggregated `events:`/`outputs:` string in `/api/sessions/`) echo the raw `err.Error()` of the underlying SQLite layer into the JSON response. Even on a loopback control API this leaks internal details (schema/table names, file paths, driver errors) that a caller cannot otherwise observe. Prefer logging the detailed error server-side and returning a stable generic message to the client.

- [x] **OCR-235** `cmd/hackathonkit-ctl/detach_windows.go:12` _documentation_
  The comment claims the child keeps "its own process group", but with `DETACHED_PROCESS` the child inherits no console, and Windows process groups are meaningful only for processes attached to a console (`CREATE_NEW_PROCESS_GROUP` is documented as being ignored when it cannot apply). Combined with DETACHED_PROCESS the flag is effectively inert, so the stated rationale is misleading. Either drop the redundant `CREATE_NEW_PROCESS_GROUP` or reword the comment to describe what actually happens (no console inherited, thus no console control events reach the daemon). Low severity / doc-only.

- [x] **OCR-236** `cmd/hackathonkit-ctl/main.go:147` _bug_
  `len(task)` counts bytes while `truncate(task, 40)` limits runes, so for tasks containing multi-byte characters the check can trigger while `truncate` returns the string unchanged, appending a spurious `...`. Compare rune counts instead.

- [x] **OCR-237** `cmd/hackathonkit-ctl/main.go:251` _maintainability_
  Error causes are flattened with %v so callers cannot unwrap them. In cmd/hackathonkit-ctl/main.go the retry branch wraps only the second git failure while firstErr (the actual reason the first attempt failed) is flattened with %v. In internal/routing/fly/hash_encoder.go the fallback error is formatted with %v while only the primary cause uses %w. Use %w for every cause (or errors.Join, since errors is already imported) so errors.Is/As can match all of them.

- [x] **OCR-238** `cmd/hackathonkit-ctl/main.go:306` _bug_
  `fs.Parse` returns `flag.ErrHelp` for `-h/--help`, and no subcommand handles it: `hackathonkit-ctl run --help` (same for `ingest`, `sessions list`) prints the flag usage to the process `os.Stderr` (the FlagSet default output, again ignoring the injected writer) and then `run()` reports `error: flag: help requested` with exit code 1, whereas the top-level `help` path returns 0. Treat `errors.Is(err, flag.ErrHelp)` as a successful help request (e.g. return nil) and/or set `fs.SetOutput(stderr)` so flag diagnostics go to the provided writer.

- [x] **OCR-239** `cmd/hackathonkit-ctl/proc_windows.go:19` _bug_
  The `pid int` argument is truncated to `uint32` without a range check. `readPID` (daemon.go:207-215) only rejects `pid <= 0`, so a corrupt or hand-edited pid file containing a value above math.MaxUint32 (e.g. 4294967297) is silently folded onto an unrelated, potentially live PID here, and `os.FindProcess` in the caller truncates the same way before killing it. Validate the upper bound (e.g. reject `pid > math.MaxUint32`) before narrowing the value.

- [x] **OCR-240** `cmd/hackathonkit-ctl/tui.go:47` _bug_
  `Ingest` is invoked with an unbounded `context.Background()` on the startup path, before `program.Run()`. A slow or stalled ingest (large JSONL corpus, lock contention) blocks the TUI from ever starting, with no deadline and no cancellation. Give it a bounded context so startup cannot hang indefinitely.

- [x] **OCR-241** `cmd/hackathonkit-mcp/main.go:44` _security_
  Coverage gap in the credential filter: `redactMarkerValues` only matches `key=value` markers, so the equally common `key: value` / `key value` renderings produced by middleware, config dumps and HTTP-error echo (e.g. `password: hunter2`, `Secret Key: abc`, `Authorization: Bearer eyJ...`) pass through unreleased into the logs. Add colon/space-separated variants of the sensitive keys (the same idea the repo's shell-side redaction uses). Note the delimiter is consumed by the marker, so a double space would need the `"key:"` form as well if you want full coverage.

- [x] **OCR-242** `cmd/hackathonkit-mcp/main.go:366` _security_
  Raw backend errors are logged without going through the repository's redaction helper. In cmd/hackathonkit-mcp/main.go the Serve error is logged verbatim (the only backend/error log in the file that bypasses redactBackendError, which redacts every sibling path such as metadata_disabled/objectstore_disabled/storage_backend_error). In internal/objectstore/minio.go the raw MinIO MakeBucket error is emitted with %v even though the same redaction helper documents that MinIO errors can echo DSNs/credentials, and Open still returns success so the caller never gets a chance to redact it. Route both through redaction (or log without the raw error text).

- [x] **OCR-243** `cmd/hackathonkit-rehearse/main.go:169` _bug_
  Returning `nil` for an error on the walk root silently converts an inaccessible/missing `data/artifacts` directory into a successful, empty listing (`out` empty, `err` nil). `run` only records a note when `listArtifacts` returns an error, so this case is reported as a clean "0 artifact(s)" PASS-style run instead of surfacing the filesystem problem. Propagate the error (or explicitly handle `os.IsNotExist` and add a note) so a missing artifacts directory is visible. Errors on non-root paths are already propagated.

- [x] **OCR-244** `docker-compose.yml:51` _bug_
  `HACKATHONKIT_DEMO_TOKEN` defaults to empty here, and `authorizeMutation` now fails closed on an empty token (cmd/demo-api/main.go:544-549), so every state-changing endpoint (plan approvals, `POST /routing`) returns 401 whenever `.env` has no token — e.g. a fresh `.env.example` copy. Prefer requiring it like the other secrets so the misconfiguration surfaces at `docker compose up` instead of at the first dashboard click.

- [x] **OCR-245** `internal/atomicfile/atomicfile.go:84` _bug_
  Both directory-sync errors (`Open`, `Sync`) are discarded and the function still returns nil. On a crash/power loss the rename may be durable while the directory entry is not, so the caller is told the write succeeded even though the new name can disappear — precisely the durability guarantee this package exists to provide. Unlike the temp-file cleanup (which is documented as best-effort), nothing here documents this as intentionally best-effort. Consider surfacing the sync failure (or at least documenting the deliberate best-effort behavior) so callers relying on crash safety are not misled.

- [x] **OCR-246** `internal/config/config.go:377` _bug_
  The pre-lock read and parse errors are silently discarded (`if data, err := os.ReadFile(...); err == nil`). If routing.yaml exists but is transiently unreadable, `current` stays at `{normal, 1}`; when `routingInit` is false (a freshly constructed Manager that has never read the file) this then writes version 2, regressing the on-disk version below an already higher value. `Routing()` handles the same failure by logging and keeping the last known state; handle/report the error here consistently and avoid defaulting the version to 1 on a real read error.

- [x] **OCR-247** `internal/config/dotenv.go:21` _bug_
  Only the exact lowercase `export ` (single ASCII space) prefix is stripped. A shell-legal line such as `export\tKEY=value` (tab) or `EXPORT KEY=value` is left intact, so the parsed key becomes `export\tKEY` / `EXPORT KEY`, fails `validEnvKey`, and the assignment is dropped with only a log line - the variable silently stays unset. Suggest consuming the `export` keyword followed by any whitespace while still refusing to match a real key that merely starts with `export` (e.g. `exportFOO=1`).

- [x] **OCR-248** `internal/control/handoff.go:105` _maintainability_
  `stage.Goal` (and `plans.Summary(*h.Plan)` on the line above) is interpolated without the `line()` normalizer that task/risk-reasons/learnings go through, so a goal containing a newline or stray whitespace breaks the intended single-line, paste-ready bullet format. Apply `line()` here for consistency.

- [x] **OCR-249** `internal/control/risk.go:85` _bug_
  The panic classifier is fragile: only the exact negation form `no [errors or ]panic` is stripped, so other negated or quantified forms in tool payloads (e.g. "without panic", "panic-free", "0 panics", "panics=0") still match `panicWord` and are reported as real panic events, inflating the panic reason/score. Conversely a genuine panic expressed as, say, "recovered without crash: panic" could be masked if it shares a line with a stripped negation. Consider matching on bounded values/keys or an explicit allowlist of positive signals rather than free-text word matching.

- [x] **OCR-250** `internal/control/store.go:168` _bug_
  `parseTime` collapses every unparseable/foreign-format timestamp to `time.Time{}` with no error signal, and `formatTime` maps zero back to `""`. Callers cannot distinguish "corrupt ts" from "never set": `handleSessionRecord` passes the result to `EndSession`, which substitutes `time.Now()` for a zero ts, and `AppendEvent` does the same — so a malformed `ts` in the JSONL is silently recorded as ingest time rather than surfaced. The same ambiguity breaks the lexicographic window range in `EventsBetween` and cursor-rewrite detection. Return an error (or a validity flag) so callers can reject/locate corrupt records instead of fabricating timestamps.

- [x] **OCR-251** `internal/evaluation/bundle.go:108` _maintainability_
  The `os.Stat` error cause is discarded: if `os.Stat` fails for a reason other than the path not being a directory (e.g. permission denied on `root`), the caller sees the misleading "bundle root is not a directory". Split the two cases and wrap the original error with `%w`.

- [x] **OCR-252** `internal/evaluation/bundle.go:209` _bug_
  Ownership-marker/order bug: `.hackathonkit-bundle` is written into the temp bundle *before* `filepath.WalkDir` runs, and it is not part of `DefaultBundleExcludes`, so a repository file named `.hackathonkit-bundle` at the root is copied over the marker. The resulting bundle then contains a marker whose content != `outAbs`, so the next run refuses to overwrite it with "not a bundle created by this tool" and the output directory becomes permanently unusable without manual cleanup. The same ordering silently discards a repo file named `git-sha.txt`/`rubric-version.txt`, and `bundle.json`/`bundle-hash.txt` are copied into the bundle yet excluded from the hash. Reserve these names when copying (or write the marker after the walk).

- [x] **OCR-253** `internal/evaluation/bundle.go:365` _security_
  Glob patterns that contain a `/` silently match nothing: `path.Match(rule.pattern, rel)` uses `*` which never crosses `/`, and the second check matches the same pattern against `base`, which never contains `/` (and is only reached if the full-path match failed). So user excludes like `**/secrets`, `data/*/cache` or `*/generated` are accepted by `validateExcludes` (they are syntactically valid) but exclude nothing — exactly the leak the comment on `validateExcludes` claims to prevent. Consider matching the pattern against every ancestor prefix of `rel`, or rejecting patterns that combine `/` with glob metacharacters.

- [x] **OCR-254** `internal/evaluation/evaluation.go:152` _bug_
  When a score is missing (or out of range) the loop `continue`s, so `sum` no longer reflects the criteria the report actually declared. The subsequent total/sum check therefore emits a second, misleading problem such as `total 75 does not match criterion sum 50` for a report that only omitted one criterion, obscuring the real cause. Track whether all criterion scores were usable and only reconcile `total` against a complete `sum` (the report is already rejected either way).

- [x] **OCR-255** `internal/evaluation/evaluation.go:427` _bug_
  `minDelta` rejects NaN but not ±Inf. A non-finite threshold reaches `Calibration.NoiseFloor` (`math.Max(minDelta, 2*delta)`), which then cannot be marshalled (`json: unsupported value: +Inf`) and, when fed back into `Compare`, is silently clamped to 0 — losing the operator's intent. This is reachable through configuration: `internal/config/config.go` accepts any `min_delta >= 0`, including a YAML `.inf`. Reject non-finite thresholds here.

- [x] **OCR-256** `internal/evaluation/evaluation.go:430` _bug_
  `Calibrate` compares the means of two runs but, unlike `Rank`, never verifies that both aggregates were produced under the same rubric version. Both runs are allowed to carry an empty or differing `RubricVersion`, which contradicts the package contract that scores from different rubric versions are never compared. Add the same version guard `Rank` applies.

- [x] **OCR-257** `internal/learning/learning.go:94` _bug_
  `WithEmbedder` (and `WithClock`) mutate struct fields that `Record`/`Recall` read on request paths with no synchronization. Current wiring happens before the store is served, but nothing prevents a caller from invoking these after the store is in use, which would be a data race. Either document that these must be called before first use or guard the fields.

- [x] **OCR-258** `internal/learning/learning.go:157` _performance_
  `s.mu` is held across the cross-process lock acquisition (up to ~1s of sleeping on contention) and the subsequent file read/write. Because Recall/Top/Count only need `s.mu` to read the store, they can be stalled for the whole duration of a peer process's write. Consider using a separate mutex for the file-lock sequence, or reading the store without blocking on the writer mutex.

- [x] **OCR-259** `internal/learning/learning.go:388` _bug_
  `Count` converts any load failure (corrupt JSON, permission error, I/O error) into a successful `0`, silently masking data loss and making a broken store indistinguishable from an empty one. Since this is a public method, consider returning `(int, error)` or at least logging the failure so operators can observe it.

- [x] **OCR-260** `internal/mcp/registry.go:30` _maintainability_
  Duplicate tool names are silently overwritten: an existing entry's `Description`, `InputSchema`, and `Handler` are replaced in place while the original `order` entry is kept. A duplicate registration (easy to introduce when many `register*` functions feed the same registry) is thus masked instead of surfaced. Consider returning a bool/error for duplicates or at least logging/panicking on conflict so callers can detect the mistake.

- [x] **OCR-261** `internal/mcp/server.go:59` _bug_
  Truncation is byte-based, so a multi-byte UTF-8 rune can be cut in half and the client receives a torn sequence (rendered as U+FFFD) in the error message. Truncate on a rune boundary instead. Note also that this sanitizer is only applied to `err.Error()` strings; the client-supplied tool name (`"unknown tool: " + p.Name`) and method (`"method not found: " + req.Method`) are echoed back unbounded and unsanitized at the same RPC boundary.

- [x] **OCR-262** `internal/mcp/server.go:119` _bug_
  When the discarded oversized frame ends at EOF without a trailing newline, the drain loop returns before the oversize sentinel is ever pushed onto `lines`, so no error response is emitted and `ServeStreams` returns nil (apparent success). The client cannot distinguish this from a clean shutdown, even though the same oversized frame is reported as an error when it is newline-terminated. Deliver the oversize response before reporting EOF.

- [x] **OCR-263** `internal/metadata/metadata.go:272` _bug_
  `poolRef` hands out the pool reference and releases the read lock before the caller calls `Exec`, while `Close` nils the field and then closes the pool outside the write lock. A `Record*` call that obtained the reference just before shutdown can therefore execute against a pool that is being closed; pgx returns a "closed pool" error, and callers discard it (`_ =`), so the record is dropped silently. Since `Close()` is reached via `defer meta.Close()` in `main` while request goroutines may still be recording, consider coordinating shutdown with in-flight writes (e.g. reference counting / WaitGroup) rather than relying on the nil check alone.

- [x] **OCR-264** `internal/objectstore/minio.go:56` _bug_
  Whitespace is trimmed only for the validation check, while the raw untrimmed value is what actually gets used, so a value with surrounding whitespace (common when read from a .env/CRLF file) passes the guard and then fails later with an opaque error. In internal/objectstore/minio.go the check trims AccessKey/SecretKey but credentials.NewStaticV4 receives the untrimmed values, yielding a confusing SignatureDoesNotMatch at request time. In internal/resources/probe.go Configured is computed from the trimmed key but the raw apiKey is sent as the header, producing a spurious 401/403 for a key that was reported as configured. Normalize once and use the trimmed value both for the check and for the request.

- [x] **OCR-265** `internal/observability/obs.go:167` _performance_
  Prepending each chunk with `append(tmp[:n], buf...)` re-copies the entire accumulated buffer on every iteration, and the subsequent `bytes.Count(buf, ...)` re-scans it as well, making the backward read O(n^2) in the tail size (up to ~4 MiB → tens of MiB of copying plus scanning for a file with few newlines in its last 4 MiB, e.g. one very long line). Accumulate the chunks in a slice and concatenate once at the end (or read into a preallocated buffer at the right offsets) and count only the freshly read chunk.

- [x] **OCR-266** `internal/observability/obs.go:170` _bug_
  The backward read stops as soon as `limit+1` **newlines** are buffered, but lines that are not valid JSON (blank lines, `undefined\n` written by `plugins/hackathonkit/scripts/hooks.js`, a record truncated by a crashed writer, or any non-JSONL writer sharing `logs/*.jsonl`) are only discarded later by the `json.Valid` filter. A tail containing such lines therefore makes `TailJSONL` return fewer than `limit` records even though more valid records exist earlier in the file — silently under-returning on the user-facing caller (`cmd/demo-api/main.go` `tail()` passes the raw `?limit=` value). Count the lines that actually pass validation, or keep reading until `limit` valid records are collected or `offset` reaches 0. The same silent truncation happens on the `maxTailBytes` path for a file whose tail exceeds 4 MiB.

- [x] **OCR-267** `internal/openai/client.go:50` _maintainability_
  A single hardcoded 180s `Timeout` is applied to every request, including large audio uploads (files up to 25MB) and long chat completions. Because `http.Client.Timeout` caps the whole round trip and callers can only shorten it via their context, a slow-but-legitimate upload can never succeed even if the caller would allow more time. Consider making the timeout configurable (e.g. via `config.OpenAISettings`) or relying on the caller's context deadline for long-running endpoints.

- [x] **OCR-268** `internal/openai/client.go:174` _bug_
  The "missing vector" check only catches a nil slice; a provider that returns an empty array (`"embedding": []`) yields a non-nil zero-length vector that passes this guard and is then returned to callers as a silently invalid embedding. Check the length instead of nil in both the unindexed branch and in the final validation loop.

- [x] **OCR-269** `internal/openai/client.go:190` _bug_
  The indexed branch assigns `result[index] = d.Embedding` without detecting duplicate indices. A provider returning the same index twice silently overwrites the earlier vector, leaving another slot nil (surfacing later as a confusing "missing a vector" error) or, worse, mapping vectors to the wrong inputs. Consider rejecting duplicate indices explicitly.

- [x] **OCR-270** `internal/plans/plans.go:161` _maintainability_
  The synthetic corrupt-plan error discards the underlying `json.Unmarshal` cause, so callers (e.g. `internal/tools/plan.go`, which surfaces `latestErr.Error()`) cannot tell why a file was rejected. Preserve the cause so diagnostics are actionable.

- [x] **OCR-271** `internal/quality/proc_unix.go:1` _maintainability_
  The build constraint excludes `plan9`, `js` and `wasip1`, but the only other implementation in the package (`proc_windows.go`) is gated on `windows`. On those three GOOS neither file matches, so `configureProcessGroup`/`killProcessGroup` are undefined and the whole `quality` package fails to compile (e.g. `GOOS=js GOARCH=wasm go build ./...`), rather than silently losing group-kill semantics. Add a no-op fallback stub constrained to `!unix && !windows` (or `plan9 || js || wasip1`) so every target that the package can be built for has the functions defined — the sibling `cmd/hackathonkit-ctl` detach/proc helpers hit the same asymmetry.

- [x] **OCR-272** `internal/quality/proc_windows.go:12` _bug_
  On the Windows build the process-group contract exposed to callers is not actually honoured: `configureProcessGroup` is a no-op and `killProcessGroup` terminates only the direct child, so grandchildren spawned by gate commands (e.g. `go test ./...` test binaries, `sh scripts/*.sh` children) keep running after `quality.go` reports `timeout`. `WaitDelay` only bounds the pipe drain, it does not reap those descendants, which can hold source/output files, ports or locks long after the gate completed. This is documented here, but if hermetic teardown is required, start the child with a Job Object (`CREATE_SUSPENDED` + `AssignProcessToJobObject` + `TerminateJobObject`) or at minimum `syscall.CREATE_NEW_PROCESS_GROUP` plus `taskkill /T`, and record the intended limitation where callers can see it.

- [x] **OCR-273** `internal/quality/quality.go:104` _bug_
  `exec.ErrWaitDelay` is documented as being returned by `Cmd.Wait` "if the process exits with a successful status code but its output pipes are not closed before the command's WaitDelay expires" — exactly the lingering-grandchild scenario this WaitDelay is meant to tolerate. With this switch such a check is reported as `failed`, and `Check` has no field carrying the cause, so a successful `go test`/`go build` whose output pipes stayed open is indistinguishable from a real failure and the gate is blocked with no actionable reason. Consider distinguishing `errors.Is(err, exec.ErrWaitDelay)` (own status, or at least record the cause/truncated output) from a genuine non-zero exit.

- [x] **OCR-274** `internal/resources/matrix.go:61` _maintainability_
  `yaml.Unmarshal` ignores unknown/misspelled fields by default. Since this file is documented as authoritative at runtime, a typo such as `capabilites:` inside a track silently yields an empty playbook (no decisions) instead of an error. Prefer a decoder with `KnownFields(true)` to fail fast on config typos.

- [x] **OCR-275** `internal/resources/matrix.go:81` _maintainability_
  `Validate` never inspects the variant lists: empty or duplicate variant `id`s pass validation even though `Plan` relies on the id (plus provider) to exclude the chosen variant from `Alternatives`. An empty id also collides with the zero `Variant` used for "no variants defined", making the two cases indistinguishable in output. Add checks that each capability has at least one variant and that ids are non-empty and unique.

- [x] **OCR-276** `internal/resources/matrix.go:113` _maintainability_
  A providerless variant is treated as always-ready and returned as soon as it is encountered. When it is listed before a ready provider-backed variant (e.g. `tabular: [deterministic, openai]` with `openai_api` ready), the ready provider is silently skipped and the decision is not marked `Degraded`. The `Variant` doc comment calls providerless variants the deterministic *fallback*, which suggests ready providers should be preferred; if the ordering is instead authoritative, please document that contract explicitly so config authors do not misread it.

- [x] **OCR-277** `internal/resources/matrix.go:198` _maintainability_
  `providerMissing` returns `nil` for a provider that has no matching `Status` entry (e.g. a typo'd or unknown provider id) as well as for a `Status` with both `MissingKeys` and `EnvKeys` empty. In those cases a degraded decision reports no missing keys, so a misconfigured provider id stays invisible to the user. Consider surfacing the unresolved provider id in `Missing`.

- [x] **OCR-278** `internal/resources/resources.go:112` _bug_
  `t.ID[:2]` assumes every id is at least two bytes with `-` at index 2, so a malformed/renamed id panics here, and the numeric branch (`number == t.ID[:2]`) matches only the two-digit prefix while the slug is compared with full equality only. Consequently a reference such as `1-management` resolves to `01-energy` instead of the track the user named. Split the id with `strings.Cut(t.ID, "-")` and validate the parts rather than slicing at fixed offsets.

- [x] **OCR-279** `internal/resources/resources.go:154` _maintainability_
  `Save` copies and persists every `Claimed` entry verbatim, and `Load` accepts any key, whereas `SetAnswer` rejects unknown providers. Hand-edited or legacy ids thus round-trip through the file forever: they are never surfaced by `Statuses`/`View` and never cleared. Filter `Claimed` against `defs` when loading or saving so the persisted profile only contains known provider ids.

- [x] **OCR-280** `internal/resources/resources.go:264` _maintainability_
  This agent prompt enumerates only OpenAI, NVIDIA, Telegram, Codex Pro and the track, while `defs` also contains `postgres` and `minio`. In the `!present` flow the agent will never be told to ask about those two providers, so the profile can end up incomplete relative to the configured provider set. Derive the list from `Defs()` (or `Statuses`) instead of hardcoding a subset.

- [x] **OCR-281** `internal/routing/fly/embed.go:136` _bug_
  The one-entry cache stores failed embeddings too (`ok == false`), so a transient embedder error for a given task string is replayed indefinitely until a *different* task evicts the entry. This differs from `ensureCentroids`, which deliberately bounds its negative cache to 30s. A long-lived caller that re-requests the same task (or a benchmark loop over one task) will never retry the embedder even after it recovers. Consider caching only successful results, or giving the negative entry a short TTL like the centroid cache.

- [x] **OCR-282** `internal/routing/fly/hash_encoder.go:61` _performance_
  `current.String()` is evaluated twice per flush, allocating a copy each time (once for the rune count, once for `ToLower`). In this per-token/per-text hot loop, build the string once and reuse it.

- [x] **OCR-283** `internal/routing/fly/llm.go:20` _bug_
  The typed-nil guard only covers `reflect.Ptr`. Any other nilable dynamic type that can implement `LLMClassifier` (named map/slice/func/chan type with value receivers, or an interface value) passes this check and `ClassifyTask` is then invoked on the nil value, which may panic inside the implementation. `reflect.ValueOf(r.llm)` is also evaluated three times. Consider a single reflect lookup that tests all nil-capable kinds (keeping the explicit `r.llm == nil` check, since `reflect.ValueOf(nil)` is an invalid Kind).

- [x] **OCR-284** `internal/routing/fly/llm.go:55` _maintainability_
  `first` is taken from the first element of a random map-iteration order, which is a fragile way to express the "all scores equal" check: it is only correct because the comparison is order-independent, and it silently changes meaning if anyone later assumes `first` is the max or a specific category. Deriving a deterministic baseline (min/max) makes the intent explicit and removes the subtlety.

- [x] **OCR-285** `internal/routing/fly/mb.go:289` _performance_
  The cached `*MBrain` is never mutated in place — `applyDopamine` builds a `clone` and publishes it via `r.brain = work`, and `Train` is not invoked on a Router-owned brain by any production call site. Holding the process-global `brainMu` across the purely CPU-bound `brain.scores` (O(KC) allocation plus an O(KC log KC) sort of 4133 elements and map writes) therefore adds no safety but serializes every concurrent `Recommend` across all Routers, and makes scoring block behind `applyDopamine`'s disk write. Take a local reference under the lock (or drop the lock here entirely) and score outside it.

- [x] **OCR-286** `internal/routing/fly/mb.go:404` _bug_
  Weights are clamped to +/-`mbWeightLimit`, but `Bias[category]` grows by `eta*0.1` on every `Train` call with no upper bound. Over repeated epochs/examples the bias can grow arbitrarily large while the readout weights saturate, letting the bias dominate the softmax logits and skew `Rank`/`scores` away from the learned weights. Clamp the bias to the same limit (or decay it) so it cannot overwhelm the weighted sum.

- [x] **OCR-287** `internal/routing/fly/memory.go:197` _maintainability_
  Embedding failure or an unexpected vector count is silently swallowed: the example is stored keyword-only with no log or metric. Callers cannot tell "no embedder configured" from "transient embedding outage", so a persistent embedder failure silently degrades semantic scoring to keyword-only. At minimum log the error for observability.

- [x] **OCR-288** `internal/routing/fly/memory.go:202` _performance_
  `memoryMu` is a package-level mutex shared by every Router, and it is held across `loadMemory` (file read + potential rename) and `saveMemory` (temp-file create/write/fsync/rename). All routers in the process therefore serialize on disk I/O even when they target different `memoryPath` values, and a slow filesystem stalls unrelated feedback writes. Prefer a per-Router mutex (or a path-keyed lock) so contention is scoped to the memory file actually being touched.

- [x] **OCR-289** `internal/routing/fly/memory.go:238` _maintainability_
  On a failed save the function returns `len(kept)` — the number of examples that would have been persisted — alongside the error, which is a success-shaped count for a write that never reached disk. Callers that ignore the error (or log the count before checking it) will report a memory size that does not exist. Return 0 on failure so the count only ever reflects persisted state.

- [x] **OCR-290** `internal/routing/fly/router.go:46` _bug_
  `embedTask` (internal/routing/fly/embed.go) returns this cached slice directly to its callers without copying (`return vec, ok`), so the same backing array is handed out by every call. Today all callers (`embeddingCategoryScores`, `mbCategoryScores`, and the `taskVector` passed to `memoryCategoryScores`) only read it, so there is no live corruption, but the ownership contract of `embedCacheVec` is undocumented and a single future in-place mutation (e.g. normalizing/zeroing an element) would silently corrupt the one-entry cache for subsequent requests. Either return a copy from `embedTask` or document on this field that the returned vector must be treated as read-only.

- [x] **OCR-291** `internal/routing/fly/router.go:150` _maintainability_
  Fusion is asymmetric between encoders, which makes the weight of earlier evidence depend on which downstream encoders happen to answer. Here the embedding step only reweights categories that `embeddingScores` returns (`combined = 0.5*prior + 0.5*emb`), so a keyword-only category keeps its full weight, while the kNN/MB/LLM steps below multiply *every* existing category by 0.6/0.4 and then add their own categories undecayed. Consequence: a task's category ranking (and its relation to the 0.55 threshold) changes depending on whether embeddings/LLM happen to be available, even though the task text is identical. This mirrors the open backlog item L67 — please confirm the intent and document it, or unify the combine step so the effective weight of each encoder is independent of the others.

- [x] **OCR-292** `internal/storage/store.go:80` _security_
  Fail-open fallback in the containment check. Walking up the ancestor chain, if no ancestor can be resolved by `EvalSymlinks` the loop reaches the filesystem root and `parent == p` returns `nil` (path accepted) instead of failing closed. This is the opposite of the fail-closed behaviour used for the root resolution above. (It is currently hard to reach because `sanitize` guarantees `full` is lexically under `s.root`, but a concurrent delete/rename of the root or an ancestor turns it into an accepted out-of-root path.) Return an error here so an unverifiable path is rejected.

- [x] **OCR-293** `internal/telegram/telegram.go:235` _bug_
  Only `HTML` gets entity-aware chunking; every other parse mode (notably `MarkdownV2`, which the `telegram.send` tool accepts) is chunked by `ChunkText`, which cuts at arbitrary rune/newline boundaries. A `MarkdownV2` entity or escape sequence that spans the 4096-rune boundary (e.g. `*bold*`, `__underline__`, `\*`) leaves the chunk unparsable and Telegram answers 400, so long formatted messages fail to send. Either add minimal MarkdownV2-balanced chunking, or reject/document unsupported parse modes rather than emitting chunks the API will refuse.

- [x] **OCR-294** `internal/telegram/telegram.go:240` _maintainability_
  On a mid-chunk failure this returns the zero-ish `SendResult` with `Chunks` set to the *total planned* chunk count, not the number actually delivered, and `MessageID` holding the last successfully sent chunk (or 0). The caller in `internal/tools/telegram.go` surfaces this on partial failure as `"chunks": result.Chunks`, so a partial send is reported as if every chunk had been planned/sent, which can mislead retry logic. Consider tracking and returning the number of chunks actually sent (e.g. a distinct `Sent`/`Delivered` field or setting `Chunks` to the sent count) so the partial result is unambiguous.

- [x] **OCR-295** `internal/tools/context.go:169` _performance_
  Every file is read fully into memory and then copied once more to a string just to count newlines (`string(data)` allocates a full copy per file, including large/binary assets). Since `data` is already a byte slice, use `bytes.Count(data, []byte("\n"))` to avoid the copy.

- [x] **OCR-296** `internal/tools/context.go:210` _maintainability_
  `total` is accumulated in this loop but never read afterwards (the function returns only `items`), so this is dead code that misleads readers into thinking a manifest subtotal is reported somewhere. Remove it or actually surface it.

- [x] **OCR-297** `internal/tools/context.go:295` _bug_
  `frontmatterDescription` hand-rolls a YAML subset, so it can mis-measure the description and produce wrong `description_warn` flags: block-scalar indicators other than `|`/`>`/`|-`/`>-` (e.g. `|2-`, `>-2`) are returned as literal text; the inline-`#` stripping is skipped when the value is quoted, so `description: "abc" # note` keeps the comment in the length; and `strings.Trim(value, "\"'")` strips unbalanced/escaped quotes. Prefer a real frontmatter/YAML parser, or normalize these forms explicitly.

- [x] **OCR-298** `internal/tools/data.go:69` _bug_
  `readCSVTable` mutates the first record's header slice in place (`headers[0] = ...`). `headers` aliases `records[0]` and `rows` aliases the same `records` backing array, which is fine here, but the BOM-strip only handles the first cell. More importantly, the ragged-row warning tells users short rows are padded, yet nothing pads them — downstream consumers that index by header position get an empty cell via the `idx < len(row)` guard, but the message is misleading. Either pad rows to header width or reword the warning.

- [x] **OCR-299** `internal/tools/deps.go:54` _maintainability_
  The helper accepts a nil `props` and would then emit `{"type":"object","properties":null}`; `properties` must be a JSON Schema object, and strict MCP clients/validators reject `null`. Every current caller passes a map literal (empty `map[string]any{}` when a tool takes no arguments), so this is latent, but normalizing nil keeps the schema valid for future callers. A guard that every `required` name exists in `props` would likewise catch schema/name drift early.

- [x] **OCR-300** `internal/tools/deps.go:115` _bug_
  Argument normalization silently turns unusable input into a default/absent value: `argInt` returns `def` for out-of-range values (indistinguishable from an omitted argument) and truncates fractions; `argStringSlice` drops non-string elements and collapses to `nil` if all are non-string, which callers treat as 'argument absent'; `resources.record` iterates only known `resources.Defs()` ids, so unknown/misspelled resource keys are dropped while a recognized key can still make `changed` true. Reject or warn on invalid/unknown arguments so callers do not operate on an unintended set.

- [x] **OCR-301** `internal/tools/deps.go:165` _maintainability_
  `argMap` hands back the caller's own map. Handlers store the result into persisted state (`plan.MandatoryScenario`) or pass it into `Router.Recommend(...)`, so any later in-place mutation of the returned map also mutates the request argument map and anything that aliases it. Returning a shallow copy (or documenting read-only ownership) avoids the aliasing surprise.

- [x] **OCR-302** `internal/tools/documents.go:297` _maintainability_
  This `max_chars` clamping block is duplicated verbatim in `documents.extract` and `documents.get`, but with a different statement order (extract clamps `>10_000_000` before the `<=0` fallback; get does the reverse), which makes the two handlers easy to diverge on a future edit. Also, the trailing `if maxChars <= 0 { maxChars = 60000 }` is unreachable: `config.Manager` always yields `Tools.MaxTextChars > 0` (default 60000, and non-positive values are ignored on load), so after the first fallback the value can never be <= 0. Extract this into a shared helper (e.g. `clampMaxChars(args, cfg)`) and drop the dead branch.

- [x] **OCR-303** `internal/tools/evaluation.go:369` _bug_
  Both `int(...)` conversions accept any finite float64 (up to ~1.8e308) even though Go's conversion of an out-of-range float to int is implementation-dependent (e.g. MinInt64 on amd64/arm64): the votes guard lets `1e300` pass as integral and reach `int(fv)`, and the xlsx date/elapsed serial lets a crafted numeric cell reach `int(math.Round(serial * 86400))`. Add explicit finiteness/range validation before converting (bound votes to a sane integer range; reject/clamp Excel serials outside the valid date range).

- [x] **OCR-304** `internal/tools/evaluation.go:491` _maintainability_
  `InvalidReport.Index` is populated from two independent index spaces: the inline-JSON loop (`for i, item := range raw`) and this file loop (`for i, rel := ...`). Both entries land in the same `DroppedInvalid` slice after the caller prepends `loadInvalids`, so receivers cannot tell whether an index refers to the inline array or the file list, and indices can collide without any discriminator. Either namespace the inline/file indices (e.g. an offset or a `source` field) or document and normalize them after merging.

- [x] **OCR-305** `internal/tools/evaluation.go:618` _bug_
  Truncating to 64 chars without any uniqueness suffix means two distinct values that share the first 64 characters (e.g. long hashes/ids differing only in the tail) map to the same directory segment, silently overwriting persisted aggregates/rankings. Preserve uniqueness by appending a short hash of the full value instead of a plain prefix.

- [x] **OCR-306** `internal/tools/files.go:118` _performance_
  `len([]rune(string(b)))` allocates a full rune slice (up to ~8M runes ≈ 32MB) just to count characters, on top of the `string(b)` copies already made for the line count. Use `utf8.RuneCount(b)` (add `unicode/utf8` to the imports) to count without the extra allocation; consider computing `s := string(b)` once and reusing it for the line count.

- [x] **OCR-307** `internal/tools/knowledge.go:113` _performance_
  Both `knowledge.index` and the `security` artifact fallback call `Store.ReadArtifact`, which materialises the entire stored artifact into memory (`os.ReadFile` / unbounded remote `objects.Get`) before any cap or scan limit is applied. For `security` this re-opens the OOM vector `readCapped` was added to close, despite the comment saying the read is capped; for `knowledge` it makes indexing an unbounded-memory operation. Expose/use a size-limited read (or rehydrate and then apply `readCapped`) instead of `ReadArtifact`.

- [x] **OCR-308** `internal/tools/knowledge.go:302` _maintainability_
  `sort.Slice` compares only `Score`, so equal-scored hits have no defined relative order (and the subsequent `hits[:limit]` truncation may keep/drop different hits for what should be the same result). Sparse TF-IDF scores produce many exact ties, so add stable tiebreakers (e.g. IndexID then ChunkID) to make the result deterministic and reproducible.

- [x] **OCR-309** `internal/tools/plan.go:166` _bug_
  `plans.Load` keeps the `slug` stored *inside* the file (it only falls back to the requested slug when that field is empty), and `plans.Save` derives the target path from `plan.Slug` (plans.go:107-118, 213-231). So if a plan file's `slug` field and its filename stem diverge (renamed/hand-copied files), this update is written to a different file than the one just read, creating or clobbering an unrelated plan. Force the requested slug back onto the loaded plan before saving.

- [x] **OCR-310** `internal/tools/timeseries.go:251` _performance_
  For every point after the first, the whole rolling window is copied into a new slice and `mean`/`stddev` are recomputed from scratch, giving O(n*window) work. `window` is user-controlled up to 10000, so a large table combined with a large window can make this tool very slow. Consider maintaining rolling sums (or prefix sums) and computing the mean/variance incrementally.

- [x] **OCR-311** `internal/tools/timeseries.go:319` _maintainability_
  Both helpers lack the safe edge-case guard their siblings have: `minOf`/`maxOf` index `values[0]` without an empty check (they will panic if reused on an empty slice), and `snippet` has no non-positive `limit` guard (`r[:limit]` panics for `limit < 0`), unlike `truncateRunes`. Add length/limit checks so these generic helpers fail safe.

- [x] **OCR-312** `internal/tools/util.go:233` _bug_
  `stddev` accumulates squared deviations in a naive `sum`, so for large-magnitude input (|v| above ~1e154) `d*d` overflows to +Inf and the result becomes Inf; `mean`'s naive `sum += v` likewise overflows for values near `math.MaxFloat64`. `parseNumber` accepts such finite values (it only rejects NaN/Inf), and callers serialize the result (`data.go` min/max/mean/std, `timeseries.go` `"std": round2(stddev(values))`) with `json.MarshalIndent`, which fails on non-finite floats and destroys the whole tool result. Consider a numerically stable accumulation (e.g. Welford for the variance) or guarding non-finite results.

- [x] **OCR-313** `internal/tools/xlsx.go:444` _bug_
  The guard rejects any read once `remaining` reaches 0, so an entry whose decompressed size is exactly the configured limit fails with "exceeds the configured size limit" instead of ending with `io.EOF` (the XML decoder reads past the root element to confirm EOF). Probe one extra byte to distinguish a clean EOF from a real overrun.

- [x] **OCR-314** `overlay.json:3` _maintainability_
  Spelling inconsistency in the path key: the source uses `Dokument`, while the corresponding target path and the standard Windows user folder are `Documents`. Since these keys are exact-match paths used for substitution, a misspelled segment such as `Dokument` would make the `Replace` mapping silently fail to match the real source file. All five keys share this segment, so please verify the actual folder name (e.g. `Dokument` vs `Documents`) before relying on this overlay.

- [x] **OCR-315** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:10` _bug_
  `readlink` without `-f` and without a loop resolves exactly one hop. If the launcher is reached through a chain (link -> link -> real file) or the link target lives in another symlinked directory, `SELF` ends up as an intermediate path, so `SCRIPT_DIR`/`REPO_ROOT` (`../../..`) are computed from the wrong tree and the script can exec the wrong `bin/hackathonkit-mcp` or report a bogus missing-compose path (residual of the S3-L128 class of bug, now only partially covered). Note also that line 16 uses plain `pwd`; a symlinked parent directory keeps the logical path, so the three `..` hops stay on the literal path even though `bin/` resolves elsewhere. Loop until `[ -L ]` is false (or use `readlink -f`/`realpath` when available) and `pwd -P` for the physical directory, or fail clearly when the derived root does not contain `docker-compose.yml`.

- [x] **OCR-316** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:21` _maintainability_
  `--repo-root "$REPO_ROOT"` is prepended before `"$@"` with no validation, so a caller-supplied `--repo-root` is accepted silently and wins (the Go flag package keeps the last occurrence). That is at best a surprising precedence and at worst lets an argument override the derived root. It is also asymmetric with the docker branch below, which never injects `--repo-root`, so the two transports can end up operating on different roots. Validate/reject a duplicate flag or document the precedence explicitly.

- [x] **OCR-317** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:46` _test_
  No automated test exercises this launcher: `scripts/smoke.test.sh`/`smoke.sh` cover smoke.sh's transport selection, and `hooks.test.js` covers hooks.js, but nothing asserts the launcher's own behavior. The paths added here (local binary vs docker fallback, the non-executable guard, `"$@"` passthrough and the exit-status propagation) are therefore unverified and can regress unnoticed. A `.test.sh` with stubbed `docker`/binary (same pattern as smoke.test.sh) would pin the branch selection and the propagated status.

- [x] **OCR-318** `plugins/hackathonkit/scripts/hooks.js:150` _bug_
  The stdin payload is truncated to 1MB before parsing. A larger payload is cut mid-JSON, `parsePayload` then returns `{}`, and the session id (and tool name) are silently lost, which degrades to wrong/unknown session attribution in the `tool-use` path. At minimum log/flag truncation, or parse incrementally / raise the limit so valid payloads are not mangled.

- [x] **OCR-319** `plugins/hackathonkit/scripts/hooks.js:303` _style_
  Formatting is inconsistent here (statement on the same line as the function opening brace), which is easy to miss in review and diverges from the rest of the file. Also note the comment above `readActive()` claims the tail is the "most recently started" session, but the sort key is file mtime; if that ever diverges from `started_at` the comment is misleading.

- [x] **OCR-320** `plugins/hackathonkit/scripts/hooks.js:328` _security_
  `safeBinary` stats the candidate and then a later `execFileSync` re-resolves the path, leaving a TOCTOU window in which a group/world-writable or foreign-owned file can be swapped in between the check and the exec. Consider re-checking immediately before execution (or opening the file and exec'ing via the held fd) so the validated inode is the one executed.

- [x] **OCR-321** `scripts/coverage-check.sh:34` _bug_
  The anchor `(^|[[:space:]])` is not a real token boundary: whitespace inside a quoted GOFLAGS value also matches, so `GOFLAGS='-ldflags="-X a -buildvcs=b"'` has `-buildvcs=b` silently stripped out of the `-ldflags` value, corrupting the build flags. The bare `-buildvcs` form is likewise not removed and ends up duplicated with the appended flag. Since Go flag parsing lets the last occurrence win, you can drop the fragile string surgery entirely and just append the disabling flag.

- [x] **OCR-322** `scripts/coverage-check.sh:58` _performance_
  `$COVER_LOG` is discarded here and then, on the below-target branch, `go tool cover -func="$PROFILE"` is executed a second time to build the diagnostics (the same double invocation flagged in the backlog). Keep the file until after the comparison and feed it to `sort`, so the (potentially large) profile is only processed once.

- [x] **OCR-323** `scripts/coverage-check.sh:64` _bug_
  Correction to the previous suggestion: the multi-dot guard should be expressed purely with globs. `*.*.*` matches any value containing two or more literal dots, so combine it with the existing character-class arm (the at-least-one-digit requirement is already implicit for values produced by `go tool cover`, and can be added with a separate `*[0-9]*` check mirroring the TARGET validation).

- [x] **OCR-324** `scripts/docker-check.sh:25` _documentation_
  The header comment documents only exit codes 0 (ok), 1 (failed) and 2 (docker unavailable), but the argument parser also exits 64 for an unknown flag; `--fast` is likewise accepted yet absent from the usage string. Update the header/usage so callers (and CI) can interpret every exit code and flag the script can produce.

- [x] **OCR-325** `scripts/doctor.ps1:45` _maintainability_
  This check hard-codes `$true`, so it can never fail and its result is meaningless as a "check" (the `Check` helper exists to detect failures). Either turn it into an informational `Write-Host`/`[info]` line like the telegram postgres entries, or pass `($null -ne $go)` and adjust the detail to state that Go is optional.

- [x] **OCR-326** `scripts/doctor.ps1:117` _maintainability_
  The `-ne "unknown"` comparison can never be true: `$stateVersion` is only ever assigned from `$state.version`, `""`, or `"unreadable"`, so `"unknown"` is never produced. Dead condition that obscures intent — remove it or make the sentinel consistent with the values actually assigned.

- [x] **OCR-327** `scripts/doctor.sh:168` _maintainability_
  The version is scraped with a hand-rolled `sed` pattern that only tolerates spaces after the colon (not tabs) and silently yields an empty string (`[ok] install state version `) on any JSON formatting drift, hiding a corrupt/unreadable install state behind a passing check. Consider validating the extracted value (or using `jq`/`python3` like the rest of the script) and failing the check when the version cannot be parsed.

- [x] **OCR-328** `scripts/doctor.sh:234` _maintainability_
  `smoke.sh` (and `plugin-check.sh` above) are executed with no timeout and only the *last* output line is surfaced on failure, so a hanging child blocks doctor indefinitely and a multi-line/non-final-line root cause is discarded, making failures hard to diagnose. Consider bounding the invocation (e.g. `timeout`) and reporting more than `tail -n 1` (or pointing the user at the full log).

- [x] **OCR-329** `scripts/e2e-fly.ps1:16` _maintainability_
  `2>$null` on the MCP invocation discards stderr entirely, which hides the binary's own error output and makes failures hard to diagnose. At minimum capture stderr (or only suppress it when a debug/verbose flag is off) so failing checks can show the underlying cause.

- [x] **OCR-330** `scripts/e2e-fly.ps1:81` _test_
  Only the last 6 log lines are read (`-Tail 6`), so if additional events (e.g. from prior runs or other tools) push the `recommend`/`feedback` entries out of that window, the checks for neuron events fail spuriously. Consider reading more lines or filtering by a unique task marker to make the assertion robust.

- [x] **OCR-331** `scripts/env-set.test.sh:13` _maintainability_
  `check` reads `$3` unconditionally, while several call sites pass only two arguments (e.g. "append keeps existing keys", "literal key is replaced"). This happens to work because the script does not set `-u`, but it is fragile: enabling `set -u` later, or copying the helper into a stricter script, would break. Default the detail parameter explicitly.

- [x] **OCR-332** `scripts/env-set.test.sh:64` _maintainability_
  Several helper invocations are not guarded, unlike the ones in the earlier sections. Under `set -e` a failure here aborts the whole suite with the helper's exit status and no `[fail]` line, making the cause hard to diagnose (and skipping the remaining assertions). Make the guard consistent across all invocations.

- [x] **OCR-333** `scripts/env-set.test.sh:74` _test_
  The comment states "a single trailing newline is a terminator", but the helper reads stdin via `VALUE=$(cat)`, so command substitution strips *all* trailing newlines. As written the test only covers exactly one trailing newline; multi-newline input (where semantics diverge from the documented behavior) is untested. Add a case such as `printf 'v\n\n'` and assert the actual expected result so the intended contract is pinned down.

- [x] **OCR-334** `scripts/env-set.test.sh:79` _test_
  Coverage gap: the quoting path in the helper (`enc`) triggers on space, tab, `#`, `"`, `'`, `$`, backtick, `\`, CR and LF, but only space/backslash/newline/`$` are exercised here. Boundary values such as an empty value, a value with a leading/trailing space, one containing `#`, and single quotes are not tested, so regressions in those branches would go unnoticed. Consider adding these cases.

- [x] **OCR-335** `scripts/gen-mcp-manifest.ps1:27` _bug_
  `$null -eq $template` only catches a zero-byte template, and even that is version dependent (`Get-Content -Raw` returns `$null` on Windows PowerShell 5.1 but `''` on PowerShell 7). A whitespace-only template falls through and is only rejected indirectly by the JSON validation. Check for empty/whitespace explicitly.

- [x] **OCR-336** `scripts/install-prompts.ps1:14` _maintainability_
  Diagnostics/failures are written with `Write-Host`, which targets the host/information stream rather than stderr (the shell counterparts redirect errors to `>&2`). Callers and CI logs classify them as ordinary output. Emit failure messages via `Write-Error` / `[Console]::Error.WriteLine($Message)` (keep the explicit exit) in both `install-prompts.ps1` and `gen-mcp-manifest.ps1`.

- [x] **OCR-337** `scripts/install-prompts.sh:12` _bug_
  If `HOME` is unset/empty (e.g. `env -i sh scripts/install-prompts.sh`), this silently resolves to `/.codex/prompts`; the install then creates files in the filesystem root (succeeding when run as root) instead of failing fast. Guard against an unset `HOME` (or empty `CODEX_HOME`) before computing `DEST_DIR`.

- [x] **OCR-338** `scripts/install-prompts.sh:39` _maintainability_
  The success message is printed unconditionally: it claims prompts were removed even when nothing matched, when `$DEST_DIR` does not exist, or when every file was skipped because it was modified. Additionally, destination prompt files that no longer exist in the source (renamed/removed commands) are never cleaned up and remain installed forever. Track and report the actual number of removals (and consider deleting stale non-source `.md` files that are byte-identical to a previously shipped prompt).

- [x] **OCR-339** `scripts/plugin-check.ps1:1` _bug_
  Two robustness gaps around the job itself: `Start-Job` can return `$null` (e.g. when the job subsystem is unavailable), in which case `Wait-Job $null` emits a non-terminating error under the `Continue` preference and the script falls into the timeout branch, printing the misleading "timed out after 120s" verdict. Also, `$ErrorActionPreference = "Continue"` combined with the unchecked `[System.IO.Path]::GetFullPath()` calls above means a malformed `source.path` leaves `$cmdFull`/`$sourceFull` `$null` and the subsequent `.StartsWith()` call cascades. Guard the job object after `Start-Job` and validate the resolved paths before using them.

- [x] **OCR-340** `scripts/plugin-check.ps1:20` _maintainability_
  The shape assumptions here are not validated. An empty `installed` array is falsy, so it produces the misleading message "unexpected ... shape (no installed array)" instead of "plugin not installed"; and if the CLI renames the field or nests `source.path` differently, the script silently falls through to "hackathonkit plugin is not installed" / "has no source.path" and tells the user to re-run setup, masking a real parser/shape mismatch. Validate explicitly (e.g. `$parsed.PSObject.Properties.Name -contains 'installed'` and an array check) and distinguish "shape mismatch" from "not installed".

- [x] **OCR-341** `scripts/plugin-check.sh:17` _maintainability_
  The EXIT trap is installed while `ERR_FILE` is still unset: if `mktemp` for ERR_FILE fails (or an interrupt fires between the two `mktemp` calls), cleanup runs `rm -f "$JSON_FILE" ""`. This currently relies on `rm -f` silently tolerating an empty operand, which is implementation-dependent. Initialize `ERR_FILE=""` (or assign it before installing the traps) so cleanup is unambiguous.

- [x] **OCR-342** `scripts/repair.ps1:20` _bug_
  In `repair.ps1`, `$global:LASTEXITCODE` is not reset to 0 before the `install-prompts.ps1 install` call or before `docker info`, unlike the other helper invocations. If the child fails to launch, throws before reaching its `exit`, or resolves to a function/alias wrapper that does not set an exit code, a stale `0` is read and the failure is silently reported as success (the docker branch then wrongly selects the docker runtime). Reset `$global:LASTEXITCODE = 0` before each call (or base the check on `$?`/script existence) and use `Get-Command -CommandType Application` for tool lookups.

- [x] **OCR-343** `scripts/repair.ps1:57` _bug_
  Several `Test-Path`/`Get-ChildItem` calls omit `-LiteralPath` (e.g. `$bin`/`$demo`/`$ctl` in `repair.ps1`, and the adapter-file probe plus the final `Get-ChildItem -Directory $ClaudeSkills` in `sync-harness.ps1`), unlike the rest of each file. `-Path` interprets wildcard characters `[`/`]`/`*`/`?`, so a repository root containing them can match the wrong path or fail. Use `-LiteralPath` consistently.

- [x] **OCR-344** `scripts/repair.ps1:61` _maintainability_
  Build outputs and package paths are relative (`bin/...`, `./cmd/...`), so they silently depend on the earlier `Set-Location -LiteralPath $root` having succeeded (it is called without `-ErrorAction Stop`). If the working directory is not what you expect, artifacts can be written outside the repo root. Prefer root-qualified paths, e.g. `go build -o (Join-Path $root 'bin\hackathonkit-mcp.exe') ./cmd/hackathonkit-mcp`, and only build into `bin/` after confirming the directory creation succeeded.

- [x] **OCR-345** `scripts/setup-resources.sh:34` _bug_
  `read` with the default IFS strips leading/trailing blanks from the line, so a secret that legitimately starts or ends with a space/tab (e.g. a password or a DSN fragment) is silently corrupted before it is handed to `set_key`. Use `IFS=` to preserve the value verbatim.

- [x] **OCR-346** `scripts/setup-resources.sh:54` _bug_
  Only INT and TERM are trapped. If the shell is terminated by an untrapped signal while `read_secret` has the terminal in `-echo` (SIGHUP when the terminal window closes, SIGQUIT via Ctrl-\), the EXIT trap does not run and the user is left with echo disabled. Trap HUP and QUIT as well.

- [x] **OCR-347** `scripts/setup.sh:94` _maintainability_
  The `rm -rf` loop is unguarded and silent: it deletes `$dir/$skill` for every name in the hardcoded `EXPLICIT_ONLY_SKILLS` list (which is duplicated again in the echo below), so a renamed/removed skill turns the intent into a silent no-op and stale mirrors are never detected. A mis-resolved `$dir` (e.g. an empty `$ROOT` or a symlinked `.agents/skills`) would also allow deletion outside the repository, and any `rm` failure is swallowed by `set -e`'s exit without a message. Check the directory exists, keep the deletion contained to the repo, and report failures/missing entries so drift is visible.

- [x] **OCR-348** `scripts/smoke.ps1:16` _maintainability_
  The docker/compose branch is only reached when `bin\hackathonkit-mcp.exe` is absent. A stale or broken local binary therefore silently bypasses the container smoke path, hiding docker/compose regressions. Consider an explicit switch (e.g. `-Runtime local|docker|auto`) and/or verifying the binary's freshness.

- [x] **OCR-349** `scripts/smoke.ps1:83` _bug_
  `docker compose run --rm` creates a one-off container whose name/labels differ from the service container, and `docker compose kill hackathonkit-mcp` may not match the timed-out one-off run container, leaving an orphaned server process behind. Prefer killing by project/service with an explicit `--project-name`, or capture the container id (`docker compose run -d` / `docker ps --filter`) and `docker rm -f` it. Note also that after `Kill()` the redirected temp files may still be locked on Windows, so the `Remove-Item ... -ErrorAction SilentlyContinue` in `finally` can leak them.

- [x] **OCR-350** `scripts/smoke.sh:67` _maintainability_
  Auto mode silently switches the runtime based on a `find | head` staleness probe. `find` output order is not deterministic, so `NEWEST` is an arbitrary newer file (the warning may name a different file each run), and a single newer .go file is treated as "the binary is stale" — masking a broken/not-rebuilt local binary and running the test against different code than the developer expects. Consider making the transport explicit/overridable (e.g. honour a SKIP_STALE env var or emit a clear, deterministic decision) and sort the candidate list before picking the newest.

- [x] **OCR-351** `scripts/smoke.test.sh:111` _test_
  This case is documented as "--compose without docker fails loudly", but `$TMP/bin` (which still contains the fake `docker` stub) is on `PATH`, so the test only verifies that a bogus `HACKATHONKIT_SMOKE_DOCKER` name is rejected — it never exercises the "docker CLI absent" situation it claims to cover. Drop `$TMP/bin` from `PATH` (or remove the stub) for this invocation, or rename/reword the test so the intent matches what is asserted.

- [x] **OCR-352** `scripts/sync-harness.ps1:77` _maintainability_
  `$ClaudeOld` is only assigned inside the `try`, but it is dereferenced unconditionally in `finally`. This relies on loose variable semantics (it will throw under `Set-StrictMode -Version Latest`, which a user profile could enable) and the deletion logic duplicates the identical check already performed inside the `try`. Initialise `$ClaudeOld = $null` before the `try` and drop the redundant in-try cleanup so the finally block is the single owner of backup deletion.

- [x] **OCR-353** `scripts/sync-harness.ps1:122` _maintainability_
  The `$Missing` counter is reused for JSON-parse failures, which are a different failure category than "missing adapter file". `sync-harness.sh` deliberately keeps two separate counters (`MISSING` / `INVALID`) and prints a combined summary. Reusing one counter conflates the two categories and, because of the `exit 1` after the first loop, the intended combined summary is never produced. Use a dedicated `$Invalid` counter (and report both).

- [x] **OCR-354** `scripts/sync-harness.sh:77` _bug_
  The INT/TERM handlers call `finish_harness` and then `exit`, which fires the EXIT trap and runs `finish_harness` a second time. Besides duplicated diagnostics it re-executes the restore/`rm -rf` logic on state that the first invocation may have already modified. Let the signal handlers just exit (or re-raise the signal) and leave cleanup to the EXIT trap.

- [x] **OCR-355** `scripts/sync-harness.sh:134` _maintainability_
  `ADAPTERS` verifies the plugin-side sources (including `plugins/hackathonkit/commands/*.md`) but not the mirrors this script just wrote into `.opencode/commands` and `.claude/commands`. A silent failure of the mirroring loop above would still be reported as "adapters ready", and only `CMDS_COUNT` (a bare count) covers it. Add the mirrored command paths (or a source/mirror count comparison) so the verification phase actually covers what was generated.

- [x] **OCR-356** `scripts/sync-skills.sh:44` _bug_
  Content verification is optional: on minimal systems without `diff` the mirror is published based only on directory/file/`SKILL.md` counts, so a truncated or corrupted file (same entry count) passes as a valid mirror. `diff -r` also does not compare file modes or symlink targets. Either require `diff` (fail with a clear diagnostic when it is missing) or fall back to a portable content check (sorted file list compared with `cmp`, or checksums) before swapping.

- [x] **OCR-357** `scripts/uninstall.ps1:28` _bug_
  `Test-SameTree` only compares file path+hash sets, so two directories that differ solely in empty subdirectories (or two empty directories) compare equal. If the shipped skill happens to consist only of empty directories, a user's own empty skill sharing that name would be treated as unmodified and deleted. Consider comparing the directory structure (e.g. include relative directory entries) rather than only files.

- [x] **OCR-358** `scripts/uninstall.ps1:88` _bug_
  `Get-ChildItem -Directory` is called without `-Force`, so hidden shipped skill directories would never be enumerated or removed, while `Test-SameTree` itself uses `-Force` when hashing trees. This inconsistency can leave installed skills behind; add `-Force` for parity.

- [x] **OCR-359** `scripts/uninstall.sh:60` _maintainability_
  `codex` availability is probed twice with two separate `if` blocks. Consolidate into a single `command -v` check with an `else` branch to avoid a redundant lookup and the risk of the two branches drifting apart.

---

# OCR Backlog - Pass 2 (post-sweep full scan)

Source: `ocr scan` after the v0.16.86 sweep. New findings on the changed code.

All pass-2 criticals/highs were fixed in v0.16.87 (except the documented `scripts/redact.sh` false positive). Remaining medium/low below.

## MEDIUM (pass 2)

- [ ] **P2-001** `.codex/agents/evaluator.toml:24` _bug_
  The self-validation list is narrower than what `eval.aggregate` actually enforces. `ValidateJudgeReport` (internal/evaluation/evaluation.go) additionally rejects reports missing a score for any of the five criteria, reports containing unknown criteria, deductions with negative/non-finite points, non-finite `total`, and a `snapshot_sha` that does not match the bundle. Because the agent is told only to check maxima, total, evidence presence and deduction criterion/reason, an otherwise plausible report can be silently dropped into `dropped_invalid`, driving the run to `inconclusive` for a reason the instructions never surfaced. Align this list with the validator or point at it as the authoritative check.

- [ ] **P2-002** `.codex/agents/evaluator.toml:26` _security_
  This rule tells the agent not to copy secrets, but the fields it emits (`evidence[].finding`/`source`, `questions[]`, `top_improvements[]`) are precisely where untrusted artifact text gets quoted, and unlike the reviewer/researcher agent configs this file gives no safe fallback. Add an explicit redaction contract with the same coverage (API keys, tokens, passwords, connection strings, private keys, session cookies, Authorization/bearer headers, personal data) and the "summarise and note the omission" fallback so a quoted passage never becomes a secret-leak channel into the persisted report.

- [ ] **P2-003** `.codex/agents/researcher.toml:27` _bug_
  Coupling bug: "If any are missing ... persist nothing" disables persistence whenever *any* listed tool is absent — including read-only helpers such as `files.describe`, `learning.recall` or `knowledge.search`. That contradicts the later contract line ("the artifact id when `reports.create` persisted the brief") and silently drops the `theme-research-<slug>` artifact the handoff to `$build-planner` depends on. Scope the no-persist rule to when the persistence tools (`reports.create`/`learning.record`) are missing and mirror the skill's fallback matrix.

- [ ] **P2-004** `.codex/agents/reviewer.toml:21` _maintainability_
  The read-only rule is duplicated across three bullets (lines 21, 22, 26) and the copies disagree: line 21 says read-only "still permits read commands", line 22 says "You are read-only: do not run tests or builds", and line 26 says "do not run commands or fetches". This is not just redundancy (drift risk if only one copy is edited later) — it conflates the physical sandbox capability (which still allows process execution/network) with a behavioral instruction, which is exactly the confusion that misleads the model about what is actually prevented. Consolidate into one canonical statement of the instruction, separate from any description of sandbox capability.

- [ ] **P2-005** `.codex/agents/reviewer.toml:23` _bug_
  "Check evidence: tests actually ran" is unsatisfiable for a read-only agent: it cannot re-run anything, so it cannot distinguish a genuine run from a fabricated or stale log. The preceding bullet tells it to downgrade what it cannot confirm to "unverified - read-only", but this bullet reasserts confidence in test output as if the agent could authenticate it. Define what counts as trustworthy evidence (CI-produced artifacts with ids/commit hashes) and require self-reported logs to be marked unverified.

- [ ] **P2-006** `.codex/agents/reviewer.toml:26` _security_
  Merged finding for reviewer.toml and evaluator.toml: the sandbox comments concede that `sandbox_mode = 'read-only'` does not deny file reads, process execution or outbound network, and `approval_policy = 'never'` only prevents escaping the sandbox. Containment of untrusted artifacts therefore rests solely on prompt-level instructions (e.g. 'never run commands, builds or network fetches'), the exact path a malicious artifact would target. The residual risk is currently documented only in a TOML/prompt comment, so operators may over-trust the sandbox boundary. Prefer enforcing the boundary via an agent-level network/tool restriction where the Codex schema exposes one (the global `web_search = 'disabled'` covers only search, not shell egress); otherwise treat this as an accepted, monitored risk and surface it in operator-facing docs, not only in instructions to the model.

- [ ] **P2-007** `.codex/agents/reviewer.toml:27` _maintainability_
  The output contract claims to be "checkable", but it has gaps/ambiguities that make it non-conforming for real findings: (a) `line` is required for every finding even though file-agnostic/architectural findings have no single line; (b) the shape of `unverified[]` entries is undefined (no field list), so the very list this agent is told to demand of others is itself unspecified; (c) the allowed `severity` values are not enumerated. Because nothing validates this file (scripts/codex-config.test.sh only parses config.toml), the structured contract is prose-only and downstream consumers cannot rely on it. Specify `line` as nullable, list the `unverified[]` fields, and enumerate severities.

- [ ] **P2-008** `.codex/config.toml:21` _documentation_
  "which Codex applies with higher precedence" is a load-bearing assertion: the entire portability mitigation (absolute launcher path written to the user config) only works if user-level config really overrides this project-level `[mcp_servers.hackathonkit]` table. If Codex layers project config above user config (a common pattern), the relative `args` here would shadow the generated absolute path and the override would be silently ineffective. Please confirm the precedence direction against the Codex schema/docs and, if project config can win, adjust the committed file (e.g. omit the local override when the user config is present) and document the guarantee.

- [ ] **P2-009** `.codex/config.toml:37` _security_
  The launcher path is resolved against Codex's process working directory, not against this config file's location, so the executed artifact is chosen by cwd. Two consequences: (1) starting Codex from any subdirectory (or a temp cwd, as `scripts/plugin-check.sh` does) silently fails to register the server rather than surfacing an error; (2) if a same-named script exists under the cwd, that untrusted file is what gets executed as an MCP server. The comments document the portability angle but not the trust/execution implication. Prefer keeping a single source of truth for an absolute path (the user-config override written by `scripts/gen-codex-config.sh`), or resolve the path relative to the config file, so launch cannot be redirected by cwd.

- [ ] **P2-010** `.dockerignore:9` _performance_
  This block only excludes gitignored build output under `bin`, `data`, `logs`, `evals`. Other gitignored build/local artifacts are still shipped to the daemon via the Dockerfile's `COPY . .`:
  - root-level binaries `/hackathonkit-mcp`, `/hackathonkit-demo-api`, `/hackathonkit-ctl`, `/hackathonkit-rehearse` (`.gitignore:2-5`)
  - `*.exe` (`.gitignore:6`)
  - the `.coverage` data file (`.gitignore:14`) — the existing `coverage`/`**/coverage` patterns only match the directory, not the `.coverage` file
  - the legacy generated `judge-bundle/` alias (`.gitignore:41`)
  
  These bloat the build context, can leak local-only artifacts and bust the `COPY . .` layer cache. Mirror the remaining `.gitignore` build-output patterns here.

- [ ] **P2-011** `.gitattributes:3` _bug_
  `eol=lf` on the global rule forces LF into the working tree for every file git treats as text, not just in the repository. Any Windows-native text format that is added later and is not in the CRLF block below will be checked out with LF, which can break tooling that expects CRLF (e.g. `.resx`, `.appconfig`/`.config`, `.appxmanifest`, `.wixproj`, `.nuspec`, `.runsettings`, `.slnf`, `.filters` for other project types). The CRLF override list is therefore safety-critical and easy to fall out of sync. Consider dropping `eol=lf` from the global rule (leaving `* text=auto`, which already normalises the repository to LF) so the working tree keeps platform-native endings, or document that this list must be kept complete.

- [ ] **P2-012** `.gitattributes:51` _bug_
  The explicit binary list is a denylist and is incomplete; every binary/archive/asset type missing here still goes through the `text=auto` NUL-byte heuristic. Combined with the global `eol=lf`, a single misdetection silently rewrites the file with LF endings (corruption), which is exactly what this block is meant to prevent. Commonly needed entries still absent: archives/compressed (`.rar`, `.xz`, `.bz2`, `.zst`, `.lz`, `.lzma`, `.nupkg`, `.deb`, `.rpm`, `.apk`, `.ipa`), disk images (`.iso`, `.dmg`, `.img`), media (`.mov`, `.avi`, `.webm`, `.mkv`, `.wmv`, `.flv`, `.aac`, `.wma`, `.opus`), images/design (`.psd`, `.psb`, `.ai`, `.tga`, `.dds`, `.exr`, `.ktx`), fonts (`.ttc`, `.otc`), and data (`.sqlite`, `.db`, `.bin`, `.dat`, `.pdb`). Given the list must be exhaustive to work, an allow-list of text extensions (or `-text` catch-all plus explicit text patterns) is more robust than enumerating binaries.

- [ ] **P2-013** `.gitignore:24` _bug_
  Both `data/*/` and `evals/*/` use a trailing slash, so they only match *directories* one level down. Consequences: (1) any generated file written directly under `data/` (e.g. `data/state.json`, `data/export.csv`) is NOT ignored, contradicting the comment "All runtime/generated state under data/ is ignored" and risking large/generated content being committed; (2) `!evals/README.md` is a no-op (a file is never excluded by a directory-only pattern), and any generated *file* at the top level of `evals/` (e.g. `evals/<kind>.json`) is silently tracked. Drop the trailing slashes (`data/*`, `evals/*`) so files are covered too; the existing `!data/input/` and `!data/fly/` negations still correctly re-include those directories and contents, and `!evals/README.md` becomes a meaningful re-inclusion.

- [ ] **P2-014** `Dockerfile:44` _security_
  This `quality` stage is the LAST stage in the file, so the default build target of a bare `docker build .` (no `--target`) is this image — which runs as root and carries the full Go toolchain plus git. That contradicts the project checklist rule "containers run as `${HACKATHONKIT_UID:-1000}:${HACKATHONKIT_GID:-1000}`, never root" and makes it easy to accidentally tag/publish the privileged CI image (or ship it as the "main" image). Consider ordering the stages so that a non-root runtime stage is last (e.g. move this `quality` stage above `hackathonkit-mcp`/`demo-api`) and requiring `--target quality` (compose already sets `target: quality`); otherwise add a prominent warning that `--target` is mandatory.

- [ ] **P2-015** `cmd/demo-api/fly_live.go:112` _bug_
  `snapshot` is built under `f.mu` but stored after the lock is released. With two publishers (the poller goroutine plus any direct caller of `refreshSnapshot`, which the doc comment advertises), the sequence G1(refresh→rev=5, unlock) → G2(refresh→rev=6, unlock) → G2.Store(6) → G1.Store(5) publishes an older snapshot last, and every subscriber then observes a monotonically-decreasing `Revision` (SSE dedup compares `snapshot.Revision == lastRevision`, so the newer update is silently lost until a later poll bumps the revision again). Move the `Store` inside the lock so build-and-publish are atomic.

- [ ] **P2-016** `cmd/demo-api/main.go:355` _bug_
  The comment above this check states that a client omitting the revision (zero) or sending a negative value must not bypass the guard, but the equality check does not enforce that: `plans.Latest`/`plans.Load` never normalize `Revision`, so a plan file with `revision: 0` (or no revision field) yields `plan.Revision == 0`, and a client that omits `revision` (also 0) passes the guard. Reject non-positive revisions explicitly, e.g. `if body.Revision < 1 || body.Revision != plan.Revision`.

- [ ] **P2-017** `cmd/fly-train/main.go:199` _bug_
  If `AddFeedback` fails part way through the replay (e.g. a transient `saveMemory` disk error), the loop returns immediately with no rollback: `fly-memory.json` (and, via the dopamine side effect, possibly `fly-brain.json`) has already been rewritten with only the events processed so far and will be loaded by the live router as if complete. Consider failing fast before mutating anything, retrying transient errors, or documenting that the index can be left partially rebuilt and should be re-run.

- [ ] **P2-018** `cmd/fly-train/main.go:273` _bug_
  A single JSONL line longer than the 4 MB scanner limit makes `scanner.Scan()` stop and `scanner.Err()` return `bufio.ErrTooLong`, which is returned as a fatal error and aborts the whole dataset/eval run. This is inconsistent with the `onSkip` contract, where malformed JSON and missing fields are skipped per line. At minimum, surface an actionable error; ideally use a `bufio.Reader`/`ReadString('\n')` loop (or a larger, configurable limit) so one oversized line is skipped like the other bad lines instead of failing the entire training run.

- [ ] **P2-019** `cmd/hackathonkit-ctl/daemon.go:86` _bug_
  `daemonStart` treats "pid file present and process alive" as readiness, but `daemonForeground` writes the pid file *before* calling `ListenAndServe`. If the daemon later fails to bind (port already in use) it still exits and cleans up the pid file, yet this loop can observe the pid as alive while the initial ingest runs and report `daemon started`. The parent therefore reports success for a daemon that never listened. Consider having the child bind the listener before writing the pid file (and serve with `server.Serve(listener)`), or otherwise signal readiness only after a successful bind.

- [ ] **P2-020** `cmd/hackathonkit-ctl/daemon.go:201` _bug_
  The graceful-shutdown result is discarded. `Server.Shutdown` returns `context.DeadlineExceeded` when in-flight handlers do not finish within 3s, but the `<-shutdownDone` wait only waits for Shutdown to *return*, not for handlers to finish. Handlers can run up to `WriteTimeout` (30s), so after this 3s timeout `daemonForeground` proceeds to the deferred `store.Close()` while handlers are still executing store queries; those requests then fail with `sql: database is closed`. Handle the error (log it and fall back to `server.Close()`), or ensure the store is only closed after handlers have drained (e.g. track in-flight requests and wait for them).

- [ ] **P2-021** `cmd/hackathonkit-ctl/main.go:248` _maintainability_
  `addWorktree` creates a `hackathonkit/<name>` branch, but no code path ever deletes it: `worktree remove` only runs `git worktree remove`, and the setup-failure rollback above only removes the directory. Failed/completed runs therefore leak branches in the repository indefinitely, and the slug collisions below then interact badly with their existence. Consider creating a detached or per-run branch and pruning it when the worktree is removed.

- [ ] **P2-022** `cmd/hackathonkit-ctl/main.go:335` _bug_
  The worktree name is derived by `slugify`, which strips every non-alphanumeric rune, so distinct tasks/slugs (e.g. "fix: auth" and "fix auth", or "Bug A" vs "bug-a") collapse to the same name. With `--worktree`, the second run then fails with "worktree already exists", and because the branch persists (see above) it can also silently reuse an unrelated branch. Add a short uniqueness suffix (e.g. a timestamp/counter) to the derived name.

- [ ] **P2-023** `cmd/hackathonkit-ctl/main.go:357` _bug_
  If `UpsertSession` fails, the worktree directory is force-removed but (1) the `hackathonkit/<name>` branch created by `addWorktree` is left behind, and (2) the removal command's error is discarded, so an orphaned worktree directory can remain with no signal to the caller. Surface the removal failure (e.g. join it into the returned error) and delete the branch as part of the rollback.

- [ ] **P2-024** `cmd/hackathonkit-ctl/main.go:371` _bug_
  The task is appended as a bare trailing argument with no `--` terminator. A task beginning with `-` (e.g. `--help`, `-x`) will be parsed by codex as a flag rather than the prompt, silently changing behavior. Insert a `--` before the task argument so it is always treated as a positional value.

- [ ] **P2-025** `cmd/hackathonkit-ctl/main.go:375` _maintainability_
  cmdRun accepts `stdout`/`stderr` writers so it can be exercised/tested in isolation, but the child process is wired directly to `os.Stdout`/`os.Stderr`, so codex output bypasses the injected contract and escapes to the real terminal. This makes the function's output untestable and fragments where output goes. Route the child's stdout/stderr through the provided writers (or `io.MultiWriter` if you still want terminal output); `os.Stdin` can stay for interactivity.

- [ ] **P2-026** `cmd/hackathonkit-ctl/proc_unix.go:38` _bug_
  `processMatchesPath` reads `/proc/<pid>/exe`, which only exists on Linux, yet this file is compiled for every non-Windows GOOS (`//go:build !windows`); on darwin/BSD the readlink fails, so for `pid != os.Getpid()` it returns false. Because `readPID` (daemon.go:246) trusts the recorded executable path as the primary identity check, a live daemon is reported as stopped: `daemonStatus` prints "stopped", `daemonStop` removes the pid file without signalling, and `daemonForeground` overwrites it and launches a second daemon. The comment in daemon.go claiming executable-path matching "proves identity on every platform" is therefore wrong. Either narrow this file to `//go:build linux` and add a darwin/BSD implementation (e.g. `proc_pidpath`/`sysctl` KERN_PROCARGS, or matching the process list), or make the check explicitly portable/fail-safe instead of silently failing on non-Linux Unix; treat "cannot verify on this platform" distinctly from "verified not ours".

- [ ] **P2-027** `cmd/hackathonkit-ctl/proc_windows.go:47` _bug_
  A hard `TerminateProcess` bypasses the daemon's graceful shutdown entirely: `daemonForeground` installs `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` and relies on its defers to drain in-flight HTTP handlers and `store.Close()` the SQLite store, but `TerminateProcess` cannot be caught, so none of that runs when the process is stopped via `daemon stop`. In-flight ingest/DB writes can be lost and the pid file is left behind. Consider an explicit cooperative shutdown request (e.g. a control endpoint or a named event) with `TerminateProcess` only as a post-timeout fallback.

- [ ] **P2-028** `cmd/hackathonkit-ctl/proc_windows.go:50` _bug_
  Check-then-kill is not atomic and can terminate an unrelated process. In `daemonStop` (cmd/hackathonkit-ctl/daemon.go), identity is validated by `readPID` -> `processAlive` + `processMatchesPath`, each of which opens and closes its own short-lived handle; the pid is then handed to `os.FindProcess`/`terminateProcess`. On Windows pids are recycled aggressively, so a process that exits between the validation and this `Kill` lets the new owner of that pid be terminated. Retain the verified handle from a single `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION|PROCESS_TERMINATE)` call, re-check the image path on that same handle, and call `windows.TerminateProcess` on it so verification and termination are atomic.

- [ ] **P2-029** `cmd/hackathonkit-ctl/tui.go:67` _bug_
  `Sessions` and `Events` share a single 5s deadline created before the sessions query runs. If `Sessions` is slow (large DB, SQLite lock contention, or a queued query because the pool is limited to one connection via `SetMaxOpenConns(1)`), it can consume most or all of the budget, so the subsequent `Events` call fails with `context.DeadlineExceeded`. That surfaces as a misleading "events unavailable" state on an otherwise successful refresh, and because the newest overlapping load is the one applied, it can even be the load that gets starved. Consider giving the events query its own timeout derived from the same parent instead of sharing one deadline across both queries.

- [ ] **P2-030** `cmd/hackathonkit-mcp/main.go:107` _security_
  Under-redaction gap: when the key and its value are separated by whitespace *plus* a stray separator (e.g. `password = hunter2`, `password : hunter2`), the marker that matches is the `<key>+space` one, `valueStart` lands on `=`/`:`, and the scan stops at the following whitespace. Only the separator is replaced, so the real secret survives in the output (result: `password [redacted] hunter2`). The same happens for the `"password":"secret"` (JSON key) rendering, where no marker matches at all. Since this routine is the only thing preventing DSN/credentials in startup logs, extend the skip logic to also consume a leading `=`/`:` (and following whitespace) before capturing the value, and consider handling a quoted key.

- [ ] **P2-031** `cmd/hackathonkit-rehearse/main.go:381` _bug_
  On timeout, `c.close()` calls `cmd.Wait()` while the reader goroutine spawned a few lines above is still blocked in `c.stdout.ReadBytes('\n')` on the StdoutPipe. `os/exec` documents that it is incorrect to call `Wait` before all reads from a `StdoutPipe` have completed, because `Wait` closes the parent's read end of the pipe; the blocked goroutine can then observe a closed/reused descriptor. Consider draining stdout from a single long-lived reader goroutine (or closing the pipe only after the reader exits) instead of calling `Wait` with an in-flight read.

- [ ] **P2-032** `internal/config/config.go:230` _maintainability_
  The same numeric fields are merged twice: `mergeFile` applies `tools.*` and `routing.*` with a `> 0` guard, then `mergeNumeric` (always reached because the same YAML parses into `filePatch`) overwrites them with pointer semantics. The `mergeFile` branches for `Tools`/`Routing` are effectively dead and the two guards diverge — e.g. a negative `fly_threshold`/`top_k` passes `mergeNumeric` while `mergeFile` would have ignored it. Consolidate numeric handling in one place (keeping the pointer patch for explicit zeros) to avoid the two paths drifting apart.

- [ ] **P2-033** `internal/config/config.go:445` _bug_
  Unlike `Routing()` and `NewManager`, `Evaluation()` does not log a non-`IsNotExist` read error: a permission/IO failure on bind-mounted `evaluation.yaml` is silently swallowed and the defaults/last cache are returned, making a broken config look like a successful load. Log the error (as done for routing) so the failure is observable.

- [ ] **P2-034** `internal/config/lock_unix.go:23` _bug_
  `syscall.Flock(..., LOCK_EX)` blocks indefinitely with no timeout or cancellation, unlike the non-unix implementation in `lock_other.go`, which bounds acquisition with a 5s deadline and returns a timeout error. `acquireLock` is reached from `Manager.SetRoutingMode`, which is invoked from the demo-api HTTP handler (`cmd/demo-api/main.go:388`) and the MCP server (`cmd/hackathonkit-mcp/main.go:207`), so a peer process that holds the lock while stuck on slow/hung filesystem I/O inside its own read-modify-write will wedge this request/worker indefinitely. Mirror the portable path by polling with LOCK_EX|LOCK_NB against a deadline (and document the behavior if blocking is intended).

- [ ] **P2-035** `internal/control/ingest.go:189` _bug_
  The comment claims a crash cannot replay already-ingested records, but the cursor is written *after* `handle` applies the record. A crash (or a `flush` failure reported below) between the apply and the cursor write leaves the cursor pointing at the previous record, so the next run replays it. The `events` table has no unique constraint, so replayed `tool_use`/`mcp_call`/`fly_feedback` records become duplicate rows. Either persist the cursor before applying (accepting possible loss instead of duplication) or correct the comment to document the actual at-least-once semantics; if duplicates are unacceptable, add a dedup key to `events`.

- [ ] **P2-036** `internal/control/ingest.go:313` _bug_
  `readFileBounded` uses `io.LimitReader`, which silently truncates a file at `maxIngestBytes` and returns no error. A decision file at or above 64 MiB therefore yields truncated bytes, `json.Unmarshal` fails, and the `continue` above skips it on every cycle — the file is never ingested and nothing is logged or returned. Detect truncation (e.g. read `max+1` and treat `len == max+1` as oversized, or `Stat` first) and surface it through the same `log.Printf`/error path as other read failures so the file is not silently dropped forever.

- [ ] **P2-037** `internal/control/risk.go:76` _bug_
  `panicNegation` only strips negations that occur in free-text values (`"no panic"`, `"0 panics"`, `panic_free`). The most common way a tool reports the absence of a panic is a quoted **key/value** (`{"panic":false}`, `{"panics":0}`, `{"panic_count":0}`), and there the first byte after `panic` is `"`, which `[\s:=]*` cannot consume, so no alternative matches and `panicWord` then matches `panic` → a benign payload is scored as a panic. Worse, `panicWord` ignores the value entirely, so `{"panic":false}` and `{"panic":true}` are classified identically. This inflates `riskPanic`/the stored reasons (violating the `[^a-z]panic`/negation contract documented above) and makes the reasons non-authoritative. Suggest handling an explicit boolean/numeric `panic`/`panics` field separately in `payloadHasPanic`, or making the pattern quote-aware (illustrative suggestion below).

- [ ] **P2-038** `internal/evaluation/evaluation.go:650` _bug_
  compareEntries treats differences within epsilon (1e-9) as equal, which is not transitive: A~B and B~C can hold while A≁C. Using it as the sort.SliceStable less predicate here, and then relying on adjacent-pair comparison to populate Unresolved, means a tie cluster can be left non-contiguous or split. ApplyCommission then re-checks only neighbouring entries to recover tier boundaries, so it can mis-merge genuinely different scores into a single commission tier (forcing an unnecessary majority vote and possibly ordering a lower-scored entry above a higher one) or split a real tier and reject a valid commission vote. Compute tie membership with a consistent tolerance-based clustering, or compare exact/rounded values so the ordering predicate is a strict weak ordering.

- [ ] **P2-039** `internal/learning/learning.go:519` _bug_
  `load` never validates the on-disk schema version: `save` stamps `fileVersion`, but any file is unmarshalled as the current format. A file written by an incompatible (e.g. newer) build is silently mis-parsed instead of being rejected, which can drop or misinterpret fields. Reject unknown versions (or migrate them) after unmarshalling.

- [ ] **P2-040** `internal/learning/learning.go:548` _bug_
  The package advertises "durable" learnings, but `save` renames the temp file into place without fsyncing it (or the containing directory). After a crash/power loss the rename may be persisted while the file's data is not, so a write that `Record` already reported as succeeded can be lost or leave a zero-length/truncated store. `tmp.Sync()` before `Close` and a directory fsync after `Rename` are needed to honor the durability claim.

- [ ] **P2-041** `internal/learning/proc_unix.go:21` _bug_
  `processAlive` collapses every non-EPERM error into "not alive". Its only caller (`Store.lockFile` in learning.go) uses a `false` result to `os.Remove` another process's lock file, so an indeterminate probe failure (e.g. a transient EINTR, or EINVAL on a platform where signal 0 is not accepted) is silently interpreted as "owner died" and the live lock is broken, letting two writers mutate the store concurrently. The contract should distinguish "definitely gone" (ESRCH) from "could not determine" and default indeterminate results to alive, matching the conservative intent of the caller's stale-lock logic. Also note `os.ErrProcessDone` (returned by `Process.Signal` for an already-reaped process) is not ESRCH.

- [ ] **P2-042** `internal/mcp/registry.go:76` _bug_
  `cloneMap`/`cloneSlice` always return non-nil containers, so a nested nil map/slice (`null` in JSON) is silently rewritten to `{}`/`[]`. Preserve nil so JSON serialization matches the registered schema.

- [ ] **P2-043** `internal/mcp/server.go:164` _bug_
  The write error for the oversize-frame response is discarded (`_ = enc.Encode(...)`), and the parse-error response below does the same, while the success path returns the encode error. If the output stream is broken (broken pipe, full disk, closed client), these error replies silently vanish and the loop keeps running; if the input then ends, `ServeStreams` returns `nil`, so the caller sees a clean shutdown even though responses were never delivered. Propagate the error consistently with the success path.

- [ ] **P2-044** `internal/mcp/server.go:266` _bug_
  When `CallTimeoutSec` exceeds `maxTimeoutSec` (24h) the condition is false and `timeout` silently stays at the hard-coded 30s default. A caller who configures, say, 30h gets a 30s per-call deadline instead, with no error or warning. Clamp to the supported maximum instead of falling back to the default (keep the seconds value bounded before the multiplication to avoid the previous overflow).

- [ ] **P2-045** `internal/metadata/metadata.go:146` _performance_
  These backfill/`SET NOT NULL` statements run unconditionally on every startup. `ALTER COLUMN ... SET NOT NULL` requires ACCESS EXCLUSIVE and validates the column, which can block all concurrent access and force a full table scan on large `routing_runs`/`eval_runs` tables; the surrounding `ADD COLUMN IF NOT EXISTS` statements also take locks even when already applied. Consider gating migrations behind a schema-version check so the destructive/blocking DDL runs only when needed.

- [ ] **P2-046** `internal/metadata/metadata.go:160` _bug_
  `isDuplicateDDL` only tolerates `42P07`/`42710`, but the comment claims it handles a concurrent creator winning the DDL race. `CREATE TABLE IF NOT EXISTS` executed concurrently in PostgreSQL can still fail with `23505` (unique_violation on `pg_type_typname_nsp_index` / `pg_class`), and `ADD COLUMN IF NOT EXISTS`/`ALTER COLUMN` races can surface as `42701` (duplicate_column). With the current filter these benign races hard-fail `openPostgres`. Add the concurrent-DDL codes to the benign set.

- [ ] **P2-047** `internal/metadata/metadata.go:193` _bug_
  `schema` and `migrations` share a single 15s `ddlCtx`. If the schema Exec consumes most/all of that budget (e.g. slow startup under load), the migrations Exec inherits a nearly/fully expired deadline and fails with `context deadline exceeded`, aborting `openPostgres` even though migrations would otherwise succeed. Give each DDL step its own timeout.

- [ ] **P2-048** `internal/metadata/metadata.go:220` _bug_
  The read lock is held across `pool.Exec`, i.e. across network/DB I/O. `Close()` takes the write lock and therefore blocks until every in-flight write finishes. If a caller passes a context without a deadline (or the DB stalls), a single slow query can block `Close()` — and shutdown — indefinitely, since the lock is released only when Exec returns. Use a bounded context for the writes (or an ownership model such as an atomic pointer/refcount) so Close cannot be pinned by a stalled query.

- [ ] **P2-049** `internal/objectstore/minio.go:26` _security_
  Any non-empty but unrecognized value (e.g. a typo like `ture`/`flase`, or `enable`) falls through to `return false`, silently disabling TLS while the static access/secret keys are still sent to the endpoint. There is no warning, so the downgrade to cleartext is invisible at runtime. For a security-relevant switch like this, fail fast on an unrecognized value (or at minimum log a warning) instead of silently defaulting to plaintext.

- [ ] **P2-050** `internal/objectstore/minio.go:55` _bug_
  The endpoint and credential fields are trimmed to defend against stray whitespace (the code comment explicitly cites a CRLF `.env`), but `s.Bucket` is not. A `MINIO_BUCKET` that picked up a trailing `\r`/space (or the default check in `SettingsFromEnv`, which only tests `bucket == ""`) will be passed verbatim to `BucketExists`/`MakeBucket`. minio-go rejects such a name, so `Open` fails and object storage is silently disabled — exactly the failure mode the trimming above was meant to prevent. Trim the bucket too (and treat a whitespace-only value as unset).

- [ ] **P2-051** `internal/openai/client.go:253` _bug_
  The `maxAudioBytes` cap is validated against the pre-copy `Stat` size, but the actual bytes written into the multipart buffer by `io.Copy` are unbounded. A file that grows between the `Stat` and the copy (or a symlinked/pseudo file under the input dir that reports size 0, which `Store.InputPath` does not reject for intermediate components) makes `io.Copy` read until EOF, so the whole file accumulates in `buf` without ever hitting the guard — a memory-exhaustion path on the MCP request side. Enforce the limit on the bytes actually copied.

- [ ] **P2-052** `internal/plans/plans.go:120` _bug_
  `Load` trusts the `slug` field stored inside the JSON file and returns it verbatim when non-empty; it is neither normalized nor checked against the requested name. Every caller that persists a loaded plan must therefore remember to force `plan.Slug = slug` back (e.g. `internal/tools/plan.go:181` does this manually), and any future caller that uses `Plan.Slug` to build a path can be redirected by a renamed/hand-edited file (the audit backlog already records this for the demo-api approval path). Reconcile the stored slug with the requested, already-sanitized name here so the library is safe regardless of caller discipline.

- [ ] **P2-053** `internal/quality/proc_unix.go:24` _bug_
  The liveness probe tests the direct child, not the process group, and the synchronization argument in the comment is incomplete:
  
  1) Over-skip: `configureProcessGroup` makes the child the group leader, but the group can outlive the leader whenever grandchildren keep running (e.g. after `cmd.Wait` reaps the child during the `WaitDelay` drain window in quality.go). At that point `Process.Signal` returns `os.ErrProcessDone`, so this early return skips the group kill and leaves alive exactly the grandchildren the function exists to terminate.
  
  2) Residual race: `killProcessGroup` is called from `cmd.Cancel`, which os/exec runs concurrently with `cmd.Wait`. There is still a TOCTOU window between this probe and `syscall.Kill(-pid, SIGKILL)` below: the child can be reaped in between, its PID/PGID recycled, and the negative-PID SIGKILL delivered to an unrelated process group. The `Signal`/`Wait` synchronization only covers the instant of the probe, not the following group kill.
  
  The leader-liveness heuristic therefore errs in both directions; consider driving termination from the group (treating only `ESRCH` as benign) and documenting the residual recycling risk (e.g. pidfd on Linux) instead of gating on the leader's liveness.

- [ ] **P2-054** `internal/quality/proc_windows.go:23` _bug_
  `killProcessGroup` is invoked from `cmd.Cancel` in `quality.go`, which os/exec can call from its context-watcher goroutine after `Process.Wait()` has already reaped the child. Once reaped, `cmd.Process.Pid` may be recycled to an unrelated process, so `taskkill /T /F /PID <oldPid>` can terminate an innocent process tree. Add a liveness probe before killing (e.g. bail out if the process is already done) so taskkill only targets a live child; for robust tree cleanup prefer a Job Object, which is referenced by handle rather than a reusable PID. Also consider `exec.CommandContext` here, since the plain `Run()` has no timeout and can block the cancellation goroutine.

- [ ] **P2-055** `internal/quality/quality.go:99` _bug_
  Status classification is gated on `runCtx.Err()` *before* the outcome derived from `err`, so the outcome of the command itself is ignored whenever the deadline/cancellation has fired. Two concrete consequences:
  
  1. If a command exits successfully but the deadline fires concurrently (or, more likely, the process already exited and `Wait` is still draining pipes for up to `WaitDelay = 10s`), `runCtx.Err()` is already `DeadlineExceeded` and the check is reported as `timeout` even though the command succeeded.
  2. The `exec.ErrWaitDelay` branch right below — whose comment explicitly says to report a cleanly-exited command as passing — is effectively unreachable in exactly that window, so its intended `ok` + note is swallowed and replaced with `timeout`. The `gofmt` output-based failure is shadowed the same way.
  
  Determine the result from `err` first (success / `ErrWaitDelay`), and only fall back to `runCtx.Err()` when the command actually failed.

- [ ] **P2-056** `internal/resources/matrix.go:92` _bug_
  `Validate` checks variant IDs, duplicates and capability references, but never validates the provider ID against the known provider set. A typo (e.g. `openai` instead of `openai_api`) loads fine; `Ready` then never matches, `providerMissing` returns "unresolved provider ...", and the capability silently degrades instead of failing fast — inconsistent with the file's own fail-fast philosophy for unknown tracks/capabilities. The package already exposes `knownProvider` for exactly this purpose. Reject non-empty provider IDs that are not defined.

- [ ] **P2-057** `internal/resources/matrix.go:98` _bug_
  `Validate` iterates `plan.Capabilities` but never rejects an empty list. A track declared with `capabilities: []` (or omitted) passes validation, then `Plan` returns a `resolved` plan with zero decisions and the tool reports success with `capabilities: 0`, silently doing no work for that track. Fail fast here like the other checks.

- [ ] **P2-058** `internal/resources/probe.go:51` _security_
  `http.DefaultClient` follows up to 10 redirects by default, and this request carries the API key as a bearer token. The base URL is an external (and optionally operator-overridden, e.g. `OPENAI_BASE_URL`) endpoint, so a 3xx response can steer the probe's GET to an arbitrary host — a blind SSRF path from a compromised/rogue provider into the internal network. Note that `net/http` only strips the `Authorization` header for *cross-host* redirects; a same-domain/subdomain redirect still re-sends the key. The repo already installs a `CheckRedirect` guard for the credential-bearing Telegram client (`internal/telegram/telegram.go`), so this probe should do the same. Define a package-level client, e.g.:
  
  ```go
  var probeClient = &http.Client{
  	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
  }
  ```

- [ ] **P2-059** `internal/resources/resources.go:135` _bug_
  The `len(ref) >= 4 && strings.Contains(label, ref)` branch matches an arbitrary 4+ character substring against each label, evaluated per track in slice order. This resolves ambiguous or unrelated references to whatever track happens to come first: e.g. `LookupTrack("forecast")` matches track 05 ("delay forecast") before 10 ("demand forecast"), and any short common word can silently select the wrong track. Prefer matching on whole words/boundaries, the slug, or a unique match, and reject an ambiguous reference instead of returning the first hit.

- [ ] **P2-060** `internal/resources/resources.go:173` _bug_
  `Save` rejects an unknown `Track` with an error, but `Load` performs no track validation at all: a hand-edited or legacy `configs/resources.json` with an invalid `track` is accepted and silently returned. That invalid value then surfaces through `View` and is fed to `routing.variant` as the default track reference, so invalid persisted state propagates without any signal. Consider mirroring the Save-side validation here (normalize valid refs to their canonical ID, and clear or reject unknown ones).

- [ ] **P2-061** `internal/resources/resources.go:202` _bug_
  `Save` treats `CodexPro` as authoritative and unconditionally overwrites `Claimed["codex_pro"]`, but `Load` (and `Statuses`) OR-merge the two fields. A caller that builds a `Profile` directly with `Claimed["codex_pro"] = true` (leaving `CodexPro` false, exactly how callers like `applyResourcesSet`'s SetAnswer path or `main_test.go` populate `Claimed`) will have the claim silently reverted to `false` on save. To keep the two representations genuinely consistent regardless of which one the caller set, mirror the OR-merge used by Load.

- [ ] **P2-062** `internal/routing/fly/embed.go:172` _bug_
  This recovery path unconditionally clears the shared `r.centroids` and deletes the on-disk cache on any dimension mismatch, without coordinating with concurrent builders/writers. A concurrent goroutine that just rebuilt valid centroids (or a `buildCentroids` that just rewrote the file via `writeFileAtomic`) can have its result nulled/deleted by this goroutine, and during a `ChainEmbedder` dimension flip (primary 1536 vs hash fallback 512) every request re-runs the full exemplar embedding batch plus an atomic disk write, i.e. rebuild thrash on the request path. Re-check that `r.centroids` is still the mismatching map under `r.mu` before nulling it, and add a short cooldown/backoff before rebuilding so repeated mismatches are not rebuilt on every request.

- [ ] **P2-063** `internal/routing/fly/embed.go:284` _bug_
  The shared single-flight build is executed with the *first caller's* context (`Recommend` passes the request context down). If that context is cancelled (e.g. a client disconnect), the embed fails, `ok` becomes false, and the code below sets `centroidFailAt = time.Now()`. That negative cache then disables the embedding signal for every other caller for 30s, and the concurrent waiters on `building` are all released with `false` even though their own contexts were still valid. One caller's cancellation should not degrade routing for unrelated requests: only populate the shared negative cache when the failure was not caused by context cancellation (e.g. guard the `centroidFailAt` assignment with `ctx.Err() == nil`), or run the build with a context that is independent of any single caller.

- [ ] **P2-064** `internal/routing/fly/events.go:35` _maintainability_
  Unlike the `brain == nil` and `r.embedder == nil` branches, the embed/scores failure paths return the empty snapshot without setting `empty["reason"]`. Callers that surface `reason` report no cause for an embedding or scoring failure, making them indistinguishable from an internally-unknown state. Set a reason for both `r.embedTask` and `brain.scores` failures for consistent diagnostics.

- [ ] **P2-065** `internal/routing/fly/mb.go:55` _bug_
  `WithBrain` writes `r.brainPath` with no synchronization, while `loadBrain` reads the very same field under the process-global `brainMu` (and `saveBrain` reads it with no lock at all). The field therefore has one guarded and two unguarded accesses. Today WithBrain is only called during construction, but nothing enforces that, so a Router reused/reconfigured after it starts serving can produce an unsynchronized read/write on `r.brainPath`. Either guard the write with the same mutex used by `loadBrain`, or document that WithBrain must not be called once the Router is shared.

- [ ] **P2-066** `internal/routing/fly/mb.go:77` _maintainability_
  Every failure mode of loading collapses to `nil` with no log or error: an unreadable file (swallowed on the `os.ReadFile` branch), invalid JSON, and a structurally invalid brain (`KC == 0`, empty `Readout`) are all treated as "no brain configured". A corrupt or truncated brain therefore silently disables all MB scoring and dopamine learning with no diagnostic. Note also that a single category whose readout length differs from `KC` makes `scores` reject the whole brain. Consider logging the distinction (missing file vs. corrupt brain) so operators can tell the two apart.

- [ ] **P2-067** `internal/routing/fly/mb.go:290` _bug_
  `brainSaveMu` is per-Router, but `brainMu` is process-global, which suggests multiple Routers in one process are expected. Two Routers configured with the same `brainPath` can each run this read-modify-persist sequence concurrently (each holds only its own `brainSaveMu`), so the last writer silently overwrites the other's learned weights and both in-memory caches diverge. If sharing a brain file across Routers is supported, a lock keyed by the resolved path (or a process-global writer lock) is needed; otherwise document the single-writer assumption.

- [ ] **P2-068** `internal/routing/fly/mb.go:383` _maintainability_
  `Train` mutates the receiver in place (`adjust` writes Readout slice elements and re-assigns `b.Readout[category]`, `Categories` is appended to, `Bias` is written) and performs no synchronization. The package relies on the documented invariant that a published brain is immutable so that `mbCategoryScores`/`ActivationSnapshot` can score without a lock; an exported, unsynchronized mutator on the same type contradicts that contract. Nowhere is it stated that `Train` is only safe on a single-owner brain. Either have `Train` operate on a copy that the caller publishes explicitly, or rename/document it to make the single-owner requirement unambiguous.

- [ ] **P2-069** `internal/routing/fly/memory.go:201` _bug_
  `saveMemory` reimplements the temp-file-plus-rename pattern but omits the directory fsync, so the rename itself is not durable: after a crash the update can vanish while the old snapshot reappears, even though this function returned success. The project already provides `internal/atomicfile.Write`, which flushes the temp file, renames, then fsyncs the containing directory and surfaces a sync failure (see `embed.go` which already uses it). Consider delegating to that helper instead of maintaining a second, subtly weaker copy. Note also that the `fly-memory-*.tmp` names here are not covered by `atomicfile`'s stale-temp reclamation, so crashed writes leak temp files indefinitely.

- [ ] **P2-070** `internal/routing/fly/memory.go:253` _bug_
  On decode failure the code unconditionally renames `memoryPath` to `memoryPath+".corrupt"`. Because `os.Rename` replaces an existing destination, a second corruption silently overwrites the previously quarantined file, destroying recoverable data that the first quarantine was meant to preserve. Guard against an existing backup (or use a unique/timestamped suffix, e.g. `.corrupt.<unixnano>`).

- [ ] **P2-071** `internal/routing/fly/router.go:324` _bug_
  MB categories are not constrained to `categoryCapabilities`: `cmd/fly-train` builds a brain from arbitrary dataset `row.Category` values (`fly.NewMBrain(... , categories, ...)`). `mbCategoryScores` can therefore return a category with no capability mapping, and this branch lets it win route selection. Such a route corresponds to no recommendation, so `Result.Route` can name an unusable category while its score is not reflected in `MaxScore`/`Recommendations` (and if other mapped categories still produce recommendations, callers like `internal/tools/system.go` report a bogus route). Treat unmapped categories as unavailable for routing.

- [ ] **P2-072** `internal/skills/validate.go:206` _bug_
  The explicit-only enforcement only runs in the `else` branch, i.e. only when `validateOpenAIYAML` returned no error at all. Any unrelated `agents/openai.yaml` problem (blank `display_name`, `short_description` over 80 chars, missing `default_prompt`, or a YAML parse error) suppresses the `policy.allow_implicit_invocation` check, so the invocation-policy defect is silently dropped from the problem list. This is the only enforcement of the explicit-only policy, so it should not be gated on unrelated field errors; validate the policy independently of the other field checks (e.g. report parse errors separately from the field-level problems) so the guard always runs.

- [ ] **P2-073** `internal/telegram/telegram.go:589` _bug_
  The closing tags emitted here are unbalanced. Every chunk appended in the main loop (line 624, `prefix+body+closers`) already closes *all* tags in the incoming stack via `scanTags`, so the tags currently held in `stack` were already closed at the end of the previous chunk. Appending another set of closing tags as its own chunk therefore produces a chunk with unmatched end tags (e.g. `</b></i>`); Telegram rejects it with a 400, so this branch does not actually make deeply-nested HTML deliverable — it only swaps an oversized chunk for an invalid one. Emit nothing and just reset `stack` (losing the formatting for the remainder) so the message stays parseable, and add a test with >4096 runes of deeply nested tags to exercise this path.

- [ ] **P2-074** `internal/tools/context.go:155` _performance_
  `skillContextItems` walks the entire skills tree and calls `os.ReadFile` on every entry, but for non-SKILL.md assets the data is only used to count newlines (bytes/tokens come from `info.Size()`). This materializes every file — including large/binary assets — in memory on each `system.context_budget` call. Cap the read (e.g. only read SKILL.md for frontmatter, and stream/limit newline counting with an `io.Reader`/`bufio` for the rest), or skip files above a size threshold.

- [ ] **P2-075** `internal/tools/context.go:176` _bug_
  `bytes.Count(data, []byte("\n")) + 1` over-counts by one for the common case of files ending in a trailing newline (e.g. "a\nb\n" is reported as 3 lines, not 2), which can incorrectly flag SKILL.md/manifest files as exceeding `skillLinesWarn`. The same formula is used again in `manifestContextItems`. Use a helper that only adds the extra line when the content does not end with a newline.

- [ ] **P2-076** `internal/tools/data.go:50` _performance_
  `ReadAll` reads and parses the entire file with no cancellation check, and `loadTable` only checks `ctx.Err()` after the whole parse has completed. For a file up to `maxTableBytes` (64 MB) this can block well past a caller's deadline/cancellation. Iterate with `r.Read()` in a loop (or use `r.ReadAll` only for small inputs) and check `ctx.Err()` periodically so the work is abandoned when the caller cancels.

- [ ] **P2-077** `internal/tools/data.go:100` _bug_
  The substring fallback only refuses when *more than one* header contains the needle. A single incidental match (e.g. requesting `id` against a table whose only header is `grid`, or `date` against `updated_at`) is accepted silently, so callers get the wrong column without any warning or error. Since `columnIndex` is used to resolve query/where/filter columns, this can return incorrect data. Consider requiring full-token equality (or at least logging a warning when a non-exact match is used) instead of binding on any unique substring.

- [ ] **P2-078** `internal/tools/data.go:216` _security_
  The byte limit is checked against the on-disk file size, but `readAnyTable` may read a *compressed* container (`.xlsx` is a zip). A file well under 64 MB can expand to a much larger sheet, and `readXLSXTable` materializes all rows before the `maxTableRows` trim is applied, so the limit does not bound peak memory. Consider streaming the sheet with an early row cap, or bounding the declared uncompressed size for archive formats.

- [ ] **P2-079** `internal/tools/data.go:405` _performance_
  This allocates a full-length `[]float64` for every numeric column (up to 32 cols × 2,000,000 rows × 8 B ≈ 512 MB retained), and the pairwise loop then allocates two more fresh slices per pair (~496 pairs) for the strong-correlation scan, giving billions of float comparisons. The column/row caps bound the worst case but not to a small value. Consider capping the rows used for correlation (sampling) and/or computing the coefficient with reusable scratch buffers instead of per-pair allocations.

- [ ] **P2-080** `internal/tools/data.go:516` _bug_
  Ordering comparisons only count a skip when the *cell* (or the value) fails to parse. If `value` is non-numeric (e.g. `where age > abc`), every row increments `skippedNonNumeric` and the caller warns that "the column value is not numeric for an ordering comparison" even though the cell values are perfectly numeric. The result is a silently empty match set with a misleading warning. Validate the comparison value once, before the row loop, and return an error for `>`,`>=`,`<`,`<=` when it cannot be parsed; keep the skip counter for genuine per-cell non-numeric values only.

- [ ] **P2-081** `internal/tools/documents.go:88` _performance_
  Extraction never observes the request context. `extractText` (and the helpers it calls: the `docxText` XML token loop, `readXLSXSheets`, the PDF plain-text read, and the raw `io.ReadAll`) performs synchronous, potentially long file/archive I/O, yet none of them accept or check `ctx`. The server wraps tool calls in a timeout (`internal/mcp/server.go` `context.WithTimeout`) and other long-running tool loops in this package do check `ctx.Err()` (e.g. `internal/tools/data.go`), so a large DOCX/PDF/XLSX can run well past the deadline without ever aborting. Consider threading `ctx` into `extractText`/`pdfText`/`docxText` and checking `ctx.Err()` periodically (and wiring a context-aware reader) so cancellation/deadline actually stops extraction.

- [ ] **P2-082** `internal/tools/evaluation.go:552` _security_
  TOCTOU: containment is validated against the *resolved* path (`resolveExistingPath(full)`), but the function returns the original unresolved `full`. Every caller (`os.ReadFile`, `writeRepoJSON`'s `MkdirAll`/`os.Rename`) then re-walks the unresolved path, so a symlink that is swapped in between the check and the use (or a component replaced by a symlink to `/etc`) is followed unchecked and can read/overwrite files outside the repository root. Return the already-resolved path (or re-resolve and re-check immediately before the I/O, e.g. use `O_NOFOLLOW`/open then fstat) so the checked target is the one actually used.

- [ ] **P2-083** `internal/tools/extras.go:225` _bug_
  Numeric X values are silently lost for supported inputs. The tool documents that points accept `t/v` keys (and `x/y`), but X only accepts a `string` or `float64`; `t` is only read as a string. So a point like `{"t": 1700000000, "v": 5}` (or an `x` supplied as `int`/`json.Number`, which the Y branch below explicitly handles) yields `x == ""`. That drops the X coordinate, forces `chartXValues` to return `useX == false`, and silently falls back to index spacing with index labels. Mirror the numeric handling used for Y (int, json.Number) for both `x` and `t`.

- [ ] **P2-084** `internal/tools/extras.go:273` _bug_
  `chartXValues` mixes two different scales into one axis: plain numeric X values are appended as-is, while date/timestamp strings are converted to Unix seconds (`float64(ts.Unix())`, ~1.7e9). Heterogeneous input such as one series using years/indices (`"2024"` -> 2024) and another using dates (`"2024-01-01"` -> 1704067200) is accepted, and then the whole chart is scaled against the 1.7e9 max, collapsing the numeric points onto the left edge — a silently nonsensical chart. Detect a mix and refuse to enable value-axis mode (or normalise to a single unit).

- [ ] **P2-085** `internal/tools/files.go:129` _bug_
  When `readAnyTable` / `readXLSXSheets` fail (malformed CSV, unreadable file, etc.), the error is only appended to `warnings` while the envelope is still built as `mcp.OK`. Callers that key off `status == "ok"` will treat this as a successful describe even though `rows`/`columns`/`headers` are absent, silently hiding a real read failure. Note the sibling path `Deps.loadTable` (internal/tools/data.go) returns `mcp.Fail` for the very same `readAnyTable` errors. Prefer `mcp.Fail` when a parse error occurs, or `mcp.Partial(...)` when metadata is still usable.

- [ ] **P2-086** `internal/tools/knowledge.go:179` _bug_
  `readCapped` is implemented with `io.All/LimitReader` and does not error on oversized input; it just returns up to `limit` bytes. So reading with `maxRawTextBytes+1` means (a) the extra byte is never inspected to detect an oversized file, and (b) a local artifact larger than the cap is silently truncated to ~20MB with no warning, while the object-store fallback below issues a warning. Bound the local read to `maxRawTextBytes` (rune-safely) and emit the same warning when the read hit the cap.

- [ ] **P2-087** `internal/tools/knowledge.go:190` _bug_
  `raw = raw[:maxRawTextBytes]` slices a byte buffer at an arbitrary byte offset and can split a multi-byte UTF-8 rune, so the resulting `text` (and every tokenized chunk) starts with invalid UTF-8 / a U+FFFD. A rune-safe helper already exists in this package (`truncateRunes`).

- [ ] **P2-088** `internal/tools/learning.go:72` _bug_
  Inconsistent nil guard: only `d.Learning` is checked, but this handler (and `learning.promote`) immediately dereferences `d.Cfg`. Today `main.go` and the test registry always set `Cfg` whenever `Learning` is set, so this is latent rather than currently reachable, but a caller that supplies a store without a config manager makes the tool panic instead of returning a structured `mcp.Fail`. Add a `d.Cfg == nil` check (or a shared precondition helper) so the two guards stay consistent.

- [ ] **P2-089** `internal/tools/security.go:115` _maintainability_
  The underlying error is discarded, so every failure mode (missing file, permission denied, I/O error, decode issue) is reported as the same generic "is not readable" message. This hides the cause from logs and from callers that might need to distinguish not-found from a transient store failure. Wrap the original error with %w (and do the same for the InputPath/readCapped/ReadArtifact branches below).

- [ ] **P2-090** `internal/tools/security.go:133` _performance_
  The comment claims the read is "capped to the scan limit", but `ReadArtifact` performs a full `os.ReadFile` / remote object fetch into memory and only *afterwards* is `raw` sliced to `securityScanLimit+1`. A large artifact therefore bypasses the cap and is read entirely into memory, defeating the truncation guard and creating a memory/DoS vector. Read through a size-limited reader (or add a capped/count variant) instead of loading the whole object.

- [ ] **P2-091** `internal/tools/security.go:151` _bug_
  Line-by-line scanning defeats the multi-line injection patterns. `scanSecurity` matches each line independently, so any pattern whose separator spans a newline (`ignore\s+...previous`, and the `.`-based rules which never cross `\n` anyway) is evaded by splitting the payload across two lines, e.g. "ignore all\nprevious instructions". For a security detector this is a real bypass. Consider also matching the full normalized text (collapsing newlines/whitespace) and mapping hits back to line numbers, rather than only per-line.

- [ ] **P2-092** `internal/tools/security.go:159` _security_
  Only matches from rules flagged `secret` are redacted. Excerpts for the non-secret rules return the raw matched text, and the exfiltration rule's `.{0,40}` span between the verb and the keyword can itself contain credential material (e.g. "send sk-ABCDEF... to the api keys"), which then lands in findings, logs and artifacts. Redact (or at least truncate/scrub) excerpts for all rules, not just secret ones.

- [ ] **P2-093** `internal/tools/system.go:93` _performance_
  `emit_activation` defaults to true, so whenever a store is configured every `routing.fly_recommend` call invokes `d.Router.ActivationSnapshot`, which re-embeds the exact same `task` that `Router.Recommend` already embedded in this same call (`ActivationSnapshot` -> `r.embedTask`, events.go:35). With an OpenAI-backed embedder this adds a second network round trip, roughly doubling the latency/cost of the request path. Consider reusing the vector computed during `Recommend`, or flipping the default to false so callers explicitly opt into the extra embed pass.

- [ ] **P2-094** `internal/tools/telegram.go:28` _bug_
  The guard validates `parse_mode` case-insensitively, but the original (possibly lowercase) value is forwarded unchanged to `SendMessage`, whose payload sends it verbatim (`payload["parse_mode"] = opts.ParseMode` in internal/telegram/telegram.go). Telegram's `parse_mode` is expected to use canonical casing ("HTML"/"MarkdownV2"), so an input like `html`/`markdownv2` passes validation here yet may be rejected/ignored by the API. Normalize to the canonical value before forwarding so validation and the forwarded value agree.

- [ ] **P2-095** `internal/tools/timeseries.go:282` _bug_
  Variance is derived from the one-pass `sumSq - sum*sum/n` expression while `sum`/`sumSq` are incrementally accumulated and decremented across the window. For large values (or after many add/subtract cycles) this suffers catastrophic cancellation and can produce a slightly negative variance, which the next line clamps to 0. That makes `s` zero, routes the point into the `constant-window` branch, and can emit a false anomaly with score 0 for a window that is not actually constant. Consider a numerically stable computation (e.g. Welford-style running mean/M2 with removal, or recomputing mean/variance over the window) instead of clamping a cancellation-corrupted value.

- [ ] **P2-096** `internal/tools/util.go:86` _bug_
  The magnitude heuristic silently reinterprets compact numeric dates as epoch values. `20240101120000` (YYYYMMDDHHMMSS, 14 digits) is >= 1e11, so it is parsed as **milliseconds** giving a date around year 2611, and `202401011200` (YYYYMMDDHHMM, 12 digits) similarly lands in the millisecond branch. Depending on the source column, this yields a wrong timestamp with no error. Conversely, negative epoch values (`strconv.ParseInt` accepts a leading '-') match none of the positive thresholds and are silently dropped. Consider restricting each branch to a plausible epoch window (e.g. seconds 1e8..~4e9) and/or detecting all-digit date-shaped values before the epoch conversion, so ambiguous magnitudes fall through to the layout parsers instead of being misclassified.

- [ ] **P2-097** `internal/tools/util.go:285` _bug_
  Unlike `mean`/`stddev`/`sqrt`, `pearson` performs no finiteness check. With large-magnitude inputs (note `mean` clamps an overflowed sum to 0, which makes `a = xs[i] - mx` even larger) `dx`/`dy` can overflow to +Inf and `num` to ±Inf, so `num / sqrt(dx*dy)` can return NaN (or +Inf). Callers such as data.go serialize the result, and `json.Marshal` fails on NaN/Inf, so a degenerate dataset can break output. Guard the return value consistently with the other statistics helpers.

- [ ] **P2-098** `internal/tools/xlsx.go:294` _bug_
  `readSharedStrings` sets `inT` for *any* `<t>` while inside an `<si>`, including `<t>` elements nested inside phonetic runs (`<rPh sb=.. eb=..><t>...</t></rPh>`). Since the concatenated text is never scoped to the real value runs, phonetic/hint text gets appended to the cell string (e.g. `<si><t>漢字</t><rPh><t>かんじ</t></rPh></si>` yields `漢字かんじ`). Track the enclosing element (or an `inRPh` flag set on `<rPh>` and cleared on `</rPh>`) and only accumulate `<t>` when it is a direct child of `<si>`/`<r>`, skipping `<rPh>` content.

- [ ] **P2-099** `internal/tools/xlsx.go:493` _bug_
  The exact-limit EOF probe can spin forever if the underlying reader returns `(0, nil)` (a legal, if discouraged, `io.Reader` result) instead of blocking or returning EOF: neither `n > 0` nor `err != nil` holds, so the `for` loop never terminates. Because this reader wraps request-controlled zip entries on the decode path, add a guard (e.g. bail out with the configured size-limit error after a `(0, nil)` read) rather than looping unconditionally.

- [ ] **P2-100** `internal/tools/xlsx.go:752` _bug_
  `validExcelSerial` allows serials up to ~2.95e6, so `serial * 86400` reaches ~2.55e11, which overflows a 32-bit `int`. On 32-bit builds `int(math.Round(...))` silently wraps, producing a garbage/negative elapsed time. Compute in 64-bit (e.g. `float64`->`int64`) and/or clamp the magnitude before the conversion, consistent with how `validExcelSerial` guards the other formatters.

- [ ] **P2-101** `plugins/hackathonkit/.codex-plugin/plugin.json:16` _bug_
  `"Interactive"` is not one of the recognized capability tokens for this manifest (the allowed permission tokens are `Read`/`Write`). An unrecognized token may be rejected by the manifest validator or silently ignored, so the intended capability would not actually be granted. Remove this entry or replace it with a supported token.

- [ ] **P2-102** `plugins/hackathonkit/hooks/codex-hooks.json:11` _security_
  `%PLUGIN_ROOT%` is interpolated into the cmd.exe command without quotes (same on the `tool-use` and `stop` entries, lines 24/37). If the plugin install path contains spaces (e.g. `C:\Users\Jane Doe\.codex\...`) cmd splits the path and `node` receives a truncated script argument, so the Windows hook silently no-ops (failures are swallowed). A path containing cmd metacharacters (`&`, `|`, `>`) would additionally allow command injection. The POSIX form quotes `"$PLUGIN_ROOT/scripts/hooks.js"`, so the two platforms are not equivalent. If Codex's Windows re-escaping genuinely prevents quotes, don't depend on shell expansion of the root: pass the plugin root via an env var that `hooks.js` reads, or resolve the script from a fixed location inside the script itself.

- [ ] **P2-103** `plugins/hackathonkit/hooks/codex-hooks.json:24` _maintainability_
  This repo's own hook-schema note (docs/OCR-BACKLOG.md, OCR-129) documents a command hook as accepting only `type` + `command` + `timeout`; `commandWindows` is not part of that schema (same on lines 11 and 37). If Codex does not recognise the key, it is either silently dropped — so Windows falls back to the POSIX `command` whose `$PLUGIN_ROOT` cmd.exe cannot expand, breaking all three Windows hooks — or the whole manifest is rejected, disabling every lifecycle hook. Please confirm `commandWindows` against the current Codex plugin hook schema (or move the platform split into `hooks.js`) before relying on it.

- [ ] **P2-104** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.test.sh:44` _test_
  The local binary's exit status is never asserted: `LOCAL_STATUS` is plumbed through the fake binary (`exit "${LOCAL_STATUS:-0}"`) but the harness always leaves it at 0, and the one place that would exercise the local exec path (`run_launcher --list-tools` on line 63) is run outside an `if`. A regression where the launcher swallows or mangles the local binary's exit code would pass silently. Add a case that runs the local path with a non-zero `LOCAL_STATUS` and asserts the code is propagated.

- [ ] **P2-105** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.test.sh:82` _test_
  The exit-status test silently depends on state left by the previous block (the local binary is still non-executable, so execution falls through to docker) and on the fake docker special-casing `compose version`. If any additional docker preflight subcommand is added to the launcher, `DOCKER_STATUS=7` will leak into it and the launcher will abort with 1 instead of 7, producing a false result. Make the fake docker return 0 for all non-`run` subcommands, and explicitly set up the non-executable state here rather than relying on the prior block.

- [ ] **P2-106** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.test.sh:91` _test_
  The duplicate `--repo-root` guard is only exercised in its space-separated form (`--repo-root /tmp`) and only on the local-binary path. The launcher's guard also matches `--repo-root=*`, and the rejection happens before the binary/docker branch, so both the `--repo-root=/tmp` form and the docker-compose fallback path are untested. Add cases for `--repo-root=/tmp` and for invoking the guard while the local binary is non-executable.

- [ ] **P2-107** `scripts/codex-config.test.sh:12` _test_
  `need_python` only verifies that `python3` exists, but the suite depends on `tomllib`, which requires Python 3.11+. On Python 3.8–3.10 the very first `tomllib` call raises `ModuleNotFoundError`, which under `set -e` aborts the run with an opaque traceback instead of a clear diagnostic. Check the module, not just the interpreter:
  
  ```sh
  python3 -c 'import tomllib' >/dev/null 2>&1 || { echo "[codex-config-test] python3 with tomllib (3.11+) required" >&2; exit 2; }
  ```

- [ ] **P2-108** `scripts/codex-config.test.sh:113` _test_
  This refusal assertion only verifies a non-zero exit code. `gen-codex-config.sh` exits non-zero for several unrelated reasons (missing launcher, invalid rendered TOML, bad invocation), so the test would report `[ok]` even if the hand-written-table detection regressed entirely. Assert the distinguishing evidence instead — redirect stderr to a file and check it contains the specific refusal message ("already defines [mcp_servers.hackathonkit]"), or scope the assertion to the detection predicate. The `hand2` spaced-header variant has the same weakness (and additionally does not verify the file was left intact, unlike `hand`).

- [ ] **P2-109** `scripts/docker-check.sh:41` _security_
  Security: `.env` is created with a plain `cp`, so it inherits `.env.example`'s mode modified by the caller's umask. A checked-in template is typically 0644, which means a freshly created `.env` (which is then filled with real `MINIO_ROOT_PASSWORD`/`POSTGRES_PASSWORD`/tokens) can end up world-readable. Note the comment below claims env-set.sh preserves 0600, but env-set.sh preserves whatever mode it finds — it only defaults to 600 when the file did not previously exist, so the guarantee does not hold here. Create the file under a restrictive umask.

- [ ] **P2-110** `scripts/docker-check.sh:146` _bug_
  Robustness: the no-curl fallback assumes a POSIX shell (and curl or wget) is present inside the `minio` and `postgres` images. The official MinIO image is minimal (UBI-micro based) and may not ship `sh`/`curl`/`wget`, so `docker compose exec -T minio sh -c ...` can fail for reasons unrelated to MinIO's health, and readiness then only succeeds via the incidental `wget` in the postgres image. This makes the gate depend on tooling that is not part of the image contract; prefer querying the container state/health (`docker compose ps --format '{{.Health}}'`) instead of re-implementing the probe inside the containers.

- [ ] **P2-111** `scripts/doctor.ps1:9` _bug_
  On timeout the in-flight `BeginConnect` is never completed via `EndConnect`, so the `IAsyncResult.AsyncWaitHandle` is leaked and any eventual connection failure surfaces as an unobserved async exception; additionally `$client.Close()` is invoked here *and* again in `finally`. Since `Close()` does not reliably abort a pending OS-level connect, release the wait handle and avoid the double-close.

- [ ] **P2-112** `scripts/doctor.ps1:76` _bug_
  `Get-FileHash` raises an error for locked/unreadable/zero-byte files. With `$ErrorActionPreference = "Continue"` the error is swallowed, the hash expression evaluates to `$null`, and an entry like `"$rel|"` is emitted. If the same file fails on both sides the mirror comparison can report a false match (or a confusing mismatch) instead of surfacing the real problem. Wrap hashing so failures are explicit. Apply the same fix to the `.agents\skills` loop below.

- [ ] **P2-113** `scripts/doctor.sh:23` _bug_
  `run_bounded` silently degrades to an unbounded run when `timeout` is not installed, so a hanging `scripts/plugin-check.sh`/`scripts/smoke.sh` can block doctor.sh forever and the `124` branches below become unreachable. Consider a portable bound (run child in background, `wait` with a kill timer) or at least print a warning so the user knows checks are not bounded.

- [ ] **P2-114** `scripts/doctor.sh:96` _security_
  This branch echoes the last 5 lines of the child's combined stdout/stderr straight into the report (`SMOKE_OUT` below does the same). Those scripts may print environment values, tokens or credential-bearing URLs, which would leak into terminal/CI logs. Prefer reporting only the exit code plus a pointer to run the script manually, or redact known secret patterns before echoing.

- [ ] **P2-115** `scripts/e2e-fly.ps1:20` _performance_
  The MCP binary is invoked synchronously with no timeout: a hung/slow process (or network stall) blocks the entire test run indefinitely. The same applies to every `& $exe --set-routing-mode ...` call below. Wrap the native invocation with a bounded wait (e.g. `Start-Process`/`Wait-Job` with a timeout, or `Start-Process -Wait` under a job) and kill the process on timeout so the script can fail instead of hanging.

- [ ] **P2-116** `scripts/e2e-fly.ps1:32` _maintainability_
  `Call()` registers a FAIL and returns `$null` on any failure, but every caller immediately dereferences the result (`$ctx.status`, `$rec.metrics.encoder`, `$fb.data.stored`, ...) without short-circuiting. Because `$null` property access silently yields `$null` under the default (non-StrictMode) preference, a single failed call cascades into several extra, misleading `[FAIL]` lines (e.g. `fly_recommend status`, `fly_recommend encoder`, `fly_recommend top-k` all fail even though the real problem was the JSON-RPC call). Returning a sentinel or guarding callers would keep one root cause = one failure.

- [ ] **P2-117** `scripts/e2e-fly.ps1:92` _bug_
  `pamBefore`/`ppl1Before` default to 0 when `data/index/fly-brain.json` is absent, and every counter assertion only compares the persisted value before vs. after. This makes the assertions unreliable: a missing artifact makes `PAM incremented` fail for the wrong reason (both sides read 0), while an artifact mutated by another process between the two reads can make it pass spuriously. Consider requiring/validating the artifact (fail fast if absent) and, ideally, reading the counters from the same stable source instead of re-reading the file mid-test.

- [ ] **P2-118** `scripts/e2e-fly.ps1:125` _test_
  `fbEvents` counts every `feedback` line in the last 200 regardless of task, and only the `recommend` events are filtered by the hard-coded task marker. Stale feedback/neuron events from earlier runs (or a truncated `-Tail 200` window under concurrent runs/buffering) can therefore satisfy the `neuron events: feedback` / `active KC exported` checks without this run producing them. Scope all event queries to the current run (e.g. filter by task/marker and/or record the pre-run log line count and only inspect lines appended after it).

- [ ] **P2-119** `scripts/e2e-fly.ps1:141` _bug_
  Routing mode is global mutable state and the restore call ignores its own exit code. If `--set-routing-mode normal` fails here, the script exits leaving the environment in `fly` mode; combined with the fact that section 2 sets `normal` outside the try/finally, concurrent runs will also interfere with each other's global mode. Check the restore invocation's exit code (and surface it as a FAIL) so a failed restore is not silently swallowed.

- [ ] **P2-120** `scripts/env-set-test.sh:0` _test_
  This block claims to verify that a peer replacing our lock's owner token is not deleted by our cleanup, and the comment says the lock dir is "snapshot[ted], swap[ped], and restore[d]". That never happens: a *fresh* foreign lock is created before the helper starts, so the helper's `lock_is_stale` sees a live pid, aborts with `LOCKED=0`, and `release_lock` returns early without ever evaluating its ownership check. The `release_lock` branch `[ "$owner" = "$LOCK_TOKEN" ] || { [ -z "$owner" ] && ... }` (i.e. the case where we hold the lock but a peer's token is present) is therefore never exercised, and the assertion only proves the writer aborted. Either genuinely simulate the swap while the writer holds the lock, or drop the misleading comment/claim so coverage isn't overstated.

- [ ] **P2-121** `scripts/env-set-test.sh:0` _test_
  The test hard-codes pid 999999 to mean "dead". `lock_is_stale` probes it with `kill -0`; on hosts where that pid happens to exist (or `pid_max` permits it), the lock is treated as live, the helper retries for ~10s and exits non-zero, and both "dead-pid lock reclaimed" assertions fail spuriously. Spawn-and-reap a real process to obtain a guaranteed-dead pid, or pick a pid derived from the running system's `pid_max`, instead of a magic constant.

- [ ] **P2-122** `scripts/env-set.sh:174` _bug_
  Embedded newlines/CR are rewritten to the literal two-character sequences `\n`/`\r`. A POSIX shell does not recognise these escapes inside double quotes, so a later `source`/`.` of the file yields a literal backslash+n instead of a newline, silently corrupting the value (values read from stdin legitimately contain newlines, since `$(cat)` preserves them). This is only correct for parsers that explicitly decode `\n`. Either emit the raw newline inside the quotes (sh allows multi-line double-quoted strings) or document that consumers must decode these escapes rather than source the file directly.

- [ ] **P2-123** `scripts/env-set.sh:181` _bug_
  Duplicate detection only matches a line that starts exactly with `key=`. Common `.env` forms such as `export KEY=...`, leading whitespace, or `KEY = value` are not recognized, so an existing definition is left in place while a second canonical `KEY=...` line is appended. A shell that sources the file will then apply whichever assignment it reaches last, so the stale value may win and the file accumulates duplicate keys. Since the key is already validated to `[A-Za-z_][A-Za-z0-9_]*` (no regex metacharacters), this can be matched with a regex that also tolerates `export` and surrounding whitespace.

- [ ] **P2-124** `scripts/gen-codex-config.sh:23` _bug_
  The `--remove` path performs the same destructive strip (anything from a lone BEGIN marker to EOF is deleted) but, unlike the install path, runs no TOML validation and never trims/verifies the result. A stray/unbalanced BEGIN marker in a user-edited file therefore silently deletes all following content with no safety check. Reuse the python3/tomllib validation (when available) before the `mv`, and at minimum warn/refuse when an unbalanced marker is detected instead of truncating to EOF.

- [ ] **P2-125** `scripts/gen-codex-config.sh:56` _bug_
  The hand-written-table guard only recognizes a single-line `[mcp_servers.hackathonkit]` header. Valid TOML that defines the same table in another spelling — an inline table (`mcp_servers.hackathonkit = { ... }`), dotted keys (`mcp_servers.hackathonkit.command = "..."`), or a quoted/whitespaced header the regex doesn't cover — is not detected, so the script proceeds and the subsequent strip/append can merge with or clobber hand-written settings despite the intC of refusing to. Consider broadening the detection (or validating the resulting merge) rather than relying on one header shape.

- [ ] **P2-126** `scripts/gen-codex-config.sh:99` _bug_
  The blanket fallback to 180 contradicts the stated guarantee that the override "cannot silently shorten" the project defaults. If the awk scope/parse misses a legitimate value — e.g. a quoted key (`"startup_timeout_sec" =`), an array-of-tables/indented header that the `table_header` regex rejects, or the `mcp_servers`/`hackathonkit` substring match being triggered by a comment on a different header — a project default larger than 180 will be replaced by 180 here, silently shortening the budget. Prefer omitting the key entirely when it cannot be parsed, or fall back to a value that cannot shrink the budget, and tighten/verify the parser.

- [ ] **P2-127** `scripts/gen-codex-config.sh:110` _bug_
  The read-modify-write of `$DEST` has no locking. Two overlapping invocations (e.g. the plugin installer running twice, or a user re-running while an earlier run is still in flight) both read the same `$DEST`, each strips/appends in its own temp file, and whichever `mv` lands last silently discards the other run's result — the atomic `mv` only guarantees the *final* file is not torn, not that the update is not lost. Consider guarding with an exclusive lock (e.g. `flock`/`mkdir` lockdir) around the read-modify-write section.

- [ ] **P2-128** `scripts/gen-mcp-manifest.sh:33` _bug_
  `tr -d '[:cntrl:]'` *deletes* every C0/DEL byte from the value instead of escaping it. The accompanying comment claims "such bytes cannot appear in a path", but on POSIX filesystems a path may legally contain any byte except NUL and '/'. So a root such as a directory whose name contains a newline/tab/backspace is silently rewritten to a different (still valid) path and the manifest is published pointing somewhere unintended — a silent corruption rather than a loud failure and, unlike the previous revision, other C0 bytes no longer produce invalid JSON but they still change the value. Prefer failing fast on control characters, or emit proper `\uXXXX` escapes, so the value written always equals the caller value.

- [ ] **P2-129** `scripts/install-prompts.ps1:38` _maintainability_
  `New-Item` (and the `Copy-Item` in the loop) are not wrapped in try/catch. With `$ErrorActionPreference = "Stop"` any failure (disk full, permission denied, `$DestDir` exists as a file) surfaces as a raw terminating exception with an opaque stack trace instead of the friendly `[prompts]` message used elsewhere, and a partial install (some prompts copied, some not) is never reported. Wrap the create/copy operations and emit a consistent `[prompts]` diagnostic via `Fail`.

- [ ] **P2-130** `scripts/install-prompts.sh:26` _maintainability_
  Under `set -e`, any `cp` (or `mkdir`) failure aborts the script immediately mid-loop, leaving a partially installed state with no error summary and no indication of which prompts were applied. Handle per-file failures explicitly (report and track failures, then summarize) so the user knows the true state. This concern applies to the `cp` inside the install loop as well.

- [ ] **P2-131** `scripts/install-prompts.sh:34` _bug_
  The destination guard only rejects symlinks and directories. FIFOs, sockets and device files pass the guard, so the following `cp` can clobber a device or block indefinitely on a FIFO. Also, this check-then-cp sequence is a TOCTOU race if `$dest` is swapped in between. Reject any existing non-regular destination (including dangling symlinks).

- [ ] **P2-132** `scripts/install-prompts.sh:39` _bug_
  Locally modified prompts are only warned about and then overwritten unconditionally, silently destroying user edits with no backup. Consider preserving the modified file (e.g. to `$dest.bak`) before clobbering it.

- [ ] **P2-133** `scripts/install-prompts.sh:56` _bug_
  `[ -f "$dest" ]` follows symlinks, so a symlink pointing at a file with matching content is removed by `rm -f` even though `install` explicitly refuses to touch symlinks. This is inconsistent with install and can delete a user-created link. Require the destination to be a non-symlink regular file, matching the install guard.

- [ ] **P2-134** `scripts/plugin-check.ps1:81` _security_
  The containment guarantee depends on `Resolve-Path` following symlinks/junctions. That behavior is only guaranteed on PowerShell 6+; on Windows PowerShell 5.1 `Resolve-Path` returns the link path itself, so a symlink placed inside the plugin (pointing to an executable outside it) would pass the `StartsWith` check and be executed. Use an explicit link-resolution helper (e.g. loop over `Get-Item ... | Select-Object -ExpandProperty Target`, or `[System.IO.File]::ResolveLinkTarget` on .NET 6+) and only then compare, or document/require PowerShell 7+.

- [ ] **P2-135** `scripts/plugin-check.ps1:82` _bug_
  This `Resolve-Path` call (and the `$cmdResolved` one above) is not wrapped in try/catch and no `-ErrorAction` is set. If resolution fails (broken/replaced link, race between `Test-Path` and this call), `$sourceResolved` becomes `$null`; the following `$sourceResolved.TrimEnd(...)` then throws, so the script aborts with an unhandled error instead of the intended clean diagnostic. Guard these resolutions and fail with an explicit exit 1.

- [ ] **P2-136** `scripts/plugin-check.ps1:91` _bug_
  `$env:TEMP` is a Windows-only convention; on Linux/macOS (PowerShell Core, which this script clearly targets given the `$env:OS`/separator handling below) it is normally unset and `$env:TMPDIR` is used instead. `Join-Path $env:TEMP ...` will therefore fail (or bind an empty path) and the script exits with "cannot create the temp directory" even though nothing is wrong. Resolve the temp root portably.

- [ ] **P2-137** `scripts/plugin-check.ps1:126` _performance_
  `Stop-Job` terminates the job, but the native MCP executable spawned inside it is not necessarily killed and can be orphaned on timeout, holding the temp files/ports opened for the check. Capture the child PID (or run the executable via a tracked process) and kill the process tree (e.g. `Stop-Process -Id ... -Force -ErrorAction SilentlyContinue` / `taskkill /T /F`) on the timeout path.

- [ ] **P2-138** `scripts/plugin-check.sh:82` _security_
  The guard only inspects the plugin root directory (`os.lstat(source_raw)`), not the files that are actually executed/consumed. A `.mcp.json` or launcher script that is itself group/world-writable (e.g. an existing `scripts/hackathonkit-mcp-launcher.sh` with mode 0777) is still executed, so the stated guarantee ("refuse a symlinked or group/world-writable plugin tree") is not met. The manifest and the resolved command should be `stat`ed as well and rejected if they are not regular files or are group/world-writable. Note also that this remains a check-then-use (TOCTOU) window: the state can change between `lstat`, `realpath` and the later `subprocess.run`, so this is a mitigation and not a hard guarantee.

- [ ] **P2-139** `scripts/quality-gate.sh:12` _security_
  HACKATHONKIT_UID/GID come straight from the environment and are later used verbatim in a root `chown -R`. Unlike the Dockerfile (which validates them as non-zero numeric ids before building the image), nothing here rejects malformed values: an option-like string (`--reference=...`), a typo, or `0` would be interpreted by chown as an option/owner and can silently re-own the whole evidence tree (or error out). Validate the values as numeric (dropping/ignoring anything else) before building `owner`, and skip the chown when the result is empty.

- [ ] **P2-140** `scripts/quality-gate.sh:20` _bug_
  The recursive chown covers the entire `evals/quality` tree on every run, not just the evidence written by the current invocation (`evals/quality/<sha>`). This needlessly reassigns ownership of unrelated historical evidence and, with concurrent gate runs, makes two `chown -R` processes interleave over the same tree (partial/half-applied ownership, races with another run's cleanup). Consider chowning only this run's output directory (or using `chown --from=root`) instead of the whole tree.

- [ ] **P2-141** `scripts/quality-gate.sh:38` _bug_
  Freshness detection only considers `*.go`, `go.mod` and `go.sum`. Any build-affecting non-Go input (embedded assets/templates/SQL/configs, or generated sources) can change without triggering a rebuild, so the prebuilt binary keeps reporting evidence for stale sources. Also, `2>/dev/null` hides `find` permission errors, so an unreadable subtree would be silently skipped and treated as up-to-date. Consider widening the tracked patterns (or hashing inputs) and surfacing/forbidding find errors instead of swallowing them.

- [ ] **P2-142** `scripts/redact.sh:20` _bug_
  The keyword alternation is wrapped in unanchored `[A-Za-z_]*` runs, so any longer identifier containing a keyword is matched and its value redacted: `monkey=banana` (matches `key`), `passenger=...`/`bypass=...` (matches `pass`), `keyboard: ...`/`tokenizer: ...`, etc. The header comment only warns about URL over-redaction, so this silent corruption of benign output is undocumented. Add token boundaries, e.g. `(^|[^A-Za-z0-9_])([A-Za-z_]*KEY[A-Za-z_]*)...` and a trailing boundary, or otherwise require the keyword to be a whole identifier.

- [ ] **P2-143** `scripts/redact.sh:24` _bug_
  The JSON rule only handles double-quoted keys and values. A value containing an escaped quote (e.g. `"password": "a\"b"`) terminates the `[^\"]*` match early, leaving the remainder (and potentially the secret) unredacted. Single-quoted or unquoted JSON-like forms are not covered either. Consider handling escaped quotes (`"(\\.|[^\"\\])*"`) or documenting/limiting the supported JSON subset.

- [ ] **P2-144** `scripts/redact.test.sh:10` _security_
  The failure branch echoes `detail`, and every caller sets `detail` to `got: $out` — i.e. the raw redactor output. When a test fails precisely because the secret was *not* redacted, this prints the plaintext credential to stdout/CI logs, defeating the purpose of the redactor. On failure, avoid echoing raw output (or gate it behind an explicit debug flag) and instead print a safe placeholder.

- [ ] **P2-145** `scripts/redact.test.sh:25` _test_
  Coverage gaps leave several likely regression paths untested: empty input, leading/trailing whitespace around keys, values spanning newlines, the same key appearing twice on one line, and — most importantly — the documented fail-closed path when `sed -E` is unavailable. Also no test asserts idempotency (running the redactor on already-redacted output must be a no-op). Consider adding cases for these boundary conditions.

- [ ] **P2-146** `scripts/redact.test.sh:41` _test_
  This assertion only checks that the substrings `a@b`/`c@d` are absent, so it passes even if the redactor emits nothing, drops the URLs, or mangles them into something unrelated. The test gives false confidence for the comma-joined case. Assert the actual expected output (e.g. `[ "$out" = 'http://***@b,http://***@d' ]`) instead of only checking for absence.

- [ ] **P2-147** `scripts/redact.test.sh:49` _test_
  Over-redaction is only checked with three benign inputs. Given that `pass`/`token`/`key`/`secret` are matched as substrings, inputs like `token_count=5`, `keyboard=us`, `secretary=x` or prose such as `description=my secret plan` are realistic over-redaction candidates that no test guards. Add assertions that credential-keyword substrings used as ordinary identifiers/values stay readable.

- [ ] **P2-148** `scripts/repair.ps1:44` _bug_
  When the existing install-state cannot be read or parsed, the profile silently degrades to the hard-coded default `full`. For a machine that was installed as `core`, a transient read/parse failure (e.g. a partially written file) turns this repair run into an implicit upgrade to `full`, and the result is then persisted by write-install-state.ps1. Consider defaulting to the last-known profile only when it can be determined, or failing loudly instead of assuming `full` when the state file exists but is unreadable.

- [ ] **P2-149** `scripts/repair.sh:18` _bug_
  Check-then-remove-then-create is not atomic. Run A can detect a stale lock and `rm -rf "$LOCK"`, then Run B (which also read the old stale pid) executes its own `rm -rf "$LOCK"` after A has already recreated and populated the lock, deleting A's live lock. Both runs then pass the `mkdir` gate and execute concurrently, defeating the serialization the lock is meant to provide. Verify ownership after acquiring (re-read the pid and confirm it is `$$`) or use an atomic primitive (e.g. `ln`/`set -C`) instead of `rm -rf` on the shared path.

- [ ] **P2-150** `scripts/repair.sh:61` _bug_
  The temp files are only removed by the explicit `cleanup_bin` call in the build-failure branch. Because `set -e` is active, if the 2nd or 3rd `mktemp` fails (e.g. disk/permission error), the script exits immediately and the previously created temp file(s) leak in `bin/`. Likewise, Ctrl-C/SIGTERM during the build leaves all three temps behind, since the INT/TERM traps installed earlier only `exit` and the EXIT trap only removes the lock. Register the cleanup with a trap (or build into a dedicated temp dir) so interrupted/failed runs do not accumulate `*.tmp.XXXXXX` files.

- [ ] **P2-151** `scripts/repair.sh:78` _bug_
  Installing the three binaries with sequential `mv` is not atomic: if a later `mv` (or the following `chmod`) fails under `set -e`, the script exits having replaced only part of `bin/`, leaving a mixed-version toolchain (e.g. new mcp with old demo-api/ctl) with no rollback. Consider staging the built binaries in a temp directory and swapping them in with a single atomic operation, or restoring the originals on failure.

- [ ] **P2-152** `scripts/repair.sh:93` _bug_
  The fallback `codex plugin list | grep -q hackathonkit` matches any line containing the substring `hackathonkit` (e.g. a differently-versioned/partially installed plugin, or the `hackathonkit-local` marketplace name), so a failed `plugin add` can be masked and the script proceeds to write install state as if the plugin were installed. Match the exact plugin id/config key (e.g. `grep -q 'hackathonkit@hackathonkit-local'`) or otherwise assert the expected plugin version.

- [ ] **P2-153** `scripts/set-version.test.sh:29` _test_
  The comment states the contract is "exit 2, leaving files untouched", but the assertions only require *a* non-zero exit and never verify that no file was modified. A regression that exits 1 instead of 2, or one that rewrites files before validating the version, would go undetected. Capture the exit status and assert it equals 2, and compare a file's content before/after the rejected run.

- [ ] **P2-154** `scripts/set-version.test.sh:47` _test_
  The helper syncs three manifests, but the suite only asserts two of them: `plugins/hackathonkit/.codex-plugin/plugin.json` is created in the fixture (set to 1.2.3) yet never checked after the bump. A regression in that specific target would pass silently. Add the missing assertion.

- [ ] **P2-155** `scripts/setup-resources.sh:108` _bug_
  `TELEGRAM_CHAT_ID` is a required `EnvKeys` entry for the telegram provider (`internal/resources/resources.go`), yet an empty/skipped chat id is silently ignored here (`|| true`), so `TELEGRAM` stays 1 and the profile advertises telegram as claimed while the provider is not ready. Treat a missing chat id the same as a missing token (downgrade `TELEGRAM=0`), or at least warn the user that the notification target was not stored.

- [ ] **P2-156** `scripts/setup-resources.sh:117` _bug_
  Partial multi-key provider leaves stale credentials behind. When `MINIO_ENDPOINT`/`MINIO_ACCESS_KEY` succeed but `MINIO_SECRET_KEY` is empty, `MINIO` is downgraded to 0, yet the endpoint and access key remain in `.env` — exactly the "provider that cannot authenticate" state the comment above claims to avoid. Read all three values first and only write them (and claim the provider) when the full set is non-empty.

- [ ] **P2-157** `scripts/setup.ps1:101` _security_
  The `try`/`catch` wraps not only the RNG/token generation but also the whole atomic `.env` write and its own integrity assertion (`throw "atomic .env write did not produce ..."`). Consequently a genuine write failure (e.g. read-only `.env`, `File.Replace` unsupported) is silently downgraded to a warning and setup continues. If `.env` still holds a shipped placeholder, the demo API then runs protected by a known/weak token — which is exactly what the placeholder check above tried to prevent. Narrow the catch to the token-generation step, or make write failures fatal so the operator cannot proceed with an unset/placeholder token.

- [ ] **P2-158** `scripts/setup.ps1:117` _security_
  `[IO.Path]::GetFullPath()` does not resolve reparse points. If `$dir` (e.g. `.agent/skills`) is itself a junction/symlink to a location outside the repo, `$dirFull` is still the link path, so every child (`$dirFull\<skill>`) passes `StartsWith($dirFull)`. The per-skill reparse check only inspects the child, not the mirror root, so `Remove-TreeNoReparse` would delete real directories at the link target — defeating this containment guard. Reject the whole mirror when `$dir` is a reparse point before iterating.

- [ ] **P2-159** `scripts/setup.sh:68` _security_
  The idempotency check accepts *any* non-empty value, so a shipped placeholder such as `HACKATHONKIT_DEMO_TOKEN=changeme` (or `your-token-here`) is treated as a real secret and no strong token is generated. Since this value is what protects the demo API, the file can end up with a guessable token. The PowerShell sibling (scripts/setup.ps1:63-72) already rejects a known placeholder list before deciding to regenerate; apply the same validation here.

- [ ] **P2-160** `scripts/setup.sh:121` _bug_
  `docker compose up -d` returns as soon as the containers are created, not when they are healthy. RUNTIME is then set to "docker" and the success message is printed even if postgres/redis/minio crash-loop immediately, so install-state.json claims a working docker runtime that is not actually serving. The PowerShell sibling waits for health (`up -d --wait`, setup.ps1:168); add the same wait (or a health probe) so RUNTIME reflects what is really running.

- [ ] **P2-161** `scripts/smoke.ps1:21` _maintainability_
  In `auto` mode the mere existence of `bin\hackathonkit-mcp.exe` selects the local binary with no freshness check and without logging which transport was chosen. A stale artifact can therefore silently pass the smoke test while the container path is never exercised. Consider comparing the binary's timestamp against the sources under `cmd/`/`internal/` (warn or fall back to docker when stale) and emitting the selected runtime via `Write-Host` so the bypass is at least visible.

- [ ] **P2-162** `scripts/smoke.sh:34` _maintainability_
  The 60s cap is a fixed magic number; a cold `docker compose run` (image pull + container start) can easily exceed it and abort with a generic failure that hides whether the code or the environment is at fault. Consider making the limit configurable (and larger for the compose path). Note also that when `timeout` is not installed the server runs completely unbounded, so a hanging server still stalls CI.

- [ ] **P2-163** `scripts/smoke.sh:82` _bug_
  The staleness heuristic only scans `.go` files under `cmd/` and `internal/`. Changes to `go.mod`/`go.sum`, embedded assets, `Makefile`, or other packages will not mark the binary stale, so `--auto` may silently run an outdated `bin/hackathonkit-mcp`. Additionally, when docker is unavailable the script only prints a warning and proceeds to run the possibly stale binary, which can mask real failures. Consider widening the freshness check (or forcing a rebuild/`--binary` requirement in that case).

- [ ] **P2-164** `scripts/smoke.sh:136` _maintainability_
  The python3 validator and the grep-based fallback enforce different contracts: python accepts any dict with ids {1,2,3} and a result, while the fallback additionally requires exactly three `"jsonrpc"` occurrences. Validation therefore depends on whether python3 happens to be installed. Make both paths check the `jsonrpc` field for consistency.

- [ ] **P2-165** `scripts/smoke.sh:148` _bug_
  The fallback extracts ids with `grep -o '"id"...'` across the whole stdout, so any nested `"id"` field inside a `system.context` result (or any other nested object) is also counted as a top-level response id. That inflates `GOT_ID_COUNT` and makes the handshake fail spuriously. Prefer parsing the top-level id (e.g. via python) or anchor the match to the start of the JSON-RPC envelope instead of matching `"id"` anywhere in the line.

- [ ] **P2-166** `scripts/smoke.sh:155` _bug_
  `grep -q '"error"'` scans the entire stdout, so a legitimate tool result whose payload contains the token `"error"` (e.g. a diagnostic object/field inside `system.context`) is treated as a JSON-RPC error and fails the smoke test. The same over-broad matching applies to `grep -c '"jsonrpc"'` below. Validate the parsed top-level object (error member present on the envelope) rather than grepping raw text.

- [ ] **P2-167** `scripts/smoke.test.sh:54` _bug_
  Under `set -e`, this assignment aborts the entire suite when `grep -c` finds no match (it prints `0` and exits `1`). Because it depends on `smoke.sh` keeping the exact literal `{"jsonrpc":"2.0","id":` prefix, any reformatting of the requests (spaces, variable-built payloads, etc.) silently kills the run before a single assertion executes, rather than failing a clear check. Make the derivation non-fatal and assert on it explicitly.

- [ ] **P2-168** `scripts/smoke.test.sh:127` _test_
  The failure-path checks only compare numeric exit codes and never assert the user-facing stderr messages (`--binary requires ...`, `--compose requires the docker CLI`, `unknown argument: ...`). Additionally the stub always returns a well-formed success response, so malformed/error JSON paths in `smoke.sh` are never exercised. Add stderr assertions and at least one error-response stub mode to cover these branches.

- [ ] **P2-169** `scripts/sync-harness.ps1:11` _bug_
  Reparse points (symlinks/junctions) are silently skipped here and in `Get-TreeManifest`. Since both the copy and the manifest skip them, a skill reachable through a junction/symlink is silently omitted from the mirror while the source and copy manifests still compare equal — the mirror is incomplete yet reported as a match. Prefer failing (or at least warning) when a reparse point is found under the skills source instead of dropping it silently.

- [ ] **P2-170** `scripts/sync-harness.ps1:87` _bug_
  `Get-Process -Id $holderPid` only proves that *some* process with that PID exists. If the PID was reused by an unrelated process, a genuinely stale lock is treated as live and the loop spins for roughly 20s before failing, blocking the sync instead of recovering. Consider validating the process start time against the lock's creation time (or relying on the owner token) rather than PID existence alone.

- [ ] **P2-171** `scripts/sync-harness.ps1:154` _maintainability_
  The required-file and JSON-manifest validation runs only after `.claude/skills`, `.opencode/commands` and `.claude/commands` have already been mutated. On validation failure the script exits 1, but the mirror/command side effects are already committed, leaving the working tree modified on a reported failure. Consider validating the required files and JSON manifests before performing any mutation.

- [ ] **P2-172** `scripts/sync-harness.sh:156` _bug_
  The mirror is swapped with two separate `mv` operations (`mv "$dest" "$CMDS_OLD"` then `mv "$CMDS_STAGE" "$dest"`), so `$dest` is momentarily absent. If the process is SIGKILLed between them the traps do not run and an orphaned `$dest.old.<pid>` is left behind; because the next run uses a different `$$` suffix it never reconciles that backup, leaking it indefinitely. Reconcile stale `*.old.*` directories at startup (or otherwise guarantee the backup is cleaned up).

- [ ] **P2-173** `scripts/sync-harness.sh:227` _bug_
  When no `python3`/`node` is available the fallback only checks the first byte and the final byte, so malformed manifests (e.g. `{ "a": }`) pass validation and the script still prints "adapters ready". The warning is emitted, but consider failing here (or reflecting the weakened check in the exit status) rather than reporting readiness with an unvalidated manifest.

- [ ] **P2-174** `scripts/sync-skills.ps1:54` _bug_
  The retry budget is 100 × 200ms = 20s, but eviction of a lock with an unparseable PID (or one whose PID is dead but younger than 60s) requires the lock to be older than 60s. A run that crashed shortly before leaves a lock this invocation can neither evict in time nor wait out, so the sync always fails. Align the retry budget with the staleness threshold, and/or evict immediately when the owner PID is confirmed absent (the age gate only needs to protect against PID recycling, not against a confirmed-dead owner).

- [ ] **P2-175** `scripts/sync-skills.ps1:58` _bug_
  If writing the owner token fails, `-ErrorAction SilentlyContinue` hides it, but the `finally` still requires `$owner -eq $lockToken` to release the lock. A failed write therefore leaks this run's own lock permanently (until age eviction). Do not swallow the write error, or treat a missing/unreadable owner file as "owned" when the `pid` file still contains this run's PID.

- [ ] **P2-176** `scripts/sync-skills.ps1:78` _bug_
  `Get-Process -Id $holderPid -ErrorAction SilentlyContinue` treats any failure — including access-denied for a holder owned by another user — as "process is dead". Combined with the 60s age gate below, this can let a concurrent run evict the lock of a holder that is still alive and mid-copy. Distinguish "not found" from other errors (e.g. use `-ErrorAction Stop` in a `try` and inspect the exception, or check `$null` vs a thrown error) before concluding the holder is gone.

- [ ] **P2-177** `scripts/sync-skills.ps1:119` _bug_
  The lock is released only in the `try/finally` that begins later, but all work between acquisition and that `try` (this recovery `Move-Item`, plus the `$tmp` `New-Item`/`Remove-Item`) runs under `$ErrorActionPreference = "Stop"` with no cleanup guard. If any of these throws, the script aborts and the lock is never removed, blocking every subsequent run until the 60s age eviction kicks in. Move the post-acquisition body inside the same `try/finally` (or wrap the lock in an outer `try/finally`) so release is guaranteed.

- [ ] **P2-178** `scripts/sync-skills.sh:58` _bug_
  `find ... | wc -l` masks `find` failures under `set -e`. The pipeline's status is that of `wc -l`, which succeeds even if `find` fails, so `ERROR` output goes to stderr but the variable ends up as `0`. The script then continues with bogus counts and reports a misleading "mirror mismatch" instead of the real error. Add `set -o pipefail` (and quote the substitutions) or capture and check `find`'s exit status directly.

- [ ] **P2-179** `scripts/sync-skills.sh:80` _bug_
  The `cmp` fallback relies on unquoted word splitting of `$A_LIST` (`for rel in $A_LIST`), so filenames containing spaces, tabs or newlines are split into bogus paths; the resulting `cmp` fails and the script declares a mismatch even though the mirror is fine (and in other orderings files can be skipped). Use a newline-safe loop and reject names with newlines, e.g. read with a `while IFS= read -r` loop over the `find` output, or `find ... -exec cmp` per file.

- [ ] **P2-180** `scripts/sync-skills.sh:94` _bug_
  Stale-lock reclamation is unsafe. `kill -0` returns non-zero not only when the process is gone but also when it exists and is owned by another user (EPERM), so `! kill -0` will delete a *live* lock held by a different user, defeating mutual exclusion. Conversely, PID reuse can make a dead lock's pid appear alive, so the stale lock is never freed. A lock dir with no `pid` file is also never reclaimed (see the other comment). Prefer an age/mtime-based staleness check as a fallback and never treat EPERM as "dead".

- [ ] **P2-181** `scripts/uninstall.ps1:16` _bug_
  Excluding reparse-point entries from BOTH trees means a user-authored skill that only *adds* a symlink/junction (or a link to extra content) produces the same entry set as the shipped skill, so `Test-SameTree` returns true and the whole target directory is deleted — contradicting the stated intent to preserve user-authored skills. Consider treating any extra link in the target as a difference (i.e. keep it) rather than silently filtering links out of the comparison.

- [ ] **P2-182** `scripts/uninstall.ps1:148` _security_
  Check-then-delete race: the reparse-point/existence checks (and, in the shared branch, the content comparison) happen before `Remove-TreeNoReparse`/`Remove-Item` runs. A path swapped for a reparse point or modified copy between the check and the deletion defeats the junction/user-skill protections. Where possible, delete from the already-fetched item object or re-validate the attribute immediately before removal.

- [ ] **P2-183** `scripts/uninstall.sh:73` _bug_
  Substring match: `grep -q hackathonkit` also matches unrelated plugins whose name merely contains the token (e.g. `hackathonkit-tools`, `my-hackathonkit`). Because the match gates a destructive `codex plugin remove`, use a boundary-anchored pattern so only the exact `hackathonkit` entry triggers removal.

- [ ] **P2-184** `scripts/uninstall.sh:82` _bug_
  Same substring issue for the marketplace: `grep -q hackathonkit-local` matches any marketplace whose name contains that token (e.g. `hackathonkit-local-v2`), so an unrelated registration can trigger the remove branch. Anchor the match to a complete field.

- [ ] **P2-185** `scripts/uninstall.sh:99` _bug_
  Dry-run/apply parity: the apply path skips these commands when the `codex` CLI is absent, and skips each one when the plugin/marketplace is not actually registered. The dry-run branch unconditionally prints both commands, so the preview advertises removals that apply would not perform. Mirror the apply-path conditions (codex availability and registration checks) in the preview.

- [ ] **P2-186** `scripts/uninstall.sh:116` _bug_
  Unconditional `|| true` masks any failure of the managed-config removal, so a failure to strip the hackathonkit block is silently reported as success. This is inconsistent with every other removal path, which increments FAILURES. Capture the exit status and warn (and optionally increment FAILURES) when it fails.

- [ ] **P2-187** `scripts/validate-skills.sh:7` _bug_
  When `command -v` cannot locate the script (e.g. it is not on `PATH`, invoked via a name found in an odd way, or `command -v --` is unsupported and returns nothing), `SELF` keeps the bare name. `dirname "$SELF"` then yields `.`, so `ROOT` silently becomes the parent of the caller's CWD instead of the real checkout — and for a validation gate this can produce a false "pass" against the wrong tree. Also note `command -v --`, `dirname --` and `cd --` are not strictly POSIX (the `--` handling varies across `sh` implementations such as dash/busybox ash), which compounds the mis-resolution. Fail loudly when the script location cannot be resolved instead of falling back to a bare name.

- [ ] **P2-188** `scripts/validate-skills.sh:13` _bug_
  The repository-identity guard only checks for `docker-compose.yml`. A wrong tree that happens to contain that file passes, while the referenced standard (`docs/SKILL-STANDARD.md`) — which is what this script claims to validate against, and what the `go run`/binary path actually reads — is never confirmed to exist. Since `ROOT` resolution above can mis-resolve, strengthen the guard by also requiring the files the validator depends on (at minimum `docs/SKILL-STANDARD.md`, and ideally `cmd/hackathonkit-mcp`).

- [ ] **P2-189** `scripts/verify-command.test.sh:42` _test_
  These content assertions are too permissive: a bare `coverage`, `seven checks`, or `do not ask the user` can match incidental prose, so a real content regression would still pass. In addition, `evaluate.md` is only asserted to exist (the generic existence loop) and has no content check at all, leaving that command's behavior untested. Tighten patterns (anchored phrases) and add at least one content assertion for `evaluate.md`.

- [ ] **P2-190** `scripts/verify-command.test.sh:78` _maintainability_
  This test mutates the working tree: `sync-harness.sh` regenerates `.opencode/commands/*.md` and `.claude/commands/*.md` in place, but the test never snapshots/restores them, so running the suite dirties the repo and can mask real drift. Worse, `|| true` swallows the script's exit status: if sync fails, the failure surfaces later as misleading "mirror ... missing or stale" assertions instead of the actual sync error. Capture and assert the sync exit status before comparing mirrors.

- [ ] **P2-191** `scripts/verify-command.test.sh:92` _bug_
  Under `set -e` this is fatal: if `install-prompts.sh install` returns non-zero, the whole test aborts immediately and no `[fail]` line or summary is printed, so the real cause is hidden and only the trap cleanup runs. Guard the invocation so failure is reported through `check`.

- [ ] **P2-192** `scripts/write-install-state.ps1:63` _bug_
  When `$srcSkills` is missing the script only emits a warning (`Write-Warning` at the earlier check), yet `$Skills` is still set to the mirror directory count. A stale `.agents/skills` mirror left from a previous install will therefore be reported as a positive skills count even though the plugin source no longer exists, overstating a broken installation. Force the count to 0 (or skip the mirror comparison) when the source directory is absent.

- [ ] **P2-193** `scripts/write-install-state.ps1:99` _bug_
  `New-Item` runs outside the `try`/`catch` that is meant to turn write failures into the friendly `Fail` message. A permission or path error here (e.g. read-only root, invalid name) will surface as an unhandled terminating exception instead of `[install-state] cannot write ...`. Wrap the directory creation in the `try` (with `-ErrorAction Stop`) so it follows the same error-reporting path.

- [ ] **P2-194** `scripts/write-install-state.ps1:109` _bug_
  Two concerns with this swap: (1) `File.Replace` is not available on all platforms/filesystems (it throws `PlatformNotSupportedException` on .NET Framework and on some non-Windows filesystems), and this script is explicitly invoked for binary/docker installs that may run on Linux; (2) the `Test-Path`/`Move` pair is a check-then-act race — a concurrent run can create `$out` between the two calls, making `File.Move` throw and discarding the freshly generated state. Prefer an overwrite-style move (or fall back to `Move -Force`/delete+move) and catch `PlatformNotSupportedException` to fall back safely.

- [ ] **P2-195** `scripts/write-install-state.sh:22` _bug_
  `json_escape` is still incomplete: it deletes CR/LF and escapes only `\` and `"`. TAB and other control characters (U+0000–U+001F, e.g. a tab or \x01 in the plugin version string) are emitted verbatim, which makes `install-state.json` invalid JSON (RFC 8259 forbids raw control chars inside strings) and can break the strict consumers (`doctor.sh`, `repair.sh`) that parse this file. Escape all control characters (e.g. emit \t/\n/\r/\uXXXX) or reject values containing them.

- [ ] **P2-196** `scripts/write-install-state.sh:29` _bug_
  `json.load(...).get("version", "")` returns `None` when the manifest contains `"version": null`, and `print(None)` writes the literal string `None` as the version. Since the value is non-empty, the `-z` fallback never runs and `install-state.json` silently records `"version": "None"`. Non-string values (objects/arrays) are likewise stringified into a misleading version. Guard the type and coerce falsy values to empty so the fallback/warning path is taken: `v = json.load(...).get("version") or ""; print(v if isinstance(v, str) else "")`.

## LOW (pass 2)

- [ ] **P2-197** `.codex/agents/evaluator.toml:20` _maintainability_
  The criterion maxima are duplicated inline while the same sentence declares `rubric.md` the source of truth. If rubric.md is revised (and its rubric version bumped), this copy silently mis-scores with no signal, and the prompt is the one the model reads on every run. Prefer keeping only the authoritative reference (or have the agent read maxima from `rubric.md`) instead of an unreferenced numeric copy.

- [ ] **P2-198** `.codex/agents/researcher.toml:5` _maintainability_
  The comment asserts high effort is "bounded" and offers to "pin it here to freeze behavior", but the file only sets `model_reasoning_effort = "high"`: no `model` is pinned (so behavior is not actually frozen to a specific model version) and there is no token/time bound (the pass limit lives only in the prose instructions). Reword so the comment doesn't imply containment the config doesn't provide, or pin `model` explicitly if freezing behavior is the intent.

- [ ] **P2-199** `.codex/agents/researcher.toml:26` _security_
  The rule says secrets must be redacted "everywhere they could persist" but the enumeration only names the brief and the `reports.create`/`learning.record` outputs. `documents.ingest` (stored artifact) and `knowledge.index` (persisted chunk text) also persist content outside the sandbox, so secret-bearing text routed through them is not covered. Either list those persistence destinations explicitly or narrow the wording to the three outputs.

- [ ] **P2-200** `.codex/config.toml:14` _maintainability_
  This comment block is orphaned and self-contradictory: the first sentence documents the semantics of a root key that is not present in the file, then the next sentence says that key is invalid. It reads like leftover documentation for a removed `persistent_instructions` line and will mislead future editors into thinking some prompt instruction is being appended when nothing is configured here. Collapse it to a single clear statement, e.g. "Project instructions live in AGENTS.md; there is no valid `persistent_instructions` key, so none is set."

- [ ] **P2-201** `.dockerignore:17` _maintainability_
  Redundant pair: `**/` matches any number of directories *including zero*, so `**/node_modules` already covers the root `node_modules`. The same duplication exists for `coverage`/`**/coverage` and `htmlcov`/`**/htmlcov` above. The extra plain patterns are dead rules and make the anchoring intent ambiguous (and prone to drift when edited); keep only the recursive form for clarity.

- [ ] **P2-202** `.gitattributes:32` _maintainability_
  Treating `.obj` as a binary mesh asset is questionable: Wavefront OBJ is a text format with no common binary variant, so `binary` (which sets `-diff -merge -text`) silently discards diffs and merge support for these files. It is also easily confused with the MSVC/C++ object-file extension of the same name, and the "demo assets" rationale in the comment above will not hold once any real (text) OBJ files are tracked. The binary `.stl`/`.glb` entries are justified; reconsider forcing `.obj`/`.gltf` (glTF is JSON) to binary. If the intent is only to protect specific binary fixtures, scope the rule to those paths instead of the whole extension.

- [ ] **P2-203** `.gitignore:41` _maintainability_
  This pattern is unanchored, so it ignores any directory named `judge-bundle` anywhere in the tree, not just the legacy root-level alias its comment describes. A legitimately tracked nested directory sharing that name would be silently excluded. If this is only meant to cover the root-level legacy alias, anchor it with a leading slash.

- [ ] **P2-204** `Dockerfile:2` _security_
  Base images are referenced by mutable tags only (`golang:1.27.0-alpine3.24` here and in the `quality` stage, `alpine:3.24.2` in `runtime-base`). A tag can be re-pointed upstream, so builds are not reproducible and are exposed to silent upstream mutation / supply-chain substitution. Pin each `FROM` by digest (e.g. `golang:1.27.0-alpine3.24@sha256:...`, `alpine:3.24.2@sha256:...`) and refresh digests deliberately via a dependency-update job.

- [ ] **P2-205** `cmd/demo-api/fly_live.go:225` _bug_
  On the read-limit jump, `skipPartial` is set unconditionally (`info.Size() > flyLiveReadLimit` guarantees `f.offset > 0`), so the first line in the new window is always discarded. If `info.Size() - flyLiveReadLimit` happens to land exactly on a line boundary, that first line is a complete event and it is dropped permanently: after this read `f.offset` reaches the current size and the unchanged fast-path prevents any re-read. In the worst case (the window contains a single recommend/feedback event) the only available event is lost. Consider deciding `skipPartial` from the byte preceding the new offset (e.g. probe byte `f.offset-1` via `file.ReadAt` and only skip when it is not `'\n'`) instead of assuming the window starts mid-line.

- [ ] **P2-206** `cmd/fly-bench/main.go:175` _maintainability_
  The stated determinism definition says the "full ranked order" was compared, but `sameOrder` only compares `Capability` strings and ignores `Score` (the field that actually drives ordering and is the only value persisted in the router's output). If the intent is to detect score-level nondeterminism, compare `Score` too; otherwise reword the claim so readers don't over-read what was measured.

- [ ] **P2-207** `cmd/fly-train/main.go:136` _performance_
  `vectorNorm(vectors[i])` is recomputed for every example on every epoch, doing `epochs * len(rows)` normalizations and slice allocations when the normalized vectors are constant across epochs. Precompute them once before the epoch loop.

- [ ] **P2-208** `cmd/hackathonkit-ctl/daemon.go:146` _bug_
  The ownership check (`readPID`) and the pid-file write are not atomic: two concurrent launches (two `daemon start`, or a start racing `foreground`) can both read "no live owner" and proceed, running duplicate daemons. Additionally `os.WriteFile` truncates and rewrites in place, so a concurrent `readPIDFile` can observe a partially written file and fail parsing. Write to a temp file and `os.Rename` for atomicity, and guard ownership with an exclusively-created lock file (e.g. `O_CREATE|O_EXCL`) rather than a plain check-then-write.

- [ ] **P2-209** `cmd/hackathonkit-ctl/daemon.go:282` _bug_
  The fallback identity check only requires the substring `hackathonkit-ctl` in the target's cmdline. If a stale pid file (one without a recorded executable path) is later recycled by an unrelated process whose argv contains that substring — including another `hackathonkit-ctl daemon ...` invocation — `readPID` will report it as ours and `daemonStop` will SIGTERM the wrong process. Match the exact executable (or require the `daemon foreground` argv) instead of a bare substring.

- [ ] **P2-210** `cmd/hackathonkit-ctl/detach_unix.go:1` _maintainability_
  The `unix` build constraint (POSIX: linux/darwin/bsd/solaris/aix/etc.) is paired only with the sibling `detach_windows.go` (`//go:build windows`). For any other GOOS (plan9, js/wasip1, wasip2) no file defines `detachSysProcAttr`, so `daemon.go:80` will not compile and the package fails to build for those targets. Since this helper is called unconditionally, either add a matching fallback stub (e.g. a `//go:build !unix && !windows` file returning a minimal `*syscall.SysProcAttr`) or explicitly document that only unix/windows are supported. Note also that the sibling `proc_unix.go` still uses `//go:build !windows`, so the build-tag scheme across the package is currently inconsistent.

- [ ] **P2-211** `cmd/hackathonkit-ctl/main.go:342` _bug_
  The session ID keys uniqueness on a second-resolution timestamp plus PID. Because the SQLite store is shared and PIDs are reused by the OS, back-to-back invocations in the same second can collide and cause the second `UpsertSession` to overwrite the first session's row. Use a monotonic/unique suffix (e.g. a random or nanosecond component) to guarantee uniqueness.

- [ ] **P2-212** `cmd/hackathonkit-ctl/proc_windows.go:37` _bug_
  The comment claims only a nonexistent pid (`ERROR_INVALID_PARAMETER`) is 'not alive', but this collapses *every* `OpenProcess` failure other than `ERROR_ACCESS_DENIED` into 'dead', and the `GetExitCodeProcess` branch below likewise returns false on any error. A transient/environmental failure (e.g. `ERROR_NOT_ENOUGH_MEMORY`) is therefore reported as a dead process, which lets `daemonStop` conclude 'not running' (and drop the pid file) while the daemon is still up. Either handle `ERROR_INVALID_PARAMETER` explicitly and treat other failures conservatively, or make the comment state that this probe fails closed.

- [ ] **P2-213** `cmd/hackathonkit-ctl/tui.go:46` _bug_
  The error from `store.Close()` is silently discarded, which can hide a failed WAL checkpoint/finalization on exit while `cmdTUI` still returns success (or only the `program.Run` error). Consider making the return value named and joining the close error, e.g. change the signature to `func cmdTUI(args []string) (retErr error)` and use `defer func() { if cerr := store.Close(); cerr != nil { retErr = errors.Join(retErr, cerr) } }()`.

- [ ] **P2-214** `cmd/hackathonkit-ctl/tui.go:258` _performance_
  When `rightWidth > 0`, `left.String()` is rendered twice: once for the initial `body` assignment and again inside `lipgloss.JoinHorizontal`. The first render result is discarded, wasting work on every redraw (which happens on each 2s tick). Restructure so the left pane is rendered exactly once.

- [ ] **P2-215** `cmd/hackathonkit-mcp/main.go:483` _bug_
  Returning the callback error aborts the entire traversal on the first unreadable entry, so `countFiles` returns a partial count; the subsequent `pending eval decisions: N` line then understates the real number (only a side note is written to diag). To keep counting readable directories while still surfacing the failure, log the error here and continue the walk (e.g. return `filepath.SkipDir` for an unreadable directory, `nil` otherwise) instead of propagating it.

- [ ] **P2-216** `cmd/hackathonkit-rehearse/main.go:287` _bug_
  Pipe file descriptors are leaked on the early-return error paths of startMCP: if `cmd.StdoutPipe()` fails after `cmd.StdinPipe()` already succeeded, the stdin writer is never closed; if `cmd.Start()` fails, both the stdin writer and the stdout reader remain open. Since the descriptors are never handed to any owner on these paths, they should be closed before returning.

- [ ] **P2-217** `cmd/hackathonkit-rehearse/main.go:417` _maintainability_
  `close()` is not idempotent and has no closed-state guard, yet it can be invoked more than once for the same client: once from the timeout branch inside `request()` (which sets `c.dead = true` and calls `c.close()`), and again from the `defer client.close()` in `run()`. Each invocation spawns a goroutine that calls `c.cmd.Wait()` and may call `c.cmd.Process.Kill()`. Calling `cmd.Wait()` a second time returns "Wait was already called" (ignored here) and the repeated Kill is redundant, so the shutdown contract is fragile. Guard with a `sync.Once`/`closed` flag so the process is reaped exactly once and `close()` is safe to call from any path.

- [ ] **P2-218** `docker-compose.yml:104` _maintainability_
  The comment describes Redis as "optional" because nothing consumes it yet, but the service has no `profiles:` entry, so a plain `docker compose up` always starts it and publishes host port 6379 (unused attack surface / resource use). If it is genuinely optional, gate it behind a profile so it is only started on demand (e.g. `docker compose --profile cache up`).

- [ ] **P2-219** `internal/atomicfile/atomicfile.go:55` _bug_
  The stale-temp sweep deletes any file matching `<base><tempMarker>*` older than one hour. The unique marker fixed the namespace collision with unrelated tools, but the residual race remains: a concurrent writer for the *same* path whose write is stalled/suspended (e.g. SIGSTOP, a blocked frozen process, or an extremely slow huge write) longer than an hour has its still-live temp file unlinked here, so its later `os.Rename` fails with ENOENT. Since the sweep is explicitly best-effort, consider tracking in-process temp names to exclude them, or document this residual race.

- [ ] **P2-220** `internal/atomicfile/atomicfile.go:70` _bug_
  `os.Chmod(dir, dirMode)` blindly tightens whatever directory the caller's path resolves to, including a pre-existing directory this package did not create. Worse, when `path` has no directory component `filepath.Dir` returns `"."`, so this chmods the process's current working directory to 0700 (and a root-level path such as `/state.json` would chmod the filesystem root). Guard against `dir == "."` (and the volume/FS root) and/or only tighten directories this call created.

- [ ] **P2-221** `internal/config/config.go:332` _bug_
  `Routing()` reads the file outside `m.mu` and only afterwards takes the lock to publish `m.routingCache`. A concurrent `SetRoutingMode` can write the new file, install the fresh cache under the lock, and then this call (which read the older snapshot earlier) clobbers the cache with stale state and returns the old mode right after the writer reported success. Consider re-checking/versioning under the lock (or comparing against the current version) before overwriting the cache.

- [ ] **P2-222** `internal/config/dotenv.go:119` _bug_
  Malformed double-quoted values are silently corrupted. A dangling backslash is dropped with no warning: because `escaped` is set to true on the last `\` and the loop then ends (e.g. `KEY="abc\` or a value that ends with `\` before an escaped closing quote), the pending backslash is never flushed. Similarly, an unterminated quote returns a partially parsed value instead of reporting anything. Since `LoadDotEnv` already logs invalid keys and `Setenv` failures, an unterminated/malformed quote should also be diagnosed rather than silently producing a different value.

- [ ] **P2-223** `internal/config/dotenv.go:132` _bug_
  Inline-comment stripping only recognizes the exact two-character sequence " #" (a space immediately before `#`) and only for unquoted values. A tab-separated trailing comment (`KEY=value\t# note`) is therefore kept as part of the value, while a leading `#` (`KEY=#foo`) is treated as data. This diverges from common dotenv parsers, which treat `#` as a comment start when preceded by any whitespace (or at the start of the value). Align the rule with the expected contract or document the deviation explicitly.

- [ ] **P2-224** `internal/control/handoff.go:40` _security_
  Raw internal error text is appended to Notes and later emitted by RenderHandoff into the markdown handoff, which is meant to be pasted into another agent session. Store/plan errors can embed filesystem paths and storage/query details, leaking internal information across that boundary. Consider logging the detailed error and recording only a generic (sanitized) reason in the handoff notes.

- [ ] **P2-225** `internal/control/handoff.go:65` _documentation_
  This counts every `eval_decision` event in the session window, but an `eval_decision` records a decision that was already made and has no unresolved state (see ingest/risk handling). Reporting the total as "pending eval decisions" (field `PendingDecisions`) gives the resuming agent a misleading workload signal. Consider renaming the field/output to reflect "recent eval decisions", or track an explicit pending/resolved state instead of deriving it from recorded-decision events.

- [ ] **P2-226** `internal/control/handoff.go:105` _bug_
  Event Type/Tool/Status and `plans.Summary` output are interpolated into the markdown without going through the whitespace-collapsing `line()` helper used for every other field. A value containing newlines or markdown (e.g. from an unusual hook payload, plan slug, stage ID, or stage status) can break the paste-ready block or inject extra list items. Apply `line()` to each component and to the `plans.Summary` output for consistency.

- [ ] **P2-227** `internal/control/ingest.go:211` _bug_
  Files smaller than the 256-byte window get no head fingerprint (`hasFP == false`), so rotation detection falls back to the size check alone. If a small JSONL stream (e.g. offset 100) is truncated and recreated with different content that is still larger than the old offset but under 256 bytes, `offset > info.Size()` is false and no fingerprint exists to compare, so the new file's head is silently skipped. This is a known limitation of the fixed window; consider storing a fingerprint for small files as well, or comparing a hash of `(size, first N bytes)` with `N` scaled to the file size.

- [ ] **P2-228** `internal/control/store.go:103` _bug_
  The path is concatenated into a `file:` URI without escaping. A path containing `?` or `#` (both legal in filesystem names, e.g. a repo checked out under a `feature#2` directory, or a user-supplied `--root`) is treated by SQLite as the query/fragment delimiter: the path is silently truncated and the remainder is parsed as DSN parameters, so the intended DB may not be opened, the `mode=ro` guard could be dropped in `OpenReadOnly`, or the WAL/busy_timeout pragmas can be lost. Build the path via `net/url` (which percent-encodes `?`/`#`) or escape it explicitly.

- [ ] **P2-229** `internal/control/store.go:315` _maintainability_
  One corrupt row aborts the entire listing: a single malformed `risk_reasons` value makes `Sessions`/`Session` (and the TUI / `sessions list`) fail outright, and `scanEvents` does the same for a malformed `payload`. This is inconsistent with the ingest path, which deliberately skips malformed records so "one bad record must not stall the stream". Consider tolerating a corrupt JSON value (leave the field nil and/or log it) so a single bad row cannot render the store unreadable.

- [ ] **P2-230** `internal/control/store.go:471` _documentation_
  Contract mismatch: the doc says an empty `sessionID` counts "all sessions", but the query filters `session_id = ?`, so passing `""` actually counts only session-less/global events (`session_id = ''`). A caller that follows the documented behavior will silently get a wrong (global-only) count. Either fix the comment to say "session-less events when sessionID is empty", or change the predicate (e.g. `(? = '' OR session_id = ?)`) to match the documented semantics.

- [ ] **P2-231** `internal/evaluation/bundle.go:388` _maintainability_
  The error from `globToRegexp` is silently discarded. This is only safe because `FreezeBundle` calls `validateExcludes` before `compileExcludes`; if that ordering ever changes (or the function is reused), an invalid glob yields a nil regex which `isExcluded` treats as a non-match, silently leaking files that were meant to be excluded. Propagate the error from `compileExcludes` (return `([]excludeRule, error)`) or make the failure explicit instead of dropping it.

- [ ] **P2-232** `internal/learning/proc_windows.go:37` _bug_
  GetExitCodeProcess returns STILL_ACTIVE (259) for a running process **and** for a process that terminated with exit code 259, so the two cases are indistinguishable here and a dead lock owner can be reported as alive. The only caller (`Store.lockFile`) uses this result to decide whether a lock file is stale: a false "alive" prevents reclamation, so after the 50 retries it fails with "learning store is locked by another process" while the stale lock file (and any future acquisitions) stay blocked. Disambiguate by waiting on the process handle with a zero timeout — WAIT_TIMEOUT means still running, WAIT_OBJECT_0 means it has exited — which requires requesting `SYNCHRONIZE` access (0x00100000) in addition to PROCESS_QUERY_LIMITED_INFORMATION above. Since that is an inherent Win32 quirk rather than a plain bug, consider at least documenting the caveat next to the constant comment if the collision is acceptable.

- [ ] **P2-233** `internal/mcp/jsonrpc.go:5` _bug_
  `Request` exposes the required `jsonrpc` field but nothing validates that it is `"2.0"`, and `Server.handle` (internal/mcp/server.go) dispatches on `Method` without ever checking it. A client sending `{"jsonrpc":"1.0",...}` (or omitting the field entirely) is therefore served normally instead of being rejected as an Invalid Request, which is non-compliant with JSON-RPC 2.0 / MCP. Consider adding a version-check helper here (e.g. `func (r *Request) HasValidVersion() bool`) and having the server reject a bad version with `CodeInvalidRequest`, or explicitly document that the version is intentionally advisory.

- [ ] **P2-234** `internal/mcp/jsonrpc.go:60` _maintainability_
  The error-code set omits the standard `CodeInvalidRequest = -32600`. Cases the spec classifies as Invalid Request (unsupported `jsonrpc` version, missing/empty `method`, malformed request object) currently fall through to `CodeMethodNotFound` or ad-hoc codes in `Server.handle`, producing spec-inconsistent error codes. Defining `-32600` here gives handlers a consistent way to signal invalid requests.

- [ ] **P2-235** `internal/mcp/registry.go:132` _maintainability_
  The `default` branch returns pointers, structs, and arrays unchanged. For pointer/struct values that internally reference maps/slices, the cloned schema still aliases registry state, contradicting the doc comment ("callers can never mutate registry state through a returned schema"). Either handle `reflect.Ptr`/`reflect.Struct`/`reflect.Array` or document the limitation explicitly.

- [ ] **P2-236** `internal/mcp/streams.go:8` _maintainability_
  Both helpers return the process globals directly. Since `os.Stdin`/`os.Stdout` are themselves package variables, a caller that wants a non-process stream must reassign those globals (affecting the whole process) or pass explicit `in`/`out` to `ServeStreams`. If per-instance stream injection is ever needed (e.g. alternate transports or parallel tests), consider storing overridable readers/writers on the Server instead of reading the globals here. Non-blocking design note — current behavior is correct.

- [ ] **P2-237** `internal/metadata/metadata.go:239` _bug_
  When `Close()` has already set `pool = nil`, every `Record*` returns `nil` (success) rather than an error, and `Enabled()` can still have reported `true` in the window before `Close` acquired the write lock. Records dropped during shutdown are thus indistinguishable from persisted ones. If these records are relied upon, return a sentinel (e.g. `errRecorderClosed`) so callers can detect the drop instead of silently losing data.

- [ ] **P2-238** `internal/objectstore/minio.go:102` _bug_
  The `Close` error is discarded. minio-go's GetObject returns a lazy reader whose `Close` releases the underlying HTTP response/connection and can surface a transport error that `ReadAll` may not have observed on all paths. Silently dropping it means a failed/truncated fetch can be reported to the caller as a clean read. Consider surfacing (or at least logging) the close error when the read otherwise succeeded, e.g. via a named return so the deferred close can amend the result.

- [ ] **P2-239** `internal/openai/client.go:99` _security_
  The error string embeds up to 400 chars of the raw provider response body. These errors propagate verbatim across the tool boundary (e.g. `audio.transcribe`/MCP `mcp.Fail(trace(ctx), terr.Error())`), so a gateway/proxy that echoes request content can leak text or the base64 image payload submitted to the model. Consider returning only a short, sanitized provider message (e.g. extracted `error.message`) rather than the raw body. The same pattern exists in `Transcribe` ("openai transcribe: status %d: %s").

- [ ] **P2-240** `internal/plans/plans.go:38` _maintainability_
  `DefaultStages` is an exported package-level slice, so its backing array is shared by every caller. Although the doc comment declares it immutable, the idiomatic and already-used pattern `plan.Stages = plans.DefaultStages` / `append(stages, plans.DefaultStages...)` makes accidental in-place mutation (e.g. `plan.Stages[i].Status = ...`) corrupt the template process-wide with no copy-on-read protection; `Save`'s nil check cannot repair it. Consider keeping the template unexported and exposing a copy-returning accessor so callers cannot alias the shared backing array.

- [ ] **P2-241** `internal/plans/plans.go:224` _bug_
  `CurrentStage` returns `false` both when every stage is done/cut and when the plan has no stages at all (e.g. a plan saved with an empty/nil stage list, which `Save` persists as `[]`). `Summary` then reports `all stages complete` for a plan that has not even been laid out, which is misleading in hooks/CLI output. Distinguish the empty-plan case.

- [ ] **P2-242** `internal/quality/proc_unix.go:16` _bug_
  Group-kill failures other than `ESRCH` (e.g. `EPERM`, or a group that still has live members) are silently discarded and the function returns no error, so the caller (`cmd.Cancel` in quality.go) cannot tell whether the tree was actually terminated. A failed group kill leaves compilers/grandchildren running while the timeout path appears to succeed. Return or at least log the non-ESRCH error so this failure is observable.

- [ ] **P2-243** `internal/quality/proc_windows.go:22` _security_
  `taskkill` is resolved through `PATH` (and on Windows the current-directory lookup rules) rather than from a fixed system location. If a binary named `taskkill.exe` is planted earlier in `PATH`/the working directory, it would be executed instead. Use the absolute path from the system directory, e.g. `filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")`.

- [ ] **P2-244** `internal/resources/matrix.go:211` _documentation_
  The doc says DegradedCapabilities lists capabilities "running on a deterministic fallback while a provider-backed variant exists", but the implementation returns every decision with `Degraded == true`. `Degraded` is also set (a) when the terminal chosen variant is provider-backed (a capability with no providerless variant) and (b) for a capability with no variants at all, in neither of which a deterministic fallback exists. Align the comment with the actual semantics (or narrow the filter) so callers' expectations about the deterministic-fallback contract hold.

- [ ] **P2-245** `internal/routing/fly/events.go:39` _performance_
  This takes the process-global `brainMu` around the CPU-bound `brain.scores` (O(KC) activation plus an O(KC log KC) sort), which serializes every Router's `loadBrain`/`ActivationSnapshot` across the process. It is also inconsistent with `mbCategoryScores` (mb.go:272-274), which relies on the documented invariant that published brains are immutable (applyDopamine clones and swaps) and scores without a lock. `scores`/`activate` return freshly allocated maps/slices, so the lock provides no protection here. Consider scoring without the lock (or add a comment explaining why this path differs).

- [ ] **P2-246** `internal/routing/fly/hash_encoder.go:113` _bug_
  The fallback error is flattened with `%v`, so its cause is dropped from the error chain: `errors.Is`/`errors.As` can only reach the primary error when both embedders fail. Since this module targets go 1.27, `fmt.Errorf` supports multiple `%w` verbs and produces a joined error, so both causes can be preserved.

- [ ] **P2-247** `internal/routing/fly/mb.go:454` _bug_
  `NewMBrain` stores the caller's slice directly instead of copying it. `Train` later does `b.Categories = append(b.Categories, category)`, so when the caller's slice has spare capacity the append writes into the caller's backing array, and any caller-side mutation of the slice leaks into the brain (and vice versa). Copy the slice on entry.

- [ ] **P2-248** `internal/routing/fly/router.go:130` _bug_
  Substring matching on short stems over-matches unrelated words and can inflate a category. Concretely, the `documents` stem "акт" also fires for "факт", "контакт", "практика", "характеристика"; the `classification` stem "логи" fires for "биологические"/"логистика"; the `geo` keyword "поля" fires for "полярный". A single incidental hit awards the category 0.3 and can change the reported route. Consider token/word-boundary matching or lengthening the ambiguous stems.

- [ ] **P2-249** `internal/skills/validate.go:119` _bug_
  When the skills directory exists but contains no skill subdirectories (or every entry was skipped because it is not a directory), `Total`/`Failed` both remain 0, so `OK` is reported `true` and the CLI exits 0 for an empty or relocated catalog. Add an explicit check that at least one skill was validated (or treat a zero-skill catalog as a failure) so silencing the catalog cannot pass the gate.

- [ ] **P2-250** `internal/skills/validate.go:188` _security_
  `filepath.IsAbs(ref)` is unreachable: both reference regexes force a `references/` prefix, so `ref` can never be absolute. The containment guarantee therefore rests entirely on the `EvalSymlinks` block below, but that block is skipped whenever `EvalSymlinks(dir)` or `EvalSymlinks(target)` returns an error, so a reference that resolves outside the skill directory via a symlink component is silently accepted instead of being reported. Consider resolving the paths unconditionally and treating a resolution failure as a problem (fail closed), and drop or replace the dead `IsAbs` guard.

- [ ] **P2-251** `internal/storage/store.go:31` _maintainability_
  `backendContext` always derives from `context.Background()`, so every `meta.RecordArtifact`/`objects.Put`/`objects.Get` call ignores the caller's deadline and cancellation. The callers (e.g. tool handlers in internal/tools) do hold a request `ctx`, but these Store methods take no `context.Context`, so a client disconnect cannot abort a stalled Postgres/MinIO call and the handler blocks up to `backendTimeout()` per backend call (two sequential calls in `WriteArtifact`). Consider threading `ctx` through these methods and deriving the timeout from it.

- [ ] **P2-252** `internal/storage/store.go:251` _security_
  Read path is not hardened against symlink races, while the write path is. `ensureWithin` verifies containment at check time, but the subsequent `os.ReadFile(full)` follows symlinks; if a component of `full` is replaced with a symlink pointing outside `s.root` between the check and the read, content outside the root is returned. `safeWriteFile` defends against exactly this (Lstat + temp-file + rename), so the asymmetry is likely unintentional. Consider opening with `O_NOFOLLOW`/`openat` (RESOLVE_BENEATH) or re-verifying via the opened fd. `ReadIndex`'s `os.ReadFile(full)` has the same gap.

- [ ] **P2-253** `internal/tools/context.go:50` _maintainability_
  `skillContextItems` deliberately returns partially collected `items` together with the walk error, but the handler discards them and returns `mcp.Fail` on any `skillErr`. Today only context cancellation propagates, where an error response is reasonable; however this makes the partial-return contract dead and would silently drop all tool/manifest items for any future non-cancellation error. Either have the caller surface the partial items, or drop the partial return and document that a scan error aborts the whole report.

- [ ] **P2-254** `internal/tools/context.go:310` _bug_
  `frontmatterBlock` treats any line whose trimmed content equals `---` as the closing fence and only signals success when a closing fence is found. Two problems: a `---` line appearing inside a multi-line description (e.g. an unquoted value) prematurely ends the block, and a SKILL.md whose frontmatter is unterminated silently yields an empty description, so the oversized-description flag is never raised. Since the block is fed to a real YAML parser, only treat a `---` at the expected document level as the terminator and surface a malformed/unterminated frontmatter condition rather than returning empty.

- [ ] **P2-255** `internal/tools/deps.go:99` _bug_
  `argFloat` parses string arguments verbatim, unlike `argString` which trims. A whitespace-padded numeric string such as `" 2.5 "` (or `"\n7"`) fails `strconv.ParseFloat` and silently falls back to `def`, so a value the client intended to supply is discarded. Trim before parsing for consistency.

- [ ] **P2-256** `internal/tools/deps.go:122` _maintainability_
  The out-of-range guard is fixed at 32-bit bounds even though the function returns a 64-bit `int` on common platforms. A legitimately large but valid value (e.g. a big `offset`/`max_chars`/`limit` above 2^31) is silently replaced by the caller's default, which is indistinguishable from an omitted argument — exactly the failure mode the comment above tries to avoid. Consider documenting the intentional int32 contract explicitly, or deriving the bound from the platform `int` width (noting float64 cannot represent MaxInt64 exactly, so an inclusive comparison needs care).

- [ ] **P2-257** `internal/tools/deps.go:175` _bug_
  The comment claims the shallow copy prevents handler mutation from leaking into the caller's argument map, but only the top level is detached: nested `map[string]any`/`[]any` values are still shared by reference. Handlers storing the result (e.g. `plan.MandatoryScenario`) or passing it to `Router.Recommend` can still mutate the caller's nested data. Either deep-copy the nested values or correct the comment to state that only the top level is detached and nested values are shared read-only.

- [ ] **P2-258** `internal/tools/documents.go:197` _bug_
  Off-by-one at the size boundary: `counter.n > maxDocxXMLBytes` is used to detect truncation, but the reader is limited to `maxDocxXMLBytes+1`. A well-formed `word/document.xml` whose size is exactly `maxDocxXMLBytes+1` is fully consumed and parses to `io.EOF`, so the final check still fires and the caller gets a spurious "exceeded the size limit; text was truncated" warning even though the text is complete. Using a read limit of `maxDocxXMLBytes+2` and flagging only when `counter.n > maxDocxXMLBytes+1` (in both the error branch and the post-loop check) distinguishes "exactly at the boundary" from "genuinely truncated".

- [ ] **P2-259** `internal/tools/documents.go:282` _performance_
  `len([]rune(text))` is evaluated twice for the same possibly multi-megabyte text (once for the metadata artifact, once for the response), and `snippet(text, 400)` further converts the entire string to runes internally just to return 400 characters. With a text-heavy DOCX near the 64 MiB extraction cap this means several redundant O(n) rune conversions and large transient allocations on the request path. Compute the rune count once and reuse it, and consider a byte-based fast path for the preview.

- [ ] **P2-260** `internal/tools/evaluation.go:39` _bug_
  Index collision when merging load failures with validation failures: `readReports` numbers its `InvalidReport.Index` over the combined inline+file input (including entries that failed to load), while `AggregateReports` numbers the reports it receives by their position in the already-filtered `reports` slice. A loaded report that fails schema validation can therefore receive the same `Index` as a load-failure entry (e.g. a missing path at index 1 and the next successfully-loaded report rejected at index 1), making the `dropped_invalid` entries ambiguous. Carry the original input index through to aggregation (or renumber after merging) if the index is meant to identify the caller's input.

- [ ] **P2-261** `internal/tools/evaluation.go:590` _bug_
  `os.CreateTemp` always creates the temp file with mode 0600, and `os.Rename` replaces the destination with it. Persisted artifacts (aggregate.json/calibration.json/decision/ranking) therefore end up 0600 regardless of umask, whereas the previous in-place write produced 0644. Any other repo tooling or judge process running as a different UID can no longer read them. Explicitly set the intended mode (e.g. `os.Chmod(tmpName, 0o644)`) before the rename, or chmod `full` afterwards.

- [ ] **P2-262** `internal/tools/extras.go:293` _bug_
  `renderChartSVG` dereferences `series[0].Points[0]` without a guard. Today the only caller (`registerCharts`) is protected because `parseChartSeries` guarantees a non-empty series with at least one point, but this is an implicit, undocumented precondition on an unexported general-purpose renderer. A cheap guard makes the invariant explicit and prevents a panic if the function is reused (e.g. a bar/line chart built from another source).

- [ ] **P2-263** `internal/tools/files.go:39` _security_
  `err.Error()` is forwarded verbatim to the client. `Store.InputPath` wraps the `os.Stat` failure with `%w` (internal/storage/store.go: `cannot stat input %s: %w`), and that inner error embeds the resolved absolute path, so an MCP client can receive server filesystem layout details on permission/ENOTDIR-type failures. Consider returning a generic message (e.g. the relative path) and logging the wrapped error internally.

- [ ] **P2-264** `internal/tools/knowledge.go:92` _bug_
  `pos` is a byte offset into `lower` (the lowercased text), but `utf8.RuneCountInString(lower[:pos])` yields a rune count in `lower` which is then applied to `r := []rune(text)`. `strings.ToLower` can change the rune count for some inputs (e.g. U+0130 'İ' lowercases to two code points), so the window start can be misaligned and the returned snippet may not contain the matched term. Compute the rune index from the same string used to search, or fold case in a rune-count-preserving way.

- [ ] **P2-265** `internal/tools/knowledge.go:311` _performance_
  This loop performs up to `maxIndexesPerSearch` file reads plus `json.Unmarshal` of each index with no `ctx.Err()` check, so a cancelled/timed-out request keeps doing I/O and allocating until the loop finishes. Add a `ctx.Err()` check at the top of the loop and return early on cancellation.

- [ ] **P2-266** `internal/tools/knowledge.go:315` _bug_
  The `maxIndexesPerSearch` guard is only applied when `len(allowed) == 0`. When the caller supplies `index_id`/`index_ids`, `scanned` increments but is never compared, so an explicit scope with a long list of ids forces reading and unmarshalling every matching index with no upper bound. Apply the cap regardless of whether an explicit scope was given (or bound the size of `allowed`).

- [ ] **P2-267** `internal/tools/learning.go:33` _security_
  Raw store errors are forwarded verbatim to the MCP client via `err.Error()` (same pattern in `learning.recall` and `learning.promote`). The learning store errors embed absolute filesystem paths, e.g. `learning store <abs>/learnings.json: <json error>` from `Store.load` and `write learning lock <abs>/learnings.json.lock: ...` from `Store.lockFile`, so internal paths and storage details leak across the tool boundary. Consider returning a sanitized message and logging the detail server-side.

- [ ] **P2-268** `internal/tools/plan.go:57` _bug_
  Schema/type mismatch with silent data loss. The stage property is declared as `"type": "number"`, but `argInt(m, "timebox_minutes", 0)` returns the default (0) for any non-integral value (`value != math.Trunc(value)`), so a caller sending `timebox_minutes: 20.5` silently gets a 0 timebox with no error or warning. Worse, a fractional negative such as `-1.5` also collapses to 0, so the `timebox < 0` guard below never fires for it. Either declare the property as `"type": "integer"` (matching the `int` field and rejecting fractions up front), or explicitly treat a supplied-but-unusable timebox as a validation failure in the handler.

- [ ] **P2-269** `internal/tools/plan.go:84` _maintainability_
  Inconsistent handling of the `stages` argument: a non-array value emits a `stages must be an array; default stages were used` warning, but an explicitly supplied empty array (`stages: []`) silently falls back to `plans.DefaultStages` with no warning. A caller that intended to override the stage list (e.g. an agent that derived an empty list) gets no signal that its input was discarded. Emit the same warning when `stages` is present but yields no usable stages, or document that an empty list intentionally means "use defaults".

- [ ] **P2-270** `internal/tools/quality.go:19` _bug_
  When the evidence directory exists but every `gate.json` is unreadable/corrupt, `LatestReport` returns `ok=false` with a non-nil `evidenceErr`. This branch then emits *both* "no gate evidence found; run scripts/quality-gate.sh" and "evidence could not be read: ...", which is self-contradictory: evidence was in fact found, it just could not be parsed. Agents keying off the first warning may conclude the gate never ran and simply re-run it instead of investigating the corrupt artifact. `plan.status` (internal/tools/plan.go:239-243) avoids this by replacing the message when an error is present. Prefer selecting the message based on `evidenceErr`.

- [ ] **P2-271** `internal/tools/security.go:79` _documentation_
  `scanned_chars` is set from `len(text)`, which counts bytes, not characters, and the accompanying warning also says "%d characters" while the limit is applied in bytes. For multibyte input both the reported size and the warning wording are misleading. Use `utf8.RuneCountInString(text)` (and reword the warning) if characters are intended, or rename to reflect bytes.

- [ ] **P2-272** `internal/tools/security.go:205` _bug_
  The zero-width character set omits common invisible controls such as ZWNJ (U+200C), ZWJ (U+200D), the word joiners (U+2060–U+2064), the soft hyphen (U+00AD) and the Arabic letter mark (U+061C). ZWJ/ZWNJ are frequently used to split tokens and evade pattern matching, so their absence is a detection gap in a tool whose purpose is catching hidden characters.

- [ ] **P2-273** `internal/tools/system.go:97` _security_
  The raw `err.Error()` from `observability.AppendJSONL` (and from `RecordFlyPrediction` below) is appended into `telemetryWarnings` and then returned to the MCP caller through `env.WithWarnings`; the same happens for `AddFeedback` errors returned via `mcp.Fail(trace(ctx), err.Error())`. These messages can embed absolute filesystem paths and backend details derived from `d.Store.Root()` / the metadata store, leaking internal layout across the tool boundary. Consider wrapping/redacting these errors (the repo already has `redactBackendError`) before surfacing them to callers.

- [ ] **P2-274** `internal/tools/system.go:149` _bug_
  When `emit_activation` is false (or absent from a caller that disabled it), `snapshot` stays a nil map, yet the handler still appends a `type: "recommend"` record to `fly-neuron-events.jsonl` whose `active_kc`, `mbon`, `kc_total`, `activation_available`, `activation_complete` and `active_kc_count` are all `null`. Reading a nil map is safe, but the emitted event is now indistinguishable from a snapshot that failed to capture, and `activation_available: null` breaks the boolean contract `ActivationSnapshot` uses (it always returns a real bool, never null). Either skip the neuron event when activation was not captured, or write explicit `activation_available: false` plus a reason.

- [ ] **P2-275** `internal/tools/timeseries.go:147` _maintainability_
  Inconsistent handling of out-of-range numeric args: `window` and `threshold` reset out-of-range values but also emit a warning, whereas `max_points` (here) and `limit` (below) silently coerce to their defaults. A caller passing e.g. `max_points: 10000` gets a truncated 120-point series with no diagnostic and cannot tell the request was altered. Emit a warning (as done for window/threshold) when these values are clamped, or reject them with an error.

- [ ] **P2-276** `internal/tools/util.go:193` _bug_
  The trailing-dash trim happens before the 60-rune cut, so truncation can leave the slug ending in '-'. Re-trim after slicing to keep the invariant that slugs never end (or start) with a dash.

- [ ] **P2-277** `internal/tools/xlsx.go:288` _bug_
  This limit check runs at the top of the loop while `out` is only appended on `</si>`, so it triggers only once `len(out)` has already reached `maxXLSXSharedStrings+1` — the cap actually permits one extra entry. Check after appending (or use `>=`) if the intent is to enforce the stated maximum exactly.

- [ ] **P2-278** `internal/tools/xlsx.go:438` _bug_
  Cells whose reference is at or beyond `maxXLSXCols` are dropped via `continue` *before* `budget.addCells` is charged. A crafted sheet can therefore contain millions of out-of-range cells that are fully decoded/parsed but never counted against `maxXLSXCells`, defeating the per-workbook cell cap (work is only bounded by the 256 MB sheet-byte cap). Charge the budget (or short-circuit the cell) before skipping so the cap still bounds total work.

- [ ] **P2-279** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.js:15` _bug_
  `fs.existsSync` followed by `spawnSync` is a check-then-use race (TOCTOU): the launcher could be removed/replaced between the two calls. Note also that if it disappears before `spawnSync`, `sh`/`cmd.exe` itself still spawns and the failure surfaces as a confusing child error (non-zero status) rather than this clear "not found" message. Rely on the `result.error` path as the authoritative safeguard rather than the pre-check.

- [ ] **P2-280** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.js:25` _maintainability_
  The non-Windows branch hardcodes `sh` and assumes it is resolvable from PATH; if `sh` is unavailable or in an unusual location, the failure message is only the generic spawn error. Consider making the shell overridable via an environment variable and emitting a clearer diagnostic when it cannot be found.

- [ ] **P2-281** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.js:31` _bug_
  Only `result.status` is mapped; `result.signal` is ignored. If the child launcher is terminated by a signal, `status` is `null` and the process reports a generic exit code 1, hiding the real termination cause and making failures harder to diagnose. Consider surfacing the signal in the error message and deriving the exit code from it (e.g., `128 + signal number`).

- [ ] **P2-282** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:42` _security_
  The root validation is satisfied by either artifact (`hooks.js` **or** `go.mod`), so any directory that merely contains a `go.mod` passes and is then trusted as the source of an `exec`'d binary (`$REPO_ROOT/bin/hackathonkit-mcp`). Since the whole point is to guarantee the derived root is a genuine Hackathon Agent Kit checkout, require both markers (or a more specific marker) to make the guard meaningful.

- [ ] **P2-283** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.test.sh:56` _test_
  The launcher is always invoked via `sh`, so its shebang and executable bit are never validated, and only the launcher file is copied into the temp repo. A broken/interpreter-mismatched shebang or a newly sourced sibling helper would go undetected. Consider `chmod +x` the copied launcher and invoking it directly (at least once) so the shebang/exec-bit path is covered.

- [ ] **P2-284** `plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.test.sh:63` _test_
  This assertion is order- and format-sensitive: it requires the injected `--repo-root` and its value to appear as an exact substring before the passthrough args. Any benign change (extra arguments, quoting, or appending arguments) makes it fail even though the launcher still resolves the correct root. Consider asserting each token/order independently (e.g. that the line starts with `local --repo-root <REPO>` and contains `--list-tools`) or documenting that the ordering is a hard contract.

- [ ] **P2-285** `plugins/hackathonkit/scripts/hooks.js:262` _security_
  `removeActive` performs a filesystem write (unlink) without the `ROOT_VALID` guard that every other mutating helper applies (`writeActive`, `appendJSONL`, `touchActive`, `ownedByRoot`). It is currently only reached after `ownedByRoot()` returns true, so it is not exploitable today, but it breaks the stated invariant that session state is only touched under a validated root and silently deletes under an unvalidated `safeCwd()` if the call flow ever changes. Add the guard for consistency/defense in depth.

- [ ] **P2-286** `plugins/hackathonkit/scripts/hooks.js:313` _maintainability_
  The `mtime` property pushed here is never read: sorting uses `started` (which already falls back to `stat.mtimeMs`) and the returned value maps `item.entry` only. This is dead state that also duplicates the fallback logic; drop the field so the intent (mtime as fallback for entries without `started_at`) stays in one place.

- [ ] **P2-287** `scripts/codex-config.test.sh:136` _test_
  The suite asserts the tracked repo config is untouched, but never hashes `$WORK/.codex/config.toml` even though the generator is pointed at `$WORK` as the project root and reads that file for budget derivation. A regression that rewrites/injects into the scratch project config would go undetected. Capture its hash before the runs and assert equality here alongside the tracked-config check.

- [ ] **P2-288** `scripts/codex-config.test.sh:145` _test_
  The hostile-value regression only exercises `startup_timeout_sec`. `tool_timeout_sec` is set to the benign `137`, so a fallback or injection bug specific to the `T` extraction path (including other shell metacharacters such as backticks, `$(...)`, or `&&`) would pass unnoticed. Add a second hostile case for `tool_timeout_sec` (and/or a payload using a different metacharacter) asserting it also falls back to the default and does not execute.

- [ ] **P2-289** `scripts/coverage-check.sh:2` _documentation_
  The header documents the threshold but not the exit statuses this script produces: 0 = at/above target, 1 = test/`go tool cover` failure or below target, 2 = invalid `HACKATHONKIT_COVERAGE_TARGET`. Callers (`internal/quality/quality.go`, CI, `/verify`) interpret the exit code, and this repo already expects scripts to document them (see the OCR-324 convention applied to `scripts/docker-check.sh`). Please document them here.

- [ ] **P2-290** `scripts/coverage-check.sh:13` _maintainability_
  The percentage validation logic is duplicated: `TARGET` is checked with a character-class glob plus a dot-count glob plus a separate digit requirement, while `TOTAL` repeats the character-class/dot-count globs without the digit arm. The two can easily drift apart (and the edge inputs like `.`, `.5`, `1.`, `1..2`, `101` are only exercised implicitly). Consider extracting a small `valid_percent()` helper used by both call sites and adding a shell test covering those edge values.

- [ ] **P2-291** `scripts/coverage-check.sh:78` _bug_
  This sorts the entire `go tool cover -func` output, which also contains the aggregate `total:\t(statements)\tNN.N%` line. The summary can therefore show up in the "worst coverage" list (and with few functions it displaces an actionable entry), even though it is not a function. Filter the summary line out before sorting.

- [ ] **P2-292** `scripts/docker-check.sh:127` _bug_
  Silent fallback: when the `docker compose port` scrape yields nothing (container not yet up, unexpected output format, or a resolution error), the script silently assumes host port 9000 and probes it. That can produce a false pass against an unrelated service already bound to 9000, or a confusing 'minio did not become ready' failure. Consider failing loudly (or reporting the raw `docker compose port` output) instead of guessing the port.

- [ ] **P2-293** `scripts/doctor.ps1:139` _maintainability_
  Unlike the `plugin-check.ps1`/`smoke.ps1` checks, the hook test has no existence guard. If `hooks.test.js` is missing, node exits non-zero and the failure is reported as a generic "session ownership test" failure, hiding the actual cause. The sibling `scripts/doctor.sh` guards for this; mirror that behavior here.

- [ ] **P2-294** `scripts/doctor.ps1:168` _bug_
  `[bool]$p.enabled` mis-parses string values: in PowerShell `[bool]"false"` is `$true` (any non-empty string is truthy). If the codex JSON ever encodes `enabled` as a string, an installed-but-disabled plugin would be reported as enabled. Compare explicitly.

- [ ] **P2-295** `scripts/doctor.sh:36` _bug_
  When jq is present but the CLI output is malformed or uses an unexpected schema, both `jq -e` calls fail and `PLUGIN_STATE` keeps its initial value `missing`, so a parse problem is reported as "run scripts/setup.sh". The python3/awk fallbacks distinguish this case via `unknown`; make jq consistent by validating the JSON first and setting `unknown` on failure.

- [ ] **P2-296** `scripts/doctor.sh:129` _bug_
  The mirror check mixes semantics: it compares the set of top-level directories against a recursive `SKILL.md` count. Matching top-level names plus equal recursive counts can still pass on divergent trees (e.g. a nested `SKILL.md` appearing under one existing skill while another skill loses its own), and `-name SKILL.md` matches directories too. Compare full relative file lists with `-type f`, or otherwise compare the same depth/kind on both sides.

- [ ] **P2-297** `scripts/doctor.sh:179` _maintainability_
  The pattern requires `"version"` to begin a line, so minified/compact JSON or a differently formatted file produces a spurious "could not parse" failure, and a nested `"version"` key could be picked up instead. Prefer a real JSON parser (jq/python3 are already used elsewhere) or relax the pattern.

- [ ] **P2-298** `scripts/doctor.sh:206` _bug_
  This direct binary invocation (as well as the `--validate-skills` call above) is not wrapped in `run_bounded`, so the timeout protection described in the header comment does not actually cover every external process. Route these calls through `run_bounded` for consistent bounding.

- [ ] **P2-299** `scripts/doctor.sh:207` _bug_
  If the local binary exists but neither jq nor python3 is available, `RESOURCES_PRESENT` stays `false`, so the check silently reports "not configured (run scripts/setup-resources.sh)" even when resources are configured. Add an explicit unknown/not-verifiable outcome, or an awk/grep fallback, so the result is not misleading.

- [ ] **P2-300** `scripts/env-set-test.sh:0` _maintainability_
  `file_mode` calls `exit 2` from inside `$(...)` when neither stat syntax works. Because it runs in a subshell the exit status is swallowed, only a stderr line is emitted, and the caller then produces an ordinary `[fail]` with an empty mode/detail. A broken harness therefore looks like a product bug. Prefer detecting the stat variant once up front (failing loudly) or propagating a distinguishable sentinel rather than a silent `exit 2` buried in command substitution.

- [ ] **P2-301** `scripts/env-set-test.sh:0` _test_
  Only a single negative path is covered (one invalid key). Other rejection branches of the helper are untested: empty FILE/KEY (usage, exit 2), symlink resolution failure / too many hops, the fatal mode-restore and chown failures, and stdin larger/multi-line negatives. Consider adding cases for these so regressions in the error paths are caught.

- [ ] **P2-302** `scripts/env-set-test.sh:0` _performance_
  Both live-lock cases force the helper through its full retry loop (10 iterations of `sleep 1`), so each blocked-writer invocation adds ~10s and these two tests add ~20s to the suite. If the helper can be given a shorter/overridable lock timeout (or the tests run in parallel), do so to keep the regression suite fast.

- [ ] **P2-303** `scripts/env-set.sh:41` _bug_
  `$(cat)` already strips *all* trailing newlines, so the following `${VALUE%"\n"}` is dead code, and intentional trailing blank lines in a supplied value are silently dropped rather than the "single trailing newline" the comment describes. If preserving trailing content matters, read the value without command substitution (e.g. via a temp file / `IFS= read -r` loop); otherwise adjust the comment to state that all trailing newlines are removed.

- [ ] **P2-304** `scripts/env-set.sh:159` _bug_
  `on_signal_env_set` always exits 130 regardless of the signal received. For SIGTERM the conventional exit status is 143; using 130 unconditionally misreports a terminal signal as an interrupt to callers/tests that inspect the status.

- [ ] **P2-305** `scripts/env-set.sh:206` _bug_
  The final install does not guard against `$FILE` being an existing directory (e.g. a symlink that resolves to a directory, or a typo). In that case `[ -f "$FILE" ]` was false so `INPUT=/dev/null`, and `mv` will move the temp file *into* the directory under its temp name, reporting success while the intended env file was never written. Add an explicit check (reject directories / non-regular targets) before the rename.

- [ ] **P2-306** `scripts/gen-codex-config.sh:47` _security_
  `toml_escape` only escapes backslash and double quote. A launcher path containing a control character (notably a literal newline, but also TAB/CR) would emit an invalid or multi-line TOML basic string, and on hosts without `python3` the validation that would otherwise catch it is skipped. Escape/handle control characters (or reject a path that is not a plain single-line string) to guarantee a well-formed `args` entry.

- [ ] **P2-307** `scripts/gen-mcp-manifest.ps1:24` _maintainability_
  `Resolve-Path` normalizes relative/`..` segments to an absolute path, but it does not reliably dereference symlinks/junctions (on Windows PowerShell 5.1 it returns the link path itself rather than its target). The preceding comment therefore overstates the guarantee that a symlinked root 'cannot ... redirect the launcher'. The absolute-path goal is met, but either correct the comment or resolve the link target explicitly if symlink dereferencing is actually required.

- [ ] **P2-308** `scripts/gen-mcp-manifest.ps1:30` _bug_
  On Windows PowerShell 5.1 `Get-Content -Raw` without `-Encoding` decodes a BOM-less file using the system ANSI code page, so a template containing non-ASCII bytes would be mojibake'd here and then re-written as UTF-8 into the manifest. The current template is ASCII-only, but the read should be explicit to avoid silently corrupting future non-ASCII content.

- [ ] **P2-309** `scripts/gen-mcp-manifest.ps1:39` _bug_
  The two substitutions are chained, so the second `Replace` re-scans the text inserted by the first. If the canonical root literally contains `{{MCP_LAUNCHER}}` (a directory named that), it is silently rewritten to `hackathonkit-mcp-launcher.cmd`, corrupting the recorded repository path while still producing valid JSON — the placeholder guard cannot catch this because no braces remain. Substitute in a single pass over the original template that never re-scans inserted values (or reject roots containing `{{`/`}}` with an explicit message).

- [ ] **P2-310** `scripts/gen-mcp-manifest.ps1:46` _bug_
  This guard scans the *rendered* manifest for any `{{...}}` sequence, but the canonical root has already been spliced in at this point. A repository whose absolute path legitimately contains double braces (e.g. `C:\work\{{demo}}\hackathonkit`, or a UNC path with such a share) therefore makes this check fail and aborts a perfectly valid manifest — the braces came from the root path as data, not from an unexpanded template placeholder. Prefer validating the *template* for unknown placeholders before substitution, or at minimum match only the two known tokens and treat braces in the resolved root as legitimate data.

- [ ] **P2-311** `scripts/gen-mcp-manifest.sh:12` _maintainability_
  `TEMPLATE` is derived from the pre-canonical `$ROOT` (line 12) but is used after `ROOT` has been rewritten to the canonical `pwd -P` value, and the `-d`/`-f` checks all validate the pre-canonical path while the manifest is written under the canonical path. Today this happens to work only because the script never `cd`s, but it makes the checks and the actual target implicitly depend on the current working directory. Recompute `TEMPLATE` after the canonicalisation block so every path is derived from the single resolved root.

- [ ] **P2-312** `scripts/gen-mcp-manifest.sh:21` _bug_
  `'${1:-}'` is empty whenever the caller did not pass a root, which is exactly the case where the *default* root (derived from `$0`/`dirname`) failed to resolve — so the diagnostic reports nothing useful and hides the offending path. Interpolate the resolved value (`$ROOT`) so the user can see (and act on) the path that actually failed.

- [ ] **P2-313** `scripts/gen-mcp-manifest.sh:62` _bug_
  The post-render guard greps the *output* for the two known tokens, but the awk loop above already replaces every occurrence of each token, so a leftover can only ever mean the substituted value itself contains the token. That makes this check ineffective at its stated purpose and able to false-positive: a canonical root that legitimately contains the literal `{{REPO_ROOT}}`/`{{MCP_LAUNCHER}}` (a valid directory name) aborts a perfectly good run. Conversely an unknown placeholder (e.g. `{{SOMETHING_ELSE}}`) in the template is never caught. Grep the *template* for placeholder tokens generically (or verify the awk exit status) instead of re-scanning the substituted output.

- [ ] **P2-314** `scripts/install-prompts.ps1:27` _bug_
  `IsPathRooted` also returns true for a drive-relative path such as `C:` (volume separator with no root), so a value like `CODEX_HOME=C:` passes validation and `Join-Path "C:" "prompts"` resolves relative to the current directory on that drive, writing prompts to an unintended location. Reject bare drive specs explicitly to guarantee the resolved destination is fully rooted.

- [ ] **P2-315** `scripts/install-prompts.sh:16` _security_
  `DEST_DIR` is derived directly from `CODEX_HOME`/`HOME` without normalization or validation, so a mis-set or attacker-influenced value can direct writes (install) and content-matching deletions (remove) into arbitrary directories. Consider normalizing/validating the value (e.g. reject relative or unexpected paths) before using it.

- [ ] **P2-316** `scripts/install-prompts.sh:70` _bug_
  `status` also follows symlinks: a symlinked destination that happens to match the source content is reported as installed, yet `install` would skip it as a non-regular destination. This makes `status` report success while install/remove behave differently. Treat non-regular destinations consistently (e.g. count them as missing or out of date).

- [ ] **P2-317** `scripts/plugin-check.ps1:8` _bug_
  `$codexCmd` is only used to test presence; the actual invocation uses bare `codex`, which is resolved anew with normal command precedence (alias/function before application). If a function or alias named `codex` exists it would run instead, yielding misleading results (and possibly invoking an unintended binary). Invoke the already-resolved executable explicitly.

- [ ] **P2-318** `scripts/plugin-check.ps1:191` _maintainability_
  The response validation only asserts that the `result` property exists and that `isError` is falsy. As written, `"result": null` passes (property present, `$null.isError` is falsy), an empty `tools/list` passes, and an `initialize` result is never checked for `protocolVersion`. Since the script's purpose is to prove the server actually works, validate at least that `result` is non-null and that ids 2/3 contain meaningful payloads. Also note duplicate ids silently overwrite the earlier response in `$responses`.

- [ ] **P2-319** `scripts/plugin-check.sh:129` _bug_
  `os.path.realpath()` does not require the path to exist; it resolves non-existent components lexically. So a missing/typo'd command (or a manifest pointing at a directory) passes the containment check and only fails later inside `subprocess.run` with an opaque OSError/IsADirectoryError, rather than a clear "command does not exist" message. The PowerShell sibling (`plugin-check.ps1:76`) explicitly checks existence. Also note `real_command == source` is allowed, which lets the plugin root directory itself qualify as a "command". Add an `os.path.isfile(real_command)` check (mirroring the PS script) before executing.

- [ ] **P2-320** `scripts/plugin-check.sh:138` _bug_
  The manifest's `env` block (if any) is ignored, so a server configured to rely on manifest-provided environment variables is started without them and the check reports a false failure. Consider merging `server.get("env")` into the subprocess environment, e.g. `env={**os.environ, **manifest_env}` when it is a dict.

- [ ] **P2-321** `scripts/quality-gate.sh:67` _maintainability_
  This EXIT trap duplicates the explicit `chown_evidence` call made just before each `exit`, and it is not failure-safe: `rm -rf "$TMP_DIR"` runs under `set -e`, so if the removal fails the trap aborts before `chown_evidence` and overrides the gate's real exit status. Let the removal failure be non-fatal so cleanup and the child's status are always preserved.

- [ ] **P2-322** `scripts/redact.sh:15` _security_
  Only the availability of `sed -E` is verified up front; there is no check that the actual redaction invocation succeeds. Because sed streams and buffers output, a runtime failure could still emit partial (potentially unredacted) output before `set -e` terminates the script, so the fail-closed guarantee described in the header is not fully upheld. Consider buffering through a temp file (redact to temp, then emit only on success) or trapping the exit to suppress output.

- [ ] **P2-323** `scripts/redact.sh:23` _security_
  This rule only fires for tokens that contain `://` and greedily consumes up to the last `@`, so scheme-less credentials (`user:pass@host`), and credential forms using other delimiters, pass through unredacted; conversely it over-redacts benign `user@host` tokens in paths/queries. The header acknowledges the over-redaction trade-off but the missed scheme-less credential case is a real gap if such inputs can occur.

- [ ] **P2-324** `scripts/redact.test.sh:44` _maintainability_
  Several assertions (here and on the two-quoted-value / json / unquoted-quote cases) rely on exact full-output equality. That is brittle: any harmless change in spacing or formatting in the redactor output makes the suite fail, discouraging legitimate refactors. Prefer targeted assertions (key redacted + secret absent + surrounding text intact) where the exact byte layout is not the behaviour under test.

- [ ] **P2-325** `scripts/repair.ps1:21` _maintainability_
  Inconsistent failure policy: sync-harness/gen-mcp-manifest/write-install-state abort the run on non-zero exit, but install-prompts only warns. If prompts are genuinely optional this is fine, but the asymmetry is easy to misread and no state is recorded that prompts were skipped. Either document the intentional difference or log it clearly so a partial install is discoverable.

- [ ] **P2-326** `scripts/repair.sh:46` _bug_
  Docker is only considered when both `docker` and `timeout` exist, so on hosts where `timeout` is unavailable (notably macOS without coreutils) the MCP image check is skipped entirely and RUNTIME silently degrades to `binary`, forcing a local Go rebuild or an outright failure even though a usable Docker image exists. If this is the intended tradeoff, consider documenting it here; otherwise bound the probe portably (background the command and kill it after a short wait) rather than gating on `timeout`.

- [ ] **P2-327** `scripts/set-version.sh:24` _maintainability_
  The regex accepts leading zeros (`01.002.0003`) and rejects valid SemVer forms such as `1.2.3-rc.1`/`1.2.3+build`. If leading-zero versions are never intended, the check is too permissive and could silently publish a malformed version; if prerelease/build metadata is intended to be supported, the pattern is too strict. Please confirm the intended semantics and tighten/document accordingly.

- [ ] **P2-328** `scripts/set-version.sh:34` _maintainability_
  `sed -i.bak` is not POSIX (the script declares `#!/usr/bin/env sh`) and its behavior varies across implementations. More importantly, because `set -e` is active, if `sed` fails (or the process is interrupted) the script aborts *before* `rm -f` runs, leaving stale `*.bak` files next to the target. Consider a temp-file + `mv` approach, or register a `trap 'rm -f "$MAIN.bak"' EXIT INT TERM` so backups are always cleaned up.

- [ ] **P2-329** `scripts/set-version.sh:55` _bug_
  This verification uses `$VERSION` inside an ERE, so the dots are treated as "any character". Version `0.17.0` would also match a value like `0x17y0`, weakening the edit verification. Since `sed` always writes the exact literal `"version": "$VERSION"`, verify with a fixed string instead.

- [ ] **P2-330** `scripts/set-version.sh:65` _bug_
  Same unescaped-regex issue as the JSON check: `$VERSION` is matched as a regex, so dots match any character. The `sed` above always emits `Status: v$VERSION.` literally, so a fixed-string match is both stricter and simpler.

- [ ] **P2-331** `scripts/set-version.test.sh:53` _test_
  This no-argument re-run reads the version from main.go and is *expected* to change nothing, so the idempotency comparison passes even if the re-run is a complete silent no-op. The separate exit-status check catches a crash, but not a regression where the no-arg path stops syncing altogether. To actually exercise re-sync, deliberately drift one file to a wrong version before the no-arg run and assert it gets corrected; also assert the exit status explicitly rather than relying on the `if/else` report.

- [ ] **P2-332** `scripts/setup-resources.sh:47` _maintainability_
  An empty value is treated as a no-op that leaves the key untouched, so re-running the questionnaire with an empty answer can neither clear a previously stored secret nor overwrite it — a stale credential silently survives while `resources.json` may flip the provider to `false`, leaving `.env` and the profile inconsistent. Either document this explicitly or provide a way to clear the key (e.g. write the empty value) so the stored state matches the recorded answer.

- [ ] **P2-333** `scripts/setup.ps1:68` _security_
  The placeholder blacklist only matches exact values, so common shipped placeholders such as `change-me-in-production`, `my-token` or `secret` are treated as real tokens and never regenerated, leaving the demo API protected by a guessable value. Since this decision governs the fail-closed auth, consider requiring the existing value to look random (e.g. a 32-char hex string) rather than enumerating a few known placeholders.

- [ ] **P2-334** `scripts/setup.ps1:76` _maintainability_
  `RandomNumberGenerator.Create()` returns an `IDisposable` that is never disposed (the whole call chain result is dropped). Prefer `using` / explicit disposal so the cryptographic provider handle is released deterministically.

- [ ] **P2-335** `scripts/setup.sh:151` _bug_
  The script still attempts `codex plugin add` even when the preceding `marketplace add` failed. Registering the plugin cannot succeed while the marketplace is unregistered, so this only produces a confusing second failure. The PowerShell sibling deliberately skips the install in this case (setup.ps1:197-207); gate the `plugin add` on the marketplace add succeeding.

- [ ] **P2-336** `scripts/smoke.ps1:55` _bug_
  `$proc` is a `System.Diagnostics.Process` obtained via `Start-Process -PassThru` and is never disposed in either branch, so its OS process handle stays open until GC. Dispose it (ideally in a `finally`) after the wait completes to release the handle deterministically.

- [ ] **P2-337** `scripts/smoke.test.sh:35` _maintainability_
  `check()` and `write_responder()` assign to `name`/`ok`/`detail`/`path`/`marker` as plain shell variables. POSIX sh has no `local`, so these leak into the global scope and can be clobbered by callers or by the command substitutions spanning the calls, which makes the harness fragile if reused or reordered. Use a subshell or subshell-scoped function bodies (or at minimum unique/`_`-prefixed names) to avoid accidental cross-talk.

- [ ] **P2-338** `scripts/smoke.test.sh:49` _maintainability_
  The marker is interpolated into the sed replacement unescaped. `marker` happens to be a fixed literal today (`binary`/`docker`), but any value containing `/`, `&`, or a newline would corrupt the generated stub or make sed fail. Escape the value or use a safely-quoted replacement so `write_responder` cannot silently produce a broken stub.

- [ ] **P2-339** `scripts/smoke.test.sh:95` _test_
  Forcing staleness with `sleep 1` relies on filesystem timestamp granularity. On coarse-mtime filesystems (or when both writes land in the same tick) `find ... -newer` can consider the binary not stale, so this test would nondeterministically use the local binary instead of compose. Set the binary's mtime explicitly to an old value so the ordering is guaranteed regardless of the clock resolution.

- [ ] **P2-340** `scripts/sync-harness.sh:210` _bug_
  `cmp` is used for mirror verification without first confirming it exists (unlike `sync-skills.sh`, which probes `diff`/`cmp` and refuses to publish otherwise). If `cmp` is unavailable, the command fails and every mirror is reported as "missing or stale", causing a spurious hard failure. Guard the availability of `cmp` (or fall back to `diff`/a byte-wise comparison) before treating a mismatch as fatal.

- [ ] **P2-341** `scripts/sync-skills.ps1:124` _maintainability_
  Orphan cleanup keys solely off the PID embedded in the directory name. If a PID is recycled to an unrelated live process, the orphan becomes invisible to the sweep forever and accumulates on disk; there is no age-based fallback. Consider also checking the artifact's age (or an owner-token file) in addition to liveness before skipping removal.

- [ ] **P2-342** `scripts/sync-skills.ps1:156` _bug_
  `Move-Item` to a destination that already exists as a directory moves the source *inside* it rather than replacing it. If an `$old` path with this PID already exists (PID reuse after a crash), `$dst` ends up nested under `$old`, and the later `Remove-Item $old -Recurse` then deletes the real mirror. Delete/guard an existing `$old` first, or use a guaranteed-unique backup name.

- [ ] **P2-343** `scripts/sync-skills.sh:32` _bug_
  The rollback only fires when `$DST` is absent, so a destination that is present but incomplete/wrong (e.g. a partially moved or externally-created directory) is left as-is while the good backup is deleted by the `rm -rf "$OLD"` below — the success path clears `OLD`, so this branch is only reached from `finish()`, where `$DST` existing means "swap succeeded". Also note `rm -rf "$OLD"` follows the path; if `$DST` were replaced by a symlink the existence test and removal semantics differ from what the comment claims. Make the restore condition explicit about "swap never happened" and validate that `$DST` is a real directory before deleting the backup.

- [ ] **P2-344** `scripts/sync-skills.sh:125` _bug_
  The lock is released (`rm -rf "$LOCK"`) before `LOCKED=0`, and the traps are not cleared until `trap - ...` below. A signal arriving in that window makes `finish()` remove `$LOCK` a second time (harmless) but more importantly `LOCKED` still reads `1`; set `LOCKED=0`/disable the lock cleanup before removing the directory so the state variables and filesystem stay consistent, and clear the traps as early as the work is actually complete.

- [ ] **P2-345** `scripts/uninstall.ps1:109` _maintainability_
  The dry-run preview hardcodes `$HOME\.codex`, but the actual removal is delegated to `install-prompts.ps1`, which honors `$env:CODEX_HOME` (and `$HOME` only as a fallback). When `CODEX_HOME` is set the printed path is wrong/misleading. Resolve the same prompts directory (or re-use the helper) for the message.

- [ ] **P2-346** `scripts/uninstall.ps1:115` _test_
  There is no test coverage for this destructive script (no test files reference `uninstall.ps1`). The riskiest logic — repo-root containment guard, reparse-point handling in `Remove-TreeNoReparse`, and the `Test-SameTree` user-skill preservation heuristic — has no automated boundary tests; consider adding dry-run/Pester tests around these paths.

- [ ] **P2-347** `scripts/uninstall.sh:90` _maintainability_
  Inconsistent failure accounting: this removal failure is only emitted as a "note" and does not increment CODEX_FAIL, whereas the failure in the corresponding branch above does. If the intent is best-effort, the note should say so explicitly; otherwise set CODEX_FAIL so the final exit code reflects it.

- [ ] **P2-348** `scripts/uninstall.sh:110` _bug_
  Inaccurate preview path: the prompts target is `${CODEX_HOME:-$HOME/.codex}/prompts`, not `$HOME/.codex/prompts`. When `CODEX_HOME` is set the dry-run message points at the wrong directory.

- [ ] **P2-349** `scripts/validate-skills.sh:35` _test_
  The wrapper exposes three distinct runtime paths and exit codes (0 = success, 1 = failure, 2 = no runtime) but has no self-test/harness reference, unlike sibling scripts covered by `scripts/smoke.test.sh`. A regression in any branch (e.g. the exit-code collapse above, a change to the guard, or the no-runtime path) would go unnoticed. Add cases that stub `bin/hackathonkit-mcp` and `go` on `PATH` and assert the exact status codes, stdout/stderr labels, and the no-runtime exit 2.

- [ ] **P2-350** `scripts/verify-command.test.sh:47` _maintainability_
  Ordering is inferred from the FIRST occurrence of each name across the whole file. If `build.md` mentions `system-design-expert` anywhere before the `cp-expert-refactoring` invocation (e.g. a summary, comment, or checklist), the assertion produces a false failure, and the `cut` at the end of the pipeline means a grep miss is never surfaced by `set -e`. Prefer checking the actual invocation order (e.g. line numbers of the specific invocation lines, or a targeted pattern) rather than raw byte offsets of the first mention.

- [ ] **P2-351** `scripts/verify-command.test.sh:105` _maintainability_
  These assertions encode installer internals (that `remove` keeps a modified `verify.md` but deletes an unmodified `plan.md`). They read as a behavior contract, but if `install-prompts.sh` changes strategy (e.g. removes the whole prompts dir or compares against a checksum instead of content) these checks silently depend on implementation details rather than a documented contract. Add a comment documenting the intended contract, or make the installer's preservation rules explicit so the test and implementation stay in sync.

- [ ] **P2-352** `scripts/write-install-state.ps1:19` _security_
  Validation of `docker-compose.yml`/`go.mod` is performed against the caller-supplied `$Root` before it is canonicalized, and the resolved path is then trusted for every read and for the final write. A symlink swapped between the validation step and this `Resolve-Path` call redirects the resolved root to an arbitrary directory whose writes are never validated. Resolve (canonicalize) the path first and then re-validate the canonical root before using it.

- [ ] **P2-353** `scripts/write-install-state.sh:32` _bug_
  The fallback regex allows arbitrary leading whitespace (`^[[:space:]]*`), so it is not anchored to the top-level key and will match an indented/nested `"version":` field (e.g. inside a dependency block) that appears before the top-level one, returning the wrong value. Consider anchoring to a column-0 key (`^"version"`) or, when the value cannot be reliably determined, warn instead of guessing.
