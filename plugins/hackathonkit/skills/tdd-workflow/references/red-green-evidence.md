# Red-Green Evidence

Every TDD cycle leaves three checkpoints. A checkpoint is a small JSON object with the exact command and the observed output; it is what goes into `plan.update` evidence and the final answer.

## RED Checkpoint

The test must exist and fail on the intended assertion before any implementation code is written.

```json
{
  "phase": "red",
  "test": "internal/plans/plans_test.go::TestSlug/keeps_digits",
  "command": "go test ./internal/plans/... -run TestSlug -v -count=1",
  "expected": "Slug(\"S3 plan\") = \"s3-plan\"",
  "observed": "--- FAIL: TestSlug/keeps_digits: Slug(\"S3 plan\") = \"s3 plan\", want \"s3-plan\"",
  "status": "failed"
}
```

Reject a RED checkpoint when the output is a compile error, a missing fixture, a panic, or a pass. The failure line must name the test and the expected value.

## GREEN Checkpoint

```json
{
  "phase": "green",
  "test": "internal/plans/plans_test.go::TestSlug/keeps_digits",
  "command": "go test ./internal/plans/... -v -count=1",
  "observed": "--- PASS: TestSlug (0.00s) ... ok hackathonkit.local/agentkit/internal/plans",
  "change": "lowercase after replacing spaces and punctuation; digits preserved",
  "status": "passed"
}
```

The focused command runs first, then the package command. Record both; a focused pass alone is not GREEN.

## REFACTOR Checkpoint

```json
{
  "phase": "refactor",
  "commands": [
    "go test ./internal/plans/... -count=1",
    "go test ./... -count=1"
  ],
  "observed": "ok all packages",
  "change": "extracted normalizeToken; no behavior change",
  "status": "passed"
}
```

A refactor checkpoint is invalid if any test name, assertion or fixture changed. If behavior had to change, that is a new RED/GREEN cycle, not a refactor.

## Gate Checkpoint

Generated once per stage, after REFACTOR, against the current tree.

```json
{
  "phase": "gate",
  "command": "scripts/quality-gate.sh",
  "sha": "9f2c41a...",
  "ok": true,
  "checks": [
    {"name": "gofmt", "status": "ok", "duration_ms": 41},
    {"name": "build", "status": "ok", "duration_ms": 812},
    {"name": "vet", "status": "ok", "duration_ms": 640},
    {"name": "test", "status": "ok", "duration_ms": 5230},
    {"name": "selftest", "status": "ok", "duration_ms": 1104},
    {"name": "skills", "status": "ok", "duration_ms": 980}
  ]
}
```

The `sha` must match the working tree you are claiming about. If the tree changed after the gate, the checkpoint is stale and the gate must run again.

## plan.update Evidence

Pass evidence as strings; keep each one short and verifiable.

```json
{
  "slug": "telemetry-anomalies",
  "stage": "S3",
  "status": "done",
  "evidence": [
    "red: internal/plans/plans_test.go::TestSlug/keeps_digits failed on expected assertion",
    "green: go test ./internal/plans/... -count=1 ok",
    "gate: sha 9f2c41a ok (gofmt/build/vet/test/selftest/skills)",
    "learning: id L-0142 pattern table-driven slug cases"
  ]
}
```

## Final Answer Block

```text
TDD - stage S3 (telemetry-anomalies, revision 7)
RED    internal/plans/plans_test.go::TestSlug/keeps_digits  -> expected assertion failure
GREEN  go test ./internal/plans/... -count=1               -> ok
GATE   sha 9f2c41a ok; gofmt/build/vet/test/selftest/skills all ok
PLAN   S3 done, revision 7, evidence 4 items
```

If any line cannot be filled with a real command result, the stage is not done; say what is missing instead of writing the block.
