# Critic Checklists

Each critic walks its list against the read-only bundle and returns only findings with a reproduction.

## A - Bug Hunter

Runtime and error paths:

- [ ] Empty file, header-only file, single row, single column.
- [ ] Missing column, duplicate column, unexpected extra column.
- [ ] Wrong encoding (UTF-8 BOM, CP1251), mixed line endings, embedded delimiter.
- [ ] Numbers: negative, zero, NaN, Inf, very large, leading zeros, decimal comma.
- [ ] Division by zero and empty aggregates (mean of zero rows).
- [ ] Missing keys/fields in JSON/YAML; nested nulls; wrong types.
- [ ] Truncated or corrupt files (half a PDF, ZIP without a member).
- [ ] Timestamp formats outside the accepted set; timezone-free values.
- [ ] Long input: does it stream or load everything; what happens at 10x size.
- [ ] Error envelope: `status`, `warnings` and `isError` agree; no panic escapes.
- [ ] State: repeated calls with the same input stay deterministic; no global mutation leaks.

Evidence to require: `file:line`, the exact input, the command and its output.

## B - Logic / Product Skeptic

- [ ] Every feature claimed in the README is reachable through a tool or the demo UI.
- [ ] The mandatory scenario in the plan is the same scenario the demo runs.
- [ ] No dead code paths that no input can reach.
- [ ] The demo path uses real input, not a hardcoded fixture that hides a failure.
- [ ] No contradictions between the task statement, the plan and the README.
- [ ] Numbers in the pitch trace to an artifact, not to a claim.
- [ ] The weakest demo step is identified and its failure behaviour checked.
- [ ] User value: what decision does the output change for the user.
- [ ] A judge question likely to break the demo, turned into a finding if unanswered.

Evidence to require: artifact id or `file:line` plus the step where the contradiction shows.

## C - Reproducibility / Adversarial Reviewer

- [ ] Fresh clone + `scripts/setup.sh` in a clean environment (no leftover `.env`).
- [ ] Docker compose builds and starts without host-specific paths.
- [ ] Every required env var is documented in `.env.example`; a missing key degrades with a warning.
- [ ] No hardcoded absolute paths, usernames, ports or secrets in the bundle.
- [ ] Cold start: MCP `initialize` and `tools/list` before any warm-up.
- [ ] Malformed input returns an error envelope, not a crash or a hang.
- [ ] Timeouts: a slow backend does not hang the tool forever.
- [ ] Permissions: `data/`, `logs/` and `configs/` writable by the container user.
- [ ] Secret leakage: grep the bundle for tokens, keys and `.env` content.
- [ ] Normal/Fly routing toggle works without restarting the MCP process.
- [ ] `go build ./...`, `go vet ./...` and `go test ./...` pass from the bundle.

Evidence to require: command, output and the commit or bundle hash.

## Severity Examples

| Finding | Severity |
|---|---|
| mandatory scenario crashes on the sample input | critical |
| README install step misses an env var; setup fails | critical |
| demo works only because of a pre-existing local file | high |
| a feature listed in the README is unreachable | high |
| error message is misleading but the path works | medium |
| typo in the README | low |
