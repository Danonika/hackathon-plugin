---
description: Run the full verification gate (7 checks including test coverage) before claiming work is done.
argument-hint: "[--target N]"
---

Run the Hackathon Agent Kit verification gate against the current tree and report every check,
including test coverage. Arguments: $ARGUMENTS (optional `--target N` to override the
coverage threshold).

1. From the repository root, run the gate. With a target override use
   `HACKATHONKIT_COVERAGE_TARGET=N sh scripts/quality-gate.sh`; otherwise
   `sh scripts/quality-gate.sh`. The gate runs seven checks and writes
   `evals/quality/<sha>/gate.json`: gofmt, build, vet, test, coverage, selftest, skills.
2. Read `quality.gate` (or `evals/quality/<sha>/gate.json`) and report each check by
   name with its `status` and `duration_ms`. Quote coverage as `X% (target N%)`.
3. If any check failed or timed out, quote its output tail, fix the cause and rerun;
   never claim done on a failed or stale gate. Then run the mandatory scenario,
   confirm artifacts exist, and restate any envelope warnings.

Codex installs custom prompts from `~/.codex/prompts/`; copy this file there
(`cp .codex/prompts/verify.md ~/.codex/prompts/`) to use `/verify`.
