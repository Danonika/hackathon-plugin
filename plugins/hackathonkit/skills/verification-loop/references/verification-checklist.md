# Verification Checklist

Run the phases in order against the current tree. Each phase produces evidence; a claim may not be stronger than its weakest phase.

## Phase Table

| # | Phase | Exact command | Pass criteria | Evidence to keep |
|---|---|---|---|---|
| 1 | gofmt | `gofmt -l internal cmd` | empty output | command + empty output |
| 1 | build | `go build ./...` | exit 0 | exit code + output tail |
| 1 | vet | `go vet ./...` | exit 0 | exit code + output tail |
| 2 | tests | `go test ./...` | exit 0, no `FAIL` | package list tail |
| 2 | selftest | `go run ./cmd/hackathonkit-mcp --selftest` | exit 0, handshake ok | last lines |
| 3 | skills | `go run ./cmd/hackathonkit-mcp --validate-skills` | every skill `ok`, empty problems | summary + failing skill names |
| 4 | gate | `quality.gate` (MCP) | `ok: true`, `sha` matches tree | `sha`, `summary`, `checks[]` |
| 5 | scenario | `mandatory_scenario.command` on `input` | observed == `expected` | run log or artifact id |
| 6 | artifacts | resolve each `artifact_id` under `data/artifacts` | exists, non-empty, `status != error` | id + producing envelope |
| 7 | warnings | collect `warnings` from every envelope | all restated, none interpretation-changing | verbatim list |

The gate wraps phases 1-3 and stores them at `evals/quality/<sha>/gate.json`:

```sh
scripts/quality-gate.sh              # local runtime (go or bin/hackathonkit-mcp)
docker compose run --rm quality      # container path, Go-enabled image
```

## Phase 4 Details

`quality.gate` reads the latest evidence; it never runs checks. Read it only after the tree stopped changing.

- `available: false` (envelope `partial`) -> no `gate.json` exists; run the gate script and call the tool again.
- `ok: false` -> `failed[]` lists `<name> (<status>)`; each `checks[].output` is the tail of the failing command.
- Check statuses are `ok`, `failed` or `timeout`; all three must be reported by name.
- The gate is read-only with respect to source; it never edits files.

## Phase 5 Details

The mandatory scenario is the plan's contract, not a friendly demo.

- Copy `input`, `command` and `expected` verbatim from `plan.status`; do not reword the command.
- Run the command, capture the observed output, and compare it to `expected` field by field.
- If the scenario needs `data/input` that is missing, report the missing input and stop; do not pick a substitute file.
- Keep the run log or the produced `artifact_id` as the evidence pointer.

## Phase 6 Details

For each `artifact_id` in the answer:

- It is repository-relative to `data/artifacts`; confirm the file exists and is non-empty.
- The producing tool envelope (chart, report, ingest, transcribe, vision) returned `status != "error"` and a non-empty id.
- Reports and charts: open the preview or first lines and confirm the claimed content is visible.
- If an artifact was regenerated, verify the id you cite is the new one, not the previous run's.

## Phase 7 Details

- `warnings` from every envelope in the task are part of the contract; copy them, never summarize them away.
- A truncation warning changes what the data can support; a fallback warning changes the confidence of the result.
- Warnings do not fail the claim by themselves, but hiding one does; if a warning invalidates the claim, report `failed`.

## Claim Template

```text
Claim: verified
Gate: sha 9f2c41a ok (gofmt/build/vet/test/selftest/skills all ok)
Scenario: data/input/sample.csv -> expected anomaly list + chart + report; observed all three
Artifacts: reports/telemetry.md (ok), charts/telemetry-anomalies.svg (ok)
Warnings: documents.extract truncated at 12000 chars (expected, noted)
Decision: none needed
```

## Stale-Evidence Rule

Evidence binds to the tree that produced it. Before claiming:

- The gate `sha` must match the tree; a dirty tree after the gate invalidates it.
- New commits, new edits or regenerated artifacts require a rerun of the affected phases.
- The cheapest correct answer to a stale gate is to rerun `scripts/quality-gate.sh`, not to explain why the old run still counts.
