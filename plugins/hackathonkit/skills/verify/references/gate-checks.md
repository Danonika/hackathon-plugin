# Gate Checks

`scripts/quality-gate.sh` runs seven checks and writes
`evals/quality/<sha>/gate.json`.

| Check | Command | Reads |
|---|---|---|
| gofmt | `gofmt -l .` | formatting drift |
| build | `go build ./...` | compile errors |
| vet | `go vet ./...` | vet diagnostics |
| test | `go test ./...` | failing tests |
| coverage | `scripts/coverage-check.sh` | repo total vs target |
| selftest | `hackathonkit-mcp --selftest` | MCP wiring |
| skills | `hackathonkit-mcp --validate-skills` | SKILL.md standard |

Read the report with `quality.gate` or the JSON file. Report each check's
`status` and `duration_ms`. Quote the tail of `output` for any failure.
